// Package engine holds the provider-neutral core of Oiax: promotion
// graph validation, edge evaluation, and planning.
//
// Two purity rules govern this package:
//
//   - The planner must not make provider API calls. Observation happens
//     before planning; application happens after.
//   - Given identical graph and observed state, the planner must produce
//     an equivalent plan.
//
// Nothing in this package may import the git layer or a forge provider;
// both are injected as observed state.
package engine

import (
	"slices"

	"github.com/skaphos/oiax/pkg/api/v1alpha1"
)

// Graph is the engine's view of a promotion graph.
type Graph struct {
	Name       string
	Branches   map[string]Branch
	Promotions []Promotion
	Backflow   BackflowPolicy
}

// Branch is the engine's view of per-branch settings, with defaults
// applied (an unset drift policy is DriftForbidden).
type Branch struct {
	Role  v1alpha1.Role
	Drift v1alpha1.DriftPolicy
}

// Promotion is one directed promotion edge.
type Promotion struct {
	From         string
	To           string
	Expectations Expectations
}

// Expectations are reporting-only edge expectations.
type Expectations struct {
	// MergeMethod is empty when the edge declares no expectation.
	MergeMethod v1alpha1.MergeMethod
}

// BackflowPolicy is the engine's view of the backflow configuration,
// with defaults applied. Enabled is false when the graph declares no
// backflow.
type BackflowPolicy struct {
	Enabled  bool
	Sources  []string
	Target   string
	Strategy v1alpha1.BackflowStrategy
}

// FromConfig converts a parsed configuration document into the engine
// model, applying defaults. It does not validate; call Validate on the
// result.
func FromConfig(cfg *v1alpha1.PromotionGraph) *Graph {
	g := &Graph{
		Name:     cfg.Metadata.Name,
		Branches: make(map[string]Branch, len(cfg.Spec.Branches)),
	}
	for name, b := range cfg.Spec.Branches {
		drift := b.Drift
		if drift == "" {
			drift = v1alpha1.DriftForbidden
		}
		g.Branches[name] = Branch{Role: b.Role, Drift: drift}
	}
	for _, p := range cfg.Spec.Promotions {
		edge := Promotion{From: p.From, To: p.To}
		if p.Expectations != nil {
			edge.Expectations.MergeMethod = p.Expectations.MergeMethod
		}
		g.Promotions = append(g.Promotions, edge)
	}
	if bf := cfg.Spec.Backflow; bf != nil {
		strategy := bf.Strategy
		if strategy == "" {
			strategy = v1alpha1.BackflowStrategyCherryPick
		}
		g.Backflow = BackflowPolicy{
			Enabled:  true,
			Sources:  slices.Clone(bf.Sources),
			Target:   bf.Target,
			Strategy: strategy,
		}
	}
	return g
}
