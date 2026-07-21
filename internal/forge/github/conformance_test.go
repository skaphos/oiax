package github

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/forge/forgetest"
)

// TestGitHubConformance runs the shared forge conformance battery against the
// GitHub provider wired to a stateful in-memory fake of the GitHub REST API.
func TestGitHubConformance(t *testing.T) {
	forgetest.Run(t, func(t *testing.T) forge.Forge {
		fake := newGHFake()
		srv := httptest.NewServer(fake.handler(t))
		t.Cleanup(srv.Close)
		return &Provider{
			Owner: "acme", Repo: "widgets", Token: testToken,
			BaseURL: srv.URL, HTTP: srv.Client(),
		}
	})
}

// ghFake is a minimal stateful GitHub REST API: enough of pull requests and
// issues (labels, state, body) for the conformance battery to drive the provider
// through the interface.
type ghFake struct {
	mu     sync.Mutex
	next   int
	pulls  map[int]*ghFakePull
	issues map[int]*ghFakeIssue
}

type ghFakePull struct {
	number                  int
	title, body             string
	headRef, baseRef, state string
	labels                  []string
}

type ghFakeIssue struct {
	number int
	body   string
	state  string
	labels []string
}

func newGHFake() *ghFake {
	return &ghFake{pulls: map[int]*ghFakePull{}, issues: map[int]*ghFakeIssue{}}
}

func (g *ghFake) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		p := strings.TrimPrefix(r.URL.Path, "/repos/acme/widgets")
		switch {
		case p == "/pulls" && r.Method == http.MethodPost:
			g.createPull(t, w, r)
		case p == "/pulls" && r.Method == http.MethodGet:
			g.listPulls(t, w, r)
		case strings.HasSuffix(p, "/labels") && r.Method == http.MethodPost:
			g.addLabels(t, w, r, p)
		case strings.HasSuffix(p, "/comments") && r.Method == http.MethodPost:
			writeJSON(t, w, http.StatusCreated, map[string]any{"id": 1})
		case strings.HasPrefix(p, "/pulls/") && r.Method == http.MethodGet:
			g.getPull(t, w, p)
		case strings.HasPrefix(p, "/pulls/") && r.Method == http.MethodPatch:
			g.patchPull(t, w, r, p)
		case p == "/issues" && r.Method == http.MethodPost:
			g.createIssue(t, w, r)
		case p == "/issues" && r.Method == http.MethodGet:
			g.listIssues(t, w, r)
		case strings.HasPrefix(p, "/issues/") && r.Method == http.MethodGet:
			g.getIssue(t, w, p)
		case strings.HasPrefix(p, "/issues/") && r.Method == http.MethodPatch:
			g.patchIssue(t, w, r, p)
		default:
			t.Fatalf("gh fake: unhandled %s %s", r.Method, r.URL.Path)
		}
	}
}

func ghNum(p string) int {
	segs := strings.Split(strings.Trim(p, "/"), "/")
	if len(segs) >= 2 {
		n, _ := strconv.Atoi(segs[1])
		return n
	}
	return 0
}

func (g *ghFake) createPull(t *testing.T, w http.ResponseWriter, r *http.Request) {
	var body struct{ Title, Head, Base, Body string }
	decode(t, r, &body)
	for _, pl := range g.pulls {
		if pl.state == "open" && pl.headRef == body.Head && pl.baseRef == body.Base {
			writeJSON(t, w, http.StatusUnprocessableEntity, map[string]any{"message": "A pull request already exists"})
			return
		}
	}
	g.next++
	pl := &ghFakePull{number: g.next, title: body.Title, body: body.Body, headRef: body.Head, baseRef: body.Base, state: "open"}
	g.pulls[pl.number] = pl
	writeJSON(t, w, http.StatusCreated, g.pullJSON(pl))
}

