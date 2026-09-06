package store

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/v2/internal/notification"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

func codecLedger(t *testing.T) *notification.LedgerV1 {
	t.Helper()
	r := notification.RepositoryIdentity{Provider: "github", Host: "github.com", ID: "1", Name: "example/repo"}
	l := notification.NewLedger(r, "graph", strings.Repeat("a", 40))
	p := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: "slack", EndpointEnv: "SLACK"}}}
	l, err := notification.AcceptPolicy(l, notification.PolicyRevisionV1{ConfigOID: l.AnchorOID, PolicyDigest: strings.Repeat("a", 64)}, p, time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC), notification.RevisionEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestNotificationLedgerCodec(t *testing.T) {
	t.Parallel()
	l := codecLedger(t)
	data, err := Encode(l)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	again, err := Encode(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, again) {
		t.Fatal("noncanonical round trip")
	}
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"duplicate", bytes.Replace(data, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1)},
		{"new schema", bytes.Replace(data, []byte(`"version":1`), []byte(`"version":2`), 1)},
		{"trailing", append(append([]byte{}, data...), []byte(`{}`)...)},
		{"unknown field", bytes.Replace(data, []byte(`"version":1`), []byte(`"version":1,"secret":"no"`), 1)},
		{"oversized", bytes.Repeat([]byte(" "), (8<<20)+1)},
		{"bad anchor", bytes.Replace(data, []byte(l.AnchorOID), []byte("HEAD"), 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Decode(bytes.NewReader(tc.data)); err == nil {
				t.Fatal("invalid ledger accepted")
			}
		})
	}
	l.Deliveries["orphan"] = notification.DeliveryRecord{EventID: "unknown", Destination: "ops", Generation: "bad", Status: notification.StatusPending}
	if _, err := Encode(l); err == nil {
		t.Fatal("invalid cross-reference accepted")
	}
}

