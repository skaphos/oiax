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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/git"
	"github.com/skaphos/oiax/internal/tmpl"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

// cherryPickedFromRE captures the source object id recorded by
// 'git cherry-pick -x' in a "(cherry picked from commit <sha>)" provenance
// line. It anchors a STANDALONE line (git's -x output is always a bare line),
// so a downstream commit already returned to the target is recognized by
// identity even when its patch-id was rewritten during conflict resolution,
// while the same object id merely embedded in a prose sentence is not mistaken
// for provenance.
var cherryPickedFromRE = regexp.MustCompile(`(?m)^\(cherry picked from commit ([0-9a-f]{7,64})\)$`)

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
	// RefuseShallow makes a shallow clone a hard error instead of a warning.
	// A shallow clone (actions/checkout's default fetch-depth: 1) silently
	// disables the patch-identity and baseline rungs of the equivalence
	// ladder, so already-promoted content looks unpromoted and Oiax opens
	// spurious promotion requests. Locally the operator sees the warning and
	// can act; under CI the run is unattended and a wrong PR lands before
	// anyone reads the log, so the CLI sets this under a detected CI host
	// (mirroring the pinned-config-ref refusal in effectiveConfigRef). The
	// recovery is the same either way: fetch full history (fetch-depth: 0).
	RefuseShallow bool
	// Templates renders the human-facing text of created requests and
	// conflict artifacts (SKA-54). A nil Templates uses the built-in
	// defaults, mirroring Log's nil-safety.
	Templates *tmpl.Set
}

// Result carries what Apply did, for exit-code and summary decisions.
type Result struct {
	// Applied counts the create/update/close actions performed.
	Applied int
	// Superseded counts the stale managed backflow requests closed during
	// apply — superseded by a newer head or orphaned by a history rewrite.
	Superseded int
	// Divergence is true when the plan reported any divergence requiring
	// human attention (drives reconcile's exit code 3).
	Divergence bool
	// Conflicts counts the durable backflow-conflict artifacts CREATED this
	// run (SKA-601) — an apply-summary count only, never surfaced in the plan
	// JSON. Adopt/advance/consolidate/close do not increment it.
	Conflicts int
}

// Plan observes state for every promotion edge, in the graph's declaration
// order so the plan is deterministic, and builds the plan. It makes no
// mutation. Managed requests are listed once (open and merged) and matched
// to each edge, so discovery cost is independent of edge count.
func (c *Coordinator) Plan(ctx context.Context) (engine.Plan, error) {
	c.warnMergeMethodMismatch(ctx)
	c.warnSourceBranchDeletion(ctx)

	// A shallow clone (actions/checkout's default fetch-depth: 1) has no merge
	// base for fork points that predate the fetch depth, so the patch-identity
	// and baseline rungs silently switch off and already-promoted content looks
	// unpromoted — spurious promotion PRs. Surface a clear warning rather than
	// proceed with silently degraded equivalence detection.
	if shallow, err := c.Git.IsShallowRepository(ctx); err != nil {
		return engine.Plan{}, fmt.Errorf("detect shallow repository: %w", err)
	} else if shallow {
		if c.RefuseShallow {
			return engine.Plan{}, errors.New("shallow clone detected: equivalence detection is degraded " +
				"(merge-base, patch-identity and baseline rungs are unreliable) and would produce spurious " +
				"promotion requests; refusing under CI. Fetch full history: set fetch-depth: 0 on " +
				"actions/checkout (fetchDepth: 0 on Azure Pipelines)")
		}
		c.log().Warn("shallow clone detected: equivalence detection is degraded " +
			"(merge-base, patch-identity and baseline rungs are unreliable), which can " +
			"produce spurious promotion requests; set fetch-depth: 0 on actions/checkout " +
			"for correct results")
	}

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
	candidates := 0
	for _, p := range c.Graph.Promotions {
		obs, err := c.observe(ctx, p.From, p.To, open, merged)
		if err != nil {
			return engine.Plan{}, err
		}
		candidates += len(obs.Candidates)
		edges = append(edges, engine.EvaluateEdge(obs))
	}
	plan := engine.BuildPlan(c.Graph, edges)
	c.logPlanCounts(plan, edges, candidates)
	return plan, nil
}

// logPlanCounts emits the per-run observability counts (SKA-600): how many
// edges each ladder rung settled, how many promotion candidates were
// inspected, and how the backflow exclusion ladder classified downstream
// commits. One structured record per run, so a log pipeline can track
// convergence without parsing plan JSON.
//
// The backflow tallies are deduplicated by commit SHA across edges, not
// summed over the per-edge summaries: a backflow source with several
// incoming promotion edges carries the SAME downstream commit in each
// edge's view, and apply returns the union (see backflowActionState) — so
// the run-level count must be the union too, or one hotfix would be
// reported N times.
func (c *Coordinator) logPlanCounts(plan engine.Plan, edges []engine.EdgeState, candidates int) {
	inSync := 0
	settledBy := make(map[engine.Equivalence]int)
	for _, e := range plan.Edges {
		if e.InSync {
			inSync++
		}
		settledBy[e.Equivalence]++
	}
	seenReturn := make(map[string]struct{})
	excludedBy := make(map[engine.BackflowExclusionReason]int)
	seenExcluded := make(map[string]struct{})
	for _, e := range edges {
		if !c.isBackflowSource(e.To.Name) {
			continue
		}
		for _, cm := range e.ToReturn {
			seenReturn[cm.SHA] = struct{}{}
		}
		for _, x := range e.Excluded {
			if _, ok := seenExcluded[x.SHA]; ok {
				continue
			}
			seenExcluded[x.SHA] = struct{}{}
			excludedBy[x.Reason]++
		}
	}
	toReturn := len(seenReturn)
	c.log().Info("plan built",
		"graph", plan.Graph,
		"edges", len(plan.Edges),
		"inSync", inSync,
		"actions", len(plan.Actions),
		"candidates", candidates,
		slog.Group("settledBy",
			"reachability", settledBy[engine.EquivalenceReachability],
			"patchIdentity", settledBy[engine.EquivalencePatchIdentity],
			"headTree", settledBy[engine.EquivalenceHeadTree],
			"baseline", settledBy[engine.EquivalenceBaseline],
		),
		slog.Group("backflow",
			"toReturn", toReturn,
			"excludedSkip", excludedBy[engine.BackflowExcludedSkip],
			"excludedProvenance", excludedBy[engine.BackflowExcludedProvenance],
			"excludedPatchID", excludedBy[engine.BackflowExcludedPatchID],
		),
	)
}

