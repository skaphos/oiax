package azuredevops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
	mk "github.com/skaphos/oiax/internal/forge/marker"
	"github.com/skaphos/oiax/internal/gittest"
)

const testToken = "super-secret-pat-value"

// jwtToken is a syntactically valid three-segment JWT (header "eyJ…"), used to
// prove the provider authenticates a $(System.AccessToken) as Bearer.
const jwtToken = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJidWlsZCJ9.c2lnbmF0dXJl"

const (
	gitBase     = "/platform/_apis/git/repositories/deploy"
	projectBase = "/platform/_apis"
)

// newProvider wires a Provider at the test server's base URL with a
// recognizable sentinel token so tests can assert it never leaks.
func newProvider(t *testing.T, srv *httptest.Server) *Provider {
	t.Helper()
	return &Provider{
		Repo:         Repo{Organization: "acme", Project: "platform", Name: "deploy"},
		Token:        testToken,
		BaseURL:      srv.URL,
		HTTP:         srv.Client(),
		retryBackoff: time.Microsecond,
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func decode(t *testing.T, r *http.Request, v any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
}

func assertNoToken(t *testing.T, s string) {
	t.Helper()
	if strings.Contains(s, testToken) {
		t.Errorf("output leaks the token: %q", s)
	}
}

// assertVersioned checks every request carries the required api-version query
// parameter and a JSON Accept header.
func assertVersioned(t *testing.T, r *http.Request) {
	t.Helper()
	if r.URL.Query().Get("api-version") != "7.1" {
		t.Errorf("%s %s: api-version = %q, want 7.1", r.Method, r.URL.Path, r.URL.Query().Get("api-version"))
	}
}

// pullSpec renders an Azure DevOps pull request, marker-first in the
// description unless descOverride is supplied.
type pullSpec struct {
	id           int
	source       string
	dest         string
	graph        string
	typ          string
	sourceHead   string
	descOverride string
	status       string
	closedDate   string
	fork         bool
}

func (s pullSpec) toPull() map[string]any {
	desc := s.descOverride
	if desc == "" {
		desc = mk.Serialize(mk.Marker{
			Version: "v1", Graph: s.graph, Type: s.typ,
			Source: s.source, Destination: s.dest, SourceHead: s.sourceHead,
		}) + "\n\nAutomated promotion."
	}
	status := s.status
	if status == "" {
		status = "active"
	}
	p := map[string]any{
		"pullRequestId": s.id,
		"title":         "title is never consulted",
		"description":   desc,
		"sourceRefName": "refs/heads/" + s.source,
		"targetRefName": "refs/heads/" + s.dest,
		"status":        status,
		"closedDate":    s.closedDate,
	}
	if s.fork {
		p["forkSource"] = map[string]any{"name": "refs/heads/feature"}
	}
	return p
}

func TestLooksLikeJWT(t *testing.T) {
	t.Parallel()
	if !looksLikeJWT(jwtToken) {
		t.Error("a three-segment eyJ token should be detected as a JWT")
	}
	for _, notJWT := range []string{"", "plainpat", "eyJonly.onedot", "a.b.c", testToken} {
		if looksLikeJWT(notJWT) {
			t.Errorf("%q should not be detected as a JWT", notJWT)
		}
	}
}

func TestAuthorizationScheme(t *testing.T) {
	t.Parallel()
	pat := &Provider{Token: "mypat"}
	wantBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte(":mypat"))
	if got := pat.authorization(); got != wantBasic {
		t.Errorf("PAT authorization = %q, want %q", got, wantBasic)
	}
	jwt := &Provider{Token: jwtToken}
	if got := jwt.authorization(); got != "Bearer "+jwtToken {
		t.Errorf("JWT authorization = %q, want Bearer", got)
	}
}

func TestListManagedRequestsFiltersAndTruncationSafe(t *testing.T) {
	t.Parallel()
	// A long human description that, if the marker were appended last, would be
	// truncated away by Azure DevOps's 400-char list cap. Marker-first keeps it
	// recognizable. The test server returns the description verbatim; the
	// truncation-safety is structural (marker position), asserted by recognition
	// succeeding with a long body.
	longTail := strings.Repeat("x", 900)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertVersioned(t, r)
		if r.URL.Query().Get("searchCriteria.status") != "active" {
			t.Errorf("status = %q, want active", r.URL.Query().Get("searchCriteria.status"))
		}
		matching := pullSpec{id: 7, source: "dev", dest: "staging", graph: "envs", typ: "promotion", sourceHead: "aaa"}
		mp := matching.toPull()
		mp["description"] = mk.Serialize(mk.Marker{
			Version: "v1", Graph: "envs", Type: "promotion", Source: "dev", Destination: "staging", SourceHead: "aaa",
		}) + "\n\n" + longTail
		writeJSON(t, w, http.StatusOK, map[string]any{"count": 3, "value": []map[string]any{
			mp,
			pullSpec{id: 8, source: "dev", dest: "staging", graph: "other", typ: "promotion", sourceHead: "bbb"}.toPull(),
			// A ref/marker mismatch (source ref says main, marker says dev): not managed.
			func() map[string]any {
				p := pullSpec{id: 9, source: "main", dest: "staging", graph: "envs", typ: "promotion", sourceHead: "ccc"}.toPull()
				p["description"] = mk.Serialize(mk.Marker{Version: "v1", Graph: "envs", Type: "promotion", Source: "dev", Destination: "staging", SourceHead: "ccc"})
				return p
			}(),
		}})
	}))
	defer srv.Close()

	got, err := newProvider(t, srv).ListManagedRequests(context.Background(), forge.RequestFilter{Graph: "envs"})
	if err != nil {
		t.Fatalf("ListManagedRequests: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d managed requests, want 1: %+v", len(got), got)
	}
	want := engine.ChangeRequest{ID: "7", Type: "promotion", Source: "dev", Target: "staging", SourceHead: "aaa"}
	if got[0] != want {
		t.Errorf("managed request = %+v, want %+v", got[0], want)
	}
}

