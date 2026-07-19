package v1

import (
	"strings"
	"testing"
)

// validGraph returns the canonical five-branch example, which every
// mutation test starts from.
func validGraph() *PromotionGraph {
	return &PromotionGraph{
		APIVersion: APIVersion,
		Kind:       KindPromotionGraph,
		Metadata:   Metadata{Name: "environments"},
		Spec: PromotionGraphSpec{
			Branches: map[string]Branch{
				"development":        {Role: RoleSource},
				"test":               {},
				"qa":                 {},
				"production-stage-1": {},
				"main":               {Role: RoleTerminal},
			},
			Promotions: []Promotion{
				{From: "development", To: "test"},
				{From: "test", To: "qa"},
				{From: "qa", To: "production-stage-1"},
				{From: "production-stage-1", To: "main"},
			},
			Backflow: &Backflow{
				Sources:  []string{"production-stage-1", "main"},
				Target:   "development",
				Strategy: BackflowStrategyCherryPick,
			},
		},
	}
}

func TestValidateAcceptsCanonicalGraph(t *testing.T) {
	if errs := validGraph().Validate(); len(errs) > 0 {
		t.Fatalf("Validate = %v, want no errors", errs)
	}
}

func TestValidateAcceptsDeprecatedAPIVersionAlias(t *testing.T) {
	cfg := validGraph()
	cfg.APIVersion = APIVersionV1Alpha1
	if errs := cfg.Validate(); len(errs) > 0 {
		t.Fatalf("Validate = %v, want no errors (the v1alpha1 alias is accepted)", errs)
	}
}

// TestValidateAcceptsUndefaultedDocument pins that Validate does not require
// Default: every field with a documented default may be left unset.
func TestValidateAcceptsUndefaultedDocument(t *testing.T) {
	cfg := validGraph()
	cfg.Spec.Backflow.Strategy = "" // defaults to cherry-pick
	for name, b := range cfg.Spec.Branches {
		b.Drift = "" // defaults to forbidden
		cfg.Spec.Branches[name] = b
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		t.Fatalf("Validate = %v, want no errors for an undefaulted document", errs)
	}
}

