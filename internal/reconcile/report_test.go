package reconcile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skaphos/oiax/v2/internal/engine"
	"github.com/skaphos/oiax/v2/internal/forge"
	"github.com/skaphos/oiax/v2/internal/tmpl"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

// onlyConflict returns the single report Apply produced, which must be a
// conflict for main -> development.
func onlyConflict(t *testing.T, res Result) *ConflictGuidance {
	t.Helper()
	if !res.Divergence || len(res.Divergences) != 1 {
		t.Fatalf("Divergence = %v, Divergences = %+v; want exactly one report", res.Divergence, res.Divergences)
	}
	r := res.Divergences[0]
	if r.Kind != KindConflict || r.From != "main" || r.To != "development" || r.Conflict == nil {
		t.Fatalf("report = %+v, want a main -> development conflict", r)
	}
	return r.Conflict
}

func wantKnown(t *testing.T, g *ConflictGuidance, id string) {
	t.Helper()
	if g.Artifact != ArtifactKnown || g.ArtifactID != id || g.ArtifactURL != fakeConflictURL(forge.ConflictArtifactID(id)) {
		t.Fatalf("artifact = %v %q %q, want known %s at %s", g.Artifact, g.ArtifactID, g.ArtifactURL, id,
			fakeConflictURL(forge.ConflictArtifactID(id)))
	}
}

func wantNoArtifact(t *testing.T, g *ConflictGuidance, status ArtifactStatus) {
	t.Helper()
	if g.Artifact != status || g.ArtifactID != "" || g.ArtifactURL != "" {
		t.Fatalf("artifact = %v %q %q, want status %v with no address", g.Artifact, g.ArtifactID, g.ArtifactURL, status)
	}
}

