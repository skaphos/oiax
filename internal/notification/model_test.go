package notification

import (
	"errors"
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
