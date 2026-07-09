package engine

import (
	"fmt"
	"slices"

	"github.com/skaphos/oiax/pkg/api/v1alpha1"
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

	for _, e := range edges {
		plan.Actions = append(plan.Actions, planPromotion(e)...)
		plan.Actions = append(plan.Actions, planDownstream(g, e)...)
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
	// policy, then report. Backflow request construction is v0.2 scope;
	// a backflow source's downstream content is reported until then.
	if b, ok := g.Branches[e.To.Name]; ok && b.Drift == v1alpha1.DriftExpected && !g.isBackflowSource(e.To.Name) {
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

func (g *Graph) isBackflowSource(branch string) bool {
	return g.Backflow.Enabled && slices.Contains(g.Backflow.Sources, branch)
}
