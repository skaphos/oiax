package azuredevops

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/forge"
	mk "github.com/skaphos/oiax/internal/forge/marker"
)

// workItemJSON renders a work item with a conflict marker in its HTML
// description and the oiax + oiax/conflict tags.
func workItemJSON(id int, graph, source, target, head, state string) map[string]any {
	desc := mk.Serialize(mk.Marker{
		Version: "v1", Graph: graph, Type: mk.ConflictType,
		Source: source, Destination: target, SourceHead: head,
	}) + "<br/><br/>\nBackflow conflict."
	return map[string]any{
		"id": id,
		"fields": map[string]any{
			"System.Description":  desc,
			"System.Tags":         "oiax; oiax/conflict",
			"System.State":        state,
			"System.WorkItemType": "Issue",
		},
	}
}

// issueStates is a Basic-process states response: an open state and a Completed
// "Done" state, exercising category-driven open/close decisions.
func issueStates() map[string]any {
	return map[string]any{"value": []map[string]any{
		{"name": "To Do", "category": "Proposed"},
		{"name": "Doing", "category": "InProgress"},
		{"name": "Done", "category": "Completed"},
	}}
}

func TestListConflictArtifactsFiltersOpenAndSorts(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == projectBase+"/wit/wiql":
			var body map[string]string
			decode(t, r, &body)
			if !strings.Contains(body["query"], "CONTAINS 'oiax/conflict'") {
				t.Errorf("WIQL must filter by the oiax/conflict tag: %q", body["query"])
			}
			// Return ids out of order to prove the provider sorts ascending.
			writeJSON(t, w, http.StatusOK, map[string]any{"workItems": []map[string]any{{"id": 20}, {"id": 10}, {"id": 30}}})
		case r.Method == http.MethodGet && r.URL.Path == projectBase+"/wit/workitems":
			if !strings.Contains(r.URL.RawQuery, "errorPolicy=omit") {
				t.Error("hydrate must set errorPolicy=omit so a deleted item cannot fail the listing")
			}
			writeJSON(t, w, http.StatusOK, map[string]any{"value": []map[string]any{
				workItemJSON(20, "g", "dev", "main", "h20", "To Do"),
				workItemJSON(10, "g", "dev", "main", "h10", "Done"),      // closed → excluded
				workItemJSON(30, "other", "dev", "main", "h30", "To Do"), // wrong graph → excluded
			}})
		case r.Method == http.MethodGet && r.URL.Path == projectBase+"/wit/workitemtypes/Issue/states":
			writeJSON(t, w, http.StatusOK, issueStates())
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	got, err := newProvider(t, srv).ListConflictArtifacts(context.Background(), "g")
	if err != nil {
		t.Fatalf("ListConflictArtifacts: %v", err)
	}
	if len(got) != 1 || got[0].ID != "20" || got[0].SourceHead != "h20" {
		t.Fatalf("want only the open, matching-graph artifact 20, got %+v", got)
	}
}

func TestCreateConflictArtifactEscapesBodyAndTags(t *testing.T) {
	t.Parallel()
	var patch []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != projectBase+"/wit/workitems/$Issue" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != contentTypeJSONPatch {
			t.Errorf("Content-Type = %q, want json-patch", ct)
		}
		decode(t, r, &patch)
		writeJSON(t, w, http.StatusOK, map[string]any{"id": 55})
	}))
	defer srv.Close()

	art, err := newProvider(t, srv).CreateConflictArtifact(context.Background(), forge.ConflictArtifactSpec{
		Graph: "g", Source: "dev", Target: "main", SourceHead: "abc",
		Title: "Conflict", Body: "Commit <script>alert(1)</script> failed",
	})
	if err != nil {
		t.Fatalf("CreateConflictArtifact: %v", err)
	}
	if art.ID != "55" {
		t.Errorf("id = %q, want 55", art.ID)
	}
	fields := map[string]string{}
	for _, op := range patch {
		fields[op["path"].(string)] = op["value"].(string)
	}
	if fields["/fields/System.Tags"] != mk.LabelOiax+"; "+mk.LabelConflict {
		t.Errorf("tags = %q", fields["/fields/System.Tags"])
	}
	desc := fields["/fields/System.Description"]
	if strings.Contains(desc, "<script>") {
		t.Error("attacker-influenced body must be HTML-escaped in the work-item description (stored-XSS guard)")
	}
	if !strings.Contains(desc, "&lt;script&gt;") {
		t.Errorf("body should be HTML-escaped, got %q", desc)
	}
	m, ok := mk.Parse(desc)
	if !ok || m.Type != mk.ConflictType || m.SourceHead != "abc" {
		t.Errorf("marker not parseable from description: %+v ok=%v", m, ok)
	}
}

func TestCloseConflictArtifactUsesCompletedCategoryState(t *testing.T) {
	t.Parallel()
	var patch []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == projectBase+"/wit/workitems/7":
			writeJSON(t, w, http.StatusOK, workItemJSON(7, "g", "dev", "main", "h", "To Do"))
		case r.Method == http.MethodGet && r.URL.Path == projectBase+"/wit/workitemtypes/Issue/states":
			writeJSON(t, w, http.StatusOK, issueStates())
		case r.Method == http.MethodPatch && r.URL.Path == projectBase+"/wit/workitems/7":
			decode(t, r, &patch)
			writeJSON(t, w, http.StatusOK, map[string]any{"id": 7})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	if err := newProvider(t, srv).CloseConflictArtifact(context.Background(), "7", forge.Reason{Summary: "resolved"}); err != nil {
		t.Fatalf("CloseConflictArtifact: %v", err)
	}
	fields := map[string]string{}
	for _, op := range patch {
		fields[op["path"].(string)] = op["value"].(string)
	}
	if fields["/fields/System.State"] != "Done" {
		t.Errorf("close state = %q, want the Completed-category state Done", fields["/fields/System.State"])
	}
	if !strings.Contains(fields["/fields/System.History"], "resolved") {
		t.Errorf("close must add the reason as a discussion comment, got %q", fields["/fields/System.History"])
	}
}