// observe assembles the pure EdgeObservation the engine consumes for one
// edge. Every git/forge error is wrapped with the edge context.
func (c *Coordinator) observe(ctx context.Context, from, to string, open, merged []engine.ChangeRequest) (engine.EdgeObservation, error) {
	// A branch declared in the promotion graph but present on neither the local
	// heads nor the origin-tracking refs is rarely a typo — the config was
	// validated, and the graph reconciled on earlier runs. Overwhelmingly it
	// means the branch was deleted, and the usual way a LONG-LIVED graph branch
	// gets deleted is a forge that auto-deletes a merged request's source
	// branch (see warnSourceBranchDeletion). Say so, rather than leaving an
	// operator to work backwards from a bare "not found".
	wrap := func(err error) error {
		if errors.Is(err, git.ErrBranchNotFound) {
			return fmt.Errorf("edge %s->%s: %w; it is declared in the promotion graph, so it most "+
				"likely was deleted when a promotion request merged — restore the branch, or remove "+
				"it from the graph if the topology changed", from, to, err)
		}
		return fmt.Errorf("edge %s->%s: %w", from, to, err)
	}

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

	// Backflow inputs. When the destination is a backflow source, its
	// downstream-only commits are returned to the backflow target; gather the
	// signals the engine's ToReturn filter consumes. Direction: `to` is the
	// downstream source (e.g. main), the backflow target (e.g. development) is
	// authoritative. The two strategies gather different inputs: cherry-pick
	// walks the per-commit identity ladder, merge returns the range wholesale by
	// ancestry (ADR-0006).
	if c.isBackflowSource(to) {
		target := c.Graph.Backflow.Target
		merge := c.Graph.Backflow.Strategy == v1.BackflowStrategyMerge

		// cherry-pick draws its candidates from the edge-local from..to range, so
		// it runs only when that range is non-empty. merge is target-relative
		// (target..source, computed inside observeMergeBackflow) and MUST run even
		// when this edge's from..to is empty — otherwise a source reached only by
		// an edge whose from..to is empty (e.g. an intermediate branch that has
		// already caught up) would skip the fence, the skip scan, and the
		// wholesale return while target..source is still non-empty.
		if merge || len(downstream) > 0 {
			var obsErr error
			if merge {
				obsErr = c.observeMergeBackflow(ctx, to, target, &obs)
			} else {
				obsErr = c.observeCherryPickBackflow(ctx, from, to, target, &obs)
			}
			if obsErr != nil {
				return engine.EdgeObservation{}, wrap(obsErr)
			}

			// The trailing segment of the deterministic backflow branch name is
			// the short SHA of the downstream (source) head.
			short, err := c.Git.ShortSHA(ctx, to)
			if err != nil {
				return engine.EdgeObservation{}, wrap(err)
			}
			obs.SourceHeadShort = short
		}
	} else if len(downstream) > 0 {
		// The destination is NOT a backflow source, so its downstream-only
		// residue is headed for ActionReportDivergence (or the drift: expected
		// ignore). The raw from..to range includes content already represented
		// upstream — promotion merge commits, diffs since returned by other
		// means — and reporting it raw is exactly the ancestry-only model
		// ADR-0002 rejects (Amendment 1). Filter to genuinely-unrepresented
		// content before evaluation, mirroring the cherry-pick returnable
		// restriction above.
		if err := c.observeDownstreamReport(ctx, from, to, mb, &obs); err != nil {
			return engine.EdgeObservation{}, wrap(err)
		}
	}

	return obs, nil
}

// observeDownstreamReport filters the raw downstream-only range (from..to) of
// a non-backflow-source destination down to the content genuinely
// unrepresented on the upstream source, per ADR-0002 Amendment 1: the
// divergence report obeys the same content posture as promotion detection.
// Three rungs, mirroring the backflow returnable filter but pointed upstream:
//
//   - a diff-carrying commit whose stable patch-id already appears on the
//     source since the merge base is represented by content — cleared;
//   - an empty non-merge commit carries no content — cleared;
//   - a merge commit is cleared only when mechanically re-merging its parents
//     reproduces its exact tree (benign residue of a merge-commit promotion
//     process); an evil merge — a tree the mechanical merge does not produce,
//     carrying conflict-resolution or strategy-option edits — is kept.
//
// Whatever survives is genuine drift and is reported. Errors are returned
// unwrapped; the caller adds the edge context.
func (c *Coordinator) observeDownstreamReport(ctx context.Context, from, to, mb string, obs *engine.EdgeObservation) error {
	// Patch-ids of the downstream range (from..to), keyed by commit SHA. As in
	// the backflow filter, merge commits and empty commits contribute no entry.
	dpid, err := c.Git.PatchIDs(ctx, from, to)
	if err != nil {
		return err
	}
	// Patch-ids present on the source since divergence — the upstream mirror
	// of the backflow filter's ReturnedPatchIDs. Skipped when the branches
	// share no common ancestor, matching the destination-set guard in observe.
	sourceIDs := make(map[string]struct{})
	if mb != "" {
		sp, err := c.Git.PatchIDs(ctx, mb, from)
		if err != nil {
			return err
		}
		for _, pid := range sp {
			sourceIDs[pid] = struct{}{}
		}
	}
	merges, err := c.Git.MergeCommitSHAs(ctx, from, to)
	if err != nil {
		return err
	}
	drifted := make([]engine.Commit, 0, len(obs.DownstreamOnly))
	for _, cm := range obs.DownstreamOnly {
		if pid, ok := dpid[cm.SHA]; ok {
			if _, represented := sourceIDs[pid]; represented {
				continue
			}
			drifted = append(drifted, cm)
			continue
		}
		if _, isMerge := merges[cm.SHA]; !isMerge {
			// No patch-id and not a merge: an empty commit, no content.
			continue
		}
		benign, err := c.Git.MergeReproducible(ctx, cm.SHA)
		if err != nil {
			return err
		}
		if !benign {
			drifted = append(drifted, cm)
		}
	}
	if cleared := len(obs.DownstreamOnly) - len(drifted); cleared > 0 {
		c.log().Debug("downstream residue cleared by content",
			"from", from, "to", to,
			"downstreamOnly", len(obs.DownstreamOnly), "cleared", cleared, "drifted", len(drifted))
	}
	if len(drifted) == 0 {
		drifted = nil
	}
	obs.DownstreamOnly = drifted
	return nil
}

// observeCherryPickBackflow gathers the identity-ladder inputs a cherry-pick
// backflow edge needs and writes them into obs: the returnable (diff-carrying)
// subset of the downstream range, the patch-ids already present on the target,
// and the skip/provenance identity sets. Errors are returned unwrapped; the
// caller adds the edge context. (Extracted from observe so the merge strategy,
// which returns the range wholesale by ancestry, takes a separate path.)
func (c *Coordinator) observeCherryPickBackflow(ctx context.Context, from, to, target string, obs *engine.EdgeObservation) error {
	// Patch-ids of the downstream range (from..to), keyed by commit SHA: the
	// content fingerprint of each downstream-only commit. PatchIDs omits merge
	// commits and empty commits (neither has a diff).
	dpid, err := c.Git.PatchIDs(ctx, from, to)
	if err != nil {
		return err
	}
	obs.DownstreamPatchIDs = dpid

	// Restrict the returnable set to commits that carry a diff. The raw
	// downstream range includes merge commits (from an ordinary promotion
	// merged into the source, the common case) and any empty commits, and
	// cherry-pick can return neither: a merge has no mainline (`git
	// cherry-pick` of it fails outright) and an empty commit no content.
	// Keeping only commits with a patch-id drops both, so an ordinary merge
	// on the source cannot become a permanent false divergence that blocks
	// genuine hotfixes batched behind it.
	returnable := make([]engine.Commit, 0, len(obs.DownstreamOnly))
	for _, cm := range obs.DownstreamOnly {
		if _, ok := dpid[cm.SHA]; ok {
			returnable = append(returnable, cm)
		}
	}
	obs.DownstreamOnly = returnable

	// Patch-ids already present on the backflow target since it diverged
	// from the source: a downstream commit whose diff is here has already
	// been returned by content.
	returned := make(map[string]struct{})
	tmb, err := c.Git.MergeBase(ctx, to, target)
	if err != nil {
		return err
	}
	if tmb != "" {
		rp, err := c.Git.PatchIDs(ctx, tmb, target)
		if err != nil {
			return err
		}
		for _, pid := range rp {
			returned[pid] = struct{}{}
		}
	}
	obs.ReturnedPatchIDs = returned

	// SHAs resolved as already returned (or withheld) by identity rather
	// than content: any downstream commit carrying the O6 skip trailer,
	// and the source SHA named in a target commit's cherry-pick -x
	// provenance trailer. Kept as two sets so the plan's per-commit
	// exclusion diagnostics can name which rung excluded each commit.
	skips, provenance, err := c.backflowAlreadyReturned(ctx, from, to, target, tmb)
	if err != nil {
		return err
	}
	obs.SkippedByTrailer = skips
	obs.ReturnedByProvenance = provenance
	return nil
}

