// Package tmpl renders the human-facing text Oiax authors: managed-request
// titles and bodies, the durable backflow-conflict artifact, and the merge
// backflow strategy's --no-ff merge-commit message. Text is produced from Go
// text/template documents — the built-in defaults, or templates configured
// in spec.templates (SKA-54 / issue #54) — over a documented, closed
// variable context (Context) and a curated function set. Configuration
// stays declarative data: there is no arbitrary code execution surface.
//
// Three constraints govern rendering (docs/adr/0011):
//
//   - The marker is Oiax-owned. Templates render only the human text; the
//     provider appends the machine-readable marker block after it. Rendered
//     output that contains a recognizable marker block, or an unclosed HTML
//     comment that would swallow the appended marker, is rejected.
//   - Commit subjects are untrusted free text. They are exposed to bodies
//     (capped in count and length) but never reach marker identity fields,
//     and rendered titles are reduced to a single sanitized line.
//   - Rendering happens once, at creation. Oiax never re-renders the human
//     body of an existing managed request (updates rewrite only the
//     marker), so volatile template output cannot thrash a managed request.
//
// Every configured template is compiled and sample-rendered at load, so a
// broken template fails `oiax validate` (and plan/reconcile) up front, in
// the same round trip as every other configuration error.
package tmpl

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"text/template"

	mk "github.com/skaphos/oiax/internal/forge/marker"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

const (
	// MaxCommits caps the Commits list exposed to a template. Context.Count
	// always carries the true total, so a template can render "and N more".
	MaxCommits = 100
	// maxSubjectLen caps each exposed commit subject, in runes. Subjects are
	// untrusted free text; the cap bounds what an attacker-influenced commit
	// can inject into a rendered body.
	maxSubjectLen = 200
	// maxTitleLen caps a rendered title, in runes. GitHub truncates around
	// 256; a longer render is reduced silently rather than failing a run on
	// attacker-sized input.
	maxTitleLen = 256
	// maxBodyBytes caps a rendered body. GitHub rejects bodies past 65536
	// bytes; the margin leaves room for the appended marker block.
	maxBodyBytes = 60000
	// MaxFileBytes caps a template file read (bodyFile / file references).
	MaxFileBytes = 1 << 20 // 1 MiB
)

// Commit is one commit exposed to a template. Subject is untrusted free
// text (author-controlled); it is capped but not escaped — bodies are
// Markdown, and escaping policy belongs to the template author.
type Commit struct {
	// SHA is the full object id.
	SHA string
	// ShortSHA is the abbreviated object id (first 7 hex digits).
	ShortSHA string
	// Subject is the commit subject line, capped at 200 runes. UNTRUSTED.
	Subject string
}

// Conflict is the failing-commit information a backflow-conflict template
// receives. It is nil for every other surface.
type Conflict struct {
	// SHA is the object id of the commit (or source head) that failed.
	SHA string
	// Subject is its subject line. UNTRUSTED.
	Subject string
	// Applied is the count of commits cherry-picked cleanly before the
	// conflict. Meaningful only when Whole is false.
	Applied int
	// Whole is true for the merge strategy (the whole downstream set was
	// attempted in one --no-ff merge), false for cherry-pick.
	Whole bool
}

// Context is the variable context every template renders over. Fields not
// meaningful for a surface hold their zero value; see
// docs/reference/templates.md for the per-surface population table.
type Context struct {
	// Graph is the promotion graph name (metadata.name).
	Graph string
	// Type is the surface: "promotion", "backflow", or "conflict".
	Type string
	// From is the edge's source branch (for backflow surfaces: the
	// backflow source, i.e. the downstream branch).
	From string
	// To is the edge's destination branch (for backflow surfaces: the
	// backflow target).
	To string
	// Count is the true number of commits the request moves, returns, or
	// failed to return — it can exceed len(Commits), which is capped.
	Count int
	// Commits lists the commits (newest first, capped at MaxCommits).
	// Subjects are UNTRUSTED.
	Commits []Commit
	// SourceHead is the full source head object id.
	SourceHead string
	// SourceHeadShort is the abbreviated source head.
	SourceHeadShort string
	// Strategy is the backflow strategy ("cherry-pick" or "merge"); empty
	// on promotion surfaces.
	Strategy string
	// Mechanism is the human wording for Strategy ("cherry-pick" or
	// "merge commit"); empty on promotion surfaces.
	Mechanism string
	// Conflict carries the failing-commit information on the
	// backflow-conflict surface; nil elsewhere.
	Conflict *Conflict
}

