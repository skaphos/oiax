package v1

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Default applies the documented defaulting pass in place, following the
// Kubernetes convention of mutating the receiver:
//
//   - an empty APIVersion becomes APIVersion (the canonical string) and an
//     empty Kind becomes KindPromotionGraph, so a hand-constructed document
//     needs no boilerplate;
//   - a branch with no drift policy gets DriftForbidden;
//   - a declared backflow policy with no strategy gets
//     BackflowStrategyCherryPick, and the merge strategy with no
//     expectedMergeMethod gets MergeMethodMerge (the only method that
//     preserves the returned commits' SHAs and ancestry).
//
// Default never overwrites a set field and is idempotent. It applies no
// semantic judgement — call Validate to check the result.
func (g *PromotionGraph) Default() {
	if g.APIVersion == "" {
		g.APIVersion = APIVersion
	}
	if g.Kind == "" {
		g.Kind = KindPromotionGraph
	}
	for name, b := range g.Spec.Branches {
		if b.Drift == "" {
			b.Drift = DriftForbidden
			g.Spec.Branches[name] = b
		}
	}
	if bf := g.Spec.Backflow; bf != nil {
		if bf.Strategy == "" {
			bf.Strategy = BackflowStrategyCherryPick
		}
		if bf.Strategy == BackflowStrategyMerge && bf.ExpectedMergeMethod == "" {
			bf.ExpectedMergeMethod = MergeMethodMerge
		}
	}
}

// Validate checks the semantic rules a PromotionGraph must satisfy before
// reconciliation. It returns every violation found, in a deterministic
// order, so a broken configuration is fixed in one round trip rather than
// one error at a time. A nil result means the document is valid.
//
// Validate is the canonical rule set: internal validation consumes it, so
// an external Go integrator validating a document gets exactly the checks
// `oiax validate` runs. It accepts a document with or without Default
// applied — every field with a documented default (drift, backflow
// strategy, expectedMergeMethod) may be left unset — but APIVersion and
// Kind are required document fields; call Default first (or set them) on a
// hand-constructed document.
//
// Ref existence ("every configured branch must exist") is deliberately not
// checked here: it requires repository state, which this pure package
// never fetches.
func (g *PromotionGraph) Validate() []error {
	var errs []error
	report := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if g.APIVersion != APIVersion && g.APIVersion != APIVersionV1Alpha1 {
		report("unsupported apiVersion %q (want %q)", g.APIVersion, APIVersion)
	}
	if g.Kind != KindPromotionGraph {
		report("unsupported kind %q (want %q)", g.Kind, KindPromotionGraph)
	}
	if g.Metadata.Name == "" {
		report("metadata.name is required")
	}
	if len(g.Spec.Branches) == 0 {
		report("spec.branches must declare at least one branch")
	}

	for _, name := range sortedBranchNames(g.Spec.Branches) {
		b := g.Spec.Branches[name]
		if err := validateRefName(name); err != nil {
			report("branch %q: invalid branch name: %v", name, err)
		}
		switch b.Role {
		case RoleNone, RoleSource, RoleTerminal:
		default:
			report("branch %q: unknown role %q (want %q, %q, or unset)", name, b.Role, RoleSource, RoleTerminal)
		}
		switch b.Drift {
		// An unset drift policy is valid: Default resolves it to DriftForbidden.
		case "", DriftForbidden, DriftExpected:
		default:
			report("branch %q: unknown drift policy %q (want %q or %q)", name, b.Drift, DriftForbidden, DriftExpected)
		}
	}

	seen := make(map[[2]string]bool, len(g.Spec.Promotions))
	for i, p := range g.Spec.Promotions {
		if p.From == "" || p.To == "" {
			report("promotion edge %d: from and to are required", i)
			continue
		}
		if _, ok := g.Spec.Branches[p.From]; !ok {
			report("promotion edge %s -> %s: branch %q is not declared in spec.branches", p.From, p.To, p.From)
		}
		if _, ok := g.Spec.Branches[p.To]; !ok {
			report("promotion edge %s -> %s: branch %q is not declared in spec.branches", p.From, p.To, p.To)
		}
		if p.From == p.To {
			report("promotion edge %s -> %s: an edge must connect two distinct branches", p.From, p.To)
		}
		key := [2]string{p.From, p.To}
		if seen[key] {
			report("promotion edge %s -> %s is declared more than once", p.From, p.To)
		}
		seen[key] = true

		if p.Expectations != nil {
			switch p.Expectations.MergeMethod {
			case "", MergeMethodMerge, MergeMethodSquash, MergeMethodRebase:
			default:
				report("promotion edge %s -> %s: unknown mergeMethod %q (want %q, %q, or %q)",
					p.From, p.To, p.Expectations.MergeMethod,
					MergeMethodMerge, MergeMethodSquash, MergeMethodRebase)
			}
		}

		if from, ok := g.Spec.Branches[p.From]; ok && from.Role == RoleTerminal {
			report("branch %q has role %q but is the source of promotion edge %s -> %s", p.From, RoleTerminal, p.From, p.To)
		}
		if to, ok := g.Spec.Branches[p.To]; ok && to.Role == RoleSource {
			report("branch %q has role %q but is the destination of promotion edge %s -> %s", p.To, RoleSource, p.From, p.To)
		}

		errs = append(errs, validateRequestTemplate(
			fmt.Sprintf("promotion edge %s -> %s: templates", p.From, p.To), p.Templates)...)
	}

	if cycle := findCycle(g.Spec.Promotions); cycle != nil {
		report("promotion graph contains a cycle: %s", strings.Join(cycle, " -> "))
	}

	errs = append(errs, g.validateBackflow()...)
	errs = append(errs, g.validateTemplates()...)
	return errs
}

