package engine

import (
	"fmt"
	"slices"

	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

// BuildPlan derives the actions required to converge the observed edge
// states toward the desired state: exactly one active managed promotion
// request per diverged edge, no duplicates, no stale leftovers.
//
// BuildPlan is pure: it makes no provider calls and produces an
// equivalent plan for identical inputs. Backflow planning (returning
// downstream-only commits to the backflow target) is v0.2 scope; until
// then, downstream-only content on a backflow source is reported.
func BuildPlan(g *Graph, edges []EdgeState) Plan {
	plan := Plan{
		PlanFormatVersion: PlanFormatVersion,
		Graph:             g.Name,
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
		// The destination is a downstream backflow source: its
		// downstream-only commits are returned to the backflow target by
		// cherry-pick. When every commit is already returned (matched by
		// content, provenance, or an Oiax-Backflow: skip trailer) ToReturn is
		// empty and the edge has converged — emit nothing.
		if len(e.ToReturn) == 0 {
			return nil
		}
		source, target := e.To.Name, g.Backflow.Target
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
