package engine

import (
	"reflect"
	"testing"
)

// commits builds a candidate slice from SHAs (newest first, subjects unused
// by the ladder).
func commits(shas ...string) []Commit {
	if len(shas) == 0 {
		return nil
	}
	cs := make([]Commit, len(shas))
	for i, s := range shas {
		cs[i] = Commit{SHA: s}
	}
	return cs
}

// patchIDs maps each SHA to a synthetic patch-id "p-<sha>".
func patchIDs(shas ...string) map[string]string {
	m := make(map[string]string, len(shas))
	for _, s := range shas {
		m[s] = "p-" + s
	}
	return m
}

// idSet builds a set from the given members.
func idSet(members ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(members))
	for _, s := range members {
		m[s] = struct{}{}
	}
	return m
}

func TestEvaluateEdge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		obs            EdgeObservation
		wantEquiv      Equivalence
		wantUnpromoted []Commit
	}{
		{
			// Rung 1. rev-list to..from empty: the destination already
			// reaches every source commit by ancestry.
			name: "reachability in sync",
			obs: EdgeObservation{
				From:       BranchState{Name: "dev", Head: "h-dev"},
				To:         BranchState{Name: "test", Head: "h-test"},
				Candidates: nil,
			},
			wantEquiv:      EquivalenceReachability,
			wantUnpromoted: nil,
		},
		{
			// Rung 2. Every candidate's stable patch-id already appears in
			// the destination: a rebase merge rewrote the SHAs but preserved
			// the diffs.
			name: "patch identity settles a rebase merge",
			obs: EdgeObservation{
				From:                BranchState{Name: "dev", Head: "h-dev"},
				To:                  BranchState{Name: "test", Head: "h-test"},
				Candidates:          commits("a", "b"),
				CandidatePatchIDs:   patchIDs("a", "b"),
				DestinationPatchIDs: idSet("p-a", "p-b"),
			},
			wantEquiv:      EquivalencePatchIdentity,
			wantUnpromoted: nil,
		},
		{
			// Rung 2 partial. One candidate matches by patch-id, one does
			// not, no higher rung settles the rest ⇒ the survivor is
			// unpromoted and the edge reports reachability.
			name: "partial patch identity leaves a survivor",
			obs: EdgeObservation{
				From:                BranchState{Name: "dev", Head: "h-dev"},
				To:                  BranchState{Name: "test", Head: "h-test"},
				Candidates:          commits("a", "b"),
				CandidatePatchIDs:   patchIDs("a", "b"),
				DestinationPatchIDs: idSet("p-a"),
			},
			wantEquiv:      EquivalenceReachability,
			wantUnpromoted: commits("b"),
		},
		{
			// Rung 3. No patch-id matches, but the source and destination
			// trees are identical: a squash merge at the moment of merge.
			name: "head-tree settles a squash merge",
			obs: EdgeObservation{
				From:                BranchState{Name: "dev", Head: "h-dev"},
				To:                  BranchState{Name: "test", Head: "h-test"},
				Candidates:          commits("a", "b"),
				CandidatePatchIDs:   patchIDs("a", "b"),
				DestinationPatchIDs: idSet(),
				TreesEqual:          true,
			},
			wantEquiv:      EquivalenceHeadTree,
			wantUnpromoted: nil,
		},
		{
			// Rung 4. Trees differ (the source advanced after the squash
			// merged), but every candidate is reachable from the merged
			// request's recorded sourceHead.
			name: "baseline settles a squash after the source advanced",
			obs: EdgeObservation{
				From:                BranchState{Name: "dev", Head: "h-dev"},
				To:                  BranchState{Name: "test", Head: "h-test"},
				Candidates:          commits("a", "b"),
				CandidatePatchIDs:   patchIDs("a", "b"),
				DestinationPatchIDs: idSet(),
				TreesEqual:          false,
				PromotedByBaseline:  idSet("a", "b"),
			},
			wantEquiv:      EquivalenceBaseline,
			wantUnpromoted: nil,
		},
		{
			// Rung 4 partial. The baseline covers the old commit but not the
			// one added after the merge ⇒ that commit is unpromoted.
			name: "baseline leaves a post-merge survivor",
			obs: EdgeObservation{
				From:                BranchState{Name: "dev", Head: "h-dev"},
				To:                  BranchState{Name: "test", Head: "h-test"},
				Candidates:          commits("new", "old"),
				CandidatePatchIDs:   patchIDs("new", "old"),
				DestinationPatchIDs: idSet(),
				TreesEqual:          false,
				PromotedByBaseline:  idSet("old"),
			},
			wantEquiv:      EquivalenceReachability,
			wantUnpromoted: commits("new"),
		},
		{
			// Rung 5. Nothing settles the edge: real, unpromoted divergence.
			name: "promotion required for a diverged edge",
			obs: EdgeObservation{
				From:                BranchState{Name: "dev", Head: "h-dev"},
				To:                  BranchState{Name: "test", Head: "h-test"},
				Candidates:          commits("a", "b", "c"),
				CandidatePatchIDs:   patchIDs("a", "b", "c"),
				DestinationPatchIDs: idSet(),
				TreesEqual:          false,
			},
			wantEquiv:      EquivalenceReachability,
			wantUnpromoted: commits("a", "b", "c"),
		},
		{
			// Destination ahead: downstream-only content, no candidates.
			// Reachability settles the promotion direction as in sync; the
			// downstream commits pass through untouched.
			name: "destination ahead is in sync with downstream carried through",
			obs: EdgeObservation{
				From:           BranchState{Name: "dev", Head: "h-dev"},
				To:             BranchState{Name: "test", Head: "h-test"},
				Candidates:     nil,
				DownstreamOnly: commits("x"),
			},
			wantEquiv:      EquivalenceReachability,
			wantUnpromoted: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EvaluateEdge(tt.obs)

			if got.Equivalence != tt.wantEquiv {
				t.Errorf("Equivalence = %q, want %q", got.Equivalence, tt.wantEquiv)
			}
			if !reflect.DeepEqual(got.Unpromoted, tt.wantUnpromoted) {
				t.Errorf("Unpromoted = %v, want %v", got.Unpromoted, tt.wantUnpromoted)
			}
			// Pass-through fields the ladder must never mutate.
			if got.From != tt.obs.From {
				t.Errorf("From = %+v, want %+v", got.From, tt.obs.From)
			}
			if got.To != tt.obs.To {
				t.Errorf("To = %+v, want %+v", got.To, tt.obs.To)
			}
			if !reflect.DeepEqual(got.DownstreamOnly, tt.obs.DownstreamOnly) {
				t.Errorf("DownstreamOnly = %v, want %v", got.DownstreamOnly, tt.obs.DownstreamOnly)
			}
		})
	}
}

