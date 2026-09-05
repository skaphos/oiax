package reconcile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/notification"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

type NotificationPreview struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Observation   string                    `json:"observation"`
	Reason        string                    `json:"reason,omitempty"`
	Items         []NotificationPreviewItem `json:"items"`
}
type NotificationPreviewItem struct {
	EventID     string                     `json:"eventId,omitempty"`
	Destination string                     `json:"destination"`
	Event       v1.NotificationEvent       `json:"event"`
	RequestType v1.NotificationRequestType `json:"requestType"`
	RequestID   string                     `json:"requestId,omitempty"`
	Decision    string                     `json:"decision"`
	Reason      string                     `json:"reason"`
}

// PreviewNotifications performs only reads. No endpoint environment lookup,
// policy acceptance, ledger transition, claim or dispatch is reachable here.
func (c *Coordinator) PreviewNotifications(ctx context.Context, plan engine.Plan) *NotificationPreview {
	if !c.NotificationPolicy.IsEnabled() {
		return nil
	}
	preview := &NotificationPreview{SchemaVersion: 1, Observation: "unavailable", Items: []NotificationPreviewItem{}}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := c.withNotificationRuntime(ctx, func(r *NotificationRuntime) error { preview = r.preview(ctx, plan); return nil })
	if err != nil {
		preview.Reason = NotificationProblem(err).Reason
	}
	return preview
}

func (r *NotificationRuntime) preview(ctx context.Context, plan engine.Plan) *NotificationPreview {
	now := r.now()
	snapshot, err := r.Store.Read(ctx)
	if errors.Is(err, notification.ErrAbsent) {
		return composeNotificationPreview(r.Policy, nil, nil, plan, now, "uninitialized", "current-cutoff-on-next-reconcile")
	}
	if err != nil || snapshot.Ledger == nil {
		if err == nil {
			err = notification.ErrInvalidState
		}
		return composeNotificationPreview(r.Policy, nil, nil, plan, now, "unavailable", NotificationProblem(err).Reason)
	}
	l := snapshot.Ledger
	incoming := notification.PolicyRevisionV1{ConfigOID: r.ConfigOID, PolicyDigest: policyDigest(r.Policy)}
	evidence := notification.RevisionEvidence{AcceptedOID: l.PolicyRevision.ConfigOID, IncomingOID: r.ConfigOID}
	if evidence.AcceptedOID != evidence.IncomingOID && r.VerifyRevision != nil {
		evidence.Relation, err = r.VerifyRevision(ctx, evidence.AcceptedOID, evidence.IncomingOID)
		if err != nil {
			evidence.Relation = notification.RevisionUnknown
		}
	}
	if err := notification.CheckRevision(l.PolicyRevision, incoming, evidence); err != nil {
		return composeNotificationPreview(r.Policy, l, nil, plan, now, "unavailable", NotificationProblem(err).Reason)
	}
	// Project the reviewed policy on a private copy, never advancing the store.
	l, err = notification.AcceptPolicy(l, incoming, r.Policy, now, evidence)
	if err != nil {
		return composeNotificationPreview(r.Policy, nil, nil, plan, now, "unavailable", NotificationProblem(err).Reason)
	}
	state, reason := "complete", ""
	var events []notification.EventV1
	add := func(req notification.LifecycleRequest) {
		if !req.Repository.Same(r.Repository) {
			return
		}
		if e, ok := notification.CreationEvent(r.Topology, r.Policy, req, now); ok {
			events = append(events, e)
		}
		if e, ok := notification.MergeEvent(r.Topology, r.Policy, req, now); ok {
			events = append(events, e)
		}
	}
	if r.Reader == nil {
		return composeNotificationPreview(r.Policy, l, nil, plan, now, "unavailable", "notification-lifecycle-unavailable")
	}
	known := make([]string, 0, len(l.KnownRequests))
	for id, req := range l.KnownRequests {
		if req.State == notification.LifecycleOpen {
			known = append(known, id)
		}
	}
	sort.Strings(known)
	if len(known) > 100 {
		state = "incomplete"
		known = known[:100]
	}
	for _, id := range known {
		req, err := r.Reader.GetLifecycleRequest(ctx, forge.RequestID(id))
		if err != nil {
			state = "incomplete"
			continue
		}
		add(req)
	}
	for _, kind := range []v1.NotificationEvent{v1.NotificationRequestCreated, v1.NotificationRequestMerged} {
		from := now
		for _, d := range l.Destinations {
			for _, sub := range d.Subscriptions {
				if sub.Event == kind && sub.Cutoff.Before(from) {
					from = sub.Cutoff
				}
			}
		}
		q := forge.LifecycleQuery{Graph: r.Graph, Kind: kind, From: from, Through: now, Limit: 100}
		for page := 0; page < 2; page++ {
			result, err := r.Reader.ListLifecyclePage(ctx, q)
			for _, req := range result.Requests {
				add(req)
			}
			if err != nil {
				state = "incomplete"
				reason = "notification-discovery-incomplete"
				break
			}
			if result.Progress.Complete {
				break
			}
			q.Cursor = result.Progress.Cursor
			if page == 1 {
				state = "incomplete"
			}
		}
	}
	return composeNotificationPreview(r.Policy, l, events, plan, now, state, reason)
}

