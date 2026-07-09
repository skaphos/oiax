// Package reconcile is the coordination layer: it is the only place where
// git observations and forge requests become engine.EdgeObservations, and
// the only place engine actions become forge calls. The engine stays pure
// (it never imports git or forge); reconcile does the I/O and hands the
// engine already-gathered observation data.
//
// The flow is observe → plan → apply:
//
//   - Plan observes every promotion edge (branch heads, reachability,
//     patch identity, head trees, managed requests, merged baselines),
//     evaluates each edge through the engine's equivalence ladder, and
//     builds the plan. It never mutates.
//   - Apply executes a plan's actions against the forge (create/update/
//     close), reporting divergence without touching it. It never merges,
//     approves, force-pushes long-lived branches, or edits unmanaged
//     requests.
package reconcile

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/git"
)

// Coordinator wires the git layer and a forge provider to the engine. It
// holds no mutable state of its own; Plan and Apply take a context and
// operate against the injected dependencies.
type Coordinator struct {
	// Git observes local branch state.
	Git *git.Runner
	// Forge observes and mutates managed change requests.
	Forge forge.Forge
	// Graph is the loaded, validated promotion graph.
	Graph *engine.Graph
	// Log receives structured logs; warnings/errors also surface as GitHub
	// Actions annotations when its handler was built for Actions. A nil Log
	// discards output.
	Log *slog.Logger
}

// Result carries what Apply did, for exit-code and summary decisions.
type Result struct {
	// Applied counts the create/update/close actions performed.
	Applied int
	// Divergence is true when the plan reported any divergence requiring
	// human attention (drives reconcile's exit code 3).
	Divergence bool
}

// Plan observes state for every promotion edge, in the graph's declaration
// order so the plan is deterministic, and builds the plan. It makes no
// mutation. Managed requests are listed once (open and merged) and matched
// to each edge, so discovery cost is independent of edge count.
func (c *Coordinator) Plan(ctx context.Context) (engine.Plan, error) {
	filter := forge.RequestFilter{Graph: c.Graph.Name, Type: engine.RequestTypePromotion}

	open, err := c.Forge.ListManagedRequests(ctx, filter)
	if err != nil {
		return engine.Plan{}, fmt.Errorf("list open managed requests: %w", err)
	}
	mergedFilter := filter
	mergedFilter.State = forge.RequestStateMerged
	merged, err := c.Forge.ListManagedRequests(ctx, mergedFilter)
	if err != nil {
		return engine.Plan{}, fmt.Errorf("list merged managed requests: %w", err)
	}

	edges := make([]engine.EdgeState, 0, len(c.Graph.Promotions))
	for _, p := range c.Graph.Promotions {
		obs, err := c.observe(ctx, p.From, p.To, open, merged)
		if err != nil {
			return engine.Plan{}, err
		}
		edges = append(edges, engine.EvaluateEdge(obs))
	}
	return engine.BuildPlan(c.Graph, edges), nil
}

// observe assembles the pure EdgeObservation the engine consumes for one
// edge. Every git/forge error is wrapped with the edge context.
func (c *Coordinator) observe(ctx context.Context, from, to string, open, merged []engine.ChangeRequest) (engine.EdgeObservation, error) {
	wrap := func(err error) error { return fmt.Errorf("edge %s->%s: %w", from, to, err) }

	fromHead, err := c.Git.Head(ctx, from)
	if err != nil {
		return engine.EdgeObservation{}, wrap(err)
	}
	toHead, err := c.Git.Head(ctx, to)
	if err != nil {
		return engine.EdgeObservation{}, wrap(err)
	}
	// Candidates are source content not reachable in the destination
	// (to..from); downstream is destination content absent from the source
	// (from..to).
	candidates, err := c.Git.UniqueCommits(ctx, to, from)
	if err != nil {
		return engine.EdgeObservation{}, wrap(err)
	}
	downstream, err := c.Git.UniqueCommits(ctx, from, to)
	if err != nil {
		return engine.EdgeObservation{}, wrap(err)
	}
	mb, err := c.Git.MergeBase(ctx, from, to)
	if err != nil {
		return engine.EdgeObservation{}, wrap(err)
	}
	candPatch, err := c.Git.PatchIDs(ctx, to, from)
	if err != nil {
		return engine.EdgeObservation{}, wrap(err)
	}
	// The destination patch-id set is what the patch-identity rung tests
	// candidates against. Skip it when the branches share no ancestor.
	destPatch := make(map[string]struct{})
	if mb != "" {
		dp, err := c.Git.PatchIDs(ctx, mb, to)
		if err != nil {
			return engine.EdgeObservation{}, wrap(err)
		}
		for _, pid := range dp {
			destPatch[pid] = struct{}{}
		}
	}
	fromTree, err := c.Git.TreeSHA(ctx, from)
	if err != nil {
		return engine.EdgeObservation{}, wrap(err)
	}
	toTree, err := c.Git.TreeSHA(ctx, to)
	if err != nil {
		return engine.EdgeObservation{}, wrap(err)
	}

	obs := engine.EdgeObservation{
		From:                engine.BranchState{Name: from, Head: fromHead},
		To:                  engine.BranchState{Name: to, Head: toHead},
		Candidates:          toEngineCommits(candidates),
		DownstreamOnly:      toEngineCommits(downstream),
		CandidatePatchIDs:   candPatch,
		DestinationPatchIDs: destPatch,
		TreesEqual:          fromTree == toTree,
		ManagedRequest:      matchRequest(open, from, to),
	}

	// The baseline rung uses the recorded sourceHead of the newest merged
	// managed request for this edge: any candidate reachable from it was
	// promoted even if the merge rewrote SHAs.
	if mr := matchRequest(merged, from, to); mr != nil && mr.SourceHead != "" {
		promoted := make(map[string]struct{})
		for _, cm := range candidates {
			anc, err := c.Git.IsAncestor(ctx, cm.SHA, mr.SourceHead)
			if err != nil {
				return engine.EdgeObservation{}, wrap(err)
			}
			if anc {
				promoted[cm.SHA] = struct{}{}
			}
		}
		obs.PromotedByBaseline = promoted
	}

	return obs, nil
}

