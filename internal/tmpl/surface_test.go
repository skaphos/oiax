package tmpl

import (
	"strings"
	"testing"

	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

// conflictTemplates configures only the backflow-conflict body.
func conflictTemplates(body string) *v1.Templates {
	return &v1.Templates{BackflowConflict: &v1.RequestTemplate{Body: body}}
}

func conflictCtx(from, to, subject string, whole bool) Context {
	c := backflowCtx("cherry-pick")
	c.Type = "conflict"
	c.From = from
	c.To = to
	c.Conflict = &Conflict{
		SHA:     "cccccccccccccccccccccccccccccccccccccccc",
		Subject: subject,
		Applied: 2,
		Whole:   whole,
	}
	return c
}

const artifactClosing = "\n\nOiax manages this conflict record. It closes automatically once the conflict clears " +
	"(the replay succeeds, the edge converges, or the commit becomes returned/skipped). " +
	"Do not edit the metadata block below."

// The built-in conflict body renders on both surfaces (spec 002 FR-003,
// FR-004): the artifact keeps the forge-neutral closing sentence, the
// summary drops it, and otherwise ordinary text is identical.
func TestBuiltinConflictBodySurfaces(t *testing.T) {
	for _, whole := range []bool{false, true} {
		c := conflictCtx("main", "development", "fix: conflicting hotfix", whole)
		_, artifact, err := Default().BackflowConflict(c)
		if err != nil {
			t.Fatalf("artifact: %v", err)
		}
		summary, builtin, err := Default().BackflowConflictSummary(c)
		if err != nil {
			t.Fatalf("summary: %v", err)
		}
		if builtin {
			t.Fatal("built-in set reported a fallback")
		}
		if !strings.HasSuffix(artifact, artifactClosing) {
			t.Fatalf("whole=%v artifact body does not end with the closing sentence:\n%s", whole, artifact)
		}
		if strings.Contains(summary, "Oiax manages this conflict record") {
			t.Fatalf("whole=%v summary body keeps the artifact-only sentence:\n%s", whole, summary)
		}
		if want := strings.TrimSuffix(artifact, artifactClosing); summary != want {
			t.Fatalf("whole=%v summary differs beyond the surface sentence:\n%q\nwant\n%q", whole, summary, want)
		}
	}
}

// Branch names Git allows can carry Markdown and shell syntax. In the
// summary, prose and inline code get contained text, while the commands
// keep the literal ref, shell-quoted (spec 002 "Branch names").
func TestBuiltinConflictSummaryHostileBranches(t *testing.T) {
	const from, to = "release/`<b>`x", "dev$(id)"
	summary, _, err := Default().BackflowConflictSummary(conflictCtx(from, to, "fix: x", false))
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	open := strings.Index(summary, "```sh\n")
	end := strings.Index(summary, "\n```\n")
	if open < 0 || end < open {
		t.Fatalf("no sh fence in summary:\n%s", summary)
	}
	fence := summary[open:end]
	outside := summary[:open] + summary[end:]
	if strings.Contains(outside, "<b>") {
		t.Fatalf("branch HTML reached the summary outside the fence:\n%s", outside)
	}
	safe, _, err := Default().BackflowConflictSummary(conflictCtx("main", "development", "fix: x", false))
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	so := strings.Index(safe, "```sh\n")
	se := strings.Index(safe, "\n```\n")
	if got, want := strings.Count(outside, "`"), strings.Count(safe[:so]+safe[se:], "`"); got != want {
		t.Fatalf("branch added backticks outside the fence: %d, want %d", got, want)
	}
	for _, want := range []string{
		"git fetch origin 'release/`<b>`x' 'dev$(id)'",
		"origin/'dev$(id)'",
	} {
		if !strings.Contains(fence, want) {
			t.Fatalf("fence lacks %q:\n%s", want, fence)
		}
	}
	// The artifact keeps the literal names in prose (OOS-004) and quotes
	// them in commands.
	_, artifact, err := Default().BackflowConflict(conflictCtx(from, to, "fix: x", false))
	if err != nil {
		t.Fatalf("artifact: %v", err)
	}
	if !strings.Contains(artifact, "`"+to+"`") || !strings.Contains(artifact, "'dev$(id)'") {
		t.Fatalf("artifact lost the literal branch names:\n%s", artifact)
	}
}

// The built-in summary body prints the subject as contained text; the
// artifact keeps the cleaned value as is.
func TestBuiltinConflictSummaryContainsSubject(t *testing.T) {
	c := conflictCtx("main", "development", "[steps](https://attacker.example) <img src=x>", false)
	summary, _, err := Default().BackflowConflictSummary(c)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	for _, bad := range []string{"[steps]", "](https://attacker", "https://attacker", "<img"} {
		if strings.Contains(summary, bad) {
			t.Fatalf("summary keeps %q:\n%s", bad, summary)
		}
	}
	_, artifact, err := Default().BackflowConflict(c)
	if err != nil {
		t.Fatalf("artifact: %v", err)
	}
	if !strings.Contains(artifact, c.Conflict.Subject) {
		t.Fatalf("artifact lost the subject:\n%s", artifact)
	}
	if c.Conflict.Subject != "[steps](https://attacker.example) <img src=x>" {
		t.Fatal("summary render mutated the caller's context")
	}
}

// A configured body that reads commit text keeps rendering the artifact,
// but the summary uses the built-in body (spec 002 FR-016).
func TestConfiguredConflictBodyFallsBackInSummary(t *testing.T) {
	s := resolve(t, conflictTemplates("Custom: {{.Conflict.Subject}}"))
	c := conflictCtx("main", "development", "fix: x", false)
	_, artifact, err := s.BackflowConflict(c)
	if err != nil {
		t.Fatalf("artifact: %v", err)
	}
	if artifact != "Custom: fix: x" {
		t.Fatalf("artifact = %q", artifact)
	}
	summary, builtin, err := s.BackflowConflictSummary(c)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if !builtin {
		t.Fatal("summary did not report the built-in fallback")
	}
	want, _, err := Default().BackflowConflictSummary(c)
	if err != nil {
		t.Fatalf("built-in summary: %v", err)
	}
	if summary != want {
		t.Fatalf("fallback summary is not the built-in body:\n%s", summary)
	}
}

// A configured body that does not read commit text renders as written on
// both surfaces, differing only where it branches on .Surface (spec 002
// SC-003 (a), (b)).
func TestConfiguredConflictBodyUsedInSummary(t *testing.T) {
	tests := []struct {
		name, body, artifact, summary string
	}{
		{"surface guard", `Edge {{.From}}.{{if eq .Surface "artifact"}} Artifact only.{{end}}`,
			"Edge main. Artifact only.", "Edge main."},
		{"no surface", `Edge {{.From}} at {{shortSHA .Conflict.SHA}}.`,
			"Edge main at ccccccc.", "Edge main at ccccccc."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := resolve(t, conflictTemplates(tt.body))
			c := conflictCtx("main", "development", "fix: x", false)
			_, artifact, err := s.BackflowConflict(c)
			if err != nil {
				t.Fatalf("artifact: %v", err)
			}
			summary, builtin, err := s.BackflowConflictSummary(c)
			if err != nil {
				t.Fatalf("summary: %v", err)
			}
			if builtin {
				t.Fatal("configured body without commit text fell back")
			}
			if artifact != tt.artifact || summary != tt.summary {
				t.Fatalf("artifact = %q, summary = %q; want %q, %q", artifact, summary, tt.artifact, tt.summary)
			}
		})
	}
}