// observeMergeBackflow gathers the inputs a merge-strategy backflow edge needs
// and writes them into obs. Unlike cherry-pick, a merge returns the downstream
// set WHOLESALE onto the target, so identity is by ancestry alone (ADR-0006):
// the returnable set is the target-relative range target..source, and once the
// return merges the source becomes an ancestor of the target and that range
// empties — no patch-id or provenance rung runs. Errors are returned unwrapped;
// the caller adds the edge context.
//
// It reads two live signals the pure engine turns into exit-3 divergences:
//
//   - Amendment 1 (fence): the TARGET branch's live merge-commit capability,
//     read every plan and NOT swallowed on error — a fetch failure is an
//     operational error (exit 1), while a successful read that forbids merge
//     commits becomes the exit-3 divergence via TargetCanMergeCommit;
//   - Amendment 2 (skip-in-range): any Oiax-Backflow: skip trailer inside the
//     returnable range, which a wholesale merge cannot honor, routed to
//     SkippedInRange — deliberately NOT SkippedByTrailer, which
//     engine.backflowToReturn would use to silently exclude the commit.
//
// It leaves DownstreamPatchIDs/ReturnedPatchIDs/SkippedByTrailer/
// ReturnedByProvenance nil so engine.backflowToReturn returns the whole set.
func (c *Coordinator) observeMergeBackflow(ctx context.Context, to, target string, obs *engine.EdgeObservation) error {
	// The returnable set is the target-relative range (commits on the source
	// not yet on the backflow target), newest first. Using target..source — not
	// the promotion edge's from..to — is what lets ancestry settle the edge:
	// once the wholesale return merges, the source head is an ancestor of the
	// target and this range is empty, so the next plan proposes nothing.
	downstream, err := c.Git.UniqueCommits(ctx, target, to)
	if err != nil {
		return err
	}
	obs.DownstreamOnly = toEngineCommits(downstream)
	if len(downstream) == 0 {
		// Converged by ancestry: the source head is already reachable from the
		// target, so there is nothing to return. Skip the live fence read and the
		// skip scan — both are moot with nothing to merge, and reading the fence
		// here would let a squash-only target report a spurious divergence on a
		// settled edge (planMergeBackflow evaluates the fence only when
		// TargetCanMergeCommit is non-nil, which this early return leaves unset).
		return nil
	}

	// Amendment 1 (live merge-commit fence). Read the target branch's actual
	// permitted merge methods every plan. A FETCH error propagates loudly (an
	// operational failure) rather than being swallowed to "not allowed"; a
	// successful read that forbids merge commits becomes the exit-3 divergence
	// the pure engine emits from TargetCanMergeCommit.
	mm, err := c.Forge.TargetMergeMethods(ctx, target)
	if err != nil {
		return fmt.Errorf("read target merge methods for %s: %w", target, err)
	}
	can := mm.MergeCommitAllowed()
	obs.TargetCanMergeCommit = &can

	// Amendment 2 (skip-in-range hard error). A wholesale merge cannot honor a
	// per-commit Oiax-Backflow: skip trailer, so any skip inside the returnable
	// range is a hard error, not an exclusion. Read the trailers over the same
	// target-relative range and record the matches in SkippedInRange (newest
	// first, nil when none).
	targetRef, err := c.Git.ResolveRev(ctx, target)
	if err != nil {
		return err
	}
	toRef, err := c.Git.ResolveRev(ctx, to)
	if err != nil {
		return err
	}
	skips, err := c.backflowSkipTrailers(ctx, targetRef+".."+toRef)
	if err != nil {
		return err
	}
	var inRange []engine.Commit
	for _, cm := range obs.DownstreamOnly {
		if _, ok := skips[cm.SHA]; ok {
			inRange = append(inRange, cm)
		}
	}
	obs.SkippedInRange = inRange
	return nil
}

// isBackflowSource reports whether a branch is a configured backflow source
// (a downstream branch whose downstream-only commits are returned to the
// backflow target). It mirrors the engine's unexported predicate; reconcile
// cannot call across the package boundary.
func (c *Coordinator) isBackflowSource(branch string) bool {
	return c.Graph.Backflow.Enabled && slices.Contains(c.Graph.Backflow.Sources, branch)
}

// backflowAlreadyReturned resolves, by identity, which downstream-only SHAs
// need no backflow, keeping the two identity signals separate so per-commit
// exclusion diagnostics can name the rung:
//
//   - skips: downstream commits carrying the O6 'Oiax-Backflow: skip' trailer,
//     intentionally not backflowed;
//   - provenance: SHAs a target commit's 'cherry picked from commit <sha>'
//     provenance names — already returned.
//
// It reads commit message bodies, which the git.Runner does not expose, so it
// shells out directly following the same no-shell posture: the range endpoints
// reach git as operands after --end-of-options, never as a shell string.
func (c *Coordinator) backflowAlreadyReturned(ctx context.Context, from, to, target, targetMergeBase string) (skips, provenance map[string]struct{}, err error) {
	// Resolve each branch endpoint to the ref that actually holds it. Under
	// actions/checkout only the triggering branch is a local head; every other
	// branch in a promotion graph exists solely as an origin-tracking ref. These
	// ranges are read by shelling out to git directly (the Runner does not expose
	// commit bodies/trailers), so — unlike the rest of the git layer, which routes
	// endpoints through validRev — they must resolve names themselves rather than
	// hard-coding refs/heads/<name>, which failed on every non-triggering branch.
	fromRef, err := c.Git.ResolveRev(ctx, from)
	if err != nil {
		return nil, nil, err
	}
	toRef, err := c.Git.ResolveRev(ctx, to)
	if err != nil {
		return nil, nil, err
	}

	// O6 skip trailer on the downstream-only range (from..to). This is a git
	// TRAILER (a key:value in the message's last paragraph), parsed with git's
	// own trailer semantics — not a whole-body line match — so a commit that
	// merely quotes 'Oiax-Backflow: skip' in prose elsewhere in its body does
	// not falsely suppress a legitimate hotfix.
	skips, err = c.backflowSkipTrailers(ctx, fromRef+".."+toRef)
	if err != nil {
		return nil, nil, err
	}

	// Cherry-pick -x provenance on the target's commits since it diverged from
	// the source (targetMergeBase..target). targetMergeBase is already an object
	// id from a prior MergeBase call; target still needs resolving to its ref.
	provenance = make(map[string]struct{})
	if targetMergeBase != "" {
		targetRef, err := c.Git.ResolveRev(ctx, target)
		if err != nil {
			return nil, nil, err
		}
		targetBodies, err := c.commitBodies(ctx, targetMergeBase+".."+targetRef)
		if err != nil {
			return nil, nil, err
		}
		for _, body := range targetBodies {
			// `git cherry-pick -x` appends the "(cherry picked from commit <sha>)"
			// line to the message's LAST paragraph (its trailer block). Match only
			// that block — mirroring how backflowSkipTrailers leans on git's own
			// trailer semantics — so a commit that merely mentions a source SHA, or
			// even quotes a provenance line, in earlier prose does not falsely mark
			// that source commit already-returned and silently drop a live hotfix.
			for _, m := range cherryPickedFromRE.FindAllStringSubmatch(lastParagraph(body), -1) {
				provenance[m[1]] = struct{}{}
			}
		}
	}
	return skips, provenance, nil
}

// commitBodies returns the full commit message body of every commit in the
// given rev-range, keyed by full commit SHA. It runs in the git Runner's
// working directory. Records are NUL-delimited (commit bodies contain
// newlines) and each is "<sha>\x1f<body>".
func (c *Coordinator) commitBodies(ctx context.Context, revRange string) (map[string]string, error) {
	cmd := exec.CommandContext(ctx, "git", "log", "--no-color", "-z",
		"--format=%H%x1f%B", "--end-of-options", revRange)
	cmd.Dir = c.Git.Dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("read commit bodies for %q: %w: %s", revRange, err, strings.TrimSpace(stderr.String()))
	}
	out := make(map[string]string)
	for _, rec := range strings.Split(stdout.String(), "\x00") {
		if strings.Trim(rec, "\n") == "" {
			continue
		}
		sha, body, ok := strings.Cut(rec, "\x1f")
		if !ok {
			return nil, fmt.Errorf("unexpected git log record %q", rec)
		}
		out[strings.TrimLeft(sha, "\n")] = body
	}
	return out, nil
}

// lastParagraph returns the final blank-line-delimited block of a commit
// message body — the trailer block, where `git cherry-pick -x` records its
// "(cherry picked from commit <sha>)" provenance line. Restricting provenance
// matching to this block (rather than the whole body) keeps a stray mention of
// the phrase in an earlier prose paragraph from being read as genuine
// provenance.
func lastParagraph(body string) string {
	body = strings.TrimRight(body, "\n")
	if i := strings.LastIndex(body, "\n\n"); i >= 0 {
		return body[i+2:]
	}
	return body
}

