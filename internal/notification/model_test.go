package notification

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

func modelRepo() RepositoryIdentity {
	return RepositoryIdentity{Provider: "github", Host: "github.com", ID: "123", Name: "example/repo"}
}
func modelPolicy() *v1.NotificationPolicy {
	return &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: "slack", EndpointEnv: "SLACK"}}}
}
func modelTime() time.Time { return time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC) }
func modelRevision(c string) PolicyRevisionV1 {
	return PolicyRevisionV1{ConfigOID: strings.Repeat(c, 40), PolicyDigest: strings.Repeat(c, 64)}
}
func modelLedger(t *testing.T) *LedgerV1 {
	t.Helper()
	l := NewLedger(modelRepo(), "graph", modelRevision("a").ConfigOID)
	l, err := AcceptPolicy(l, modelRevision("a"), modelPolicy(), modelTime(), RevisionEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	return l
}
func modelEvent() EventV1 {
	r := RequestV1{ID: "42", Type: "promotion", Source: "dev", Destination: "test", URL: "https://github.com/example/repo/pull/42"}
	return EventV1{ID: EventID(modelRepo(), r.ID, "request-merged"), Kind: "request-merged", Repository: modelRepo(), Graph: "graph", Request: r, OccurredAt: modelTime(), ObservedAt: modelTime(), Snapshot: CommitSnapshot{CommitsUnavailable: true}}
}

func modelReadyDelivery(t *testing.T) (*LedgerV1, string) {
	t.Helper()
	l := modelLedger(t)
	e := modelEvent()
	var err error
	if l, err = AdmitEvent(l, l.PolicyRevision.ConfigOID, e); err != nil {
		t.Fatal(err)
	}
	key := DeliveryKey(e.ID, "ops", l.Destinations["ops"].Generation)
	if l, err = SaveMessage(l, l.PolicyRevision.ConfigOID, key, RenderedMessageV1{Body: "saved"}); err != nil {
		t.Fatal(err)
	}
	return l, key
}

func TestNotificationIdentity(t *testing.T) {
	t.Parallel()
	r := modelRepo()
	original := EventID(r, "42", "request-merged")
	r.Name = "renamed/repository"
	r.Host = "GITHUB.COM."
	if original != EventID(r, "42", "request-merged") {
		t.Fatal("rename changed identity")
	}
	if original == EventID(r, "42", "request-created") {
		t.Fatal("event kinds collide")
	}
	if !strings.HasPrefix(original, "sha256:") || len(original) != 71 {
		t.Fatal(original)
	}
	r.ID = "12"
	a := EventID(r, "345", "request-merged")
	r.ID = "123"
	if a == EventID(r, "45", "request-merged") {
		t.Fatal("ambiguous concatenation")
	}
	r.ID = "123"
	if len(GraphKey(r, "graph")) != 64 {
		t.Fatal("invalid graph key")
	}
	if GraphKey(r, "graph") == GraphKey(r, "other") {
		t.Fatal("graphs collide")
	}
}

func TestNotificationPolicyRevision(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		rev      PolicyRevisionV1
		relation RevisionRelation
		want     error
	}{
		{"same", modelRevision("a"), RevisionUnknown, nil},
		{"mismatch", PolicyRevisionV1{ConfigOID: modelRevision("a").ConfigOID, PolicyDigest: modelRevision("b").PolicyDigest}, RevisionUnknown, ErrPolicyMismatch},
		{"descendant", modelRevision("b"), RevisionDescendant, nil},
		{"ancestor", modelRevision("b"), RevisionAncestor, ErrStaleRevision},
		{"divergent", modelRevision("b"), RevisionDivergent, ErrUnorderedRevision},
		{"unknown", modelRevision("b"), RevisionUnknown, ErrUnorderedRevision},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := modelLedger(t)
			before := l.Clone()
			evidence := RevisionEvidence{AcceptedOID: l.PolicyRevision.ConfigOID, IncomingOID: tc.rev.ConfigOID, Relation: tc.relation}
			got, err := AcceptPolicy(l, tc.rev, modelPolicy(), modelTime().Add(time.Hour), evidence)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if !reflect.DeepEqual(l, before) {
				t.Fatal("input mutated")
			}
			if err == nil && (!reflect.DeepEqual(got.Destinations, l.Destinations) || got.PolicyRevision != tc.rev) {
				t.Fatal("unrelated revision reset cutoff or failed to advance")
			}
		})
	}
	l := modelLedger(t)
	next := modelRevision("b")
	_, err := AcceptPolicy(l, next, modelPolicy(), modelTime(), RevisionEvidence{AcceptedOID: strings.Repeat("c", 40), IncomingOID: next.ConfigOID, Relation: RevisionDescendant})
	if !errors.Is(err, ErrUnorderedRevision) {
		t.Fatal("stale ancestry evidence accepted")
	}
}

