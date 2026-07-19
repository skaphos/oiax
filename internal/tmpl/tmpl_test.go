package tmpl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

// resolve builds a Set from just a Templates block (and optional per-edge
// promotion entries), the way production reaches Resolve after document
// validation.
func resolve(t *testing.T, ts *v1.Templates, promotions ...v1.Promotion) *Set {
	t.Helper()
	s, err := Resolve(&v1.PromotionGraph{Spec: v1.PromotionGraphSpec{
		Templates:  ts,
		Promotions: promotions,
	}}, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return s
}

func promotionCtx() Context {
	return Context{
		Graph: "environments",
		Type:  "promotion",
		From:  "development",
		To:    "test",
		Count: 3,
		Commits: []Commit{
			NewCommit("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "feat: one"),
			NewCommit("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "fix: two"),
		},
		SourceHead:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SourceHeadShort: "aaaaaaa",
	}
}

func backflowCtx(mechanism string) Context {
	c := promotionCtx()
	c.Type = "backflow"
	c.From = "main"
	c.To = "development"
	c.Strategy = "cherry-pick"
	c.Mechanism = mechanism
	return c
}

// The defaults must reproduce the pre-template hardcoded strings byte for
// byte — adopting the template engine changed no request text.
func TestDefaultsMatchLegacyText(t *testing.T) {
	d := Default()

	title, body, err := d.Promotion("development", "test", promotionCtx())
	if err != nil {
		t.Fatalf("promotion: %v", err)
	}
	if want := "oiax: promote development to test"; title != want {
		t.Errorf("promotion title = %q, want %q", title, want)
	}
	wantBody := "Oiax opened this request to promote 3 commit(s) from `development` into `test`.\n\n" +
		"This request is managed by Oiax. Do not edit the metadata block below."
	if body != wantBody {
		t.Errorf("promotion body = %q, want %q", body, wantBody)
	}

	title, body, err = d.Backflow(backflowCtx("cherry-pick"))
	if err != nil {
		t.Fatalf("backflow: %v", err)
	}
	if want := "oiax: backflow main to development"; title != want {
		t.Errorf("backflow title = %q, want %q", title, want)
	}
	wantBody = "Oiax opened this request to return 3 downstream-only commit(s) from `main` back to `development` by cherry-pick.\n\n" +
		"This request is managed by Oiax. Do not edit the metadata block below."
	if body != wantBody {
		t.Errorf("backflow body = %q, want %q", body, wantBody)
	}
}

// The default conflict body must reproduce the legacy backflowConflictBody
// output for both strategies (the exact fmt.Sprintf text it replaced).
func TestDefaultConflictBodyMatchesLegacyText(t *testing.T) {
	legacy := func(from, to, sha, subject, short string, applied int, whole bool) string {
		var mechanism string
		if whole {
			mechanism = "The merge strategy attempted the whole downstream source set in a " +
				"single `--no-ff` merge; the merge conflicts, so nothing is returned."
		} else {
			mechanism = fmt.Sprintf(
				"The cherry-pick strategy applied %d commit(s) cleanly before this one "+
					"conflicted; the replay is aborted and nothing is returned.", applied)
		}
		return fmt.Sprintf(
			"Oiax could not return the downstream-only commits from `%s` to `%s`: the "+
				"backflow replay conflicts on the `%s` -> `%s` edge (source `%s`).\n\n"+
				"**Failing commit:** `%s` — %s\n\n"+
				"%s\n\n"+
				"### What to do\n\n"+
				"Resolve by promoting or cherry-picking the fix by hand, or mark the commit "+
				"`Oiax-Backflow: skip` if it should never return. See the "+
				"[backflow guide](docs/guides/backflow.md#when-a-replay-conflicts) for the "+
				"full playbook.\n\n"+
				"Oiax manages this issue. It closes automatically once the conflict clears "+
				"(the replay succeeds, the edge converges, or the commit becomes "+
				"returned/skipped). Do not edit the metadata block below.",
			from, to, from, to, short, sha, subject, mechanism)
	}

	for _, whole := range []bool{false, true} {
		c := backflowCtx("cherry-pick")
		c.Type = "conflict"
		c.Conflict = &Conflict{
			SHA:     "cccccccccccccccccccccccccccccccccccccccc",
			Subject: "fix: conflicting hotfix",
			Applied: 2,
			Whole:   whole,
		}
		title, body, err := Default().BackflowConflict(c)
		if err != nil {
			t.Fatalf("whole=%v: %v", whole, err)
		}
		if want := "oiax: backflow conflict main -> development"; title != want {
			t.Errorf("conflict title = %q, want %q", title, want)
		}
		want := legacy("main", "development",
			"cccccccccccccccccccccccccccccccccccccccc", "fix: conflicting hotfix", "aaaaaaa", 2, whole)
		if body != want {
			t.Errorf("whole=%v conflict body =\n%q\nwant\n%q", whole, body, want)
		}
	}
}

func TestResolveCustomInlineAndPerEdgeOverride(t *testing.T) {
	s := resolve(t,
		&v1.Templates{Promotion: &v1.RequestTemplate{
			Title: "promote {{.From}} ({{.Count}} commits)",
			Body:  "Global scaffold for {{.Graph}}.\n{{range .Commits}}- {{.ShortSHA}} {{.Subject}}\n{{end}}",
		}},
		v1.Promotion{From: "test", To: "main", Templates: &v1.RequestTemplate{
			Title: "PROD change record: {{.From}} -> {{.To}}",
		}},
	)

	// Non-overridden edge renders the global custom pair.
	title, body, err := s.Promotion("development", "test", promotionCtx())
	if err != nil {
		t.Fatal(err)
	}
	if want := "promote development (3 commits)"; title != want {
		t.Errorf("title = %q, want %q", title, want)
	}
	if !strings.Contains(body, "- aaaaaaa feat: one") || !strings.Contains(body, "- bbbbbbb fix: two") {
		t.Errorf("body = %q, want the commit list", body)
	}

	// Overridden edge: its own title, but the body falls back per FIELD to
	// the global custom body — not to the built-in default.
	c := promotionCtx()
	c.From, c.To = "test", "main"
	title, body, err = s.Promotion("test", "main", c)
	if err != nil {
		t.Fatal(err)
	}
	if want := "PROD change record: test -> main"; title != want {
		t.Errorf("override title = %q, want %q", title, want)
	}
	if !strings.Contains(body, "Global scaffold for environments.") {
		t.Errorf("override body = %q, want the global custom body", body)
	}

	if !s.PromotionCustomized("development", "test") || !s.PromotionCustomized("test", "main") {
		t.Error("both edges must report customized")
	}
	if Default().PromotionCustomized("development", "test") {
		t.Error("defaults must not report customized")
	}
}

func TestResolveBodyFile(t *testing.T) {
	read := func(path string) ([]byte, error) {
		if path != ".oiax/templates/promotion.md.tmpl" {
			return nil, fmt.Errorf("unexpected path %q", path)
		}
		return []byte("File scaffold: {{.From}} -> {{.To}}"), nil
	}
	cfg := &v1.PromotionGraph{Spec: v1.PromotionGraphSpec{Templates: &v1.Templates{
		Promotion: &v1.RequestTemplate{BodyFile: ".oiax/templates/promotion.md.tmpl"},
	}}}
	s, err := Resolve(cfg, read)
	if err != nil {
		t.Fatal(err)
	}
	_, body, err := s.Promotion("development", "test", promotionCtx())
	if err != nil {
		t.Fatal(err)
	}
	if want := "File scaffold: development -> test"; body != want {
		t.Errorf("body = %q, want %q", body, want)
	}

	// Read failure surfaces with the slot and path.
	_, err = Resolve(cfg, func(string) ([]byte, error) { return nil, fmt.Errorf("no such ref") })
	if err == nil || !strings.Contains(err.Error(), "templates.promotion.body") {
		t.Errorf("read error = %v, want it to name the slot", err)
	}

	// No reader at all is a clear error, not a panic.
	_, err = Resolve(cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "no file reader") {
		t.Errorf("nil-reader error = %v", err)
	}

	// An oversized template file is rejected.
	big := func(string) ([]byte, error) { return make([]byte, MaxFileBytes+1), nil }
	if _, err = Resolve(cfg, big); err == nil || !strings.Contains(err.Error(), "byte template limit") {
		t.Errorf("oversize error = %v", err)
	}
}

// Load-time sample rendering rejects templates that cannot render or whose
// output violates the marker-ownership rules — in validate's round trip,
// not deep inside an apply.
func TestResolveRejectsBrokenTemplates(t *testing.T) {
	cases := []struct {
		name    string
		ts      *v1.Templates
		wantErr string
	}{
		{
			name:    "syntax error",
			ts:      &v1.Templates{Promotion: &v1.RequestTemplate{Body: "{{.From"}},
			wantErr: "parse templates.promotion.body",
		},
		{
			name:    "unknown field",
			ts:      &v1.Templates{Promotion: &v1.RequestTemplate{Body: "{{.Nope}}"}},
			wantErr: "render templates.promotion.body",
		},
		{
			name:    "unknown function",
			ts:      &v1.Templates{Promotion: &v1.RequestTemplate{Body: "{{now}}"}},
			wantErr: "parse templates.promotion.body",
		},
		{
			name: "marker forgery",
			ts: &v1.Templates{Promotion: &v1.RequestTemplate{
				Body: "scaffold\n<!--\noiax:\n  graph: forged\n-->\n"}},
			wantErr: "marker",
		},
		{
			name:    "unclosed comment",
			ts:      &v1.Templates{Promotion: &v1.RequestTemplate{Body: "scaffold <!-- fill in"}},
			wantErr: "unclosed HTML comment",
		},
		{
			name:    "empty title",
			ts:      &v1.Templates{Promotion: &v1.RequestTemplate{Title: "{{if false}}x{{end}}"}},
			wantErr: "title is empty",
		},
		{
			name:    "conflict variable on promotion surface",
			ts:      &v1.Templates{Promotion: &v1.RequestTemplate{Body: "{{.Conflict.SHA}}"}},
			wantErr: "render templates.promotion.body",
		},
		{
			name:    "empty merge message",
			ts:      &v1.Templates{BackflowMergeMessage: &v1.TextTemplate{Text: "  \n"}},
			wantErr: "merge message is empty",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Resolve(&v1.PromotionGraph{Spec: v1.PromotionGraphSpec{Templates: tc.ts}}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// Balanced HTML comments without an oiax key are legal: governance
// scaffolds use them as fill-in prompts.
func TestBodyAllowsBalancedComments(t *testing.T) {
	s := resolve(t, &v1.Templates{Promotion: &v1.RequestTemplate{
		Body: "Approver justification:\n<!-- reviewer: replace with the change justification -->\n",
	}})
	if _, _, err := s.Promotion("development", "test", promotionCtx()); err != nil {
		t.Fatalf("balanced comment rejected: %v", err)
	}
}

// Rendered titles are reduced to one sanitized, capped line: an
// attacker-sized or newline-carrying subject cannot break the title or
// fail the run.
func TestTitleSanitizedAndCapped(t *testing.T) {
	s := resolve(t, &v1.Templates{Promotion: &v1.RequestTemplate{
		Title: "{{(index .Commits 0).Subject}} {{.From}}",
	}})
	c := promotionCtx()
	c.Commits[0].Subject = "evil\nsubject\twith controls"
	title, _, err := s.Promotion("development", "test", c)
	if err != nil {
		t.Fatal(err)
	}
	if want := "evil subject with controls development"; title != want {
		t.Errorf("title = %q, want %q", title, want)
	}

	s = resolve(t, &v1.Templates{Promotion: &v1.RequestTemplate{
		Title: strings.Repeat("x", 500),
	}})
	title, _, err = s.Promotion("development", "test", promotionCtx())
	if err != nil {
		t.Fatal(err)
	}
	if len(title) != 256 {
		t.Errorf("title length = %d, want capped at 256", len(title))
	}
}

func TestCuratedFunctions(t *testing.T) {
	s := resolve(t, &v1.Templates{Promotion: &v1.RequestTemplate{
		Body: `{{trunc 4 "abcdefgh"}} {{shortSHA .SourceHead}}`,
	}})
	_, body, err := s.Promotion("development", "test", promotionCtx())
	if err != nil {
		t.Fatal(err)
	}
	if want := "abcd aaaaaaa"; body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestNewCommitCaps(t *testing.T) {
	c := NewCommit("0123456789abcdef", strings.Repeat("s", 300))
	if c.ShortSHA != "0123456" {
		t.Errorf("ShortSHA = %q", c.ShortSHA)
	}
	if r := []rune(c.Subject); len(r) != maxSubjectLen+1 { // cap + ellipsis
		t.Errorf("subject rune length = %d, want %d", len(r), maxSubjectLen+1)
	}
	if !strings.HasSuffix(c.Subject, "…") {
		t.Errorf("capped subject must end with an ellipsis: %q", c.Subject)
	}
	short := NewCommit("abc", "ok")
	if short.ShortSHA != "abc" || short.Subject != "ok" {
		t.Errorf("short commit mangled: %+v", short)
	}
}

func TestMergeMessage(t *testing.T) {
	// Unconfigured: ok=false, git's default message.
	if _, ok, err := Default().MergeMessage(backflowCtx("merge commit")); ok || err != nil {
		t.Fatalf("default merge message = ok=%v err=%v, want ok=false", ok, err)
	}

	s := resolve(t, &v1.Templates{BackflowMergeMessage: &v1.TextTemplate{
		Text: "backflow: return {{.Count}} commit(s) from {{.From}} to {{.To}}\n\nGraph: {{.Graph}}\n",
	}})
	msg, ok, err := s.MergeMessage(backflowCtx("merge commit"))
	if err != nil || !ok {
		t.Fatalf("merge message: ok=%v err=%v", ok, err)
	}
	want := "backflow: return 3 commit(s) from main to development\n\nGraph: environments"
	if msg != want {
		t.Errorf("msg = %q, want %q (trailing whitespace trimmed)", msg, want)
	}
}

// The example templates shipped in docs/examples/templates must always
// compile and sample-render — they are the copy-paste starting point the
// governance guide points at, so doc drift here is a user-facing break.
func TestShippedExampleTemplatesResolve(t *testing.T) {
	dir := filepath.Join("..", "..", "docs", "examples", "templates")
	slots := map[string]func(body string) *v1.Templates{
		"promotion-change-record.md.tmpl": func(b string) *v1.Templates {
			return &v1.Templates{Promotion: &v1.RequestTemplate{Body: b}}
		},
		"backflow-change-record.md.tmpl": func(b string) *v1.Templates {
			return &v1.Templates{Backflow: &v1.RequestTemplate{Body: b}}
		},
	}
	for name, wrap := range slots {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read shipped example: %v", err)
			}
			if _, err := Resolve(&v1.PromotionGraph{Spec: v1.PromotionGraphSpec{
				Templates: wrap(string(data)),
			}}, nil); err != nil {
				t.Errorf("shipped example must resolve: %v", err)
			}
		})
	}
}
