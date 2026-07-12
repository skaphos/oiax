package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/gittest"
)

const testToken = "super-secret-token-value"

// newProvider wires a Provider at the test server's base URL. The token
// is a recognizable sentinel so tests can assert it never leaks.
func newProvider(t *testing.T, srv *httptest.Server) *Provider {
	t.Helper()
	return &Provider{
		Owner:   "acme",
		Repo:    "widgets",
		Token:   testToken,
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
		// Shrink backoff so retry paths run instantly under test; a
		// server-provided Retry-After of 0 still exercises the honoring path.
		retryBackoff: time.Microsecond,
	}
}

// prJSON renders a pull request as GitHub would, with a managed marker
// unless bodyOverride is supplied.
type prSpec struct {
	number       int
	source       string
	dest         string
	graph        string
	typ          string
	sourceHead   string
	labels       []string
	bodyOverride string // when non-empty, used verbatim as the body
	state        string
	mergedAt     string // empty => null
	createdAt    string // empty => omitted
	authorLogin  string
}

func (s prSpec) toPull() map[string]any {
	body := s.bodyOverride
	if body == "" {
		body = "Automated promotion.\n\n" + serializeMarker(marker{
			Version:     "v1",
			Graph:       s.graph,
			Type:        s.typ,
			Source:      s.source,
			Destination: s.dest,
			SourceHead:  s.sourceHead,
		})
	}
	labels := make([]map[string]string, 0, len(s.labels))
	for _, l := range s.labels {
		labels = append(labels, map[string]string{"name": l})
	}
	state := s.state
	if state == "" {
		state = "open"
	}
	pr := map[string]any{
		"number": s.number,
		"state":  state,
		"title":  "PR title is never consulted",
		"body":   body,
		"head":   map[string]string{"ref": s.source, "sha": s.sourceHead},
		"base":   map[string]string{"ref": s.dest},
		"labels": labels,
		"user":   map[string]string{"login": s.authorLogin},
	}
	if s.mergedAt != "" {
		pr["merged_at"] = s.mergedAt
	} else {
		pr["merged_at"] = nil
	}
	if s.createdAt != "" {
		pr["created_at"] = s.createdAt
	}
	return pr
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

// TestDoOnceConstructionErrorNotRetried pins that a deterministic pre-round-trip
// failure (here an invalid HTTP method, which makes http.NewRequestWithContext
// fail before anything is sent) is NOT classified as a transient no-response
// error: it must fail fast rather than be retried by send, since a retry would
// fail identically.
func TestDoOnceConstructionErrorNotRetried(t *testing.T) {
	t.Parallel()
	p := &Provider{Owner: "acme", Repo: "widgets", Token: testToken, HTTP: http.DefaultClient}

	_, err := p.doOnce(context.Background(), "BAD METHOD", "http://example.invalid", nil, nil)
	if err == nil {
		t.Fatal("expected a construction error from an invalid method")
	}
	var noResp *errNoResponse
	if errors.As(err, &noResp) {
		t.Errorf("construction error wrapped as errNoResponse (would be retried): %v", err)
	}
	if _, retry := retryDelay(err, http.Header{}, time.Second); retry {
		t.Errorf("construction error classified as retryable; want fail-fast: %v", err)
	}
}

func TestRepoMergeMethods(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/repos/acme/widgets" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"allow_merge_commit": true,
			"allow_squash_merge": false,
			"allow_rebase_merge": true,
		})
	}))
	defer srv.Close()

	got, err := newProvider(t, srv).RepoMergeMethods(context.Background())
	if err != nil {
		t.Fatalf("RepoMergeMethods: %v", err)
	}
	want := forge.MergeMethods{Merge: true, Squash: false, Rebase: true}
	if got != want {
		t.Errorf("RepoMergeMethods = %+v, want %+v", got, want)
	}
}

// assertAuth verifies every request carries the expected headers.
func assertAuth(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
		t.Errorf("Authorization = %q, want bearer with token", got)
	}
	if got := r.Header.Get("Accept"); got != acceptHeader {
		t.Errorf("Accept = %q, want %q", got, acceptHeader)
	}
	if got := r.Header.Get("X-GitHub-Api-Version"); got != apiVersion {
		t.Errorf("X-GitHub-Api-Version = %q, want %q", got, apiVersion)
	}
}

func TestMarkerRoundTrip(t *testing.T) {
	t.Parallel()
	in := marker{
		Version:     "v1",
		Graph:       "environments",
		Type:        "promotion",
		Source:      "development",
		Destination: "test",
		SourceHead:  "4f2a91c0deadbeef4f2a91c0deadbeef4f2a91c0",
	}
	body := "Human authored description.\n\n" + serializeMarker(in)
	got, ok := parseMarker(body)
	if !ok {
		t.Fatal("parseMarker returned ok=false for a serialized marker")
	}
	if got != in {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, in)
	}

	// A stray "oiax:" in prose (outside an HTML comment) is not a marker.
	if _, ok := parseMarker("we mention oiax: here in prose"); ok {
		t.Error("parseMarker matched prose, want no marker")
	}
}

func TestReplaceMarkerPreservesText(t *testing.T) {
	t.Parallel()
	orig := marker{Version: "v1", Graph: "g", Type: "promotion", Source: "s", Destination: "d", SourceHead: "aaa"}
	body := "Lead text.\n\n" + serializeMarker(orig) + "\n\nTrailing text."
	updated := orig
	updated.SourceHead = "bbb"
	got, ok := replaceMarker(body, updated)
	if !ok {
		t.Fatal("replaceMarker ok=false")
	}
	if !strings.HasPrefix(got, "Lead text.\n\n") || !strings.HasSuffix(got, "\n\nTrailing text.") {
		t.Errorf("human text not preserved: %q", got)
	}
	m, ok := parseMarker(got)
	if !ok || m.SourceHead != "bbb" {
		t.Errorf("sourceHead not updated: %+v ok=%v", m, ok)
	}
}

// TestManagedMarkerIdentity pins the on-forge identity rules directly at
// managedMarker: identity is marker presence + a recognized version + the
// branch relationship, and NOT the oiax label. It fails against the pre-fix
// build, which required an exact v1 version and the oiax label.
func TestManagedMarkerIdentity(t *testing.T) {
	t.Parallel()
	// pull builds a ghPull whose default (empty body) carries a managed marker
	// for the development->test edge with the given version; a non-empty body is
	// used verbatim.
	pull := func(version string, labels []string, body string) ghPull {
		if body == "" {
			body = "Automated promotion.\n\n" + serializeMarker(marker{
				Version: version, Graph: "environments", Type: "promotion",
				Source: "development", Destination: "test", SourceHead: "aaa",
			})
		}
		ls := make([]ghLabel, 0, len(labels))
		for _, l := range labels {
			ls = append(ls, ghLabel{Name: l})
		}
		return ghPull{
			Number: 1,
			Body:   body,
			Head:   ghRef{Ref: "development"},
			Base:   ghRef{Ref: "test"},
			Labels: ls,
		}
	}

	tests := []struct {
		name   string
		pr     ghPull
		wantOK bool
	}{
		{"v1 with the oiax label is managed", pull("v1", []string{LabelOiax, LabelPromotion}, ""), true},
		// The label is decorative: a human removing it must not make oiax lose
		// track of its own request.
		{"v1 without the oiax label is still managed", pull("v1", []string{LabelPromotion}, ""), true},
		// Forward compatibility: a marker written by a newer release is still
		// recognized, so an older oiax never opens a duplicate.
		{"v2 marker is recognized as managed", pull("v2", []string{LabelOiax}, ""), true},
		// Prose that merely mentions oiax outside an HTML comment is never a marker.
		{"prose mentioning oiax is not a marker", pull("", nil, "we track this with oiax: promotion, but it is prose"), false},
		{"no marker at all is not managed", pull("", nil, "an ordinary human PR body"), false},
		{"malformed version token is not a marker", pull("draft", []string{LabelOiax}, ""), false},
		{"branch relationship contradicting the marker is not managed", func() ghPull {
			p := pull("v1", []string{LabelOiax}, "")
			p.Base = ghRef{Ref: "staging"}
			return p
		}(), false},
		// A fork PR (head branch in a different repository than the base) is
		// never managed even with a well-formed matching marker: only push
		// access can put a branch in the base repo, so this is the provenance
		// signal a PR author cannot forge with body text and branch names.
		{"a fork PR whose head repo differs from its base repo is not managed", func() ghPull {
			p := pull("v1", []string{LabelOiax}, "")
			p.Head.Repo = ghRepo{FullName: "attacker/widgets"}
			p.Base.Repo = ghRepo{FullName: "acme/widgets"}
			return p
		}(), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, ok := managedMarker(tc.pr); ok != tc.wantOK {
				t.Fatalf("managedMarker ok = %v, want %v", ok, tc.wantOK)
			}
		})
	}
}