// backflowSkipTrailers returns the set of commit SHAs in revRange whose
// message carries the O6 'Oiax-Backflow: skip' git trailer. It uses git's
// %(trailers) pretty-format with a key filter, so matching follows git's
// trailer semantics (last-paragraph key:value blocks) rather than a naive
// whole-body scan: a mention of the trailer in ordinary prose does not match.
// A trailer value is recognized as a skip when it is exactly 'skip'
// (case-insensitive, trimmed). It follows the same no-shell posture as
// commitBodies: the range reaches git as an operand after --end-of-options.
func (c *Coordinator) backflowSkipTrailers(ctx context.Context, revRange string) (map[string]struct{}, error) {
	cmd := exec.CommandContext(ctx, "git", "log", "--no-color", "-z",
		"--format=%H%x1f%(trailers:key=Oiax-Backflow,valueonly=true,separator=%x1e)",
		"--end-of-options", revRange)
	cmd.Dir = c.Git.Dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("read skip trailers for %q: %w: %s", revRange, err, strings.TrimSpace(stderr.String()))
	}
	skips := make(map[string]struct{})
	for _, rec := range strings.Split(stdout.String(), "\x00") {
		if strings.Trim(rec, "\n") == "" {
			continue
		}
		sha, values, ok := strings.Cut(rec, "\x1f")
		if !ok {
			return nil, fmt.Errorf("unexpected git log record %q", rec)
		}
		for _, v := range strings.Split(values, "\x1e") {
			if strings.EqualFold(strings.TrimSpace(v), "skip") {
				skips[strings.TrimLeft(sha, "\n")] = struct{}{}
				break
			}
		}
	}
	return skips, nil
}