// NewCommit builds a template Commit from a raw sha and subject, applying
// the documented abbreviation and the untrusted-subject length cap.
func NewCommit(sha, subject string) Commit {
	short := sha
	if len(short) > 7 {
		short = short[:7]
	}
	if r := []rune(subject); len(r) > maxSubjectLen {
		subject = string(r[:maxSubjectLen]) + "…"
	}
	return Commit{SHA: sha, ShortSHA: short, Subject: subject}
}

// The built-in templates. Their rendered output is byte-identical to the
// pre-template hardcoded strings (guarded by tests), so adopting this
// package changed no request text.
const (
	defaultPromotionTitle = "oiax: promote {{.From}} to {{.To}}"
	defaultPromotionBody  = "Oiax opened this request to promote {{.Count}} commit(s) from `{{.From}}` into `{{.To}}`.\n\n" +
		"This request is managed by Oiax. Do not edit the metadata block below."

	defaultBackflowTitle = "oiax: backflow {{.From}} to {{.To}}"
	defaultBackflowBody  = "Oiax opened this request to return {{.Count}} downstream-only commit(s) from `{{.From}}` back to `{{.To}}` by {{.Mechanism}}.\n\n" +
		"This request is managed by Oiax. Do not edit the metadata block below."

	defaultConflictTitle = "oiax: backflow conflict {{.From}} -> {{.To}}"
	defaultConflictBody  = "Oiax could not return the downstream-only commits from `{{.From}}` to `{{.To}}`: the " +
		"backflow replay conflicts on the `{{.From}}` -> `{{.To}}` edge (source `{{.SourceHeadShort}}`).\n\n" +
		"**Failing commit:** `{{.Conflict.SHA}}` — {{.Conflict.Subject}}\n\n" +
		"{{if .Conflict.Whole}}The merge strategy attempted the whole downstream source set in a " +
		"single `--no-ff` merge; the merge conflicts, so nothing is returned." +
		"{{else}}The cherry-pick strategy applied {{.Conflict.Applied}} commit(s) cleanly before this one " +
		"conflicted; the replay is aborted and nothing is returned.{{end}}\n\n" +
		"### What to do\n\n" +
		"Resolve by promoting or cherry-picking the fix by hand, or mark the commit " +
		"`Oiax-Backflow: skip` if it should never return. See the " +
		"[backflow guide](docs/guides/backflow.md#when-a-replay-conflicts) for the " +
		"full playbook.\n\n" +
		"Oiax manages this issue. It closes automatically once the conflict clears " +
		"(the replay succeeds, the edge converges, or the commit becomes " +
		"returned/skipped). Do not edit the metadata block below."
)

// funcMap is the curated function set templates may call, beyond
// text/template's builtins (printf, len, index, range, if, ...). It is
// deliberately small and free of anything volatile (no time, no
// environment): rendered output must be a deterministic function of the
// Context.
func funcMap() template.FuncMap {
	return template.FuncMap{
		// trunc caps s at n runes: {{trunc 72 .Subject}}.
		"trunc": func(n int, s string) string {
			if n < 0 {
				n = 0
			}
			if r := []rune(s); len(r) > n {
				return string(r[:n])
			}
			return s
		},
		// shortSHA abbreviates an object id to 7 hex digits.
		"shortSHA": func(s string) string {
			if len(s) > 7 {
				return s[:7]
			}
			return s
		},
	}
}

// pair is one surface's compiled title and body.
type pair struct {
	title, body *template.Template
	// customized is true when either member came from configuration rather
	// than the built-in defaults.
	customized bool
}

