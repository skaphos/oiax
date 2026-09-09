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
	// One read finds the due work, and each send after the first re-observes
	// the ledger before contacting the receiver; only two writes are made.
	if commits != 2 || reads != 5 {
		t.Fatalf("five deliveries cost %d writes and %d reads; want one claim and one receipt write with four pre-send observations", commits, reads)
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