// TestEvaluateEdgeToReturn exercises the pure backflow filter: DownstreamOnly
// minus everything already returned, by content (patch-id) or by resolved SHA
// (cherry-pick -x provenance and the O6 'Oiax-Backflow: skip' trailer).
func TestEvaluateEdgeToReturn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		obs          EdgeObservation
		wantToReturn []Commit
	}{
		{
			name: "no downstream content yields nothing to return",
			obs: EdgeObservation{
				From: BranchState{Name: "main", Head: "h-main"},
				To:   BranchState{Name: "dev", Head: "h-dev"},
			},
			wantToReturn: nil,
		},
		{
			name: "all downstream content returns when nothing already returned",
			obs: EdgeObservation{
				From:           BranchState{Name: "prod", Head: "h-prod"},
				To:             BranchState{Name: "main", Head: "h-main"},
				DownstreamOnly: commits("x", "y"),
			},
			wantToReturn: commits("x", "y"),
		},
		{
			name: "patch-id already on target excludes a returned commit",
			obs: EdgeObservation{
				From:               BranchState{Name: "prod", Head: "h-prod"},
				To:                 BranchState{Name: "main", Head: "h-main"},
				DownstreamOnly:     commits("x", "y"),
				DownstreamPatchIDs: patchIDs("x", "y"),
				ReturnedPatchIDs:   idSet("p-x"), // x already on target by content
			},
			wantToReturn: commits("y"),
		},
		{
			name: "O6 skip trailer and cherry-pick provenance SHAs are excluded",
			obs: EdgeObservation{
				From:           BranchState{Name: "prod", Head: "h-prod"},
				To:             BranchState{Name: "main", Head: "h-main"},
				DownstreamOnly: commits("skip", "picked", "fresh"),
				// "skip" carries Oiax-Backflow: skip; "picked" already returned
				// via cherry-pick -x provenance. Both resolved to SHAs upstream.
				SkippedByTrailer:     idSet("skip"),
				ReturnedByProvenance: idSet("picked"),
			},
			wantToReturn: commits("fresh"),
		},
		{
			name: "order is preserved (newest first) after filtering",
			obs: EdgeObservation{
				From:                 BranchState{Name: "prod", Head: "h-prod"},
				To:                   BranchState{Name: "main", Head: "h-main"},
				DownstreamOnly:       commits("c", "b", "a"),
				ReturnedByProvenance: idSet("b"),
			},
			wantToReturn: commits("c", "a"),
		},
		{
			name: "everything already returned yields nil, not empty slice",
			obs: EdgeObservation{
				From:                 BranchState{Name: "prod", Head: "h-prod"},
				To:                   BranchState{Name: "main", Head: "h-main"},
				DownstreamOnly:       commits("x", "y"),
				ReturnedByProvenance: idSet("x", "y"),
			},
			wantToReturn: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EvaluateEdge(tt.obs)
			if !reflect.DeepEqual(got.ToReturn, tt.wantToReturn) {
				t.Errorf("ToReturn = %v, want %v", got.ToReturn, tt.wantToReturn)
			}
			// The promotion ladder must never mutate DownstreamOnly.
			if !reflect.DeepEqual(got.DownstreamOnly, tt.obs.DownstreamOnly) {
				t.Errorf("DownstreamOnly = %v, want %v", got.DownstreamOnly, tt.obs.DownstreamOnly)
			}
		})
	}
}

