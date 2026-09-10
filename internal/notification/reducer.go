package notification

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"
	"unicode/utf8"

	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

var (
	ErrPolicyMismatch    = errors.New("policy-revision-mismatch")
	ErrStaleRevision     = errors.New("stale-config-revision")
	ErrUnorderedRevision = errors.New("config-revision-unordered")
	ErrNotDue            = errors.New("delivery-not-due")
	ErrClaimLost         = errors.New("delivery-claim-lost")
	ErrInvalidState      = errors.New("invalid-notification-state")
	ErrCapacity          = errors.New("notification-capacity-exhausted")

	// ErrRevisionUnreachable narrows ErrUnorderedRevision to the one unordered
	// case that has a recovery: the accepted configuration commit is not in the
	// repository at all, so `merge-base --is-ancestor` cannot answer and never
	// will while the ledger keeps naming it. It WRAPS ErrUnorderedRevision, so
	// every caller that already defers on an unorderable revision still defers,
	// byte for byte; only the diagnostic layer distinguishes it, to name the
	// operator recovery instead of asking for a descendant commit that cannot
	// be produced.
	ErrRevisionUnreachable = fmt.Errorf("config-revision-unreachable: %w", ErrUnorderedRevision)

	// ErrRevisionReachable refuses an operator-authorized reset for a ledger
	// whose accepted commit is still present. Ordering is decidable there, so
	// the ordinary rule applies — commit a reviewed descendant — and the reset
	// must not become a general way around the ordering guarantee.
	ErrRevisionReachable = errors.New("config-revision-reachable")
)

type RevisionRelation string

const (
	RevisionUnknown    RevisionRelation = "unknown"
	RevisionDescendant RevisionRelation = "descendant"
	RevisionAncestor   RevisionRelation = "ancestor"
	RevisionDivergent  RevisionRelation = "divergent"
	// RevisionOverride is not a relation Git can report. It is an operator's
	// explicit authorization to accept the incoming revision without ancestry,
	// and it is produced by exactly one code path — the `oiax notifications
	// reset` verifier, which yields it only for an accepted commit it has
	// positively established is absent. No automatic verifier returns it, so a
	// scheduled run can never advance policy on this evidence.
	RevisionOverride RevisionRelation = "override"
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
	case RevisionDescendant, RevisionOverride:
		return nil
	case RevisionAncestor:
		return ErrStaleRevision
	default:
		return ErrUnorderedRevision
	}
}

// OverrideRevision is the operator-authorized recovery for a ledger whose
// accepted configuration commit no longer exists: nothing can be proved a
// descendant of an object the repository does not have, so every subsequent run
// defers forever and the documented fix ("commit a reviewed descendant") is
// impossible. It advances policy exactly as AcceptPolicy does — receipts,
// events and delivery evidence are preserved, never reset — and additionally
// appends an immutable record naming both OIDs, so the gap in the ordering
// chain stays visible in the ledger forever rather than being papered over.
//
// It is deliberately not reachable from a scheduled run: the caller must supply
// RevisionOverride evidence, which only the reset verifier produces.
func OverrideRevision(l *LedgerV1, incoming PolicyRevisionV1, policy *v1.NotificationPolicy, now time.Time, evidence RevisionEvidence) (*LedgerV1, error) {
	if l == nil || now.IsZero() || !policy.IsEnabled() {
		return nil, ErrInvalidState
	}
	if evidence.Relation != RevisionOverride || evidence.AcceptedOID != l.PolicyRevision.ConfigOID || evidence.IncomingOID != incoming.ConfigOID {
		return nil, ErrInvalidState
	}
	// An empty or identical accepted OID needs no override; refusing here keeps
	// the audit free of records that document nothing.
	if !ValidOID(l.PolicyRevision.ConfigOID) || l.PolicyRevision.ConfigOID == incoming.ConfigOID {
		return nil, ErrInvalidState
	}
	if len(l.RevisionOverrides) >= MaxRevisionOverrides {
		return nil, ErrCapacity
	}
	out, err := acceptPolicy(l, incoming, policy, now, evidence, true)
	if err != nil {
		return nil, err
	}
	out.RevisionOverrides = append(slices.Clone(out.RevisionOverrides), RevisionOverrideV1{
		Version: 1, PriorOID: l.PolicyRevision.ConfigOID, AcceptedOID: incoming.ConfigOID, RecordedAt: now.UTC(),
	})
	if err := CheckCapacity(out, true); err != nil {
		return nil, err
	}
	return out, nil
}