func TestUpdateConflictArtifactRefreshesDescription(t *testing.T) {
	t.Parallel()
	var patch []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == projectBase+"/wit/workitems/9":
			writeJSON(t, w, http.StatusOK, workItemJSON(9, "g", "dev", "main", "old", "To Do"))
		case r.Method == http.MethodPatch && r.URL.Path == projectBase+"/wit/workitems/9":
			decode(t, r, &patch)
			writeJSON(t, w, http.StatusOK, map[string]any{"id": 9})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	err := newProvider(t, srv).UpdateConflictArtifact(context.Background(), "9", forge.ConflictArtifactSpec{
		Graph: "g", Source: "dev", Target: "main", SourceHead: "new", Body: "updated",
	})
	if err != nil {
		t.Fatalf("UpdateConflictArtifact: %v", err)
	}
	if len(patch) != 1 || patch[0]["path"] != "/fields/System.Description" {
		t.Fatalf("update patch = %+v", patch)
	}
	m, ok := mk.Parse(patch[0]["value"].(string))
	if !ok || m.SourceHead != "new" {
		t.Errorf("refreshed marker = %+v ok=%v", m, ok)
	}
}

func TestRepoMergeMethodsAllowsEverything(t *testing.T) {
	t.Parallel()
	m, err := (&Provider{}).RepoMergeMethods(context.Background())
	if err != nil {
		t.Fatalf("RepoMergeMethods: %v", err)
	}
	if !m.Merge || !m.Squash || !m.Rebase || m.RequiresLinearHistory {
		t.Errorf("Azure DevOps has no repo-level merge settings; want all allowed, got %+v", m)
	}
}

func TestTargetMergeMethodsFromPolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		policies []map[string]any
		want     forge.MergeMethods
	}{
		{
			name:     "no policy allows everything",
			policies: nil,
			want:     forge.MergeMethods{Merge: true, Squash: true, Rebase: true},
		},
		{
			name: "squash-only forbids merge commits (linear history)",
			policies: []map[string]any{{
				"isEnabled": true,
				"settings": map[string]any{
					"allowNoFastForward": false, "allowSquash": true, "allowRebase": false, "allowRebaseMerge": false,
					"scope": []map[string]any{{"refName": "refs/heads/main", "matchKind": "exact", "repositoryId": "repo-guid"}},
				},
			}},
			want: forge.MergeMethods{Merge: false, Squash: true, Rebase: false, RequiresLinearHistory: true},
		},
		{
			name: "merge commit allowed clears linear history",
			policies: []map[string]any{{
				"isEnabled": true,
				"settings": map[string]any{
					"allowNoFastForward": true, "allowSquash": true, "allowRebase": true,
					"scope": []map[string]any{{"refName": "refs/heads/main", "matchKind": "exact", "repositoryId": "repo-guid"}},
				},
			}},
			want: forge.MergeMethods{Merge: true, Squash: true, Rebase: true, RequiresLinearHistory: false},
		},
		{
			name: "legacy useSquashMerge means squash-only",
			policies: []map[string]any{{
				"isEnabled": true,
				"settings": map[string]any{
					"useSquashMerge": true,
					"scope":          []map[string]any{{"refName": "refs/heads/main", "matchKind": "exact"}},
				},
			}},
			want: forge.MergeMethods{Merge: false, Squash: true, Rebase: false, RequiresLinearHistory: true},
		},
		{
			name: "policy scoped to a different repository is ignored",
			policies: []map[string]any{{
				"isEnabled": true,
				"settings": map[string]any{
					"allowNoFastForward": false, "allowSquash": true,
					"scope": []map[string]any{{"refName": "refs/heads/main", "matchKind": "exact", "repositoryId": "some-other-repo"}},
				},
			}},
			want: forge.MergeMethods{Merge: true, Squash: true, Rebase: true},
		},
		{
			name: "policy scoped to a different branch is ignored",
			policies: []map[string]any{{
				"isEnabled": true,
				"settings": map[string]any{
					"allowNoFastForward": false, "allowSquash": true,
					"scope": []map[string]any{{"refName": "refs/heads/release", "matchKind": "exact", "repositoryId": "repo-guid"}},
				},
			}},
			want: forge.MergeMethods{Merge: true, Squash: true, Rebase: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case gitBase:
					writeJSON(t, w, http.StatusOK, map[string]any{"id": "repo-guid"})
				case projectBase + "/policy/configurations":
					if got := r.URL.Query().Get("policyType"); got != mergeStrategyPolicyType {
						t.Errorf("policyType = %q, want the merge-strategy GUID", got)
					}
					writeJSON(t, w, http.StatusOK, map[string]any{"value": tc.policies})
				default:
					t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
				}
			}))
			defer srv.Close()

			got, err := newProvider(t, srv).TargetMergeMethods(context.Background(), "main")
			if err != nil {
				t.Fatalf("TargetMergeMethods: %v", err)
			}
			if got != tc.want {
				t.Errorf("TargetMergeMethods = %+v, want %+v", got, tc.want)
			}
		})
	}
}