// Every artifact path reports the artifact that stays the pointer
// (spec 002 FR-006, FR-007; T021).
func TestConflictReportArtifactPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("created", func(t *testing.T) {
		r, _, a := conflictHarness(t)
		c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}
		res, err := c.Apply(ctx, engine.Plan{Actions: []engine.Action{a}})
		if err != nil {
			t.Fatal(err)
		}
		wantKnown(t, onlyConflict(t, res), "1")
	})

	t.Run("adopted", func(t *testing.T) {
		r, _, a := conflictHarness(t)
		head, _ := r.Head(ctx, "main")
		f := &fakeForge{}
		f.seedConflict("4", "main", "development", head)
		c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
		res, err := c.Apply(ctx, engine.Plan{Actions: []engine.Action{a}})
		if err != nil {
			t.Fatal(err)
		}
		wantKnown(t, onlyConflict(t, res), "4")
	})

	advanced := func(t *testing.T, updateErr error) {
		r, commit, a := conflictHarness(t)
		first, _ := r.Head(ctx, "main")
		commit("app.txt", "hotfix-change-2\n", "second conflicting hotfix")
		short, _ := r.ShortSHA(ctx, "main")
		a.Branch = engine.BackflowBranchName("main", "development", short)
		f := &fakeForge{updateConflictErr: updateErr}
		f.seedConflict("3", "main", "development", first)
		c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
		res, err := c.Apply(ctx, engine.Plan{Actions: []engine.Action{a}})
		if err != nil {
			t.Fatal(err)
		}
		if len(f.conflictUpdated) != 1 {
			t.Fatalf("updates = %d, want the advance attempt", len(f.conflictUpdated))
		}
		wantKnown(t, onlyConflict(t, res), "3")
	}
	t.Run("advanced", func(t *testing.T) { advanced(t, nil) })
	t.Run("advance failed", func(t *testing.T) { advanced(t, errors.New("503 forge unavailable")) })

	t.Run("left alone for a staler run", func(t *testing.T) {
		r, commit := gitHarness(t)
		checkout(t, r, "development")
		commitOn(t, r.Dir, "app.txt", "dev-change\n", "dev edit")
		checkout(t, r, "main")
		h1 := commit("app.txt", "hotfix-change\n", "first conflicting hotfix")
		h2 := commit("app.txt", "hotfix-change-2\n", "second conflicting hotfix")
		checkout(t, r, "development")
		gitExec(t, r.Dir, "branch", "-f", "main", h1)
		short, _ := r.ShortSHA(ctx, "main")
		a := engine.Action{
			Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
			Unpromoted: 1, Branch: engine.BackflowBranchName("main", "development", short),
		}
		f := &fakeForge{}
		f.seedConflict("3", "main", "development", h2)
		c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
		res, err := c.Apply(ctx, engine.Plan{Actions: []engine.Action{a}})
		if err != nil {
			t.Fatal(err)
		}
		if len(f.conflictUpdated) != 0 {
			t.Fatal("staler run wrote the record")
		}
		wantKnown(t, onlyConflict(t, res), "3")
	})

	t.Run("left alone on a shallow clone", func(t *testing.T) {
		r, _, a := conflictHarness(t)
		f := &fakeForge{}
		f.seedConflict("6", "main", "development", strings.Repeat("e", 40))
		head := gitExec(t, r.Dir, "rev-parse", "HEAD")
		if err := os.WriteFile(filepath.Join(r.Dir, ".git", "shallow"), []byte(head+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
		res, err := c.Apply(ctx, engine.Plan{Actions: []engine.Action{a}})
		if err != nil {
			t.Fatal(err)
		}
		if len(f.conflictUpdated) != 0 {
			t.Fatal("shallow clone rewrote a record it cannot classify")
		}
		wantKnown(t, onlyConflict(t, res), "6")
	})

	t.Run("canonical after consolidation", func(t *testing.T) {
		r, _, a := conflictHarness(t)
		head, _ := r.Head(ctx, "main")
		f := &fakeForge{}
		f.seedConflict("2", "main", "development", head)
		f.seedConflict("5", "main", "development", head)
		c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
		res, err := c.Apply(ctx, engine.Plan{Actions: []engine.Action{a}})
		if err != nil {
			t.Fatal(err)
		}
		wantKnown(t, onlyConflict(t, res), "2")
	})

	// A concurrent run records a lower-numbered artifact after this run
	// created its own; the end-of-Apply sweep closes this run's duplicate,
	// and the report must follow the canonical (post-merge review on #126).
	t.Run("re-pointed after the sweep closes this run's duplicate", func(t *testing.T) {
		r, _, a := conflictHarness(t)
		head, _ := r.Head(ctx, "main")
		f := &fakeForge{nextConflictNum: 7}
		f.afterCreate = func(f *fakeForge) {
			f.afterCreate = nil
			f.seedConflict("3", "main", "development", head)
		}
		c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
		res, err := c.Apply(ctx, engine.Plan{Actions: []engine.Action{a}})
		if err != nil {
			t.Fatal(err)
		}
		closed8 := false
		for _, id := range f.conflictClosed {
			closed8 = closed8 || id == "8"
		}
		if !closed8 {
			t.Fatalf("sweep did not close this run's duplicate 8: closed %v", f.conflictClosed)
		}
		wantKnown(t, onlyConflict(t, res), "3")
	})
}

// No artifact can be hosted or recorded: the report says so and carries no
// address (spec 002 FR-008; T049).
func TestConflictReportNoArtifact(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		forge  *fakeForge
		tmpls  *tmpl.Set
		status ArtifactStatus
	}{
		{"unhostable", &fakeForge{conflictArtifactsUnsupported: true}, nil, ArtifactUnhostable},
		{"create refused as unhostable", &fakeForge{createConflictErr: fmt.Errorf("410 Gone: %w", forge.ErrConflictArtifactsUnsupported)},
			nil, ArtifactUnhostable},
		{"list failed", &fakeForge{listConflictsErr: errors.New("502")}, nil, ArtifactNotRecorded},
		{"create failed", &fakeForge{createConflictErr: errors.New("502")}, nil, ArtifactNotRecorded},
		{"artifact render failed", &fakeForge{}, failingArtifactTemplates(t), ArtifactNotRecorded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _, a := conflictHarness(t)
			c := &Coordinator{Git: r, Forge: tt.forge, Graph: testGraph(), Templates: tt.tmpls}
			res, err := c.Apply(ctx, engine.Plan{Actions: []engine.Action{a}})
			if err != nil {
				t.Fatal(err)
			}
			g := onlyConflict(t, res)
			wantNoArtifact(t, g, tt.status)
			if g.SummaryBody == "" || g.RenderFailed {
				t.Fatalf("summary guidance missing with no artifact: %+v", g)
			}
		})
	}
}