// Apply executes a plan's actions against the forge, in plan order, and
// reports the outcome. It is idempotent (the next reconcile converges) and
// aborts on the first forge error with a wrapped error — there is no
// rollback.
func (c *Coordinator) Apply(ctx context.Context, plan engine.Plan) (Result, error) {
	var res Result
	for _, a := range plan.Actions {
		switch a.Type {
		case engine.ActionCreatePromotionRequest:
			// The action carries no live head; re-resolve the current source
			// head so the created request records an up-to-date baseline.
			head, err := c.Git.Head(ctx, a.From)
			if err != nil {
				return res, fmt.Errorf("apply create %s->%s: %w", a.From, a.To, err)
			}
			if _, err := c.Forge.CreateRequest(ctx, forge.CreateRequest{
				Graph:      c.Graph.Name,
				Type:       engine.RequestTypePromotion,
				Source:     a.From,
				Target:     a.To,
				SourceHead: head,
				Title:      fmt.Sprintf("oiax: promote %s to %s", a.From, a.To),
				Body:       promotionBody(a),
			}); err != nil {
				return res, fmt.Errorf("apply create %s->%s: %w", a.From, a.To, err)
			}
			res.Applied++

		case engine.ActionUpdateManagedRequest:
			if a.Request == nil {
				return res, fmt.Errorf("apply update %s->%s: action carries no request", a.From, a.To)
			}
			// The action carries only the stale recorded sourceHead; re-resolve
			// the live source head so the baseline follows the branch.
			head, err := c.Git.Head(ctx, a.From)
			if err != nil {
				return res, fmt.Errorf("apply update %s->%s: %w", a.From, a.To, err)
			}
			if err := c.Forge.UpdateRequest(ctx, forge.UpdateRequest{
				ID:         forge.RequestID(a.Request.ID),
				SourceHead: head,
			}); err != nil {
				return res, fmt.Errorf("apply update %s->%s: %w", a.From, a.To, err)
			}
			res.Applied++

		case engine.ActionCloseObsoleteRequest:
			if a.Request == nil {
				return res, fmt.Errorf("apply close %s->%s: action carries no request", a.From, a.To)
			}
			if err := c.Forge.CloseRequest(ctx, forge.RequestID(a.Request.ID), forge.Reason{Summary: a.Reason}); err != nil {
				return res, fmt.Errorf("apply close %s->%s: %w", a.From, a.To, err)
			}
			res.Applied++

		case engine.ActionReportDivergence:
			res.Divergence = true
			// A descriptive message so the GitHub Actions annotation (routed
			// through the logger's handler) carries the edge and reason.
			c.log().Warn(fmt.Sprintf("reported divergence on %s -> %s: %s", a.From, a.To, a.Reason))

		case engine.ActionCreateBackflowRequest:
			return res, fmt.Errorf("apply %s->%s: backflow requests are v0.2 scope and must not appear in a v0.1 plan", a.From, a.To)

		case engine.ActionNoOp:
			// Nothing to do.

		default:
			return res, fmt.Errorf("apply: unknown action type %q", a.Type)
		}
	}
	return res, nil
}

// log returns a usable logger even when none was injected, so Apply never
// panics on a nil Log.
func (c *Coordinator) log() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// matchRequest returns a copy of the first request whose source and target
// match the edge, or nil when none does. The forge guarantees at most one
// open managed request per edge; for merged requests the first match is the
// newest (GitHub returns closed pulls newest-first).
func matchRequest(reqs []engine.ChangeRequest, from, to string) *engine.ChangeRequest {
	for i := range reqs {
		if reqs[i].Source == from && reqs[i].Target == to {
			r := reqs[i]
			return &r
		}
	}
	return nil
}

// toEngineCommits maps git-local commits to the engine's commit type,
// preserving order and the nil/empty distinction.
func toEngineCommits(cs []git.Commit) []engine.Commit {
	if cs == nil {
		return nil
	}
	out := make([]engine.Commit, len(cs))
	for i, c := range cs {
		out[i] = engine.Commit{SHA: c.SHA, Subject: c.Subject}
	}
	return out
}

// promotionBody is the human body of a created promotion request; the
// provider appends the machine-readable marker after it.
func promotionBody(a engine.Action) string {
	return fmt.Sprintf(
		"Oiax opened this request to promote %d commit(s) from `%s` into `%s`.\n\n"+
			"This request is managed by Oiax. Do not edit the metadata block below.",
		a.Unpromoted, a.From, a.To)
}