// Apply executes a plan's actions against the forge, in plan order, and
// reports the outcome. It is idempotent (the next reconcile converges) and
// aborts on the first forge error with a wrapped error — there is no
// rollback.
func (c *Coordinator) Apply(ctx context.Context, plan engine.Plan) (Result, error) {
	var res Result
	// The set of backflow edges that still conflict this run, keyed by
	// {source, target}. Initialized here (never nil) so the first conflict's
	// record path can write it without a nil-map panic, and so the end-of-Apply
	// sweep can distinguish "still conflicting" from "resolved" independently of
	// any best-effort forge outcome (ADR 0008).
	conflicted := map[[2]string]bool{}
	for _, a := range plan.Actions {
		switch a.Type {
		case engine.ActionCreatePromotionRequest:
			// The action carries no live head; re-resolve the current source
			// head so the created request records an up-to-date baseline.
			head, err := c.Git.Head(ctx, a.From)
			if err != nil {
				return res, fmt.Errorf("apply create %s->%s: %w", a.From, a.To, err)
			}
			// Text renders once, here at creation: an existing managed
			// request's human body is never re-rendered (updates rewrite only
			// the marker), so volatile template output cannot thrash it. A
			// render failure fails the apply loudly — a governance template's
			// scaffold is the change record, so a request without it must not
			// be opened (fail closed).
			title, body, err := c.promotionText(ctx, a, head)
			if err != nil {
				return res, fmt.Errorf("apply create %s->%s: %w", a.From, a.To, err)
			}
			if _, err := c.Forge.CreateRequest(ctx, forge.CreateRequest{
				Graph:      c.Graph.Name,
				Type:       engine.RequestTypePromotion,
				Source:     a.From,
				Target:     a.To,
				SourceHead: head,
				Title:      title,
				Body:       body,
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
			// A merge-strategy backflow edge can diverge here (never entering
			// applyBackflow) via the ADR-0006 amendments — a skip trailer in
			// range, or a target that forbids merge commits (planMergeBackflow).
			// Mark such an edge conflicted for this run BEFORE the sweep so a
			// still-diverging edge's durable artifact is never false-closed
			// (SKA-601); mirrors the sweep's config gate.
			if a.To == c.Graph.Backflow.Target && c.isBackflowSource(a.From) {
				conflicted[[2]string{a.From, a.To}] = true
			}
			// A descriptive message so the GitHub Actions annotation (routed
			// through the logger's handler) carries the edge and reason.
			c.log().Warn(fmt.Sprintf("reported divergence on %s -> %s: %s", a.From, a.To, a.Reason))

		case engine.ActionCreateBackflowRequest:
			if err := c.applyBackflow(ctx, a, &res, conflicted); err != nil {
				return res, err
			}

		default:
			return res, fmt.Errorf("apply: unknown action type %q", a.Type)
		}
	}
	// Close durable conflict artifacts whose edge no longer conflicts this run
	// (SKA-601). Reached only after the action loop completed without a returned
	// error — an errored Apply returns early above and leaves every artifact
	// untouched. Best-effort: resolveBackflowConflicts returns nil in practice.
	if err := c.resolveBackflowConflicts(ctx, conflicted, &res); err != nil {
		return res, err
	}
	// Per-run apply counts (SKA-600), the mutation-side complement of "plan
	// built": one structured record per run.
	c.log().Info("apply complete",
		"graph", plan.Graph,
		"applied", res.Applied,
		"superseded", res.Superseded,
		"divergence", res.Divergence,
		"conflicts", res.Conflicts,
	)
	return res, nil
}

// applyBackflow returns downstream-only commits to the backflow target by
// cherry-pick, then opens (or adopts) the managed backflow request. All git
// mutation happens in an ephemeral detached worktree that is always removed,
// so the caller's checkout and index are never touched. A cherry-pick
// conflict is a reported divergence (res.Divergence), not a created request:
// nothing is pushed and no request is opened.
func (c *Coordinator) applyBackflow(ctx context.Context, a engine.Action, res *Result, conflicted map[[2]string]bool) error {
	// The action carries a count and the plan-time branch, not the SHAs, so
	// re-derive the commits to return from live state — mirroring how the
	// promotion arms re-resolve the live head.
	st, err := c.backflowActionState(ctx, a)
	if err != nil {
		return fmt.Errorf("apply backflow %s->%s: %w", a.From, a.To, err)
	}
	toReturn := st.ToReturn
	if len(toReturn) == 0 {
		// Converged between plan and apply: nothing left to return.
		return nil
	}

	// Recompute the deterministic branch name from the LIVE source head just
	// observed, not the plan-time name on the action. The re-derived commits
	// belong to the current head, so if a hotfix advanced the source between
	// plan and apply the branch must identify that head — otherwise the content
	// lands on a branch named for a stale head and the name no longer maps to
	// what it carries (O4).
	branch := engine.BackflowBranchName(a.From, a.To, st.SourceHeadShort)

	// The template context for every backflow surface this action can
	// touch (request, merge message, conflict artifact): assembled once
	// from the same live state the replay operates on.
	tc := c.backflowContext(a, st, string(engine.RequestTypeBackflow))

	// Replay onto the target head inside an ephemeral detached worktree, always
	// cleaned up, so the caller's branch and index are never mutated.
	wt, cleanup, err := c.Git.Worktree(ctx, a.To)
	if err != nil {
		return fmt.Errorf("apply backflow %s->%s: %w", a.From, a.To, err)
	}
	defer cleanup()

	var head string
	if c.Graph.Backflow.Strategy == v1.BackflowStrategyMerge {
		// The templatable merge-commit message (SKA-54): empty when no
		// template is configured, keeping git's default. The rendered
		// message is a deterministic function of the merge inputs, so
		// re-runs at the same heads still produce an identical SHA.
		msg, _, err := c.templates().MergeMessage(tc)
		if err != nil {
			return fmt.Errorf("apply backflow %s->%s: %w", a.From, a.To, err)
		}
		// Merge strategy (ADR-0006): a single --no-ff merge of the source head
		// onto the target head returns the downstream set wholesale, producing a
		// two-parent merge commit. A merge conflict maps to the SAME reported-
		// divergence (exit 3) path a cherry-pick conflict uses: push nothing,
		// create nothing, worktree left clean by git merge --abort.
		head, err = wt.Merge(ctx, st.To.Head, msg)
		if err != nil {
			var conflict *git.MergeConflict
			if errors.As(err, &conflict) {
				res.Divergence = true
				c.log().Warn(fmt.Sprintf(
					"backflow %s -> %s halted on merge conflict at %s %q; no request created",
					a.From, a.To, conflict.SHA, conflict.Subject))
				// Merge attempts the whole source set (no per-commit clean
				// count), so whole == true and applied is ignored. Best-effort;
				// never downgrades the exit-3 divergence above.
				return c.recordBackflowConflict(ctx, a, st, conflict.SHA, conflict.Subject, 0, true, conflicted, res)
			}
			return fmt.Errorf("apply backflow %s->%s: %w", a.From, a.To, err)
		}
	} else {
		// Cherry-pick strategy: DownstreamOnly (and thus ToReturn) is
		// newest-first; cherry-pick applies oldest-first.
		shas := make([]string, len(toReturn))
		for i, cm := range toReturn {
			shas[len(toReturn)-1-i] = cm.SHA
		}
		head, err = wt.CherryPick(ctx, shas)
		if err != nil {
			var conflict *git.CherryPickConflict
			if errors.As(err, &conflict) {
				// Reported divergence (reconcile exit 3): surface the failing
				// commit and how many applied cleanly, push nothing, create nothing.
				res.Divergence = true
				c.log().Warn(fmt.Sprintf(
					"backflow %s -> %s halted on cherry-pick conflict at %s %q after %d applied cleanly; no request created",
					a.From, a.To, conflict.SHA, conflict.Subject, conflict.Applied))
				// Cherry-pick carries a per-commit clean count, so whole ==
				// false. Best-effort; never downgrades the exit-3 divergence.
				return c.recordBackflowConflict(ctx, a, st, conflict.SHA, conflict.Subject, conflict.Applied, false, conflicted, res)
			}
			return fmt.Errorf("apply backflow %s->%s: %w", a.From, a.To, err)
		}
	}

	// Every replayed commit may have been dropped by `git cherry-pick
	// --empty=drop` because its content already reached the target through a
	// squash or rebase merge that gave it a different patch-id than any single
	// replayed commit — so the plan's patch-identity rung never marked it
	// returned, yet the replay produces no new content. CherryPick then returns
	// the target head unchanged: the edge has CONVERGED. Push nothing, create
	// nothing, adopt or supersede nothing. Doing otherwise would force-push the
	// target head over a real backflow commit and 422-adopt the existing request
	// as success — silently losing the hotfix — or open a head==base request the
	// forge rejects with 422 on every run.
	targetHead, err := c.Git.Head(ctx, a.To)
	if err != nil {
		return fmt.Errorf("apply backflow %s->%s: %w", a.From, a.To, err)
	}
	if head == targetHead {
		return nil
	}

	// Churn reduction (M6): if the deterministic branch already points at this
	// exact replayed head, a prior run already pushed it, so re-pushing the
	// identical SHA is redundant. Compare against the branch's last-fetched
	// head and skip ONLY the force-push of an unchanged replay, sparing
	// whatever request references it a re-trigger of CI. This is deliberately
	// narrower than skipping the whole block: RemoteTrackingHead only proves
	// the branch was pushed with this content at some point, never that an
	// open request currently references it — a prior run's CreateRequest can
	// have failed after its PushBranch succeeded, or a human can have closed
	// the request without merging, and in both cases the branch head still
	// matches while no request exists. So CreateRequest below always still
	// runs; it is safely idempotent (422 duplicate -> adoptDuplicate) when a
	// matching open request does exist, and creates the missing one when it
	// doesn't. This does NOT stabilize the replay parent, so when the (busy)
	// target advances between runs the head genuinely changes and the
	// force-push still churns until the hotfix merges (bounded/self-healing).
	cur, ok, err := c.Git.RemoteTrackingHead(ctx, branch)
	if err != nil {
		return fmt.Errorf("apply backflow %s->%s: %w", a.From, a.To, err)
	}
	if !ok || cur != head {
		// Force-push the deterministic branch (confined to oiax/ by the
		// provider). The branch name is a pure function of the source head,
		// and the replayed HEAD is deterministic too (CherryPick pins the
		// committer), so a repeated run at the same source head re-pushes an
		// identical SHA — a genuine no-op.
		if err := c.Forge.PushBranch(ctx, forge.BranchPush{Name: branch, SHA: head, Force: true}); err != nil {
			return fmt.Errorf("apply backflow %s->%s: %w", a.From, a.To, err)
		}
	}

	// Open (or adopt) the managed backflow request. The head branch is the
	// pushed oiax/ branch; the base is the backflow target. Text renders
	// once, at creation (an adopt on 422 never rewrites the body), and a
	// render failure fails the apply — fail closed, as on promotion.
	title, body, err := c.templates().Backflow(tc)
	if err != nil {
		return fmt.Errorf("apply backflow %s->%s: %w", a.From, a.To, err)
	}
	if _, err := c.Forge.CreateRequest(ctx, forge.CreateRequest{
		Graph:      c.Graph.Name,
		Type:       engine.RequestTypeBackflow,
		Source:     branch,
		Target:     a.To,
		SourceHead: head,
		Title:      title,
		Body:       body,
	}); err != nil {
		return fmt.Errorf("apply backflow %s->%s: %w", a.From, a.To, err)
	}
	res.Applied++

	// Supersede: at most one active managed backflow request per (source,
	// target). A new hotfix advanced the branch, so close any strictly older
	// one.
	if err := c.supersedeBackflow(ctx, a, branch, res); err != nil {
		return fmt.Errorf("apply backflow %s->%s: %w", a.From, a.To, err)
	}
	return nil
}

// backflowActionState re-derives, from live state, the return set a backflow
// action operates on. The action's From is the backflow source (a downstream
// branch); the returned EdgeState carries the commits to return (ToReturn) and
// the current source-head short SHA (SourceHeadShort) that names the branch.
//
// The return set is the UNION of the returnable content across EVERY promotion
// edge into the backflow source, not just the first. A source can have several
// incoming edges; a commit that is downstream-only relative to a second
// incoming edge (present on the first edge's upstream but absent on the
// second's) is still content the source carries and the target lacks, and must
// be returned. Deriving the set from a single edge would silently drop it. Each
// edge's ToReturn is already filtered by patch identity and already-returned,
// so normally promoted content — which reached the source through its immediate
// upstream — is excluded from every term of the union.
func (c *Coordinator) backflowActionState(ctx context.Context, a engine.Action) (engine.EdgeState, error) {
	source := a.From
	target := c.Graph.Backflow.Target

	want := make(map[string]struct{})
	var short, sourceHead string
	var found bool
	for _, p := range c.Graph.Promotions {
		if p.To != source {
			continue
		}
		found = true
		obs, err := c.observe(ctx, p.From, source, nil, nil)
		if err != nil {
			return engine.EdgeState{}, err
		}
		st := engine.EvaluateEdge(obs)
		short = st.SourceHeadShort
		// The merge executor needs the live source head to merge onto the
		// target (cherry-pick replays the SHAs instead). Every incoming edge
		// observes the same source branch, so its head is stable across them.
		sourceHead = st.To.Head
		for _, cm := range st.ToReturn {
			want[cm.SHA] = struct{}{}
		}
	}
	if !found {
		return engine.EdgeState{}, fmt.Errorf("no promotion edge into backflow source %q", source)
	}

	// Order the union by one traversal of the source relative to the backflow
	// target (target..source, newest first). Cherry-pick needs a stable
	// topological order; merging the per-edge lists would not preserve it, and a
	// mis-ordered replay could fail to apply a child before its parent.
	ordered, err := c.Git.UniqueCommits(ctx, target, source)
	if err != nil {
		return engine.EdgeState{}, err
	}
	toReturn := make([]engine.Commit, 0, len(want))
	for _, cm := range ordered {
		if _, ok := want[cm.SHA]; ok {
			toReturn = append(toReturn, engine.Commit{SHA: cm.SHA, Subject: cm.Subject})
		}
	}

	return engine.EdgeState{
		To:              engine.BranchState{Name: source, Head: sourceHead},
		ToReturn:        toReturn,
		SourceHeadShort: short,
	}, nil
}

// supersedeBackflow closes any still-open managed backflow request for the
// same logical (source,target) whose encoded source head is STRICTLY OLDER
// than the one just created (an ancestor of it). The deterministic branch
// encodes the source head, so a newer hotfix yields a new branch and the prior
// request is stale; its oiax/ head branch is deleted and then the request is
// closed with an explanatory comment (delete-before-close, so a failed delete
// leaves the request open to retry rather than leaking the ref — L11),
// preventing orphan-ref accumulation.
//
// The ancestry guard makes supersede monotonic under concurrency. Two
// overlapping runs can observe divergent source heads and each create its own
// branch; a plain "close every other-named request" would let the run that saw
// the OLDER head close the NEWER run's request. Closing only requests whose
// head is an ancestor of the current one — never a descendant, nor a head whose
// lookup merely failed transiently — ensures a run never closes work built on a
// newer head than its own. A create that adopted an existing request (same
// branch) leaves nothing to supersede.
//
// Beyond the ancestry-guarded supersede it also cleans up an ORPHAN (L3): a
// still-open managed request whose encoded source head no longer resolves to
// any commit at all (a definitive not-found — the source branch's history was
// rewritten out from under it) can never converge, so its head branch is
// deleted and then it is closed with an explanation (same delete-before-close
// ordering, for the same retry-safety reason). A merely transient/unavailable
// lookup is never treated as an orphan; only git's clean "no such object"
// signal is.
//
// That "no such object" signal is ambiguous on a shallow clone (actions/
// checkout's default fetch-depth: 1): a depth-limited fetch only brings down
// each ref's tip, so an older, genuinely non-rewritten ancestor commit — the
// ordinary case for a stale-but-valid backflow request — resolves to the exact
// same "not found" as a real rewrite. The orphan path is therefore gated on
// IsShallowRepository: on a shallow clone it never closes, falling back to the
// same conservative "leave it alone" default as an operational CommitExists
// error, exactly as M5's Plan warning already flags this condition as
// unreliable.
func (c *Coordinator) supersedeBackflow(ctx context.Context, a engine.Action, branch string, res *Result) error {
	open, err := c.Forge.ListManagedRequests(ctx, forge.RequestFilter{
		Graph: c.Graph.Name,
		Type:  engine.RequestTypeBackflow,
	})
	if err != nil {
		return err
	}
	prefix := engine.BackflowBranchName(a.From, a.To, "")
	curHead, err := c.Git.ResolveCommit(ctx, strings.TrimPrefix(branch, prefix))
	if err != nil {
		return fmt.Errorf("resolve current backflow head: %w", err)
	}
	shallow, err := c.Git.IsShallowRepository(ctx)
	if err != nil {
		return fmt.Errorf("detect shallow repository: %w", err)
	}
	for _, r := range open {
		if r.Target != a.To || !strings.HasPrefix(r.Source, prefix) || r.Source == branch {
			continue
		}
		encoded := strings.TrimPrefix(r.Source, prefix)

		// Classify the stale request's encoded head. CommitExists reports a
		// DEFINITIVE not-found (the object is absent) apart from a malformed or
		// transient lookup, which surfaces as an error and is left strictly
		// alone.
		exists, err := c.Git.CommitExists(ctx, encoded)
		if err != nil {
			continue
		}
		if !exists {
			if shallow {
				// A shallow clone cannot tell "rewritten" apart from "never
				// fetched" (see the doc comment above): treat this exactly like
				// an operational CommitExists error and leave the request alone
				// rather than risk closing a legitimately stale-but-ancestor
				// request on a false "history was rewritten" claim.
				continue
			}
			// Orphan (L3): the encoded source head resolves to nothing, so this
			// request can never converge. Delete the leftover head branch (L11)
			// FIRST, then close. DeleteBranch is idempotent (an already-deleted
			// branch is treated as success), so ordering it first costs nothing on
			// the success path but makes the failure path self-healing: on a
			// genuine delete error we return before closing, so the request stays
			// open and in the next run's open set to retry the delete-then-close
			// pair. Closing first would drop the request from ListManagedRequests
			// on every later run, permanently leaking the oiax/ branch a failed
			// delete left behind.
			if err := c.deleteBackflowBranch(ctx, r.Source); err != nil {
				return err
			}
			reason := forge.Reason{Summary: fmt.Sprintf(
				"Closed by Oiax: this backflow request's encoded source head (%s) no longer resolves to any "+
					"commit — the %s branch history was rewritten — so it can never converge. Cleaning up the "+
					"orphaned request.", encoded, a.From)}
			if err := c.Forge.CloseRequest(ctx, forge.RequestID(r.ID), reason); err != nil {
				return err
			}
			res.Superseded++
			c.log().Info("closed orphaned backflow request",
				"request", r.ID, "branch", r.Source, "encodedHead", encoded)
			continue
		}

		// Resolve the encoded head for the ancestry test. An unexpected resolve
		// failure (it existed a moment ago) is left alone rather than closed.
		staleHead, err := c.Git.ResolveCommit(ctx, encoded)
		if err != nil {
			continue
		}
		if staleHead == curHead {
			continue
		}
		// Only supersede when the stale head is an ancestor of the current one.
		anc, err := c.Git.IsAncestor(ctx, staleHead, curHead)
		if err != nil {
			return err
		}
		if !anc {
			continue
		}
		// Delete the superseded head branch (L11) FIRST, then close: confined to
		// oiax/, so removing the ref is in-contract, and DeleteBranch is
		// idempotent, so a genuine delete failure returns before the close —
		// leaving the request open for the next run to retry the delete-then-close
		// pair rather than closing it, dropping it from ListManagedRequests, and
		// permanently leaking the orphaned ref.
		if err := c.deleteBackflowBranch(ctx, r.Source); err != nil {
			return err
		}
		reason := forge.Reason{Summary: fmt.Sprintf(
			"Superseded by %s: a newer %s head advanced the backflow branch.", branch, a.From)}
		if err := c.Forge.CloseRequest(ctx, forge.RequestID(r.ID), reason); err != nil {
			return err
		}
		res.Superseded++
		c.log().Info("superseded backflow request",
			"request", r.ID, "stale", r.Source, "current", branch)
	}
	return nil
}

// deleteBackflowBranch removes a superseded, closed, or orphaned managed
// backflow request's head branch. Deletion is confined to the oiax/ namespace:
// the branch is a pure-oiax artifact, so removing it is in-contract, and a name
// outside oiax/ is never touched (defense in depth over the provider's own
// namespace guard). The forge treats an already-deleted branch as success, so a
// repeated reconcile stays idempotent.
func (c *Coordinator) deleteBackflowBranch(ctx context.Context, branch string) error {
	if !strings.HasPrefix(branch, "oiax/") {
		return nil
	}
	if err := c.Forge.DeleteBranch(ctx, branch); err != nil {
		return fmt.Errorf("delete backflow branch %q: %w", branch, err)
	}
	return nil
}

// log returns a usable logger even when none was injected, so Apply never
// panics on a nil Log.
// warnMergeMethodMismatch warns, in graph-declaration order, for every
// promotion edge whose configured mergeMethod the repository does not currently
// permit — so a human notices a merge-button setting that contradicts the
// config before a promotion request cannot be merged the expected way. It is
// advisory: the repository's permitted methods are fetched only when at least
// one edge declares a mergeMethod, and a fetch failure is logged at debug and
// never fails planning. Oiax never modifies repository settings.
func (c *Coordinator) warnMergeMethodMismatch(ctx context.Context) {
	type want struct{ edge, method string }
	var wants []want
	for _, p := range c.Graph.Promotions {
		if m := string(p.Expectations.MergeMethod); m != "" {
			wants = append(wants, want{edge: p.From + " -> " + p.To, method: m})
		}
	}
	if len(wants) == 0 {
		return
	}
	allowed, err := c.Forge.RepoMergeMethods(ctx)
	if err != nil {
		c.log().Debug("skipping mergeMethod repository-settings check: " + err.Error())
		return
	}
	for _, w := range wants {
		if !allowed.Allows(w.method) {
			c.log().Warn(fmt.Sprintf(
				"config expects %q merges on %s, but the repository does not allow %q merges; "+
					"enable it in the repository's merge settings or change mergeMethod",
				w.method, w.edge, w.method))
		}
	}
}

// warnSourceBranchDeletion warns when the repository automatically deletes a
// merged request's source branch.
//
// Oiax opens every promotion request FROM a long-lived graph branch, so a
// repository-wide auto-delete — written for short-lived feature branches, and
// unable to tell the two apart — deletes a graph branch the moment the first
// promotion request merges. The next reconcile then cannot resolve that branch
// and the whole graph stalls. A branch-deletion protection rule is not a
// reliable guard: the deletion runs as the merging user, so any bypass role
// bypasses it silently as a side effect of pressing Merge.
//
// The warning is unconditional rather than CI-fatal (cf. RefuseShallow): the
// setting degrades nothing until someone merges, so refusing to reconcile a
// currently-healthy graph would be worse than the hazard. A settings read that
// fails — a token without repository-read scope, a forge without the concept —
// is logged at debug and skipped; an advisory check must never break reconcile.
func (c *Coordinator) warnSourceBranchDeletion(ctx context.Context) {
	if len(c.Graph.Promotions) == 0 {
		return
	}
	deletes, err := c.Forge.RepoDeletesSourceOnMerge(ctx)
	if err != nil {
		c.log().Debug("skipping source-branch-deletion repository-settings check: " + err.Error())
		return
	}
	if !deletes {
		return
	}
	sources := make([]string, 0, len(c.Graph.Promotions))
	seen := make(map[string]struct{}, len(c.Graph.Promotions))
	for _, p := range c.Graph.Promotions {
		if _, dup := seen[p.From]; dup {
			continue
		}
		seen[p.From] = struct{}{}
		sources = append(sources, p.From)
	}
	c.log().Warn(fmt.Sprintf(
		"repository deletes the source branch when a request merges, but every promotion request "+
			"is opened from a long-lived graph branch (%s); merging one deletes that branch and "+
			"every later reconcile fails on it. A branch-deletion rule does not prevent this — the "+
			"deletion runs as the merging user, so a bypass role skips the rule silently. Turn off "+
			"automatic head-branch deletion for this repository",
		strings.Join(sources, ", ")))
}

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

// templates returns the wired template set, or the built-in defaults when
// none was injected — mirroring log()'s nil-safety. The rendered human
// text always sits ABOVE the machine-readable marker the provider appends;
// the marker itself is never templatable (see internal/tmpl).
func (c *Coordinator) templates() *tmpl.Set {
	if c.Templates != nil {
		return c.Templates
	}
	return tmpl.Default()
}

// promotionText renders the title and body of a promotion request to
// create. The built-in text needs only the action's count and the live
// head; a CUSTOMIZED template (global or per-edge) gets the full variable
// context, re-deriving the unpromoted commit list from live state —
// through the same observe/evaluate path the plan used, including the
// baseline rung (the merged-request listing), so the listed commits are
// the post-ladder set, never raw rev-list output. The extra observation
// cost is paid only on the rare create action of an edge whose text is
// actually customized.
func (c *Coordinator) promotionText(ctx context.Context, a engine.Action, head string) (title, body string, err error) {
	tc := tmpl.Context{
		Graph:      c.Graph.Name,
		Type:       string(engine.RequestTypePromotion),
		From:       a.From,
		To:         a.To,
		Count:      a.Unpromoted,
		SourceHead: head,
	}
	if c.templates().PromotionCustomized(a.From, a.To) {
		merged, err := c.Forge.ListManagedRequests(ctx, forge.RequestFilter{
			Graph: c.Graph.Name,
			Type:  engine.RequestTypePromotion,
			State: forge.RequestStateMerged,
		})
		if err != nil {
			return "", "", err
		}
		obs, err := c.observe(ctx, a.From, a.To, nil, merged)
		if err != nil {
			return "", "", err
		}
		st := engine.EvaluateEdge(obs)
		short, err := c.Git.ShortSHA(ctx, a.From)
		if err != nil {
			return "", "", err
		}
		tc.Count = len(st.Unpromoted)
		tc.Commits = tmplCommits(st.Unpromoted)
		tc.SourceHeadShort = short
	}
	return c.templates().Promotion(a.From, a.To, tc)
}

// backflowContext assembles the template context the backflow surfaces
// share (the managed request, the merge-commit message, and — with
// Conflict filled in by the caller — the conflict artifact), from the same
// re-derived live state the replay operates on. Count is the true return
// count; Commits is capped (tmplCommits).
func (c *Coordinator) backflowContext(a engine.Action, st engine.EdgeState, typ string) tmpl.Context {
	strategy := c.Graph.Backflow.Strategy
	mechanism := "cherry-pick"
	if strategy == v1.BackflowStrategyMerge {
		mechanism = "merge commit"
	}
	return tmpl.Context{
		Graph:           c.Graph.Name,
		Type:            typ,
		From:            a.From,
		To:              a.To,
		Count:           len(st.ToReturn),
		Commits:         tmplCommits(st.ToReturn),
		SourceHead:      st.To.Head,
		SourceHeadShort: st.SourceHeadShort,
		Strategy:        string(strategy),
		Mechanism:       mechanism,
	}
}

// tmplCommits converts engine commits to the template commit list,
// applying the documented count cap; Context.Count carries the true total,
// so a template can say "and N more". Order is preserved (newest first).
func tmplCommits(cs []engine.Commit) []tmpl.Commit {
	n := min(len(cs), tmpl.MaxCommits)
	if n == 0 {
		return nil
	}
	out := make([]tmpl.Commit, n)
	for i := range n {
		out[i] = tmpl.NewCommit(cs[i].SHA, cs[i].Subject)
	}
	return out
}

// canonicalConflictArtifacts groups the open conflict artifacts by
// (source, target), keeps the lowest-numbered as canonical, and closes every
// other duplicate with a consolidating comment. arts MUST be in ascending
// issue-number order (the ListConflictArtifacts contract), so the first per
// group is canonical. Best-effort: a close failure logs and leaves the extra
// for the next run to collapse. Returns the canonical artifact per edge key.
// Used by both the record path and the sweep — the read-path consolidation
// guarantee (ADR 0008): a leaked duplicate self-heals on the next reconcile.
func (c *Coordinator) canonicalConflictArtifacts(ctx context.Context, arts []forge.ConflictArtifact) map[[2]string]forge.ConflictArtifact {
	canon := make(map[[2]string]forge.ConflictArtifact)
	for _, art := range arts {
		key := [2]string{art.Source, art.Target}
		if _, seen := canon[key]; !seen {
			canon[key] = art
			continue
		}
		if err := c.Forge.CloseConflictArtifact(ctx, art.ID, forge.Reason{
			Summary: fmt.Sprintf(
				"Closed by Oiax: consolidating a duplicate backflow-conflict artifact "+
					"for %s -> %s; the lower-numbered issue is canonical.",
				art.Source, art.Target),
		}); err != nil {
			c.log().Warn(fmt.Sprintf(
				"backflow conflict artifact consolidation failed for %s -> %s (issue %s); leaving for next run",
				art.Source, art.Target, art.ID), "err", err)
		}
	}
	return canon
}

// recordBackflowConflict ensures a durable conflict artifact exists for the
// (a.From -> a.To) edge, keyed to the live source head. It first marks the edge
// conflicted for this run (before any forge I/O), then best-effort ensures the
// artifact: create-if-absent, adopt-if-same-head, advance-to-live-tip
// otherwise, under a guard that never regresses the record to a staler run's
// head. A forge failure logs a warning and returns nil so the exit-3
// divergence is never downgraded to exit 1 (ADR 0008). Only called inside the
// errors.As conflict branches. whole distinguishes merge (true) from
// cherry-pick (false); applied is the clean count and is ignored when whole.
func (c *Coordinator) recordBackflowConflict(
	ctx context.Context, a engine.Action, st engine.EdgeState,
	sha, subject string, applied int, whole bool,
	conflicted map[[2]string]bool, res *Result) error {
	// (1) Mark the edge conflicted for this run BEFORE any forge call, so the
	// end-of-Apply sweep can never false-close a genuinely conflicting edge
	// even if every forge call below fails on a flaky forge.
	edge := [2]string{a.From, a.To}
	conflicted[edge] = true

	// (2) The full live source head is the ancestry key.
	curHead := st.To.Head

	// (3) List (best-effort). The conflicted flag is already set, so a list
	// failure leaves any existing artifact untouched and exit 3 intact.
	arts, err := c.Forge.ListConflictArtifacts(ctx, c.Graph.Name)
	if err != nil {
		c.log().Warn(fmt.Sprintf(
			"backflow conflict artifact list failed for %s -> %s; leaving for next run",
			a.From, a.To), "err", err)
		return nil
	}

	// (4) Consolidate duplicates on the read path and pick the canonical.
	canon := c.canonicalConflictArtifacts(ctx, arts)
	existing, ok := canon[edge]

	// (5) The spec to create or refresh with, keyed to this run's live head.
	// The failing subject is attacker-influenceable free text and lives ONLY
	// in the rendered human text, never in a marker identity field. Unlike
	// the create paths, a render failure here follows this function's
	// best-effort posture (ADR 0008): warn and leave the artifact for the
	// next run rather than downgrade the exit-3 divergence to exit 1.
	tc := c.backflowContext(a, st, "conflict")
	tc.Conflict = &tmpl.Conflict{SHA: sha, Subject: subject, Applied: applied, Whole: whole}
	title, body, terr := c.templates().BackflowConflict(tc)
	if terr != nil {
		c.log().Warn(fmt.Sprintf(
			"backflow conflict artifact text render failed for %s -> %s; leaving for next run",
			a.From, a.To), "err", terr)
		return nil
	}
	spec := forge.ConflictArtifactSpec{
		Graph:      c.Graph.Name,
		Source:     a.From,
		Target:     a.To,
		SourceHead: curHead,
		Title:      title,
		Body:       body,
	}

	// (6) Absent: create.
	if !ok {
		if _, err := c.Forge.CreateConflictArtifact(ctx, spec); err != nil {
			c.log().Warn(fmt.Sprintf(
				"backflow conflict artifact create failed for %s -> %s; leaving for next run",
				a.From, a.To), "err", err)
			return nil
		}
		res.Conflicts++
		return nil
	}

	// updateArtifact refreshes the canonical artifact in place (best-effort). An
	// advance is not a new artifact, so it never bumps res.Conflicts.
	updateArtifact := func() error {
		if err := c.Forge.UpdateConflictArtifact(ctx, existing.ID, spec); err != nil {
			c.log().Warn(fmt.Sprintf(
				"backflow conflict artifact update failed for %s -> %s; leaving for next run",
				a.From, a.To), "err", err)
		}
		return nil
	}

	// (7) Present: decide the write on the recorded head vs the live head.
	if existing.SourceHead == curHead {
		// Adopt, no write: the recorded head matches this run's source head, so
		// the artifact already points the operator at the right stuck head. We
		// deliberately skip refreshing the body to avoid an issue-edit every
		// reconcile. Accepted bound (ADR 0008): on the cherry-pick strategy a
		// pure TARGET advance while the source head is fixed can shift which
		// commit conflicts, so the body's failing-commit line may lag until the
		// source head next moves — at which point this branch is not taken and
		// the body refreshes. The merge strategy is unaffected (its failing SHA
		// is the source head itself).
		return nil
	}
	exists, err := c.Git.CommitExists(ctx, existing.SourceHead)
	if err != nil {
		// Transient lookup failure: leave the record alone.
		return nil
	}
	if !exists {
		shallow, err := c.Git.IsShallowRepository(ctx)
		if err != nil {
			return nil
		}
		if shallow {
			// A shallow clone cannot tell "rewritten" from "never fetched" (the
			// supersedeBackflow caveat): leave the record alone.
			return nil
		}
		// Recorded head was rewritten away on a full clone: the live tip is
		// authoritative — follow it so the operator is not stranded on a commit
		// that no longer exists on the branch.
		return updateArtifact()
	}
	// Recorded head still resolves. Advance only when THIS run's head is not a
	// strict ancestor of the recorded head (equality was handled above), i.e.
	// the live head is a descendant of, or diverged from, the recorded head.
	older, err := c.Git.IsAncestor(ctx, curHead, existing.SourceHead)
	if err != nil {
		return nil
	}
	if older {
		// This run observed a staler head: never regress the newer record.
		return nil
	}
	return updateArtifact()
}

// resolveBackflowConflicts closes durable conflict artifacts whose edge no
// longer conflicts this run — the replay succeeded, the edge converged, or the
// edge quiesced (produced no backflow action). Close is monotonic,
// orphan-aware, and gated to currently-configured backflow edges, mirroring
// supersedeBackflow. Best-effort throughout: a forge error logs and returns
// nil rather than failing Apply.
func (c *Coordinator) resolveBackflowConflicts(ctx context.Context, conflicted map[[2]string]bool, res *Result) error {
	arts, err := c.Forge.ListConflictArtifacts(ctx, c.Graph.Name)
	if err != nil {
		c.log().Warn("backflow conflict artifact sweep: list failed; leaving all artifacts for next run", "err", err)
		return nil
	}
	// Consolidate any leaked duplicate on the read path, then sweep canonicals.
	canon := c.canonicalConflictArtifacts(ctx, arts)
	if len(canon) == 0 {
		// Nothing to sweep — skip the git ancestry work entirely.
		return nil
	}
	shallow, err := c.Git.IsShallowRepository(ctx)
	if err != nil {
		// Cannot classify orphans safely: leave every artifact alone.
		return nil
	}
	for _, art := range canon {
		edge := [2]string{art.Source, art.Target}
		// (a) Config gate: only sweep artifacts for a currently-configured
		// backflow edge. An artifact whose edge left the config is left for
		// manual dismissal — a run that no longer examines an edge must not
		// close its artifact on stale info.
		if !c.Graph.Backflow.Enabled || art.Target != c.Graph.Backflow.Target || !c.isBackflowSource(art.Source) {
			continue
		}
		// (b) Still-conflicting gate: this run re-recorded a conflict for the
		// edge (the flag was set before any forge I/O), so never false-close it.
		if conflicted[edge] {
			continue
		}
		resolved := fmt.Sprintf(
			"Closed by Oiax: the backflow from %s to %s no longer conflicts (the "+
				"replay succeeded, the edge converged, or the commits became "+
				"returned/skipped).",
			art.Source, art.Target)
		// (c) Resolve the live source head.
		curHead, err := c.Git.Head(ctx, art.Source)
		if err != nil {
			// Branch gone / unresolvable → orphan path.
			exists, cerr := c.Git.CommitExists(ctx, art.SourceHead)
			if cerr != nil {
				continue // transient: leave alone
			}
			if !exists && !shallow {
				c.closeBackflowConflict(ctx, art, fmt.Sprintf(
					"Closed by Oiax: the backflow from %s to %s can never converge — the "+
						"source branch history was rewritten/removed.",
					art.Source, art.Target))
			}
			// shallow, or the recorded head still exists: leave alone.
			continue
		}
		// Monotonic close gate: close only when the recorded head is an ancestor
		// of (or equal to) the live head. A recorded head that is diverged from,
		// or newer than, the live head is left for manual dismissal.
		if art.SourceHead == curHead {
			c.closeBackflowConflict(ctx, art, resolved)
			continue
		}
		anc, err := c.Git.IsAncestor(ctx, art.SourceHead, curHead)
		if err != nil {
			continue
		}
		if anc {
			c.closeBackflowConflict(ctx, art, resolved)
		}
	}
	return nil
}

// closeBackflowConflict closes a resolved (or orphaned) conflict artifact with
// the given reason comment, best-effort: a forge error logs and leaves the
// artifact open to retry on the next run.
func (c *Coordinator) closeBackflowConflict(ctx context.Context, art forge.ConflictArtifact, reason string) {
	if err := c.Forge.CloseConflictArtifact(ctx, art.ID, forge.Reason{Summary: reason}); err != nil {
		c.log().Warn(fmt.Sprintf(
			"backflow conflict artifact close failed for %s -> %s (issue %s); leaving open to retry",
			art.Source, art.Target, art.ID), "err", err)
	}
}