// failingArtifactTemplates is a configured conflict body that passes the
// load-time sample render (two sample commits) but fails on the artifact
// surface at apply time, where the conflict context lists one commit.
func failingArtifactTemplates(t *testing.T) *tmpl.Set {
	t.Helper()
	s, err := tmpl.Resolve(&v1.PromotionGraph{Spec: v1.PromotionGraphSpec{Templates: &v1.Templates{
		BackflowConflict: &v1.RequestTemplate{Body: `Steps.{{if eq .Surface "artifact"}}{{with index .Commits 1}}{{.SHA}}{{end}}{{end}}`},
	}}}, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return s
}

// The guidance carries what the CLI prints, rendered for the summary
// surface, for both strategies (spec 002 T022).
func TestConflictReportGuidance(t *testing.T) {
	ctx := context.Background()
	for _, strategy := range []v1.BackflowStrategy{v1.BackflowStrategyCherryPick, v1.BackflowStrategyMerge} {
		t.Run(string(strategy), func(t *testing.T) {
			r, _, a := conflictHarness(t)
			g := testGraph()
			g.Backflow.Strategy = strategy
			var logBuf bytes.Buffer
			c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: g, ArtifactNoun: "work item",
				Log: NewLogger("text", AnnotateGitHub, nil, &logBuf)}
			res, err := c.Apply(ctx, engine.Plan{Actions: []engine.Action{a}})
			if err != nil {
				t.Fatal(err)
			}
			cg := onlyConflict(t, res)
			if cg.Strategy != string(strategy) || cg.Whole != (strategy == v1.BackflowStrategyMerge) {
				t.Fatalf("strategy = %q whole = %v", cg.Strategy, cg.Whole)
			}
			if len(cg.SHA) != 40 || cg.Subject == "" || cg.ArtifactNoun != "work item" {
				t.Fatalf("guidance = %+v", cg)
			}
			if cg.SummaryBody == "" || strings.Contains(cg.SummaryBody, "Oiax manages this conflict record") {
				t.Fatalf("summary body is not the summary-surface render:\n%s", cg.SummaryBody)
			}
			// The conflict warning moved to the CLI, after the artifact step.
			if strings.Contains(logBuf.String(), "halted on") {
				t.Fatalf("apply still logs the conflict warning:\n%s", logBuf.String())
			}
		})
	}
}

// The noun defaults to "issue" when the coordinator does not say.
func TestConflictReportDefaultNoun(t *testing.T) {
	r, _, a := conflictHarness(t)
	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}
	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatal(err)
	}
	if g := onlyConflict(t, res); g.ArtifactNoun != "issue" {
		t.Fatalf("noun = %q, want issue", g.ArtifactNoun)
	}
}

