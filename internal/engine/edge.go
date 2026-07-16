package engine

// EdgeObservation is everything the equivalence ladder needs to settle one
// promotion edge. All git and forge work is already done before evaluation;
// this struct is pure data. EvaluateEdge never reaches out to git or a forge —
// it only walks the ladder over these injected fields.
type EdgeObservation struct {
	// From is the source branch (Name + Head).
	From BranchState
	// To is the destination branch (Name + Head).
	To BranchState

	// Candidates are commits in to..from (source content not reachable in the
	// destination by ancestry), newest first. Empty ⇒ reachability says in
	// sync.
	Candidates []Commit

	// DownstreamOnly are commits in from..to (destination content absent from
	// the source), newest first. Passed straight through to EdgeState.
	DownstreamOnly []Commit

	// SourceHeadShort is the short SHA of the backflow source head — the
	// downstream head (To.Head) abbreviated. It is the trailing segment of the
	// deterministic backflow branch name and is passed straight through to
	// EdgeState. Empty when To is not a backflow source.
	SourceHeadShort string

	// DownstreamPatchIDs maps each DownstreamOnly SHA to its stable patch-id.
	// Used to decide, by content, which downstream commits are already
	// represented on the backflow target.
	DownstreamPatchIDs map[string]string
	// ReturnedPatchIDs is the set of stable patch-ids already present on the
	// backflow target for the downstream range. A downstream commit whose
	// patch-id is here has already been returned (handles cherry-picks whose
	// SHAs were rewritten and independent re-application).
	ReturnedPatchIDs map[string]struct{}
	// SkippedByTrailer is the set of DownstreamOnly SHAs carrying the O6
	// 'Oiax-Backflow: skip' trailer — intentionally not backflowed.
	SkippedByTrailer map[string]struct{}
	// ReturnedByProvenance is the set of DownstreamOnly SHAs a backflow-target
	// commit's 'git cherry-pick -x' provenance trailer names — already
	// returned by identity even when conflict resolution rewrote the patch-id.
	ReturnedByProvenance map[string]struct{}

	// CandidatePatchIDs maps each candidate SHA to its stable patch-id.
	CandidatePatchIDs map[string]string
	// DestinationPatchIDs is the set of stable patch-ids present in
	// merge-base(from,to)..to. A candidate whose patch-id is here is promoted.
	DestinationPatchIDs map[string]struct{}

	// TreesEqual reports tree(from) == tree(to).
	TreesEqual bool

	// PromotedByBaseline is the set of candidate SHAs reachable from the
	// recorded sourceHead of a merged managed request for this edge. Empty
	// when there is no such request.
	PromotedByBaseline map[string]struct{}

	// ManagedRequest is the open managed request for this edge, if any
	// (drives create/update/close in BuildPlan).
	ManagedRequest *ChangeRequest
	// Mergeable is advisory forge mergeability, passed through.
	Mergeable *bool
}

// EvaluateEdge applies the content-based equivalence ladder and returns the
// EdgeState that BuildPlan consumes. It is pure: an identical observation
// yields an identical EdgeState, and it makes no git or forge calls.
//
// The ladder runs first-conclusive-rung-wins:
//
//  1. Reachability: no candidates ⇒ in sync.
//  2. Patch identity: candidates whose stable patch-id already appears in the
//     destination are promoted (handles rebase merges and 1:1 rewrites).
//  3. Head-tree: equal source and destination trees ⇒ in sync (squash at the
//     moment of merge).
//  4. Baseline: candidates reachable from a merged managed request's recorded
//     sourceHead are promoted (squash after the source advanced).
//  5. Promotion required: any survivors are the unpromoted commits.
func EvaluateEdge(obs EdgeObservation) EdgeState {
	toReturn, excluded := backflowToReturn(obs)
	state := EdgeState{
		From:            obs.From,
		To:              obs.To,
		DownstreamOnly:  obs.DownstreamOnly,
		ToReturn:        toReturn,
		Excluded:        excluded,
		SourceHeadShort: obs.SourceHeadShort,
		ManagedRequest:  obs.ManagedRequest,
		Mergeable:       obs.Mergeable,
		Equivalence:     EquivalenceReachability,
	}

	// Rung 1: reachability. Nothing reachable-only on the source ⇒ in sync.
	if len(obs.Candidates) == 0 {
		return state
	}

	// Rung 2: patch identity. Drop candidates already represented by content
	// in the destination. An emptied set settles the edge.
	remaining := make([]Commit, 0, len(obs.Candidates))
	for _, c := range obs.Candidates {
		if _, ok := obs.DestinationPatchIDs[obs.CandidatePatchIDs[c.SHA]]; ok {
			continue
		}
		remaining = append(remaining, c)
	}
	if len(remaining) == 0 {
		state.Equivalence = EquivalencePatchIdentity
		return state
	}

	// Rung 3: head-tree. Equal trees mean the destination already carries the
	// source content even though the commits differ.
	if obs.TreesEqual {
		state.Equivalence = EquivalenceHeadTree
		return state
	}

	// Rung 4: promotion baseline. Drop survivors reachable from a merged
	// managed request's recorded sourceHead. An emptied set settles the edge.
	survivors := make([]Commit, 0, len(remaining))
	for _, c := range remaining {
		if _, ok := obs.PromotedByBaseline[c.SHA]; ok {
			continue
		}
		survivors = append(survivors, c)
	}
	if len(survivors) == 0 {
		state.Equivalence = EquivalenceBaseline
		return state
	}

	// Rung 5: promotion required. The survivors are unpromoted. No higher rung
	// settled them, so the edge carries EquivalenceReachability, matching the
	// planner's expectation for a diverged edge.
	state.Unpromoted = survivors
	state.Equivalence = EquivalenceReachability
	return state
}

// backflowToReturn purely filters DownstreamOnly down to the commits that
// still need returning to the backflow target, and records why each dropped
// commit was excluded. A downstream commit is excluded when its SHA was
// resolved as intentionally withheld (the O6 'Oiax-Backflow: skip' trailer),
// already returned by identity (cherry-pick -x provenance), or already
// returned by content (its patch-id is present on the target) — reported in
// that precedence order when several apply. Order is preserved (newest
// first); both results are nil when empty, matching the Unpromoted
// convention so identical observations yield DeepEqual EdgeStates.
func backflowToReturn(obs EdgeObservation) ([]Commit, []BackflowExclusion) {
	if len(obs.DownstreamOnly) == 0 {
		return nil, nil
	}
	toReturn := make([]Commit, 0, len(obs.DownstreamOnly))
	var excluded []BackflowExclusion
	exclude := func(c Commit, reason BackflowExclusionReason) {
		excluded = append(excluded, BackflowExclusion{SHA: c.SHA, Subject: c.Subject, Reason: reason})
	}
	for _, c := range obs.DownstreamOnly {
		if _, ok := obs.SkippedByTrailer[c.SHA]; ok {
			exclude(c, BackflowExcludedSkip)
			continue
		}
		if _, ok := obs.ReturnedByProvenance[c.SHA]; ok {
			exclude(c, BackflowExcludedProvenance)
			continue
		}
		if pid, ok := obs.DownstreamPatchIDs[c.SHA]; ok {
			if _, present := obs.ReturnedPatchIDs[pid]; present {
				exclude(c, BackflowExcludedPatchID)
				continue
			}
		}
		toReturn = append(toReturn, c)
	}
	if len(toReturn) == 0 {
		toReturn = nil
	}
	return toReturn, excluded
}
