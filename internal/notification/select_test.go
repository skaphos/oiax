package notification

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/v2/internal/engine"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

func notificationGraph() *engine.Graph {
	return &engine.Graph{
		Name:       "graph",
		Promotions: []engine.Promotion{{From: "development", To: "test"}},
		Backflow:   engine.BackflowPolicy{Enabled: true, Sources: []string{"test", "production"}, Target: "development"},
	}
}

func lifecycleFixture(kind v1.NotificationRequestType, mergedAt time.Time) LifecycleRequest {
	repo := RepositoryIdentity{Provider: "github", Host: "github.com", ID: "123", Name: "example/repo"}
	request := RequestV1{ID: "42", Type: kind, Source: "development", Destination: "test", LogicalSource: "development", LogicalDestination: "test", URL: "https://github.com/example/repo/pull/42"}
	if kind == v1.NotificationBackflow {
		request.Source = "oiax/backflow/test/abcdef0"
		request.Destination = "development"
		request.LogicalSource = "test"
		request.LogicalDestination = "development"
	}
	return LifecycleRequest{Repository: repo, Graph: "graph", Request: request, State: LifecycleMerged, MergedAt: mergedAt}
}

func TestMergeEventNormalizesBothManagedRequestTypes(t *testing.T) {
	t.Parallel()
	mergedAt := time.Date(2026, 9, 5, 12, 0, 0, 0, time.FixedZone("offset", -5*60*60))
	observedAt := mergedAt.Add(time.Minute)
	policy := &v1.NotificationPolicy{EnvironmentNames: map[string]string{"development": "Development", "test": "Testing"}}
	for _, kind := range []v1.NotificationRequestType{v1.NotificationPromotion, v1.NotificationBackflow} {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			request := lifecycleFixture(kind, mergedAt)
			event, ok := MergeEvent(notificationGraph(), policy, request, observedAt)
			if !ok {
				t.Fatal("managed merge was not normalized")
			}
			if event.Kind != v1.NotificationRequestMerged || event.Request.Type != kind || event.ID != EventID(request.Repository, "42", v1.NotificationRequestMerged) {
				t.Fatalf("unstable event identity: %+v", event)
			}
			if !event.OccurredAt.Equal(mergedAt.UTC()) || !event.ObservedAt.Equal(observedAt.UTC()) || !event.Snapshot.CommitsUnavailable {
				t.Fatalf("lifecycle facts changed: %+v", event)
			}
			if kind == v1.NotificationPromotion && (event.SourceEnvironment != "Development" || event.DestinationEnvironment != "Testing") {
				t.Fatalf("promotion labels = %q -> %q", event.SourceEnvironment, event.DestinationEnvironment)
			}
			if kind == v1.NotificationBackflow && (event.SourceEnvironment != "Testing" || event.DestinationEnvironment != "Development" || event.Request.Source != "oiax/backflow/test/abcdef0") {
				t.Fatalf("backflow facts = %+v", event)
			}
		})
	}

	legacy := lifecycleFixture(v1.NotificationBackflow, mergedAt)
	legacy.Request.LogicalSource = ""
	event, ok := MergeEvent(notificationGraph(), policy, legacy, observedAt)
	if !ok || event.Request.LogicalSource != "" || event.SourceEnvironment != legacy.Request.Source {
		t.Fatalf("legacy logical edge was fabricated: %+v", event)
	}
}

func TestMergeEventRejectsUnprovenOrOrphanedLifecycleFacts(t *testing.T) {
	t.Parallel()
	mergedAt := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	base := lifecycleFixture(v1.NotificationPromotion, mergedAt)
	cases := map[string]func(*LifecycleRequest){
		"open":                    func(r *LifecycleRequest) { r.State = LifecycleOpen },
		"closed unmerged":         func(r *LifecycleRequest) { r.State = LifecycleClosed },
		"missing merged time":     func(r *LifecycleRequest) { r.MergedAt = time.Time{} },
		"wrong graph":             func(r *LifecycleRequest) { r.Graph = "other" },
		"orphaned promotion edge": func(r *LifecycleRequest) { r.Request.Source = "feature" },
		"unknown request type":    func(r *LifecycleRequest) { r.Request.Type = "unknown" },
	}
	for name, mutate := range cases {
		request := base
		mutate(&request)
		if _, ok := MergeEvent(notificationGraph(), nil, request, mergedAt.Add(time.Minute)); ok {
			t.Errorf("%s produced a merge event", name)
		}
	}
	backflow := lifecycleFixture(v1.NotificationBackflow, mergedAt)
	backflow.Request.LogicalSource = "removed"
	if _, ok := MergeEvent(notificationGraph(), nil, backflow, mergedAt.Add(time.Minute)); ok {
		t.Error("orphaned explicit backflow edge produced an event")
	}
	branchEquality := base
	branchEquality.State = LifecycleClosed
	branchEquality.Request.Destination = branchEquality.Request.Source
	if _, ok := MergeEvent(notificationGraph(), nil, branchEquality, mergedAt.Add(time.Minute)); ok {
		t.Error("branch equality was mistaken for a forge merge")
	}
	if _, ok := MergeEvent(nil, nil, base, mergedAt.Add(time.Minute)); ok {
		t.Error("nil topology produced an event")
	}
}

func TestDefaultSubscriptionsRouteOnlyPostActivationMerges(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	repo := lifecycleFixture(v1.NotificationPromotion, now).Repository
	configOID := strings.Repeat("a", 40)
	ledger := NewLedger(repo, "graph", configOID)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "ENDPOINT"}}}
	var err error
	ledger, err = AcceptPolicy(ledger, PolicyRevisionV1{ConfigOID: configOID, PolicyDigest: policyDigestForTest(policy)}, policy, now, RevisionEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	for index, kind := range []v1.NotificationRequestType{v1.NotificationPromotion, v1.NotificationBackflow} {
		request := lifecycleFixture(kind, now.Add(time.Duration(index+1)*time.Minute))
		request.Request.ID = strconv.Itoa(42 + index)
		request.Request.URL = "https://github.com/example/repo/pull/" + request.Request.ID
		event, ok := MergeEvent(notificationGraph(), policy, request, now.Add(3*time.Minute))
		if !ok {
			t.Fatal("fixture merge not eligible")
		}
		ledger, err = AdmitEvent(ledger, configOID, event)
		if err != nil {
			t.Fatal(err)
		}
	}
	old := lifecycleFixture(v1.NotificationPromotion, now.Add(-time.Second))
	old.Request.ID = "41"
	old.Request.URL = "https://github.com/example/repo/pull/41"
	oldEvent, _ := MergeEvent(notificationGraph(), policy, old, now.Add(3*time.Minute))
	ledger, err = AdmitEvent(ledger, configOID, oldEvent)
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Deliveries) != 2 {
		t.Fatalf("default subscriptions admitted %d deliveries, want both post-activation request types only", len(ledger.Deliveries))
	}
}

func policyDigestForTest(policy *v1.NotificationPolicy) string {
	return Digest("test-policy", string(policy.Destinations[0].Type), policy.Destinations[0].EndpointEnv)
}