// Set is a compiled, load-validated template set. The zero value is not
// usable; build one with Default or Resolve.
type Set struct {
	promotion pair
	// perEdge holds the resolved per-edge promotion overrides, keyed by
	// {from, to}; edges without an override use promotion.
	perEdge  map[[2]string]pair
	backflow pair
	conflict pair
	// mergeMsg is nil when unconfigured: git's default merge message.
	mergeMsg *template.Template
}

// defaultSet compiles the built-in templates once. template.Must is safe:
// the defaults are constants and compile-covered by tests.
var defaultSet = sync.OnceValue(func() *Set {
	compile := func(name, text string) *template.Template {
		return template.Must(template.New(name).Funcs(funcMap()).Parse(text))
	}
	return &Set{
		promotion: pair{
			title: compile("default promotion title", defaultPromotionTitle),
			body:  compile("default promotion body", defaultPromotionBody),
		},
		backflow: pair{
			title: compile("default backflow title", defaultBackflowTitle),
			body:  compile("default backflow body", defaultBackflowBody),
		},
		conflict: pair{
			title: compile("default conflict title", defaultConflictTitle),
			body:  compile("default conflict body", defaultConflictBody),
		},
	}
})

// Default returns the built-in template set — the text Oiax authors when
// spec.templates is absent.
func Default() *Set {
	return defaultSet()
}

// FileReader reads a repository-relative file from the same source the
// configuration was read from: the pinned config ref for plan/reconcile
// (ADR 0003), the working tree for validate/graph.
type FileReader func(path string) ([]byte, error)

// Resolve compiles the configured template set over the built-in defaults
// and validates every configured template by sample-rendering it. cfg is
// the already-Validated document; read supplies template file contents and
// may be nil when the configuration references no files.
func Resolve(cfg *v1.PromotionGraph, read FileReader) (*Set, error) {
	d := Default()
	s := &Set{
		promotion: d.promotion,
		backflow:  d.backflow,
		conflict:  d.conflict,
	}
	t := cfg.Spec.Templates

	var err error
	if t != nil {
		if s.promotion, err = overlayPair("templates.promotion", s.promotion, t.Promotion, read); err != nil {
			return nil, err
		}
		if s.backflow, err = overlayPair("templates.backflow", s.backflow, t.Backflow, read); err != nil {
			return nil, err
		}
		if s.conflict, err = overlayPair("templates.backflowConflict", s.conflict, t.BackflowConflict, read); err != nil {
			return nil, err
		}
		if m := t.BackflowMergeMessage; m != nil {
			text, terr := templateText("templates.backflowMergeMessage", m.Text, m.File, read)
			if terr != nil {
				return nil, terr
			}
			if s.mergeMsg, terr = compileTemplate("templates.backflowMergeMessage", text); terr != nil {
				return nil, terr
			}
		}
	}
	for _, p := range cfg.Spec.Promotions {
		if p.Templates == nil {
			continue
		}
		where := fmt.Sprintf("promotion edge %s -> %s: templates", p.From, p.To)
		ep, perr := overlayPair(where, s.promotion, p.Templates, read)
		if perr != nil {
			return nil, perr
		}
		if s.perEdge == nil {
			s.perEdge = make(map[[2]string]pair)
		}
		s.perEdge[[2]string{p.From, p.To}] = ep
	}

	if err := s.sampleRender(); err != nil {
		return nil, err
	}
	return s, nil
}

// overlayPair layers one configured RequestTemplate over a base pair,
// per field: an unset title or body keeps the base's.
func overlayPair(where string, base pair, rt *v1.RequestTemplate, read FileReader) (pair, error) {
	if rt == nil {
		return base, nil
	}
	out := base
	out.customized = true
	if rt.Title != "" {
		t, err := compileTemplate(where+".title", rt.Title)
		if err != nil {
			return pair{}, err
		}
		out.title = t
	}
	if rt.Body != "" || rt.BodyFile != "" {
		text, err := templateText(where+".body", rt.Body, rt.BodyFile, read)
		if err != nil {
			return pair{}, err
		}
		t, err := compileTemplate(where+".body", text)
		if err != nil {
			return pair{}, err
		}
		out.body = t
	}
	return out, nil
}

