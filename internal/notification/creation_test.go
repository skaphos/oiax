package notification

import (
	"strings"
	"testing"
	"time"

	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

func TestCreationEventRequiresOriginalProvenance(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	for _, kind := range []v1.NotificationRequestType{v1.NotificationPromotion, v1.NotificationBackflow} {
		t.Run(string(kind), func(t *testing.T) {
			req := lifecycleFixture(kind, now)
			req.CreatedAt = now.Add(-time.Minute)
			o := NotificationOriginV1{Version: 1, OperationID: "original", Graph: req.Graph, ConfigOID: strings.Repeat("a", 40), ObservedAt: req.CreatedAt.Add(-time.Second), LogicalSource: req.Request.LogicalSource, LogicalTarget: req.Request.LogicalDestination, SourceOID: strings.Repeat("b", 40), BaseOID: strings.Repeat("c", 40)}
			req.Origin = &o
			created, ok := CreationEvent(notificationGraph(), nil, req, now)
			if !ok || created.Kind != v1.NotificationRequestCreated || !created.OccurredAt.Equal(req.CreatedAt) || !created.ObservedAt.Equal(now) || !created.Snapshot.CommitsUnavailable {
				t.Fatalf("creation facts = %+v, %v", created, ok)
			}
			merged, ok := MergeEvent(notificationGraph(), nil, req, now)
			if !ok || merged.ID == created.ID {
				t.Fatal("short-lived request collapsed creation and merge")
			}
			for name, mutate := range map[string]func(*LifecycleRequest){
				"no origin":             func(r *LifecycleRequest) { r.Origin = nil },
				"wrong graph":           func(r *LifecycleRequest) { r.Origin.Graph = "other" },
				"wrong actual target":   func(r *LifecycleRequest) { r.Request.Destination = "other" },
				"wrong logical source":  func(r *LifecycleRequest) { r.Request.LogicalSource = "other" },
				"missing creation time": func(r *LifecycleRequest) { r.CreatedAt = time.Time{} },
				"future creation":       func(r *LifecycleRequest) { r.CreatedAt = now.Add(time.Hour) },
				"invalid state":         func(r *LifecycleRequest) { r.State = "unknown" },
			} {
				bad := req
				copy := o
				bad.Origin = &copy
				mutate(&bad)
				if _, ok := CreationEvent(notificationGraph(), nil, bad, now); ok {
					t.Errorf("%s admitted", name)
				}
			}
		})
	}
}

func TestEarliestAdmissibleTimeTracksActiveCutoffs(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	l := NewLedger(RepositoryIdentity{Provider: "github", Host: "github.com", ID: "123"}, "graph", strings.Repeat("a", 40))
	if _, ok := EarliestAdmissibleTime(l, v1.NotificationRequestMerged); ok {
		t.Fatal("unsubscribed kind reported a scan bound")
	}
	sub := func(event v1.NotificationEvent, cutoff time.Time) map[string]Subscription {
		return map[string]Subscription{SubscriptionKey(event, v1.NotificationPromotion): {Event: event, RequestType: v1.NotificationPromotion, Cutoff: cutoff}}
	}
	l.Destinations["retired"] = DestinationState{Name: "retired", Subscriptions: sub(v1.NotificationRequestMerged, now.Add(-72*time.Hour))}
	l.Destinations["late"] = DestinationState{Name: "late", Active: true, Subscriptions: sub(v1.NotificationRequestMerged, now)}
	l.Destinations["early"] = DestinationState{Name: "early", Active: true, Subscriptions: sub(v1.NotificationRequestMerged, now.Add(-time.Hour))}
	l.Destinations["other"] = DestinationState{Name: "other", Active: true, Subscriptions: sub(v1.NotificationRequestCreated, now.Add(-48*time.Hour))}
	bound, ok := EarliestAdmissibleTime(l, v1.NotificationRequestMerged)
	// The earliest active cutoff for the kind wins; a retired destination's
	// cutoff and another kind's cutoff never widen the scan.
	if want := now.Add(-time.Hour - time.Second); !ok || !bound.Equal(want) {
		t.Fatalf("merged bound = (%s, %v), want %s", bound, ok, want)
	}
	if bound, ok := EarliestAdmissibleTime(l, v1.NotificationRequestCreated); !ok || !bound.Equal(now.Add(-48*time.Hour-time.Second)) {
		t.Fatalf("created bound = (%s, %v)", bound, ok)
	}
}