// Commit subjects reach a configured artifact body cleaned (spec 002 FR-013):
// no control or bidi characters, at most 200 runes (T022 regression).
func TestConflictArtifactCommitSubjectsCleaned(t *testing.T) {
	r, commit := gitHarness(t)
	checkout(t, r, "development")
	commitOn(t, r.Dir, "app.txt", "dev-change\n", "dev edit")
	checkout(t, r, "main")
	commit("app.txt", "hotfix-change\n", "fix: \u202eevil\u202c "+strings.Repeat("x", 300))
	short, _ := r.ShortSHA(context.Background(), "main")
	a := engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
		Unpromoted: 1, Branch: engine.BackflowBranchName("main", "development", short),
	}
	set, err := tmpl.Resolve(&v1.PromotionGraph{Spec: v1.PromotionGraphSpec{Templates: &v1.Templates{
		BackflowConflict: &v1.RequestTemplate{Body: `{{range .Commits}}[{{.Subject}}]{{end}}`},
	}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeForge{}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph(), Templates: set}
	if _, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}}); err != nil {
		t.Fatal(err)
	}
	if len(f.conflictCreated) != 1 {
		t.Fatalf("created = %d", len(f.conflictCreated))
	}
	body := f.conflictCreated[0].Body
	if strings.ContainsAny(body, "\u202e\u202c") {
		t.Fatalf("artifact body keeps bidi controls: %q", body)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(body, "["), "]")
	if n := len([]rune(inner)); n > 200 || !strings.HasSuffix(inner, "…") {
		t.Fatalf("commit subject is %d runes (want capped at 200 with …): %q", n, inner)
	}
}

// Any other reported divergence is one KindOther report; a conflict on the
// same edge wins (spec 002 FR-011).
func TestDivergenceReportsOnePerEdge(t *testing.T) {
	var res Result
	res.addReport(DivergenceReport{From: "a", To: "b", Kind: KindOther})
	res.addReport(DivergenceReport{From: "c", To: "d", Kind: KindOther})
	res.addReport(DivergenceReport{From: "a", To: "b", Kind: KindConflict, Conflict: &ConflictGuidance{}})
	res.addReport(DivergenceReport{From: "a", To: "b", Kind: KindOther})
	if len(res.Divergences) != 2 || res.Divergences[0].Kind != KindConflict || res.Divergences[1].From != "c" {
		t.Fatalf("reports = %+v", res.Divergences)
	}
}

// Two conflicting backflow sources give two conflict reports in plan action
// order, and another edge's reported divergence gives one KindOther report;
// repeated runs keep the same order (spec 002 FR-011, SC-006; T054).
func TestDivergenceReportsFollowPlanOrder(t *testing.T) {
	ctx := context.Background()
	r, commit := gitHarness(t)
	checkout(t, r, "development")
	commitOn(t, r.Dir, "app.txt", "dev-change\n", "dev edit")
	checkout(t, r, "test")
	commitOn(t, r.Dir, "app.txt", "test-hotfix\n", "test hotfix")
	checkout(t, r, "main")
	commit("app.txt", "main-hotfix\n", "main hotfix")

	g := testGraph()
	g.Backflow.Sources = []string{"main", "test"}
	action := func(from string) engine.Action {
		short, _ := r.ShortSHA(ctx, from)
		return engine.Action{Type: engine.ActionCreateBackflowRequest, From: from, To: "development",
			Unpromoted: 1, Branch: engine.BackflowBranchName(from, "development", short)}
	}
	plan := engine.Plan{Actions: []engine.Action{
		action("test"),
		{Type: engine.ActionReportDivergence, From: "development", To: "test", Reason: "drift"},
		action("main"),
	}}
	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: g}
	want := []string{"test->development conflict", "development->test other", "main->development conflict"}
	for run := range 20 {
		res, err := c.Apply(ctx, plan)
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		var got []string
		for _, d := range res.Divergences {
			kind := "other"
			if d.Kind == KindConflict {
				kind = "conflict"
			}
			got = append(got, d.From+"->"+d.To+" "+kind)
		}
		if strings.Join(got, ", ") != strings.Join(want, ", ") {
			t.Fatalf("run %d: reports = %v, want %v", run, got, want)
		}
	}
}

