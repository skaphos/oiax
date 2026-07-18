package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/engine"
)

// Scenario tests (SKA-602) strengthen the determinism-as-concurrency and
// conflict-classification claims of ADR-0002/0004 against real git via the
// coordinator, rather than in prose or unit isolation. They deliberately reuse
// the fixtures in reconcile_test.go (gitHarness, commitOn, checkout, gitExec,
// conflictHarness, testGraph, fakeForge, oidHex) so a reader can see exactly
// which invariant each new case adds on top of the existing suite.

// TestScenarioSquashPromotionSettlesLadderViaHeadTree exercises the end-to-end
// ladder shape for a SQUASH merge (the remaining gap: rebase is covered by
// TestPlanPatchIdentityRungSettlesEdge and merge-commit by the merge-strategy
// suite). A squash promotes development's two feature commits into test as a
// SINGLE commit whose combined patch-id matches neither original: reachability
// cannot settle (both commits are source-only) and per-commit patch-identity
// cannot settle (the squash rewrote them into one), so only the head-tree rung
// — equal content despite rewritten SHAs — keeps the edge in sync (ADR-0002:
// divergence by content, not ancestry). A regression would emit a spurious
// promotion request after every squash merge.
func TestScenarioSquashPromotionSettlesLadderViaHeadTree(t *testing.T) {
	r, _ := gitHarness(t)

	// development: two distinct feature commits.
	checkout(t, r, "development")
	commitOn(t, r.Dir, "feature.txt", "feature\n", "add feature on development")
	commitOn(t, r.Dir, "more.txt", "more\n", "add more on development")

	// test: the same content squashed into ONE commit (distinct SHA, combined
	// patch-id). main tracks test so test->main is trivially in sync and the
	// plan isolates development->test.
	checkout(t, r, "test")
	if err := os.WriteFile(filepath.Join(r.Dir, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tSquash := commitOn(t, r.Dir, "more.txt", "more\n", "squash-merge development into test")
	gitExec(t, r.Dir, "branch", "-f", "main", tSquash)

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}

	// Pin the deciding rung: two source-only candidates, equal trees, and
	// neither candidate patch-id present on the destination.
	obs, err := c.observe(context.Background(), "development", "test", nil, nil)
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if len(obs.Candidates) != 2 {
		t.Fatalf("want 2 source-only candidates, got %d: %+v", len(obs.Candidates), obs.Candidates)
	}
	if !obs.TreesEqual {
		t.Fatal("squash produced unequal trees; the head-tree rung cannot settle the edge")
	}
	for _, cand := range obs.Candidates {
		pid, ok := obs.CandidatePatchIDs[cand.SHA]
		if !ok {
			t.Fatalf("candidate %s missing patch-id (merge commits have none); the case no longer isolates head-tree", cand.SHA)
		}
		if _, ok := obs.DestinationPatchIDs[pid]; ok {
			t.Fatalf("candidate %s patch-id unexpectedly present; the case no longer isolates head-tree", cand.SHA)
		}
	}
	if got := engine.EvaluateEdge(obs); got.Equivalence != engine.EquivalenceHeadTree || len(got.Unpromoted) != 0 {
		t.Fatalf("edge settled as %q with %d unpromoted, want head-tree with 0",
			got.Equivalence, len(got.Unpromoted))
	}

	// End-to-end: the plan must not propose promoting the already-squashed edge.
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for _, a := range plan.Actions {
		if a.From == "development" && a.To == "test" &&
			(a.Type == engine.ActionCreatePromotionRequest || a.Type == engine.ActionUpdateManagedRequest) {
			t.Errorf("head-tree should settle development->test, got spurious %+v", a)
		}
	}
}

// TestScenarioBackflowPushIsByteIdenticalAcrossIndependentRepos proves the
// ADR-0004 byte-identical-determinism claim at the backflow integration level:
// two runs observing identical heads produce byte-identical pushes and exactly
// one request each — never a churny force-push that re-triggers CI every
// reconcile. It is checked across INDEPENDENT object stores (two fresh repos),
// a stronger statement than same-repo idempotency
// (TestApplyBackflowSkipsUnchangedRepush): the replayed backflow head is a pure
// function of its inputs, not of the local clone.
func TestScenarioBackflowPushIsByteIdenticalAcrossIndependentRepos(t *testing.T) {
	run := func() (branch, sha string, creates int) {
		t.Helper()
		r, commit := gitHarness(t)
		checkout(t, r, "main")
		commit("hotfix.txt", "urgent\n", "hotfix on main")

		f := &fakeForge{createResult: engine.ChangeRequest{ID: "1", Type: engine.RequestTypeBackflow}}
		c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
		plan, err := c.Plan(context.Background())
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if _, err := c.Apply(context.Background(), plan); err != nil {
			t.Fatalf("apply: %v", err)
		}
		if len(f.pushed) != 1 {
			t.Fatalf("want exactly 1 push, got %d", len(f.pushed))
		}
		return f.pushed[0].Name, f.pushed[0].SHA, len(f.created)
	}

	branchA, shaA, createsA := run()
	branchB, shaB, createsB := run()

	if !oidHex.MatchString(shaA) {
		t.Fatalf("pushed SHA %q is not a full object id", shaA)
	}
	if shaA != shaB {
		t.Errorf("backflow push not byte-identical across independent repos: %q vs %q", shaA, shaB)
	}
	if branchA != branchB {
		t.Errorf("deterministic branch name differs across repos: %q vs %q", branchA, branchB)
	}
	if createsA != 1 || createsB != 1 {
		t.Errorf("want exactly one request per run, got %d and %d", createsA, createsB)
	}
}

// TestScenarioCherryPickConflictOnNthCandidateReportsPartialCleanCount covers
// the conflict matrix's "Nth candidate" case: a multi-commit backflow set whose
// FIRST commit applies cleanly and whose SECOND conflicts. Apply must still
// report divergence (exit 3, unchanged), push and create nothing, and open one
// artifact whose body records that one commit applied cleanly before the
// conflict — distinct from conflictHarness, where the very first candidate
// conflicts and the clean count is zero.
func TestScenarioCherryPickConflictOnNthCandidateReportsPartialCleanCount(t *testing.T) {
	r, commit := gitHarness(t)

	// development diverges app.txt so the second returned commit cannot apply.
	checkout(t, r, "development")
	commitOn(t, r.Dir, "app.txt", "dev-change\n", "dev edit")

	// main gains two downstream-only commits: a clean new file (older), then a
	// conflicting edit to app.txt (newer). Cherry-pick replays oldest-first, so
	// the new file applies cleanly before the edit conflicts.
	checkout(t, r, "main")
	commit("newfile.txt", "new\n", "clean hotfix on main")
	commit("app.txt", "main-hotfix\n", "conflicting hotfix on main")

	short, err := r.ShortSHA(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	a := engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
		Unpromoted: 2, Branch: engine.BackflowBranchName("main", "development", short),
	}
	f := &fakeForge{}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Divergence || res.Conflicts != 1 {
		t.Errorf("result = %+v, want Divergence=true Conflicts=1", res)
	}
	if len(f.pushed) != 0 || len(f.created) != 0 {
		t.Errorf("a conflict must push and create nothing: pushed=%d created=%d", len(f.pushed), len(f.created))
	}
	if len(f.conflictCreated) != 1 || !strings.Contains(f.conflictCreated[0].Body, "applied 1 commit(s) cleanly") {
		t.Errorf("created body = %q, want it to report 1 clean commit before the conflict", f.conflictCreated)
	}
}

// TestScenarioBackflowMixedDropAndApplyConverges covers the conflict matrix's
// `--empty=drop` mid-sequence case: a backflow set where one returned commit's
// content already reached the target (its pick reduces to an empty diff) while
// another is genuinely new. Cherry-pick must drop the redundant commit, apply
// the new one, and push a single fresh commit — never treat the drop as a
// conflict or lose the new content. This complements
// TestApplyBackflowAllCommitsDropConverges, where EVERY commit drops and the
// edge converges with no push.
func TestScenarioBackflowMixedDropAndApplyConverges(t *testing.T) {
	r, commit := gitHarness(t)

	// development already carries the hotfix.txt content, but combined with an
	// unrelated file in ONE commit, so its patch-id differs from main's
	// single-file hotfix and the ladder does not pre-exclude that commit from
	// the returned set (it is dropped only at replay time, as an empty diff).
	checkout(t, r, "development")
	if err := os.WriteFile(filepath.Join(r.Dir, "other.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit("hotfix.txt", "urgent\n", "development already carries the hotfix content")

	// main: the redundant hotfix (older, drops on replay), then a genuinely new
	// file (newer, applies).
	checkout(t, r, "main")
	commit("hotfix.txt", "urgent\n", "hotfix on main (already on development)")
	commit("newfile.txt", "fresh\n", "genuinely new hotfix on main")

	short, err := r.ShortSHA(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	a := engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
		Unpromoted: 2, Branch: engine.BackflowBranchName("main", "development", short),
	}
	f := &fakeForge{createResult: engine.ChangeRequest{ID: "1", Type: engine.RequestTypeBackflow}}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Applied != 1 || res.Divergence {
		t.Errorf("result = %+v, want Applied=1 Divergence=false (converged with one surviving commit)", res)
	}
	if len(f.pushed) != 1 {
		t.Fatalf("want exactly 1 push (the surviving new commit), got %d", len(f.pushed))
	}
	developmentHead, _ := r.Head(context.Background(), "development")
	if f.pushed[0].SHA == developmentHead || !oidHex.MatchString(f.pushed[0].SHA) {
		t.Errorf("pushed SHA %q must be a fresh replayed commit distinct from the target head", f.pushed[0].SHA)
	}
	if len(f.created) != 1 || f.created[0].SourceHead != f.pushed[0].SHA {
		t.Errorf("want one request referencing the pushed head, got created=%+v", f.created)
	}
}

// TestScenarioOperationalFailureIsErrorNotDivergence covers the conflict
// matrix's operational-vs-content distinction at the coordinator level: a
// cancelled context is an OPERATIONAL failure, so Apply must return an error
// and leave Divergence false — never map it to exit 3, and never open a
// conflict artifact. Contrast the content-conflict path
// (TestApplyBackflowConflictReportsDivergence), where the same edge yields
// Divergence=true with no error. The git-layer discrimination is pinned by
// TestCherryPickCancelledContextIsOperationalError /
// TestMergeCancelledContextIsOperationalError.
func TestScenarioOperationalFailureIsErrorNotDivergence(t *testing.T) {
	r, _, a := conflictHarness(t)
	f := &fakeForge{}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, err := c.Apply(ctx, engine.Plan{Actions: []engine.Action{a}})
	if err == nil {
		t.Fatal("Apply on a cancelled context returned nil, want an operational error")
	}
	if res.Divergence {
		t.Error("a cancelled context must not be reported as content divergence (exit 3)")
	}
	if res.Conflicts != 0 || len(f.conflictCreated) != 0 {
		t.Errorf("an operational failure must open no conflict artifact: Conflicts=%d created=%d",
			res.Conflicts, len(f.conflictCreated))
	}
}
