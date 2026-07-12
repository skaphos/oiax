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

	// Backflow identity-ladder inputs. When the destination is a backflow
	// source, its downstream-only commits are returned to the backflow target;
	// gather the content and identity signals the engine's ToReturn filter
	// consumes. Direction: `to` is the downstream source (e.g. main), the
	// backflow target (e.g. development) is authoritative.
	if c.isBackflowSource(to) && len(downstream) > 0 {
		target := c.Graph.Backflow.Target

		// Patch-ids of the downstream range (from..to), keyed by commit SHA:
		// the content fingerprint of each downstream-only commit. PatchIDs
		// omits merge commits and empty commits (neither has a diff).
		dpid, err := c.Git.PatchIDs(ctx, from, to)
		if err != nil {
			return engine.EdgeObservation{}, wrap(err)
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
			return engine.EdgeObservation{}, wrap(err)
		}
		if tmb != "" {
			rp, err := c.Git.PatchIDs(ctx, tmb, target)
			if err != nil {
				return engine.EdgeObservation{}, wrap(err)
			}
			for _, pid := range rp {
				returned[pid] = struct{}{}
			}
		}
		obs.ReturnedPatchIDs = returned

		// SHAs resolved as already returned by identity rather than content:
		// the source SHA named in a target commit's cherry-pick -x provenance
		// trailer, and any downstream commit carrying the O6 skip trailer.
		already, err := c.backflowAlreadyReturned(ctx, from, to, target, tmb)
		if err != nil {
			return engine.EdgeObservation{}, wrap(err)
		}
		obs.AlreadyReturned = already

		// The trailing segment of the deterministic backflow branch name is the
		// short SHA of the downstream (source) head.
		short, err := c.Git.ShortSHA(ctx, to)
		if err != nil {
			return engine.EdgeObservation{}, wrap(err)
		}
		obs.SourceHeadShort = short
	}

	return obs, nil
}

// isBackflowSource reports whether a branch is a configured backflow source
// (a downstream branch whose downstream-only commits are returned to the
// backflow target). It mirrors the engine's unexported predicate; reconcile
// cannot call across the package boundary.
func (c *Coordinator) isBackflowSource(branch string) bool {
	return c.Graph.Backflow.Enabled && slices.Contains(c.Graph.Backflow.Sources, branch)
}

// backflowAlreadyReturned resolves, by identity, which downstream-only SHAs
// have already been returned to the backflow target:
//
//   - a downstream commit carrying the O6 'Oiax-Backflow: skip' trailer is
//     treated as intentionally not backflowed;
//   - a target commit's 'cherry picked from commit <sha>' provenance names a
//     downstream SHA that has already been returned.
//
// It reads commit message bodies, which the git.Runner does not expose, so it
// shells out directly following the same no-shell posture: the range endpoints
// reach git as operands after --end-of-options, never as a shell string.
func (c *Coordinator) backflowAlreadyReturned(ctx context.Context, from, to, target, targetMergeBase string) (map[string]struct{}, error) {
	already := make(map[string]struct{})

	// O6 skip trailer on the downstream-only range (from..to). This is a git
	// TRAILER (a key:value in the message's last paragraph), parsed with git's
	// own trailer semantics — not a whole-body line match — so a commit that
	// merely quotes 'Oiax-Backflow: skip' in prose elsewhere in its body does
	// not falsely suppress a legitimate hotfix.
	skips, err := c.backflowSkipTrailers(ctx, "refs/heads/"+from+"..refs/heads/"+to)
	if err != nil {
		return nil, err
	}
	for sha := range skips {
		already[sha] = struct{}{}
	}

	// Cherry-pick -x provenance on the target's commits since it diverged from
	// the source (targetMergeBase..target).
	if targetMergeBase != "" {
		targetBodies, err := c.commitBodies(ctx, targetMergeBase+"..refs/heads/"+target)
		if err != nil {
			return nil, err
		}
		for _, body := range targetBodies {
			// `git cherry-pick -x` appends the "(cherry picked from commit <sha>)"
			// line to the message's LAST paragraph (its trailer block). Match only
			// that block — mirroring how backflowSkipTrailers leans on git's own
			// trailer semantics — so a commit that merely mentions a source SHA, or
			// even quotes a provenance line, in earlier prose does not falsely mark
			// that source commit already-returned and silently drop a live hotfix.
			for _, m := range cherryPickedFromRE.FindAllStringSubmatch(lastParagraph(body), -1) {
				already[m[1]] = struct{}{}
			}
		}
	}
	return already, nil
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
			if err := c.applyBackflow(ctx, a, &res); err != nil {
				return res, err
			}

		case engine.ActionNoOp:
			// Nothing to do.

		default:
			return res, fmt.Errorf("apply: unknown action type %q", a.Type)
		}
	}
	return res, nil
}