// A later edge's consolidation closes the record an earlier edge's report
// named, and a later action then fails, so the end-of-Apply sweep never
// runs: the earlier report must already follow the canonical (review on
// #160).
func TestConflictReportRepointedBeforeALaterFailure(t *testing.T) {
	ctx := context.Background()
	r, commit := gitHarness(t)
	checkout(t, r, "development")
	commitOn(t, r.Dir, "app.txt", "dev-change\n", "dev edit")
	checkout(t, r, "test")
	commitOn(t, r.Dir, "app.txt", "test-hotfix\n", "test hotfix")
	checkout(t, r, "main")
	commit("app.txt", "main-hotfix\n", "main hotfix")
	mainHead, _ := r.Head(ctx, "main")

	g := testGraph()
	g.Backflow.Sources = []string{"main", "test"}
	action := func(from string) engine.Action {
		short, _ := r.ShortSHA(ctx, from)
		return engine.Action{Type: engine.ActionCreateBackflowRequest, From: from, To: "development",
			Unpromoted: 1, Branch: engine.BackflowBranchName(from, "development", short)}
	}
	f := &fakeForge{nextConflictNum: 7}
	f.afterCreate = func(f *fakeForge) {
		// main's record (8) is created; a concurrent run's lower-numbered
		// record for the same edge appears before test's record path lists.
		f.afterCreate = nil
		f.seedConflict("3", "main", "development", mainHead)
	}
	c := &Coordinator{Git: r, Forge: f, Graph: g}
	res, err := c.Apply(ctx, engine.Plan{Actions: []engine.Action{
		action("main"),
		action("test"),
		{Type: engine.ActionUpdateManagedRequest, From: "development", To: "test"}, // no request: fails
	}})
	if err == nil {
		t.Fatal("want the later action to fail")
	}
	var main *ConflictGuidance
	for _, d := range res.Divergences {
		if d.From == "main" {
			main = d.Conflict
		}
	}
	if main == nil {
		t.Fatalf("no main report: %+v", res.Divergences)
	}
	wantKnown(t, main, "3")
}

// The record path's list failed, but the end-of-Apply sweep found the edge's
// record: the report becomes known with its address (review on #160).
func TestConflictReportKnownWhenTheSweepFindsTheRecord(t *testing.T) {
	r, _, a := conflictHarness(t)
	head, _ := r.Head(context.Background(), "main")
	f := &fakeForge{listConflictsFailures: 1}
	f.seedConflict("5", "main", "development", head)
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatal(err)
	}
	wantKnown(t, onlyConflict(t, res), "5")
}

// Cleaning precedes truncation for commit subjects too: a subject that
// starts with 200 invisible bidi controls keeps its visible text (review on
// #160).
func TestConflictArtifactCommitSubjectCleanedBeforeCap(t *testing.T) {
	r, commit := gitHarness(t)
	checkout(t, r, "development")
	commitOn(t, r.Dir, "app.txt", "dev-change\n", "dev edit")
	checkout(t, r, "main")
	commit("app.txt", "hotfix-change\n", strings.Repeat(string(rune(0x202e)), 200)+"fix: return the missing configuration")
	short, _ := r.ShortSHA(context.Background(), "main")
	a := engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
		Unpromoted: 1, Branch: engine.BackflowBranchName("main", "development", short),
	}
	set, err := tmpl.Resolve(&v1.PromotionGraph{Spec: v1.PromotionGraphSpec{Templates: &v1.Templates{
		BackflowConflict: &v1.RequestTemplate{Body: `{{range .Commits}}[{{.Subject}}]{{end}}`},
	}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeForge{}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph(), Templates: set}
	if _, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}}); err != nil {
		t.Fatal(err)
	}
	if len(f.conflictCreated) != 1 || f.conflictCreated[0].Body != "[fix: return the missing configuration]" {
		t.Fatalf("artifact body = %q, want the visible subject", f.conflictCreated)
	}
}