func SubscriptionKey(event v1.NotificationEvent, kind v1.NotificationRequestType) string {
	return string(event) + "/" + string(kind)
}

// AcceptPolicy is atomic with revision advancement. Global-off is deliberately
// a no-op: it must not invent a retirement that an effect-free invocation could
// never have recorded. A descendant content revert is an ordinary new revision.
func AcceptPolicy(l *LedgerV1, incoming PolicyRevisionV1, policy *v1.NotificationPolicy, now time.Time, evidence RevisionEvidence) (*LedgerV1, error) {
	return acceptPolicy(l, incoming, policy, now, evidence, false)
}

// acceptPolicy keeps operator override evidence confined to OverrideRevision,
// which also appends the durable audit record. Ordinary reducer callers cannot
// advance a policy merely by manufacturing RevisionOverride evidence.
func acceptPolicy(l *LedgerV1, incoming PolicyRevisionV1, policy *v1.NotificationPolicy, now time.Time, evidence RevisionEvidence, allowOverride bool) (*LedgerV1, error) {
	if !policy.IsEnabled() {
		return l, nil
	}
	if l == nil || now.IsZero() {
		return nil, ErrInvalidState
	}
	if evidence.Relation == RevisionOverride && !allowOverride {
		return nil, ErrUnorderedRevision
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
// Claims are not capped because no terminal decision is safe until a result is
// durably recorded.
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
	out := l.Clone()
	if err := applyResult(out, key, attemptID, result, now); err != nil {
		return nil, err
	}
	if err := CheckCapacity(out, false); err != nil {
		return nil, err
	}
	return out, nil
}

// applyResult reduces one attempt result in place on an already cloned ledger.
// A destination lease is released only when it belongs to this attempt.
func applyResult(out *LedgerV1, key, attemptID string, result AttemptResult, now time.Time) error {
	r, ok := out.Deliveries[key]
	if !ok || !slices.Contains(r.AttemptIDs, attemptID) || !validAttemptOutcome(result.Code) || now.IsZero() {
		return ErrInvalidState
	}
	if r.Status == StatusDelivered {
		return nil
	}
	if result.Code == OutcomeAccepted {
		r.Status = StatusDelivered
		r.AcceptedAt = now.UTC()
		r.DeliveredAt = now.UTC()
		r.Code = result.Code
	} else {
		if r.Status == StatusSkipped || r.Lease.AttemptID != attemptID {
			return nil
		}
		if TerminalFailure(r.Attempts, result.Code) {
			r.Status = StatusSkipped
			// An intrinsically terminal transport result retains its diagnostic.
			// Exhaustion of the bounded retry policy is a distinct terminal fact.
			if result.Code == OutcomePayloadTooLarge {
				r.Code = result.Code
			} else {
				r.Code = OutcomeAbandoned
			}
		} else {
			r.Status = StatusRetryable
			r.Code = result.Code
			r.NextAttemptAt = now.UTC().Add(RetryDelay(r.Attempts, result))
		}
	}
	// Only a real HTTP status is retained; anything else records no exchange.
	r.LastStatus = 0
	if result.Status >= 100 && result.Status <= 599 {
		r.LastStatus = result.Status
	}
	d := out.Destinations[r.Destination]
	if d.Lease.AttemptID == attemptID {
		d.Lease = Lease{}
		out.Destinations[d.Name] = d
	}
	r.Lease = Lease{}
	out.Deliveries[key] = r
	return nil
}

// validAttemptOutcome excludes terminal ledger facts that no transport may
// return as a receipt. Retirement is produced only by policy acceptance and
// abandonment only by the reducer after exhausting deterministic failures.
func validAttemptOutcome(code OutcomeCode) bool {
	return ValidOutcome(code) && code != OutcomeRetired && code != OutcomeAbandoned
}

// BatchID names the destination lease held by one run's batch. It is distinct
// from every per-record attempt ID, so single-record transitions never release it.
func BatchID(operationID, destination string) string {
	return Digest("batch-v1", operationID, destination)
}

// ClaimBatch reserves a destination for one run and claims every listed record
// that is individually due, returning the claimed keys in input order. A live
// destination lease or pacing window fences the whole batch as not due; records
// that are individually not due are skipped rather than failed, and a batch
// that claims nothing is not due. Each claimed record carries its own attempt
// ID so late results keep their evidence exactly as single claims do.
// Claim counts remain unrestricted when the prior attempt's result is unknown;
// only RecordResults can make a terminal decision from durable evidence.
func ClaimBatch(l *LedgerV1, configOID, destination, batchID string, keys []string, attempts map[string]string, now time.Time) (*LedgerV1, []string, error) {
	if l.PolicyRevision.ConfigOID != configOID {
		return nil, nil, ErrStaleRevision
	}
	if batchID == "" || len(batchID) > 128 || now.IsZero() {
		return nil, nil, ErrInvalidState
	}
	d, ok := l.Destinations[destination]
	if !ok || !d.Active || now.Before(d.Lease.Until) || now.Before(d.NextSendAt) {
		return nil, nil, ErrNotDue
	}
	out := l.Clone()
	var claimed []string
	for _, key := range keys {
		r, ok := out.Deliveries[key]
		attemptID := attempts[key]
		if !ok || r.Destination != destination || attemptID == "" || len(attemptID) > 128 {
			return nil, nil, ErrInvalidState
		}
		e := out.Events[r.EventID]
		_, subscribed := d.Subscriptions[SubscriptionKey(e.Kind, e.Request.Type)]
		if r.Status == StatusDelivered || r.Status == StatusSkipped || d.Generation != r.Generation || !subscribed || r.Message == nil || now.Before(r.NextAttemptAt) || now.Before(r.Lease.Until) {
			continue
		}
		if slices.Contains(r.AttemptIDs, attemptID) {
			return nil, nil, ErrInvalidState
		}
		r.Status = StatusClaimed
		r.Attempts++
		r.AttemptIDs = append(r.AttemptIDs, attemptID)
		r.Lease = Lease{AttemptID: attemptID, Until: now.UTC().Add(ClaimDuration)}
		out.Deliveries[key] = r
		claimed = append(claimed, key)
	}
	if len(claimed) == 0 {
		return nil, nil, ErrNotDue
	}
	d.Lease = Lease{AttemptID: batchID, Until: now.UTC().Add(ClaimDuration)}
	d.NextSendAt = now.UTC().Add(time.Second)
	out.Destinations[d.Name] = d
	if err := CheckCapacity(out, true); err != nil {
		return nil, nil, err
	}
	return out, claimed, nil
}

// RenewBatch extends a still-live batch lease and every listed record that is
// still claimed by its batch attempt. It never revives an expired or superseded
// lease: a run whose batch lost its fence must stop sending.
func RenewBatch(l *LedgerV1, configOID, destination, batchID string, attempts map[string]string, now time.Time) (*LedgerV1, error) {
	if now.IsZero() {
		return nil, ErrInvalidState
	}
	if l.PolicyRevision.ConfigOID != configOID {
		return nil, ErrStaleRevision
	}
	d, ok := l.Destinations[destination]
	if !ok || !d.Active || d.Lease.AttemptID != batchID || !now.Before(d.Lease.Until) {
		return nil, ErrNotDue
	}
	out := l.Clone()
	until := now.UTC().Add(ClaimDuration)
	for key, attemptID := range attempts {
		r, ok := out.Deliveries[key]
		if !ok || r.Status != StatusClaimed || r.Lease.AttemptID != attemptID || !now.Before(r.Lease.Until) || d.Generation != r.Generation {
			continue
		}
		r.Lease.Until = until
		out.Deliveries[key] = r
	}
	d.Lease.Until = until
	out.Destinations[d.Name] = d
	return out, nil
}

// CheckBatchSend proves, on freshly observed durable state, that a batch still
// owns its destination and one claimed record at the caller's accepted
// revision. It is evaluated immediately before every POST and replaces the
// per-record renewal write: a policy accepted by another run while the batch
// is sending retires or regenerates the record, and a stale payload must then
// stay unsent rather than be delivered to a subscription that no longer exists.
//
// ErrStaleRevision and ErrNotDue mean the batch lost the destination and must
// stop. ErrClaimLost means only this record was settled by another attempt
// (for example a late accepted result); the batch still owns the destination
// and may continue with its remaining records.
func CheckBatchSend(l *LedgerV1, configOID, destination, batchID, key, attemptID string, now time.Time) error {
	if l == nil {
		return ErrAbsent
	}
	if now.IsZero() {
		return ErrInvalidState
	}
	if l.PolicyRevision.ConfigOID != configOID {
		return ErrStaleRevision
	}
	d, ok := l.Destinations[destination]
	if !ok || !d.Active || d.Lease.AttemptID != batchID || !now.Before(d.Lease.Until) {
		return ErrNotDue
	}
	r, ok := l.Deliveries[key]
	if !ok || r.Status != StatusClaimed || r.Lease.AttemptID != attemptID || !now.Before(r.Lease.Until) || d.Generation != r.Generation || r.Message == nil {
		return ErrClaimLost
	}
	e := l.Events[r.EventID]
	if _, subscribed := d.Subscriptions[SubscriptionKey(e.Kind, e.Request.Type)]; !subscribed {
		return ErrClaimLost
	}
	return nil
}

// AttemptReceipt pairs a record's batch attempt with the result to record.
type AttemptReceipt struct {
	AttemptID string
	Result    AttemptResult
}

// RecordResults reduces every receipt of one batch with RecordResult's rules,
// then releases the destination lease only if this batch still holds it. Every
// claimed record must have a receipt: an attempt abandoned before its POST is
// recorded as canceled so the ledger never keeps a stale earlier code.
func RecordResults(l *LedgerV1, destination, batchID string, receipts map[string]AttemptReceipt, now time.Time) (*LedgerV1, error) {
	if batchID == "" || now.IsZero() {
		return nil, ErrInvalidState
	}
	out := l.Clone()
	keys := slices.Sorted(maps.Keys(receipts))
	for _, key := range keys {
		receipt := receipts[key]
		if r, ok := out.Deliveries[key]; !ok || r.Destination != destination {
			return nil, ErrInvalidState
		}
		if err := applyResult(out, key, receipt.AttemptID, receipt.Result, now); err != nil {
			return nil, err
		}
	}
	if d, ok := out.Destinations[destination]; ok && d.Lease.AttemptID == batchID {
		d.Lease = Lease{}
		d.NextSendAt = now.UTC().Add(time.Second)
		out.Destinations[d.Name] = d
	}
	if err := CheckCapacity(out, false); err != nil {
		return nil, err
	}
	return out, nil
}

func RetryDelay(attempts int, result AttemptResult) time.Duration {
	delay := time.Minute * time.Duration(1<<min(max(attempts-1, 0), 6))
	delay = min(delay, time.Hour)
	if !TransientOutcome(result.Code) {
		delay = time.Hour
	}
	return max(delay, min(max(result.RetryAfter, 0), 24*time.Hour))
}

// TransientOutcome reports whether an identical later attempt could plausibly
// succeed without an operator change. Everything else is a deterministic
// configuration, endpoint or size fault that repeats on every retry.
func TransientOutcome(code OutcomeCode) bool {
	switch code {
	case OutcomeNetwork, OutcomeService, OutcomeRateLimited, OutcomeCanceled:
		return true
	default:
		return false
	}
}

// TerminalFailure reports whether a durably recorded failure must stop consuming
// ledger budget. An oversize payload is already bounded and truncated by the
// transport, so its recorded rejection is final; other non-transient faults keep
// a bounded attempt budget before a recorded result abandons the delivery. A
// transient code is never terminal, so a record whose attempts were spent on
// canceled claims is abandoned only once a receiver or endpoint actually refuses
// it and that refusal is persisted.
func TerminalFailure(attempts int, code OutcomeCode) bool {
	if code == OutcomePayloadTooLarge {
		return true
	}
	return !TransientOutcome(code) && attempts >= MaxAttempts
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