// TestUpdateRequestRefusesNewerMarkerVersion proves the acting/recognition
// split: a v2 marker is recognized as managed (so it is never duplicated) but
// this build must not rewrite a schema it does not understand.
func TestUpdateRequestRefusesNewerMarkerVersion(t *testing.T) {
	t.Parallel()
	newer := prSpec{number: 5, source: "development", dest: "test", graph: "environments",
		typ: "promotion", sourceHead: "old-head", labels: []string{LabelOiax, LabelPromotion}}.toPull()
	newer["body"] = "Automated promotion.\n\n" + serializeMarker(marker{
		Version: "v2", Graph: "environments", Type: "promotion",
		Source: "development", Destination: "test", SourceHead: "old-head",
	})
	patched := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patched = true
		}
		writeJSON(t, w, http.StatusOK, newer)
	}))
	defer srv.Close()

	p := newProvider(t, srv)
	err := p.UpdateRequest(context.Background(), forge.UpdateRequest{ID: "5", SourceHead: "new-head"})
	if err == nil {
		t.Fatal("UpdateRequest should refuse a marker version newer than this build understands")
	}
	if !strings.Contains(err.Error(), "not supported by this build") {
		t.Errorf("error = %v, want a version-understanding refusal", err)
	}
	if patched {
		t.Error("UpdateRequest rewrote a marker it does not understand")
	}
}

func TestListManagedRequestsFiltering(t *testing.T) {
	t.Parallel()
	pulls := []map[string]any{
		// Managed promotion for the graph — kept.
		prSpec{number: 1, source: "development", dest: "test", graph: "environments",
			typ: "promotion", sourceHead: "aaa", labels: []string{LabelOiax, LabelPromotion}}.toPull(),
		// Valid marker, oiax label absent — the label is decorative, so this is
		// still one of oiax's managed requests and is kept.
		prSpec{number: 2, source: "development", dest: "test", graph: "environments",
			typ: "promotion", sourceHead: "bbb", labels: []string{"other"}}.toPull(),
		// Marker graph differs from the filter — excluded.
		prSpec{number: 3, source: "development", dest: "test", graph: "other-graph",
			typ: "promotion", sourceHead: "ccc", labels: []string{LabelOiax, LabelPromotion}}.toPull(),
		// Branch relationship contradicts the marker (base != destination) — unmanaged.
		prSpec{number: 4, source: "development", dest: "staging", graph: "environments",
			typ: "promotion", sourceHead: "ddd", labels: []string{LabelOiax, LabelPromotion},
			bodyOverride: serializeMarker(marker{Version: "v1", Graph: "environments",
				Type: "promotion", Source: "development", Destination: "test", SourceHead: "ddd"})}.toPull(),
		// No marker at all — unmanaged.
		prSpec{number: 5, source: "development", dest: "test", graph: "environments",
			typ: "promotion", sourceHead: "eee", labels: []string{LabelOiax},
			bodyOverride: "just a normal human PR body"}.toPull(),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		if r.URL.Path != "/repos/acme/widgets/pulls" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("state"); got != "open" {
			t.Errorf("state = %q, want open", got)
		}
		writeJSON(t, w, http.StatusOK, pulls)
	}))
	defer srv.Close()

	p := newProvider(t, srv)
	got, err := p.ListManagedRequests(context.Background(),
		forge.RequestFilter{Graph: "environments", Type: engine.RequestTypePromotion})
	if err != nil {
		t.Fatalf("ListManagedRequests: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d managed requests, want 2: %+v", len(got), got)
	}
	byID := map[string]engine.ChangeRequest{}
	for _, cr := range got {
		byID[cr.ID] = cr
	}
	want := map[string]engine.ChangeRequest{
		"1": {ID: "1", Type: engine.RequestTypePromotion, Source: "development", Target: "test", SourceHead: "aaa"},
		// PR 2 has no oiax label yet is still recognized: identity does not
		// depend on the (decorative) label.
		"2": {ID: "2", Type: engine.RequestTypePromotion, Source: "development", Target: "test", SourceHead: "bbb"},
	}
	for id, w := range want {
		if byID[id] != w {
			t.Errorf("managed request %s = %+v, want %+v", id, byID[id], w)
		}
	}
}

func TestListManagedRequestsMergedState(t *testing.T) {
	t.Parallel()
	pulls := []map[string]any{
		// Merged managed request — kept for the baseline rung.
		prSpec{number: 10, source: "development", dest: "test", graph: "environments",
			typ: "promotion", sourceHead: "merged1", labels: []string{LabelOiax, LabelPromotion},
			state: "closed", mergedAt: "2026-01-02T03:04:05Z"}.toPull(),
		// Closed but never merged — excluded.
		prSpec{number: 11, source: "development", dest: "test", graph: "environments",
			typ: "promotion", sourceHead: "closed1", labels: []string{LabelOiax, LabelPromotion},
			state: "closed"}.toPull(),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		if got := r.URL.Query().Get("state"); got != "closed" {
			t.Errorf("state = %q, want closed", got)
		}
		writeJSON(t, w, http.StatusOK, pulls)
	}))
	defer srv.Close()

	p := newProvider(t, srv)
	got, err := p.ListManagedRequests(context.Background(),
		forge.RequestFilter{Graph: "environments", Type: engine.RequestTypePromotion, State: forge.RequestStateMerged})
	if err != nil {
		t.Fatalf("ListManagedRequests: %v", err)
	}
	if len(got) != 1 || got[0].ID != "10" {
		t.Fatalf("merged filter = %+v, want only PR 10", got)
	}
}