func FuzzNotificationLedgerCodec(f *testing.F) {
	f.Add([]byte(`{"version":1}`))
	f.Add([]byte(`{"version":1,"version":2}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		l, err := Decode(bytes.NewReader(data))
		if err != nil {
			return
		}
		out, err := Encode(l)
		if err != nil {
			t.Fatal("accepted state cannot encode", err)
		}
		if !json.Valid(out) {
			t.Fatal("invalid encoded JSON")
		}
		if _, err := Decode(bytes.NewReader(out)); err != nil {
			t.Fatal("round trip failed", err)
		}
	})
}

func populatedLedger(t *testing.T) *notification.LedgerV1 {
	t.Helper()
	l := codecLedger(t)
	now := time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC)
	r := notification.RequestV1{ID: "42", Type: "promotion", Source: "dev", Destination: "test", URL: "https://github.com/example/repo/pull/42"}
	e := notification.EventV1{ID: notification.EventID(l.Repository, r.ID, "request-merged"), Kind: "request-merged", Repository: l.Repository, Graph: l.Graph, Request: r, OccurredAt: now, ObservedAt: now,
		Snapshot: notification.CommitSnapshot{SourceOID: l.AnchorOID, BaseOID: l.AnchorOID, CommitCount: 1, CommitCountKnown: true, Commits: []notification.CommitSummary{{SHA: l.AnchorOID, ShortSHA: l.AnchorOID[:7], Subject: "change"}}}}
	l, err := notification.AdmitEvent(l, l.PolicyRevision.ConfigOID, e)
	if err != nil {
		t.Fatal(err)
	}
	key := notification.DeliveryKey(e.ID, "ops", l.Destinations["ops"].Generation)
	l, err = notification.SaveMessage(l, l.PolicyRevision.ConfigOID, key, notification.RenderedMessageV1{Title: "Ready", Body: "Saved"})
	if err != nil {
		t.Fatal(err)
	}
	l, err = notification.Claim(l, l.PolicyRevision.ConfigOID, key, "attempt", now)
	if err != nil {
		t.Fatal(err)
	}
	l.KnownRequests[r.ID] = notification.LifecycleRequest{Repository: l.Repository, Graph: l.Graph, Request: r, State: notification.LifecycleMerged, CreatedAt: now, MergedAt: now,
		Origin: &notification.NotificationOriginV1{Version: 1, OperationID: "origin", Graph: l.Graph, ConfigOID: l.AnchorOID, SourceOID: l.AnchorOID, BaseOID: l.AnchorOID, ObservedAt: now, LogicalSource: "dev", LogicalTarget: "test"}}
	l.Scans["merge"] = notification.ScanProgress{Version: 1, From: now, Through: now, Complete: true}
	return l
}

func TestNotificationLedgerFullStateValidation(t *testing.T) {
	t.Parallel()
	baseline := populatedLedger(t)
	if _, err := Encode(baseline); err != nil {
		t.Fatal(err)
	}
	var key, eventID string
	for k := range baseline.Deliveries {
		key = k
	}
	for k := range baseline.Events {
		eventID = k
	}
	for _, tc := range []struct {
		name   string
		change func(*notification.LedgerV1)
	}{
		{"identity", func(l *notification.LedgerV1) { l.Repository.ID = "" }},
		{"repository name", func(l *notification.LedgerV1) { l.Repository.Name = "" }},
		{"event repository name", func(l *notification.LedgerV1) {
			e := l.Events[eventID]
			e.Repository.Name = ""
			l.Events[eventID] = e
		}},
		{"event request URL", func(l *notification.LedgerV1) {
			e := l.Events[eventID]
			e.Request.URL = ""
			l.Events[eventID] = e
		}},
		{"known repository name", func(l *notification.LedgerV1) {
			r := l.KnownRequests["42"]
			r.Repository.Name = ""
			l.KnownRequests["42"] = r
		}},
		{"known request URL", func(l *notification.LedgerV1) {
			r := l.KnownRequests["42"]
			r.Request.URL = ""
			l.KnownRequests["42"] = r
		}},
		{"provider", func(l *notification.LedgerV1) { l.Repository.Provider = "other" }},
		{"azure ids", func(l *notification.LedgerV1) { l.Repository.Provider = "azuredevops" }},
		{"nil map", func(l *notification.LedgerV1) { l.Destinations = nil }},
		{"bad destination", func(l *notification.LedgerV1) {
			d := l.Destinations["ops"]
			d.Generation = "bad"
			l.Destinations["ops"] = d
		}},
		{"subscription", func(l *notification.LedgerV1) {
			d := l.Destinations["ops"]
			d.Subscriptions["invalid"] = notification.Subscription{}
			l.Destinations["ops"] = d
		}},
		{"event key", func(l *notification.LedgerV1) { l.Events["wrong"] = l.Events[eventID] }},
		{"event fact", func(l *notification.LedgerV1) { e := l.Events[eventID]; e.Request.ID = "wrong"; l.Events[eventID] = e }},
		{"commit oid", func(l *notification.LedgerV1) {
			e := l.Events[eventID]
			e.Snapshot.SourceOID = "HEAD"
			l.Events[eventID] = e
		}},
		{"commit subject", func(l *notification.LedgerV1) {
			e := l.Events[eventID]
			e.Snapshot.Commits[0].Subject = "bad\nsubject"
			l.Events[eventID] = e
		}},
		{"unknown total", func(l *notification.LedgerV1) {
			e := l.Events[eventID]
			e.Snapshot.CommitCountKnown = false
			l.Events[eventID] = e
		}},
		{"unavailable with commits", func(l *notification.LedgerV1) {
			e := l.Events[eventID]
			e.Snapshot.CommitsUnavailable = true
			l.Events[eventID] = e
		}},
		{"bad status", func(l *notification.LedgerV1) { r := l.Deliveries[key]; r.Status = "untrusted"; l.Deliveries[key] = r }},
		{"bad code", func(l *notification.LedgerV1) { r := l.Deliveries[key]; r.Code = "secret"; l.Deliveries[key] = r }},
		{"false delivered", func(l *notification.LedgerV1) {
			r := l.Deliveries[key]
			r.Status = notification.StatusDelivered
			l.Deliveries[key] = r
		}},
		{"false retry", func(l *notification.LedgerV1) {
			r := l.Deliveries[key]
			r.Status = notification.StatusRetryable
			l.Deliveries[key] = r
		}},
		{"false skip", func(l *notification.LedgerV1) {
			r := l.Deliveries[key]
			r.Status = notification.StatusSkipped
			l.Deliveries[key] = r
		}},
		{"unclaimed", func(l *notification.LedgerV1) { r := l.Deliveries[key]; r.Lease.AttemptID = ""; l.Deliveries[key] = r }},
		{"invented attempt", func(l *notification.LedgerV1) {
			r := l.Deliveries[key]
			r.Lease.AttemptID = "invented"
			l.Deliveries[key] = r
		}},
		{"duplicate attempts", func(l *notification.LedgerV1) {
			r := l.Deliveries[key]
			r.Attempts++
			r.AttemptIDs = append(r.AttemptIDs, "attempt")
			l.Deliveries[key] = r
		}},
		{"bad message", func(l *notification.LedgerV1) {
			r := l.Deliveries[key]
			r.Message.Title = "bad\nmessage"
			l.Deliveries[key] = r
		}},
		{"request identity", func(l *notification.LedgerV1) {
			r := l.KnownRequests["42"]
			r.Graph = "wrong"
			l.KnownRequests["42"] = r
		}},
		{"request state", func(l *notification.LedgerV1) {
			r := l.KnownRequests["42"]
			r.State = "closed"
			l.KnownRequests["42"] = r
		}},
		{"missing merge time", func(l *notification.LedgerV1) {
			r := l.KnownRequests["42"]
			r.MergedAt = time.Time{}
			l.KnownRequests["42"] = r
		}},
		{"origin", func(l *notification.LedgerV1) {
			r := l.KnownRequests["42"]
			r.Origin.Version = 2
			l.KnownRequests["42"] = r
		}},
		{"scan", func(l *notification.LedgerV1) { l.Scans["merge"] = notification.ScanProgress{Version: 2} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := baseline.Clone()
			tc.change(l)
			if _, err := Encode(l); err == nil {
				t.Fatal("invalid state accepted")
			}
			// Bypass Encode's validation to exercise the persisted decode boundary.
			raw, err := json.Marshal(l)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Decode(bytes.NewReader(raw)); err == nil {
				t.Fatal("invalid persisted state accepted")
			}
		})
	}
	for _, result := range []notification.AttemptResult{{Code: notification.OutcomeAccepted}, {Code: notification.OutcomeNetwork}} {
		l, err := notification.RecordResult(baseline, key, "attempt", result, time.Date(2026, 9, 4, 18, 1, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		data, err := Encode(l)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Decode(bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNotificationLedgerJSONBoundaries(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`{"x":1,"\u0078":2}`, `{"a":{"b":1,"b":2}}`, `[1,]`, `{"x":`, `[] {}`, strings.Repeat("[", 66) + strings.Repeat("]", 66)} {
		if uniqueJSON([]byte(raw)) {
			t.Fatalf("invalid/duplicate JSON accepted: %s", raw)
		}
	}
	if !uniqueJSON([]byte(`{"x":[1,true,null,{"y":"z"}]}`)) {
		t.Fatal("valid JSON rejected")
	}
	if _, err := Encode(nil); err == nil {
		t.Fatal("nil ledger accepted")
	}
	l := codecLedger(t)
	l.Repository.Provider = "azuredevops"
	l.Repository.OrganizationID = "org"
	l.Repository.ProjectID = "project"
	if _, err := Encode(l); err != nil {
		t.Fatal("Azure identity rejected", err)
	}
}
