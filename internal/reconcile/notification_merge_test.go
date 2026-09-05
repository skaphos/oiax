package reconcile

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/internal/notification"
	"github.com/skaphos/oiax/internal/notification/notificationtest"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

func TestNotificationMergeDeliveryAndRepeat(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC)
	repo := notification.RepositoryIdentity{Provider: "github", Host: "github.com", ID: "1", Name: "example/repo"}
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: "webhook", EndpointEnv: "AUDIT"}}}
	store := &notificationtest.MemoryStore{}
	sender := &notificationtest.Recorder{Result: notification.AttemptResult{Code: notification.OutcomeAccepted}}
	runtime := &NotificationRuntime{Store: store, Repository: repo, Graph: "graph", ConfigOID: strings.Repeat("a", 40), Policy: policy, Now: func() time.Time { return now }, OperationID: func() string { return "run" }, LookupEnv: func(string) (string, bool) { return "https://receiver.invalid/secret", true }, Sender: func(v1.NotificationDestination) notification.Sender { return sender }}
	if err := runtime.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	e := notification.EventV1{Repository: repo, Graph: "graph", Kind: "request-merged", Request: notification.RequestV1{ID: "42", Type: "promotion", Source: "dev", Destination: "test", URL: "https://github.com/example/repo/pull/42"}, OccurredAt: now, ObservedAt: now, Snapshot: notification.CommitSnapshot{CommitsUnavailable: true}}
	e.ID = notification.EventID(repo, e.Request.ID, e.Kind)
	if err := runtime.Admit(context.Background(), []notification.EventV1{e}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range 1000 {
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
}