func TestListManagedRequestsPagination(t *testing.T) {
	t.Parallel()
	var srvURL string
	page1 := []map[string]any{
		prSpec{number: 1, source: "development", dest: "test", graph: "environments",
			typ: "promotion", sourceHead: "aaa", labels: []string{LabelOiax, LabelPromotion}}.toPull(),
	}
	page2 := []map[string]any{
		prSpec{number: 2, source: "test", dest: "prod", graph: "environments",
			typ: "promotion", sourceHead: "bbb", labels: []string{LabelOiax, LabelPromotion}}.toPull(),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		if r.URL.Query().Get("page") == "2" {
			writeJSON(t, w, http.StatusOK, page2)
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/acme/widgets/pulls?state=open&per_page=100&page=2>; rel="next"`, srvURL))
		writeJSON(t, w, http.StatusOK, page1)
	}))
	defer srv.Close()
	srvURL = srv.URL

	p := newProvider(t, srv)
	got, err := p.ListManagedRequests(context.Background(), forge.RequestFilter{Graph: "environments"})
	if err != nil {
		t.Fatalf("ListManagedRequests: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("pagination got %d requests, want 2: %+v", len(got), got)
	}
}

func TestCreateRequestSuccessAndLabels(t *testing.T) {
	t.Parallel()
	var gotCreateBody map[string]any
	var gotLabels map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/pulls":
			decode(t, r, &gotCreateBody)
			writeJSON(t, w, http.StatusCreated, prSpec{number: 7, source: "development", dest: "test",
				graph: "environments", typ: "promotion", sourceHead: "aaa",
				authorLogin: "app-bot"}.toPull())
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/issues/7/labels":
			decode(t, r, &gotLabels)
			writeJSON(t, w, http.StatusOK, []map[string]string{})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newProvider(t, srv)
	got, err := p.CreateRequest(context.Background(), forge.CreateRequest{
		Graph: "environments", Type: engine.RequestTypePromotion,
		Source: "development", Target: "test", SourceHead: "aaa",
		Title: "Promote development to test", Body: "Automated promotion.",
	})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	want := engine.ChangeRequest{ID: "7", Type: engine.RequestTypePromotion,
		Source: "development", Target: "test", SourceHead: "aaa"}
	if got != want {
		t.Errorf("created request = %+v, want %+v", got, want)
	}
	// The body carries head/base and the marker.
	if gotCreateBody["head"] != "development" || gotCreateBody["base"] != "test" {
		t.Errorf("create payload head/base wrong: %+v", gotCreateBody)
	}
	if m, ok := parseMarker(gotCreateBody["body"].(string)); !ok || m.SourceHead != "aaa" || m.Destination != "test" {
		t.Errorf("create body marker wrong: %+v ok=%v", m, ok)
	}
	if len(gotLabels["labels"]) != 2 || gotLabels["labels"][0] != LabelOiax || gotLabels["labels"][1] != LabelPromotion {
		t.Errorf("labels = %+v, want [%s %s]", gotLabels["labels"], LabelOiax, LabelPromotion)
	}
}

func TestCreateRequestDegradationWarning(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/pulls" {
			writeJSON(t, w, http.StatusCreated, prSpec{number: 8, source: "development", dest: "test",
				graph: "environments", typ: "promotion", sourceHead: "aaa",
				authorLogin: botLogin}.toPull())
			return
		}
		writeJSON(t, w, http.StatusOK, []map[string]string{})
	}))
	defer srv.Close()

	var warnings []string
	p := newProvider(t, srv)
	p.Warn = func(msg string) { warnings = append(warnings, msg) }

	for range 2 {
		if _, err := p.CreateRequest(context.Background(), forge.CreateRequest{
			Graph: "environments", Type: engine.RequestTypePromotion,
			Source: "development", Target: "test", SourceHead: "aaa",
		}); err != nil {
			t.Fatalf("CreateRequest: %v", err)
		}
	}
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want exactly 1 (once): %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], botLogin) {
		t.Errorf("warning does not mention the bot: %q", warnings[0])
	}
}

func TestCreateRequestAdopts422Duplicate(t *testing.T) {
	t.Parallel()
	// The re-list returns several open managed promotion requests in the same
	// graph — the normal case for a multi-node chain (development->test->main).
	// Only one matches the create's source/target, and it is not first, so the
	// adoption predicate must discriminate by edge: a regression that returned
	// the first (or any other) request would adopt the wrong PR and record its
	// SourceHead against this edge.
	otherEdge := prSpec{number: 40, source: "test", dest: "main", graph: "environments",
		typ: "promotion", sourceHead: "other-head", labels: []string{LabelOiax, LabelPromotion}}.toPull()
	matchEdge := prSpec{number: 42, source: "development", dest: "test", graph: "environments",
		typ: "promotion", sourceHead: "live-head", labels: []string{LabelOiax, LabelPromotion}}.toPull()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/pulls":
			// GitHub refuses the duplicate head/base pair.
			writeJSON(t, w, http.StatusUnprocessableEntity, map[string]any{
				"message": "Validation Failed",
				"errors": []map[string]string{
					{"resource": "PullRequest", "code": "custom",
						"message": "A pull request already exists for acme:development."},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/pulls":
			writeJSON(t, w, http.StatusOK, []map[string]any{otherEdge, matchEdge})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newProvider(t, srv)
	got, err := p.CreateRequest(context.Background(), forge.CreateRequest{
		Graph: "environments", Type: engine.RequestTypePromotion,
		Source: "development", Target: "test", SourceHead: "new-head",
	})
	if err != nil {
		t.Fatalf("CreateRequest should adopt the duplicate, got error: %v", err)
	}
	if got.ID != "42" || got.SourceHead != "live-head" {
		t.Errorf("adopted request = %+v, want the matching PR 42 (development->test), not another open edge", got)
	}
}

func TestCreateRequest422NoSurvivorReturnsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			writeJSON(t, w, http.StatusUnprocessableEntity, map[string]any{"message": "Validation Failed"})
			return
		}
		// Re-list surfaces nothing matching.
		writeJSON(t, w, http.StatusOK, []map[string]any{})
	}))
	defer srv.Close()

	p := newProvider(t, srv)
	_, err := p.CreateRequest(context.Background(), forge.CreateRequest{
		Graph: "environments", Type: engine.RequestTypePromotion,
		Source: "development", Target: "test", SourceHead: "x",
	})
	if err == nil {
		t.Fatal("expected error when no surviving request surfaces")
	}
	assertNoToken(t, err.Error())
}

func TestUpdateRequestRewritesSourceHead(t *testing.T) {
	t.Parallel()
	original := prSpec{number: 5, source: "development", dest: "test", graph: "environments",
		typ: "promotion", sourceHead: "old-head", labels: []string{LabelOiax, LabelPromotion}}.toPull()
	var patchedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		switch r.Method {
		case http.MethodGet:
			writeJSON(t, w, http.StatusOK, original)
		case http.MethodPatch:
			var body map[string]string
			decode(t, r, &body)
			patchedBody = body["body"]
			writeJSON(t, w, http.StatusOK, original)
		default:
			t.Errorf("unexpected %s", r.Method)
		}
	}))
	defer srv.Close()

	p := newProvider(t, srv)
	if err := p.UpdateRequest(context.Background(), forge.UpdateRequest{ID: "5", SourceHead: "new-head"}); err != nil {
		t.Fatalf("UpdateRequest: %v", err)
	}
	m, ok := parseMarker(patchedBody)
	if !ok || m.SourceHead != "new-head" {
		t.Errorf("patched marker sourceHead = %+v ok=%v, want new-head", m, ok)
	}
}

func TestUpdateRequestRefusesUnmanaged(t *testing.T) {
	t.Parallel()
	// No marker in the body => unmanaged. (A missing oiax label no longer makes
	// a request unmanaged; the label is decorative, so the fixture must lack the
	// marker itself to exercise the refusal.)
	unmanaged := prSpec{number: 5, source: "development", dest: "test", graph: "environments",
		typ: "promotion", sourceHead: "h", labels: []string{LabelOiax, LabelPromotion},
		bodyOverride: "just a human PR body, no marker"}.toPull()
	patched := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patched = true
		}
		writeJSON(t, w, http.StatusOK, unmanaged)
	}))
	defer srv.Close()

	p := newProvider(t, srv)
	err := p.UpdateRequest(context.Background(), forge.UpdateRequest{ID: "5", SourceHead: "new"})
	if err == nil {
		t.Fatal("UpdateRequest should refuse an unmanaged request")
	}
	if patched {
		t.Error("UpdateRequest patched an unmanaged request")
	}
}

func TestCloseRequestCommentsThenCloses(t *testing.T) {
	t.Parallel()
	managed := prSpec{number: 9, source: "development", dest: "test", graph: "environments",
		typ: "promotion", sourceHead: "h", labels: []string{LabelOiax, LabelPromotion}}.toPull()
	var order []string
	var comment string
	var closedState string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		switch {
		case r.Method == http.MethodGet:
			writeJSON(t, w, http.StatusOK, managed)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comments"):
			order = append(order, "comment")
			var b map[string]string
			decode(t, r, &b)
			comment = b["body"]
			writeJSON(t, w, http.StatusCreated, map[string]string{})
		case r.Method == http.MethodPatch:
			order = append(order, "close")
			var b map[string]string
			decode(t, r, &b)
			closedState = b["state"]
			writeJSON(t, w, http.StatusOK, managed)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newProvider(t, srv)
	if err := p.CloseRequest(context.Background(), "9", forge.Reason{Summary: "superseded"}); err != nil {
		t.Fatalf("CloseRequest: %v", err)
	}
	if len(order) != 2 || order[0] != "comment" || order[1] != "close" {
		t.Errorf("call order = %v, want [comment close]", order)
	}
	if comment != "superseded" {
		t.Errorf("comment = %q, want superseded", comment)
	}
	if closedState != "closed" {
		t.Errorf("close state = %q, want closed", closedState)
	}
}

// TestCloseRequestRefusesNewerMarkerVersion mirrors the UpdateRequest gate:
// a v2 marker is recognized as managed (so it is never duplicated) but this
// build must not close a schema it does not understand.
func TestCloseRequestRefusesNewerMarkerVersion(t *testing.T) {
	t.Parallel()
	newer := prSpec{number: 9, source: "development", dest: "test", graph: "environments",
		typ: "promotion", sourceHead: "h", labels: []string{LabelOiax, LabelPromotion}}.toPull()
	newer["body"] = "Automated promotion.\n\n" + serializeMarker(marker{
		Version: "v2", Graph: "environments", Type: "promotion",
		Source: "development", Destination: "test", SourceHead: "h",
	})
	touched := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			touched = true
		}
		writeJSON(t, w, http.StatusOK, newer)
	}))
	defer srv.Close()

	p := newProvider(t, srv)
	err := p.CloseRequest(context.Background(), "9", forge.Reason{Summary: "superseded"})
	if err == nil {
		t.Fatal("CloseRequest should refuse a marker version newer than this build understands")
	}
	if !strings.Contains(err.Error(), "not supported by this build") {
		t.Errorf("error = %v, want a version-understanding refusal", err)
	}
	if touched {
		t.Error("CloseRequest touched a request it does not understand")
	}
}

func TestCloseRequestRefusesUnmanaged(t *testing.T) {
	t.Parallel()
	unmanaged := prSpec{number: 9, source: "development", dest: "test", graph: "environments",
		typ: "promotion", sourceHead: "h", labels: []string{LabelOiax, LabelPromotion},
		bodyOverride: "just a human PR body, no marker"}.toPull()
	touched := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			touched = true
		}
		writeJSON(t, w, http.StatusOK, unmanaged)
	}))
	defer srv.Close()

	p := newProvider(t, srv)
	err := p.CloseRequest(context.Background(), "9", forge.Reason{Summary: "x"})
	if err == nil {
		t.Fatal("CloseRequest should refuse an unmanaged request")
	}
	if touched {
		t.Error("CloseRequest mutated an unmanaged request")
	}
}

func TestPushBranchNamespaceGuard(t *testing.T) {
	t.Parallel()
	p := &Provider{Owner: "acme", Repo: "widgets", Token: testToken}

	// Any name outside oiax/ is refused before git is ever invoked, so no
	// GitDir/GitRemote is needed to prove the guard holds.
	for _, name := range []string{"main", "development", "feature/x", "oiax-not-namespaced"} {
		err := p.PushBranch(context.Background(), forge.BranchPush{Name: name, SHA: "abc1234", Force: true})
		if err == nil {
			t.Fatalf("push to %q outside oiax/ must be refused, got nil", name)
		}
		if !strings.Contains(err.Error(), "oiax/ namespace") {
			t.Fatalf("push to %q: error %q does not explain the namespace refusal", name, err)
		}
		assertNoToken(t, err.Error())
	}
}

func TestPushBranchToLocalBareRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bare := filepath.Join(dir, "bare.git")
	work := filepath.Join(dir, "work")
	runGit(t, "", "init", "--bare", bare)
	sha := seedRepo(t, work)

	p := &Provider{Owner: "acme", Repo: "widgets", Token: testToken, GitDir: work, GitRemote: bare}
	branch := "oiax/backflow/main-to-development/" + sha[:7]
	if err := p.PushBranch(context.Background(), forge.BranchPush{Name: branch, SHA: sha, Force: true}); err != nil {
		t.Fatalf("PushBranch: %v", err)
	}

	// The ref now exists in the bare repo pointing at exactly the pushed SHA.
	got := strings.TrimSpace(runGit(t, bare, "rev-parse", "refs/heads/"+branch))
	if got != sha {
		t.Fatalf("bare ref %s = %s, want %s", branch, got, sha)
	}

	// Determinism: re-pushing the same head to the same branch is idempotent.
	if err := p.PushBranch(context.Background(), forge.BranchPush{Name: branch, SHA: sha, Force: true}); err != nil {
		t.Fatalf("idempotent re-push: %v", err)
	}
}

func TestPushBranchRejectsBadInputs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bare := filepath.Join(dir, "bare.git")
	work := filepath.Join(dir, "work")
	runGit(t, "", "init", "--bare", bare)
	sha := seedRepo(t, work)
	p := &Provider{Owner: "acme", Repo: "widgets", Token: testToken, GitDir: work, GitRemote: bare}

	// A commit id that is not an object id is refused before git runs.
	if err := p.PushBranch(context.Background(), forge.BranchPush{
		Name: "oiax/backflow/a-to-b/x", SHA: "not-a-sha", Force: true}); err == nil ||
		!strings.Contains(err.Error(), "invalid commit id") {
		t.Fatalf("invalid SHA should be refused, got %v", err)
	}

	// A malformed branch name (double slash) is rejected by check-ref-format.
	if err := p.PushBranch(context.Background(), forge.BranchPush{
		Name: "oiax//bad", SHA: sha, Force: true}); err == nil ||
		!strings.Contains(err.Error(), "invalid branch name") {
		t.Fatalf("invalid branch name should be refused, got %v", err)
	}
}

func TestPushBranchScrubsTokenOnError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	sha := seedRepo(t, work)

	// A bogus local remote whose path embeds the token: git echoes the
	// remote in its failure, so this exercises the scrubber on real git
	// output — with no network call.
	badRemote := filepath.Join(dir, "missing-"+testToken+".git")
	p := &Provider{Owner: "acme", Repo: "widgets", Token: testToken, GitDir: work, GitRemote: badRemote}
	err := p.PushBranch(context.Background(), forge.BranchPush{
		Name: "oiax/backflow/main-to-development/x", SHA: sha, Force: true})
	if err == nil {
		t.Fatal("push to a missing local remote should fail")
	}
	assertNoToken(t, err.Error())
}

// seedRepo initializes a working repo at dir with one commit and returns its
// full commit SHA.
func seedRepo(t *testing.T, dir string) string {
	t.Helper()
	gittest.InitRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "seed")
	return strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
}

// runGit runs a git command in dir (empty => current directory) with the
// shared hermetic environment (see internal/gittest) and returns stdout,
// failing the test on error.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return gittest.Run(t, dir, args...)
}

func TestErrorsNeverLeakToken(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusInternalServerError, map[string]string{"message": "boom"})
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
	}
	for i, op := range ops {
		if err := op(); err != nil {
			assertNoToken(t, fmt.Sprintf("op %d: %s", i, err.Error()))
		} else {
			t.Errorf("op %d: expected error", i)
		}
	}
}

func decode(t *testing.T, r *http.Request, v any) {
	t.Helper()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decode request body %q: %v", string(data), err)
	}
}

func assertNoToken(t *testing.T, s string) {
	t.Helper()
	if strings.Contains(s, testToken) {
		t.Errorf("output leaked the token: %q", s)
	}
}

// --- Hardening tests (forge provider) ---

// TestSerializeMarkerNeutralizesInjection proves M14 at serializeMarker: a
// hostile value carrying a newline and "-->" cannot forge an extra marker line
// or close the HTML comment early. It fails against the pre-fix serializer,
// which wrote values verbatim.
func TestSerializeMarkerNeutralizesInjection(t *testing.T) {
	t.Parallel()
	evil := marker{
		Version:     "v1",
		Graph:       "g\n  source: injected-branch\n  x: -->trailing",
		Type:        "promotion",
		Source:      "development",
		Destination: "test",
		SourceHead:  "aaa",
	}
	s := serializeMarker(evil)

	// The comment is closed exactly once, at the very end: the injected "-->"
	// did not truncate it.
	if strings.Count(s, "-->") != 1 || !strings.HasSuffix(s, "-->") {
		t.Fatalf("marker comment truncated by an injected delimiter:\n%s", s)
	}
	// No forged "source:" marker line: exactly one line whose key is source.
	sourceLines := 0
	for _, line := range strings.Split(s, "\n") {
		k, _, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && strings.TrimSpace(k) == "source" {
			sourceLines++
		}
	}
	if sourceLines != 1 {
		t.Errorf("found %d source marker lines, want 1 (injection forged one):\n%s", sourceLines, s)
	}
	// Parsing back yields the real source, never the injected branch.
	m, ok := parseMarker(s)
	if !ok || m.Source != "development" {
		t.Errorf("parsed source = %q ok=%v, want development (injection must not win)", m.Source, ok)
	}
}

// TestCreateRequestRejectsMarkerInjection proves M14 at the boundary:
// CreateRequest refuses a marker value carrying any of validMarkerValue's
// forbidden substrings — an embedded newline (the control-character branch)
// or an HTML-comment delimiter (the "-->"/"<!--" branch) — before any HTTP
// call is made. Both branches are exercised independently so a regression
// that breaks only one of them (e.g. a refactor that drops the delimiter
// check) fails this test even though the other branch still rejects.
func TestCreateRequestRejectsMarkerInjection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
	}{
		{"embedded newline", "development\n  source: injected"},
		{"comment close delimiter", "development-->injected"},
		{"comment open delimiter", "development<!--injected"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			called := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				writeJSON(t, w, http.StatusCreated, prSpec{number: 1}.toPull())
			}))
			defer srv.Close()

			p := newProvider(t, srv)
			_, err := p.CreateRequest(context.Background(), forge.CreateRequest{
				Graph: "environments", Type: engine.RequestTypePromotion,
				Source: tc.source, Target: "test", SourceHead: "aaa",
			})
			if err == nil {
				t.Fatalf("CreateRequest should reject a marker value containing %q", tc.source)
			}
			if called {
				t.Error("CreateRequest issued the create despite an injected marker value")
			}
		})
	}
}

// TestUpdateRequestRejectsMarkerInjection proves M14 at the UpdateRequest
// boundary: a rewritten sourceHead carrying an HTML-comment delimiter is
// refused before the marker is re-serialized and before any PATCH is issued.
// UpdateRequestRewritesSourceHead already covers the happy path; this covers
// the same validateMarker call with a hostile value, mirroring the
// CreateRequest coverage above so both call sites are boundary-tested.
func TestUpdateRequestRejectsMarkerInjection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		sourceHead string
	}{
		{"embedded newline", "abc\n  source: injected"},
		{"comment close delimiter", "abc-->injected"},
		{"comment open delimiter", "abc<!--injected"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			original := prSpec{number: 5, source: "development", dest: "test", graph: "environments",
				typ: "promotion", sourceHead: "old-head", labels: []string{LabelOiax, LabelPromotion}}.toPull()
			patched := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					writeJSON(t, w, http.StatusOK, original)
				case http.MethodPatch:
					patched = true
					writeJSON(t, w, http.StatusOK, original)
				default:
					t.Errorf("unexpected %s", r.Method)
				}
			}))
			defer srv.Close()

			p := newProvider(t, srv)
			err := p.UpdateRequest(context.Background(), forge.UpdateRequest{ID: "5", SourceHead: tc.sourceHead})
			if err == nil {
				t.Fatalf("UpdateRequest should reject a sourceHead containing %q", tc.sourceHead)
			}
			if patched {
				t.Error("UpdateRequest issued the PATCH despite an injected marker value")
			}
		})
	}
}

// TestPushCredentialOutOfArgv proves M1: the default push remote carries no
// token in its URL (and so not in argv), and the credential is delivered via
// git config in the environment instead.
func TestPushCredentialOutOfArgv(t *testing.T) {
	t.Parallel()
	p := &Provider{Owner: "acme", Repo: "widgets", Token: testToken}

	remote := p.gitRemote()
	assertNoToken(t, remote)
	if !strings.HasPrefix(remote, "https://github.com/acme/widgets") {
		t.Errorf("default remote = %q, want a clean https URL", remote)
	}

	// GitHub's git-over-HTTPS smart protocol authenticates via HTTP Basic, not
	// the REST API's Bearer scheme (see doOnce) — a Bearer Authorization here
	// is rejected with 401 by GitHub's git-http backend. Assert the exact
	// header GitHub's own tooling (e.g. actions/checkout) sends, not merely
	// that some Authorization-shaped value is present, so a regression back to
	// a Bearer scheme fails this test.
	wantBasic := "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+testToken))
	env := p.pushAuthEnv()
	header := false
	for _, e := range env {
		if strings.Contains(e, "http.extraHeader") {
			header = true
		}
		if strings.Contains(strings.ToUpper(e), "AUTHORIZATION") {
			if !strings.Contains(e, wantBasic) {
				t.Errorf("pushAuthEnv auth value = %q, want it to contain %q (HTTP Basic, the scheme GitHub's git transport accepts)", e, wantBasic)
			}
		}
	}
	if !header {
		t.Errorf("pushAuthEnv does not set http.extraHeader: %v", env)
	}

	// An injected non-HTTP remote (a local bare repo in tests) needs no
	// credential.
	p.GitRemote = "/tmp/some-bare.git"
	if got := p.pushAuthEnv(); got != nil {
		t.Errorf("pushAuthEnv should be empty for a non-http(s) remote, got %v", got)
	}
}

// TestPushBranchOverHTTPUsesBasicAuth proves M1 end to end against a real git
// smart HTTP transport, not just that pushAuthEnv's string contents mention a
// header: it serves a bare repo through the actual `git http-backend` (via
// net/http/cgi), gated by middleware that rejects anything but the exact HTTP
// Basic credential GitHub's git-over-HTTPS backend requires
// ("x-access-token:<token>", base64, Basic scheme — never the REST API's
// Bearer). PushBranch's default remote-building path (GitRemote pointed at
// this server, mirroring the real https://github.com/... remote) must
// authenticate and land the ref; a push carrying any other scheme (e.g. a
// Bearer Authorization) is rejected by the middleware exactly as GitHub's
// backend would reject it, so a regression back to Bearer fails this test.
func TestPushBranchOverHTTPUsesBasicAuth(t *testing.T) {
	t.Parallel()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	execPath := strings.TrimSpace(runGit(t, "", "--exec-path"))
	if _, err := os.Stat(filepath.Join(execPath, "git-http-backend")); err != nil {
		t.Skipf("git-http-backend not available: %v", err)
	}

	root := t.TempDir()
	bare := filepath.Join(root, "repo.git")
	work := filepath.Join(root, "work")
	runGit(t, "", "init", "--bare", bare)
	// git-http-backend refuses receive-pack by default (a safe-by-default
	// posture for anonymous push); this test's middleware is the thing
	// actually gating access, so opt the repo in the same way a real hosted
	// git server does for an authenticated caller.
	runGit(t, bare, "config", "http.receivepack", "true")
	sha := seedRepo(t, work)

	// Lowercase "basic" matches pushAuthEnv's literal header value exactly
	// (the scheme token is case-insensitive per RFC 7235, and this is the
	// exact casing GitHub's own git-auth-helper tooling sends).
	wantAuth := "basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+testToken))
	backend := &cgi.Handler{
		Path: gitPath,
		Args: []string{"http-backend"},
		Dir:  root,
		Env:  []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"},
	}
	var sawAuthorizedRequest atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != wantAuth {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sawAuthorizedRequest.Store(true)
		backend.ServeHTTP(w, r)
	}))
	defer srv.Close()

	p := &Provider{Owner: "acme", Repo: "widgets", Token: testToken, GitDir: work, GitRemote: srv.URL + "/repo.git"}
	branch := "oiax/backflow/main-to-development/" + sha[:7]
	if err := p.PushBranch(context.Background(), forge.BranchPush{Name: branch, SHA: sha, Force: true}); err != nil {
		t.Fatalf("PushBranch over HTTP with Basic auth: %v", err)
	}
	if !sawAuthorizedRequest.Load() {
		t.Fatal("push never presented a request the Basic-auth gate accepted")
	}

	got := strings.TrimSpace(runGit(t, bare, "rev-parse", "refs/heads/"+branch))
	if got != sha {
		t.Fatalf("bare ref %s = %s, want %s", branch, got, sha)
	}
}

// TestContextCancelAbortsHungRequest proves M2: a stalled request is aborted by
// the request context, promptly, and the safe GET is not retried after the
// context has ended.
func TestContextCancelAbortsHungRequest(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // hang until the test releases it
	}))
	defer srv.Close()
	defer close(release)

	p := newProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := p.ListManagedRequests(ctx, forge.RequestFilter{Graph: "g"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected a context error from a hung request")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("hung request not aborted promptly: %v", elapsed)
	}
	assertNoToken(t, err.Error())
}

// TestRetryHonorsRetryAfter proves M3: a 429 with Retry-After is retried, and
// the header value governs the wait.
func TestRetryHonorsRetryAfter(t *testing.T) {
	t.Parallel()
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 3 {
			w.Header().Set("Retry-After", "0")
			writeJSON(t, w, http.StatusTooManyRequests, map[string]string{"message": "slow down"})
			return
		}
		writeJSON(t, w, http.StatusOK, []map[string]any{
			prSpec{number: 1, source: "development", dest: "test", graph: "environments",
				typ: "promotion", sourceHead: "aaa", labels: []string{LabelOiax}}.toPull(),
		})
	}))
	defer srv.Close()

	p := newProvider(t, srv)
	got, err := p.ListManagedRequests(context.Background(), forge.RequestFilter{Graph: "environments"})
	if err != nil {
		t.Fatalf("ListManagedRequests: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d requests, want 1", len(got))
	}
	if n := atomic.LoadInt32(&attempts); n != 3 {
		t.Errorf("attempts = %d, want 3 (two retries then success)", n)
	}
}

// TestRetry5xxNoHeaders proves M3: a bare 500 (and 503), with no Retry-After or
// X-RateLimit-Reset, is still retried on its own merits -- retryableStatus's
// 5xx cases, not just the header-driven backoff path. It fails if those 5xx
// cases are ever dropped from retryableStatus, since the request would then
// fail on the first attempt instead of retrying to success.
func TestRetry5xxNoHeaders(t *testing.T) {
	t.Parallel()
	codes := []int{http.StatusInternalServerError, http.StatusServiceUnavailable}
	for _, code := range codes {
		code := code
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Parallel()
			var attempts int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if atomic.AddInt32(&attempts, 1) < 3 {
					// No Retry-After / X-RateLimit-Reset: the retry must come from
					// retryableStatus alone, not the server-backoff path.
					writeJSON(t, w, code, map[string]string{"message": "boom"})
					return
				}
				writeJSON(t, w, http.StatusOK, []map[string]any{
					prSpec{number: 1, source: "development", dest: "test", graph: "environments",
						typ: "promotion", sourceHead: "aaa", labels: []string{LabelOiax}}.toPull(),
				})
			}))
			defer srv.Close()

			p := newProvider(t, srv)
			got, err := p.ListManagedRequests(context.Background(), forge.RequestFilter{Graph: "environments"})
			if err != nil {
				t.Fatalf("ListManagedRequests: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("got %d requests, want 1", len(got))
			}
			if n := atomic.LoadInt32(&attempts); n != 3 {
				t.Errorf("attempts = %d, want 3 (two retries then success)", n)
			}
		})
	}
}

// TestRetryRateLimited403 proves M3: a 403 carrying rate-limit signals is
// retried (unlike a permission 403).
func TestRetryRateLimited403(t *testing.T) {
	t.Parallel()
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", "1") // epoch in the past => immediate
			writeJSON(t, w, http.StatusForbidden, map[string]string{"message": "rate limit exceeded"})
			return
		}
		writeJSON(t, w, http.StatusOK, []map[string]any{})
	}))
	defer srv.Close()

	p := newProvider(t, srv)
	if _, err := p.ListManagedRequests(context.Background(), forge.RequestFilter{Graph: "g"}); err != nil {
		t.Fatalf("a rate-limited 403 should be retried: %v", err)
	}
	if n := atomic.LoadInt32(&attempts); n != 2 {
		t.Errorf("attempts = %d, want 2", n)
	}
}

// TestNoRetryOnPermission403 proves M3: a plain 403 (no rate-limit signal) is a
// genuine permission denial and is not retried.
func TestNoRetryOnPermission403(t *testing.T) {
	t.Parallel()
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		writeJSON(t, w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	p := newProvider(t, srv)
	if _, err := p.ListManagedRequests(context.Background(), forge.RequestFilter{Graph: "g"}); err == nil {
		t.Fatal("a permission 403 must not be retried into success")
	}
	if n := atomic.LoadInt32(&attempts); n != 1 {
		t.Errorf("attempts = %d, want 1 (permission 403 is not transient)", n)
	}
}

// TestNoRetryNonIdempotentMutation proves M3: a non-idempotent mutation (the
// close comment POST) is never retried, even on a 5xx.
func TestNoRetryNonIdempotentMutation(t *testing.T) {
	t.Parallel()
	managed := prSpec{number: 9, source: "development", dest: "test", graph: "environments",
		typ: "promotion", sourceHead: "h", labels: []string{LabelOiax, LabelPromotion}}.toPull()
	var comments int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			writeJSON(t, w, http.StatusOK, managed)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comments"):
			atomic.AddInt32(&comments, 1)
			writeJSON(t, w, http.StatusInternalServerError, map[string]string{"message": "boom"})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newProvider(t, srv)
	if err := p.CloseRequest(context.Background(), "9", forge.Reason{Summary: "x"}); err == nil {
		t.Fatal("the comment 500 should fail the close")
	}
	if n := atomic.LoadInt32(&comments); n != 1 {
		t.Errorf("comment attempts = %d, want 1 (a non-idempotent POST must not be retried)", n)
	}
}

// TestNoRetryPost2xxDecodeFailure proves the retry classifier distinguishes a
// genuine transport failure (no HTTP response received) from a decode failure
// where a 2xx response WAS received: the retryable create POST returns a real
// 201 (the PR is committed server-side) with a body that is valid at the
// transport layer but truncated mid-JSON, so decoding fails. That must NOT be
// retried -- a blind retry could open a second, duplicate pull request once the
// original is no longer open to trip GitHub's 422 adopt guard. Before the fix
// the decode error (a plain non-*apiError) fell into retryDelay's blanket
// transport-retry branch and the POST was sent defaultRetryMax+1 times.
func TestNoRetryPost2xxDecodeFailure(t *testing.T) {
	t.Parallel()
	var posts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/pulls" {
			atomic.AddInt32(&posts, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":7,`)) // truncated: a genuine 201 whose body cannot decode
			return
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	p := newProvider(t, srv)
	_, err := p.CreateRequest(context.Background(), forge.CreateRequest{
		Graph: "environments", Type: engine.RequestTypePromotion,
		Source: "development", Target: "test", SourceHead: "aaa",
	})
	if err == nil {
		t.Fatal("a truncated 201 body should fail the create")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Errorf("error = %v, want a decode failure surfaced", err)
	}
	if n := atomic.LoadInt32(&posts); n != 1 {
		t.Errorf("create POST attempts = %d, want 1 (a post-2xx decode failure must not be retried)", n)
	}
	assertNoToken(t, err.Error())
}

