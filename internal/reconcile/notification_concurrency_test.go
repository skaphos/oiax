package reconcile

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/skaphos/oiax/v2/internal/notification"
	"github.com/skaphos/oiax/v2/internal/notification/notificationtest"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

type replaceOnConflictStore struct {
	mu          sync.Mutex
	current     notification.Snapshot
	replacement notification.Snapshot
	replaced    bool
}

func (s *replaceOnConflictStore) Read(ctx context.Context) (notification.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return notification.Snapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return notification.Snapshot{Ledger: s.current.Ledger.Clone(), Revision: s.current.Revision}, nil
}

func (s *replaceOnConflictStore) Commit(ctx context.Context, expected string, transition notification.Transition) (notification.Snapshot, error) {
	s.mu.Lock()
	current := notification.Snapshot{Ledger: s.current.Ledger.Clone(), Revision: s.current.Revision}
	first := !s.replaced
	s.mu.Unlock()
	if current.Revision != expected {
		return notification.Snapshot{}, notification.ErrConflict
	}
	next, err := transition(ctx, current.Ledger)
	if err != nil {
		return notification.Snapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if first && !s.replaced {
		s.current = notification.Snapshot{Ledger: s.replacement.Ledger.Clone(), Revision: s.replacement.Revision}
		s.replaced = true
		return notification.Snapshot{}, notification.ErrConflict
	}
	if s.current.Revision != expected {
		return notification.Snapshot{}, notification.ErrConflict
	}
	s.current = notification.Snapshot{Ledger: next.Clone(), Revision: notification.Digest(expected, next.PolicyRevision.ConfigOID)}
	return notification.Snapshot{Ledger: s.current.Ledger.Clone(), Revision: s.current.Revision}, nil
}

func seededNotificationLedger(t *testing.T, repo notification.RepositoryIdentity, graph, oid string, policy *v1.NotificationPolicy, now time.Time) *notification.LedgerV1 {
	t.Helper()
	ledger := notification.NewLedger(repo, graph, oid)
	var err error
	ledger, err = notification.AcceptPolicy(ledger, notification.PolicyRevisionV1{ConfigOID: oid, PolicyDigest: policyDigest(policy)}, policy, now, notification.RevisionEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}

func TestNotificationConcurrentInitializationConverges(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "ENDPOINT"}}}
	store := &notificationtest.MemoryStore{}
	first := mergeRuntime(func() time.Time { return now }, store, policy)
	second := mergeRuntime(func() time.Time { return now }, store, policy)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var workers sync.WaitGroup
	for _, runtime := range []*NotificationRuntime{first, second} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			errs <- runtime.Activate(context.Background())
		}()
	}
	close(start)
	workers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Ledger.Destinations) != 1 || !snapshot.Ledger.Destinations["ops"].Active || snapshot.Ledger.PolicyRevision.ConfigOID != first.ConfigOID {
		t.Fatalf("initialization race did not converge: %+v", snapshot.Ledger)
	}
}

func TestNotificationConflictRereducesAgainstNewerConfig(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "ENDPOINT"}}}
	repo := notification.RepositoryIdentity{Provider: "github", Host: "github.com", ID: "1", Name: "example/repo"}
	oldOID, newOID := strings.Repeat("a", 40), strings.Repeat("b", 40)
	oldLedger := seededNotificationLedger(t, repo, "graph", oldOID, policy, now)
	newLedger, err := notification.AcceptPolicy(oldLedger, notification.PolicyRevisionV1{ConfigOID: newOID, PolicyDigest: policyDigest(policy)}, policy, now.Add(time.Minute), notification.RevisionEvidence{AcceptedOID: oldOID, IncomingOID: newOID, Relation: notification.RevisionDescendant})
	if err != nil {
		t.Fatal(err)
	}
	store := &replaceOnConflictStore{
		current:     notification.Snapshot{Ledger: oldLedger, Revision: "old-revision"},
		replacement: notification.Snapshot{Ledger: newLedger, Revision: "new-revision"},
	}
	runtime := mergeRuntime(func() time.Time { return now.Add(2 * time.Minute) }, store, policy)
	runtime.ConfigOID = oldOID
	comparisons := 0
	runtime.VerifyRevision = func(_ context.Context, accepted, incoming string) (notification.RevisionRelation, error) {
		comparisons++
		if accepted != newOID || incoming != oldOID {
			t.Fatalf("stale comparison = %s -> %s", accepted, incoming)
		}
		return notification.RevisionAncestor, nil
	}
	if err := runtime.Activate(context.Background()); !errors.Is(err, notification.ErrStaleRevision) {
		t.Fatalf("conflict re-reduction = %v", err)
	}
	if comparisons != 1 {
		t.Fatalf("revision evidence was not recomputed after conflict: %d", comparisons)
	}
	snapshot, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Ledger.PolicyRevision.ConfigOID != newOID {
		t.Fatal("stale retry replaced newer configuration")
	}
}