// Resolve sample-renders the conflict body on both surfaces, so a body
// that fails only for the summary is rejected at load.
func TestResolveRejectsSummaryOnlyRenderFailure(t *testing.T) {
	_, err := Resolve(&v1.PromotionGraph{Spec: v1.PromotionGraphSpec{
		Templates: conflictTemplates(`ok{{if eq .Surface "summary"}}{{index "ab" 5}}{{end}}`),
	}}, nil)
	if err == nil {
		t.Fatal("Resolve accepted a body that fails on the summary surface")
	}
	if _, err := Resolve(&v1.PromotionGraph{Spec: v1.PromotionGraphSpec{
		Templates: conflictTemplates(`ok{{if eq .Surface "nowhere"}}{{index "ab" 5}}{{end}}`),
	}}, nil); err != nil {
		t.Fatalf("Resolve rejected a body that renders on both surfaces: %v", err)
	}
}

func TestShellArg(t *testing.T) {
	tests := map[string]string{
		"main":           "main",
		"release/1.2":    "release/1.2",
		"feat_x+y=z@1,2": "feat_x+y=z@1,2",
		"dev$(id)":       "'dev$(id)'",
		"a b":            "'a b'",
		"it's":           `'it'\''s'`,
		"":               "''",
	}
	for in, want := range tests {
		if got := shellArg(in); got != want {
			t.Errorf("shellArg(%q) = %q, want %q", in, got, want)
		}
	}
}
