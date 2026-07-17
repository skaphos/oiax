package engine

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

func edge(from, to string) EdgeState {
	return EdgeState{
		From: BranchState{Name: from, Head: "head-" + from},
		To:   BranchState{Name: to, Head: "head-" + to},
	}
}

func TestBuildPlan(t *testing.T) {
	g := FromConfig(validGraph())

	tests := []struct {
		name string
		edge EdgeState
		want []ActionType
	}{
		{
			name: "in sync produces nothing",
			edge: edge("development", "test"),
			want: nil,
		},
		{
			name: "divergence without request creates one",
			edge: func() EdgeState {
				e := edge("development", "test")
				e.Unpromoted = []Commit{{SHA: "d"}, {SHA: "e"}}
				e.Equivalence = EquivalenceReachability
				return e
			}(),
			want: []ActionType{ActionCreatePromotionRequest},
		},
		{
			name: "existing request with current baseline needs nothing",
			edge: func() EdgeState {
				e := edge("development", "test")
				e.Unpromoted = []Commit{{SHA: "d"}}
				e.ManagedRequest = &ChangeRequest{ID: "7", SourceHead: "head-development"}
				return e
			}(),
			want: nil,
		},
		{
			name: "source advanced updates the baseline, never duplicates",
			edge: func() EdgeState {
				e := edge("development", "test")
				e.Unpromoted = []Commit{{SHA: "d"}, {SHA: "e"}}
				e.ManagedRequest = &ChangeRequest{ID: "7", SourceHead: "stale"}
				return e
			}(),
			want: []ActionType{ActionUpdateManagedRequest},
		},
		{
			name: "edge synchronized out-of-band closes the obsolete request",
			edge: func() EdgeState {
				e := edge("development", "test")
				e.ManagedRequest = &ChangeRequest{ID: "7", SourceHead: "head-development"}
				return e
			}(),
			want: []ActionType{ActionCloseObsoleteRequest},
		},
		{
			name: "downstream-only content on a backflow source is returned",
			edge: func() EdgeState {
				e := edge("production-stage-1", "main")
				e.DownstreamOnly = []Commit{{SHA: "x", Subject: "hotfix"}}
				e.ToReturn = []Commit{{SHA: "x", Subject: "hotfix"}}
				e.SourceHeadShort = "abc1234"
				return e
			}(),
			want: []ActionType{ActionCreateBackflowRequest},
		},
		{
			name: "backflow source with everything already returned converges",
			edge: func() EdgeState {
				e := edge("production-stage-1", "main")
				e.DownstreamOnly = []Commit{{SHA: "x", Subject: "hotfix"}}
				e.ToReturn = nil // all already returned
				e.SourceHeadShort = "abc1234"
				return e
			}(),
			want: nil,
		},
		{
			name: "downstream-only content on a non-backflow-source is reported",
			edge: func() EdgeState {
				e := edge("development", "test")
				e.DownstreamOnly = []Commit{{SHA: "x", Subject: "drift"}}
				return e
			}(),
			want: []ActionType{ActionReportDivergence},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := BuildPlan(g, []EdgeState{tt.edge})
			var got []ActionType
			for _, a := range plan.Actions {
				got = append(got, a.Type)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("actions = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("actions = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestBuildPlanBackflowRequestPayload(t *testing.T) {
	g := FromConfig(validGraph())

	// main is a backflow source; the target is development. Two commits remain
	// to return after identity filtering.
	e := edge("production-stage-1", "main")
	e.DownstreamOnly = []Commit{{SHA: "x", Subject: "hotfix a"}, {SHA: "y", Subject: "hotfix b"}, {SHA: "z", Subject: "already returned"}}
	e.ToReturn = []Commit{{SHA: "x", Subject: "hotfix a"}, {SHA: "y", Subject: "hotfix b"}}
	e.SourceHeadShort = "deadbee"

	plan := BuildPlan(g, []EdgeState{e})
	if len(plan.Actions) != 1 {
		t.Fatalf("actions = %+v, want exactly one backflow action", plan.Actions)
	}
	a := plan.Actions[0]
	if a.Type != ActionCreateBackflowRequest {
		t.Fatalf("type = %q, want %q", a.Type, ActionCreateBackflowRequest)
	}
	if a.From != "main" || a.To != "development" {
		t.Errorf("from/to = %q -> %q, want main -> development", a.From, a.To)
	}
	if a.Unpromoted != 2 {
		t.Errorf("Unpromoted = %d, want 2 (ToReturn count, not DownstreamOnly)", a.Unpromoted)
	}
	want := "oiax/backflow/main-to-development/deadbee"
	if a.Branch != want {
		t.Errorf("Branch = %q, want %q", a.Branch, want)
	}
	if a.Branch != BackflowBranchName("main", "development", "deadbee") {
		t.Errorf("Branch not built by BackflowBranchName")
	}
}

func TestBackflowBranchName(t *testing.T) {
	got := BackflowBranchName("main", "development", "abc1234")
	if want := "oiax/backflow/main-to-development/abc1234"; got != want {
		t.Fatalf("BackflowBranchName = %q, want %q", got, want)
	}
}

func TestBuildPlanRespectsExpectedDrift(t *testing.T) {
	cfg := validGraph()
	// Detach test from backflow concerns and mark its drift expected.
	cfg.Spec.Branches["test"] = v1.Branch{Drift: v1.DriftExpected}
	g := FromConfig(cfg)

	e := edge("development", "test")
	e.DownstreamOnly = []Commit{{SHA: "x"}}

	plan := BuildPlan(g, []EdgeState{e})
	if len(plan.Actions) != 0 {
		t.Fatalf("actions = %v, want none (drift expected)", plan.Actions)
	}
}

func TestBuildPlanIsDeterministic(t *testing.T) {
	g := FromConfig(validGraph())
	e := edge("development", "test")
	e.Unpromoted = []Commit{{SHA: "d"}}

	a := BuildPlan(g, []EdgeState{e})
	b := BuildPlan(g, []EdgeState{e})
	if len(a.Actions) != len(b.Actions) || a.Actions[0] != b.Actions[0] {
		t.Fatalf("plans differ for identical input: %v vs %v", a, b)
	}
	if a.PlanFormatVersion != PlanFormatVersion || a.Graph != "environments" {
		t.Fatalf("plan header = %+v, want format %d graph %q", a, PlanFormatVersion, "environments")
	}
}

func TestBuildPlanDedupsBackflowAcrossIncomingEdges(t *testing.T) {
	// A backflow source with two incoming promotion edges yields two EdgeStates
	// with the same To, each proposing a backflow for the same (source, target)
	// pair and the same branch name. Exactly one action must be emitted — two
	// would double-push the branch and open two identical requests.
	g := FromConfig(validGraph())

	e1 := edge("qa", "main")
	e1.DownstreamOnly = []Commit{{SHA: "h1", Subject: "hotfix a"}}
	e1.ToReturn = []Commit{{SHA: "h1", Subject: "hotfix a"}}
	e1.SourceHeadShort = "abc1234"

	e2 := edge("production-stage-1", "main")
	e2.DownstreamOnly = []Commit{{SHA: "h1", Subject: "hotfix a"}, {SHA: "h2", Subject: "hotfix b"}}
	e2.ToReturn = []Commit{{SHA: "h1", Subject: "hotfix a"}, {SHA: "h2", Subject: "hotfix b"}}
	e2.SourceHeadShort = "abc1234"

	plan := BuildPlan(g, []EdgeState{e1, e2})
	var backflow int
	var surviving Action
	for _, a := range plan.Actions {
		if a.Type == ActionCreateBackflowRequest {
			backflow++
			surviving = a
			if a.From != "main" || a.To != "development" {
				t.Errorf("backflow action = %q->%q, want main->development", a.From, a.To)
			}
		}
	}
	if backflow != 1 {
		t.Fatalf("planned %d backflow actions, want exactly 1", backflow)
	}
	// The surviving action must report the UNION across both edges (h1 via qa,
	// h1+h2 via production-stage-1), not just the first edge's partial count —
	// apply cherry-picks and pushes the full union.
	if surviving.Unpromoted != 2 {
		t.Errorf("Unpromoted = %d, want 2 (union of both incoming edges)", surviving.Unpromoted)
	}
}

// TestBuildPlanEdgeSummaries confirms the plan carries one diagnostic
// summary per evaluated edge, in input order — including in-sync edges that
// produce no action, whose settling rung would otherwise be invisible.
func TestBuildPlanEdgeSummaries(t *testing.T) {
	g := FromConfig(validGraph())

	// "test" is NOT a backflow source, so the degenerate ToReturn/Excluded
	// views EvaluateEdge computes there (its exclusion inputs are never
	// observed) must not be published — only the downstream-only count is.
	inSync := edge("development", "test")
	inSync.Equivalence = EquivalencePatchIdentity
	inSync.DownstreamOnly = []Commit{{SHA: "x"}}
	inSync.ToReturn = []Commit{{SHA: "x"}}
	inSync.Excluded = []BackflowExclusion{{SHA: "y", Reason: BackflowExcludedPatchID}}

	diverged := edge("test", "qa")
	diverged.Equivalence = EquivalenceReachability
	diverged.Unpromoted = []Commit{{SHA: "d"}, {SHA: "e"}}

	backflow := edge("production-stage-1", "main")
	backflow.Equivalence = EquivalenceReachability
	backflow.DownstreamOnly = []Commit{{SHA: "h1"}, {SHA: "h2"}, {SHA: "h3"}}
	backflow.ToReturn = []Commit{{SHA: "h1"}}
	backflow.Excluded = []BackflowExclusion{
		{SHA: "h2", Reason: BackflowExcludedSkip},
		{SHA: "h3", Reason: BackflowExcludedPatchID},
	}
	backflow.SourceHeadShort = "abc1234"

	plan := BuildPlan(g, []EdgeState{inSync, diverged, backflow})

	want := []EdgeSummary{
		{From: "development", To: "test", Equivalence: EquivalencePatchIdentity, InSync: true, DownstreamOnly: 1},
		{From: "test", To: "qa", Equivalence: EquivalenceReachability, InSync: false, Unpromoted: 2},
		{From: "production-stage-1", To: "main", Equivalence: EquivalenceReachability, InSync: true,
			DownstreamOnly: 3, ToReturn: 1, Excluded: backflow.Excluded},
	}
	if len(plan.Edges) != len(want) {
		t.Fatalf("edges = %+v, want %d summaries", plan.Edges, len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(plan.Edges[i], want[i]) {
			t.Errorf("edges[%d] = %+v, want %+v", i, plan.Edges[i], want[i])
		}
	}
}

// TestBuildPlanEdgesJSONShape pins the additive-field contract for "edges":
// absent (not null, not []) when no edges were evaluated, a JSON array when
// they were.
func TestBuildPlanEdgesJSONShape(t *testing.T) {
	g := FromConfig(validGraph())

	t.Run("no evaluated edges omits the edges key", func(t *testing.T) {
		got, err := json.Marshal(BuildPlan(g, nil))
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(got, &raw); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		if edgesRaw, ok := raw["edges"]; ok {
			t.Fatalf(`"edges" = %s, want the key absent when no edges were evaluated (never null)`, edgesRaw)
		}
	})

	t.Run("evaluated edges serialize as an array of objects", func(t *testing.T) {
		got, err := json.Marshal(BuildPlan(g, []EdgeState{edge("development", "test")}))
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(got, &raw); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		var edges []json.RawMessage
		if err := json.Unmarshal(raw["edges"], &edges); err != nil {
			t.Fatalf(`"edges" did not unmarshal as a JSON array: %v (raw: %s)`, err, raw["edges"])
		}
		if len(edges) != 1 {
			t.Fatalf("len(edges) = %d, want 1", len(edges))
		}
	})
}

// mergeGraph is the canonical graph with the backflow strategy flipped to
// merge (ExpectedMergeMethod defaults to merge in FromConfig).
func mergeGraph() *Graph {
	cfg := validGraph()
	cfg.Spec.Backflow.Strategy = v1.BackflowStrategyMerge
	return FromConfig(cfg)
}

// mergeBackflowEdge is a merge-strategy backflow edge with n commits to return
// wholesale (ToReturn == DownstreamOnly, since merge edges skip the returnable
// patch-id filter).
func mergeBackflowEdge() EdgeState {
	e := edge("production-stage-1", "main")
	e.DownstreamOnly = []Commit{{SHA: "h1", Subject: "hotfix a"}, {SHA: "h2", Subject: "hotfix b"}}
	e.ToReturn = []Commit{{SHA: "h1", Subject: "hotfix a"}, {SHA: "h2", Subject: "hotfix b"}}
	e.SourceHeadShort = "abc1234"
	return e
}

// TestPlanMergeBackflowCleanEmitsCreateWithStrategy: a clean merge edge (no
// skip in range, target allows merge commits) emits a single
// ActionCreateBackflowRequest carrying Strategy: merge and the wholesale count,
// and the edge summary publishes Strategy + the Returned set.
func TestPlanMergeBackflowCleanEmitsCreateWithStrategy(t *testing.T) {
	g := mergeGraph()
	e := mergeBackflowEdge()
	allow := true
	e.TargetCanMergeCommit = &allow

	plan := BuildPlan(g, []EdgeState{e})
	if len(plan.Actions) != 1 {
		t.Fatalf("actions = %+v, want exactly one backflow action", plan.Actions)
	}
	a := plan.Actions[0]
	if a.Type != ActionCreateBackflowRequest {
		t.Fatalf("type = %q, want %q", a.Type, ActionCreateBackflowRequest)
	}
	if a.Strategy != v1.BackflowStrategyMerge {
		t.Errorf("action Strategy = %q, want %q", a.Strategy, v1.BackflowStrategyMerge)
	}
	if a.From != "main" || a.To != "development" {
		t.Errorf("from/to = %q -> %q, want main -> development", a.From, a.To)
	}
	if a.Unpromoted != 2 {
		t.Errorf("Unpromoted = %d, want 2 (wholesale ToReturn count)", a.Unpromoted)
	}
	if a.Branch != BackflowBranchName("main", "development", "abc1234") {
		t.Errorf("Branch = %q, want deterministic backflow branch", a.Branch)
	}

	if len(plan.Edges) != 1 {
		t.Fatalf("edges = %+v, want one summary", plan.Edges)
	}
	s := plan.Edges[0]
	if s.Strategy != v1.BackflowStrategyMerge {
		t.Errorf("summary Strategy = %q, want %q", s.Strategy, v1.BackflowStrategyMerge)
	}
	if !reflect.DeepEqual(s.Returned, e.ToReturn) {
		t.Errorf("summary Returned = %+v, want %+v (wholesale returned set)", s.Returned, e.ToReturn)
	}
}

// TestPlanMergeBackflowFenceFailReportsDivergence: the live forge signal says
// the target forbids merge commits (TargetCanMergeCommit false), so the edge
// diverges (ADR-0006 Amendment 1) instead of opening a backflow request.
func TestPlanMergeBackflowFenceFailReportsDivergence(t *testing.T) {
	g := mergeGraph()
	e := mergeBackflowEdge()
	forbid := false
	e.TargetCanMergeCommit = &forbid

	plan := BuildPlan(g, []EdgeState{e})
	if len(plan.Actions) != 1 {
		t.Fatalf("actions = %+v, want exactly one divergence action", plan.Actions)
	}
	a := plan.Actions[0]
	if a.Type != ActionReportDivergence {
		t.Fatalf("type = %q, want %q", a.Type, ActionReportDivergence)
	}
	if a.Strategy != "" {
		t.Errorf("divergence Strategy = %q, want empty (only create actions are tagged)", a.Strategy)
	}
	if !strings.Contains(a.Reason, "forbid") {
		t.Errorf("Reason = %q, want a merge-commit fence explanation", a.Reason)
	}
}

// TestPlanMergeBackflowSkipInRangeReportsDivergence: a merge edge cannot honor
// an Oiax-Backflow: skip trailer inside the returnable range, so it is a hard
// error (ADR-0006 Amendment 2), NOT a silent exclusion. The skip check
// precedes the fence check.
func TestPlanMergeBackflowSkipInRangeReportsDivergence(t *testing.T) {
	g := mergeGraph()
	e := mergeBackflowEdge()
	allow := true
	e.TargetCanMergeCommit = &allow
	e.SkippedInRange = []Commit{{SHA: "h2abcdef0123", Subject: "hotfix b"}}

	plan := BuildPlan(g, []EdgeState{e})
	if len(plan.Actions) != 1 {
		t.Fatalf("actions = %+v, want exactly one divergence action", plan.Actions)
	}
	a := plan.Actions[0]
	if a.Type != ActionReportDivergence {
		t.Fatalf("type = %q, want %q", a.Type, ActionReportDivergence)
	}
	if a.Unpromoted != 1 {
		t.Errorf("Unpromoted = %d, want 1 (offending skip count)", a.Unpromoted)
	}
	if !strings.Contains(a.Reason, "skip") || !strings.Contains(a.Reason, "h2abcde") {
		t.Errorf("Reason = %q, want it to name the skip trailer and short SHA h2abcde", a.Reason)
	}
}

// TestPlanMergeBackflowSkipPrecedesFence confirms skip-in-range wins over a
// simultaneous fence failure: both would diverge, but the skip Reason is the
// more actionable, so it is reported.
func TestPlanMergeBackflowSkipPrecedesFence(t *testing.T) {
	g := mergeGraph()
	e := mergeBackflowEdge()
	forbid := false
	e.TargetCanMergeCommit = &forbid
	e.SkippedInRange = []Commit{{SHA: "h2", Subject: "hotfix b"}}

	plan := BuildPlan(g, []EdgeState{e})
	if len(plan.Actions) != 1 || plan.Actions[0].Type != ActionReportDivergence {
		t.Fatalf("actions = %+v, want one divergence action", plan.Actions)
	}
	if !strings.Contains(plan.Actions[0].Reason, "skip") {
		t.Errorf("Reason = %q, want the skip-in-range explanation to win over the fence", plan.Actions[0].Reason)
	}
}

// TestPlanMergeBackflowConvergedEmitsNothing: once the return merges, the
// source head is an ancestor of the target so the downstream range empties and
// the edge produces no action (ancestry settles a merge edge).
func TestPlanMergeBackflowConvergedEmitsNothing(t *testing.T) {
	g := mergeGraph()
	e := edge("production-stage-1", "main")
	// Downstream range empty ⇒ nothing to return.
	plan := BuildPlan(g, []EdgeState{e})
	if len(plan.Actions) != 0 {
		t.Fatalf("actions = %+v, want none (merge edge converged by ancestry)", plan.Actions)
	}
}

// TestPlanCherryPickJSONUnchanged pins the frozen-format invariant: a
// cherry-pick backflow plan's JSON must NOT gain any of the merge-only fields.
// "cherry-pick" is non-empty, so Strategy is deliberately left off cherry-pick
// actions/summaries — setting it would change every existing plan's bytes.
func TestPlanCherryPickJSONUnchanged(t *testing.T) {
	g := FromConfig(validGraph()) // cherry-pick strategy
	e := edge("production-stage-1", "main")
	e.DownstreamOnly = []Commit{{SHA: "x", Subject: "hotfix"}}
	e.ToReturn = []Commit{{SHA: "x", Subject: "hotfix"}}
	e.SourceHeadShort = "abc1234"

	got, err := json.Marshal(BuildPlan(g, []EdgeState{e}))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	for _, key := range []string{"strategy", "returned", "targetCanMergeCommit", "skippedInRange"} {
		if strings.Contains(string(got), key) {
			t.Errorf("cherry-pick plan JSON contains merge-only key %q; the frozen format must stay byte-identical:\n%s", key, got)
		}
	}
}

func TestBuildPlanActionsJSONShape(t *testing.T) {
	g := FromConfig(validGraph())

	t.Run("in sync serializes actions as an empty array, not null", func(t *testing.T) {
		plan := BuildPlan(g, []EdgeState{edge("development", "test")})
		if plan.Actions == nil {
			t.Fatalf("Actions = nil, want non-nil empty slice")
		}
		got, err := json.Marshal(plan)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(got, &raw); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		actionsRaw, ok := raw["actions"]
		if !ok {
			t.Fatal(`marshaled plan has no "actions" key; the frozen contract requires it always present`)
		}
		if actions := string(actionsRaw); actions != "[]" {
			t.Fatalf(`"actions" = %s, want "[]" (strict typed consumers of the frozen contract reject null)`, actions)
		}
	})

	t.Run("populated plan serializes actions as an array of objects", func(t *testing.T) {
		e := edge("development", "test")
		e.Unpromoted = []Commit{{SHA: "d"}, {SHA: "e"}}
		e.Equivalence = EquivalenceReachability

		plan := BuildPlan(g, []EdgeState{e})
		got, err := json.Marshal(plan)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(got, &raw); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		actionsRaw, ok := raw["actions"]
		if !ok {
			t.Fatal(`marshaled plan has no "actions" key; the frozen contract requires it always present`)
		}
		var actions []json.RawMessage
		if err := json.Unmarshal(actionsRaw, &actions); err != nil {
			t.Fatalf(`"actions" did not unmarshal as a JSON array: %v`, err)
		}
		if len(actions) != 1 {
			t.Fatalf("len(actions) = %d, want 1", len(actions))
		}
	})
}
