package github

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
	"github.com/skaphos/oiax/v2/internal/notification"
)

func TestNotificationSnapshotConformance(t *testing.T) {
	forgetest.RunNotificationSnapshots(t, func(t *testing.T, c forgetest.SnapshotCase) (forge.SnapshotReader, notification.LifecycleRequest) {
		pr := notificationPull(42, time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
		merged := "2026-09-05T12:01:00Z"
		pr.State, pr.MergedAt, pr.MergeCommitSHA, pr.Commits = "closed", &merged, strings.Repeat("c", 40), &c.Count
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Error("snapshot mutation")
				w.WriteHeader(405)
				return
			}
			switch r.URL.Path {
			case "/repos/example/repo":
				serveNotificationIdentity(w)
			case "/repos/example/repo/pulls/42":
				_ = json.NewEncoder(w).Encode(pr)
			case "/repos/example/repo/pulls/42/commits":
				if c.Mode == "unavailable" {
					w.WriteHeader(403)
					return
				}
				if r.URL.Query().Get("per_page") != "100" {
					t.Error("unbounded read")
				}
				var commits []notificationCommit
				for i := range min(c.Count, 100) {
					var commit notificationCommit
					commit.SHA = fmt.Sprintf("%040x", i+1)
					commit.Commit.Message = strings.Repeat("é", 201) + "\x1b\u202e\nbody"
					commits = append(commits, commit)
				}
				_ = json.NewEncoder(w).Encode(commits)
			case "/repos/example/repo/commits/" + strings.Repeat("c", 40):
				w.WriteHeader(404)
			default:
				t.Errorf("unexpected branch/history lookup: %s", r.URL.Path)
				w.WriteHeader(404)
			}
		}))
		t.Cleanup(server.Close)
		p := &Provider{Owner: "example", Repo: "repo", BaseURL: server.URL, HTTP: server.Client()}
		req, err := p.GetLifecycleRequest(context.Background(), "42")
		if err != nil {
			t.Fatal(err)
		}
		return p, req
	})
}

func TestNotificationImmutableMergeFallback(t *testing.T) {
	base, head, result := strings.Repeat("b", 40), strings.Repeat("a", 40), strings.Repeat("c", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/example/repo/commits/" + result:
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": result, "parents": []map[string]string{{"sha": base}, {"sha": head}}})
		case "/repos/example/repo/compare/" + base + "..." + head:
			_ = json.NewEncoder(w).Encode(map[string]any{"total_commits": 1, "commits": []map[string]any{{"sha": head, "commit": map[string]string{"message": "reviewed source"}}}})
		default:
			t.Error("moving ref fallback")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	p := &Provider{Owner: "example", Repo: "repo", BaseURL: server.URL, HTTP: server.Client()}
	snapshot, err := p.immutableMergeSnapshot(context.Background(), notification.LifecycleRequest{SourceOID: head, MergeResultOID: result}, 1)
	if err != nil || snapshot.CommitsUnavailable || snapshot.BaseOID != base || snapshot.Commits[0].SHA != head {
		t.Fatal("immutable merge proof lost", err)
	}
}