// TestEvaluateEdgeExclusionReasons exercises the per-commit exclusion
// diagnostics: every downstream-only commit dropped from ToReturn is recorded
// with the rung that excluded it, in DownstreamOnly order, with the skip →
// provenance → patch-id precedence when several rungs match one commit.
func TestEvaluateEdgeExclusionReasons(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		obs          EdgeObservation
		wantExcluded []BackflowExclusion
	}{
		{
			name: "nothing excluded yields nil",
			obs: EdgeObservation{
				From:           BranchState{Name: "prod", Head: "h-prod"},
				To:             BranchState{Name: "main", Head: "h-main"},
				DownstreamOnly: commits("x"),
			},
			wantExcluded: nil,
		},
		{
			name: "each rung names its reason, in DownstreamOnly order",
			obs: EdgeObservation{
				From:                 BranchState{Name: "prod", Head: "h-prod"},
				To:                   BranchState{Name: "main", Head: "h-main"},
				DownstreamOnly:       commits("skipped", "picked", "matched", "fresh"),
				SkippedByTrailer:     idSet("skipped"),
				ReturnedByProvenance: idSet("picked"),
				DownstreamPatchIDs:   patchIDs("matched", "fresh"),
				ReturnedPatchIDs:     idSet("p-matched"),
			},
			wantExcluded: []BackflowExclusion{
				{SHA: "skipped", Reason: BackflowExcludedSkip},
				{SHA: "picked", Reason: BackflowExcludedProvenance},
				{SHA: "matched", Reason: BackflowExcludedPatchID},
			},
		},
		{
			name: "skip wins over provenance and patch-id when all match",
			obs: EdgeObservation{
				From:                 BranchState{Name: "prod", Head: "h-prod"},
				To:                   BranchState{Name: "main", Head: "h-main"},
				DownstreamOnly:       commits("x"),
				SkippedByTrailer:     idSet("x"),
				ReturnedByProvenance: idSet("x"),
				DownstreamPatchIDs:   patchIDs("x"),
				ReturnedPatchIDs:     idSet("p-x"),
			},
			wantExcluded: []BackflowExclusion{{SHA: "x", Reason: BackflowExcludedSkip}},
		},
		{
			name: "provenance wins over patch-id",
			obs: EdgeObservation{
				From:                 BranchState{Name: "prod", Head: "h-prod"},
				To:                   BranchState{Name: "main", Head: "h-main"},
				DownstreamOnly:       commits("x"),
				ReturnedByProvenance: idSet("x"),
				DownstreamPatchIDs:   patchIDs("x"),
				ReturnedPatchIDs:     idSet("p-x"),
			},
			wantExcluded: []BackflowExclusion{{SHA: "x", Reason: BackflowExcludedProvenance}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EvaluateEdge(tt.obs)
			if !reflect.DeepEqual(got.Excluded, tt.wantExcluded) {
				t.Errorf("Excluded = %v, want %v", got.Excluded, tt.wantExcluded)
			}
			// ToReturn and Excluded partition the returnable downstream set:
			// every DownstreamOnly commit is in exactly one of the two.
			if len(got.ToReturn)+len(got.Excluded) != len(tt.obs.DownstreamOnly) {
				t.Errorf("ToReturn (%d) + Excluded (%d) != DownstreamOnly (%d)",
					len(got.ToReturn), len(got.Excluded), len(tt.obs.DownstreamOnly))
			}
		})
	}
}

