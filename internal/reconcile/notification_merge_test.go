package reconcile

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/skaphos/oiax/internal/notification"
	"github.com/skaphos/oiax/internal/notification/notificationtest"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

type notificationSenderFunc func(context.Context, string, notification.DeliveryPayloadV1) notification.AttemptResult

func (f notificationSenderFunc) Send(ctx context.Context, endpoint string, payload notification.DeliveryPayloadV1) notification.AttemptResult {
	return f(ctx, endpoint, payload)
}

func mergeRuntime(now func() time.Time, store notification.LedgerStore, policy *v1.NotificationPolicy) *NotificationRuntime {
	repo := notification.RepositoryIdentity{Provider: "github", Host: "github.com", ID: "1", Name: "example/repo"}
	return &NotificationRuntime{
		Store:       store,
		Repository:  repo,
		Graph:       "graph",
		ConfigOID:   strings.Repeat("a", 40),
		Policy:      policy,
		Now:         now,
		OperationID: func() string { return "run" },
		LookupEnv:   func(string) (string, bool) { return "https://receiver.invalid/secret", true },
	}
}

func mergeEvent(repo notification.RepositoryIdentity, id string, now time.Time) notification.EventV1 {
	event := notification.EventV1{Repository: repo, Graph: "graph", Kind: v1.NotificationRequestMerged, Request: notification.RequestV1{ID: id, Type: v1.NotificationPromotion, Source: "dev", Destination: "test", URL: "https://github.com/example/repo/pull/" + id}, OccurredAt: now, ObservedAt: now, Snapshot: notification.CommitSnapshot{CommitsUnavailable: true}}
	event.ID = notification.EventID(repo, event.Request.ID, event.Kind)
	return event
}

func TestNotificationMergeDeliveryAndRepeat(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"github", "azuredevops"} {
		for _, kind := range []v1.NotificationRequestType{v1.NotificationPromotion, v1.NotificationBackflow} {
			t.Run(provider+"/"+string(kind), func(t *testing.T) {
				t.Parallel()
				now := time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC)
				policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: "webhook", EndpointEnv: "AUDIT"}}}
				store := &notificationtest.MemoryStore{}
				sender := &notificationtest.Recorder{Result: notification.AttemptResult{Code: notification.OutcomeAccepted}}
				newRun := func() *NotificationRuntime {
					runtime := mergeRuntime(func() time.Time { return now }, store, policy)
					if provider == "azuredevops" {
						runtime.Repository = notification.RepositoryIdentity{Provider: provider, Host: "dev.azure.com", ID: "repo-id", OrganizationID: "org-id", ProjectID: "project-id", Name: "org/project/repo"}
					}
					runtime.Sender = func(v1.NotificationDestination) notification.Sender { return sender }
					return runtime
				}
				runtime := newRun()
				if err := runtime.Activate(context.Background()); err != nil {
					t.Fatal(err)
				}
				e := mergeEvent(runtime.Repository, "42", now)
				e.Request.Type = kind
				if provider == "azuredevops" {
					e.Request.URL = "https://dev.azure.com/org/project/_git/repo/pullrequest/42"
				}
				if kind == v1.NotificationBackflow {
					e.Request.Source, e.Request.Destination = "oiax/backflow/main-to-dev/abcdef0", "dev"
				}
				// The event predating activation is recorded without backfilling.
				historical := e
				historical.Request.ID = "41"
				historical.Request.URL = strings.TrimSuffix(e.Request.URL, "/42") + "/41"
				historical.ID = notification.EventID(runtime.Repository, "41", historical.Kind)
				historical.OccurredAt = now.Add(-time.Hour)
				if err := runtime.Admit(context.Background(), []notification.EventV1{historical}); err != nil {
					t.Fatal(err)
				}
				if err := runtime.Dispatch(context.Background()); err != nil || len(sender.Payloads()) != 0 {
					t.Fatal("activation backfilled a historical merge", err)
				}
				for range 1001 { // one initial delivery plus 1,000 repeat evaluations
					runtime = newRun() // only the durable store survives each run
					if err := runtime.Activate(context.Background()); err != nil {
						t.Fatal(err)
					}
					if err := runtime.Admit(context.Background(), []notification.EventV1{e}); err != nil {
						t.Fatal(err)
					}
					if err := runtime.Dispatch(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				if len(sender.Payloads()) != 1 {
					t.Fatal("durable success resent", len(sender.Payloads()))
				}
			})
		}
	}
}

