package notification

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// batchLedger admits n events for the "ops" destination with saved messages
// and returns the ledger plus the delivery keys in event order.
func batchLedger(t *testing.T, n int) (*LedgerV1, []string) {
	t.Helper()
	l := modelLedger(t)
	rev := l.PolicyRevision.ConfigOID
	keys := make([]string, 0, n)
	for i := range n {
		e := modelEvent()
		e.Request.ID = fmt.Sprintf("%d", 100+i)
		e.ID = EventID(modelRepo(), e.Request.ID, e.Kind)
		var err error
		l, err = AdmitEvent(l, rev, e)
		if err != nil {
			t.Fatal(err)
		}
		key := DeliveryKey(e.ID, "ops", l.Destinations["ops"].Generation)
		l, err = SaveMessage(l, rev, key, RenderedMessageV1{Title: "Ready", Body: "Saved"})
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key)
	}
	return l, keys
}

func batchAttempts(operation string, keys []string) map[string]string {
	attempts := make(map[string]string, len(keys))
	for _, key := range keys {
		attempts[key] = Digest("attempt-v1", operation, key)
	}
	return attempts
}

func TestNotificationBatchClaimAndRecord(t *testing.T) {
	t.Parallel()
	l, keys := batchLedger(t, 3)
	rev := l.PolicyRevision.ConfigOID
	now := modelTime()
	batch := BatchID("run", "ops")
	attempts := batchAttempts("run", keys)
	before := l.Clone()
	claimedLedger, claimed, err := ClaimBatch(l, rev, "ops", batch, keys, attempts, now)
	if err != nil || !reflect.DeepEqual(claimed, keys) {
		t.Fatalf("claim = %v, %v", claimed, err)
	}
	if !reflect.DeepEqual(l, before) {
		t.Fatal("input mutated")
	}
	d := claimedLedger.Destinations["ops"]
	if d.Lease.AttemptID != batch || !d.Lease.Until.Equal(now.Add(ClaimDuration)) || !d.NextSendAt.Equal(now.Add(time.Second)) {
		t.Fatalf("destination lease = %+v", d)
	}
	for _, key := range keys {
		r := claimedLedger.Deliveries[key]
		if r.Status != StatusClaimed || r.Attempts != 1 || r.Lease.AttemptID != attempts[key] || !r.Lease.Until.Equal(now.Add(ClaimDuration)) {
			t.Fatalf("record %s = %+v", key, r)
		}
	}
	if got := DueDeliveries(claimedLedger, now.Add(time.Minute)); len(got) != 0 {
		t.Fatal("batch lease did not fence the destination", got)
	}
	// A replayed claim for the same operation is invalid, as for single claims.
	if _, _, err := ClaimBatch(claimedLedger, rev, "ops", batch, keys, attempts, now.Add(ClaimDuration+time.Second)); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("replayed attempt = %v", err)
	}
	if _, _, err := ClaimBatch(claimedLedger, "stale", "ops", batch, keys, attempts, now); !errors.Is(err, ErrStaleRevision) {
		t.Fatal(err)
	}
	if _, _, err := ClaimBatch(l, rev, "ops", "", keys, attempts, now); !errors.Is(err, ErrInvalidState) {
		t.Fatal("empty batch ID accepted")
	}
	if _, _, err := ClaimBatch(l, rev, "missing", batch, keys, attempts, now); !errors.Is(err, ErrNotDue) {
		t.Fatal("unknown destination claimed", err)
	}
	if _, _, err := ClaimBatch(l, rev, "ops", batch, []string{"missing"}, map[string]string{"missing": "x"}, now); !errors.Is(err, ErrInvalidState) {
		t.Fatal("unknown record claimed", err)
	}
	if _, _, err := ClaimBatch(l, rev, "ops", batch, keys[:1], map[string]string{}, now); !errors.Is(err, ErrInvalidState) {
		t.Fatal("missing attempt ID accepted")
	}

	// Renewal extends only leases this batch still holds.
	renewed, err := RenewBatch(claimedLedger, rev, "ops", batch, attempts, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !renewed.Destinations["ops"].Lease.Until.Equal(now.Add(time.Minute+ClaimDuration)) || !renewed.Deliveries[keys[0]].Lease.Until.Equal(now.Add(time.Minute+ClaimDuration)) {
		t.Fatal("batch lease not renewed")
	}
	if _, err := RenewBatch(claimedLedger, rev, "ops", "other", attempts, now); !errors.Is(err, ErrNotDue) {
		t.Fatal("foreign batch renewed")
	}
	if _, err := RenewBatch(claimedLedger, rev, "ops", batch, attempts, now.Add(ClaimDuration)); !errors.Is(err, ErrNotDue) {
		t.Fatal("expired batch renewed")
	}
	if _, err := RenewBatch(claimedLedger, "stale", "ops", batch, attempts, now); !errors.Is(err, ErrStaleRevision) {
		t.Fatal(err)
	}

	// One accepted, one refused with a status, one abandoned before its POST.
	receipts := map[string]AttemptReceipt{
		keys[0]: {AttemptID: attempts[keys[0]], Result: AttemptResult{Code: OutcomeAccepted, Status: 202}},
		keys[1]: {AttemptID: attempts[keys[1]], Result: AttemptResult{Code: OutcomeConfiguration, Status: 400}},
		keys[2]: {AttemptID: attempts[keys[2]], Result: AttemptResult{Code: OutcomeCanceled}},
	}
	recorded, err := RecordResults(renewed, "ops", batch, receipts, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if d := recorded.Destinations["ops"]; d.Lease != (Lease{}) || !d.NextSendAt.Equal(now.Add(2*time.Minute+time.Second)) {
		t.Fatalf("batch lease not released: %+v", d)
	}
	if r := recorded.Deliveries[keys[0]]; r.Status != StatusDelivered || r.LastStatus != 202 || r.Lease != (Lease{}) {
		t.Fatalf("accepted = %+v", r)
	}
	if r := recorded.Deliveries[keys[1]]; r.Status != StatusRetryable || r.Code != OutcomeConfiguration || r.LastStatus != 400 || r.Lease != (Lease{}) {
		t.Fatalf("refused = %+v", r)
	}
	if r := recorded.Deliveries[keys[2]]; r.Status != StatusRetryable || r.Code != OutcomeCanceled || r.LastStatus != 0 || r.Lease != (Lease{}) || !r.NextAttemptAt.Equal(now.Add(3*time.Minute)) {
		t.Fatalf("abandoned = %+v", r)
	}
	if got := DueDeliveries(recorded, now.Add(2*time.Minute)); len(got) != 0 {
		t.Fatal("released records due before pacing and backoff", got)
	}
	if _, err := RecordResults(renewed, "ops", "", receipts, now); !errors.Is(err, ErrInvalidState) {
		t.Fatal("empty batch ID accepted")
	}
	if _, err := RecordResults(renewed, "other", batch, receipts, now); !errors.Is(err, ErrInvalidState) {
		t.Fatal("foreign destination receipt accepted")
	}
	if _, err := RecordResults(renewed, "ops", batch, map[string]AttemptReceipt{keys[0]: {AttemptID: "invented", Result: AttemptResult{Code: OutcomeAccepted}}}, now); !errors.Is(err, ErrInvalidState) {
		t.Fatal("unproven receipt accepted")
	}
}

func TestNotificationBatchSkipsRecordsThatAreNotDue(t *testing.T) {
	t.Parallel()
	l, keys := batchLedger(t, 3)
	rev := l.PolicyRevision.ConfigOID
	now := modelTime()
	delivered := l.Deliveries[keys[0]]
	delivered.Status = StatusDelivered
	delivered.Attempts, delivered.AttemptIDs = 1, []string{"earlier"}
	delivered.AcceptedAt, delivered.DeliveredAt, delivered.Code = now, now, OutcomeAccepted
	l.Deliveries[keys[0]] = delivered
	backoff := l.Deliveries[keys[1]]
	backoff.NextAttemptAt = now.Add(time.Hour)
	l.Deliveries[keys[1]] = backoff
	attempts := batchAttempts("run", keys)
	_, claimed, err := ClaimBatch(l, rev, "ops", BatchID("run", "ops"), keys, attempts, now)
	if err != nil || !reflect.DeepEqual(claimed, keys[2:]) {
		t.Fatalf("claimed = %v, %v", claimed, err)
	}
	fenced := l.Clone()
	for _, key := range keys {
		r := fenced.Deliveries[key]
		r.NextAttemptAt = now.Add(time.Hour)
		fenced.Deliveries[key] = r
	}
	if _, _, err := ClaimBatch(fenced, rev, "ops", BatchID("run", "ops"), keys, attempts, now); !errors.Is(err, ErrNotDue) {
		t.Fatal("empty batch claimed", err)
	}
	paced := l.Clone()
	d := paced.Destinations["ops"]
	d.NextSendAt = now.Add(time.Second)
	paced.Destinations["ops"] = d
	if _, _, err := ClaimBatch(paced, rev, "ops", BatchID("run", "ops"), keys, attempts, now); !errors.Is(err, ErrNotDue) {
		t.Fatal("pacing window ignored", err)
	}
}

func TestNotificationBatchCompetingRunsAndLateAcceptance(t *testing.T) {
	t.Parallel()
	l, keys := batchLedger(t, 2)
	rev := l.PolicyRevision.ConfigOID
	now := modelTime()
	first, firstAttempts := BatchID("first", "ops"), batchAttempts("first", keys)
	second, secondAttempts := BatchID("second", "ops"), batchAttempts("second", keys)
	l, claimed, err := ClaimBatch(l, rev, "ops", first, keys, firstAttempts, now)
	if err != nil || len(claimed) != 2 {
		t.Fatal(err)
	}
	// While the first batch lease is live a competing run claims nothing.
	if _, _, err := ClaimBatch(l, rev, "ops", second, keys, secondAttempts, now.Add(time.Minute)); !errors.Is(err, ErrNotDue) {
		t.Fatalf("competing batch = %v", err)
	}
	// A single-record claim is fenced by the batch lease as well.
	if _, err := Claim(l, rev, keys[0], "single", now.Add(time.Minute)); !errors.Is(err, ErrNotDue) {
		t.Fatalf("single claim during batch = %v", err)
	}
	// After the lease expires the second run takes over both records.
	later := now.Add(ClaimDuration + time.Second)
	l, claimed, err = ClaimBatch(l, rev, "ops", second, keys, secondAttempts, later)
	if err != nil || len(claimed) != 2 || l.Destinations["ops"].Lease.AttemptID != second {
		t.Fatalf("takeover = %v, %v", claimed, err)
	}
	// The first run's late failure for a superseded record changes nothing and
	// cannot release the second batch's lease; its late acceptance still wins.
	before := l.Clone()
	l, err = RecordResults(l, "ops", first, map[string]AttemptReceipt{keys[1]: {AttemptID: firstAttempts[keys[1]], Result: AttemptResult{Code: OutcomeNetwork}}}, later)
	if err != nil || !reflect.DeepEqual(before, l) {
		t.Fatal("stale failure changed the newer claim", err)
	}
	l, err = RecordResults(l, "ops", first, map[string]AttemptReceipt{keys[0]: {AttemptID: firstAttempts[keys[0]], Result: AttemptResult{Code: OutcomeAccepted, Status: 200}}}, later)
	if err != nil {
		t.Fatal(err)
	}
	if r := l.Deliveries[keys[0]]; r.Status != StatusDelivered || r.Attempts != 2 || r.LastStatus != 200 {
		t.Fatalf("late acceptance lost: %+v", r)
	}
	if l.Destinations["ops"].Lease.AttemptID != second {
		t.Fatal("late receipt released a foreign batch lease")
	}
	// The second run's later failure for the delivered record cannot regress it.
	l, err = RecordResults(l, "ops", second, map[string]AttemptReceipt{
		keys[0]: {AttemptID: secondAttempts[keys[0]], Result: AttemptResult{Code: OutcomeService, Status: 503}},
		keys[1]: {AttemptID: secondAttempts[keys[1]], Result: AttemptResult{Code: OutcomeAccepted, Status: 204}},
	}, later.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if r := l.Deliveries[keys[0]]; r.Status != StatusDelivered || r.LastStatus != 200 {
		t.Fatalf("terminal receipt regressed: %+v", r)
	}
	if r := l.Deliveries[keys[1]]; r.Status != StatusDelivered || r.LastStatus != 204 {
		t.Fatalf("second delivery lost: %+v", r)
	}
	if l.Destinations["ops"].Lease != (Lease{}) {
		t.Fatal("completed batch left its lease")
	}
}

func TestNotificationResultStatusAndOutcomeError(t *testing.T) {
	t.Parallel()
	l, keys := batchLedger(t, 1)
	rev := l.PolicyRevision.ConfigOID
	var err error
	l, err = Claim(l, rev, keys[0], "first", modelTime())
	if err != nil {
		t.Fatal(err)
	}
	l, err = RecordResult(l, keys[0], "first", AttemptResult{Code: OutcomeConfiguration, Status: 401}, modelTime().Add(time.Second))
	if err != nil || l.Deliveries[keys[0]].LastStatus != 401 {
		t.Fatalf("status lost: %+v, %v", l.Deliveries[keys[0]], err)
	}
	l, err = Claim(l, rev, keys[0], "second", modelTime().Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// An out-of-range status records no exchange rather than an invalid ledger.
	l, err = RecordResult(l, keys[0], "second", AttemptResult{Code: OutcomeNetwork, Status: 7}, modelTime().Add(2*time.Hour+time.Second))
	if err != nil || l.Deliveries[keys[0]].LastStatus != 0 {
		t.Fatalf("status not cleared: %+v, %v", l.Deliveries[keys[0]], err)
	}
	outcome := OutcomeError{Code: OutcomeConfiguration, Status: 400}
	if outcome.Error() != string(OutcomeConfiguration) {
		t.Fatalf("outcome text = %q", outcome.Error())
	}
	var found OutcomeError
	if !errors.As(fmt.Errorf("wrapped: %w", outcome), &found) || found.Status != 400 {
		t.Fatal("outcome status lost through wrapping")
	}
}

func TestNotificationCheckBatchSend(t *testing.T) {
	t.Parallel()
	l, keys := batchLedger(t, 2)
	rev := l.PolicyRevision.ConfigOID
	now := modelTime()
	batch := BatchID("run", "ops")
	attempts := batchAttempts("run", keys)
	claimed, _, err := ClaimBatch(l, rev, "ops", batch, keys, attempts, now)
	if err != nil {
		t.Fatal(err)
	}
	key := keys[0]
	for _, tc := range []struct {
		name   string
		mutate func(*LedgerV1)
		at     time.Time
		want   error
	}{
		{"owned", func(*LedgerV1) {}, now.Add(time.Second), nil},
		{"policy advanced", func(l *LedgerV1) { l.PolicyRevision.ConfigOID = "b" + rev[1:] }, now.Add(time.Second), ErrStaleRevision},
		{"destination retired", func(l *LedgerV1) { d := l.Destinations["ops"]; d.Active = false; l.Destinations["ops"] = d }, now.Add(time.Second), ErrNotDue},
		{"destination lease taken", func(l *LedgerV1) { d := l.Destinations["ops"]; d.Lease.AttemptID = "other"; l.Destinations["ops"] = d }, now.Add(time.Second), ErrNotDue},
		{"destination lease expired", func(*LedgerV1) {}, now.Add(ClaimDuration), ErrNotDue},
		{"record lease superseded", func(l *LedgerV1) { r := l.Deliveries[key]; r.Lease.AttemptID = "other"; l.Deliveries[key] = r }, now.Add(time.Second), ErrNotDue},
		{"record skipped", func(l *LedgerV1) {
			r := l.Deliveries[key]
			r.Status = StatusSkipped
			r.Code = OutcomeRetired
			l.Deliveries[key] = r
		}, now.Add(time.Second), ErrNotDue},
		{"generation changed", func(l *LedgerV1) {
			d := l.Destinations["ops"]
			d.Generation = Digest("other")
			l.Destinations["ops"] = d
		}, now.Add(time.Second), ErrNotDue},
		{"unsubscribed", func(l *LedgerV1) {
			d := l.Destinations["ops"]
			d.Subscriptions = map[string]Subscription{}
			l.Destinations["ops"] = d
		}, now.Add(time.Second), ErrNotDue},
		{"unknown record", func(l *LedgerV1) { delete(l.Deliveries, key) }, now.Add(time.Second), ErrNotDue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := claimed.Clone()
			tc.mutate(l)
			if err := CheckBatchSend(l, rev, "ops", batch, key, attempts[key], tc.at); !errors.Is(err, tc.want) {
				t.Fatalf("CheckBatchSend = %v, want %v", err, tc.want)
			}
		})
	}
	if err := CheckBatchSend(nil, rev, "ops", batch, key, attempts[key], now); !errors.Is(err, ErrAbsent) {
		t.Fatalf("nil ledger = %v", err)
	}
}
