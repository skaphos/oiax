package delivery

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/v2/internal/notification"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

func adapterPayload() notification.DeliveryPayloadV1 {
	repo := notification.RepositoryIdentity{Provider: "github", Host: "github.com", ID: "123", Name: "example/repo"}
	e := notification.EventV1{ID: notification.EventID(repo, "42", "request-merged"), Kind: "request-merged", Graph: "environments", Repository: repo,
		Request:    notification.RequestV1{ID: "42", Type: "promotion", Source: "development", Destination: "test", LogicalSource: "development", LogicalDestination: "test", URL: "https://github.com/example/repo/pull/42"},
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

func TestNotificationAdapterGoldens(t *testing.T) {
	t.Parallel()
	for _, kind := range []v1.NotificationTransport{"teams", "slack", "webhook"} {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			got, err := encode(kind, adapterPayload())
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", string(kind)+".golden.json"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != strings.TrimSuffix(string(want), "\n") {
				t.Fatalf("%s payload changed\ngot:  %s\nwant: %s", kind, got, want)
			}
		})
	}
}

func TestNotificationAdaptersRejectUnsafeLinksAndOversizedPayloads(t *testing.T) {
	t.Parallel()
	for _, kind := range []v1.NotificationTransport{"teams", "slack", "webhook"} {
		unsafe := adapterPayload()
		unsafe.Event.Request.URL = "https://attacker.invalid/pull/42"
		if _, err := encode(kind, unsafe); !errors.Is(err, notification.ErrInvalidState) {
			t.Fatalf("%s accepted unsafe request link: %v", kind, err)
		}
		oversized := adapterPayload()
		oversized.Message.Body = strings.Repeat("x", 24<<10)
		if _, err := encode(kind, oversized); !errors.Is(err, notification.ErrCapacity) {
			t.Fatalf("%s accepted oversized payload: %v", kind, err)
		}
	}
}

func TestNotificationAdaptersLabelBackflow(t *testing.T) {
	t.Parallel()
	payload := adapterPayload()
	payload.Event.Request.Type = v1.NotificationBackflow
	payload.Event.Request.Source = "oiax/backflow/test/abcdef0"
	payload.Event.Request.Destination = "development"
	payload.Event.ID = notification.EventID(payload.Event.Repository, payload.Event.Request.ID, payload.Event.Kind)
	for _, kind := range []v1.NotificationTransport{"teams", "slack", "webhook"} {
		data, err := encode(kind, payload)
		if err != nil || !strings.Contains(string(data), "backflow") || !strings.Contains(string(data), "oiax/backflow/test/abcdef0") {
			t.Fatalf("%s omitted backflow identity: %v %s", kind, err, data)
		}
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
