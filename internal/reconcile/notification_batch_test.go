package reconcile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/skaphos/oiax/v2/internal/notification"
	"github.com/skaphos/oiax/v2/internal/notification/notificationtest"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

// countingStore observes how many durable reads and writes a runtime performs
// through the LedgerStore boundary.
type countingStore struct {
	notification.LedgerStore
	mu             sync.Mutex
	reads, commits int
}

// receiptFailureStore injects failures after a sender arms it, so claim writes
// succeed and only the subsequent receipt commit is affected.
type receiptFailureStore struct {
	*notificationtest.MemoryStore
	mu        sync.Mutex
	err       error
	remaining int
}

func (s *receiptFailureStore) arm(err error, failures int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err, s.remaining = err, failures
}

func (s *receiptFailureStore) Commit(ctx context.Context, expected string, transition notification.Transition) (notification.Snapshot, error) {
	s.mu.Lock()
	if s.remaining > 0 {
		s.remaining--
		err := s.err
		s.mu.Unlock()
		return notification.Snapshot{}, err
	}
	s.mu.Unlock()
	return s.MemoryStore.Commit(ctx, expected, transition)
}

func (s *countingStore) Read(ctx context.Context) (notification.Snapshot, error) {
	s.mu.Lock()
	s.reads++
	s.mu.Unlock()
	return s.LedgerStore.Read(ctx)
}

func (s *countingStore) Commit(ctx context.Context, expected string, transition notification.Transition) (notification.Snapshot, error) {
	s.mu.Lock()
	s.commits++
	s.mu.Unlock()
	return s.LedgerStore.Commit(ctx, expected, transition)
}

func (s *countingStore) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads, s.commits
}

func (s *countingStore) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads, s.commits = 0, 0
}

// batchRuntime activates one webhook destination, admits n merge events and
// returns the runtime with a clock-advancing wait installed.
func batchRuntime(t *testing.T, clock *notificationtest.Clock, store notification.LedgerStore, n int) *NotificationRuntime {
	t.Helper()
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: "webhook", EndpointEnv: "AUDIT"}}}
	runtime := mergeRuntime(clock.Now, store, policy)
	runtime.Wait = func(ctx context.Context, delay time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		clock.Advance(delay)
		return nil
	}
	if err := runtime.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := make([]notification.EventV1, 0, n)
	for i := range n {
		events = append(events, mergeEvent(runtime.Repository, fmt.Sprintf("%d", 100+i), clock.Now().Add(time.Duration(i)*time.Second)))
	}
	if err := runtime.Admit(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	return runtime
}

func ledgerRecords(t *testing.T, store notification.LedgerStore) map[string]notification.DeliveryRecord {
	t.Helper()
	snapshot, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.Ledger.Deliveries
}

func TestNotificationDispatchBatchesLedgerWritesPerDestination(t *testing.T) {
	t.Parallel()
	clock := notificationtest.NewClock(time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC))
	store := &countingStore{LedgerStore: &notificationtest.MemoryStore{}}
	runtime := batchRuntime(t, clock, store, 5)
	sender := &notificationtest.Recorder{Result: notification.AttemptResult{Code: notification.OutcomeAccepted, Status: 204}}
	runtime.Sender = func(v1.NotificationDestination) notification.Sender { return sender }
	var diagnostics []NotificationDiagnostic
	runtime.Report = func(d NotificationDiagnostic) { diagnostics = append(diagnostics, d) }
	store.reset()
	start := clock.Now()
	if err := runtime.Dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	reads, commits := store.counts()
	// One read finds the due work, and every send re-observes the ledger
	// before contacting the receiver; only two writes are made.
	if commits != 2 || reads != 6 {
		t.Fatalf("five deliveries cost %d writes and %d reads; want one claim and one receipt write with five pre-send observations", commits, reads)
	}
	if len(sender.Payloads()) != 5 || len(diagnostics) != 5 {
		t.Fatalf("sends=%d diagnostics=%d", len(sender.Payloads()), len(diagnostics))
	}
	if clock.Now().Before(start.Add(4 * time.Second)) {
		t.Fatal("per-destination pacing was not observed inside the batch")
	}
	for _, d := range diagnostics {
		if d.Reason != "delivered" || d.Destination != "ops" {
			t.Fatalf("diagnostic = %+v", d)
		}
	}
	for key, record := range ledgerRecords(t, store) {
		if record.Status != notification.StatusDelivered || record.LastStatus != 204 || record.Lease != (notification.Lease{}) {
			t.Fatalf("record %s = %+v", key, record)
		}
	}
	snapshot, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d := snapshot.Ledger.Destinations["ops"]; d.Lease != (notification.Lease{}) || d.NextSendAt.Before(clock.Now().Add(time.Second)) {
		t.Fatalf("batch lease not released or pacing lost: %+v", d)
	}
	// Nothing is due afterwards, so a repeat run performs no write at all.
	store.reset()
	if err := runtime.Dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, commits := store.counts(); commits != 0 || len(sender.Payloads()) != 5 {
		t.Fatalf("idle run wrote %d times, sends=%d", commits, len(sender.Payloads()))
	}
}

