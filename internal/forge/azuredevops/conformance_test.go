package azuredevops

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

// TestAzureDevOpsConformance runs the shared forge conformance battery against
// the Azure DevOps provider wired to a stateful in-memory fake of the Azure
// DevOps REST API.
func TestAzureDevOpsConformance(t *testing.T) {
	forgetest.Run(t, func(t *testing.T) forge.Forge {
		fake := newADOFake()
		srv := httptest.NewServer(fake.handler(t))
		t.Cleanup(srv.Close)
		return &Provider{
			Repo:    Repo{Organization: "acme", Project: "platform", Name: "deploy"},
			Token:   testToken,
			BaseURL: srv.URL,
			HTTP:    srv.Client(),
		}
	})
}

// adoFake is a minimal stateful Azure DevOps REST API: enough of pull requests
// (with properties), work items, and their states for the conformance battery.
// It truncates list-response descriptions to 400 characters, as Azure DevOps
// does, so the marker-first storage the provider relies on is genuinely
// exercised.
type adoFake struct {
	mu    sync.Mutex
	next  int
	pulls map[int]*adoFakePull
	wis   map[int]*adoFakeWI
}

type adoFakePull struct {
	id                                        int
	description, sourceRef, targetRef, status string
	props                                     map[string]string
}

type adoFakeWI struct {
	id     int
	fields map[string]string
}

func newADOFake() *adoFake {
	return &adoFake{pulls: map[int]*adoFakePull{}, wis: map[int]*adoFakeWI{}}
}

const adoGit = "/git/repositories/deploy"

func (g *adoFake) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		p := strings.TrimPrefix(r.URL.Path, "/platform/_apis")
		switch {
		case p == adoGit+"/pullrequests" && r.Method == http.MethodPost:
			g.createPull(t, w, r)
		case p == adoGit+"/pullrequests" && r.Method == http.MethodGet:
			g.listPulls(t, w, r)
		case strings.HasSuffix(p, "/properties") && r.Method == http.MethodGet:
			g.getProps(t, w, p)
		case strings.HasSuffix(p, "/properties") && r.Method == http.MethodPatch:
			g.patchProps(t, w, r, p)
		case strings.HasSuffix(p, "/threads") && r.Method == http.MethodPost:
			writeJSON(t, w, http.StatusOK, map[string]any{"id": 1})
		case strings.HasPrefix(p, adoGit+"/pullrequests/") && r.Method == http.MethodGet:
			g.getPull(t, w, p)
		case strings.HasPrefix(p, adoGit+"/pullrequests/") && r.Method == http.MethodPatch:
			g.patchPull(t, w, r, p)
		case p == "/wit/wiql" && r.Method == http.MethodPost:
			g.wiql(t, w)
		case p == "/wit/workitems" && r.Method == http.MethodGet:
			g.batchWI(t, w, r)
		case strings.HasPrefix(p, "/wit/workitems/$") && r.Method == http.MethodPost:
			g.createWI(t, w, r)
		case strings.HasPrefix(p, "/wit/workitemtypes/") && strings.HasSuffix(p, "/states") && r.Method == http.MethodGet:
			writeJSON(t, w, http.StatusOK, map[string]any{"value": []map[string]any{
				{"name": "To Do", "category": "Proposed"},
				{"name": "Done", "category": "Completed"},
			}})
		case strings.HasPrefix(p, "/wit/workitems/") && r.Method == http.MethodGet:
			g.getWI(t, w, p)
		case strings.HasPrefix(p, "/wit/workitems/") && r.Method == http.MethodPatch:
			g.patchWI(t, w, r, p)
		default:
			t.Fatalf("ado fake: unhandled %s %s", r.Method, r.URL.Path)
		}
	}
}

// adoTailNum returns the numeric id segment that follows resource in p, e.g.
// "pullrequests" in .../pullrequests/42/properties → 42.
func adoTailNum(p, resource string) int {
	i := strings.Index(p, resource+"/")
	if i < 0 {
		return 0
	}
	rest := p[i+len(resource)+1:]
	if j := strings.IndexByte(rest, '/'); j >= 0 {
		rest = rest[:j]
	}
	n, _ := strconv.Atoi(rest)
	return n
}

func (g *adoFake) createPull(t *testing.T, w http.ResponseWriter, r *http.Request) {
	var body struct{ SourceRefName, TargetRefName, Title, Description string }
	decode(t, r, &body)
	for _, pl := range g.pulls {
		if pl.status == "active" && pl.sourceRef == body.SourceRefName && pl.targetRef == body.TargetRefName {
			writeJSON(t, w, http.StatusConflict, map[string]any{
				"message": "TF401179: An active pull request already exists.",
				"typeKey": "GitPullRequestExistsException",
			})
			return
		}
	}
	g.next++
	pl := &adoFakePull{id: g.next, description: body.Description, sourceRef: body.SourceRefName, targetRef: body.TargetRefName, status: "active", props: map[string]string{}}
	g.pulls[pl.id] = pl
	writeJSON(t, w, http.StatusCreated, map[string]any{"pullRequestId": pl.id})
}

