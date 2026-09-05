package reconcile

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/notification"
	"github.com/skaphos/oiax/internal/notification/notificationtest"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

func TestNotificationPreviewDocument(t *testing.T) {
	plan := engine.Plan{Graph: "graph"}
	var before, without, with bytes.Buffer
	if err := RenderJSON(&before, plan); err != nil {
		t.Fatal(err)
	}
	if err := RenderJSON(&without, plan, nil); err != nil {
		t.Fatal(err)
	}
	if before.String() != without.String() {
		t.Fatal("no-policy contract changed")
	}
	preview := &NotificationPreview{SchemaVersion: 1, Observation: "uninitialized", Items: []NotificationPreviewItem{}}
	if err := RenderJSON(&with, plan, preview); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if json.Unmarshal(with.Bytes(), &doc) != nil || doc["notifications"] == nil {
		t.Fatal("not one JSON document")
	}
	delete(doc, "notifications")
	var original map[string]any
	if json.Unmarshal(before.Bytes(), &original) != nil || !reflect.DeepEqual(doc, original) {
		t.Fatal("core plan changed")
	}
}

type previewReadOnlyStore struct {
	notification.LedgerStore
	t *testing.T
}

func (s previewReadOnlyStore) Commit(context.Context, string, notification.Transition) (notification.Snapshot, error) {
	s.t.Fatal("preview wrote notification state")
	return notification.Snapshot{}, notification.ErrInvalidState
}

func TestNotificationPreviewReadOnlyAndRevisionDiagnostics(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "CANARY"}}}
	store := &notificationtest.MemoryStore{}
	r := mergeRuntime(func() time.Time { return now }, store, policy)
	r.Topology = observationGraph()
	r.Reader = &observationReader{list: func(q forge.LifecycleQuery) (forge.LifecyclePage, error) {
		return forge.LifecyclePage{Progress: notification.ScanProgress{Complete: true}}, nil
	}}
	if err := r.Activate(ctx); err != nil {
		t.Fatal(err)
	}
	r.Store = previewReadOnlyStore{store, t}
	r.LookupEnv = func(string) (string, bool) { t.Fatal("preview resolved endpoint"); return "", false }
	r.Sender = func(v1.NotificationDestination) notification.Sender {
		t.Fatal("preview constructed sender")
		return nil
	}
	before, err := store.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p := r.preview(ctx, engine.Plan{}); p.Observation != "complete" {
		t.Fatalf("%+v", p)
	}
	for _, relation := range []notification.RevisionRelation{notification.RevisionAncestor, notification.RevisionDivergent, notification.RevisionUnknown} {
		r.ConfigOID = strings.Repeat("b", 40)
		r.VerifyRevision = func(context.Context, string, string) (notification.RevisionRelation, error) { return relation, nil }
		p := r.preview(ctx, engine.Plan{})
		if p.Observation != "unavailable" || p.Reason == "" {
			t.Fatalf("unsafe revision accepted: %+v", p)
		}
	}
	after, err := store.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("preview advanced state")
	}
}

func TestNotificationPreviewDecisions(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	repo := notification.RepositoryIdentity{Provider: "github", Host: "github.com", ID: "1", Name: "example/repo"}
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "DO_NOT_LOOK_UP", Events: []v1.NotificationEvent{v1.NotificationRequestCreated, v1.NotificationRequestMerged}}}}
	rev := notification.PolicyRevisionV1{ConfigOID: strings.Repeat("a", 40), PolicyDigest: policyDigest(policy)}
	l, err := notification.AcceptPolicy(notification.NewLedger(repo, "graph", rev.ConfigOID), rev, policy, now, notification.RevisionEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	e := mergeEvent(repo, "42", now)
	l, err = notification.AdmitEvent(l, rev.ConfigOID, e)
	if err != nil {
		t.Fatal(err)
	}
	key := notification.DeliveryKey(e.ID, "ops", l.Destinations["ops"].Generation)
	for _, status := range []notification.DeliveryStatus{notification.StatusPending, notification.StatusDelivered, notification.StatusRetryable, notification.StatusSkipped} {
		r := l.Deliveries[key]
		r.Status = status
		r.NextAttemptAt = now.Add(time.Hour)
		l.Deliveries[key] = r
		p := composeNotificationPreview(policy, l, []notification.EventV1{e}, engine.Plan{}, now, "complete", "")
		want := map[notification.DeliveryStatus]string{notification.StatusPending: "pending", notification.StatusDelivered: "delivered", notification.StatusRetryable: "retry-not-due", notification.StatusSkipped: "subscription-not-active"}[status]
		if len(p.Items) != 1 || p.Items[0].Decision != want {
			t.Fatalf("%s: %+v", status, p)
		}
	}
	p := composeNotificationPreview(policy, l, nil, engine.Plan{Actions: []engine.Action{{Type: engine.ActionCreatePromotionRequest, From: "dev", To: "test"}}}, now, "complete", "")
	for _, item := range p.Items {
		if item.Decision == "conditional-on-create" && (item.EventID != "" || item.RequestID != "") {
			t.Fatal("invented request/event identity")
		}
	}
}

func TestNotificationPreviewUsesCreationPrecisionEvidence(t *testing.T) {
	second := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "ENDPOINT", Events: []v1.NotificationEvent{v1.NotificationRequestCreated}}}}
	repo := notification.RepositoryIdentity{Provider: "github", Host: "github.com", ID: "1", Name: "example/repo"}
	revision := notification.PolicyRevisionV1{ConfigOID: strings.Repeat("a", 40), PolicyDigest: policyDigest(policy)}
	l, err := notification.AcceptPolicy(notification.NewLedger(repo, "graph", revision.ConfigOID), revision, policy, second.Add(100*time.Millisecond), notification.RevisionEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	e := mergeEvent(repo, "42", second)
	e.Kind = v1.NotificationRequestCreated
	e.ID = notification.EventID(repo, "42", e.Kind)
	e.ObservedAt = second.Add(500 * time.Millisecond)
	l.KnownRequests["42"] = notification.LifecycleRequest{Repository: repo, Graph: "graph", Request: e.Request, CreatedAt: second, Origin: &notification.NotificationOriginV1{Version: 1, OperationID: "created", Graph: "graph", ConfigOID: revision.ConfigOID, ObservedAt: second.Add(200 * time.Millisecond), LogicalSource: "dev", LogicalTarget: "test", SourceOID: strings.Repeat("b", 40), BaseOID: strings.Repeat("c", 40)}}
	p := composeNotificationPreview(policy, l, []notification.EventV1{e}, engine.Plan{}, e.ObservedAt, "complete", "")
	if len(p.Items) != 1 || p.Items[0].Decision != "pending" {
		t.Fatal("preview disagrees with actual admission", p)
	}
}