func (g *ghFake) listPulls(t *testing.T, w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	nums := make([]int, 0, len(g.pulls))
	for n := range g.pulls {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	out := []map[string]any{}
	for _, n := range nums {
		if pl := g.pulls[n]; state == "" || pl.state == state {
			out = append(out, g.pullJSON(pl))
		}
	}
	writeJSON(t, w, http.StatusOK, out)
}

func (g *ghFake) getPull(t *testing.T, w http.ResponseWriter, p string) {
	pl, ok := g.pulls[ghNum(p)]
	if !ok {
		writeJSON(t, w, http.StatusNotFound, map[string]any{"message": "not found"})
		return
	}
	writeJSON(t, w, http.StatusOK, g.pullJSON(pl))
}

func (g *ghFake) patchPull(t *testing.T, w http.ResponseWriter, r *http.Request, p string) {
	pl := g.pulls[ghNum(p)]
	var patch map[string]any
	decode(t, r, &patch)
	if b, ok := patch["body"].(string); ok {
		pl.body = b
	}
	if s, ok := patch["state"].(string); ok {
		pl.state = s
	}
	writeJSON(t, w, http.StatusOK, g.pullJSON(pl))
}

func (g *ghFake) addLabels(t *testing.T, w http.ResponseWriter, r *http.Request, p string) {
	var body struct{ Labels []string }
	decode(t, r, &body)
	// The /labels route applies to both pulls and issues; a conflict artifact
	// is an issue that gets its identity labels through this endpoint (create
	// no longer sends them inline).
	if pl, ok := g.pulls[ghNum(p)]; ok {
		pl.labels = append(pl.labels, body.Labels...)
	}
	if iss, ok := g.issues[ghNum(p)]; ok {
		iss.labels = append(iss.labels, body.Labels...)
	}
	writeJSON(t, w, http.StatusOK, []any{})
}

func (g *ghFake) createIssue(t *testing.T, w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title, Body string
		Labels      []string
	}
	decode(t, r, &body)
	g.next++
	iss := &ghFakeIssue{number: g.next, body: body.Body, state: "open", labels: body.Labels}
	g.issues[iss.number] = iss
	writeJSON(t, w, http.StatusCreated, g.issueJSON(iss))
}

func (g *ghFake) listIssues(t *testing.T, w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	nums := make([]int, 0, len(g.issues))
	for n := range g.issues {
		nums = append(nums, n)
	}
	// Return descending so the provider's ascending sort is exercised, not the
	// fake's storage order.
	sort.Sort(sort.Reverse(sort.IntSlice(nums)))
	out := []map[string]any{}
	for _, n := range nums {
		if iss := g.issues[n]; state == "" || iss.state == state {
			out = append(out, g.issueJSON(iss))
		}
	}
	writeJSON(t, w, http.StatusOK, out)
}

func (g *ghFake) getIssue(t *testing.T, w http.ResponseWriter, p string) {
	iss, ok := g.issues[ghNum(p)]
	if !ok {
		writeJSON(t, w, http.StatusNotFound, map[string]any{"message": "not found"})
		return
	}
	writeJSON(t, w, http.StatusOK, g.issueJSON(iss))
}

func (g *ghFake) patchIssue(t *testing.T, w http.ResponseWriter, r *http.Request, p string) {
	iss := g.issues[ghNum(p)]
	var patch map[string]any
	decode(t, r, &patch)
	if b, ok := patch["body"].(string); ok {
		iss.body = b
	}
	if s, ok := patch["state"].(string); ok {
		iss.state = s
	}
	writeJSON(t, w, http.StatusOK, g.issueJSON(iss))
}

func (g *ghFake) pullJSON(pl *ghFakePull) map[string]any {
	return map[string]any{
		"number":     pl.number,
		"state":      pl.state,
		"title":      pl.title,
		"body":       pl.body,
		"head":       map[string]any{"ref": pl.headRef, "sha": "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", "repo": map[string]any{"full_name": "acme/widgets"}},
		"base":       map[string]any{"ref": pl.baseRef, "repo": map[string]any{"full_name": "acme/widgets"}},
		"labels":     labelObjs(pl.labels),
		"merged_at":  nil,
		"created_at": "2026-01-01T00:00:00Z",
		"user":       map[string]any{"login": "human"},
	}
}

func (g *ghFake) issueJSON(iss *ghFakeIssue) map[string]any {
	return map[string]any{
		"number":       iss.number,
		"state":        iss.state,
		"body":         iss.body,
		"labels":       labelObjs(iss.labels),
		"pull_request": nil,
	}
}

func labelObjs(labels []string) []map[string]string {
	out := make([]map[string]string, 0, len(labels))
	for _, l := range labels {
		out = append(out, map[string]string{"name": l})
	}
	return out
}