func TestNotificationEpochsAndContentRevert(t *testing.T) {
	t.Parallel()
	l := modelLedger(t)
	p := modelPolicy()
	p.Destinations = append(p.Destinations, v1.NotificationDestination{Name: "audit", Type: "webhook", EndpointEnv: "AUDIT"})
	advance := func(c string) {
		t.Helper()
		rev := modelRevision(c)
		var err error
		l, err = AcceptPolicy(l, rev, p, modelTime().Add(time.Hour), RevisionEvidence{AcceptedOID: l.PolicyRevision.ConfigOID, IncomingOID: rev.ConfigOID, Relation: RevisionDescendant})
		if err != nil {
			t.Fatal(err)
		}
	}
	advance("b")
	generation := l.Destinations["ops"].Generation
	p.Destinations[0].Enabled = new(bool)
	advance("c")
	if l.Destinations["ops"].Active {
		t.Fatal("recorded disable not retired")
	}
	p.Destinations[0].Enabled = nil
	advance("d")
	if l.Destinations["ops"].Generation == generation {
		t.Fatal("reenable did not start new generation")
	}
	before := l.Clone()
	p.Destinations = nil
	advance("e")
	if !reflect.DeepEqual(l, before) {
		t.Fatal("all-disabled transition changed durable policy")
	}
}

