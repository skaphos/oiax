package notification

import (
	"encoding/json"
	"errors"
	"slices"
	"time"
	"unicode/utf8"

	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

var (
	ErrPolicyMismatch    = errors.New("policy-revision-mismatch")
	ErrStaleRevision     = errors.New("stale-config-revision")
	ErrUnorderedRevision = errors.New("config-revision-unordered")
	ErrNotDue            = errors.New("delivery-not-due")
	ErrInvalidState      = errors.New("invalid-notification-state")
	ErrCapacity          = errors.New("notification-capacity-exhausted")
)

type RevisionRelation string

const (
	RevisionUnknown    RevisionRelation = "unknown"
	RevisionDescendant RevisionRelation = "descendant"
	RevisionAncestor   RevisionRelation = "ancestor"
	RevisionDivergent  RevisionRelation = "divergent"
)

// RevisionEvidence binds a verified relation to both exact OIDs. A coordinator
// must refresh it after a CAS conflict; evidence for another pair fails closed.
type RevisionEvidence struct {
	AcceptedOID, IncomingOID string
	Relation                 RevisionRelation
}

func CheckRevision(accepted, incoming PolicyRevisionV1, evidence RevisionEvidence) error {
	if !ValidOID(incoming.ConfigOID) || !ValidDigest(incoming.PolicyDigest) {
		return ErrInvalidState
	}
	if accepted.ConfigOID == "" {
		return nil
	}
	if incoming.ConfigOID == accepted.ConfigOID {
		if incoming.PolicyDigest != accepted.PolicyDigest {
			return ErrPolicyMismatch
		}
		return nil
	}
	if evidence.AcceptedOID != accepted.ConfigOID || evidence.IncomingOID != incoming.ConfigOID {
		return ErrUnorderedRevision
	}
	switch evidence.Relation {
	case RevisionDescendant:
		return nil
	case RevisionAncestor:
		return ErrStaleRevision
	default:
		return ErrUnorderedRevision
	}
}

func SubscriptionKey(event v1.NotificationEvent, kind v1.NotificationRequestType) string {
	return string(event) + "/" + string(kind)
}

// AcceptPolicy is atomic with revision advancement. Global-off is deliberately
// a no-op: it must not invent a retirement that an effect-free invocation could
// never have recorded. A descendant content revert is an ordinary new revision.
func AcceptPolicy(l *LedgerV1, incoming PolicyRevisionV1, policy *v1.NotificationPolicy, now time.Time, evidence RevisionEvidence) (*LedgerV1, error) {
	if !policy.IsEnabled() {
		return l, nil
	}
	if l == nil || now.IsZero() {
		return nil, ErrInvalidState
	}
	if err := CheckRevision(l.PolicyRevision, incoming, evidence); err != nil {
		return nil, err
	}
	if l.PolicyRevision == incoming {
		return l.Clone(), nil
	}
	out := l.Clone()
	now = now.UTC()
	seen := map[string]bool{}
	for _, config := range policy.Destinations {
		if !config.IsEnabled() {
			continue
		}
		seen[config.Name] = true
		d, exists := out.Destinations[config.Name]
		fingerprint := Digest("destination-v1", string(config.Type), config.EndpointEnv)
		if !exists || !d.Active || d.Fingerprint != fingerprint {
			d = DestinationState{Name: config.Name, Active: true, Fingerprint: fingerprint,
				Generation: Digest("generation-v1", GraphKey(l.Repository, l.Graph), config.Name, incoming.ConfigOID, fingerprint), ActivatedAt: now, Subscriptions: map[string]Subscription{}}
		}
		events := config.Events
		if events == nil {
			events = []v1.NotificationEvent{v1.NotificationRequestMerged}
		}
		types := config.RequestTypes
		if types == nil {
			types = []v1.NotificationRequestType{v1.NotificationPromotion, v1.NotificationBackflow}
		}
		subscriptions := map[string]Subscription{}
		for _, event := range events {
			for _, kind := range types {
				key := SubscriptionKey(event, kind)
				sub, ok := d.Subscriptions[key]
				if !ok {
					sub = Subscription{Event: event, RequestType: kind, Cutoff: now}
				}
				subscriptions[key] = sub
			}
		}
		d.Subscriptions = subscriptions
		out.Destinations[d.Name] = d
	}
	for name, d := range out.Destinations {
		if !seen[name] {
			d.Active = false
			d.Subscriptions = map[string]Subscription{}
			out.Destinations[name] = d
		}
	}
	for key, record := range out.Deliveries {
		d := out.Destinations[record.Destination]
		e := out.Events[record.EventID]
		_, subscribed := d.Subscriptions[SubscriptionKey(e.Kind, e.Request.Type)]
		if record.Status != StatusDelivered && record.Status != StatusSkipped && (!d.Active || d.Generation != record.Generation || !subscribed) {
			record.Status = StatusSkipped
			record.Code = OutcomeRetired
			out.Deliveries[key] = record
		}
	}
	out.PolicyRevision = incoming
	if err := CheckCapacity(out, true); err != nil {
		return nil, err
	}
	return out, nil
}

// AdmitEvent fixes facts on first successful admission, and never replays events
// preceding an individual subscription's activation cutoff.
func AdmitEvent(l *LedgerV1, configOID string, event EventV1) (*LedgerV1, error) {
	if l.PolicyRevision.ConfigOID != configOID {
		return nil, ErrStaleRevision
	}
	if event.ID != EventID(event.Repository, event.Request.ID, event.Kind) || !event.Repository.Same(l.Repository) || event.Graph != l.Graph || event.OccurredAt.IsZero() || event.ObservedAt.IsZero() {
		return nil, ErrInvalidState
	}
	out := l.Clone()
	if existing, ok := out.Events[event.ID]; ok {
		event = existing
	}
	eligibleAt := EventAdmissionTime(out, event)
	admitted := false
	for name, d := range out.Destinations {
		sub, ok := d.Subscriptions[SubscriptionKey(event.Kind, event.Request.Type)]
		if !d.Active || !ok || eligibleAt.Before(sub.Cutoff) {
			continue
		}
		key := DeliveryKey(event.ID, name, d.Generation)
		if _, exists := out.Deliveries[key]; !exists {
			out.Deliveries[key] = DeliveryRecord{EventID: event.ID, Destination: name, Generation: d.Generation, Status: StatusPending}
		}
		admitted = true
	}
	if admitted {
		event.Snapshot.Commits = slices.Clone(event.Snapshot.Commits)
		out.Events[event.ID] = event
	}
	if err := CheckCapacity(out, true); err != nil {
		return nil, err
	}
	return out, nil
}

func SaveMessage(l *LedgerV1, configOID, key string, message RenderedMessageV1) (*LedgerV1, error) {
	if l.PolicyRevision.ConfigOID != configOID {
		return nil, ErrStaleRevision
	}
	d, ok := l.Deliveries[key]
	if !ok {
		return nil, ErrInvalidState
	}
	if d.Message != nil {
		return l.Clone(), nil
	}
	if !utf8.ValidString(message.Title) || !utf8.ValidString(message.Body) || utf8.RuneCountInString(message.Title) > 256 || len(message.Body) > 12<<10 {
		return nil, ErrCapacity
	}
	out := l.Clone()
	d.Message = &message
	out.Deliveries[key] = d
	if err := CheckCapacity(out, true); err != nil {
		return nil, err
	}
	return out, nil
}

// Claim reserves both an event and a destination. Expired leases can be replaced,
// but cannot fence a suspended HTTP sender; late results retain their attempt IDs.
func Claim(l *LedgerV1, configOID, key, attemptID string, now time.Time) (*LedgerV1, error) {
	if l.PolicyRevision.ConfigOID != configOID {
		return nil, ErrStaleRevision
	}
	r, ok := l.Deliveries[key]
	if !ok || attemptID == "" || len(attemptID) > 128 || now.IsZero() {
		return nil, ErrInvalidState
	}
	d := l.Destinations[r.Destination]
	e := l.Events[r.EventID]
	_, subscribed := d.Subscriptions[SubscriptionKey(e.Kind, e.Request.Type)]
	if r.Status == StatusDelivered || r.Status == StatusSkipped || !d.Active || d.Generation != r.Generation || !subscribed || r.Message == nil || now.Before(r.NextAttemptAt) || now.Before(r.Lease.Until) || now.Before(d.Lease.Until) || now.Before(d.NextSendAt) {
		return nil, ErrNotDue
	}
	if slices.Contains(r.AttemptIDs, attemptID) {
		return nil, ErrInvalidState
	}
	out := l.Clone()
	r = out.Deliveries[key]
	r.Status = StatusClaimed
	r.Attempts++
	r.AttemptIDs = append(r.AttemptIDs, attemptID)
	r.Lease = Lease{AttemptID: attemptID, Until: now.UTC().Add(ClaimDuration)}
	d.Lease = r.Lease
	d.NextSendAt = now.UTC().Add(time.Second)
	out.Deliveries[key] = r
	out.Destinations[d.Name] = d
	if err := CheckCapacity(out, true); err != nil {
		return nil, err
	}
	return out, nil
}

// RenewClaim must be committed immediately before dispatch, then checked against
// the freshly read accepted revision. It never revives an expired/superseded claim.
func RenewClaim(l *LedgerV1, configOID, key, attemptID string, now time.Time) (*LedgerV1, error) {
	if l.PolicyRevision.ConfigOID != configOID {
		return nil, ErrStaleRevision
	}
	r, ok := l.Deliveries[key]
	d := l.Destinations[r.Destination]
	if !ok || r.Status != StatusClaimed || r.Lease.AttemptID != attemptID || d.Lease.AttemptID != attemptID || !now.Before(r.Lease.Until) || !now.Before(d.Lease.Until) || !d.Active || d.Generation != r.Generation {
		return nil, ErrNotDue
	}
	out := l.Clone()
	r.Lease.Until = now.UTC().Add(ClaimDuration)
	d.Lease = r.Lease
	out.Deliveries[key] = r
	out.Destinations[d.Name] = d
	return out, nil
}

// RecordResult accepts no policy replacement. Only proven attempt IDs can record
// results. Terminal success wins over stale failures, including late acceptance.
func RecordResult(l *LedgerV1, key, attemptID string, result AttemptResult, now time.Time) (*LedgerV1, error) {
	r, ok := l.Deliveries[key]
	if !ok || !slices.Contains(r.AttemptIDs, attemptID) || !ValidOutcome(result.Code) || now.IsZero() {
		return nil, ErrInvalidState
	}
	out := l.Clone()
	if r.Status == StatusDelivered {
		return out, nil
	}
	if result.Code == OutcomeAccepted {
		r.Status = StatusDelivered
		r.AcceptedAt = now.UTC()
		r.DeliveredAt = now.UTC()
		r.Code = result.Code
	} else {
		if r.Status == StatusSkipped || r.Lease.AttemptID != attemptID {
			return out, nil
		}
		r.Status = StatusRetryable
		r.Code = result.Code
		r.NextAttemptAt = now.UTC().Add(RetryDelay(r.Attempts, result))
	}
	d := out.Destinations[r.Destination]
	if d.Lease.AttemptID == attemptID {
		d.Lease = Lease{}
		out.Destinations[d.Name] = d
	}
	r.Lease = Lease{}
	out.Deliveries[key] = r
	if err := CheckCapacity(out, false); err != nil {
		return nil, err
	}
	return out, nil
}

func RetryDelay(attempts int, result AttemptResult) time.Duration {
	delay := time.Minute * time.Duration(1<<min(max(attempts-1, 0), 6))
	delay = min(delay, time.Hour)
	switch result.Code {
	case OutcomeNetwork, OutcomeService, OutcomeRateLimited, OutcomeCanceled:
	default:
		delay = time.Hour
	}
	return max(delay, min(max(result.RetryAfter, 0), 24*time.Hour))
}

// CheckCapacity reserves metadata growth before admission/claim. Receipts consume
// the reservation, so a known-full ledger never authorizes an unrecordable send.
func CheckCapacity(l *LedgerV1, reserve bool) error {
	if len(l.Deliveries) > MaxDeliveries {
		return ErrCapacity
	}
	data, err := json.Marshal(l)
	if err != nil {
		return ErrInvalidState
	}
	used := len(data)
	if reserve {
		for _, d := range l.Deliveries {
			if d.Status != StatusDelivered && d.Status != StatusSkipped {
				used += ResultReserveBytes
			}
		}
	}
	if used > MaxLedgerBytes {
		return ErrCapacity
	}
	return nil
}
