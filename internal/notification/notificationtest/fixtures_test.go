package notificationtest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/v2/internal/notification"
)

func TestFixturesOwnSnapshots(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC)
	clock := NewClock(now)
	clock.Advance(time.Second)
	if !clock.Now().Equal(now.Add(time.Second)) {
		t.Fatal("clock did not advance")
	}
	store := &MemoryStore{}
	ctx := context.Background()
	if _, err := store.Read(ctx); !errors.Is(err, notification.ErrAbsent) {
		t.Fatal(err)
	}
	l := notification.NewLedger(notification.RepositoryIdentity{Provider: "github", Host: "github.com", ID: "1"}, "graph", strings.Repeat("a", 40))
	snap, err := store.Commit(ctx, "", func(context.Context, *notification.LedgerV1) (*notification.LedgerV1, error) { return l, nil })
	if err != nil {
		t.Fatal(err)
	}
	l.Graph = "changed"
	snap.Ledger.Graph = "also changed"
	read, err := store.Read(ctx)
	if err != nil || read.Ledger.Graph != "graph" {
		t.Fatal("aliased ledger", err)
	}
	if _, err := store.Commit(ctx, "stale", nil); !errors.Is(err, notification.ErrConflict) {
		t.Fatal(err)
	}
	r := &Recorder{Result: notification.AttemptResult{Code: notification.OutcomeAccepted}}
	p := notification.DeliveryPayloadV1{Event: notification.EventV1{Snapshot: notification.CommitSnapshot{Commits: []notification.CommitSummary{{Subject: "original"}}}}}
	if r.Send(ctx, "not recorded", p).Code != notification.OutcomeAccepted {
		t.Fatal("result lost")
	}
	p.Event.Snapshot.Commits[0].Subject = "changed"
	copy := r.Payloads()
	if copy[0].Event.Snapshot.Commits[0].Subject != "original" {
		t.Fatal("aliased send")
	}
	copy[0].Event.Snapshot.Commits[0].Subject = "changed again"
	if r.Payloads()[0].Event.Snapshot.Commits[0].Subject != "original" {
		t.Fatal("aliased recorder result")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if r.Send(canceled, "not recorded", p).Code != notification.OutcomeCanceled {
		t.Fatal("cancellation lost")
	}
}
