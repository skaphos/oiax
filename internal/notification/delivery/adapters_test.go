package delivery

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/internal/notification"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

func adapterPayload() notification.DeliveryPayloadV1 {
	repo := notification.RepositoryIdentity{Provider: "github", Host: "github.com", ID: "123", Name: "example/repo"}
	e := notification.EventV1{ID: notification.EventID(repo, "42", "request-merged"), Kind: "request-merged", Graph: "environments", Repository: repo,
		Request:    notification.RequestV1{ID: "42", Type: "promotion", Source: "development", Destination: "test", URL: "https://github.com/example/repo/pull/42"},
		OccurredAt: time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC), ObservedAt: time.Date(2026, 9, 4, 18, 1, 0, 0, time.UTC), Snapshot: notification.CommitSnapshot{CommitsUnavailable: true}}
	return notification.DeliveryPayloadV1{SchemaVersion: 1, Event: e, Message: notification.RenderedMessageV1{Title: "Custom constant", Body: "These commits were promoted to the test environment. <!channel> @everyone"}}
}

func TestNotificationAdapterPersistedMessagesAndFacts(t *testing.T) {
	t.Parallel()
	for _, kind := range []v1.NotificationTransport{"teams", "slack", "webhook"} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			first := adapterPayload()
			second := first
			second.Message = notification.RenderedMessageV1{Title: "Another saved title", Body: "Another recipient's text"}
			for _, payload := range []notification.DeliveryPayloadV1{first, second} {
				before := payload
				data, err := encode(kind, payload)
				if err != nil {
					t.Fatal(err)
				}
				if len(data) > 24<<10 || !json.Valid(data) || !reflect.DeepEqual(before, payload) {
					t.Fatal("payload mutated or invalid")
				}
				var decoded any
				if err := json.Unmarshal(data, &decoded); err != nil {
					t.Fatal(err)
				}
				text := allStrings(decoded)
				for _, fact := range []string{payload.Message.Title, payload.Message.Body, first.Event.Repository.Name, first.Event.Graph, string(first.Event.Kind), string(first.Event.Request.Type), first.Event.Request.Source, first.Event.Request.Destination, first.Event.ID, "42", first.Event.Request.URL, "2026-09-04T18:01:00Z"} {
					if !strings.Contains(text, fact) {
						t.Errorf("%s omitted fixed/saved fact %q", kind, fact)
					}
				}
				if strings.Contains(string(data), `"mrkdwn"`) || strings.Contains(string(data), `"msteams"`) {
					t.Fatal("active formatting or mentions enabled")
				}
			}
			payload := first
			payload.Message = notification.RenderedMessageV1{}
			data, err := encode(kind, payload)
			if err != nil || !strings.Contains(string(data), "2026-09-04T18:01:00Z") {
				t.Fatal("empty template erased identity", err)
			}
		})
	}
}

func allStrings(value any) string {
	switch v := value.(type) {
	case string:
		return v + "\n"
	case map[string]any:
		var s string
		for _, item := range v {
			s += allStrings(item)
		}
		return s
	case []any:
		var s string
		for _, item := range v {
			s += allStrings(item)
		}
		return s
	default:
		return ""
	}
}
