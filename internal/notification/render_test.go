package notification

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode"

	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

func renderFixture(event v1.NotificationEvent, kind v1.NotificationRequestType) EventV1 {
	repo := RepositoryIdentity{Provider: "github", Host: "github.com", ID: "123", Name: "example/repo"}
	request := RequestV1{ID: "42", Type: kind, Source: "development", Destination: "test", LogicalSource: "development", LogicalDestination: "test", URL: "https://github.com/example/repo/pull/42"}
	if kind == v1.NotificationBackflow {
		request.Source = "oiax/backflow/test/abcdef0"
		request.Destination = "development"
		request.LogicalSource = "test"
		request.LogicalDestination = "development"
	}
	result := EventV1{Kind: event, Repository: repo, Graph: "graph", Request: request, OccurredAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), ObservedAt: time.Date(2026, 9, 5, 12, 1, 0, 0, time.UTC), SourceEnvironment: "Testing", DestinationEnvironment: "Development", Snapshot: CommitSnapshot{CommitsUnavailable: true}}
	result.ID = EventID(repo, request.ID, event)
	return result
}

func TestRenderBuiltinCoversEveryLifecycleAndRequestType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		event v1.NotificationEvent
		kind  v1.NotificationRequestType
		title string
		body  string
	}{
		{v1.NotificationRequestCreated, v1.NotificationPromotion, "Branch promotion ready for review", "ready for review for the Development environment"},
		{v1.NotificationRequestMerged, v1.NotificationPromotion, "Branch promotion completed", "promoted to the Development environment"},
		{v1.NotificationRequestCreated, v1.NotificationBackflow, "Backflow ready for review", "ready for review to return to Development by backflow"},
		{v1.NotificationRequestMerged, v1.NotificationBackflow, "Backflow completed", "returned to Development by backflow"},
	}
	for _, tc := range cases {
		message, err := RenderBuiltin(renderFixture(tc.event, tc.kind))
		if err != nil || message.Title != tc.title || !strings.Contains(message.Body, tc.body) || !strings.Contains(message.Body, "Commit details unavailable") {
			t.Errorf("%s/%s = %+v, %v", tc.event, tc.kind, message, err)
		}
		if strings.Contains(strings.ToLower(message.Body), "deploy") {
			t.Errorf("%s/%s asserted deployment: %q", tc.event, tc.kind, message.Body)
		}
	}
}

func TestFixedFactsIncludesRequiredIdentityAndCompleteness(t *testing.T) {
	t.Parallel()
	event := renderFixture(v1.NotificationRequestMerged, v1.NotificationBackflow)
	facts, err := FixedFacts(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"example/repo", "github/github.com/123", "Graph: graph", "Event: request-merged", "Request type: backflow", "Source: oiax/backflow/test/abcdef0", "Destination: development", "Event ID: " + event.ID, "Request ID: 42", event.Request.URL, "Observed at: 2026-09-05T12:01:00Z", "Commit details unavailable"} {
		if !strings.Contains(facts, required) {
			t.Errorf("fixed facts omitted %q:\n%s", required, facts)
		}
	}
	if strings.Contains(facts, "Commit count: 0") || event.Request.LogicalSource != "test" || event.Request.LogicalDestination != "development" {
		t.Fatalf("unavailable history or logical edge was fabricated/erased:\n%s", facts)
	}

	for name, snapshot := range map[string]CommitSnapshot{
		"truncated":     {CommitsTruncated: true},
		"unknown total": {},
		"known total":   {CommitCountKnown: true, CommitCount: 3},
	} {
		event := renderFixture(v1.NotificationRequestMerged, v1.NotificationPromotion)
		event.Snapshot = snapshot
		facts, err := FixedFacts(event)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"truncated": "Commit details truncated", "unknown total": "Commit total unknown", "known total": "Commit count: 3"}[name]
		if !strings.Contains(facts, want) {
			t.Errorf("%s completeness = %q", name, facts)
		}
	}
}

func TestFixedFactsPreservesCompleteAzureRepositoryIdentity(t *testing.T) {
	t.Parallel()
	event := renderFixture(v1.NotificationRequestMerged, v1.NotificationPromotion)
	event.Repository = RepositoryIdentity{Provider: "azuredevops", Host: "dev.azure.com", OrganizationID: "organization-id", ProjectID: "project-id", ID: "repository-id", Name: "org/project/repo"}
	event.Request.URL = "https://dev.azure.com/org/project/_git/repo/pullrequest/42"
	event.ID = EventID(event.Repository, event.Request.ID, event.Kind)
	facts, err := FixedFacts(event)
	if err != nil || !strings.Contains(facts, "azuredevops/dev.azure.com/organization-id/project-id/repository-id") {
		t.Fatalf("Azure repository identity incomplete: %v\n%s", err, facts)
	}
}

func TestRenderingRejectsUnsafeIdentityAndCleansDisplayText(t *testing.T) {
	t.Parallel()
	for _, requestURL := range []string{
		"http://github.com/example/repo/pull/42",
		"https://github.com/attacker/repo/pull/42",
		"https://github.com/example/repo/pull/42?token=secret",
		"https://github.com/example/repo/pull/42?",
		"https://github.com/example/repo/pull/42#fragment",
	} {
		event := renderFixture(v1.NotificationRequestMerged, v1.NotificationPromotion)
		event.Request.URL = requestURL
		if _, err := FixedFacts(event); !errors.Is(err, ErrInvalidState) {
			t.Errorf("unsafe link %q accepted: %v", requestURL, err)
		}
	}
	event := renderFixture(v1.NotificationRequestMerged, v1.NotificationPromotion)
	event.Request.Source = "development\nInjected"
	if _, err := FixedFacts(event); !errors.Is(err, ErrInvalidState) {
		t.Fatal("control character in fixed identity accepted")
	}
	event = renderFixture(v1.NotificationRequestMerged, v1.NotificationPromotion)
	event.Graph = strings.Repeat("x", 13<<10)
	if _, err := FixedFacts(event); !errors.Is(err, ErrCapacity) {
		t.Fatalf("unfit required identity = %v", err)
	}
	event = renderFixture(v1.NotificationRequestMerged, v1.NotificationPromotion)
	event.DestinationEnvironment = "Production\u202e\nEnvironment"
	message, err := RenderBuiltin(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range message.Title + message.Body {
		if unicode.Is(unicode.Cf, r) || (unicode.IsControl(r) && r != '\n') {
			t.Fatalf("unsafe display control survived rendering: %q", message.Body)
		}
	}
}
