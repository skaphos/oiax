package engine

import (
	"fmt"
	"slices"
	"strings"

	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

// BuildPlan derives the actions required to converge the observed edge
// states toward the desired state: exactly one active managed promotion
// request per diverged edge, no duplicates, no stale leftovers.
//
// BuildPlan is pure: it makes no provider calls and produces an
// equivalent plan for identical inputs. Backflow planning is implemented:
// downstream-only commits on a backflow source are returned to the backflow
// target via ActionCreateBackflowRequest (planDownstream); downstream-only
// content on a branch that is not a configured backflow source is surfaced
// as ActionReportDivergence, unless the branch declares drift: expected, in
// which case it is silently ignored (see planDownstream).
func BuildPlan(g *Graph, edges []EdgeState) Plan {
	plan := Plan{
		PlanFormatVersion: PlanFormatVersion,
		Graph:             g.Name,
		// Actions starts as an empty (non-nil) slice so the frozen
		// planFormatVersion:1 JSON contract always serializes "actions" as
		// an array, even when the graph is fully in sync — never null. It is
		// preallocated to one action per edge (a typical lower bound) to avoid
		// reallocation on the common path; an empty graph still yields a
		// non-nil, zero-length slice that marshals to [].
		Actions: make([]Action, 0, len(edges)),
	}

	// Per-edge diagnostics: which rung settled each edge and the counts the
	// observability surfaces render, for every edge — an in-sync edge usually
	// produces no action at all (the exception is closing an obsolete
	// request), so without this its settling rung would be invisible.
	if len(edges) > 0 {
		plan.Edges = make([]EdgeSummary, 0, len(edges))
		for _, e := range edges {
			plan.Edges = append(plan.Edges, summarizeEdge(g, e))
		}
	}

	// A backflow source can have several incoming promotion edges; each yields
	// an EdgeState with the same To, and thus the same backflow (source, target)
	// pair and branch name. Emit exactly one backflow action per pair — two would
	// double-push the branch and open two identical requests. Apply returns the
	// UNION of the returnable content across every incoming edge (see
	// reconcile.backflowActionState), so accumulate that union here too: the
	// surviving action's Unpromoted/Reason must reflect the full set apply
	// cherry-picks, not just whichever edge was processed first.
	backflowIndex := make(map[[2]string]int)
	backflowSeen := make(map[[2]string]map[string]struct{})
	for _, e := range edges {
		plan.Actions = append(plan.Actions, planPromotion(e)...)
		for _, a := range planDownstream(g, e) {
			if a.Type == ActionCreateBackflowRequest {
				key := [2]string{a.From, a.To}
				if idx, ok := backflowIndex[key]; ok {
					// A later edge into the same source: fold its returnable SHAs
					// into the union and restate the surviving action's count and
					// reason so both track what apply actually returns.
					seen := backflowSeen[key]
					for _, cm := range e.ToReturn {
						seen[cm.SHA] = struct{}{}
					}
					plan.Actions[idx].Unpromoted = len(seen)
					plan.Actions[idx].Reason = fmt.Sprintf("%d downstream-only commits on %s to return to %s", len(seen), a.From, a.To)
					continue
				}
				seen := make(map[string]struct{}, len(e.ToReturn))
				for _, cm := range e.ToReturn {
					seen[cm.SHA] = struct{}{}
				}
				backflowSeen[key] = seen
				backflowIndex[key] = len(plan.Actions)
			}
			plan.Actions = append(plan.Actions, a)
		}
	}
	return plan
}

// summarizeEdge reduces one evaluated EdgeState to the per-edge diagnostic
// record the plan carries: the settling rung, sync status, and counts. The
// Excluded slice is shared, not copied — EdgeState is not mutated after
// evaluation.
//
// ToReturn and Excluded are published only when the destination is a
// configured backflow source, mirroring planDownstream's gate. EvaluateEdge
// computes EdgeState.ToReturn unconditionally, and for a non-source
// destination the exclusion inputs are never observed, so its ToReturn
// degenerates to all of DownstreamOnly — publishing that would claim pending
// backflow work on an edge nothing will ever backflow.
func summarizeEdge(g *Graph, e EdgeState) EdgeSummary {
	s := EdgeSummary{
		From:           e.From.Name,
		To:             e.To.Name,
		Equivalence:    e.Equivalence,
		InSync:         len(e.Unpromoted) == 0,
		Unpromoted:     len(e.Unpromoted),
		DownstreamOnly: len(e.DownstreamOnly),
	}
	if g.isBackflowSource(e.To.Name) {
		s.ToReturn = len(e.ToReturn)
		s.Excluded = e.Excluded
		// Publish the strategy tag and the wholesale returned set only for a
		// merge-strategy edge: "cherry-pick" is non-empty, so tagging every
		// edge would change existing plan JSON (a de-facto format break under
		// the frozen planFormatVersion 1).
		if g.Backflow.Strategy == v1.BackflowStrategyMerge {
			s.Strategy = g.Backflow.Strategy
			s.Returned = e.ToReturn
		}
	}
	return s
}

func planPromotion(e EdgeState) []Action {
	req := e.ManagedRequest

	if len(e.Unpromoted) == 0 {
		// Edge in sync. An open managed request now proposes nothing —
		// the edge synchronized out-of-band — and is obsolete.
		if req != nil {
			return []Action{{
				Type:    ActionCloseObsoleteRequest,
				From:    e.From.Name,
				To:      e.To.Name,
				Request: req,
				Reason:  "edge synchronized out-of-band; the open managed request proposes nothing",
			}}
		}
		return nil
	}

	if req == nil {
		return []Action{{
			Type:        ActionCreatePromotionRequest,
			From:        e.From.Name,
			To:          e.To.Name,
			Unpromoted:  len(e.Unpromoted),
			Equivalence: e.Equivalence,
			Reason:      fmt.Sprintf("%d unpromoted commits and no managed promotion request", len(e.Unpromoted)),
		}}
	}

	// The request head is the live source branch, so new source commits
	// are already part of the request; only the recorded sourceHead
	// (the promotion baseline) needs to follow the branch.
	if req.SourceHead != e.From.Head {
		return []Action{{
			Type:        ActionUpdateManagedRequest,
			From:        e.From.Name,
			To:          e.To.Name,
			Unpromoted:  len(e.Unpromoted),
			Equivalence: e.Equivalence,
			Request:     req,
			Reason:      "source branch advanced; record the new head as the promotion baseline",
		}}
	}
	return nil
}

func planDownstream(g *Graph, e EdgeState) []Action {
	if len(e.DownstreamOnly) == 0 {
		return nil
	}
	// Evaluation order per the design: backflow source first, then drift
	// policy, then report.
	if g.isBackflowSource(e.To.Name) {
		source, target := e.To.Name, g.Backflow.Target
		// A merge-strategy edge returns the downstream-only set wholesale, so
		// two conditions a cherry-pick tolerates are hard divergences here,
		// surfaced BEFORE any create-backflow action.
		if g.Backflow.Strategy == v1.BackflowStrategyMerge {
			return planMergeBackflow(e, source, target)
		}
		// The destination is a downstream backflow source: its
		// downstream-only commits are returned to the backflow target by
		// cherry-pick. When every commit is already returned (matched by
		// content, provenance, or an Oiax-Backflow: skip trailer) ToReturn is
		// empty and the edge has converged — emit nothing.
		if len(e.ToReturn) == 0 {
			return nil
		}
		return []Action{{
			Type:       ActionCreateBackflowRequest,
			From:       source,
			To:         target,
			Unpromoted: len(e.ToReturn),
			Branch:     BackflowBranchName(source, target, e.SourceHeadShort),
			Reason:     fmt.Sprintf("%d downstream-only commits on %s to return to %s", len(e.ToReturn), source, target),
		}}
	}
	// A non-backflow-source destination with expected drift is intentionally
	// ignored; otherwise its downstream content is a reported divergence.
	// DownstreamOnly is already content-filtered by observation (ADR-0002
	// Amendment 1), so anything still here is genuine drift, not merge or
	// patch-id residue.
	if b, ok := g.Branches[e.To.Name]; ok && b.Drift == v1.DriftExpected {
		return nil
	}
	return []Action{{
		Type:       ActionReportDivergence,
		From:       e.From.Name,
		To:         e.To.Name,
		Unpromoted: len(e.DownstreamOnly),
		Reason:     fmt.Sprintf("%s has %d commits not represented in %s", e.To.Name, len(e.DownstreamOnly), e.From.Name),
	}}
}

// planMergeBackflow plans the downstream return for a merge-strategy backflow
// edge. A merge returns the downstream-only set wholesale (all-or-nothing,
// ADR-0006), so two conditions a cherry-pick tolerates become hard
// divergences, surfaced BEFORE any create-backflow action:
//
//   - an Oiax-Backflow: skip trailer inside the returnable range — a merge
//     cannot honor per-commit exclusion (ADR-0006 Amendment 2); and
//   - a target branch that forbids merge commits, per the live forge signal —
//     the return merge cannot land (ADR-0006 Amendment 1).
//
// Both map to ActionReportDivergence (exit 3, same plumbing as any other
// divergence). Otherwise the whole set is merged back, with Strategy recorded
// so the plan JSON distinguishes the two mechanisms. This function is pure: it
// decides only from the injected EdgeState fields, never from git or a forge.
func planMergeBackflow(e EdgeState, source, target string) []Action {
	// Amendment 2: a skip trailer in range cannot be honored by a merge. Its
	// directive Reason names the offending short SHAs and the two ways out.
	if len(e.SkippedInRange) > 0 {
		return []Action{{
			Type:       ActionReportDivergence,
			From:       source,
			To:         target,
			Unpromoted: len(e.SkippedInRange),
			Reason: fmt.Sprintf(
				"backflow %s->%s: strategy merge cannot honor the Oiax-Backflow: skip trailer on %d in-range commit(s) (%s); unmark those commits or set strategy: cherry-pick on this edge",
				source, target, len(e.SkippedInRange), shortSHAList(e.SkippedInRange)),
		}}
	}
	// Amendment 1: the live forge read says the target forbids merge commits.
	if e.TargetCanMergeCommit != nil && !*e.TargetCanMergeCommit {
		return []Action{{
			Type: ActionReportDivergence,
			From: source,
			To:   target,
			Reason: fmt.Sprintf(
				"backflow %s->%s: strategy merge requires merge commits on %s, but the target branch forbids them (merge commits disabled or linear history required); allow merge commits on %s or set strategy: cherry-pick on this edge",
				source, target, target, target),
		}}
	}
	// Converged: nothing left to return (ancestry settles a merge edge once the
	// return lands).
	if len(e.ToReturn) == 0 {
		return nil
	}
	return []Action{{
		Type:       ActionCreateBackflowRequest,
		From:       source,
		To:         target,
		Unpromoted: len(e.ToReturn),
		Strategy:   v1.BackflowStrategyMerge,
		Branch:     BackflowBranchName(source, target, e.SourceHeadShort),
		Reason:     fmt.Sprintf("%d downstream-only commits on %s to return to %s", len(e.ToReturn), source, target),
	}}
}

// shortSHAList renders a comma-separated list of 7-character short SHAs for a
// human-readable divergence Reason.
func shortSHAList(cs []Commit) string {
	parts := make([]string, len(cs))
	for i, c := range cs {
		sha := c.SHA
		if len(sha) > 7 {
			sha = sha[:7]
		}
		parts[i] = sha
	}
	return strings.Join(parts, ", ")
}

// BackflowBranchName builds the deterministic branch Oiax pushes a backflow
// request to: oiax/backflow/<source>-to-<target>/<shortSHA>, where shortSHA is
// the short SHA of the backflow source (downstream) head. Determinism is the
// concurrency strategy — the same source head yields the same branch (an
// idempotent force-push), while a new hotfix advances the head to a new branch
// that supersedes the prior request.
func BackflowBranchName(source, target, shortSHA string) string {
	return fmt.Sprintf("oiax/backflow/%s-to-%s/%s", source, target, shortSHA)
}

func (g *Graph) isBackflowSource(branch string) bool {
	return g.Backflow.Enabled && slices.Contains(g.Backflow.Sources, branch)
}