func TestValidateRejections(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*PromotionGraph)
		wantErr string
	}{
		{
			name:    "unsupported apiVersion",
			mutate:  func(c *PromotionGraph) { c.APIVersion = "oiax.skaphos.dev/v2" },
			wantErr: "unsupported apiVersion",
		},
		{
			name:    "unset apiVersion",
			mutate:  func(c *PromotionGraph) { c.APIVersion = "" },
			wantErr: "unsupported apiVersion",
		},
		{
			name:    "unsupported kind",
			mutate:  func(c *PromotionGraph) { c.Kind = "PromotionPipeline" },
			wantErr: "unsupported kind",
		},
		{
			name:    "missing name",
			mutate:  func(c *PromotionGraph) { c.Metadata.Name = "" },
			wantErr: "metadata.name is required",
		},
		{
			name: "no branches",
			mutate: func(c *PromotionGraph) {
				c.Spec.Branches = nil
				c.Spec.Promotions = nil
				c.Spec.Backflow = nil
			},
			wantErr: "at least one branch",
		},
		{
			name: "cycle",
			mutate: func(c *PromotionGraph) {
				c.Spec.Promotions = append(c.Spec.Promotions, Promotion{From: "qa", To: "test"})
			},
			wantErr: "cycle",
		},
		{
			name: "self edge",
			mutate: func(c *PromotionGraph) {
				c.Spec.Promotions = append(c.Spec.Promotions, Promotion{From: "qa", To: "qa"})
			},
			wantErr: "two distinct branches",
		},
		{
			name: "duplicate edge",
			mutate: func(c *PromotionGraph) {
				c.Spec.Promotions = append(c.Spec.Promotions, Promotion{From: "test", To: "qa"})
			},
			wantErr: "declared more than once",
		},
		{
			name: "undeclared branch in edge",
			mutate: func(c *PromotionGraph) {
				c.Spec.Promotions = append(c.Spec.Promotions, Promotion{From: "qa", To: "staging"})
			},
			wantErr: `"staging" is not declared`,
		},
		{
			name: "source with incoming edge",
			mutate: func(c *PromotionGraph) {
				c.Spec.Promotions = append(c.Spec.Promotions, Promotion{From: "test", To: "development"})
			},
			wantErr: "destination of promotion edge",
		},
		{
			name: "terminal with outgoing edge",
			mutate: func(c *PromotionGraph) {
				c.Spec.Branches["extra"] = Branch{}
				c.Spec.Promotions = append(c.Spec.Promotions, Promotion{From: "main", To: "extra"})
			},
			wantErr: "source of promotion edge",
		},
		{
			name: "unknown role",
			mutate: func(c *PromotionGraph) {
				c.Spec.Branches["qa"] = Branch{Role: "gateway"}
			},
			wantErr: "unknown role",
		},
		{
			name: "unknown drift",
			mutate: func(c *PromotionGraph) {
				c.Spec.Branches["qa"] = Branch{Drift: "tolerated"}
			},
			wantErr: "unknown drift policy",
		},
		{
			name: "unknown merge method",
			mutate: func(c *PromotionGraph) {
				c.Spec.Promotions[0].Expectations = &Expectations{MergeMethod: "fast-forward"}
			},
			wantErr: "unknown mergeMethod",
		},
		{
			name: "backflow target without source role",
			mutate: func(c *PromotionGraph) {
				c.Spec.Branches["development"] = Branch{}
			},
			wantErr: `must have role "source"`,
		},
		{
			name: "backflow target undeclared",
			mutate: func(c *PromotionGraph) {
				c.Spec.Backflow.Target = "trunk"
			},
			wantErr: `target "trunk" is not declared`,
		},
		{
			name: "backflow source undeclared",
			mutate: func(c *PromotionGraph) {
				c.Spec.Backflow.Sources = append(c.Spec.Backflow.Sources, "hotfix")
			},
			wantErr: `source "hotfix" is not declared`,
		},
		{
			name: "backflow source equals target",
			mutate: func(c *PromotionGraph) {
				c.Spec.Backflow.Sources = append(c.Spec.Backflow.Sources, "development")
			},
			wantErr: "both a backflow source and the backflow target",
		},
		{
			name: "backflow source with expected drift",
			mutate: func(c *PromotionGraph) {
				c.Spec.Branches["main"] = Branch{Role: RoleTerminal, Drift: DriftExpected}
			},
			wantErr: "must be returned, not ignored",
		},
		{
			name: "backflow without sources",
			mutate: func(c *PromotionGraph) {
				c.Spec.Backflow.Sources = nil
			},
			wantErr: "at least one source",
		},
		{
			name: "unknown backflow strategy",
			mutate: func(c *PromotionGraph) {
				c.Spec.Backflow.Strategy = "rebase-and-merge"
			},
			wantErr: "unknown strategy",
		},
		{
			name: "unknown backflow expectedMergeMethod",
			mutate: func(c *PromotionGraph) {
				c.Spec.Backflow.ExpectedMergeMethod = "fast-forward"
			},
			wantErr: "unknown expectedMergeMethod",
		},
		{
			name: "merge strategy with non-merge expectedMergeMethod",
			mutate: func(c *PromotionGraph) {
				c.Spec.Backflow.Strategy = BackflowStrategyMerge
				c.Spec.Backflow.ExpectedMergeMethod = MergeMethodSquash
			},
			wantErr: "requires expectedMergeMethod",
		},
		{
			name: "branch name contains a space",
			mutate: func(c *PromotionGraph) {
				c.Spec.Branches["foo bar"] = Branch{}
			},
			wantErr: "invalid branch name",
		},
		{
			name: "branch name contains '..'",
			mutate: func(c *PromotionGraph) {
				c.Spec.Branches["foo..bar"] = Branch{}
			},
			wantErr: "invalid branch name",
		},
		{
			name: "branch name begins with '-'",
			mutate: func(c *PromotionGraph) {
				c.Spec.Branches["-foo"] = Branch{}
			},
			wantErr: "invalid branch name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validGraph()
			tt.mutate(cfg)
			errs := cfg.Validate()
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
	for _, expected := range []MergeMethod{MergeMethodMerge, ""} {
		t.Run("expectedMergeMethod="+string(expected), func(t *testing.T) {
			cfg := validGraph()
			cfg.Spec.Backflow.Strategy = BackflowStrategyMerge
			cfg.Spec.Backflow.ExpectedMergeMethod = expected
			if errs := cfg.Validate(); len(errs) > 0 {
				t.Fatalf("Validate = %v, want no errors", errs)
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
	for _, method := range []MergeMethod{MergeMethodSquash, MergeMethodRebase} {
		t.Run(string(method), func(t *testing.T) {
			cfg := validGraph()
			cfg.Spec.Backflow.Strategy = BackflowStrategyCherryPick
			cfg.Spec.Backflow.ExpectedMergeMethod = method
			if errs := cfg.Validate(); len(errs) > 0 {
				t.Fatalf("Validate = %v, want no errors", errs)
			}
		})
	}
}

func TestValidateAcceptsAtSignBranchName(t *testing.T) {
	// "@" is accepted by the real git binary (`git check-ref-format --branch
	// @` exits 0), so the pure-Go validator must accept it too: the doc
	// comment on validateRefName promises the two checks agree by
	// construction.
	cfg := validGraph()
	cfg.Spec.Branches["@"] = Branch{}
	cfg.Spec.Promotions = append(cfg.Spec.Promotions, Promotion{From: "qa", To: "@"})

	if errs := cfg.Validate(); len(errs) > 0 {
		t.Fatalf("Validate = %v, want no errors", errs)
	}
}

func TestValidateDisconnectedComponentsAllowed(t *testing.T) {
	cfg := validGraph()
	cfg.Spec.Branches["docs-draft"] = Branch{}
	cfg.Spec.Branches["docs-live"] = Branch{}
	cfg.Spec.Promotions = append(cfg.Spec.Promotions, Promotion{From: "docs-draft", To: "docs-live"})

	if errs := cfg.Validate(); len(errs) > 0 {
		t.Fatalf("Validate = %v, want no errors (disconnected components are allowed)", errs)
	}
}

func TestDefault(t *testing.T) {
	cfg := &PromotionGraph{
		Metadata: Metadata{Name: "environments"},
		Spec: PromotionGraphSpec{
			Branches: map[string]Branch{
				"development": {Role: RoleSource},
				"main":        {Role: RoleTerminal, Drift: DriftExpected},
			},
			Promotions: []Promotion{{From: "development", To: "main"}},
			Backflow: &Backflow{
				Sources: []string{"main"},
				Target:  "development",
			},
		},
	}
	cfg.Default()

	if cfg.APIVersion != APIVersion {
		t.Errorf("apiVersion = %q, want %q", cfg.APIVersion, APIVersion)
	}
	if cfg.Kind != KindPromotionGraph {
		t.Errorf("kind = %q, want %q", cfg.Kind, KindPromotionGraph)
	}
	if got := cfg.Spec.Branches["development"].Drift; got != DriftForbidden {
		t.Errorf("unset drift = %q, want %q", got, DriftForbidden)
	}
	if got := cfg.Spec.Branches["main"].Drift; got != DriftExpected {
		t.Errorf("set drift = %q, want %q (Default must not overwrite)", got, DriftExpected)
	}
	if got := cfg.Spec.Backflow.Strategy; got != BackflowStrategyCherryPick {
		t.Errorf("unset strategy = %q, want %q", got, BackflowStrategyCherryPick)
	}
	if got := cfg.Spec.Backflow.ExpectedMergeMethod; got != "" {
		t.Errorf("cherry-pick expectedMergeMethod = %q, want unset (no default)", got)
	}
}

func TestDefaultMergeStrategyExpectedMergeMethod(t *testing.T) {
	cfg := validGraph()
	cfg.Spec.Backflow.Strategy = BackflowStrategyMerge
	cfg.Default()
	if got := cfg.Spec.Backflow.ExpectedMergeMethod; got != MergeMethodMerge {
		t.Errorf("merge-strategy expectedMergeMethod = %q, want %q", got, MergeMethodMerge)
	}
}

func TestDefaultIsIdempotent(t *testing.T) {
	cfg := validGraph()
	cfg.Spec.Backflow.Strategy = "" // exercise the strategy default
	cfg.Default()
	firstStrategy := cfg.Spec.Backflow.Strategy
	firstDrift := cfg.Spec.Branches["test"].Drift
	cfg.Default()
	if cfg.Spec.Backflow.Strategy != firstStrategy || cfg.Spec.Branches["test"].Drift != firstDrift {
		t.Error("Default is not idempotent")
	}
}

// TestValidateTemplates covers the spec.templates shape rules: mutual
// exclusion of inline and file sources, path hygiene, and slot/policy
// coherence (SKA-54). Template syntax is deliberately not validated here;
// internal/tmpl compiles and sample-renders at load.
func TestValidateTemplates(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*PromotionGraph)
		wantErr string
	}{
		{
			name: "valid templates accepted",
			mutate: func(c *PromotionGraph) {
				c.Spec.Backflow.Strategy = BackflowStrategyMerge
				c.Spec.Templates = &Templates{
					Promotion:            &RequestTemplate{Title: "t", BodyFile: ".oiax/templates/promotion.md.tmpl"},
					Backflow:             &RequestTemplate{Body: "b"},
					BackflowConflict:     &RequestTemplate{Title: "t"},
					BackflowMergeMessage: &TextTemplate{Text: "m"},
				}
				c.Spec.Promotions[0].Templates = &RequestTemplate{Body: "edge"}
			},
			wantErr: "",
		},
		{
			name: "empty slot",
			mutate: func(c *PromotionGraph) {
				c.Spec.Templates = &Templates{Promotion: &RequestTemplate{}}
			},
			wantErr: "templates.promotion: declare title, body, or bodyFile",
		},
		{
			name: "body and bodyFile exclusive",
			mutate: func(c *PromotionGraph) {
				c.Spec.Templates = &Templates{Promotion: &RequestTemplate{Body: "b", BodyFile: "f"}}
			},
			wantErr: "body and bodyFile are mutually exclusive",
		},
		{
			name: "per-edge body and bodyFile exclusive",
			mutate: func(c *PromotionGraph) {
				c.Spec.Promotions[0].Templates = &RequestTemplate{Body: "b", BodyFile: "f"}
			},
			wantErr: "promotion edge development -> test: templates: body and bodyFile are mutually exclusive",
		},
		{
			name: "absolute bodyFile",
			mutate: func(c *PromotionGraph) {
				c.Spec.Templates = &Templates{Promotion: &RequestTemplate{BodyFile: "/etc/passwd"}}
			},
			wantErr: "must be repository-relative",
		},
		{
			name: "dotdot bodyFile",
			mutate: func(c *PromotionGraph) {
				c.Spec.Templates = &Templates{Promotion: &RequestTemplate{BodyFile: "../outside.tmpl"}}
			},
			wantErr: "path components",
		},
		{
			name: "backslash bodyFile",
			mutate: func(c *PromotionGraph) {
				c.Spec.Templates = &Templates{Promotion: &RequestTemplate{BodyFile: `dir\file.tmpl`}}
			},
			wantErr: "forward slashes",
		},
		{
			name: "dash-prefixed bodyFile",
			mutate: func(c *PromotionGraph) {
				c.Spec.Templates = &Templates{Promotion: &RequestTemplate{BodyFile: "--output=x"}}
			},
			wantErr: "must not begin with '-'",
		},
		{
			name: "backflow template without backflow policy",
			mutate: func(c *PromotionGraph) {
				c.Spec.Backflow = nil
				c.Spec.Templates = &Templates{Backflow: &RequestTemplate{Body: "b"}}
			},
			wantErr: "templates.backflow requires spec.backflow",
		},
		{
			name: "conflict template without backflow policy",
			mutate: func(c *PromotionGraph) {
				c.Spec.Backflow = nil
				c.Spec.Templates = &Templates{BackflowConflict: &RequestTemplate{Body: "b"}}
			},
			wantErr: "templates.backflowConflict requires spec.backflow",
		},
		{
			name: "merge message text and file exclusive",
			mutate: func(c *PromotionGraph) {
				c.Spec.Backflow.Strategy = BackflowStrategyMerge
				c.Spec.Templates = &Templates{BackflowMergeMessage: &TextTemplate{Text: "m", File: "f"}}
			},
			wantErr: "text and file are mutually exclusive",
		},
		{
			name: "merge message requires merge strategy",
			mutate: func(c *PromotionGraph) {
				c.Spec.Templates = &Templates{BackflowMergeMessage: &TextTemplate{Text: "m"}}
			},
			wantErr: `templates.backflowMergeMessage requires spec.backflow.strategy "merge"`,
		},
		{
			name: "empty merge message slot",
			mutate: func(c *PromotionGraph) {
				c.Spec.Backflow.Strategy = BackflowStrategyMerge
				c.Spec.Templates = &Templates{BackflowMergeMessage: &TextTemplate{}}
			},
			wantErr: "templates.backflowMergeMessage: declare text or file",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validGraph()
			tt.mutate(cfg)
			errs := cfg.Validate()
			if tt.wantErr == "" {
				if len(errs) > 0 {
					t.Fatalf("Validate = %v, want no errors", errs)
				}
				return
			}
			for _, err := range errs {
				if strings.Contains(err.Error(), tt.wantErr) {
					return
				}
			}
			t.Fatalf("Validate = %v, want an error containing %q", errs, tt.wantErr)
		})
	}
}
