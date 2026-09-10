package notification

import (
	"time"

	"github.com/skaphos/oiax/v2/internal/engine"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

// CreationEvent admits provider-owned requests with original POST provenance.
// Adoption alone is not creation; recovery uses the forge's original timestamp
// and request ID, independently of whether the request has since merged/closed.
func CreationEvent(graph *engine.Graph, policy *v1.NotificationPolicy, req LifecycleRequest, observedAt time.Time) (EventV1, bool) {
	if graph == nil || req.Graph != graph.Name || req.Request.ID == "" || req.CreatedAt.IsZero() || req.CreatedAt.After(observedAt) || req.Origin == nil || !ValidOrigin(*req.Origin) {
		return EventV1{}, false
	}
	switch req.State {
	case LifecycleOpen, LifecycleClosed, LifecycleMerged:
	default:
		return EventV1{}, false
	}
	o := req.Origin
	r := req.Request
	if o.Graph != req.Graph || o.LogicalTarget != r.Destination ||
		(r.Type == v1.NotificationPromotion && o.LogicalSource != r.Source) ||
		(r.LogicalSource != "" && r.LogicalSource != o.LogicalSource) ||
		(r.LogicalDestination != "" && r.LogicalDestination != o.LogicalTarget) {
		return EventV1{}, false
	}
	req.Request.LogicalSource, req.Request.LogicalDestination = o.LogicalSource, o.LogicalTarget
	// Pre-POST OIDs are hints, not verified membership. Immutable snapshot
	// enrichment is a separate phase; never substitute a moving source head.
	return eventForRequest(graph, policy, req, v1.NotificationRequestCreated, req.CreatedAt, observedAt)
}

// EventAdmissionTime returns the subscription eligibility time shared by
// admission and read-only preview. A whole-second server timestamp describes the second containing creation.
// Original pre-POST evidence can prove that an operation in that same second
// began after an activation cutoff. Use it only for subscription eligibility;
// the public event retains the server timestamp verbatim. This never grants a
// grace period to legacy/adopted requests or compensates arbitrary clock skew.
func EventAdmissionTime(l *LedgerV1, event EventV1) time.Time {
	when := event.OccurredAt
	if event.Kind != v1.NotificationRequestCreated || when.Nanosecond() != 0 {
		return when
	}
	known, ok := l.KnownRequests[event.Request.ID]
	if !ok || known.Origin == nil || !ValidOrigin(*known.Origin) || known.Request != event.Request ||
		known.Graph != event.Graph || !known.Repository.Same(event.Repository) || !known.CreatedAt.Equal(when) {
		return when
	}
	origin := known.Origin
	if origin.Graph == event.Graph && origin.ObservedAt.After(when) && !origin.ObservedAt.After(event.ObservedAt) && origin.ObservedAt.Truncate(time.Second).Equal(when) {
		return origin.ObservedAt
	}
	return when
}

// EarliestAdmissibleTime reports the earliest occurrence an event of kind could
// still be admitted at, and whether any active subscription accepts that kind at
// all. AdmitEvent drops anything below every active cutoff, and a subscription
// created later carries the cutoff of its own acceptance, so a discovery scan
// that starts here can never lose an admissible event — it only stops paying for
// history that is already unreachable. The one-second allowance covers
// EventAdmissionTime: whole-second creation evidence can move eligibility up to
// a second past the recorded occurrence, never earlier.
func EarliestAdmissibleTime(l *LedgerV1, kind v1.NotificationEvent) (time.Time, bool) {
	var earliest time.Time
	for _, d := range l.Destinations {
		if !d.Active {
			continue
		}
		for _, sub := range d.Subscriptions {
			if sub.Event != kind {
				continue
			}
			if earliest.IsZero() || sub.Cutoff.Before(earliest) {
				earliest = sub.Cutoff
			}
		}
	}
	if earliest.IsZero() {
		return time.Time{}, false
	}
	return earliest.UTC().Add(-time.Second), true
}