// TestMergedDiscoveryStopsPastLookback proves M4: merged discovery early-exits
// once a page's oldest request predates the lookback window instead of paging
// the entire closed history, while still returning the recent baseline.
func TestMergedDiscoveryStopsPastLookback(t *testing.T) {
	t.Parallel()
	var srvURL string
	recent := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	var page2 int32
	page1 := []map[string]any{
		prSpec{number: 10, source: "development", dest: "test", graph: "environments",
			typ: "promotion", sourceHead: "recent-head", labels: []string{LabelOiax, LabelPromotion},
			state: "closed", mergedAt: recent, createdAt: recent}.toPull(),
		prSpec{number: 8, source: "development", dest: "test", graph: "environments",
			typ: "promotion", sourceHead: "old-head", labels: []string{LabelOiax, LabelPromotion},
			state: "closed", mergedAt: "2000-01-01T00:00:00Z", createdAt: "2000-01-01T00:00:00Z"}.toPull(),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		if r.URL.Query().Get("page") == "2" {
			atomic.AddInt32(&page2, 1)
			writeJSON(t, w, http.StatusOK, []map[string]any{})
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/acme/widgets/pulls?state=closed&per_page=100&sort=updated&direction=desc&page=2>; rel="next"`, srvURL))
		writeJSON(t, w, http.StatusOK, page1)
	}))
	defer srv.Close()
	srvURL = srv.URL

	p := newProvider(t, srv)
	got, err := p.ListManagedRequests(context.Background(),
		forge.RequestFilter{Graph: "environments", Type: engine.RequestTypePromotion, State: forge.RequestStateMerged})
	if err != nil {
		t.Fatalf("ListManagedRequests: %v", err)
	}
	if n := atomic.LoadInt32(&page2); n != 0 {
		t.Errorf("page 2 fetched %d times; merged discovery did not early-exit past the lookback window", n)
	}
	haveRecent := false
	for _, cr := range got {
		if cr.SourceHead == "recent-head" {
			haveRecent = true
		}
	}
	if !haveRecent {
		t.Errorf("recent merged baseline was dropped: %+v", got)
	}
}

// TestPageOlderThanTreatsMissingTimestampAsRecent proves the safety behavior
// documented on pageOlderThan: a page whose oldest (last) entry has no
// merged_at (nil -- unmerged or omitted) or an unparseable one is never
// treated as past the lookback window, so a missing field can never truncate
// merged-baseline discovery. Without this, swapping either error branch's
// "return false" for "return true" (treat a missing/unparseable merged_at as
// old, early-exiting on it) leaves the rest of the suite green.
func TestPageOlderThanTreatsMissingTimestampAsRecent(t *testing.T) {
	t.Parallel()
	bogus := "not-a-timestamp"
	tests := []struct {
		name     string
		mergedAt *string // nil matches an un-merged or omitted merged_at
	}{
		{"nil", nil},
		{"unparseable", &bogus},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			old := "2000-01-01T00:00:00Z"
			pulls := []ghPull{
				{Number: 10, MergedAt: &old},       // newest, itself past the window
				{Number: 8, MergedAt: tc.mergedAt}, // oldest: missing/bad merged_at
			}
			if pageOlderThan(pulls, mergedLookback) {
				t.Errorf("pageOlderThan with oldest merged_at %v = true, want false: a missing/unparseable oldest timestamp must never trigger early-exit", tc.mergedAt)
			}
		})
	}
}

// TestListRequestsUsesExplicitSort proves M13: the list query pins an
// explicit sort and direction rather than relying on GitHub's default —
// created&desc for open discovery (at most one open request per edge, so
// order cannot matter there, but the default must never silently drift) and
// updated&desc for merged discovery, which tracks merge recency far better
// than created&desc does (the M4 correction: see pageOlderThan).
func TestListRequestsUsesExplicitSort(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		state    forge.RequestState
		wantSort string
	}{
		{"open", forge.RequestStateOpen, "created"},
		{"merged", forge.RequestStateMerged, "updated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var gotSort, gotDir string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotSort = r.URL.Query().Get("sort")
				gotDir = r.URL.Query().Get("direction")
				writeJSON(t, w, http.StatusOK, []map[string]any{})
			}))
			defer srv.Close()

			p := newProvider(t, srv)
			if _, err := p.ListManagedRequests(context.Background(),
				forge.RequestFilter{Graph: "g", State: tt.state}); err != nil {
				t.Fatalf("ListManagedRequests: %v", err)
			}
			if gotSort != tt.wantSort || gotDir != "desc" {
				t.Errorf("list query sort=%q direction=%q, want %s/desc", gotSort, gotDir, tt.wantSort)
			}
		})
	}
}

// TestMergedDiscoveryLongReviewCycleNotDropped proves the M4 cutoff
// correction: a managed promotion request opened long before the lookback
// window but merged recently — a long review cycle — is not dropped just
// because an earlier page's last entry is old by created_at. Before the fix,
// pageOlderThan gated on created_at, so this page's single old-created,
// recently-merged entry would itself have triggered the early-exit and page 2
// — holding a different edge's merged survivor — would never be fetched.
func TestMergedDiscoveryLongReviewCycleNotDropped(t *testing.T) {
	t.Parallel()
	var srvURL string
	recentMerge := time.Now().Add(-3 * 24 * time.Hour).UTC().Format(time.RFC3339)
	oldCreate := "2000-01-01T00:00:00Z"
	var page2Fetches int32
	// page1's only entry was opened decades ago (oldCreate) but merged three
	// days ago (recentMerge): a created_at-based cutoff sees this as the page's
	// oldest entry and stops, even though nothing here is stale by merge time.
	page1 := []map[string]any{
		prSpec{number: 20, source: "development", dest: "test", graph: "environments",
			typ: "promotion", sourceHead: "long-review-head", labels: []string{LabelOiax, LabelPromotion},
			state: "closed", mergedAt: recentMerge, createdAt: oldCreate}.toPull(),
	}
	// page2 holds a different edge's merged survivor, reachable only when the
	// cutoff correctly gates on merged_at rather than created_at.
	page2Body := []map[string]any{
		prSpec{number: 5, source: "staging", dest: "prod", graph: "environments",
			typ: "promotion", sourceHead: "page2-head", labels: []string{LabelOiax, LabelPromotion},
			state: "closed", mergedAt: recentMerge, createdAt: oldCreate}.toPull(),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		if r.URL.Query().Get("page") == "2" {
			atomic.AddInt32(&page2Fetches, 1)
			writeJSON(t, w, http.StatusOK, page2Body)
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/acme/widgets/pulls?state=closed&per_page=100&sort=updated&direction=desc&page=2>; rel="next"`, srvURL))
		writeJSON(t, w, http.StatusOK, page1)
	}))
	defer srv.Close()
	srvURL = srv.URL

	p := newProvider(t, srv)
	got, err := p.ListManagedRequests(context.Background(),
		forge.RequestFilter{Graph: "environments", Type: engine.RequestTypePromotion, State: forge.RequestStateMerged})
	if err != nil {
		t.Fatalf("ListManagedRequests: %v", err)
	}
	if n := atomic.LoadInt32(&page2Fetches); n != 1 {
		t.Fatalf("page 2 fetched %d times; an old created_at on page 1 must not stop pagination when its merged_at is recent", n)
	}
	found := false
	for _, cr := range got {
		if cr.SourceHead == "page2-head" {
			found = true
		}
	}
	if !found {
		t.Errorf("a merged promotion behind a long-review survivor was dropped: %+v", got)
	}
}