func (g *adoFake) listPulls(t *testing.T, w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("searchCriteria.status")
	ids := make([]int, 0, len(g.pulls))
	for id := range g.pulls {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	out := []map[string]any{}
	for _, id := range ids {
		if pl := g.pulls[id]; status == "" || pl.status == status {
			out = append(out, g.pullJSON(pl, true))
		}
	}
	writeJSON(t, w, http.StatusOK, map[string]any{"value": out, "count": len(out)})
}

func (g *adoFake) getPull(t *testing.T, w http.ResponseWriter, p string) {
	pl, ok := g.pulls[adoTailNum(p, "pullrequests")]
	if !ok {
		writeJSON(t, w, http.StatusNotFound, map[string]any{"message": "not found"})
		return
	}
	writeJSON(t, w, http.StatusOK, g.pullJSON(pl, false))
}

func (g *adoFake) patchPull(t *testing.T, w http.ResponseWriter, r *http.Request, p string) {
	pl := g.pulls[adoTailNum(p, "pullrequests")]
	var patch map[string]any
	decode(t, r, &patch)
	if d, ok := patch["description"].(string); ok {
		pl.description = d
	}
	if s, ok := patch["status"].(string); ok {
		pl.status = s
	}
	writeJSON(t, w, http.StatusOK, map[string]any{"pullRequestId": pl.id})
}

func (g *adoFake) getProps(t *testing.T, w http.ResponseWriter, p string) {
	pl, ok := g.pulls[adoTailNum(p, "pullrequests")]
	if !ok {
		writeJSON(t, w, http.StatusOK, map[string]any{"value": map[string]any{}})
		return
	}
	val := map[string]any{}
	for k, v := range pl.props {
		val[k] = map[string]any{"$type": "System.String", "$value": v}
	}
	writeJSON(t, w, http.StatusOK, map[string]any{"count": len(val), "value": val})
}

func (g *adoFake) patchProps(t *testing.T, w http.ResponseWriter, r *http.Request, p string) {
	pl := g.pulls[adoTailNum(p, "pullrequests")]
	var patch []map[string]any
	decode(t, r, &patch)
	for _, op := range patch {
		key := strings.TrimPrefix(op["path"].(string), "/")
		if v, ok := op["value"].(string); ok {
			pl.props[key] = v
		}
	}
	writeJSON(t, w, http.StatusOK, map[string]any{})
}

func (g *adoFake) wiql(t *testing.T, w http.ResponseWriter) {
	ids := make([]int, 0, len(g.wis))
	for id, wi := range g.wis {
		if hasTag(wi.fields["System.Tags"], "oiax/conflict") {
			ids = append(ids, id)
		}
	}
	// Descending, so the provider's ascending sort is what produces canonical
	// order — not the fake's return order.
	sort.Sort(sort.Reverse(sort.IntSlice(ids)))
	items := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		items = append(items, map[string]any{"id": id})
	}
	writeJSON(t, w, http.StatusOK, map[string]any{"workItems": items})
}

func (g *adoFake) batchWI(t *testing.T, w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for _, s := range strings.Split(r.URL.Query().Get("ids"), ",") {
		id, _ := strconv.Atoi(s)
		if wi, ok := g.wis[id]; ok {
			out = append(out, g.wiJSON(wi))
		}
	}
	writeJSON(t, w, http.StatusOK, map[string]any{"value": out})
}

func (g *adoFake) createWI(t *testing.T, w http.ResponseWriter, r *http.Request) {
	var patch []map[string]any
	decode(t, r, &patch)
	g.next++
	wi := &adoFakeWI{id: g.next, fields: map[string]string{"System.State": "To Do", "System.WorkItemType": "Issue"}}
	for _, op := range patch {
		field := strings.TrimPrefix(op["path"].(string), "/fields/")
		if v, ok := op["value"].(string); ok {
			wi.fields[field] = v
		}
	}
	g.wis[wi.id] = wi
	writeJSON(t, w, http.StatusOK, map[string]any{"id": wi.id})
}

func (g *adoFake) getWI(t *testing.T, w http.ResponseWriter, p string) {
	wi, ok := g.wis[adoTailNum(p, "workitems")]
	if !ok {
		writeJSON(t, w, http.StatusNotFound, map[string]any{"message": "not found"})
		return
	}
	writeJSON(t, w, http.StatusOK, g.wiJSON(wi))
}

func (g *adoFake) patchWI(t *testing.T, w http.ResponseWriter, r *http.Request, p string) {
	wi := g.wis[adoTailNum(p, "workitems")]
	var patch []map[string]any
	decode(t, r, &patch)
	for _, op := range patch {
		field := strings.TrimPrefix(op["path"].(string), "/fields/")
		if v, ok := op["value"].(string); ok {
			wi.fields[field] = v
		}
	}
	writeJSON(t, w, http.StatusOK, map[string]any{"id": wi.id})
}

func (g *adoFake) pullJSON(pl *adoFakePull, truncate bool) map[string]any {
	desc := pl.description
	if truncate && len(desc) > 400 {
		desc = desc[:400]
	}
	return map[string]any{
		"pullRequestId": pl.id,
		"description":   desc,
		"sourceRefName": pl.sourceRef,
		"targetRefName": pl.targetRef,
		"status":        pl.status,
		"closedDate":    "",
	}
}

func (g *adoFake) wiJSON(wi *adoFakeWI) map[string]any {
	fields := map[string]any{}
	for k, v := range wi.fields {
		fields[k] = v
	}
	return map[string]any{"id": wi.id, "fields": fields}
}