func TestNotificationDispatchRenewsOnlyWhenBatchLeaseNearsExpiry(t *testing.T) {
	t.Parallel()
	clock := notificationtest.NewClock(time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC))
	store := &countingStore{LedgerStore: &notificationtest.MemoryStore{}}
	runtime := batchRuntime(t, clock, store, 3)
	sends := 0
	runtime.Sender = func(v1.NotificationDestination) notification.Sender {
		return notificationSenderFunc(func(context.Context, string, notification.DeliveryPayloadV1) notification.AttemptResult {
			sends++
			if sends == 1 {
				// A slow first receiver consumes more than half of the lease.
				clock.Advance(notification.ClaimDuration/2 + time.Second)
			}
			return notification.AttemptResult{Code: notification.OutcomeAccepted}
		})
	}
	store.reset()
	if err := runtime.Dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, commits := store.counts(); commits != 3 || sends != 3 {
		t.Fatalf("writes=%d sends=%d; want claim, one renewal and receipts", commits, sends)
	}
	for key, record := range ledgerRecords(t, store) {
		if record.Status != notification.StatusDelivered || record.Attempts != 1 {
			t.Fatalf("record %s = %+v", key, record)
		}
	}
}

func TestNotificationReceiptWrittenAfterStageCancellation(t *testing.T) {
	t.Parallel()
	clock := notificationtest.NewClock(time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC))
	store := &notificationtest.MemoryStore{}
	runtime := batchRuntime(t, clock, store, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.Sender = func(v1.NotificationDestination) notification.Sender {
		return notificationSenderFunc(func(context.Context, string, notification.DeliveryPayloadV1) notification.AttemptResult {
			// The budget expires after the receiver accepted the POST.
			cancel()
			return notification.AttemptResult{Code: notification.OutcomeAccepted, Status: 202}
		})
	}
	var diagnostics []NotificationDiagnostic
	runtime.Report = func(d NotificationDiagnostic) { diagnostics = append(diagnostics, d) }
	if err := runtime.Dispatch(ctx); err != nil {
		t.Fatalf("accepted POST after cancellation = %v", err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Reason != "delivered" {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	for key, record := range ledgerRecords(t, store) {
		if record.Status != notification.StatusDelivered || record.LastStatus != 202 || record.Lease != (notification.Lease{}) {
			t.Fatalf("receipt lost after cancellation: %s = %+v", key, record)
		}
	}
	// The record can never be re-sent.
	sender := &notificationtest.Recorder{Result: notification.AttemptResult{Code: notification.OutcomeAccepted}}
	runtime.Sender = func(v1.NotificationDestination) notification.Sender { return sender }
	runtime.OperationID = func() string { return "second-run" }
	clock.Advance(notification.ClaimDuration + time.Hour)
	if err := runtime.Dispatch(context.Background()); err != nil || len(sender.Payloads()) != 0 {
		t.Fatalf("delivered record replayed: sends=%d err=%v", len(sender.Payloads()), err)
	}
}

func TestNotificationAbandonedAttemptRecordsCanceledNotStaleCode(t *testing.T) {
	t.Parallel()
	clock := notificationtest.NewClock(time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC))
	store := &notificationtest.MemoryStore{}
	runtime := batchRuntime(t, clock, store, 2)
	refused := &notificationtest.Recorder{Result: notification.AttemptResult{Code: notification.OutcomeConfiguration, Status: 400}}
	runtime.Sender = func(v1.NotificationDestination) notification.Sender { return refused }
	if err := runtime.Dispatch(context.Background()); err == nil {
		t.Fatal("refused deliveries reported success")
	}
	for key, record := range ledgerRecords(t, store) {
		if record.Status != notification.StatusRetryable || record.Code != notification.OutcomeConfiguration || record.LastStatus != 400 {
			t.Fatalf("first attempt %s = %+v", key, record)
		}
	}
	clock.Advance(2 * time.Hour)
	runtime.OperationID = func() string { return "second-run" }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sends := 0
	runtime.Sender = func(v1.NotificationDestination) notification.Sender {
		return notificationSenderFunc(func(context.Context, string, notification.DeliveryPayloadV1) notification.AttemptResult {
			sends++
			// The budget expires during the first send; the second record's
			// POST must not be issued but its claim must still be settled.
			cancel()
			return notification.AttemptResult{Code: notification.OutcomeAccepted, Status: 200}
		})
	}
	var diagnostics []NotificationDiagnostic
	runtime.Report = func(d NotificationDiagnostic) { diagnostics = append(diagnostics, d) }
	err := runtime.Dispatch(ctx)
	if !errors.Is(err, context.Canceled) || sends != 1 {
		t.Fatalf("canceled batch = %v, sends=%d", err, sends)
	}
	reasons := map[string]int{}
	for _, d := range diagnostics {
		reasons[d.Reason]++
	}
	if reasons["delivered"] != 1 || reasons["notification-canceled"] != 1 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	delivered, canceled := 0, 0
	for key, record := range ledgerRecords(t, store) {
		if record.Lease != (notification.Lease{}) || record.Attempts != 2 {
			t.Fatalf("settled record still leased: %s = %+v", key, record)
		}
		switch record.Status {
		case notification.StatusDelivered:
			delivered++
		case notification.StatusRetryable:
			canceled++
			if record.Code != notification.OutcomeCanceled || record.LastStatus != 0 {
				t.Fatalf("abandoned attempt kept the earlier code: %s = %+v", key, record)
			}
			if !record.NextAttemptAt.Equal(clock.Now().Add(2 * time.Minute)) {
				t.Fatalf("abandoned attempt backoff = %s", record.NextAttemptAt)
			}
		default:
			t.Fatalf("unexpected status: %s = %+v", key, record)
		}
	}
	if delivered != 1 || canceled != 1 {
		t.Fatalf("delivered=%d canceled=%d", delivered, canceled)
	}
	snapshot, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Ledger.Destinations["ops"].Lease != (notification.Lease{}) {
		t.Fatal("abandoned batch left the destination fenced")
	}
}

func TestNotificationStatusFlowsToLedgerDiagnosticAndLog(t *testing.T) {
	t.Parallel()
	clock := notificationtest.NewClock(time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC))
	store := &notificationtest.MemoryStore{}
	runtime := batchRuntime(t, clock, store, 1)
	var logs bytes.Buffer
	runtime.Log = slog.New(slog.NewTextHandler(&logs, nil))
	runtime.Sender = func(v1.NotificationDestination) notification.Sender {
		return &notificationtest.Recorder{Result: notification.AttemptResult{Code: notification.OutcomeConfiguration, Status: 400}}
	}
	var diagnostics []NotificationDiagnostic
	runtime.Report = func(d NotificationDiagnostic) { diagnostics = append(diagnostics, d) }
	err := runtime.Dispatch(context.Background())
	var outcome notification.OutcomeError
	if !errors.As(err, &outcome) || outcome.Code != notification.OutcomeConfiguration || outcome.Status != 400 {
		t.Fatalf("dispatch error = %v", err)
	}
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	d := diagnostics[0]
	if d.Reason != string(notification.OutcomeConfiguration) || d.Status != 400 || !strings.Contains(d.Action, "(HTTP 400)") || strings.Contains(d.Action, "HTTPS, TLS, DNS") {
		t.Fatalf("diagnostic = %+v", d)
	}
	for key, record := range ledgerRecords(t, store) {
		if record.Status != notification.StatusRetryable || record.Code != notification.OutcomeConfiguration || record.LastStatus != 400 {
			t.Fatalf("ledger %s = %+v", key, record)
		}
	}
	line := logs.String()
	for _, want := range []string{`msg="notification attempt"`, "destination=ops", "reason=configuration-failure", "status=400", "elapsed_ms="} {
		if !strings.Contains(line, want) {
			t.Fatalf("attempt log missing %q: %s", want, line)
		}
	}
	for _, forbidden := range []string{"receiver.invalid", "secret", "pull/100"} {
		if strings.Contains(line, forbidden) {
			t.Fatalf("attempt log leaked %q: %s", forbidden, line)
		}
	}
}

// A policy accepted by another run while a batch is sending must stop the
// remaining sends: the claim snapshot is not a licence to post stale payloads.
func TestNotificationBatchStopsWhenPolicyChangesMidBatch(t *testing.T) {
	t.Parallel()
	clock := notificationtest.NewClock(time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC))
	store := &notificationtest.MemoryStore{}
	runtime := batchRuntime(t, clock, store, 3)
	oldOID := runtime.ConfigOID
	newOID := strings.Repeat("b", 40)
	retiring := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "other", Type: "webhook", EndpointEnv: "OTHER"}}}
	sender := &notificationtest.Recorder{Result: notification.AttemptResult{Code: notification.OutcomeAccepted, Status: 204}}
	runtime.Sender = func(v1.NotificationDestination) notification.Sender {
		return notificationSenderFunc(func(ctx context.Context, endpoint string, payload notification.DeliveryPayloadV1) notification.AttemptResult {
			result := sender.Send(ctx, endpoint, payload)
			if len(sender.Payloads()) == 1 {
				// Another run accepts a descendant revision that retires "ops"
				// between this batch's first and second send.
				current, err := store.Read(context.Background())
				if err != nil {
					t.Error(err)
					return result
				}
				if _, err := store.Commit(context.Background(), current.Revision, func(_ context.Context, l *notification.LedgerV1) (*notification.LedgerV1, error) {
					return notification.AcceptPolicy(l, notification.PolicyRevisionV1{ConfigOID: newOID, PolicyDigest: policyDigest(retiring)}, retiring, clock.Now(), notification.RevisionEvidence{AcceptedOID: oldOID, IncomingOID: newOID, Relation: notification.RevisionDescendant})
				}); err != nil {
					t.Error(err)
				}
			}
			return result
		})
	}
	err := runtime.Dispatch(context.Background())
	if err == nil || !errors.Is(err, notification.ErrStaleRevision) {
		t.Fatalf("mid-batch policy change was not reported as stale: %v", err)
	}
	if got := len(sender.Payloads()); got != 1 {
		t.Fatalf("stale payloads were sent after the policy changed: sends=%d", got)
	}
	first := notification.EventID(runtime.Repository, "100", v1.NotificationRequestMerged)
	for key, record := range ledgerRecords(t, store) {
		switch {
		case record.EventID == first && record.Status != notification.StatusDelivered:
			t.Fatalf("first send lost its receipt: %s = %+v", key, record)
		case record.EventID != first && record.Status != notification.StatusSkipped:
			t.Fatalf("retired record was not left retired: %s = %+v", key, record)
		}
	}
	snapshot, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d := snapshot.Ledger.Destinations["ops"]; d.Lease != (notification.Lease{}) {
		t.Fatalf("batch lease was not released after stopping: %+v", d)
	}
}

