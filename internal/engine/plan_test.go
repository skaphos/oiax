package engine

import (
	"encoding/json"
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
