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
	state := EdgeState{
		From:           obs.From,
		To:             obs.To,
		DownstreamOnly: obs.DownstreamOnly,
		ManagedRequest: obs.ManagedRequest,
		Mergeable:      obs.Mergeable,
		Equivalence:    EquivalenceReachability,
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
