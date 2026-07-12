package engine

import (
	"fmt"
	"slices"
	"sort"

	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

// Validate checks the semantic rules a promotion graph must satisfy
// before reconciliation. It returns every violation found, in a
// deterministic order, so operators fix a broken configuration in one
// round trip rather than one error at a time.
//
// Ref existence ("every configured branch must exist") is deliberately
// not checked here: it requires repository state, which the engine never
// fetches itself.
func (g *Graph) Validate() []error {
	var errs []error
	report := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if g.Name == "" {
		report("metadata.name is required")
	}
	if len(g.Branches) == 0 {
		report("spec.branches must declare at least one branch")
	}

	for _, name := range sortedBranchNames(g) {
		b := g.Branches[name]
		switch b.Role {
		case v1.RoleNone, v1.RoleSource, v1.RoleTerminal:
		default:
			report("branch %q: unknown role %q (want %q, %q, or unset)", name, b.Role, v1.RoleSource, v1.RoleTerminal)
		}
		switch b.Drift {
		case v1.DriftForbidden, v1.DriftExpected:
		default:
			report("branch %q: unknown drift policy %q (want %q or %q)", name, b.Drift, v1.DriftForbidden, v1.DriftExpected)
		}
	}

	seen := make(map[[2]string]bool, len(g.Promotions))
	for i, p := range g.Promotions {
		if p.From == "" || p.To == "" {
			report("promotion edge %d: from and to are required", i)
			continue
		}
		if _, ok := g.Branches[p.From]; !ok {
			report("promotion edge %s -> %s: branch %q is not declared in spec.branches", p.From, p.To, p.From)
		}
		if _, ok := g.Branches[p.To]; !ok {
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

		switch p.Expectations.MergeMethod {
		case "", v1.MergeMethodMerge, v1.MergeMethodSquash, v1.MergeMethodRebase:
		default:
			report("promotion edge %s -> %s: unknown mergeMethod %q (want %q, %q, or %q)",
				p.From, p.To, p.Expectations.MergeMethod,
				v1.MergeMethodMerge, v1.MergeMethodSquash, v1.MergeMethodRebase)
		}

		if from, ok := g.Branches[p.From]; ok && from.Role == v1.RoleTerminal {
			report("branch %q has role %q but is the source of promotion edge %s -> %s", p.From, v1.RoleTerminal, p.From, p.To)
		}
		if to, ok := g.Branches[p.To]; ok && to.Role == v1.RoleSource {
			report("branch %q has role %q but is the destination of promotion edge %s -> %s", p.To, v1.RoleSource, p.From, p.To)
		}
	}

	if cycle := findCycle(g); cycle != nil {
		report("promotion graph contains a cycle: %s", formatCycle(cycle))
	}

	errs = append(errs, g.validateBackflow()...)
	return errs
}

func (g *Graph) validateBackflow() []error {
	if !g.Backflow.Enabled {
		return nil
	}
	var errs []error
	report := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	bf := g.Backflow
	if bf.Strategy != v1.BackflowStrategyCherryPick {
		report("backflow: unknown strategy %q (v1 supports only %q)", bf.Strategy, v1.BackflowStrategyCherryPick)
	}
	if bf.Target == "" {
		report("backflow: target is required")
	} else if target, ok := g.Branches[bf.Target]; !ok {
		report("backflow: target %q is not declared in spec.branches", bf.Target)
	} else if target.Role != v1.RoleSource {
		report("backflow: target %q must have role %q", bf.Target, v1.RoleSource)
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
		b, ok := g.Branches[src]
		if !ok {
			report("backflow: source %q is not declared in spec.branches", src)
			continue
		}
		if b.Drift == v1.DriftExpected {
			report("backflow: source %q declares drift %q; a backflow source's downstream-only content must be returned, not ignored", src, v1.DriftExpected)
		}
	}
	return errs
}

// findCycle returns the branches of one promotion cycle (first node
// repeated at the end), or nil when the graph is acyclic. Detection is
// deterministic: neighbors are visited in declaration order and roots in
// sorted order.
func findCycle(g *Graph) []string {
	next := make(map[string][]string, len(g.Branches))
	for _, p := range g.Promotions {
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

func formatCycle(cycle []string) string {
	out := ""
	for i, node := range cycle {
		if i > 0 {
			out += " -> "
		}
		out += node
	}
	return out
}

func sortedBranchNames(g *Graph) []string {
	names := make([]string, 0, len(g.Branches))
	for name := range g.Branches {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