// One event whose message cannot be rendered stays pending by itself; the
// destination's other due records are still claimed and sent in the batch.
func TestNotificationBatchIsolatesUnrenderableRecord(t *testing.T) {
	t.Parallel()
	clock := notificationtest.NewClock(time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC))
	store := &notificationtest.MemoryStore{}
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: "webhook", EndpointEnv: "AUDIT"}}}
	runtime := mergeRuntime(clock.Now, store, policy)
	runtime.Wait = func(ctx context.Context, delay time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		clock.Advance(delay)
		return nil
	}
	sender := &notificationtest.Recorder{Result: notification.AttemptResult{Code: notification.OutcomeAccepted, Status: 204}}
	runtime.Sender = func(v1.NotificationDestination) notification.Sender { return sender }
	var diagnostics []NotificationDiagnostic
	runtime.Report = func(d NotificationDiagnostic) { diagnostics = append(diagnostics, d) }
	if err := runtime.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	good := mergeEvent(runtime.Repository, "100", clock.Now())
	bad := mergeEvent(runtime.Repository, "101", clock.Now().Add(time.Second))
	bad.Request.Source = strings.Repeat("x", 13<<10) // fixed facts exceed their limit
	last := mergeEvent(runtime.Repository, "102", clock.Now().Add(2*time.Second))
	if err := runtime.Admit(context.Background(), []notification.EventV1{good, bad, last}); err != nil {
		t.Fatal(err)
	}
	diagnostics = nil // activation reported its own cutoff diagnostic
	err := runtime.Dispatch(context.Background())
	if !errors.Is(err, notification.ErrCapacity) {
		t.Fatalf("unrenderable record was not reported: %v", err)
	}
	if got := len(sender.Payloads()); got != 2 {
		t.Fatalf("renderable records were blocked: sends=%d", got)
	}
	reasons := map[string]int{}
	for _, d := range diagnostics {
		reasons[d.Reason]++
	}
	if reasons["delivered"] != 2 || reasons["notification-capacity-exhausted"] != 1 || len(diagnostics) != 3 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	for key, record := range ledgerRecords(t, store) {
		switch {
		case record.EventID == bad.ID && (record.Status != notification.StatusPending || record.Message != nil || record.Attempts != 0):
			t.Fatalf("unrenderable record was claimed or altered: %s = %+v", key, record)
		case record.EventID != bad.ID && record.Status != notification.StatusDelivered:
			t.Fatalf("renderable record not delivered: %s = %+v", key, record)
		}
	}
}

