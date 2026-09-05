package forgetest

import (
	"context"
	"testing"
	"time"

	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/notification"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

// LifecycleSeed describes fixture-owned remote state, not mutations Oiax may
// perform. Tests represent human merges by choosing authoritative server fields.
type LifecycleSeed struct {
	ID                  int
	Graph               string
	Type                v1.NotificationRequestType
	Source, Destination string
	State               notification.LifecycleState
	CreatedAt, MergedAt time.Time
	Managed             bool
	Fork                bool
}

func RunLifecycle(t *testing.T, factory func(*testing.T, []LifecycleSeed) forge.Forge) {
	t.Helper()
	t.Run("ManagedLifecycleFacts", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC)
		seed := func(id int, state notification.LifecycleState) LifecycleSeed {
			return LifecycleSeed{ID: id, Graph: "graph", Type: "promotion", Source: "dev", Destination: "test", State: state, CreatedAt: now.Add(-365 * 24 * time.Hour), MergedAt: now.Add(-time.Minute), Managed: true}
		}
		seeds := []LifecycleSeed{seed(1, notification.LifecycleOpen), seed(2, notification.LifecycleMerged), seed(3, notification.LifecycleClosed), seed(4, notification.LifecycleMerged), seed(5, notification.LifecycleMerged), seed(6, notification.LifecycleMerged), seed(7, notification.LifecycleMerged)}
		seeds[3].Managed = false
		seeds[4].Fork = true
		seeds[5].Graph = "other"
		seeds[6].Type = "backflow"
		seeds[6].Source = "oiax/backflow/removed/abcdef0"
		seeds[6].Destination = "dev"
		reader, ok := factory(t, seeds).(forge.LifecycleReader)
		if !ok {
			t.Fatal("provider lacks optional lifecycle capability")
		}
		identity, err := reader.RepositoryIdentity(context.Background())
		if err != nil || identity.ID == "" || identity.Host == "" {
			t.Fatal("immutable identity missing", err)
		}
		q := forge.LifecycleQuery{Graph: "graph", Through: now, Limit: 100}
		found := map[string]notification.LifecycleRequest{}
		complete := false
		for range 100 {
			page, err := reader.ListLifecyclePage(context.Background(), q)
			if err != nil {
				t.Fatal(err)
			}
			for _, req := range page.Requests {
				found[req.Request.ID] = req
			}
			if page.Progress.Complete {
				complete = true
				break
			}
			q.Cursor = page.Progress.Cursor
		}
		if !complete {
			t.Fatal("small stable scan never completed")
		}
		if len(found) != 4 {
			t.Fatalf("wrong ownership/graph filtering: %+v", found)
		}
		if found["2"].State != notification.LifecycleMerged || !found["2"].MergedAt.Equal(now.Add(-time.Minute)) {
			t.Fatal("old PR's new merge lost")
		}
		if found["3"].State != notification.LifecycleClosed {
			t.Fatal("closure misreported as merge")
		}
		if found["7"].Request.Type != "backflow" || found["7"].Request.LogicalSource != "" {
			t.Fatal("legacy backflow logical edge fabricated")
		}
		for _, id := range []forge.RequestID{"1", "2", "3", "7"} {
			req, err := reader.GetLifecycleRequest(context.Background(), id)
			if err != nil || req.Request.ID != string(id) {
				t.Fatal("known request direct refresh failed", err)
			}
		}
		if _, err := reader.GetLifecycleRequest(context.Background(), "999"); err == nil {
			t.Fatal("missing detail silently became a request")
		}
		if _, err := reader.GetLifecycleRequest(context.Background(), "4"); err == nil {
			t.Fatal("unmanaged detail accepted")
		}
	})
}