// TestEvaluateEdgePassesThroughSourceHeadShort confirms the backflow branch
// segment survives the ladder unchanged.
func TestEvaluateEdgePassesThroughSourceHeadShort(t *testing.T) {
	t.Parallel()

	obs := EdgeObservation{
		From:            BranchState{Name: "prod", Head: "h-prod"},
		To:              BranchState{Name: "main", Head: "h-main"},
		SourceHeadShort: "deadbee",
	}
	if got := EvaluateEdge(obs); got.SourceHeadShort != "deadbee" {
		t.Errorf("SourceHeadShort = %q, want %q", got.SourceHeadShort, "deadbee")
	}
}

// TestEvaluateEdgePassesThroughRequestAndMergeable confirms the advisory
// forge fields survive the ladder unchanged.
func TestEvaluateEdgePassesThroughRequestAndMergeable(t *testing.T) {
	t.Parallel()

	mergeable := true
	req := &ChangeRequest{ID: "7", Type: RequestTypePromotion, Source: "dev", Target: "test", SourceHead: "h-dev"}
	obs := EdgeObservation{
		From:           BranchState{Name: "dev", Head: "h-dev"},
		To:             BranchState{Name: "test", Head: "h-test"},
		Candidates:     commits("a"),
		ManagedRequest: req,
		Mergeable:      &mergeable,
	}

	got := EvaluateEdge(obs)
	if got.ManagedRequest != req {
		t.Errorf("ManagedRequest = %v, want %v", got.ManagedRequest, req)
	}
	if got.Mergeable != &mergeable {
		t.Errorf("Mergeable pointer not passed through")
	}
}

// TestEvaluateEdgeIsDeterministic asserts identical observations yield
// identical EdgeStates, the purity guarantee BuildPlan relies on.
func TestEvaluateEdgeIsDeterministic(t *testing.T) {
	t.Parallel()

	obs := EdgeObservation{
		From:                BranchState{Name: "dev", Head: "h-dev"},
		To:                  BranchState{Name: "test", Head: "h-test"},
		Candidates:          commits("a", "b"),
		CandidatePatchIDs:   patchIDs("a", "b"),
		DestinationPatchIDs: idSet("p-a"),
	}

	a := EvaluateEdge(obs)
	b := EvaluateEdge(obs)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("EvaluateEdge not deterministic:\n a = %+v\n b = %+v", a, b)
	}
}