// advanceOnCommitStore accepts a descendant policy revision that retires the
// destination immediately before the runtime's first commit, so the claim
// transition runs against a ledger whose revision no longer matches the run.
type advanceOnCommitStore struct {
	*notificationtest.MemoryStore
	mu       sync.Mutex
	advanced bool
	oldOID   string
	newOID   string
	policy   *v1.NotificationPolicy
	now      func() time.Time
}

func (s *advanceOnCommitStore) Commit(ctx context.Context, expected string, transition notification.Transition) (notification.Snapshot, error) {
	s.mu.Lock()
	first := !s.advanced
	s.advanced = true
	s.mu.Unlock()
	if first {
		current, err := s.Read(ctx)
		if err != nil {
			return notification.Snapshot{}, err
		}
		advanced, err := s.MemoryStore.Commit(ctx, current.Revision, func(_ context.Context, l *notification.LedgerV1) (*notification.LedgerV1, error) {
			return notification.AcceptPolicy(l, notification.PolicyRevisionV1{ConfigOID: s.newOID, PolicyDigest: policyDigest(s.policy)}, s.policy, s.now(), notification.RevisionEvidence{AcceptedOID: s.oldOID, IncomingOID: s.newOID, Relation: notification.RevisionDescendant})
		})
		if err != nil {
			return notification.Snapshot{}, err
		}
		expected = advanced.Revision
	}
	return s.MemoryStore.Commit(ctx, expected, transition)
}

