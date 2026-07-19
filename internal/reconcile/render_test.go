package reconcile

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/engine"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

func samplePlan() engine.Plan {
	return engine.Plan{
		PlanFormatVersion: engine.PlanFormatVersion,
		Graph:             "environments",
		Actions: []engine.Action{
			{
				Type: engine.ActionCreatePromotionRequest, From: "development", To: "test",
				Unpromoted: 3, Equivalence: engine.EquivalenceReachability,
				Reason: "3 unpromoted commits and no managed promotion request",
			},
			{
				Type: engine.ActionReportDivergence, From: "test", To: "main",
				Unpromoted: 1, Reason: "main has 1 commits not represented in test",
			},
		},
		Edges: []engine.EdgeSummary{
			{
				From: "development", To: "test",
				Equivalence: engine.EquivalenceReachability, Unpromoted: 3,
			},
			{
				From: "test", To: "main",
				Equivalence: engine.EquivalencePatchIdentity, InSync: true,
				DownstreamOnly: 3, ToReturn: 1,
				Excluded: []engine.BackflowExclusion{
					{SHA: "aaa", Subject: "skipped hotfix", Reason: engine.BackflowExcludedSkip},
					{SHA: "bbb", Subject: "returned hotfix", Reason: engine.BackflowExcludedPatchID},
				},
			},
		},
	}
}

