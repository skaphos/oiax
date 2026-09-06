package reconcile

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/v2/internal/engine"
	"github.com/skaphos/oiax/v2/internal/forge"
	"github.com/skaphos/oiax/v2/internal/notification"
	"github.com/skaphos/oiax/v2/internal/notification/notificationtest"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

type partialCreationForge struct{ *fakeForge }

func (f *partialCreationForge) CreateRequest(ctx context.Context, req forge.CreateRequest) (forge.CreateOutcome, error) {
	out, err := f.fakeForge.CreateRequest(ctx, req)
	return out, errors.Join(err, errors.New("metadata follow-up failed"))
}

func TestNotificationBackflowRetainsPartialCreation(t *testing.T) {
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	commit("hotfix.txt", "urgent\n", "hotfix")
	configOID, err := r.Head(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	f := &partialCreationForge{fakeForge: &fakeForge{createResult: engine.ChangeRequest{ID: "42", Type: engine.RequestTypeBackflow}}}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph(), ConfigOID: configOID, NotificationPolicy: &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: "webhook", EndpointEnv: "ENDPOINT", Events: []v1.NotificationEvent{v1.NotificationRequestCreated}}}}}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.Apply(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "metadata follow-up failed") || len(result.NotificationOutcomes) != 1 || len(f.created) != 1 {
		t.Fatalf("partial backflow lost: %+v, %v", result, err)
	}
	o := result.NotificationOutcomes[0].Origin
	if o == nil || o.LogicalSource != "main" || o.LogicalTarget != "development" || o.SourceOID != f.pushed[0].SHA || o.ConfigOID != configOID || f.created[0].Source == o.LogicalSource {
		t.Fatalf("incorrect backflow origin: %+v", o)
	}
}

func TestNotificationCreationSecondPrecisionCutoff(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, 9, 5, 12, 0, 0, 500000000, time.UTC)
	for _, afterCutoff := range []bool{true, false} {
		policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: "webhook", EndpointEnv: "ENDPOINT", Events: []v1.NotificationEvent{v1.NotificationRequestCreated}}}}
		store := &notificationtest.MemoryStore{}
		runtime := mergeRuntime(func() time.Time { return cutoff }, store, policy)
		runtime.Topology = observationGraph()
		if err := runtime.Activate(context.Background()); err != nil {
			t.Fatal(err)
		}
		req := lifecycleMerge(runtime.Repository, "42", cutoff.Truncate(time.Second), time.Time{})
		req.State = notification.LifecycleOpen
		operationTime := cutoff.Add(100 * time.Millisecond)
		if !afterCutoff {
			operationTime = cutoff.Add(-100 * time.Millisecond)
		}
		req.Origin = &notification.NotificationOriginV1{Version: 1, OperationID: "original", Graph: "graph", ConfigOID: runtime.ConfigOID, ObservedAt: operationTime, LogicalSource: "dev", LogicalTarget: "test", SourceOID: strings.Repeat("b", 40), BaseOID: strings.Repeat("c", 40)}
		if err := runtime.recordObservation(context.Background(), []notification.LifecycleRequest{req}, "", notification.ScanProgress{}, cutoff.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		snapshot, err := store.Read(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if afterCutoff {
			want = 1
		}
		if len(snapshot.Ledger.Deliveries) != want {
			t.Fatalf("operation after cutoff=%v: got %d deliveries, want %d", afterCutoff, len(snapshot.Ledger.Deliveries), want)
		}
		for _, event := range snapshot.Ledger.Events {
			if !event.OccurredAt.Equal(req.CreatedAt) {
				t.Fatal("forge occurrence timestamp was fabricated")
			}
		}
	}
}

func TestNotificationCreationRecoveryAndOptIn(t *testing.T) {
	t.Parallel()
	for _, kind := range []v1.NotificationRequestType{v1.NotificationPromotion, v1.NotificationBackflow} {
		for _, optIn := range []bool{false, true} {
			name := string(kind) + "/default"
			if optIn {
				name = string(kind) + "/opt-in"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
				clock := notificationtest.NewClock(now)
				policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: "webhook", EndpointEnv: "ENDPOINT"}}}
				if optIn {
					policy.Destinations[0].Events = []v1.NotificationEvent{v1.NotificationRequestCreated, v1.NotificationRequestMerged}
				}
				store := &notificationtest.MemoryStore{}
				runtime := mergeRuntime(clock.Now, store, policy)
				runtime.Topology = observationGraph()
				runtime.Wait = func(_ context.Context, d time.Duration) error { clock.Advance(d); return nil }
				sender := &notificationtest.Recorder{Result: notification.AttemptResult{Code: notification.OutcomeAccepted}}
				runtime.Sender = func(v1.NotificationDestination) notification.Sender { return sender }
				if err := runtime.Activate(context.Background()); err != nil {
					t.Fatal(err)
				}
				clock.Advance(time.Minute)
				req := lifecycleMerge(runtime.Repository, "42", clock.Now(), clock.Now())
				req.Request.Type = kind
				if kind == v1.NotificationBackflow {
					req.Request.Source, req.Request.Destination = "oiax/backflow/test-to-dev/abcdef0", "dev"
					req.Request.LogicalSource, req.Request.LogicalDestination = "test", "dev"
				}
				req.Origin = &notification.NotificationOriginV1{Version: 1, OperationID: "actual-POST", Graph: "graph", ConfigOID: runtime.ConfigOID, ObservedAt: now, LogicalSource: req.Request.LogicalSource, LogicalTarget: req.Request.LogicalDestination, SourceOID: strings.Repeat("b", 40), BaseOID: strings.Repeat("c", 40)}
				// Discovery can recover creation and merge together after a crash.
				for range 3 {
					if err := runtime.recordObservation(context.Background(), []notification.LifecycleRequest{req}, "", notification.ScanProgress{}, clock.Now()); err != nil {
						t.Fatal(err)
					}
					if err := runtime.Dispatch(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				want := 1
				if optIn {
					want = 2
				}
				if len(sender.Payloads()) != want {
					t.Fatalf("got %d messages, want %d", len(sender.Payloads()), want)
				}
				// An adopted/legacy request lacking origin cannot manufacture creation.
				req.Request.ID = "43"
				req.Origin = nil
				req.State = notification.LifecycleOpen
				if err := runtime.recordObservation(context.Background(), []notification.LifecycleRequest{req}, "", notification.ScanProgress{}, clock.Now()); err != nil {
					t.Fatal(err)
				}
				if err := runtime.Dispatch(context.Background()); err != nil || len(sender.Payloads()) != want {
					t.Fatal("legacy adoption manufactured creation", err)
				}
			})
		}
	}
}