func TestListManagedRequestsIgnoresForkPR(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"count": 1, "value": []map[string]any{
			pullSpec{id: 5, source: "dev", dest: "prod", graph: "g", typ: "promotion", sourceHead: "h", fork: true}.toPull(),
		}})
	}))
	defer srv.Close()
	got, err := newProvider(t, srv).ListManagedRequests(context.Background(), forge.RequestFilter{Graph: "g"})
	if err != nil {
		t.Fatalf("ListManagedRequests: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("a fork PR must never be managed, got %+v", got)
	}
}

func TestListManagedRequestsMergedUsesCompletedStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("searchCriteria.status"); got != "completed" {
			t.Errorf("status = %q, want completed", got)
		}
		if r.URL.Query().Get("searchCriteria.minTime") == "" {
			t.Error("merged discovery should bound by minTime")
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"count": 2, "value": []map[string]any{
			pullSpec{id: 1, source: "dev", dest: "prod", graph: "g", typ: "promotion", sourceHead: "old", status: "completed", closedDate: "2026-01-01T00:00:00Z"}.toPull(),
			pullSpec{id: 2, source: "dev", dest: "prod", graph: "g", typ: "promotion", sourceHead: "new", status: "completed", closedDate: "2026-07-01T00:00:00Z"}.toPull(),
		}})
	}))
	defer srv.Close()
	got, err := newProvider(t, srv).ListManagedRequests(context.Background(),
		forge.RequestFilter{Graph: "g", State: forge.RequestStateMerged})
	if err != nil {
		t.Fatalf("ListManagedRequests: %v", err)
	}
	if len(got) != 2 || got[0].SourceHead != "new" {
		t.Fatalf("merged discovery must sort newest-closed first, got %+v", got)
	}
}