func TestNotificationActivationRejectsMismatchedAndUnorderedPolicies(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	base := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "FIRST"}}}
	for _, tc := range []struct {
		name   string
		oid    string
		policy *v1.NotificationPolicy
		verify func(context.Context, string, string) (notification.RevisionRelation, error)
		want   error
	}{
		{name: "equal OID digest mismatch", oid: strings.Repeat("a", 40), policy: &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "CHANGED"}}}, want: notification.ErrPolicyMismatch},
		{name: "divergent", oid: strings.Repeat("b", 40), policy: base, verify: func(context.Context, string, string) (notification.RevisionRelation, error) {
			return notification.RevisionDivergent, nil
		}, want: notification.ErrUnorderedRevision},
		{name: "unknown", oid: strings.Repeat("b", 40), policy: base, verify: func(context.Context, string, string) (notification.RevisionRelation, error) {
			return notification.RevisionUnknown, errors.New("ancestry unavailable")
		}, want: notification.ErrUnorderedRevision},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &notificationtest.MemoryStore{}
			first := mergeRuntime(func() time.Time { return now }, store, base)
			if err := first.Activate(context.Background()); err != nil {
				t.Fatal(err)
			}
			before, err := store.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			incoming := mergeRuntime(func() time.Time { return now.Add(time.Minute) }, store, tc.policy)
			incoming.ConfigOID = tc.oid
			incoming.VerifyRevision = tc.verify
			if err := incoming.Activate(context.Background()); !errors.Is(err, tc.want) {
				t.Fatalf("activation = %v, want %v", err, tc.want)
			}
			after, err := store.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if after.Revision != before.Revision || after.Ledger.PolicyRevision != before.Ledger.PolicyRevision {
				t.Fatal("rejected policy mutated durable state")
			}
		})
	}
}

func TestNotificationDescendantContentRevertDoesNotReviveOldDelivery(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	store := &notificationtest.MemoryStore{}
	policy := func(endpoint string) *v1.NotificationPolicy {
		return &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: endpoint}}}
	}
	activate := func(oid, endpoint string, at time.Time) *NotificationRuntime {
		runtime := mergeRuntime(func() time.Time { return at }, store, policy(endpoint))
		runtime.ConfigOID = strings.Repeat(oid, 40)
		runtime.VerifyRevision = func(context.Context, string, string) (notification.RevisionRelation, error) {
			return notification.RevisionDescendant, nil
		}
		if err := runtime.Activate(context.Background()); err != nil {
			t.Fatal(err)
		}
		return runtime
	}
	first := activate("a", "FIRST", now)
	event := mergeEvent(first.Repository, "42", now.Add(time.Minute))
	if err := first.Admit(context.Background(), []notification.EventV1{event}); err != nil {
		t.Fatal(err)
	}
	initial, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	initialGeneration := initial.Ledger.Destinations["ops"].Generation
	activate("b", "SECOND", now.Add(2*time.Minute))
	activate("c", "FIRST", now.Add(3*time.Minute))
	final, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if final.Ledger.PolicyRevision.ConfigOID != strings.Repeat("c", 40) || final.Ledger.Destinations["ops"].Generation == initialGeneration || final.Ledger.Events[event.ID].ID != event.ID {
		t.Fatalf("descendant content revert lost revision/event identity: %+v", final.Ledger.PolicyRevision)
	}
	for _, delivery := range final.Ledger.Deliveries {
		if delivery.Status != notification.StatusSkipped || delivery.Code != notification.OutcomeRetired {
			t.Fatalf("old generation revived after content revert: %+v", delivery)
		}
	}
}

func TestNotificationExpiredSuspendedSenderLateSuccessWins(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	clock := notificationtest.NewClock(now)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "ENDPOINT"}}}
	store := &notificationtest.MemoryStore{}
	first := mergeRuntime(clock.Now, store, policy)
	first.OperationID = func() string { return "suspended" }
	second := mergeRuntime(clock.Now, store, policy)
	second.OperationID = func() string { return "replacement" }
	started := make(chan struct{})
	release := make(chan struct{})
	first.Sender = func(v1.NotificationDestination) notification.Sender {
		return notificationSenderFunc(func(ctx context.Context, _ string, _ notification.DeliveryPayloadV1) notification.AttemptResult {
			close(started)
			select {
			case <-release:
				return notification.AttemptResult{Code: notification.OutcomeAccepted}
			case <-ctx.Done():
				return notification.AttemptResult{Code: notification.OutcomeCanceled}
			}
		})
	}
	secondSender := &notificationtest.Recorder{Result: notification.AttemptResult{Code: notification.OutcomeNetwork}}
	second.Sender = func(v1.NotificationDestination) notification.Sender { return secondSender }
	if err := first.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	event := mergeEvent(first.Repository, "42", now)
	if err := first.Admit(context.Background(), []notification.EventV1{event}); err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() { firstDone <- first.Dispatch(context.Background()) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first sender did not suspend")
	}
	clock.Advance(notification.ClaimDuration + time.Minute)
	if err := second.Dispatch(context.Background()); err == nil || !strings.Contains(err.Error(), string(notification.OutcomeNetwork)) {
		t.Fatalf("replacement failure = %v", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("late accepted result = %v", err)
	}
	if len(secondSender.Payloads()) != 1 {
		t.Fatalf("expired claim was not recoverable: sends=%d", len(secondSender.Payloads()))
	}
	snapshot, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Ledger.Events) != 1 || snapshot.Ledger.Events[event.ID].ID != event.ID {
		t.Fatal("concurrent attempts changed event identity")
	}
	for _, delivery := range snapshot.Ledger.Deliveries {
		if delivery.Status != notification.StatusDelivered || delivery.Code != notification.OutcomeAccepted || delivery.Attempts != 2 {
			t.Fatalf("late success did not preserve terminal receipt: %+v", delivery)
		}
	}
}
