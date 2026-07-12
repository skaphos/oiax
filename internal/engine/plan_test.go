package engine

import (
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
