package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