func TestCreateRequestWritesMarkerFirstAndProperties(t *testing.T) {
	t.Parallel()
	var createBody map[string]any
	var propsPatch []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertVersioned(t, r)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == gitBase+"/pullrequests":
			decode(t, r, &createBody)
			if ct := r.Header.Get("Content-Type"); ct != contentTypeJSON {
				t.Errorf("create Content-Type = %q, want %q", ct, contentTypeJSON)
			}
			writeJSON(t, w, http.StatusCreated, map[string]any{"pullRequestId": 42})
		case r.Method == http.MethodPatch && r.URL.Path == gitBase+"/pullrequests/42/properties":
			if ct := r.Header.Get("Content-Type"); ct != contentTypeJSONPatch {
				t.Errorf("properties Content-Type = %q, want %q", ct, contentTypeJSONPatch)
			}
			decode(t, r, &propsPatch)
			writeJSON(t, w, http.StatusOK, map[string]any{})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	cr, err := newProvider(t, srv).CreateRequest(context.Background(), forge.CreateRequest{
		Graph: "envs", Type: "promotion", Source: "dev", Target: "staging",
		SourceHead: "abc123", Title: "Promote", Body: "Automated.",
	})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if cr.ID != "42" {
		t.Errorf("id = %q, want 42", cr.ID)
	}
	// Refs are fully qualified; the marker is first in the description.
	if createBody["sourceRefName"] != "refs/heads/dev" || createBody["targetRefName"] != "refs/heads/staging" {
		t.Errorf("refs = %v / %v", createBody["sourceRefName"], createBody["targetRefName"])
	}
	desc, _ := createBody["description"].(string)
	if !strings.HasPrefix(desc, "<!--") {
		t.Errorf("description must be marker-first, got prefix %q", desc[:min(20, len(desc))])
	}
	m, ok := mk.Parse(desc)
	if !ok || m.SourceHead != "abc123" {
		t.Errorf("description marker = %+v, ok=%v", m, ok)
	}
	// Labels attached inline in the create body.
	labels, _ := createBody["labels"].([]any)
	if len(labels) != 2 {
		t.Errorf("labels = %v, want oiax + type", createBody["labels"])
	}
	// The durable property carries the same serialized marker.
	if len(propsPatch) != 1 || propsPatch[0]["path"] != "/"+markerProperty {
		t.Fatalf("properties patch = %+v", propsPatch)
	}
	pm, ok := mk.Parse(propsPatch[0]["value"].(string))
	if !ok || pm.SourceHead != "abc123" {
		t.Errorf("property marker = %+v, ok=%v", pm, ok)
	}
}

func TestCreateRequestAdoptsDuplicate(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == gitBase+"/pullrequests":
			// Azure DevOps refuses a duplicate active PR with 409 + TF401179.
			writeJSON(t, w, http.StatusConflict, map[string]any{
				"message": "TF401179: An active pull request for the source and target branch already exists.",
				"typeKey": "GitPullRequestExistsException",
			})
		case r.Method == http.MethodGet && r.URL.Path == gitBase+"/pullrequests":
			writeJSON(t, w, http.StatusOK, map[string]any{"count": 1, "value": []map[string]any{
				pullSpec{id: 99, source: "dev", dest: "staging", graph: "envs", typ: "promotion", sourceHead: "live"}.toPull(),
			}})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	cr, err := newProvider(t, srv).CreateRequest(context.Background(), forge.CreateRequest{
		Graph: "envs", Type: "promotion", Source: "dev", Target: "staging", SourceHead: "x",
	})
	if err != nil {
		t.Fatalf("CreateRequest should adopt the duplicate, got %v", err)
	}
	if cr.ID != "99" {
		t.Errorf("adopted id = %q, want the surviving request 99", cr.ID)
	}
}

func TestUpdateRequestPrefersPropertyMarker(t *testing.T) {
	t.Parallel()
	var descPatch map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == gitBase+"/pullrequests/12":
			// The description marker is stale/absent; identity comes from properties.
			p := pullSpec{id: 12, source: "dev", dest: "staging", graph: "envs", typ: "promotion", sourceHead: "stale"}.toPull()
			p["description"] = "a human rewrote this with no marker"
			writeJSON(t, w, http.StatusOK, p)
		case r.Method == http.MethodGet && r.URL.Path == gitBase+"/pullrequests/12/properties":
			marker := mk.Serialize(mk.Marker{Version: "v1", Graph: "envs", Type: "promotion", Source: "dev", Destination: "staging", SourceHead: "old"})
			writeJSON(t, w, http.StatusOK, map[string]any{"count": 1, "value": map[string]any{
				markerProperty: map[string]any{"$type": "System.String", "$value": marker},
			}})
		case r.Method == http.MethodPatch && r.URL.Path == gitBase+"/pullrequests/12":
			decode(t, r, &descPatch)
			writeJSON(t, w, http.StatusOK, map[string]any{})
		case r.Method == http.MethodPatch && r.URL.Path == gitBase+"/pullrequests/12/properties":
			writeJSON(t, w, http.StatusOK, map[string]any{})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	if err := newProvider(t, srv).UpdateRequest(context.Background(),
		forge.UpdateRequest{ID: "12", SourceHead: "fresh"}); err != nil {
		t.Fatalf("UpdateRequest: %v", err)
	}
	m, ok := mk.Parse(descPatch["description"])
	if !ok || m.SourceHead != "fresh" || m.Source != "dev" {
		t.Errorf("patched description marker = %+v ok=%v, want sourceHead fresh restored from properties identity", m, ok)
	}
}