func TestNotificationAdmissionClaimAndMonotoneReceipt(t *testing.T) {
	t.Parallel()
	l := modelLedger(t)
	e := modelEvent()
	rev := l.PolicyRevision.ConfigOID
	old := e
	old.OccurredAt = old.OccurredAt.Add(-time.Nanosecond)
	got, err := AdmitEvent(l, rev, old)
	if err != nil || len(got.Deliveries) != 0 {
		t.Fatal("historical event admitted", err)
	}
	l, err = AdmitEvent(l, rev, e)
	if err != nil {
		t.Fatal(err)
	}
	key := DeliveryKey(e.ID, "ops", l.Destinations["ops"].Generation)
	message := RenderedMessageV1{Title: "Ready", Body: "Saved text"}
	l, err = SaveMessage(l, rev, key, message)
	if err != nil {
		t.Fatal(err)
	}
	l, err = Claim(l, rev, key, "attempt-1", modelTime())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Claim(l, rev, key, "attempt-2", modelTime().Add(-time.Second)); !errors.Is(err, ErrNotDue) {
		t.Fatal("clock skew shortened lease", err)
	}
	if _, err = Claim(l, modelRevision("b").ConfigOID, key, "attempt-2", modelTime().Add(3*time.Minute)); !errors.Is(err, ErrStaleRevision) {
		t.Fatal("stale worker claimed", err)
	}
	// Late acceptance is terminal even after lease expiry; stale failures cannot regress it.
	l, err = RecordResult(l, key, "attempt-1", AttemptResult{Code: OutcomeAccepted}, modelTime().Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	l, err = RecordResult(l, key, "attempt-1", AttemptResult{Code: OutcomeNetwork}, modelTime().Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if l.Deliveries[key].Status != StatusDelivered {
		t.Fatal("success regressed")
	}
	if _, err = Claim(l, rev, key, "attempt-2", modelTime().Add(time.Hour)); !errors.Is(err, ErrNotDue) {
		t.Fatal("terminal receipt claimed", err)
	}
	e.ObservedAt = e.ObservedAt.Add(time.Hour)
	l, err = AdmitEvent(l, rev, e)
	if err != nil {
		t.Fatal(err)
	}
	if !l.Events[e.ID].ObservedAt.Equal(modelTime()) {
		t.Fatal("retry rewrote first envelope")
	}
	if *l.Deliveries[key].Message != message {
		t.Fatal("saved payload changed")
	}
}

func TestNotificationRenewRetryAndRetirement(t *testing.T) {
	t.Parallel()
	l := modelLedger(t)
	e := modelEvent()
	rev := l.PolicyRevision.ConfigOID
	var err error
	l, err = AdmitEvent(l, rev, e)
	if err != nil {
		t.Fatal(err)
	}
	key := DeliveryKey(e.ID, "ops", l.Destinations["ops"].Generation)
	l, err = SaveMessage(l, rev, key, RenderedMessageV1{Body: "saved"})
	if err != nil {
		t.Fatal(err)
	}
	l, err = Claim(l, rev, key, "first", modelTime())
	if err != nil {
		t.Fatal(err)
	}
	l, err = RenewClaim(l, rev, key, "first", modelTime().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !l.Deliveries[key].Lease.Until.Equal(modelTime().Add(121 * time.Second)) {
		t.Fatal("lease not renewed")
	}
	if _, err := RenewClaim(l, "stale", key, "first", modelTime()); !errors.Is(err, ErrStaleRevision) {
		t.Fatal(err)
	}
	if _, err := RenewClaim(l, rev, key, "wrong", modelTime()); !errors.Is(err, ErrNotDue) {
		t.Fatal(err)
	}
	if _, err := RenewClaim(l, rev, key, "first", modelTime().Add(3*time.Minute)); !errors.Is(err, ErrNotDue) {
		t.Fatal(err)
	}
	l, err = RecordResult(l, key, "first", AttemptResult{Code: OutcomeRateLimited, RetryAfter: 48 * time.Hour}, modelTime().Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !l.Deliveries[key].NextAttemptAt.Equal(modelTime().Add(24*time.Hour + 2*time.Second)) {
		t.Fatal("Retry-After not capped")
	}
	l, err = Claim(l, rev, key, "second", modelTime().Add(25*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	before := l.Clone()
	l, err = RecordResult(l, key, "first", AttemptResult{Code: OutcomeNetwork}, modelTime().Add(25*time.Hour))
	if err != nil || !reflect.DeepEqual(before, l) {
		t.Fatal("stale failure changed newer claim", err)
	}
	if _, err := RecordResult(l, key, "invented", AttemptResult{Code: OutcomeAccepted}, modelTime()); !errors.Is(err, ErrInvalidState) {
		t.Fatal("unproven success accepted")
	}
	p := modelPolicy()
	p.Destinations[0].Enabled = new(bool)
	p.Destinations = append(p.Destinations, v1.NotificationDestination{Name: "audit", Type: "webhook", EndpointEnv: "AUDIT"})
	next := modelRevision("b")
	l, err = AcceptPolicy(l, next, p, modelTime().Add(26*time.Hour), RevisionEvidence{AcceptedOID: rev, IncomingOID: next.ConfigOID, Relation: RevisionDescendant})
	if err != nil {
		t.Fatal(err)
	}
	if l.Deliveries[key].Status != StatusSkipped {
		t.Fatal("retired attempt not skipped")
	}
	l, err = RecordResult(l, key, "second", AttemptResult{Code: OutcomeNetwork}, modelTime().Add(27*time.Hour))
	if err != nil || l.Deliveries[key].Status != StatusSkipped {
		t.Fatal("retirement revived", err)
	}
	for _, tc := range []struct {
		attempt int
		code    OutcomeCode
		want    time.Duration
	}{{1, OutcomeNetwork, time.Minute}, {2, OutcomeService, 2 * time.Minute}, {99, OutcomeNetwork, time.Hour}, {1, OutcomeMissingSecret, time.Hour}, {0, OutcomeCanceled, time.Minute}} {
		if got := RetryDelay(tc.attempt, AttemptResult{Code: tc.code}); got != tc.want {
			t.Errorf("retry %v = %s", tc, got)
		}
	}
}

func TestNotificationInvalidInputsAndCapacity(t *testing.T) {
	t.Parallel()
	l := modelLedger(t)
	rev := l.PolicyRevision.ConfigOID
	if _, err := AcceptPolicy(nil, modelRevision("a"), modelPolicy(), modelTime(), RevisionEvidence{}); !errors.Is(err, ErrInvalidState) {
		t.Fatal(err)
	}
	if err := CheckRevision(l.PolicyRevision, PolicyRevisionV1{}, RevisionEvidence{}); !errors.Is(err, ErrInvalidState) {
		t.Fatal(err)
	}
	if _, err := AdmitEvent(l, "stale", modelEvent()); !errors.Is(err, ErrStaleRevision) {
		t.Fatal(err)
	}
	e := modelEvent()
	e.ID = "wrong"
	if _, err := AdmitEvent(l, rev, e); !errors.Is(err, ErrInvalidState) {
		t.Fatal(err)
	}
	if _, err := SaveMessage(l, "stale", "missing", RenderedMessageV1{}); !errors.Is(err, ErrStaleRevision) {
		t.Fatal(err)
	}
	if _, err := SaveMessage(l, rev, "missing", RenderedMessageV1{}); !errors.Is(err, ErrInvalidState) {
		t.Fatal(err)
	}
	if _, err := Claim(l, rev, "missing", "id", modelTime()); !errors.Is(err, ErrInvalidState) {
		t.Fatal(err)
	}
	for _, oid := range []string{"", "HEAD", strings.Repeat("z", 64), strings.Repeat("A", 40)} {
		if ValidOID(oid) {
			t.Fatal("invalid OID accepted", oid)
		}
	}
	if !ValidOID(strings.Repeat("a", 64)) || ValidOutcome("untrusted error") {
		t.Fatal("enum/OID validation")
	}
	l, err := AdmitEvent(l, rev, modelEvent())
	if err != nil {
		t.Fatal(err)
	}
	key := DeliveryKey(modelEvent().ID, "ops", l.Destinations["ops"].Generation)
	if _, err := SaveMessage(l, rev, key, RenderedMessageV1{Body: strings.Repeat("a", (12<<10)+1)}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	l.Graph = strings.Repeat("a", MaxLedgerBytes)
	if err := CheckCapacity(l, true); !errors.Is(err, ErrCapacity) {
		t.Fatal("byte cap not enforced")
	}
	l.Graph = "graph"
	for i := range MaxDeliveries + 1 {
		l.Deliveries[Digest("record", time.Duration(i).String())] = DeliveryRecord{Status: StatusPending}
	}
	if err := CheckCapacity(l, true); !errors.Is(err, ErrCapacity) {
		t.Fatal("record cap not enforced")
	}
}

// A destination that can never accept anything must reach a terminal state and
// stop consuming ledger budget. Before the attempt cap, 100 records against a
// receiver answering 401 grew the ledger from 129 KB to 8.17 MB and exhausted
// capacity after roughly 1,200 hourly attempts, suspending every other
// destination with it.
func TestNotificationPermanentFailureIsAbandonedWithBoundedGrowth(t *testing.T) {
	t.Parallel()
	l := modelLedger(t)
	rev := l.PolicyRevision.ConfigOID
	now := modelTime()
	size := func() int {
		t.Helper()
		data, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		return len(data)
	}
	var keys []string
	for i := range 20 {
		e := modelEvent()
		e.Request.ID = fmt.Sprint(1000 + i)
		e.Request.URL = "https://github.com/example/repo/pull/" + e.Request.ID
		e.ID = EventID(modelRepo(), e.Request.ID, e.Kind)
		var err error
		if l, err = AdmitEvent(l, rev, e); err != nil {
			t.Fatal(err)
		}
		key := DeliveryKey(e.ID, "ops", l.Destinations["ops"].Generation)
		keys = append(keys, key)
		if l, err = SaveMessage(l, rev, key, RenderedMessageV1{Title: "Branch promotion completed", Body: "These commits were promoted to the test environment."}); err != nil {
			t.Fatal(err)
		}
	}
	admitted, settled := size(), 0
	// Two cycles per attempt budget: the second half must change nothing at all.
	for cycle := 1; cycle <= 2*MaxAttempts; cycle++ {
		for _, key := range keys {
			// Each claim paces its destination and schedules an hourly retry;
			// stepping well past both keeps every unfinished record due.
			now = now.Add(2 * time.Hour)
			attempt := Digest("attempt-v1", fmt.Sprint(cycle), key)
			next, err := Claim(l, rev, key, attempt, now)
			if errors.Is(err, ErrNotDue) {
				continue
			}
			if err != nil {
				t.Fatalf("cycle %d claim: %v", cycle, err)
			}
			if l, err = RecordResult(next, key, attempt, AttemptResult{Code: OutcomeConfiguration}, now.Add(time.Second)); err != nil {
				t.Fatalf("cycle %d result: %v", cycle, err)
			}
		}
		if cycle == MaxAttempts {
			settled = size()
		}
	}
	for _, key := range keys {
		r := l.Deliveries[key]
		if r.Status != StatusSkipped || r.Code != OutcomeAbandoned || r.Attempts != MaxAttempts || len(r.AttemptIDs) != MaxAttempts {
			t.Fatalf("record not abandoned: %s %s attempts=%d", r.Status, r.Code, r.Attempts)
		}
	}
	if settled == 0 || size() != settled {
		t.Fatalf("ledger still growing after abandonment: %d -> %d bytes", settled, size())
	}
	// The cap admits at most MaxAttempts 64-hex attempt IDs per record.
	if growth, budget := settled-admitted, len(keys)*MaxAttempts*128; growth > budget {
		t.Fatalf("attempt evidence unbounded: grew %d bytes, budget %d", growth, budget)
	}
	if err := CheckCapacity(l, true); err != nil {
		t.Fatal("permanent failure exhausted capacity", err)
	}
	if due := DueDeliveries(l, now.Add(365*24*time.Hour)); len(due) != 0 {
		t.Fatalf("abandoned records still scheduled: %d", len(due))
	}
}

// A deterministic oversize payload can never succeed, so it is terminal on its
// first result instead of being retried hourly for a day.
func TestNotificationTerminalFailureClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		attempts int
		code     OutcomeCode
		want     bool
	}{
		{1, OutcomePayloadTooLarge, true},
		{1, OutcomeConfiguration, false},
		{MaxAttempts - 1, OutcomeMissingSecret, false},
		{MaxAttempts, OutcomeMissingSecret, true},
		{MaxAttempts, OutcomeInvalidEndpoint, true},
		{MaxAttempts, OutcomeRedirect, true},
		{MaxAttempts, OutcomeResponseTooLarge, true},
		{MaxAttempts, OutcomeConfiguration, true},
		{1000, OutcomeNetwork, false},
		{1000, OutcomeService, false},
		{1000, OutcomeRateLimited, false},
		{1000, OutcomeCanceled, false},
	} {
		if got := TerminalFailure(tc.attempts, tc.code); got != tc.want {
			t.Errorf("TerminalFailure(%d, %s) = %v", tc.attempts, tc.code, got)
		}
	}
	l := modelLedger(t)
	rev := l.PolicyRevision.ConfigOID
	e := modelEvent()
	var err error
	if l, err = AdmitEvent(l, rev, e); err != nil {
		t.Fatal(err)
	}
	key := DeliveryKey(e.ID, "ops", l.Destinations["ops"].Generation)
	if l, err = SaveMessage(l, rev, key, RenderedMessageV1{Body: "saved"}); err != nil {
		t.Fatal(err)
	}
	if l, err = Claim(l, rev, key, "first", modelTime()); err != nil {
		t.Fatal(err)
	}
	if l, err = RecordResult(l, key, "first", AttemptResult{Code: OutcomePayloadTooLarge}, modelTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if r := l.Deliveries[key]; r.Status != StatusSkipped || r.Code != OutcomePayloadTooLarge {
		t.Fatalf("oversize payload retried: %s %s", r.Status, r.Code)
	}
	if _, err := Claim(l, rev, key, "second", modelTime().Add(48*time.Hour)); !errors.Is(err, ErrNotDue) {
		t.Fatal("abandoned record claimed", err)
	}
	// Late acceptance for a proven attempt still wins over a terminal size
	// failure.
	if l, err = RecordResult(l, key, "first", AttemptResult{Code: OutcomeAccepted}, modelTime().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if l.Deliveries[key].Status != StatusDelivered {
		t.Fatal("late acceptance lost to terminal size failure")
	}
}

func TestNotificationAttemptRejectsTerminalLedgerOutcomes(t *testing.T) {
	t.Parallel()
	for _, code := range []OutcomeCode{OutcomeRetired, OutcomeAbandoned} {
		t.Run(string(code), func(t *testing.T) {
			t.Parallel()
			l, key := modelReadyDelivery(t)
			var err error
			if l, err = Claim(l, l.PolicyRevision.ConfigOID, key, "single", modelTime()); err != nil {
				t.Fatal(err)
			}
			before := l.Clone()
			if _, err := RecordResult(l, key, "single", AttemptResult{Code: code}, modelTime().Add(time.Second)); !errors.Is(err, ErrInvalidState) {
				t.Fatalf("single result = %v", err)
			}
			if !reflect.DeepEqual(l, before) {
				t.Fatal("invalid single result mutated input")
			}

			l, key = modelReadyDelivery(t)
			attempts := map[string]string{key: "batch-attempt"}
			batchID := BatchID("operation", "ops")
			if l, _, err = ClaimBatch(l, l.PolicyRevision.ConfigOID, "ops", batchID, []string{key}, attempts, modelTime()); err != nil {
				t.Fatal(err)
			}
			before = l.Clone()
			receipts := map[string]AttemptReceipt{key: {AttemptID: attempts[key], Result: AttemptResult{Code: code}}}
			if _, err := RecordResults(l, "ops", batchID, receipts, modelTime().Add(time.Second)); !errors.Is(err, ErrInvalidState) {
				t.Fatalf("batch result = %v", err)
			}
			if !reflect.DeepEqual(l, before) {
				t.Fatal("invalid batch result mutated input")
			}
		})
	}
}
