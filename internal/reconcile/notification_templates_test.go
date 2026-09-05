package reconcile

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/internal/notification"
	"github.com/skaphos/oiax/internal/notification/notificationtest"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

func TestNotificationTemplatesPersistPerDestinationAcrossRetry(t *testing.T) {
	ctx := context.Background()
	clock := notificationtest.NewClock(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
	first, second := "First destination", "Second destination"
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{
		{Name: "one", Type: v1.NotificationWebhook, EndpointEnv: "ONE", Templates: &v1.NotificationTemplates{Body: &first}},
		{Name: "two", Type: v1.NotificationSlack, EndpointEnv: "TWO", Templates: &v1.NotificationTemplates{Body: &second}},
	}}
	store := &notificationtest.MemoryStore{}
	runtime := mergeRuntime(clock.Now, store, policy)
	runtime.VerifyRevision = func(context.Context, string, string) (notification.RevisionRelation, error) {
		return notification.RevisionDescendant, nil
	}
	one := &notificationtest.Recorder{Result: notification.AttemptResult{Code: notification.OutcomeNetwork}}
	two := &notificationtest.Recorder{Result: notification.AttemptResult{Code: notification.OutcomeAccepted}}
	runtime.Sender = func(d v1.NotificationDestination) notification.Sender {
		if d.Name == "one" {
			return one
		}
		return two
	}
	if err := runtime.Activate(ctx); err != nil {
		t.Fatal(err)
	}
	event := mergeEvent(runtime.Repository, "42", clock.Now())
	if err := runtime.Admit(ctx, []notification.EventV1{event}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Dispatch(ctx); err == nil {
		t.Fatal("expected retryable failure")
	}
	if len(one.Payloads()) != 1 || len(two.Payloads()) != 1 {
		t.Fatal("missing independent attempts")
	}
	a, b := one.Payloads()[0], two.Payloads()[0]
	if a.Message.Body != first || b.Message.Body != second || !reflect.DeepEqual(a.Event, b.Event) {
		t.Fatal("wording changed event truth")
	}
	changed := "new wording must not replace an attempted payload"
	policy.Destinations[0].Templates.Body = &changed
	runtime.ConfigOID = strings.Repeat("b", 40)
	runtime.OperationID = func() string { return "retry-run" }
	clock.Advance(24 * time.Hour)
	if err := runtime.Activate(ctx); err != nil {
		t.Fatal(err)
	}
	one.Result.Code = notification.OutcomeAccepted
	if err := runtime.Dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	if len(one.Payloads()) != 2 || len(two.Payloads()) != 1 || !reflect.DeepEqual(a, one.Payloads()[1]) {
		t.Fatal("retry rewrote saved payload or replayed success")
	}
}