// validateTemplates checks the shape of spec.templates: mutual exclusion of
// inline and file sources, file-path hygiene, and slot/policy coherence.
// Template SYNTAX is deliberately not checked here — parsing needs the
// curated function set, which is an implementation detail; `oiax validate`
// compiles and sample-renders every template at load instead, so syntax
// errors still surface in the same round trip.
func (g *PromotionGraph) validateTemplates() []error {
	t := g.Spec.Templates
	if t == nil {
		return nil
	}
	var errs []error
	report := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	errs = append(errs, validateRequestTemplate("templates.promotion", t.Promotion)...)
	errs = append(errs, validateRequestTemplate("templates.backflow", t.Backflow)...)
	errs = append(errs, validateRequestTemplate("templates.backflowConflict", t.BackflowConflict)...)

	// The backflow slots template artifacts only the backflow flow authors;
	// declaring them without a backflow policy is dead configuration and
	// almost certainly a mistake, so it is rejected like an unknown field.
	if g.Spec.Backflow == nil {
		if t.Backflow != nil {
			report("templates.backflow requires spec.backflow to be declared")
		}
		if t.BackflowConflict != nil {
			report("templates.backflowConflict requires spec.backflow to be declared")
		}
	}

	if m := t.BackflowMergeMessage; m != nil {
		if m.Text == "" && m.File == "" {
			report("templates.backflowMergeMessage: declare text or file")
		}
		if m.Text != "" && m.File != "" {
			report("templates.backflowMergeMessage: text and file are mutually exclusive")
		}
		if m.File != "" {
			if err := validateTemplatePath(m.File); err != nil {
				report("templates.backflowMergeMessage: invalid file %q: %v", m.File, err)
			}
		}
		// The merge-commit message exists only on the merge strategy;
		// cherry-pick replays original commits whose messages git owns
		// (the -x provenance trailer is load-bearing identity, ADR 0004).
		if g.Spec.Backflow == nil || g.Spec.Backflow.Strategy != BackflowStrategyMerge {
			report("templates.backflowMergeMessage requires spec.backflow.strategy %q", BackflowStrategyMerge)
		}
	}
	return errs
}

// validateRequestTemplate checks one title/body template slot: at least one
// field set, body and bodyFile mutually exclusive, and a clean repository-
// relative bodyFile path.
func validateRequestTemplate(where string, t *RequestTemplate) []error {
	if t == nil {
		return nil
	}
	var errs []error
	report := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}
	if t.Title == "" && t.Body == "" && t.BodyFile == "" {
		report("%s: declare title, body, or bodyFile", where)
	}
	if t.Body != "" && t.BodyFile != "" {
		report("%s: body and bodyFile are mutually exclusive", where)
	}
	if t.BodyFile != "" {
		if err := validateTemplatePath(t.BodyFile); err != nil {
			report("%s: invalid bodyFile %q: %v", where, t.BodyFile, err)
		}
	}
	return errs
}

// validateTemplatePath checks a template file reference: a clean
// repository-relative path, forward slashes only, that cannot escape the
// repository. The path is later handed to `git show <ref>:<path>` (or a
// working-tree read), so the same string must be safe for both.
func validateTemplatePath(p string) error {
	if strings.HasPrefix(p, "/") {
		return fmt.Errorf("must be repository-relative, not absolute")
	}
	if strings.Contains(p, "\\") {
		return fmt.Errorf("must use forward slashes")
	}
	if strings.HasPrefix(p, "-") {
		return fmt.Errorf("must not begin with '-'")
	}
	for _, r := range p {
		if r < 0o40 || r == 0o177 {
			return fmt.Errorf("must not contain control characters")
		}
	}
	for _, component := range strings.Split(p, "/") {
		switch component {
		case "":
			return fmt.Errorf("must not contain empty path components")
		case ".", "..":
			return fmt.Errorf("must not contain %q path components", component)
		}
	}
	return nil
}

