package engine

import (
	"testing"

	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

// validGraph returns the canonical five-branch example the plan tests
// start from. It mirrors the fixture of the same name in pkg/api/v1's
// validation tests, where the semantic rules themselves are exercised.
func validGraph() *v1.PromotionGraph {
	return &v1.PromotionGraph{
		APIVersion: v1.APIVersion,
		Kind:       v1.KindPromotionGraph,
		Metadata:   v1.Metadata{Name: "environments"},
		Spec: v1.PromotionGraphSpec{
			Branches: map[string]v1.Branch{
				"development":        {Role: v1.RoleSource},
				"test":               {},
				"qa":                 {},
				"production-stage-1": {},
				"main":               {Role: v1.RoleTerminal},
			},
			Promotions: []v1.Promotion{
				{From: "development", To: "test"},
				{From: "test", To: "qa"},
				{From: "qa", To: "production-stage-1"},
				{From: "production-stage-1", To: "main"},
			},
			Backflow: &v1.Backflow{
				Sources:  []string{"production-stage-1", "main"},
				Target:   "development",
				Strategy: v1.BackflowStrategyCherryPick,
			},
		},
	}
}

func TestFromConfigAppliesDriftDefault(t *testing.T) {
	g := FromConfig(validGraph())
	if g.Branches["test"].Drift != v1.DriftForbidden {
		t.Errorf("default drift = %q, want %q", g.Branches["test"].Drift, v1.DriftForbidden)
	}
}

func TestFromConfigResolvesBackflowDefaults(t *testing.T) {
	tests := []struct {
		name         string
		expected     v1.MergeMethod
		wantResolved v1.MergeMethod
	}{
		{
			name:         "explicit merge expectedMergeMethod",
			expected:     v1.MergeMethodMerge,
			wantResolved: v1.MergeMethodMerge,
		},
		{
			name:         "omitted expectedMergeMethod defaults to merge",
			expected:     "",
			wantResolved: v1.MergeMethodMerge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validGraph()
			cfg.Spec.Backflow.Strategy = v1.BackflowStrategyMerge
			cfg.Spec.Backflow.ExpectedMergeMethod = tt.expected

			g := FromConfig(cfg)
			if g.Backflow.Strategy != v1.BackflowStrategyMerge {
				t.Errorf("resolved strategy = %q, want %q", g.Backflow.Strategy, v1.BackflowStrategyMerge)
			}
			if g.Backflow.ExpectedMergeMethod != tt.wantResolved {
				t.Errorf("resolved expectedMergeMethod = %q, want %q", g.Backflow.ExpectedMergeMethod, tt.wantResolved)
			}
		})
	}
}

func TestFromConfigPreservesCherryPickMergeMethods(t *testing.T) {
	for _, method := range []v1.MergeMethod{v1.MergeMethodSquash, v1.MergeMethodRebase} {
		t.Run(string(method), func(t *testing.T) {
			cfg := validGraph()
			cfg.Spec.Backflow.Strategy = v1.BackflowStrategyCherryPick
			cfg.Spec.Backflow.ExpectedMergeMethod = method

			g := FromConfig(cfg)
			if g.Backflow.ExpectedMergeMethod != method {
				t.Errorf("resolved expectedMergeMethod = %q, want %q", g.Backflow.ExpectedMergeMethod, method)
			}
		})
	}
}

// TestFromConfigAgreesWithDefault pins that FromConfig's inline default
// resolution and the exported v1 defaulting pass produce the same resolved
// values: converting an undefaulted document and converting the same
// document after Default must yield the same engine model. This is the
// guard against the two defaulting sites drifting apart.
func TestFromConfigAgreesWithDefault(t *testing.T) {
	undefaulted := validGraph()
	undefaulted.Spec.Backflow.Strategy = "" // exercise every default

	defaulted := validGraph()
	defaulted.Spec.Backflow.Strategy = ""
	defaulted.Default()

	a, b := FromConfig(undefaulted), FromConfig(defaulted)
	if a.Backflow.Strategy != b.Backflow.Strategy ||
		a.Backflow.ExpectedMergeMethod != b.Backflow.ExpectedMergeMethod {
		t.Errorf("backflow policy diverges: FromConfig=%+v FromConfig after Default=%+v", a.Backflow, b.Backflow)
	}
	for name := range a.Branches {
		if a.Branches[name] != b.Branches[name] {
			t.Errorf("branch %q diverges: FromConfig=%+v FromConfig∘Default=%+v", name, a.Branches[name], b.Branches[name])
		}
	}
}
