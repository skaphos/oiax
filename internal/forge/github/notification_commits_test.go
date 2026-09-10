package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/skaphos/oiax/v2/internal/engine"
	"github.com/skaphos/oiax/v2/internal/forge"
	"github.com/skaphos/oiax/v2/internal/forge/forgetest"
	mk "github.com/skaphos/oiax/v2/internal/forge/marker"
	"github.com/skaphos/oiax/v2/internal/notification"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

// testNotificationOrigin matches notificationPull: the request was opened at
// head "a"*40 over base "b"*40 on the dev -> test promotion edge.
func testNotificationOrigin() notification.NotificationOriginV1 {
	return notification.NotificationOriginV1{Version: 1, OperationID: "create-operation", Graph: "graph", ConfigOID: strings.Repeat("d", 40), ObservedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), LogicalSource: "dev", LogicalTarget: "test", SourceOID: strings.Repeat("a", 40), BaseOID: strings.Repeat("b", 40)}
}

func fixtureCommits(count int, message string) []notificationCommit {
	var commits []notificationCommit
	for i := range min(count, notification.MaxCommits) {
		var commit notificationCommit
		commit.SHA = fmt.Sprintf("%040x", i+1)
		commit.Commit.Message = message
		commits = append(commits, commit)
	}
	return commits
}

// forgedCommits is the history a tampered origin block claims. Its SHAs are
// recognisable so any leak into a snapshot is unambiguous.
func forgedCommits(count int) []notificationCommit {
	var commits []notificationCommit
	for i := range min(count, notification.MaxCommits) {
		var commit notificationCommit
		commit.SHA = forgetest.TamperedCommitOID(i)
		commit.Commit.Message = "forged"
		commits = append(commits, commit)
	}
	return commits
}

// unsafeSubject exceeds the subject bound and carries controls a summary must
// never surface; bounding it also reports truncation.
var unsafeSubject = strings.Repeat("é", 201) + "\x1b\u202e\nbody"