// A save that fails for every record (here: the revision moved between the
// due scan and the claim write) must report the batch as stale, never panic
// on a ledger that a failed save left nil.
func TestNotificationBatchSurvivesSaveFailureForEveryRecord(t *testing.T) {
	t.Parallel()
	clock := notificationtest.NewClock(time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC))
	memory := &notificationtest.MemoryStore{}
	runtime := batchRuntime(t, clock, memory, 3)
	store := &advanceOnCommitStore{
		MemoryStore: memory,
		oldOID:      runtime.ConfigOID,
		newOID:      strings.Repeat("b", 40),
		policy:      &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "other", Type: "webhook", EndpointEnv: "OTHER"}}},
		now:         clock.Now,
	}
	runtime.Store = store
	sender := &notificationtest.Recorder{Result: notification.AttemptResult{Code: notification.OutcomeAccepted}}
	runtime.Sender = func(v1.NotificationDestination) notification.Sender { return sender }
	err := runtime.Dispatch(context.Background())
	if !errors.Is(err, notification.ErrStaleRevision) {
		t.Fatalf("stale batch = %v", err)
	}
	if got := len(sender.Payloads()); got != 0 {
		t.Fatalf("stale batch reached the receiver: sends=%d", got)
	}
	for key, record := range ledgerRecords(t, memory) {
		if record.Message != nil || record.Attempts != 0 || record.Status != notification.StatusSkipped {
			t.Fatalf("retired record was touched by the stale run: %s = %+v", key, record)
		}
	}
}

// hookSender runs a hook after its first send, so a test can change the
// durable ledger between two sends of one batch.
type hookSender struct {
	mu    sync.Mutex
	sends int
	hook  func()
}

func (s *hookSender) Send(context.Context, string, notification.DeliveryPayloadV1) notification.AttemptResult {
	s.mu.Lock()
	s.sends++
	first := s.sends == 1
	s.mu.Unlock()
	if first && s.hook != nil {
		s.hook()
	}
	return notification.AttemptResult{Code: notification.OutcomeAccepted, Status: 204}
}

func (s *hookSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sends
}

