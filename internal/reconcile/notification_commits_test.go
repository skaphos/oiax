package reconcile

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/v2/internal/notification"
	"github.com/skaphos/oiax/v2/internal/notification/notificationtest"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

func TestNotificationSnapshotFactsFixedAtAdmission(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	store := &notificationtest.MemoryStore{}
	policy := &v1.NotificationPolicy{EnvironmentNames: map[string]string{"test": "Test environment"}, Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "ENDPOINT"}}}
	r := mergeRuntime(func() time.Time { return now }, store, policy)
	r.Topology = observationGraph()
	req := lifecycleMerge(r.Repository, "42", now.Add(-time.Hour), now)
	details := notification.CommitSnapshot{SourceOID: strings.Repeat("a", 40), BaseOID: strings.Repeat("b", 40), MergeResultOID: strings.Repeat("c", 40), Commits: []notification.CommitSummary{{SHA: strings.Repeat("a", 40), Subject: "original subject"}}, CommitCountKnown: true, CommitCount: 1}
	reader := &notificationtest.Lifecycle{Snapshots: map[string]notification.CommitSnapshot{"42/request-merged": details}}
	r.Reader = reader
	if err := r.Activate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.recordObservation(ctx, []notification.LifecycleRequest{req}, "", notification.ScanProgress{}, now); err != nil {
		t.Fatal(err)
	}
	first, err := store.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := notification.EventID(r.Repository, "42", v1.NotificationRequestMerged)
	if first.Ledger.Events[id].Snapshot.CommitsUnavailable || first.Ledger.Events[id].DestinationEnvironment != "Test environment" {
		t.Fatal("facts not enriched")
	}
	reader.Snapshots = nil
	reader.Error = notification.ErrLifecycleUnavailable
	policy.EnvironmentNames["test"] = "changed label"
	if err := r.recordObservation(ctx, []notification.LifecycleRequest{req}, "", notification.ScanProgress{}, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	second, err := store.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Ledger.Events[id], second.Ledger.Events[id]) {
		t.Fatal("retry replaced original event facts")
	}
	req.Request.ID = "43"
	req.Request.URL = "https://github.com/example/repo/pull/43"
	if err := r.recordObservation(ctx, []notification.LifecycleRequest{req}, "", notification.ScanProgress{}, now); err != nil {
		t.Fatal(err)
	}
	third, err := store.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(third.Ledger.Events) != 2 || !third.Ledger.Events[notification.EventID(r.Repository, "43", v1.NotificationRequestMerged)].Snapshot.CommitsUnavailable {
		t.Fatal("enrichment failure lost lifecycle event")
	}
}
