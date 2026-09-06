package cli

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	mk "github.com/skaphos/oiax/v2/internal/forge/marker"
	"github.com/skaphos/oiax/v2/internal/gittest"
	"github.com/skaphos/oiax/v2/internal/notification"
)

func TestNotificationCreationBinary(t *testing.T) {
	t.Parallel()
	binary := buildNotificationBinary(t)
	for _, provider := range []string{"github", "azuredevops"} {
		for _, mode := range []string{"created", "metadata-failure", "recovery", "post-failure"} {
			t.Run(provider+"/"+mode, func(t *testing.T) {
				t.Parallel()
				f := newNotificationBinaryFixture(t, binary, provider, "webhook",
					"      - {name: creation, type: webhook, endpointEnv: OIAX_FIXTURE_AUDIT, events: [request-created]}\n")
				// Do not initialize notifications first: this invocation's own
				// creates must follow the newly persisted activation cutoff.
				gittest.Run(t, f.dir, "checkout", "-q", "dev")
				writeNotificationFixture(t, filepath.Join(f.dir, "app.txt"), []byte("feature\n"))
				gittest.Run(t, f.dir, "add", "app.txt")
				gittest.Run(t, f.dir, "commit", "-qm", "new feature")
				f.mu.Lock()
				f.failTarget = "stage" // actual partial progress on the first edge
				f.failMetadata = mode == "metadata-failure" || mode == "recovery"
				f.failDetails = mode == "recovery"
				f.failDiscovery = true // outcome capture is independent of discovery
				if mode == "post-failure" {
					f.failTarget = "test"
				}
				f.mu.Unlock()
				f.run(1, "reconcile")
				want := 1
				if mode == "recovery" || mode == "post-failure" {
					want = 0
				}
				if len(f.messages()) != want {
					t.Fatalf("got %d created messages, want %d", len(f.messages()), want)
				}
				if mode == "post-failure" {
					if len(f.ledger().Deliveries) != 0 {
						t.Fatal("failed POST invented a creation")
					}
					return
				}
				f.mu.Lock()
				initialBody := f.bodies[101]
				origin, ok := mk.ParseNotificationOrigin(initialBody)
				f.failMetadata, f.failDetails, f.failDiscovery = false, false, false
				f.mu.Unlock()
				if !ok || origin.ConfigOID != f.oid || origin.LogicalSource != "dev" || origin.LogicalTarget != "test" {
					t.Fatal("initial POST lacks original evidence")
				}
				// A new process recovers the original create, even when none of
				// the first run's notification reads could confirm it.
				f.run(1, "reconcile")
				if len(f.messages()) != 1 {
					t.Fatal("recovery lost or duplicated the creation message")
				}
				accepted := f.ledger()
				for _, d := range accepted.Deliveries {
					if d.Destination != "creation" || d.Status != notification.StatusDelivered {
						t.Fatalf("wrong creation routing: %+v", d)
					}
				}
				// Advancing the source changes the baseline marker, not origin or
				// the original event/receipt, and never produces another creation.
				writeNotificationFixture(t, filepath.Join(f.dir, "app.txt"), []byte("feature v2\n"))
				gittest.Run(t, f.dir, "add", "app.txt")
				gittest.Run(t, f.dir, "commit", "-qm", "source advancement")
				f.run(1, "reconcile")
				f.mu.Lock()
				afterOrigin, valid := mk.ParseNotificationOrigin(f.bodies[101])
				f.mu.Unlock()
				if !valid || afterOrigin != origin || len(f.messages()) != 1 || !reflect.DeepEqual(accepted.Deliveries, f.ledger().Deliveries) {
					t.Fatal("baseline update changed creation provenance or receipts")
				}
				// Fixture-owned merge after the original create is a second event.
				f.mu.Lock()
				f.seeds[0].State = notification.LifecycleMerged
				f.seeds[0].MergedAt = time.Now().UTC()
				f.mu.Unlock()
				// Sync both targets so no new core request is needed after merge.
				head := gittest.Run(t, f.dir, "rev-parse", "dev")
				gittest.Run(t, f.dir, "update-ref", "refs/heads/test", head)
				gittest.Run(t, f.dir, "update-ref", "refs/heads/stage", head)
				f.run(0, "reconcile")
				if len(f.messages()) != 2 {
					t.Fatal("creation and merge did not remain separate events")
				}
				if !strings.Contains(string(f.messages()[0].Body), "request-created") || !strings.Contains(string(f.messages()[1].Body), "request-merged") {
					t.Fatal("incorrect lifecycle wording")
				}
			})
		}
	}
}