// A record settled by another attempt while the batch is sending is skipped
// alone: the batch still owns the destination and its remaining records are
// still sent.
func TestNotificationBatchSkipsRecordSettledByLateResult(t *testing.T) {
	t.Parallel()
	clock := notificationtest.NewClock(time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC))
	store := &notificationtest.MemoryStore{}
	runtime := batchRuntime(t, clock, store, 3)
	ctx := context.Background()

	// An older run claimed the second record and hung; its lease has expired.
	snapshot, err := store.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	keys := notification.DueDeliveries(snapshot.Ledger, clock.Now())
	if len(keys) != 3 {
		t.Fatalf("due=%d", len(keys))
	}
	second := keys[1]
	const oldAttempt = "old-attempt"
	if _, err := store.Commit(ctx, snapshot.Revision, func(_ context.Context, l *notification.LedgerV1) (*notification.LedgerV1, error) {
		l, err := notification.SaveMessage(l, runtime.ConfigOID, second, notification.RenderedMessageV1{Title: "t", Body: "b"})
		if err != nil {
			return nil, err
		}
		return notification.Claim(l, runtime.ConfigOID, second, oldAttempt, clock.Now())
	}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(notification.ClaimDuration + time.Second)

	sender := &hookSender{}
	sender.hook = func() {
		// The hung attempt's accepted response lands after this batch's first
		// POST: the second record becomes delivered by that older attempt.
		current, err := store.Read(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		if _, err := store.Commit(ctx, current.Revision, func(_ context.Context, l *notification.LedgerV1) (*notification.LedgerV1, error) {
			return notification.RecordResult(l, second, oldAttempt, notification.AttemptResult{Code: notification.OutcomeAccepted, Status: 200}, clock.Now())
		}); err != nil {
			t.Error(err)
		}
	}
	runtime.Sender = func(v1.NotificationDestination) notification.Sender { return sender }
	var diagnostics []NotificationDiagnostic
	runtime.Report = func(d NotificationDiagnostic) { diagnostics = append(diagnostics, d) }
	err = runtime.Dispatch(ctx)
	if !errors.Is(err, notification.ErrClaimLost) {
		t.Fatalf("settled record was not reported: %v", err)
	}
	if got := sender.count(); got != 2 {
		t.Fatalf("batch did not continue past the settled record: sends=%d", got)
	}
	reasons := map[string]int{}
	for _, d := range diagnostics {
		reasons[d.Reason]++
	}
	if reasons["delivered"] != 2 || reasons["delivery-claim-lost"] != 1 || len(diagnostics) != 3 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	records := ledgerRecords(t, store)
	for i, key := range keys {
		record := records[key]
		if record.Status != notification.StatusDelivered {
			t.Fatalf("record %d = %+v", i+1, record)
		}
	}
	if record := records[second]; record.LastStatus != 200 || record.Attempts != 2 {
		t.Fatalf("late result was not the authoritative receipt: %+v", record)
	}
	snapshot, err = store.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d := snapshot.Ledger.Destinations["ops"]; d.Lease != (notification.Lease{}) {
		t.Fatalf("batch lease not released: %+v", d)
	}
}

// A runtime wired without a sender must fail closed before claiming anything,
// never invent a receiver refusal for a request that was not made.
func TestNotificationBatchNilSenderIsInvalidState(t *testing.T) {
	t.Parallel()
	clock := notificationtest.NewClock(time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC))
	store := &countingStore{LedgerStore: &notificationtest.MemoryStore{}}
	runtime := batchRuntime(t, clock, store, 2)
	runtime.Sender = func(v1.NotificationDestination) notification.Sender { return nil }
	var diagnostics []NotificationDiagnostic
	runtime.Report = func(d NotificationDiagnostic) { diagnostics = append(diagnostics, d) }
	store.reset()
	err := runtime.Dispatch(context.Background())
	if !errors.Is(err, notification.ErrInvalidState) {
		t.Fatalf("nil sender = %v", err)
	}
	if _, commits := store.counts(); commits != 0 {
		t.Fatalf("nil sender claimed work: commits=%d", commits)
	}
	if len(diagnostics) != 1 || diagnostics[0].Reason != "invalid-notification-state" {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	for key, record := range ledgerRecords(t, store) {
		if record.Status != notification.StatusPending || record.Attempts != 0 {
			t.Fatalf("record touched: %s = %+v", key, record)
		}
	}
}

// An accepted POST whose receipt write fails keeps the receiver's status on
// the uncertainty diagnostic, so the operator sees what the receiver answered.
func TestNotificationUncertainReceiptKeepsReceiverStatus(t *testing.T) {
	t.Parallel()
	clock := notificationtest.NewClock(time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC))
	memory := &notificationtest.MemoryStore{}
	runtime := batchRuntime(t, clock, memory, 1)
	runtime.Sender = func(v1.NotificationDestination) notification.Sender {
		return notificationSenderFunc(func(context.Context, string, notification.DeliveryPayloadV1) notification.AttemptResult {
			memory.WriteError = notification.ErrUnavailable
			return notification.AttemptResult{Code: notification.OutcomeAccepted, Status: 202}
		})
	}
	var diagnostics []NotificationDiagnostic
	runtime.Report = func(d NotificationDiagnostic) { diagnostics = append(diagnostics, d) }
	err := runtime.Dispatch(context.Background())
	if !errors.Is(err, notification.ErrReceiptUncertain) {
		t.Fatalf("uncertain receipt = %v", err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Reason != "accepted-receipt-uncertain" || diagnostics[0].Status != 202 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}

func TestNotificationFailedPayloadReceiptRemainsClaimedAndReportsNonDurable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		err      error
		failures int
	}{
		{name: "unavailable", err: notification.ErrUnavailable, failures: 1},
		{name: "exhausted conflict", err: notification.ErrConflict, failures: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clock := notificationtest.NewClock(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
			store := &receiptFailureStore{MemoryStore: &notificationtest.MemoryStore{}}
			runtime := batchRuntime(t, clock, store, 1)
			run := 0
			runtime.OperationID = func() string {
				run++
				return fmt.Sprintf("run-%d", run)
			}
			failReceipt := true
			sends := 0
			runtime.Sender = func(v1.NotificationDestination) notification.Sender {
				return notificationSenderFunc(func(context.Context, string, notification.DeliveryPayloadV1) notification.AttemptResult {
					sends++
					if failReceipt {
						store.arm(tc.err, tc.failures)
					}
					return notification.AttemptResult{Code: notification.OutcomePayloadTooLarge}
				})
			}
			var diagnostics []NotificationDiagnostic
			runtime.Report = func(d NotificationDiagnostic) { diagnostics = append(diagnostics, d) }
			err := runtime.Dispatch(context.Background())
			if !errors.Is(err, notification.ErrReceiptNotPersisted) || !errors.Is(err, tc.err) {
				t.Fatalf("dispatch = %v", err)
			}
			if len(diagnostics) != 1 || diagnostics[0].Reason != "delivery-receipt-not-persisted" || diagnostics[0].Status != 0 || !strings.Contains(diagnostics[0].Action, string(notification.OutcomePayloadTooLarge)) || strings.Contains(diagnostics[0].Action, "terminal") || strings.Contains(diagnostics[0].Action, "not automatically resent") {
				t.Fatalf("diagnostics = %+v", diagnostics)
			}
			for key, record := range ledgerRecords(t, store) {
				if record.Status != notification.StatusClaimed || record.Attempts != 1 || record.Code != "" {
					t.Fatalf("failed receipt changed durable record %s = %+v", key, record)
				}
			}

			failReceipt = false
			clock.Advance(notification.ClaimDuration + time.Second)
			diagnostics = nil
			if err := runtime.Dispatch(context.Background()); err == nil || !strings.Contains(err.Error(), string(notification.OutcomePayloadTooLarge)) {
				t.Fatalf("resend = %v", err)
			}
			if sends != 2 || len(diagnostics) != 1 || diagnostics[0].Reason != string(notification.OutcomePayloadTooLarge) {
				t.Fatalf("sends = %d, diagnostics = %+v", sends, diagnostics)
			}
			for key, record := range ledgerRecords(t, store) {
				if record.Status != notification.StatusSkipped || record.Code != notification.OutcomePayloadTooLarge || record.Attempts != 2 {
					t.Fatalf("resent receipt not durable %s = %+v", key, record)
				}
			}
		})
	}
}