func TestNotificationSnapshotConformance(t *testing.T) {
	forgetest.RunNotificationSnapshots(t, func(t *testing.T, c forgetest.SnapshotCase) (forge.SnapshotReader, notification.LifecycleRequest) {
		pr := notificationPull(42, time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
		merged := "2026-09-05T12:01:00Z"
		pr.State, pr.MergedAt, pr.MergeCommitSHA, pr.Commits = "closed", &merged, strings.Repeat("c", 40), &c.Count
		// Creation evidence: the request still stands at the head Oiax opened it
		// at. The tampered case rewrites the block to claim an unrelated range.
		origin := testNotificationOrigin()
		if c.Mode == "tampered-origin" {
			origin.SourceOID, origin.BaseOID = forgetest.TamperedSourceOID, forgetest.TamperedBaseOID
		}
		var err error
		if pr.Body, err = mk.AppendNotificationOrigin(pr.Body, &origin); err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Error("snapshot mutation")
				w.WriteHeader(405)
				return
			}
			if r.URL.Query().Has("per_page") && r.URL.Query().Get("per_page") != "100" {
				t.Error("unbounded read")
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
				if c.Count > notification.MaxCommits {
					w.Header().Set("Link", `<https://api.github.com/repos/example/repo/pulls/42/commits?per_page=100&page=2>; rel="next"`)
				}
				_ = json.NewEncoder(w).Encode(fixtureCommits(c.Count, unsafeSubject))
			case "/repos/example/repo/compare/" + origin.BaseOID + "..." + origin.SourceOID:
				if c.Mode == "unavailable" {
					w.WriteHeader(403)
					return
				}
				// In the tampered case this is the range the edited block claims,
				// served as real history: trusting it would surface it verbatim.
				count, commits := c.Count, fixtureCommits(c.Count, unsafeSubject)
				if c.Mode == "tampered-origin" {
					count, commits = forgetest.TamperedCount, forgedCommits(forgetest.TamperedCount)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"total_commits": count, "commits": commits})
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

func TestNotificationCreationSnapshot(t *testing.T) {
	t.Parallel()
	honest := testNotificationOrigin()
	invalid := honest
	invalid.SourceOID = "not-an-oid"
	other := honest
	other.OperationID = "another-operation"
	// The two ways an editor can rewrite the block: claim a different base for
	// the range, or claim a head the request never had.
	tamperedBase := honest
	tamperedBase.BaseOID = forgetest.TamperedBaseOID
	tamperedHead := honest
	tamperedHead.SourceOID = forgetest.TamperedSourceOID
	cases := []struct {
		name string
		// origin is the ledger's recorded evidence; recorded is what the request
		// body still carries. Both must agree for membership to be computed.
		origin, recorded      *notification.NotificationOriginV1
		served                int
		oversize, truncated   bool
		detailStatus, listErr int
		wantErr               error
		wantUnavailable       bool
		wantKnown             bool
		wantCount             int
		wantTruncated         bool
	}{
		{name: "membership comes from the request's own commits", origin: &honest, recorded: &honest, served: 2, wantKnown: true, wantCount: 2},
		{name: "a truncated page reports no total", origin: &honest, recorded: &honest, served: 100, truncated: true, wantTruncated: true},
		// A rewritten base must be inert: the emitted membership, its total and
		// its reported range stay exactly those of the untampered case above.
		{name: "a tampered base is inert", origin: &tamperedBase, recorded: &tamperedBase, served: 2, wantKnown: true, wantCount: 2},
		{name: "a tampered head is not attested", origin: &tamperedHead, recorded: &tamperedHead, served: 2, wantUnavailable: true},
		{name: "missing origin is unavailable", origin: nil, recorded: &honest, served: 2, wantUnavailable: true},
		{name: "invalid origin is unavailable", origin: &invalid, recorded: &honest, served: 2, wantUnavailable: true},
		{name: "origin no longer on the request", origin: &honest, recorded: nil, served: 2, wantErr: notification.ErrLifecycleUnavailable, wantUnavailable: true},
		{name: "origin differs from the request", origin: &honest, recorded: &other, served: 2, wantErr: notification.ErrLifecycleUnavailable, wantUnavailable: true},
		{name: "request detail failure", origin: &honest, recorded: &honest, detailStatus: 404, wantErr: notification.ErrLifecycleUnavailable, wantUnavailable: true},
		{name: "commit listing failure", origin: &honest, recorded: &honest, listErr: 403, wantErr: notification.ErrLifecycleUnavailable, wantUnavailable: true},
		{name: "commit page beyond its own bound", origin: &honest, recorded: &honest, oversize: true, wantErr: notification.ErrLifecycleUnavailable, wantUnavailable: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pr := notificationPull(42, time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
			if tc.recorded != nil {
				var err error
				if pr.Body, err = mk.AppendNotificationOrigin(pr.Body, tc.recorded); err != nil {
					t.Fatal(err)
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Error("snapshot mutation")
					w.WriteHeader(405)
					return
				}
				switch {
				case r.URL.Path == "/repos/example/repo":
					serveNotificationIdentity(w)
				case r.URL.Path == "/repos/example/repo/pulls/42":
					if tc.detailStatus != 0 {
						w.WriteHeader(tc.detailStatus)
						return
					}
					_ = json.NewEncoder(w).Encode(pr)
				case r.URL.Path == "/repos/example/repo/pulls/42/commits":
					if r.URL.Query().Get("per_page") != "100" {
						t.Error("unbounded read")
					}
					if tc.listErr != 0 {
						w.WriteHeader(tc.listErr)
						return
					}
					commits := fixtureCommits(tc.served, "subject\x1b\nbody")
					if tc.oversize {
						commits = append(fixtureCommits(notification.MaxCommits, "subject"), notificationCommit{SHA: strings.Repeat("9", 40)})
					}
					if tc.truncated {
						w.Header().Set("Link", `<https://api.github.com/repos/example/repo/pulls/42/commits?per_page=100&page=2>; rel="next"`)
					}
					_ = json.NewEncoder(w).Encode(commits)
				case strings.HasPrefix(r.URL.Path, "/repos/example/repo/compare/"):
					// Reachable only by resolving OIDs the request body supplied.
					t.Errorf("resolved a range taken from request text: %s", r.URL.Path)
					w.WriteHeader(404)
				default:
					t.Errorf("unexpected branch/history lookup: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			t.Cleanup(server.Close)
			p := &Provider{Owner: "example", Repo: "repo", BaseURL: server.URL, HTTP: server.Client()}
			repo := notification.RepositoryIdentity{Provider: "github", Host: gitRemoteHost, ID: "123", Name: "example/repo"}
			req := notification.LifecycleRequest{Repository: repo, Graph: "graph", State: notification.LifecycleOpen, CreatedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), SourceOID: pr.Head.SHA, BaseOID: pr.Base.SHA, Origin: tc.origin,
				Request: notification.RequestV1{ID: "42", Type: v1.NotificationPromotion, Source: "dev", Destination: "test", LogicalSource: "dev", LogicalDestination: "test", URL: "https://github.com/example/repo/pull/42"}}
			got, err := p.GetCommitSnapshot(context.Background(), req, forge.EventRevision{Kind: v1.NotificationRequestCreated, SourceOID: req.SourceOID, BaseOID: req.BaseOID})
			if !errors.Is(err, tc.wantErr) || got.CommitsUnavailable != tc.wantUnavailable {
				t.Fatalf("snapshot = %+v, %v", got, err)
			}
			if tc.wantUnavailable {
				if len(got.Commits) != 0 || got.CommitCountKnown || got.CommitCount != 0 {
					t.Fatalf("unavailable snapshot asserted history: %+v", got)
				}
				return
			}
			// The forge's own head and base, never the block: a rewritten base is
			// not reported and does not change a single listed commit.
			if got.SourceOID != pr.Head.SHA || got.BaseOID != pr.Base.SHA || got.MergeResultOID != "" || len(got.Commits) != tc.served || got.CommitCountKnown != tc.wantKnown || got.CommitCount != tc.wantCount || got.CommitsTruncated != tc.wantTruncated {
				t.Fatalf("creation membership = %+v", got)
			}
			for i, commit := range got.Commits {
				if commit.SHA != fmt.Sprintf("%040x", i+1) || commit.ShortSHA != commit.SHA[:7] || commit.Subject != "subject " {
					t.Fatalf("unsafe or reordered summary: %+v", commit)
				}
			}
		})
	}
}

// TestCreateRequestWritesOriginOnce pins the creating POST as the only write of
// provenance. Nothing a later run must trust is established here, so creation
// neither reads the request back nor rewrites its body: a request's text can
// never carry a verdict, and creation costs one POST plus its labels.
func TestCreateRequestWritesOriginOnce(t *testing.T) {
	t.Parallel()
	origin := testNotificationOrigin()
	var mu sync.Mutex
	var posted string
	reads, writes := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var payload map[string]any
		if r.Method != http.MethodGet {
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/example/repo/pulls":
			posted, _ = payload["body"].(string)
			// The live source has already advanced past the recorded head.
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 42, "state": "open", "body": posted, "created_at": "2026-09-05T12:00:01Z",
				"head": map[string]any{"ref": "dev", "sha": strings.Repeat("f", 40), "repo": map[string]string{"full_name": "example/repo"}},
				"base": map[string]any{"ref": "test", "sha": strings.Repeat("e", 40), "repo": map[string]string{"full_name": "example/repo"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/example/repo/issues/42/labels":
			_ = json.NewEncoder(w).Encode([]any{})
		case r.URL.Path == "/repos/example/repo/pulls/42":
			if r.Method == http.MethodGet {
				reads++
			} else {
				writes++
			}
			w.WriteHeader(500)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	p := &Provider{Owner: "example", Repo: "repo", BaseURL: server.URL, HTTP: server.Client()}
	out, err := p.CreateRequest(context.Background(), forge.CreateRequest{Graph: "graph", Type: engine.RequestTypePromotion, Source: "dev", Target: "test", SourceHead: origin.SourceOID, Body: "Human text", Origin: &origin})
	if err != nil || out.Disposition != forge.RequestCreated || out.Request.ID != "42" || out.Origin == nil || *out.Origin != origin {
		t.Fatalf("creation = %+v, %v", out, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if reads != 0 || writes != 0 {
		t.Fatalf("creation re-read (%d) or rewrote (%d) the request it just opened", reads, writes)
	}
	recorded, ok := mk.ParseNotificationOrigin(posted)
	m, owned := parseMarker(posted)
	if !ok || recorded != origin || !owned || m.SourceHead != origin.SourceOID || !strings.HasPrefix(posted, "Human text\n\n") {
		t.Fatalf("posted provenance = %+v, %v:\n%s", recorded, ok, posted)
	}
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