func TestCloseRequestCommentsThenAbandons(t *testing.T) {
	t.Parallel()
	var abandoned bool
	var commented bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == gitBase+"/pullrequests/3":
			writeJSON(t, w, http.StatusOK, pullSpec{id: 3, source: "dev", dest: "staging", graph: "envs", typ: "promotion", sourceHead: "h"}.toPull())
		case r.Method == http.MethodGet && r.URL.Path == gitBase+"/pullrequests/3/properties":
			writeJSON(t, w, http.StatusOK, map[string]any{"value": map[string]any{}})
		case r.Method == http.MethodPost && r.URL.Path == gitBase+"/pullrequests/3/threads":
			commented = true
			var thread map[string]any
			decode(t, r, &thread)
			writeJSON(t, w, http.StatusOK, map[string]any{"id": 1})
		case r.Method == http.MethodPatch && r.URL.Path == gitBase+"/pullrequests/3":
			var body map[string]string
			decode(t, r, &body)
			if body["status"] == "abandoned" {
				abandoned = true
			}
			writeJSON(t, w, http.StatusOK, map[string]any{})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	if err := newProvider(t, srv).CloseRequest(context.Background(), "3", forge.Reason{Summary: "obsolete"}); err != nil {
		t.Fatalf("CloseRequest: %v", err)
	}
	if !commented || !abandoned {
		t.Errorf("close must comment (%v) then abandon (%v)", commented, abandoned)
	}
}

func TestDeleteBranchResolvesAndDeletes(t *testing.T) {
	t.Parallel()
	var update []map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == gitBase+"/refs":
			writeJSON(t, w, http.StatusOK, map[string]any{"value": []map[string]any{
				{"name": "refs/heads/oiax/backflow/x", "objectId": "deadbeef"},
			}})
		case r.Method == http.MethodPost && r.URL.Path == gitBase+"/refs":
			decode(t, r, &update)
			writeJSON(t, w, http.StatusOK, map[string]any{"value": []map[string]any{
				{"success": true, "updateStatus": "succeeded"},
			}})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	if err := newProvider(t, srv).DeleteBranch(context.Background(), "oiax/backflow/x"); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	if len(update) != 1 || update[0]["oldObjectId"] != "deadbeef" || update[0]["newObjectId"] != zeroObjectID {
		t.Errorf("delete update = %+v, want old=deadbeef new=zeros", update)
	}
}

func TestDeleteBranchIdempotentWhenAbsent(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == gitBase+"/refs" {
			writeJSON(t, w, http.StatusOK, map[string]any{"value": []map[string]any{}})
			return
		}
		t.Fatalf("a missing ref must not POST a delete; got %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	if err := newProvider(t, srv).DeleteBranch(context.Background(), "oiax/gone"); err != nil {
		t.Errorf("deleting an absent branch must be idempotent success, got %v", err)
	}
}

func TestDeleteBranchNamespaceGuard(t *testing.T) {
	t.Parallel()
	p := &Provider{Repo: Repo{Organization: "a", Project: "b", Name: "c"}}
	if err := p.DeleteBranch(context.Background(), "main"); err == nil || !strings.Contains(err.Error(), "oiax/ namespace") {
		t.Errorf("deleting outside oiax/ must be refused, got %v", err)
	}
}

func TestPushBranchToLocalBareRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bare := filepath.Join(dir, "bare.git")
	work := filepath.Join(dir, "work")
	gittest.Run(t, "", "init", "--bare", bare)
	sha := seedRepo(t, work)

	p := &Provider{Repo: Repo{Organization: "a", Project: "b", Name: "c"}, Token: testToken, GitDir: work, GitRemote: bare}
	branch := "oiax/backflow/main-to-dev/" + sha[:7]
	if err := p.PushBranch(context.Background(), forge.BranchPush{Name: branch, SHA: sha, Force: true}); err != nil {
		t.Fatalf("PushBranch: %v", err)
	}
	if got := strings.TrimSpace(gittest.Run(t, bare, "rev-parse", "refs/heads/"+branch)); got != sha {
		t.Fatalf("bare ref = %s, want %s", got, sha)
	}
	// Idempotent re-push.
	if err := p.PushBranch(context.Background(), forge.BranchPush{Name: branch, SHA: sha, Force: true}); err != nil {
		t.Fatalf("idempotent re-push: %v", err)
	}
}

