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

// unsafeSubject exceeds the subject bound and carries controls a summary must
// never surface; bounding it also reports truncation.
var unsafeSubject = strings.Repeat("é", 201) + "\x1b\u202e\nbody"

func TestNotificationSnapshotConformance(t *testing.T) {
	forgetest.RunNotificationSnapshots(t, func(t *testing.T, c forgetest.SnapshotCase) (forge.SnapshotReader, notification.LifecycleRequest) {
		pr := notificationPull(42, time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
		merged := "2026-09-05T12:01:00Z"
		pr.State, pr.MergedAt, pr.MergeCommitSHA, pr.Commits = "closed", &merged, strings.Repeat("c", 40), &c.Count
		// Creation evidence: the creating run verified the head it opened at.
		origin := testNotificationOrigin()
		origin.HeadVerified = true
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
				_ = json.NewEncoder(w).Encode(fixtureCommits(c.Count, unsafeSubject))
			case "/repos/example/repo/compare/" + origin.BaseOID + "..." + origin.SourceOID:
				if c.Mode == "unavailable" {
					w.WriteHeader(403)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"total_commits": c.Count, "commits": fixtureCommits(c.Count, unsafeSubject)})
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
	unverified := testNotificationOrigin()
	verified := unverified
	verified.HeadVerified = true
	invalid := verified
	invalid.SourceOID = "not-an-oid"
	other := verified
	other.OperationID = "another-operation"
	cases := []struct {
		name string
		// origin is the ledger's recorded evidence; recorded is what the request
		// body still carries. Both must agree for membership to be computed.
		origin, recorded         *notification.NotificationOriginV1
		total, served            int
		detailStatus, compareErr int
		wantErr                  error
		wantUnavailable          bool
		wantKnown                bool
		wantCount                int
		wantTruncated            bool
	}{
		{name: "verified origin yields exact membership", origin: &verified, recorded: &verified, total: 2, served: 2, wantKnown: true, wantCount: 2},
		{name: "verified origin beyond one page is truncated", origin: &verified, recorded: &verified, total: 101, served: 100, wantKnown: true, wantCount: 101, wantTruncated: true},
		{name: "unverified origin lists commits with an unknown total", origin: &unverified, recorded: &unverified, total: 2, served: 2},
		{name: "unverified origin beyond one page is truncated", origin: &unverified, recorded: &unverified, total: 101, served: 100, wantTruncated: true},
		{name: "missing origin is unavailable", origin: nil, recorded: &verified, total: 2, served: 2, wantUnavailable: true},
		{name: "invalid origin is unavailable", origin: &invalid, recorded: &verified, total: 2, served: 2, wantUnavailable: true},
		{name: "origin no longer on the request", origin: &verified, recorded: nil, total: 2, served: 2, wantErr: notification.ErrLifecycleUnavailable, wantUnavailable: true},
		{name: "origin differs from the request", origin: &verified, recorded: &other, total: 2, served: 2, wantErr: notification.ErrLifecycleUnavailable, wantUnavailable: true},
		{name: "request detail failure", origin: &verified, recorded: &verified, detailStatus: 404, wantErr: notification.ErrLifecycleUnavailable, wantUnavailable: true},
		{name: "compare failure", origin: &verified, recorded: &verified, compareErr: 403, wantErr: notification.ErrLifecycleUnavailable, wantUnavailable: true},
		{name: "compare page inconsistent with its total", origin: &verified, recorded: &verified, total: 3, served: 2, wantErr: notification.ErrLifecycleUnavailable, wantUnavailable: true},
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
				switch r.URL.Path {
				case "/repos/example/repo":
					serveNotificationIdentity(w)
				case "/repos/example/repo/pulls/42":
					if tc.detailStatus != 0 {
						w.WriteHeader(tc.detailStatus)
						return
					}
					_ = json.NewEncoder(w).Encode(pr)
				case "/repos/example/repo/compare/" + verified.BaseOID + "..." + verified.SourceOID:
					if r.URL.Query().Get("per_page") != "100" {
						t.Error("unbounded read")
					}
					if tc.compareErr != 0 {
						w.WriteHeader(tc.compareErr)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"total_commits": tc.total, "commits": fixtureCommits(tc.served, "subject\x1b\nbody")})
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
			if got.SourceOID != verified.SourceOID || got.BaseOID != verified.BaseOID || got.MergeResultOID != "" || len(got.Commits) != tc.served || got.CommitCountKnown != tc.wantKnown || got.CommitCount != tc.wantCount || got.CommitsTruncated != tc.wantTruncated {
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

func TestCreateRequestVerifiesOriginHead(t *testing.T) {
	t.Parallel()
	origin := testNotificationOrigin()
	cases := []struct {
		name         string
		head         string
		readStatus   int
		patchStatus  int
		wantVerified bool
		wantPatches  int
	}{
		{name: "head still at origin is verified and persisted", head: origin.SourceOID, wantVerified: true, wantPatches: 1},
		{name: "head advanced before the POST stays unverified", head: strings.Repeat("f", 40)},
		{name: "read-back failure stays unverified", head: origin.SourceOID, readStatus: 404},
		{name: "persistence failure stays unverified", head: origin.SourceOID, patchStatus: 400, wantPatches: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			var posted, patched string
			patches := 0
			pull := func(body string) map[string]any {
				return map[string]any{"number": 42, "state": "open", "body": body, "created_at": "2026-09-05T12:00:01Z",
					"head": map[string]any{"ref": "dev", "sha": tc.head, "repo": map[string]string{"full_name": "example/repo"}},
					"base": map[string]any{"ref": "test", "sha": strings.Repeat("e", 40), "repo": map[string]string{"full_name": "example/repo"}}}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				var payload map[string]any
				if r.Method != http.MethodGet {
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
					}
				}
				body, _ := payload["body"].(string)
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/repos/example/repo/pulls":
					posted = body
					_ = json.NewEncoder(w).Encode(pull(posted))
				case r.Method == http.MethodGet && r.URL.Path == "/repos/example/repo/pulls/42":
					if tc.readStatus != 0 {
						w.WriteHeader(tc.readStatus)
						return
					}
					_ = json.NewEncoder(w).Encode(pull(posted))
				case r.Method == http.MethodPatch && r.URL.Path == "/repos/example/repo/pulls/42":
					patches++
					if tc.patchStatus != 0 {
						w.WriteHeader(tc.patchStatus)
						return
					}
					patched = body
					_ = json.NewEncoder(w).Encode(pull(patched))
				case r.Method == http.MethodPost && r.URL.Path == "/repos/example/repo/issues/42/labels":
					_ = json.NewEncoder(w).Encode([]any{})
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			t.Cleanup(server.Close)
			p := &Provider{Owner: "example", Repo: "repo", BaseURL: server.URL, HTTP: server.Client()}
			out, err := p.CreateRequest(context.Background(), forge.CreateRequest{Graph: "graph", Type: engine.RequestTypePromotion, Source: "dev", Target: "test", SourceHead: origin.SourceOID, Body: "Human text", Origin: &origin})
			if err != nil || out.Disposition != forge.RequestCreated || out.Request.ID != "42" || out.Origin == nil || out.Origin.HeadVerified != tc.wantVerified {
				t.Fatalf("creation = %+v, %v", out, err)
			}
			mu.Lock()
			defer mu.Unlock()
			// The POST itself always carries the unverified origin: the verdict
			// can only be established by reading the created request back.
			if initial, ok := mk.ParseNotificationOrigin(posted); !ok || initial != origin {
				t.Fatalf("posted origin = %+v, %v", initial, ok)
			}
			if patches != tc.wantPatches {
				t.Fatalf("got %d body rewrites, want %d", patches, tc.wantPatches)
			}
			if !tc.wantVerified {
				return
			}
			persisted, ok := mk.ParseNotificationOrigin(patched)
			if !ok || !persisted.HeadVerified {
				t.Fatalf("persisted origin = %+v, %v", persisted, ok)
			}
			persisted.HeadVerified = false
			m, owned := parseMarker(patched)
			if persisted != origin || !owned || m.SourceHead != origin.SourceOID || !strings.HasPrefix(patched, "Human text\n\n") {
				t.Fatalf("verification rewrote more than the verdict:\n%s", patched)
			}
		})
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
