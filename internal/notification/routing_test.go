package notification

import (
	"testing"
	"time"

	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

func TestRoutingCombinations(t *testing.T) {
	for _, transport := range []v1.NotificationTransport{v1.NotificationTeams, v1.NotificationSlack, v1.NotificationWebhook} {
		for _, kind := range []v1.NotificationEvent{v1.NotificationRequestCreated, v1.NotificationRequestMerged} {
			for _, typ := range []v1.NotificationRequestType{v1.NotificationPromotion, v1.NotificationBackflow} {
				p := modelPolicy()
				p.Destinations[0].Type = transport
				p.Destinations[0].Events = []v1.NotificationEvent{kind}
				p.Destinations[0].RequestTypes = []v1.NotificationRequestType{typ}
				l, err := AcceptPolicy(NewLedger(modelRepo(), "graph", modelRevision("a").ConfigOID), modelRevision("a"), p, modelTime(), RevisionEvidence{})
				if err != nil {
					t.Fatal(err)
				}
				e := modelEvent()
				e.Kind, e.Request.Type = kind, typ
				e.ID = EventID(e.Repository, e.Request.ID, kind)
				l, err = AdmitEvent(l, modelRevision("a").ConfigOID, e)
				if err != nil || len(l.Deliveries) != 1 {
					t.Fatalf("%s/%s/%s: %v", transport, kind, typ, err)
				}
			}
		}
	}
}

func TestRoutingGenerationTransitions(t *testing.T) {
	for _, mode := range []string{"wording", "rotation", "endpoint", "transport", "rename", "disable", "remove", "all-disabled", "empty-events", "empty-types", "new-subscription"} {
		t.Run(mode, func(t *testing.T) {
			l := modelLedger(t)
			generation := l.Destinations["ops"].Generation
			p := modelPolicy()
			// Keep processing enabled so retirement is actually durable.
			p.Destinations = append(p.Destinations, v1.NotificationDestination{Name: "audit", Type: v1.NotificationWebhook, EndpointEnv: "AUDIT"})
			switch mode {
			case "wording":
				p.Templates = &v1.NotificationTemplates{Body: templateTextPointer("changed")}
			case "rotation": // Secret values never enter policy or generation identity.
			case "endpoint":
				p.Destinations[0].EndpointEnv = "NEW_ENDPOINT"
			case "transport":
				p.Destinations[0].Type = v1.NotificationTeams
			case "rename":
				p.Destinations[0].Name = "renamed"
			case "disable":
				disabled := false
				p.Destinations[0].Enabled = &disabled
			case "remove":
				p.Destinations = p.Destinations[1:]
			case "all-disabled":
				p.Destinations = nil
			case "empty-events":
				p.Destinations[0].Events = []v1.NotificationEvent{}
			case "empty-types":
				p.Destinations[0].RequestTypes = []v1.NotificationRequestType{}
			case "new-subscription":
				p.Destinations[0].Events = []v1.NotificationEvent{v1.NotificationRequestCreated, v1.NotificationRequestMerged}
			}
			now := modelTime().Add(time.Hour)
			evidence := RevisionEvidence{AcceptedOID: modelRevision("a").ConfigOID, IncomingOID: modelRevision("b").ConfigOID, Relation: RevisionDescendant}
			next, err := AcceptPolicy(l, modelRevision("b"), p, now, evidence)
			if err != nil {
				t.Fatal(err)
			}
			d := next.Destinations["ops"]
			switch mode {
			case "endpoint", "transport":
				if d.Generation == generation || !d.ActivatedAt.Equal(now) {
					t.Fatal("identity change retained old epoch")
				}
			case "rename", "disable", "remove", "empty-events", "empty-types":
				if d.Active {
					t.Fatal("retirement not recorded")
				}
			case "all-disabled":
				if next != l {
					t.Fatal("disabled processing mutated state")
				}
			default:
				if d.Generation != generation || !d.ActivatedAt.Equal(modelTime()) {
					t.Fatal("presentation or subscription reset epoch")
				}
			}
			if mode == "new-subscription" {
				if !d.Subscriptions[SubscriptionKey(v1.NotificationRequestCreated, v1.NotificationPromotion)].Cutoff.Equal(now) || !d.Subscriptions[SubscriptionKey(v1.NotificationRequestMerged, v1.NotificationPromotion)].Cutoff.Equal(modelTime()) {
					t.Fatal("subscription cutoffs lost")
				}
			}
		})
	}
}

func TestRoutingDisabledResumeAndNewNameCutoff(t *testing.T) {
	for _, recorded := range []bool{false, true} {
		l := modelLedger(t)
		original := l.Destinations["ops"].Generation
		off := &v1.NotificationPolicy{}
		if recorded {
			off.Destinations = []v1.NotificationDestination{{Name: "audit", Type: v1.NotificationWebhook, EndpointEnv: "AUDIT"}}
		}
		evidence := RevisionEvidence{AcceptedOID: modelRevision("a").ConfigOID, IncomingOID: modelRevision("b").ConfigOID, Relation: RevisionDescendant}
		var err error
		l, err = AcceptPolicy(l, modelRevision("b"), off, modelTime().Add(time.Hour), evidence)
		if err != nil {
			t.Fatal(err)
		}
		on := modelPolicy()
		on.Destinations = append(on.Destinations, v1.NotificationDestination{Name: "new", Type: v1.NotificationWebhook, EndpointEnv: "SLACK"})
		evidence.AcceptedOID, evidence.IncomingOID = l.PolicyRevision.ConfigOID, modelRevision("c").ConfigOID
		l, err = AcceptPolicy(l, modelRevision("c"), on, modelTime().Add(2*time.Hour), evidence)
		if err != nil {
			t.Fatal(err)
		}
		if (l.Destinations["ops"].Generation != original) != recorded {
			t.Fatal("disable persistence semantics lost")
		}
		e := modelEvent()
		e.OccurredAt = modelTime().Add(90 * time.Minute)
		l, err = AdmitEvent(l, modelRevision("c").ConfigOID, e)
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if recorded {
			want = 0
		}
		if len(l.Deliveries) != want {
			t.Fatal("disabled-interval event replayed to fresh subscription or lost from old epoch")
		}
	}
}
