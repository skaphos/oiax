package engine

import (
	"strings"
	"testing"

	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

// validGraph returns the canonical five-branch example, which every
// mutation test starts from.
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

func TestValidateAcceptsCanonicalGraph(t *testing.T) {
	g := FromConfig(validGraph())
	if errs := g.Validate(); len(errs) > 0 {
		t.Fatalf("Validate = %v, want no errors", errs)
	}
	if g.Branches["test"].Drift != v1.DriftForbidden {
		t.Errorf("default drift = %q, want %q", g.Branches["test"].Drift, v1.DriftForbidden)
	}
}

func TestValidateRejections(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*v1.PromotionGraph)
		wantErr string
	}{
		{
			name:    "missing name",
			mutate:  func(c *v1.PromotionGraph) { c.Metadata.Name = "" },
			wantErr: "metadata.name is required",
		},
		{
			name: "no branches",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Branches = nil
				c.Spec.Promotions = nil
				c.Spec.Backflow = nil
			},
			wantErr: "at least one branch",
		},
		{
			name: "cycle",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Promotions = append(c.Spec.Promotions, v1.Promotion{From: "qa", To: "test"})
			},
			wantErr: "cycle",
		},
		{
			name: "self edge",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Promotions = append(c.Spec.Promotions, v1.Promotion{From: "qa", To: "qa"})
			},
			wantErr: "two distinct branches",
		},
		{
			name: "duplicate edge",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Promotions = append(c.Spec.Promotions, v1.Promotion{From: "test", To: "qa"})
			},
			wantErr: "declared more than once",
		},
		{
			name: "undeclared branch in edge",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Promotions = append(c.Spec.Promotions, v1.Promotion{From: "qa", To: "staging"})
			},
			wantErr: `"staging" is not declared`,
		},
		{
			name: "source with incoming edge",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Promotions = append(c.Spec.Promotions, v1.Promotion{From: "test", To: "development"})
			},
			wantErr: "destination of promotion edge",
		},
		{
			name: "terminal with outgoing edge",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Branches["extra"] = v1.Branch{}
				c.Spec.Promotions = append(c.Spec.Promotions, v1.Promotion{From: "main", To: "extra"})
			},
			wantErr: "source of promotion edge",
		},
		{
			name: "unknown role",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Branches["qa"] = v1.Branch{Role: "gateway"}
			},
			wantErr: "unknown role",
		},
		{
			name: "unknown drift",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Branches["qa"] = v1.Branch{Drift: "tolerated"}
			},
			wantErr: "unknown drift policy",
		},
		{
			name: "unknown merge method",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Promotions[0].Expectations = &v1.Expectations{MergeMethod: "fast-forward"}
			},
			wantErr: "unknown mergeMethod",
		},
		{
			name: "backflow target without source role",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Branches["development"] = v1.Branch{}
			},
			wantErr: `must have role "source"`,
		},
		{
			name: "backflow target undeclared",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Backflow.Target = "trunk"
			},
			wantErr: `target "trunk" is not declared`,
		},
		{
			name: "backflow source undeclared",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Backflow.Sources = append(c.Spec.Backflow.Sources, "hotfix")
			},
			wantErr: `source "hotfix" is not declared`,
		},
		{
			name: "backflow source equals target",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Backflow.Sources = append(c.Spec.Backflow.Sources, "development")
			},
			wantErr: "both a backflow source and the backflow target",
		},
		{
			name: "backflow source with expected drift",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Branches["main"] = v1.Branch{Role: v1.RoleTerminal, Drift: v1.DriftExpected}
			},
			wantErr: "must be returned, not ignored",
		},
		{
			name: "backflow without sources",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Backflow.Sources = nil
			},
			wantErr: "at least one source",
		},
		{
			name: "unknown backflow strategy",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Backflow.Strategy = "rebase-and-merge"
			},
			wantErr: "unknown strategy",
		},
		{
			name: "unknown backflow expectedMergeMethod",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Backflow.ExpectedMergeMethod = "fast-forward"
			},
			wantErr: "unknown expectedMergeMethod",
		},
		{
			name: "merge strategy with non-merge expectedMergeMethod",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Backflow.Strategy = v1.BackflowStrategyMerge
				c.Spec.Backflow.ExpectedMergeMethod = v1.MergeMethodSquash
			},
			wantErr: "requires expectedMergeMethod",
		},
		{
			name: "branch name contains a space",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Branches["foo bar"] = v1.Branch{}
			},
			wantErr: "invalid branch name",
		},
		{
			name: "branch name contains '..'",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Branches["foo..bar"] = v1.Branch{}
			},
			wantErr: "invalid branch name",
		},
		{
			name: "branch name begins with '-'",
			mutate: func(c *v1.PromotionGraph) {
				c.Spec.Branches["-foo"] = v1.Branch{}
			},
			wantErr: "invalid branch name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validGraph()
			tt.mutate(cfg)
			errs := FromConfig(cfg).Validate()
			if len(errs) == 0 {
				t.Fatal("Validate = no errors, want at least one")
			}
			for _, err := range errs {
				if strings.Contains(err.Error(), tt.wantErr) {
					return
				}
			}
			t.Errorf("no validation error contains %q; got %v", tt.wantErr, errs)
		})
	}
}