// templateText resolves a template slot's source text: the inline string,
// or the referenced repository file (size-capped). Mutual exclusion was
// already validated on the document.
func templateText(where, inline, file string, read FileReader) (string, error) {
	if file == "" {
		return inline, nil
	}
	if read == nil {
		return "", fmt.Errorf("%s: no file reader available to read %q", where, file)
	}
	data, err := read(file)
	if err != nil {
		return "", fmt.Errorf("%s: read %q: %w", where, file, err)
	}
	if len(data) > MaxFileBytes {
		return "", fmt.Errorf("%s: %q exceeds the %d byte template limit", where, file, MaxFileBytes)
	}
	return string(data), nil
}

func compileTemplate(name, text string) (*template.Template, error) {
	t, err := template.New(name).Funcs(funcMap()).Parse(text)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	return t, nil
}

// sampleRender renders every customized template against a representative
// sample context and applies the output rules, so a template that cannot
// render — or renders marker-forging output — is rejected at load
// (`oiax validate`), not deep inside an apply. The sample carries two
// commits; the conflict surface's sample additionally carries Conflict.
func (s *Set) sampleRender() error {
	sample := Context{
		Graph: "sample-graph",
		From:  "sample-from",
		To:    "sample-to",
		Count: 2,
		Commits: []Commit{
			NewCommit("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "sample: first subject"),
			NewCommit("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "sample: second subject"),
		},
		SourceHead:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SourceHeadShort: "aaaaaaa",
	}
	check := func(p pair, typ string, conflict *Conflict) error {
		if !p.customized {
			return nil
		}
		c := sample
		c.Type = typ
		c.Conflict = conflict
		if typ != "promotion" {
			c.Strategy = "cherry-pick"
			c.Mechanism = "cherry-pick"
		}
		if _, err := renderTitle(p.title, c); err != nil {
			return err
		}
		if _, err := renderBody(p.body, c); err != nil {
			return err
		}
		return nil
	}
	conflict := &Conflict{
		SHA:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Subject: "sample: first subject",
	}
	if err := check(s.promotion, "promotion", nil); err != nil {
		return err
	}
	for _, p := range s.perEdge {
		if err := check(p, "promotion", nil); err != nil {
			return err
		}
	}
	if err := check(s.backflow, "backflow", nil); err != nil {
		return err
	}
	if err := check(s.conflict, "conflict", conflict); err != nil {
		return err
	}
	if s.mergeMsg != nil {
		c := sample
		c.Type = "backflow"
		c.Strategy = "merge"
		c.Mechanism = "merge commit"
		if _, err := renderMergeMessage(s.mergeMsg, c); err != nil {
			return err
		}
	}
	return nil
}

// Promotion renders the title and body of a promotion request for the
// given edge, using the edge's override when one is configured.
func (s *Set) Promotion(from, to string, c Context) (title, body string, err error) {
	p := s.promotion
	if ep, ok := s.perEdge[[2]string{from, to}]; ok {
		p = ep
	}
	return renderPair(p, c)
}

// PromotionCustomized reports whether the given edge's promotion request
// text is configured (globally or per edge) rather than built-in. The
// caller uses it to decide whether assembling the full commit context is
// worth the observation cost.
func (s *Set) PromotionCustomized(from, to string) bool {
	if _, ok := s.perEdge[[2]string{from, to}]; ok {
		return true
	}
	return s.promotion.customized
}

// Backflow renders the title and body of a managed backflow request.
func (s *Set) Backflow(c Context) (title, body string, err error) {
	return renderPair(s.backflow, c)
}

// BackflowConflict renders the title and body of the durable
// backflow-conflict artifact. c.Conflict must be set.
func (s *Set) BackflowConflict(c Context) (title, body string, err error) {
	return renderPair(s.conflict, c)
}

// MergeMessage renders the --no-ff merge-commit message for the merge
// backflow strategy. ok is false when no template is configured — the
// caller keeps git's default message.
func (s *Set) MergeMessage(c Context) (msg string, ok bool, err error) {
	if s.mergeMsg == nil {
		return "", false, nil
	}
	msg, err = renderMergeMessage(s.mergeMsg, c)
	if err != nil {
		return "", false, err
	}
	return msg, true, nil
}

func renderPair(p pair, c Context) (title, body string, err error) {
	if title, err = renderTitle(p.title, c); err != nil {
		return "", "", err
	}
	if body, err = renderBody(p.body, c); err != nil {
		return "", "", err
	}
	return title, body, nil
}

func execute(t *template.Template, c Context) (string, error) {
	var b strings.Builder
	if err := t.Execute(&b, c); err != nil {
		return "", fmt.Errorf("render %s: %w", t.Name(), err)
	}
	return b.String(), nil
}

// renderTitle renders a title and reduces it to one sanitized line:
// control characters (including newlines) become spaces, surrounding
// whitespace is trimmed, and the result is capped at 256 runes. Reduction
// is silent — titles are presentation only, and erroring here would let an
// attacker-sized commit subject fail an entire reconcile. An empty result
// is an error: the forge rejects empty titles.
func renderTitle(t *template.Template, c Context) (string, error) {
	out, err := execute(t, c)
	if err != nil {
		return "", err
	}
	out = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, out)
	out = strings.TrimSpace(out)
	if r := []rune(out); len(r) > maxTitleLen {
		out = string(r[:maxTitleLen])
	}
	if out == "" {
		return "", fmt.Errorf("render %s: rendered title is empty", t.Name())
	}
	return out, nil
}

// renderBody renders a human body and enforces the marker-ownership rules:
// the output must not contain a recognizable oiax marker block (identity is
// Oiax-owned and append-only) nor an unclosed HTML comment (which would
// swallow the marker the provider appends), and must fit the forge's body
// budget with room for that marker.
func renderBody(t *template.Template, c Context) (string, error) {
	out, err := execute(t, c)
	if err != nil {
		return "", err
	}
	if err := checkBodySafety(out); err != nil {
		return "", fmt.Errorf("render %s: %w", t.Name(), err)
	}
	if len(out) > maxBodyBytes {
		return "", fmt.Errorf("render %s: rendered body is %d bytes (max %d)", t.Name(), len(out), maxBodyBytes)
	}
	return out, nil
}

// renderMergeMessage renders the merge-commit message: non-empty after
// trimming trailing whitespace, no NUL, and size-capped. Newlines are
// legitimate (subject + body paragraphs).
func renderMergeMessage(t *template.Template, c Context) (string, error) {
	out, err := execute(t, c)
	if err != nil {
		return "", err
	}
	out = strings.TrimRight(out, " \t\n")
	if out == "" {
		return "", fmt.Errorf("render %s: rendered merge message is empty", t.Name())
	}
	if strings.ContainsRune(out, 0) {
		return "", fmt.Errorf("render %s: rendered merge message contains a NUL byte", t.Name())
	}
	if len(out) > maxBodyBytes {
		return "", fmt.Errorf("render %s: rendered merge message is %d bytes (max %d)", t.Name(), len(out), maxBodyBytes)
	}
	return out, nil
}

// checkBodySafety rejects rendered output that could forge or swallow the
// Oiax-owned marker block the provider appends after the body: a
// recognizable marker block anywhere in the output (marker recognition
// takes the FIRST oiax-keyed HTML comment, so an earlier forged block
// would hijack managed-request identity), or a '<!--' with no following
// '-->' (the appended marker's closer would then terminate the body's
// comment and the marker lines would be read inside the merged block).
// Balanced HTML comments without an oiax key stay legal — governance
// scaffolds legitimately use them as fill-in prompts.
func checkBodySafety(body string) error {
	if _, found := mk.Parse(body); found {
		return errors.New("rendered body contains an oiax marker block; the marker is Oiax-owned and append-only")
	}
	rest := body
	for {
		i := strings.Index(rest, "<!--")
		if i < 0 {
			return nil
		}
		j := strings.Index(rest[i+4:], "-->")
		if j < 0 {
			return errors.New("rendered body contains an unclosed HTML comment ('<!--' without '-->'), which would swallow the appended oiax marker block")
		}
		rest = rest[i+4+j+3:]
	}
}