func (g *PromotionGraph) validateBackflow() []error {
	bf := g.Spec.Backflow
	if bf == nil {
		return nil
	}
	var errs []error
	report := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	switch bf.Strategy {
	// An unset strategy is valid: Default resolves it to cherry-pick.
	case "", BackflowStrategyCherryPick, BackflowStrategyMerge:
	default:
		report("backflow: unknown strategy %q (v1 supports %q or %q)", bf.Strategy, BackflowStrategyCherryPick, BackflowStrategyMerge)
	}
	switch bf.ExpectedMergeMethod {
	case "", MergeMethodMerge, MergeMethodSquash, MergeMethodRebase:
	default:
		report("backflow: unknown expectedMergeMethod %q (want %q, %q, or %q)",
			bf.ExpectedMergeMethod, MergeMethodMerge, MergeMethodSquash, MergeMethodRebase)
	}
	if bf.Strategy == BackflowStrategyMerge && bf.ExpectedMergeMethod != "" && bf.ExpectedMergeMethod != MergeMethodMerge {
		report("backflow: strategy %q requires expectedMergeMethod %q", BackflowStrategyMerge, MergeMethodMerge)
	}
	if bf.Target == "" {
		report("backflow: target is required")
	} else if target, ok := g.Spec.Branches[bf.Target]; !ok {
		report("backflow: target %q is not declared in spec.branches", bf.Target)
	} else if target.Role != RoleSource {
		report("backflow: target %q must have role %q", bf.Target, RoleSource)
	}
	if len(bf.Sources) == 0 {
		report("backflow: at least one source is required")
	}

	seen := make(map[string]bool, len(bf.Sources))
	for _, src := range bf.Sources {
		if seen[src] {
			report("backflow: source %q is declared more than once", src)
			continue
		}
		seen[src] = true
		if src == bf.Target {
			report("backflow: %q cannot be both a backflow source and the backflow target", src)
			continue
		}
		b, ok := g.Spec.Branches[src]
		if !ok {
			report("backflow: source %q is not declared in spec.branches", src)
			continue
		}
		if b.Drift == DriftExpected {
			report("backflow: source %q declares drift %q; a backflow source's downstream-only content must be returned, not ignored", src, DriftExpected)
		}
	}
	return errs
}

// validateRefName checks a branch name against the subset of `git
// check-ref-format --branch` rules that apply to a bare branch name (no
// refs/heads/ prefix, no @{-N} shorthand resolution). It is a pure Go
// re-implementation, not a call into git, because this package must stay
// dependency-free: reconciling this once here means a malformed branch
// name in the config is reported at validate time, in the same round trip
// as every other configuration error, instead of surfacing as a raw git
// error deep inside a later plan or reconcile. The CLI's git layer enforces
// the identical rule set via the real git binary wherever a ref actually
// reaches a git subprocess, so the two checks agree by construction.
func validateRefName(name string) error {
	if name == "" {
		return fmt.Errorf("must not be empty")
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("must not begin with '-'")
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return fmt.Errorf("must not begin or end with '/'")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("must not contain '..'")
	}
	if strings.Contains(name, "@{") {
		return fmt.Errorf("must not contain '@{'")
	}
	if strings.HasSuffix(name, ".") {
		return fmt.Errorf("must not end with '.'")
	}
	for _, component := range strings.Split(name, "/") {
		if component == "" {
			return fmt.Errorf("must not contain consecutive '/'")
		}
		if strings.HasPrefix(component, ".") {
			return fmt.Errorf("no path component may begin with '.'")
		}
		if strings.HasSuffix(component, ".lock") {
			return fmt.Errorf("no path component may end with '.lock'")
		}
	}
	for _, r := range name {
		switch {
		case r < 0o40 || r == 0o177:
			return fmt.Errorf("must not contain control characters")
		case strings.ContainsRune(" ~^:?*[\\", r):
			return fmt.Errorf("must not contain %q", string(r))
		}
	}
	return nil
}

// findCycle returns the branches of one promotion cycle (first node
// repeated at the end), or nil when the graph is acyclic. Detection is
// deterministic: neighbors are visited in declaration order and roots in
// sorted order.
func findCycle(promotions []Promotion) []string {
	next := make(map[string][]string, len(promotions))
	for _, p := range promotions {
		if p.From == p.To || p.From == "" || p.To == "" {
			continue // reported separately
		}
		next[p.From] = append(next[p.From], p.To)
	}

	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := make(map[string]int, len(next))
	var stack []string

	var visit func(node string) []string
	visit = func(node string) []string {
		state[node] = visiting
		stack = append(stack, node)
		for _, n := range next[node] {
			switch state[n] {
			case visiting:
				i := slices.Index(stack, n)
				return append(slices.Clone(stack[i:]), n)
			case unvisited:
				if cycle := visit(n); cycle != nil {
					return cycle
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[node] = done
		return nil
	}

	roots := make([]string, 0, len(next))
	for node := range next {
		roots = append(roots, node)
	}
	sort.Strings(roots)
	for _, node := range roots {
		if state[node] == unvisited {
			if cycle := visit(node); cycle != nil {
				return cycle
			}
		}
	}
	return nil
}

func sortedBranchNames(branches map[string]Branch) []string {
	names := make([]string, 0, len(branches))
	for name := range branches {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