// applyBackflow returns downstream-only commits to the backflow target by
// cherry-pick, then opens (or adopts) the managed backflow request. All git
// mutation happens in an ephemeral detached worktree that is always removed,
// so the caller's checkout and index are never touched. A cherry-pick
// conflict is a reported divergence (res.Divergence), not a created request:
// nothing is pushed and no request is opened.
func (c *Coordinator) applyBackflow(ctx context.Context, a engine.Action, res *Result) error {
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

	// DownstreamOnly (and thus ToReturn) is newest-first; cherry-pick applies
	// oldest-first.
	shas := make([]string, len(toReturn))
	for i, cm := range toReturn {
		shas[len(toReturn)-1-i] = cm.SHA
	}

	// Cherry-pick onto the target head inside an ephemeral detached worktree,
	// always cleaned up, so the caller's branch and index are never mutated.
	wt, cleanup, err := c.Git.Worktree(ctx, a.To)
	if err != nil {
		return fmt.Errorf("apply backflow %s->%s: %w", a.From, a.To, err)
	}
	defer cleanup()

	head, err := wt.CherryPick(ctx, shas)
	if err != nil {
		var conflict *git.CherryPickConflict
		if errors.As(err, &conflict) {
			// Reported divergence (reconcile exit 3): surface the failing
			// commit and how many applied cleanly, push nothing, create nothing.
			res.Divergence = true
			c.log().Warn(fmt.Sprintf(
				"backflow %s -> %s halted on cherry-pick conflict at %s %q after %d applied cleanly; no request created",
				a.From, a.To, conflict.SHA, conflict.Subject, conflict.Applied))
			return nil
		}
		return fmt.Errorf("apply backflow %s->%s: %w", a.From, a.To, err)
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

	// Force-push the deterministic branch (confined to oiax/ by the provider).
	// The branch name is a pure function of the source head, and the replayed
	// HEAD is deterministic too (CherryPick pins the committer), so a repeated
	// run at the same source head re-pushes an identical SHA — a genuine no-op.
	if err := c.Forge.PushBranch(ctx, forge.BranchPush{Name: branch, SHA: head, Force: true}); err != nil {
		return fmt.Errorf("apply backflow %s->%s: %w", a.From, a.To, err)
	}

	// Open (or adopt) the managed backflow request. The head branch is the
	// pushed oiax/ branch; the base is the backflow target.
	if _, err := c.Forge.CreateRequest(ctx, forge.CreateRequest{
		Graph:      c.Graph.Name,
		Type:       engine.RequestTypeBackflow,
		Source:     branch,
		Target:     a.To,
		SourceHead: head,
		Title:      fmt.Sprintf("oiax: backflow %s to %s", a.From, a.To),
		Body:       backflowBody(a, len(toReturn)),
	}); err != nil {
		return fmt.Errorf("apply backflow %s->%s: %w", a.From, a.To, err)
	}
	res.Applied++

	// Supersede: at most one active managed backflow request per (source,
	// target). A new hotfix advanced the branch, so close any strictly older
	// one.
	if err := c.supersedeBackflow(ctx, a, branch); err != nil {
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
	var short string
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
		To:              engine.BranchState{Name: source},
		ToReturn:        toReturn,
		SourceHeadShort: short,
	}, nil
}

// supersedeBackflow closes any still-open managed backflow request for the
// same logical (source,target) whose encoded source head is STRICTLY OLDER
// than the one just created (an ancestor of it). The deterministic branch
// encodes the source head, so a newer hotfix yields a new branch and the prior
// request is stale; it is closed with an explanatory comment, never deleted.
//
// The ancestry guard makes supersede monotonic under concurrency. Two
// overlapping runs can observe divergent source heads and each create its own
// branch; a plain "close every other-named request" would let the run that saw
// the OLDER head close the NEWER run's request. Closing only requests whose
// head is an ancestor of the current one — never a descendant or an
// unresolvable/unknown head — ensures a run never closes work built on a newer
// head than its own. A create that adopted an existing request (same branch)
// leaves nothing to supersede.
func (c *Coordinator) supersedeBackflow(ctx context.Context, a engine.Action, branch string) error {
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
	for _, r := range open {
		if r.Target != a.To || !strings.HasPrefix(r.Source, prefix) || r.Source == branch {
			continue
		}
		// Resolve the stale request's encoded source head. An unknown or
		// ambiguous object is left alone rather than closed.
		staleHead, err := c.Git.ResolveCommit(ctx, strings.TrimPrefix(r.Source, prefix))
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
		reason := forge.Reason{Summary: fmt.Sprintf(
			"Superseded by %s: a newer %s head advanced the backflow branch.", branch, a.From)}
		if err := c.Forge.CloseRequest(ctx, forge.RequestID(r.ID), reason); err != nil {
			return err
		}
	}
	return nil
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

// backflowBody is the human body of a created backflow request; the provider
// appends the machine-readable marker after it.
func backflowBody(a engine.Action, count int) string {
	return fmt.Sprintf(
		"Oiax opened this request to return %d downstream-only commit(s) from `%s` back to `%s` by cherry-pick.\n\n"+
			"This request is managed by Oiax. Do not edit the metadata block below.",
		count, a.From, a.To)
}