func TestRenderJSONIsPlanFormatVersion1(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderJSON(&buf, samplePlan()); err != nil {
		t.Fatalf("render json: %v", err)
	}
	var decoded struct {
		PlanFormatVersion int `json:"planFormatVersion"`
		Actions           []struct {
			Type string `json:"type"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	if decoded.PlanFormatVersion != 1 {
		t.Errorf("planFormatVersion = %d, want 1", decoded.PlanFormatVersion)
	}
	if len(decoded.Actions) != 2 || decoded.Actions[0].Type != "createPromotionRequest" {
		t.Errorf("actions = %+v", decoded.Actions)
	}
	if !strings.Contains(buf.String(), "\n  ") {
		t.Error("expected indented JSON")
	}
}

func TestRenderTextEmptyPlan(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderText(&buf, engine.Plan{Graph: "environments"}); err != nil {
		t.Fatalf("render text: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Promotion graph: environments") || !strings.Contains(out, "In sync, no actions.") {
		t.Errorf("text = %q", out)
	}
}

func TestRenderTextListsActions(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderText(&buf, samplePlan()); err != nil {
		t.Fatalf("render text: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"create", "development -> test", "report", "test -> main"} {
		if !strings.Contains(out, want) {
			t.Errorf("text missing %q:\n%s", want, out)
		}
	}
}

// TestRenderTextEdgeDiagnostics asserts the per-edge lines: sync status, the
// settling rung, and the backflow counts with per-reason exclusion terms.
func TestRenderTextEdgeDiagnostics(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderText(&buf, samplePlan()); err != nil {
		t.Fatalf("render text: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"edge development -> test: 3 unpromoted (reachability)",
		"edge test -> main: in sync (patch-identity), 3 downstream-only, 1 to return, 2 excluded (1 skip, 1 patch-id)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text missing %q:\n%s", want, out)
		}
	}
}

// TestRenderTextWithoutEdgeSummaries confirms a plan carrying no edge
// diagnostics (hand-built, or from a future producer that omits them) still
// renders the header and actions.
func TestRenderTextWithoutEdgeSummaries(t *testing.T) {
	plan := samplePlan()
	plan.Edges = nil
	var buf bytes.Buffer
	if err := RenderText(&buf, plan); err != nil {
		t.Fatalf("render text: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "edge ") {
		t.Errorf("unexpected edge lines:\n%s", out)
	}
	if !strings.Contains(out, "create") {
		t.Errorf("actions missing:\n%s", out)
	}
}

// TestRenderMarkdownWithoutEdgeSummaries is the markdown analogue of the text
// test above: a plan carrying no edge diagnostics renders no edges table but
// still renders the header and the actions table.
func TestRenderMarkdownWithoutEdgeSummaries(t *testing.T) {
	plan := samplePlan()
	plan.Edges = nil
	var buf bytes.Buffer
	if err := RenderMarkdown(&buf, plan); err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "| Edge |") {
		t.Errorf("unexpected edges table:\n%s", out)
	}
	for _, want := range []string{"## Oiax plan: environments", "| create | development | test |"} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q:\n%s", want, out)
		}
	}
}

// TestRenderMarkdownEscapesTableCells guards the step-summary table against a
// branch name containing '|'. Such a name is legal — `git check-ref-format`
// accepts it and the v1 config validator rejects only " ~^:?*[\\", and control
// characters — so unescaped it would open extra columns and corrupt the table,
// in both the edges rows and the actions rows.
func TestRenderMarkdownEscapesTableCells(t *testing.T) {
	plan := engine.Plan{
		Graph: "environments",
		Actions: []engine.Action{{
			Type: engine.ActionCreatePromotionRequest, From: "feat|bar", To: "test",
			Unpromoted: 1, Reason: "1 unpromoted commits on feat|bar",
		}},
		Edges: []engine.EdgeSummary{{
			From: "feat|bar", To: "test",
			Equivalence: engine.EquivalenceReachability, Unpromoted: 1,
		}},
	}
	var buf bytes.Buffer
	if err := RenderMarkdown(&buf, plan); err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, `feat|bar`) {
		t.Errorf("unescaped '|' in a table cell:\n%s", out)
	}
	for _, want := range []string{
		`| feat\|bar -> test | 1 unpromoted | reachability | 0 | 0 |  |`,
		`| create | feat\|bar | test | 1 | 1 unpromoted commits on feat\|bar |`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q:\n%s", want, out)
		}
	}
	// Every row must keep its column count: 6 edge columns and 5 action
	// columns mean 7 and 6 pipes respectively once escaped pipes are removed.
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		bare := strings.ReplaceAll(line, `\|`, "")
		if n := strings.Count(bare, "|"); n != 7 && n != 6 {
			t.Errorf("row has %d column separators, table is malformed: %q", n, line)
		}
	}
}

// mergePlan is samplePlan's merge-strategy analogue: the test -> main edge is a
// merge-strategy backflow source that returns its downstream-only set wholesale,
// so its summary carries Strategy and the Returned set.
func mergePlan() engine.Plan {
	return engine.Plan{
		PlanFormatVersion: engine.PlanFormatVersion,
		Graph:             "environments",
		Actions: []engine.Action{{
			Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
			Unpromoted: 2, Strategy: v1.BackflowStrategyMerge,
			Reason: "2 downstream-only commits on main to return to development",
		}},
		Edges: []engine.EdgeSummary{{
			From: "test", To: "main",
			Equivalence: engine.EquivalenceReachability, InSync: true,
			DownstreamOnly: 2, ToReturn: 2,
			Strategy: v1.BackflowStrategyMerge,
			Returned: []engine.Commit{
				{SHA: "aaa", Subject: "urgent hotfix"},
				{SHA: "bbb", Subject: "config tweak"},
			},
		}},
	}
}

// TestRenderTextMergeStrategyEdge asserts the human text names the merge
// mechanism and the wholesale return set for a merge-strategy backflow edge.
func TestRenderTextMergeStrategyEdge(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderText(&buf, mergePlan()); err != nil {
		t.Fatalf("render text: %v", err)
	}
	out := buf.String()
	want := "edge test -> main: in sync (reachability), 2 downstream-only, 2 to return, strategy: merge — returning 2 wholesale: urgent hotfix, config tweak"
	if !strings.Contains(out, want) {
		t.Errorf("text missing merge strategy line %q:\n%s", want, out)
	}
}

// TestRenderMarkdownMergeStrategyEdge asserts the step-summary State cell notes
// the merge strategy and its wholesale return count.
func TestRenderMarkdownMergeStrategyEdge(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderMarkdown(&buf, mergePlan()); err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "| test -> main | in sync (merge: returns 2) | reachability |") {
		t.Errorf("markdown missing merge strategy note:\n%s", out)
	}
}

// TestRenderCherryPickEdgeOmitsStrategy proves cherry-pick edges (Strategy
// empty) render exactly as before: no strategy tag in text, no strategy note in
// the markdown State cell. This is the human-output half of the frozen-format
// invariant asserted for JSON in engine.TestPlanCherryPickJSONUnchanged.
func TestRenderCherryPickEdgeOmitsStrategy(t *testing.T) {
	var text, md bytes.Buffer
	if err := RenderText(&text, samplePlan()); err != nil {
		t.Fatalf("render text: %v", err)
	}
	if err := RenderMarkdown(&md, samplePlan()); err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	for _, out := range []string{text.String(), md.String()} {
		if strings.Contains(out, "strategy") || strings.Contains(out, "returns") || strings.Contains(out, "wholesale") {
			t.Errorf("cherry-pick output must not mention merge strategy:\n%s", out)
		}
	}
}

// failingWriter fails every write, standing in for a broken pipe or full
// disk so the renderers' error propagation can be exercised.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestRenderersPropagateWriteError(t *testing.T) {
	for _, tc := range []struct {
		name string
		fn   func(io.Writer, engine.Plan) error
	}{
		{"text", RenderText},
		{"markdown", RenderMarkdown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fn(failingWriter{}, samplePlan()); err == nil {
				t.Fatal("expected write error to propagate, got nil")
			}
		})
	}
}

func TestRenderMarkdownTable(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderMarkdown(&buf, samplePlan()); err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"## Oiax plan: environments",
		"| Action | From | To |",
		"| create | development | test |",
		"| Edge | State | Settled by |",
		"| development -> test | 3 unpromoted | reachability | 0 | 0 |  |",
		"| test -> main | in sync | patch-identity | 3 | 1 | 1 skip, 1 patch-id |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q:\n%s", want, out)
		}
	}
}