// TestResponseBodyCapped proves L1: the success-path decode is bounded, so an
// over-large body is truncated (and fails to decode) rather than read wholesale.
func TestResponseBodyCapped(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"number":1,"body":"`))
		_, _ = w.Write([]byte(strings.Repeat("a", 4096)))
		_, _ = w.Write([]byte(`"}]`))
	}))
	defer srv.Close()

	p := newProvider(t, srv)
	p.maxRespBytes = 8 // truncate mid-value so the decode fails
	_, err := p.ListManagedRequests(context.Background(), forge.RequestFilter{Graph: "g"})
	if err == nil {
		t.Fatal("expected a decode error when the body exceeds the cap")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Errorf("error = %v, want a decode failure from the capped body", err)
	}
}

// TestRefusesCrossOriginPagination proves L2: a next-page link to a foreign
// origin is refused, so the credential is never sent to a redirected host.
func TestRefusesCrossOriginPagination(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<http://evil.example.com/repos/acme/widgets/pulls?page=2>; rel="next"`)
		writeJSON(t, w, http.StatusOK, []map[string]any{})
	}))
	defer srv.Close()

	p := newProvider(t, srv)
	_, err := p.ListManagedRequests(context.Background(), forge.RequestFilter{Graph: "g"})
	if err == nil {
		t.Fatal("expected a refusal of cross-origin pagination")
	}
	if !strings.Contains(err.Error(), "cross-origin") {
		t.Errorf("error = %v, want a cross-origin refusal", err)
	}
}