func TestValidateAcceptsMergeStrategy(t *testing.T) {
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
			if errs := g.Validate(); len(errs) > 0 {
				t.Fatalf("Validate = %v, want no errors", errs)
			}
			if g.Backflow.Strategy != v1.BackflowStrategyMerge {
				t.Errorf("resolved strategy = %q, want %q", g.Backflow.Strategy, v1.BackflowStrategyMerge)
			}
			if g.Backflow.ExpectedMergeMethod != tt.wantResolved {
				t.Errorf("resolved expectedMergeMethod = %q, want %q", g.Backflow.ExpectedMergeMethod, tt.wantResolved)
			}
		})
	}
}

// TestValidateAcceptsCherryPickMergeMethods pins that squash and rebase are
// valid backflow expectedMergeMethod values when paired with the cherry-pick
// strategy (the merge-strategy-only "requires expectedMergeMethod merge" rule
// does not apply). Without these positive cases, narrowing the accept list at
// validate.go would silently reject a cherry-pick config with
// expectedMergeMethod=squash or =rebase and no test would catch it.
func TestValidateAcceptsCherryPickMergeMethods(t *testing.T) {
	for _, method := range []v1.MergeMethod{v1.MergeMethodSquash, v1.MergeMethodRebase} {
		method := method
		t.Run(string(method), func(t *testing.T) {
			cfg := validGraph()
			cfg.Spec.Backflow.Strategy = v1.BackflowStrategyCherryPick
			cfg.Spec.Backflow.ExpectedMergeMethod = method

			g := FromConfig(cfg)
			if errs := g.Validate(); len(errs) > 0 {
				t.Fatalf("Validate = %v, want no errors", errs)
			}
			if g.Backflow.ExpectedMergeMethod != method {
				t.Errorf("resolved expectedMergeMethod = %q, want %q", g.Backflow.ExpectedMergeMethod, method)
			}
		})
	}
}

func TestValidateAcceptsAtSignBranchName(t *testing.T) {
	// "@" is accepted by the real git binary (`git check-ref-format --branch
	// @` exits 0), so the engine's pure-Go validator must accept it too:
	// the doc comment on validateRefName promises the two checks agree by
	// construction.
	cfg := validGraph()
	cfg.Spec.Branches["@"] = v1.Branch{}
	cfg.Spec.Promotions = append(cfg.Spec.Promotions, v1.Promotion{From: "qa", To: "@"})

	if errs := FromConfig(cfg).Validate(); len(errs) > 0 {
		t.Fatalf("Validate = %v, want no errors", errs)
	}
}

func TestValidateDisconnectedComponentsAllowed(t *testing.T) {
	cfg := validGraph()
	cfg.Spec.Branches["docs-draft"] = v1.Branch{}
	cfg.Spec.Branches["docs-live"] = v1.Branch{}
	cfg.Spec.Promotions = append(cfg.Spec.Promotions, v1.Promotion{From: "docs-draft", To: "docs-live"})

	if errs := FromConfig(cfg).Validate(); len(errs) > 0 {
		t.Fatalf("Validate = %v, want no errors (disconnected components are allowed)", errs)
	}
}