func TestNotificationUnpersistedFailuresDoNotPrematurelySettle(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		result     notification.AttemptResult
		wantStatus notification.DeliveryStatus
		wantCode   notification.OutcomeCode
	}{
		{name: "deterministic abandonment", result: notification.AttemptResult{Code: notification.OutcomeConfiguration, Status: 401}, wantStatus: notification.StatusSkipped, wantCode: notification.OutcomeAbandoned},
		{name: "transient remains retryable", result: notification.AttemptResult{Code: notification.OutcomeNetwork}, wantStatus: notification.StatusRetryable, wantCode: notification.OutcomeNetwork},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clock := notificationtest.NewClock(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
			store := &receiptFailureStore{MemoryStore: &notificationtest.MemoryStore{}}
			runtime := batchRuntime(t, clock, store, 1)
			run := 0
			runtime.OperationID = func() string {
				run++
				return fmt.Sprintf("run-%d", run)
			}
			failReceipt := true
			sends := 0
			runtime.Sender = func(v1.NotificationDestination) notification.Sender {
				return notificationSenderFunc(func(context.Context, string, notification.DeliveryPayloadV1) notification.AttemptResult {
					sends++
					if failReceipt {
						store.arm(notification.ErrUnavailable, 1)
					}
					return tc.result
				})
			}
			for cycle := 1; cycle <= notification.MaxAttempts+2; cycle++ {
				err := runtime.Dispatch(context.Background())
				if !errors.Is(err, notification.ErrReceiptNotPersisted) || !errors.Is(err, notification.ErrUnavailable) {
					t.Fatalf("cycle %d dispatch = %v", cycle, err)
				}
				clock.Advance(notification.ClaimDuration + time.Second)
			}
			for key, record := range ledgerRecords(t, store) {
				if record.Status != notification.StatusClaimed || record.Attempts != notification.MaxAttempts+2 || record.Code != "" {
					t.Fatalf("unpersisted attempts settled %s = %+v", key, record)
				}
			}

			failReceipt = false
			if err := runtime.Dispatch(context.Background()); err == nil || !strings.Contains(err.Error(), string(tc.wantCode)) {
				t.Fatalf("durable receipt = %v, want %s", err, tc.wantCode)
			}
			if sends != notification.MaxAttempts+3 {
				t.Fatalf("sends = %d, want %d", sends, notification.MaxAttempts+3)
			}
			for key, record := range ledgerRecords(t, store) {
				if record.Status != tc.wantStatus || record.Code != tc.wantCode || record.Attempts != notification.MaxAttempts+3 {
					t.Fatalf("durable receipt %s = %+v", key, record)
				}
			}
		})
	}
}