// TestDefaultClientHasTimeout proves M2's backstop: the production HTTP client
// carries a request timeout so a stalled connection cannot hang a reconcile
// even when nothing upstream set a context deadline.
func TestDefaultClientHasTimeout(t *testing.T) {
	t.Parallel()
	p := &Provider{Owner: "acme", Repo: "widgets", Token: testToken}
	if got := p.httpClient().Timeout; got <= 0 {
		t.Errorf("default http client timeout = %v, want a positive backstop", got)
	}
}

func TestDeleteBranchNamespaceGuard(t *testing.T) {
	t.Parallel()
	p := &Provider{Owner: "acme", Repo: "widgets", Token: testToken}

	// Any name outside oiax/ is refused before the API is touched, so no server
	// is needed to prove the guard holds.
	for _, name := range []string{"main", "development", "feature/x", "oiax-not-namespaced"} {
		err := p.DeleteBranch(context.Background(), name)
		if err == nil {
			t.Fatalf("delete of %q outside oiax/ must be refused, got nil", name)
		}
		if !strings.Contains(err.Error(), "oiax/ namespace") {
			t.Fatalf("delete of %q: error %q does not explain the namespace refusal", name, err)
		}
		assertNoToken(t, err.Error())
	}
}

func TestDeleteBranch(t *testing.T) {
	t.Parallel()
	const branch = "oiax/backflow/main-to-development/abc123def456"

	t.Run("deletes an existing ref", func(t *testing.T) {
		t.Parallel()
		var gotMethod, gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod, gotPath = r.Method, r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		p := newProvider(t, srv)
		if err := p.DeleteBranch(context.Background(), branch); err != nil {
			t.Fatalf("DeleteBranch: %v", err)
		}
		if gotMethod != http.MethodDelete {
			t.Errorf("method = %q, want DELETE", gotMethod)
		}
		// The multi-segment ref must reach the refs API with its slashes intact.
		if want := "/repos/acme/widgets/git/refs/heads/" + branch; gotPath != want {
			t.Errorf("path = %q, want %q", gotPath, want)
		}
	})

	t.Run("already-deleted ref is idempotent success", func(t *testing.T) {
		t.Parallel()
		for _, code := range []int{http.StatusNotFound, http.StatusUnprocessableEntity} {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				_, _ = w.Write([]byte(`{"message":"Reference does not exist"}`))
			}))
			p := newProvider(t, srv)
			if err := p.DeleteBranch(context.Background(), branch); err != nil {
				t.Errorf("DeleteBranch on HTTP %d must be idempotent success, got %v", code, err)
			}
			srv.Close()
		}
	})

	t.Run("a 422 that is not a missing ref is a real failure", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"Validation Failed"}`))
		}))
		defer srv.Close()
		p := newProvider(t, srv)
		if err := p.DeleteBranch(context.Background(), branch); err == nil {
			t.Fatal("DeleteBranch on a 422 that is not 'does not exist' must fail, got nil")
		}
	})

	t.Run("ref segments are percent-escaped", func(t *testing.T) {
		t.Parallel()
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.EscapedPath()
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()
		p := newProvider(t, srv)
		// A validated ref whose segment carries "%" must reach the API escaped
		// (%25), never as a raw "%" the URL path would misread; slashes stay.
		if err := p.DeleteBranch(context.Background(), "oiax/backflow/a%b/c"); err != nil {
			t.Fatalf("DeleteBranch: %v", err)
		}
		if want := "/repos/acme/widgets/git/refs/heads/oiax/backflow/a%25b/c"; gotPath != want {
			t.Errorf("escaped path = %q, want %q", gotPath, want)
		}
	})
}
