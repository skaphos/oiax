package forgetest_test

import (
	"encoding/json"
	"testing"

	"github.com/skaphos/oiax/v2/internal/config"
	"github.com/skaphos/oiax/v2/internal/forge/marker"
	"github.com/skaphos/oiax/v2/internal/notification"
)

func FuzzNotificationPayload(f *testing.F) {
	for _, s := range []string{`{"request":{"id":"42"},"state":"merged"}`, `{"sourceOID":"main","commits":[]}`, "<!-- oiax-notification-origin: {} -->", "apiVersion: oiax.skaphos.dev/v1\nkind: PromotionGraph\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 16384 {
			t.Skip()
		}
		_, _ = marker.ParseNotificationOrigin(raw)
		_, _ = config.Parse([]byte(raw))
		var snapshot notification.CommitSnapshot
		if json.Unmarshal([]byte(raw), &snapshot) == nil {
			bounded := notification.BoundSnapshot(snapshot)
			if len(bounded.Commits) > notification.MaxCommits {
				t.Fatal("unbounded snapshot")
			}
		}
	})
}
