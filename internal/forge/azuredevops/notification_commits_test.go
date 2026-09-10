package azuredevops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/v2/internal/forge"
	"github.com/skaphos/oiax/v2/internal/forge/forgetest"
	mk "github.com/skaphos/oiax/v2/internal/forge/marker"
	"github.com/skaphos/oiax/v2/internal/notification"
)

func TestNotificationSnapshotConformance(t *testing.T) {
	forgetest.RunNotificationSnapshots(t, func(t *testing.T, c forgetest.SnapshotCase) (forge.SnapshotReader, notification.LifecycleRequest) {
		pr := notificationAzurePull(42, time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
		pr.Status, pr.ClosedDate = "completed", "2026-09-05T12:01:00Z"
		pr.LastMergeSourceCommit.CommitID, pr.LastMergeTargetCommit.CommitID, pr.LastMergeCommit.CommitID = strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
		if c.Mode == "tampered-origin" {
			// The description is editable text; first-iteration evidence is not.
			origin := notification.NotificationOriginV1{Version: 1, OperationID: "create-operation", Graph: "graph", ConfigOID: strings.Repeat("d", 40), ObservedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), LogicalSource: "dev", LogicalTarget: "test", SourceOID: forgetest.TamperedSourceOID, BaseOID: forgetest.TamperedBaseOID}
			var err error
			if pr.Description, err = mk.AppendNotificationOrigin(pr.Description, &origin); err != nil {
				t.Fatal(err)
			}
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Error("snapshot mutation")
				w.WriteHeader(405)
				return
			}
			if serveNotificationAzureIdentity(w, r.URL.Path) {
				return
			}
			switch {
			case strings.HasSuffix(r.URL.Path, "/properties"):
				_ = json.NewEncoder(w).Encode(propertiesCollection{})
			case strings.HasSuffix(r.URL.Path, "/pullrequests/42"):
				_ = json.NewEncoder(w).Encode(pr)
			case strings.HasSuffix(r.URL.Path, "/iterations/1"):
				if c.Mode == "unavailable" {
					w.WriteHeader(403)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "sourceRefCommit": pr.LastMergeSourceCommit, "targetRefCommit": pr.LastMergeTargetCommit})
			case strings.HasSuffix(r.URL.Path, "/commits"):
				if c.Mode == "unavailable" {
					w.WriteHeader(403)
					return
				}
				q := r.URL.Query()
				if q.Get("searchCriteria.itemVersion.version") == forgetest.TamperedSourceOID || q.Get("searchCriteria.compareVersion.version") == forgetest.TamperedBaseOID {
					// The range the tampered description claims, served as real
					// history so trusting it would surface it verbatim.
					var forged []adoCommitRef
					for i := range forgetest.TamperedCount {
						forged = append(forged, adoCommitRef{CommitID: forgetest.TamperedCommitOID(i), Comment: "forged"})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"count": len(forged), "value": forged})
					return
				}
				if q.Get("searchCriteria.itemVersion.version") != strings.Repeat("a", 40) || q.Get("searchCriteria.compareVersion.version") != strings.Repeat("b", 40) || q.Get("searchCriteria.itemVersion.versionType") != "commit" || q.Get("searchCriteria.compareVersion.versionType") != "commit" {
					t.Error("moving history substituted for immutable OIDs")
				}
				var commits []adoCommitRef
				for i := range c.Count {
					commits = append(commits, adoCommitRef{CommitID: fmt.Sprintf("%040x", i+1), Comment: strings.Repeat("é", 201) + "\x1b\u202e\nbody"})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"count": len(commits), "value": commits})
			default:
				t.Errorf("unexpected branch/history lookup: %s", r.URL.Path)
				w.WriteHeader(404)
			}
		}))
		t.Cleanup(server.Close)
		p := &Provider{Repo: Repo{Organization: "org", Project: "project", Name: "repo"}, BaseURL: server.URL, HTTP: server.Client()}
		req, err := p.GetLifecycleRequest(context.Background(), "42")
		if err != nil {
			t.Fatal(err)
		}
		return p, req
	})
}