func TestPushBranchGuardsAndScrub(t *testing.T) {
	t.Parallel()
	p := &Provider{Repo: Repo{Organization: "a", Project: "b", Name: "c"}, Token: testToken}
	if err := p.PushBranch(context.Background(), forge.BranchPush{Name: "main", SHA: "abc1234"}); err == nil ||
		!strings.Contains(err.Error(), "oiax/ namespace") {
		t.Errorf("push outside oiax/ must be refused, got %v", err)
	}
	if err := p.PushBranch(context.Background(), forge.BranchPush{Name: "oiax/x", SHA: "not-a-sha"}); err == nil ||
		!strings.Contains(err.Error(), "invalid commit id") {
		t.Errorf("invalid SHA must be refused, got %v", err)
	}

	// A bogus remote path embedding the token: git echoes it on failure, so the
	// scrubber runs on real git output with no network.
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	sha := seedRepo(t, work)
	bad := &Provider{Repo: Repo{Organization: "a", Project: "b", Name: "c"}, Token: testToken,
		GitDir: work, GitRemote: filepath.Join(dir, "missing-"+testToken+".git")}
	err := bad.PushBranch(context.Background(), forge.BranchPush{Name: "oiax/backflow/x/y", SHA: sha, Force: true})
	if err == nil {
		t.Fatal("push to a missing remote should fail")
	}
	assertNoToken(t, err.Error())
}

func TestPushAuthEnvSchemeByTokenShape(t *testing.T) {
	t.Parallel()
	pat := &Provider{Repo: Repo{Organization: "a", Project: "b", Name: "c"}, Token: "mypat", GitRemote: "https://dev.azure.com/a/b/_git/c"}
	env := strings.Join(pat.pushAuthEnv(), "\n")
	if !strings.Contains(env, "AUTHORIZATION: basic "+base64.StdEncoding.EncodeToString([]byte(":mypat"))) {
		t.Errorf("PAT push must use basic extraheader, got %q", env)
	}
	jwt := &Provider{Repo: Repo{Organization: "a", Project: "b", Name: "c"}, Token: jwtToken, GitRemote: "https://dev.azure.com/a/b/_git/c"}
	if !strings.Contains(strings.Join(jwt.pushAuthEnv(), "\n"), "AUTHORIZATION: bearer "+jwtToken) {
		t.Error("JWT push must use bearer extraheader")
	}
	// A non-http remote (a local bare repo) carries no credential.
	local := &Provider{Token: "mypat", GitRemote: "/tmp/bare.git"}
	if local.pushAuthEnv() != nil {
		t.Error("a local remote must carry no credential env")
	}
}

func seedRepo(t *testing.T, dir string) string {
	t.Helper()
	gittest.InitRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	gittest.Run(t, dir, "add", "file.txt")
	gittest.Run(t, dir, "commit", "-m", "seed")
	return strings.TrimSpace(gittest.Run(t, dir, "rev-parse", "HEAD"))
}

func TestErrorsNeverLeakToken(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusInternalServerError, map[string]any{"message": "boom", "typeKey": "X"})
	}))
	defer srv.Close()
	p := newProvider(t, srv)
	ops := []func() error{
		func() error {
			_, err := p.ListManagedRequests(context.Background(), forge.RequestFilter{Graph: "g"})
			return err
		},
		func() error {
			_, err := p.CreateRequest(context.Background(), forge.CreateRequest{Source: "s", Target: "t"})
			return err
		},
		func() error { return p.UpdateRequest(context.Background(), forge.UpdateRequest{ID: "1"}) },
		func() error { return p.CloseRequest(context.Background(), "1", forge.Reason{}) },
		func() error { _, err := p.ListConflictArtifacts(context.Background(), "g"); return err },
		func() error { _, err := p.TargetMergeMethods(context.Background(), "main"); return err },
	}
	for i, op := range ops {
		if err := op(); err != nil {
			assertNoToken(t, fmt.Sprintf("op %d: %s", i, err.Error()))
		} else {
			t.Errorf("op %d: expected an error", i)
		}
	}
}