func composeNotificationPreview(policy *v1.NotificationPolicy, l *notification.LedgerV1, observed []notification.EventV1, plan engine.Plan, now time.Time, state, reason string) *NotificationPreview {
	p := &NotificationPreview{SchemaVersion: 1, Observation: state, Reason: reason, Items: []NotificationPreviewItem{}}
	if !policy.IsEnabled() {
		return nil
	}
	events := map[string]notification.EventV1{}
	for _, e := range observed {
		events[e.ID] = e
	}
	if l != nil {
		for id, e := range l.Events {
			events[id] = e
		}
	}
	for _, e := range events {
		for _, d := range policy.Destinations {
			item := NotificationPreviewItem{EventID: e.ID, Destination: d.Name, Event: e.Kind, RequestType: e.Request.Type, RequestID: e.Request.ID, Decision: "filtered", Reason: "not-subscribed"}
			if l != nil {
				ds := l.Destinations[d.Name]
				sub, subscribed := ds.Subscriptions[notification.SubscriptionKey(e.Kind, e.Request.Type)]
				record, recorded := l.Deliveries[notification.DeliveryKey(e.ID, d.Name, ds.Generation)]
				switch {
				case recorded && record.Status == notification.StatusDelivered:
					item.Decision, item.Reason = "delivered", "durable-receipt"
				case !d.IsEnabled() || !ds.Active || recorded && record.Status == notification.StatusSkipped:
					item.Decision, item.Reason = "subscription-not-active", "subscription-retired"
				case !subscribed:
				case !recorded && e.OccurredAt.Before(sub.Cutoff):
					item.Decision, item.Reason = "subscription-not-active", "before-subscription-cutoff"
				case recorded && ((record.Status == notification.StatusRetryable && now.Before(record.NextAttemptAt)) || now.Before(record.Lease.Until) || now.Before(ds.NextSendAt)):
					item.Decision, item.Reason = "retry-not-due", "retry-or-lease-not-due"
				default:
					item.Decision, item.Reason = "pending", "not-yet-delivered"
				}
			}
			p.Items = append(p.Items, item)
		}
	}
	for _, a := range plan.Actions {
		kind := v1.NotificationPromotion
		switch a.Type {
		case engine.ActionCreatePromotionRequest:
		case engine.ActionCreateBackflowRequest:
			kind = v1.NotificationBackflow
		default:
			continue
		}
		for _, d := range policy.Destinations {
			if !d.IsEnabled() {
				continue
			}
			created, typed := false, d.RequestTypes == nil
			for _, e := range d.Events {
				created = created || e == v1.NotificationRequestCreated
			}
			for _, typ := range d.RequestTypes {
				typed = typed || typ == kind
			}
			if created && typed {
				p.Items = append(p.Items, NotificationPreviewItem{Destination: d.Name, Event: v1.NotificationRequestCreated, RequestType: kind, Decision: "conditional-on-create", Reason: "requires-successful-managed-create"})
			}
		}
	}
	sort.SliceStable(p.Items, func(i, j int) bool {
		a, b := p.Items[i], p.Items[j]
		if a.EventID != b.EventID {
			return a.EventID < b.EventID
		}
		if a.Destination != b.Destination {
			return a.Destination < b.Destination
		}
		return a.RequestType < b.RequestType
	})
	return p
}

func renderNotificationPreview(w io.Writer, p *NotificationPreview) error {
	if p == nil {
		return nil
	}
	if _, err := fmt.Fprintf(w, "Notifications: %s", p.Observation); err != nil {
		return err
	}
	if p.Reason != "" {
		if _, err := fmt.Fprintf(w, " (%s)", p.Reason); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	for _, item := range p.Items {
		if _, err := fmt.Fprintf(w, "  %s: %s/%s request %s: %s (%s)\n", item.Destination, item.Event, item.RequestType, item.RequestID, item.Decision, item.Reason); err != nil {
			return err
		}
	}
	return nil
}
