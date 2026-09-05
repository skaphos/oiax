package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/internal/gittest"
	"github.com/skaphos/oiax/internal/notification"
	"github.com/skaphos/oiax/internal/reconcile"
)

func TestNotificationFailuresBinary(t *testing.T) {
	t.Parallel()
	binary := buildNotificationBinary(t)
	for _, provider := range []string{"github", "azuredevops"} {
		for _, mode := range []string{"missing-secret", "corrupt-ledger", "unavailable-notes", "incomplete-scan", "lost-ledger", "render-overflow"} {
			t.Run(provider+"/"+mode, func(t *testing.T) {
				t.Parallel()
				extra := []string{"      - {name: audit, type: webhook, endpointEnv: OIAX_FIXTURE_AUDIT, templates: {body: safe}}"}
				if mode == "render-overflow" {
					extra = append(extra, "    templates:", "      body: '{{if eq .RequestID \"43\"}}"+strings.Repeat("x", (12<<10)+1)+"{{end}}'")
				}
				f := newNotificationBinaryFixture(t, binary, provider, "webhook", extra...)
				f.run(0, "reconcile")
				f.setSeeds(f.seed(42, "promotion", "merged", time.Now().UTC(), true))
				if mode == "render-overflow" {
					f.setSeeds(f.seed(43, "promotion", "merged", time.Now().UTC(), true))
				}
				ref := "refs/notes/oiax/notifications/v1/" + notification.GraphKey(f.identity(), "graph")
				actualRemote := f.remote
				switch mode {
				case "missing-secret":
					f.missingEndpoint = true
				case "incomplete-scan":
					f.mu.Lock()
					f.failDiscovery = true
					f.mu.Unlock()
				case "unavailable-notes":
					f.remote = filepath.Join(t.TempDir(), "missing.git")
				case "lost-ledger":
					gittest.Run(t, f.remote, "update-ref", "-d", ref)
				case "corrupt-ledger":
					gittest.Run(t, f.remote, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "notes", "--ref", ref, "add", "-f", "-m", `{"version":999,"credential":"redaction-canary"}`, f.oid)
				}
				before := gittest.Run(t, actualRemote, "for-each-ref", "refs/notes/")
				out, _ := f.run(0, "plan", "--detailed-exitcode")
				var doc struct {
					Notifications reconcile.NotificationPreview `json:"notifications"`
				}
				if json.Unmarshal([]byte(out), &doc) != nil || doc.Notifications.Observation == "" {
					t.Fatal("missing preview")
				}
				if before != gittest.Run(t, actualRemote, "for-each-ref", "refs/notes/") || len(f.messages()) != 0 {
					t.Fatal("preview sent or wrote")
				}
				_, stderr := f.run(0, "reconcile")
				if strings.Contains(stderr, "redaction-canary") {
					t.Fatal("corrupt state escaped diagnostics")
				}
				if mode == "missing-secret" || mode == "render-overflow" {
					want := "missing-secret"
					if mode == "render-overflow" {
						want = "notification-template-invalid"
					}
					if !strings.Contains(stderr, want) || len(f.messages()) != 1 {
						t.Fatal("missing secret starved healthy receiver", stderr)
					}
				} else if len(f.messages()) != 0 {
					t.Fatal("unsafe or pre-cutoff send")
				}
			})
		}
	}
}