func TestNotificationDispatchCompetingRunsSendOnce(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: "webhook", EndpointEnv: "AUDIT"}}}
	store := &notificationtest.MemoryStore{}
	sender := &notificationtest.Recorder{Result: notification.AttemptResult{Code: notification.OutcomeAccepted}}
	first := mergeRuntime(func() time.Time { return now }, store, policy)
	first.OperationID = func() string { return "first" }
	first.Sender = func(v1.NotificationDestination) notification.Sender { return sender }
	second := mergeRuntime(func() time.Time { return now }, store, policy)
	second.OperationID = func() string { return "second" }
	second.Sender = first.Sender
	if err := first.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := first.Admit(context.Background(), []notification.EventV1{mergeEvent(first.Repository, "42", now)}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, runtime := range []*NotificationRuntime{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- runtime.Dispatch(context.Background())
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := len(sender.Payloads()); got != 1 {
		t.Fatalf("competing runs sent %d payloads", got)
	}
}

func TestNotificationDispatchSpacingAndIndependentFailure(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC)
	clock := notificationtest.NewClock(now)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{
		{Name: "broken", Type: "webhook", EndpointEnv: "BROKEN"},
		{Name: "ops", Type: "webhook", EndpointEnv: "AUDIT"},
	}}
	store := &notificationtest.MemoryStore{}
	runtime := mergeRuntime(clock.Now, store, policy)
	runtime.Wait = func(ctx context.Context, delay time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		clock.Advance(delay)
		return nil
	}
	broken := &notificationtest.Recorder{Result: notification.AttemptResult{Code: notification.OutcomeNetwork}}
	ops := &notificationtest.Recorder{Result: notification.AttemptResult{Code: notification.OutcomeAccepted}}
	runtime.Sender = func(destination v1.NotificationDestination) notification.Sender {
		if destination.Name == "broken" {
			return broken
		}
		return ops
	}
	if err := runtime.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := []notification.EventV1{mergeEvent(runtime.Repository, "41", now), mergeEvent(runtime.Repository, "42", now)}
	if err := runtime.Admit(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	dispatchErr := runtime.Dispatch(context.Background())
	if dispatchErr == nil || !strings.Contains(dispatchErr.Error(), string(notification.OutcomeNetwork)) {
		t.Fatalf("missing bounded failure diagnostic: %v", dispatchErr)
	}
	if len(broken.Payloads()) != 2 || len(ops.Payloads()) != 2 {
		t.Fatalf("failure starved destination: broken=%d ops=%d: %v", len(broken.Payloads()), len(ops.Payloads()), dispatchErr)
	}
	if clock.Now().Before(now.Add(time.Second)) {
		t.Fatal("per-destination spacing was not observed")
	}
}

func TestNotificationDispatchCancellationAndDisabledBypass(t *testing.T) {
	t.Parallel()
	disabled := &NotificationRuntime{Policy: &v1.NotificationPolicy{}}
	if err := disabled.Dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: "webhook", EndpointEnv: "AUDIT"}}}
	store := &notificationtest.MemoryStore{}
	runtime := mergeRuntime(func() time.Time { return now }, store, policy)
	sender := &notificationtest.Recorder{Result: notification.AttemptResult{Code: notification.OutcomeAccepted}}
	runtime.Sender = func(v1.NotificationDestination) notification.Sender { return sender }
	if err := runtime.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Admit(context.Background(), []notification.EventV1{mergeEvent(runtime.Repository, "42", now)}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runtime.Dispatch(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled dispatch = %v", err)
	}
	if len(sender.Payloads()) != 0 {
		t.Fatal("canceled dispatch contacted sender")
	}
}

func TestNotificationAcceptedWithoutReceiptIsUncertain(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: "webhook", EndpointEnv: "AUDIT"}}}
	store := &notificationtest.MemoryStore{}
	runtime := mergeRuntime(func() time.Time { return now }, store, policy)
	sends := 0
	runtime.Sender = func(v1.NotificationDestination) notification.Sender {
		return notificationSenderFunc(func(context.Context, string, notification.DeliveryPayloadV1) notification.AttemptResult {
			sends++
			store.WriteError = notification.ErrUnavailable
			return notification.AttemptResult{Code: notification.OutcomeAccepted}
		})
	}
	if err := runtime.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Admit(context.Background(), []notification.EventV1{mergeEvent(runtime.Repository, "42", now)}); err != nil {
		t.Fatal(err)
	}
	err := runtime.Dispatch(context.Background())
	if !errors.Is(err, notification.ErrReceiptUncertain) || sends != 1 {
		t.Fatalf("accepted receipt failure = %v, sends=%d", err, sends)
	}
	store.WriteError = nil
	snapshot, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range snapshot.Ledger.Deliveries {
		if record.Status == notification.StatusDelivered {
			t.Fatal("failed receipt was reported as durable success")
		}
	}
}

func TestNotificationSlowDestinationDoesNotStarveHealthyDestination(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{
		{Name: "a-slow", Type: "webhook", EndpointEnv: "SLOW"},
		{Name: "z-healthy", Type: "webhook", EndpointEnv: "HEALTHY"},
	}}
	store := &notificationtest.MemoryStore{}
	runtime := mergeRuntime(func() time.Time { return now }, store, policy)
	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	healthySent := make(chan struct{})
	runtime.Sender = func(destination v1.NotificationDestination) notification.Sender {
		return notificationSenderFunc(func(ctx context.Context, _ string, _ notification.DeliveryPayloadV1) notification.AttemptResult {
			if destination.Name == "a-slow" {
				close(slowStarted)
				select {
				case <-releaseSlow:
					return notification.AttemptResult{Code: notification.OutcomeNetwork}
				case <-ctx.Done():
					return notification.AttemptResult{Code: notification.OutcomeCanceled}
				}
			}
			close(healthySent)
			return notification.AttemptResult{Code: notification.OutcomeAccepted}
		})
	}
	if err := runtime.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Admit(context.Background(), []notification.EventV1{mergeEvent(runtime.Repository, "42", now)}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- runtime.Dispatch(context.Background()) }()
	select {
	case <-slowStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("slow destination did not begin")
	}
	select {
	case <-healthySent:
	case <-time.After(2 * time.Second):
		t.Fatal("slow destination starved healthy destination")
	}
	close(releaseSlow)
	if err := <-done; err == nil || !strings.Contains(err.Error(), string(notification.OutcomeNetwork)) {
		t.Fatalf("slow failure diagnostic = %v", err)
	}
}