// blockingStore parks the first Commit until released, holding the runtime's
// commit slot the way a stalled notes push would.
type blockingStore struct {
	notification.LedgerStore
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingStore) Commit(ctx context.Context, expected string, transition notification.Transition) (notification.Snapshot, error) {
	blocked := false
	s.once.Do(func() { blocked = true })
	if blocked {
		close(s.entered)
		<-s.release
	}
	return s.LedgerStore.Commit(ctx, expected, transition)
}

// A commit whose context ends while another commit holds the slot must give
// up on its own deadline rather than wait for the other batch and then fail
// once admitted with an expired context.
func TestNotificationCommitSlotHonoursCallerContext(t *testing.T) {
	t.Parallel()
	clock := notificationtest.NewClock(time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC))
	memory := &notificationtest.MemoryStore{}
	runtime := batchRuntime(t, clock, memory, 1)
	store := &blockingStore{LedgerStore: memory, entered: make(chan struct{}), release: make(chan struct{})}
	runtime.Store = store
	identity := func(_ context.Context, l *notification.LedgerV1) (*notification.LedgerV1, error) {
		return l.Clone(), nil
	}
	first := make(chan error, 1)
	go func() {
		_, err := runtime.commit(context.Background(), identity)
		first <- err
	}()
	select {
	case <-store.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first commit never reached the store")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := runtime.commit(ctx, identity)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued commit = %v, want the caller's deadline", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("queued commit waited for the blocked commit instead of its own deadline")
	}
	close(store.release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	// The slot is free again: a later commit proceeds normally.
	if _, err := runtime.commit(context.Background(), identity); err != nil {
		t.Fatal(err)
	}
}

// A refused POST whose receipt write fails still reports the receiver's
// verdict and status, not only the storage failure.
func TestNotificationRefusedWithoutReceiptKeepsReceiverStatus(t *testing.T) {
	t.Parallel()
	clock := notificationtest.NewClock(time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC))
	memory := &notificationtest.MemoryStore{}
	runtime := batchRuntime(t, clock, memory, 1)
	runtime.Sender = func(v1.NotificationDestination) notification.Sender {
		return notificationSenderFunc(func(context.Context, string, notification.DeliveryPayloadV1) notification.AttemptResult {
			memory.WriteError = notification.ErrUnavailable
			return notification.AttemptResult{Code: notification.OutcomeConfiguration, Status: 400}
		})
	}
	var diagnostics []NotificationDiagnostic
	runtime.Report = func(d NotificationDiagnostic) { diagnostics = append(diagnostics, d) }
	err := runtime.Dispatch(context.Background())
	if !errors.Is(err, notification.ErrReceiptNotPersisted) || !errors.Is(err, notification.ErrUnavailable) {
		t.Fatalf("write failure lost: %v", err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Reason != "delivery-receipt-not-persisted" || diagnostics[0].Status != 400 || !strings.Contains(diagnostics[0].Action, string(notification.OutcomeConfiguration)) {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}
