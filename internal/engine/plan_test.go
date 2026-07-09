package engine

import (
	"testing"

	"github.com/skaphos/oiax/pkg/api/v1alpha1"
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
			name: "downstream-only content on a backflow source is reported",
			edge: func() EdgeState {
				e := edge("production-stage-1", "main")
				e.DownstreamOnly = []Commit{{SHA: "x", Subject: "hotfix"}}
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

func TestBuildPlanRespectsExpectedDrift(t *testing.T) {
	cfg := validGraph()
	// Detach test from backflow concerns and mark its drift expected.
	cfg.Spec.Branches["test"] = v1alpha1.Branch{Drift: v1alpha1.DriftExpected}
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
