package reconcile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/git"
	"github.com/skaphos/oiax/internal/gittest"
	"github.com/skaphos/oiax/internal/tmpl"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

// oidHex matches a full git object id, for asserting a pushed commit SHA.
var oidHex = regexp.MustCompile(`^[0-9a-f]{40}$`)

// fakeForge records mutations and serves canned managed-request lists.
type fakeForge struct {
	open   []engine.ChangeRequest
	merged []engine.ChangeRequest

	created []forge.CreateRequest
	updated []forge.UpdateRequest
	closed  []forge.RequestID
	pushed  []forge.BranchPush
	deleted []string

	createResult engine.ChangeRequest
	createErr    error
	deleteErr    error

	mergeMethods      *forge.MergeMethods
	mergeMethodsErr   error
	mergeMethodsCalls int

	targetMergeMethods      *forge.MergeMethods
	targetMergeMethodsErr   error
	targetMergeMethodsCalls int
	targetMergeBranch       string

	deletesSourceOnMerge      bool
	deletesSourceOnMergeErr   error
	deletesSourceOnMergeCalls int

	// Conflict-artifact store (SKA-601): a real in-memory ordered set that
	// mimics the forge's ascending-by-issue-number list contract, so the
	// record/adopt/advance/consolidate/close state machine and the sweep can be
	// asserted. List returns a copy sorted ascending; Create appends with the
	// next number; Update rewrites the stored fields; Close removes from the
	// open store and records the closed id (resolve/consolidate/orphan).
	conflicts       []forge.ConflictArtifact
	nextConflictNum int
	conflictCreated []forge.ConflictArtifactSpec
	conflictUpdated []forge.ConflictArtifactSpec
	conflictClosed  []forge.ConflictArtifactID

	listConflictsErr  error
	createConflictErr error
	updateConflictErr error
	closeConflictErr  error
}

func (f *fakeForge) RepoMergeMethods(context.Context) (forge.MergeMethods, error) {
	f.mergeMethodsCalls++
	if f.mergeMethodsErr != nil {
		return forge.MergeMethods{}, f.mergeMethodsErr
	}
	if f.mergeMethods != nil {
		return *f.mergeMethods, nil
	}
	return forge.MergeMethods{Merge: true, Squash: true, Rebase: true}, nil
}

func (f *fakeForge) RepoDeletesSourceOnMerge(context.Context) (bool, error) {
	f.deletesSourceOnMergeCalls++
	if f.deletesSourceOnMergeErr != nil {
		return false, f.deletesSourceOnMergeErr
	}
	return f.deletesSourceOnMerge, nil
}

func (f *fakeForge) TargetMergeMethods(_ context.Context, branch string) (forge.MergeMethods, error) {
	f.targetMergeMethodsCalls++
	f.targetMergeBranch = branch
	if f.targetMergeMethodsErr != nil {
		return forge.MergeMethods{}, f.targetMergeMethodsErr
	}
	if f.targetMergeMethods != nil {
		return *f.targetMergeMethods, nil
	}
	return forge.MergeMethods{Merge: true, Squash: true, Rebase: true}, nil
}

func (f *fakeForge) ListManagedRequests(_ context.Context, filter forge.RequestFilter) ([]engine.ChangeRequest, error) {
	if filter.State == forge.RequestStateMerged {
		return f.merged, nil
	}
	return f.open, nil
}

// TargetMergeMethods / MergeCommitAllowed have no reconcile-package production
// caller yet — the backflow merge-method fence is wired in a later slice. The
// forge-side read and the MergeCommitAllowed() predicate are pinned directly in
// internal/forge/github (TestTargetMergeMethods) and internal/forge
// (MergeMethods), so there is deliberately no reconcile test here that would
// only exercise the fakeForge echo; one lands with the fence itself.

func (f *fakeForge) CreateRequest(_ context.Context, req forge.CreateRequest) (engine.ChangeRequest, error) {
	f.created = append(f.created, req)
	if f.createErr != nil {
		return engine.ChangeRequest{}, f.createErr
	}
	return f.createResult, nil
}

func (f *fakeForge) UpdateRequest(_ context.Context, req forge.UpdateRequest) error {
	f.updated = append(f.updated, req)
	return nil
}

func (f *fakeForge) CloseRequest(_ context.Context, id forge.RequestID, _ forge.Reason) error {
	f.closed = append(f.closed, id)
	return nil
}

func (f *fakeForge) PushBranch(_ context.Context, push forge.BranchPush) error {
	f.pushed = append(f.pushed, push)
	return nil
}

func (f *fakeForge) DeleteBranch(_ context.Context, name string) error {
	f.deleted = append(f.deleted, name)
	return f.deleteErr
}

// seedConflict inserts a pre-existing open conflict artifact into the store and
// keeps the next-number counter ahead of any seeded id, so a later Create does
// not collide with a seeded number.
func (f *fakeForge) seedConflict(id, source, target, sourceHead string) {
	f.conflicts = append(f.conflicts, forge.ConflictArtifact{
		ID: forge.ConflictArtifactID(id), Source: source, Target: target, SourceHead: sourceHead,
	})
	if n, err := strconv.Atoi(id); err == nil && n > f.nextConflictNum {
		f.nextConflictNum = n
	}
}

// ListConflictArtifacts returns a copy of the open store sorted ascending by
// issue number — the provider contract the canonical-lowest rule depends on.
func (f *fakeForge) ListConflictArtifacts(_ context.Context, _ string) ([]forge.ConflictArtifact, error) {
	if f.listConflictsErr != nil {
		return nil, f.listConflictsErr
	}
	out := make([]forge.ConflictArtifact, len(f.conflicts))
	copy(out, f.conflicts)
	sort.Slice(out, func(i, j int) bool {
		ni, _ := strconv.Atoi(string(out[i].ID))
		nj, _ := strconv.Atoi(string(out[j].ID))
		return ni < nj
	})
	return out, nil
}

func (f *fakeForge) CreateConflictArtifact(_ context.Context, spec forge.ConflictArtifactSpec) (forge.ConflictArtifact, error) {
	f.conflictCreated = append(f.conflictCreated, spec)
	if f.createConflictErr != nil {
		return forge.ConflictArtifact{}, f.createConflictErr
	}
	f.nextConflictNum++
	art := forge.ConflictArtifact{
		ID:         forge.ConflictArtifactID(strconv.Itoa(f.nextConflictNum)),
		Source:     spec.Source,
		Target:     spec.Target,
		SourceHead: spec.SourceHead,
	}
	f.conflicts = append(f.conflicts, art)
	return art, nil
}

func (f *fakeForge) UpdateConflictArtifact(_ context.Context, id forge.ConflictArtifactID, spec forge.ConflictArtifactSpec) error {
	f.conflictUpdated = append(f.conflictUpdated, spec)
	if f.updateConflictErr != nil {
		return f.updateConflictErr
	}
	for i := range f.conflicts {
		if f.conflicts[i].ID == id {
			f.conflicts[i].Source = spec.Source
			f.conflicts[i].Target = spec.Target
			f.conflicts[i].SourceHead = spec.SourceHead
			return nil
		}
	}
	return fmt.Errorf("update: no conflict artifact %s", id)
}

func (f *fakeForge) CloseConflictArtifact(_ context.Context, id forge.ConflictArtifactID, _ forge.Reason) error {
	if f.closeConflictErr != nil {
		return f.closeConflictErr
	}
	f.conflictClosed = append(f.conflictClosed, id)
	for i := range f.conflicts {
		if f.conflicts[i].ID == id {
			f.conflicts = append(f.conflicts[:i], f.conflicts[i+1:]...)
			return nil
		}
	}
	return nil
}

// testGraph is the three-branch graph the tests plan against.
func testGraph() *engine.Graph {
	return &engine.Graph{
		Name: "environments",
		Branches: map[string]engine.Branch{
			"development": {Role: v1.RoleSource, Drift: v1.DriftForbidden},
			"test":        {Drift: v1.DriftForbidden},
			"main":        {Role: v1.RoleTerminal, Drift: v1.DriftForbidden},
		},
		Promotions: []engine.Promotion{
			{From: "development", To: "test"},
			{From: "test", To: "main"},
		},
		Backflow: engine.BackflowPolicy{
			Enabled:  true,
			Sources:  []string{"main"},
			Target:   "development",
			Strategy: v1.BackflowStrategyCherryPick,
		},
	}
}

// gitHarness spins up a real repository so the coordinator observes actual
// git state. It returns the runner and a commit helper.
func gitHarness(t *testing.T) (*git.Runner, func(file, content, msg string) string) {
	t.Helper()
	dir := t.TempDir()
	gittest.InitRepo(t, dir)

	commit := func(file, content, msg string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		gittest.Run(t, dir, "add", ".")
		gittest.Run(t, dir, "commit", "-q", "-m", msg)
		return gittest.Run(t, dir, "rev-parse", "HEAD")
	}

	// Base commit shared by all three branches.
	commit("app.txt", "v0\n", "c0")
	gittest.Run(t, dir, "branch", "development")
	gittest.Run(t, dir, "branch", "test")

	// Expose the raw runner over the same directory.
	return &git.Runner{Dir: dir}, func(file, content, msg string) string {
		t.Helper()
		return commitOn(t, dir, file, content, msg)
	}
}

// commitOn commits on the currently checked-out branch of dir.
func commitOn(t *testing.T, dir, file, content, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", ".")
	gittest.Run(t, dir, "commit", "-q", "-m", msg)
	return gittest.Run(t, dir, "rev-parse", "HEAD")
}

func checkout(t *testing.T, r *git.Runner, branch string) {
	t.Helper()
	gittest.Run(t, r.Dir, "checkout", "-q", branch)
}

// gitExec runs a raw git command in dir, failing the test on error.
func gitExec(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return gittest.Run(t, dir, args...)
}

func TestPlanInSync(t *testing.T) {
	r, _ := gitHarness(t)
	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}

	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("in-sync plan has %d actions, want 0: %+v", len(plan.Actions), plan.Actions)
	}
	if plan.PlanFormatVersion != engine.PlanFormatVersion || plan.Graph != "environments" {
		t.Errorf("plan header = %+v", plan)
	}
}

// TestPlanMultiBranchGraphWithOriginOnlyBranches reproduces the
// actions/checkout condition end-to-end: only the triggering branch is a local
// head; the other graph branches exist solely as refs/remotes/origin/<name>. A
// fully in-sync multi-branch graph must observe cleanly and plan zero actions.
// Before the git layer resolved origin-tracking refs, observe() failed on
// Head() of a non-triggering branch and reconcile exited 1 on its first run.
func TestPlanMultiBranchGraphWithOriginOnlyBranches(t *testing.T) {
	r, _ := gitHarness(t)
	dir := r.Dir

	// development is the triggering branch (a local head); test and main exist
	// only as remote-tracking refs, their local heads removed.
	checkout(t, r, "development")
	for _, b := range []string{"test", "main"} {
		sha := gitExec(t, dir, "rev-parse", b)
		gitExec(t, dir, "update-ref", "refs/remotes/origin/"+b, sha)
		gitExec(t, dir, "branch", "-D", b)
	}

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan over origin-only branches: %v", err)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("in-sync plan has %d actions, want 0: %+v", len(plan.Actions), plan.Actions)
	}
}

func TestPlanCreatePromotionRequest(t *testing.T) {
	r, _ := gitHarness(t)
	checkout(t, r, "development")
	commitOn(t, r.Dir, "app.txt", "v1\n", "c1 on development")

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("want 1 action, got %d: %+v", len(plan.Actions), plan.Actions)
	}
	a := plan.Actions[0]
	if a.Type != engine.ActionCreatePromotionRequest || a.From != "development" || a.To != "test" {
		t.Errorf("action = %+v", a)
	}
	if a.Unpromoted != 1 {
		t.Errorf("unpromoted = %d, want 1", a.Unpromoted)
	}
}

func TestPlanBaselineRungSettlesEdge(t *testing.T) {
	// development advanced to C1 with content the destination never received
	// by ancestry, patch identity, or tree equality; only the merged
	// request's recorded sourceHead promotes it.
	r, _ := gitHarness(t)
	checkout(t, r, "development")
	c1 := commitOn(t, r.Dir, "app.txt", "v1-divergent\n", "c1 on development")

	graph := testGraph()
	f := &fakeForge{
		merged: []engine.ChangeRequest{{
			ID: "7", Type: engine.RequestTypePromotion,
			Source: "development", Target: "test", SourceHead: c1,
		}},
	}
	c := &Coordinator{Git: r, Forge: f, Graph: graph}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	// development->test is settled by the baseline; test->main is in sync.
	for _, a := range plan.Actions {
		if a.From == "development" && a.To == "test" {
			t.Errorf("baseline should settle development->test, got action %+v", a)
		}
	}
}

func TestPlanPatchIdentityRungSettlesEdge(t *testing.T) {
	// A rebase-merge scenario: development's feature commit was already
	// promoted into test as a distinct commit with the same diff (identical
	// stable patch-id, different SHA), and test then advanced. The candidate
	// is reachable-only on the source (reachability cannot settle) and the
	// head trees differ (head-tree cannot settle), so only the patch-identity
	// rung — assembled by the coordinator's destination patch-id set — can
	// keep the edge in sync. This exercises the destPatch wiring in observe()
	// end-to-end: a regression keying it on commit SHA instead of patch-id, or
	// passing the wrong range to PatchIDs, would fail to settle the edge and
	// emit a spurious promotion request.
	r, _ := gitHarness(t)

	// test = c0 -> feature -> extra.
	checkout(t, r, "test")
	commitOn(t, r.Dir, "feature.txt", "feature\n", "add feature on test")
	tExtra := commitOn(t, r.Dir, "extra.txt", "extra\n", "add extra on test")
	// main tracks test so the test->main edge is trivially in sync and the
	// plan isolates development->test.
	gitExec(t, r.Dir, "branch", "-f", "main", tExtra)

	// development carries the same feature diff as a distinct commit: same
	// stable patch-id as test's copy, different SHA. Its tree lacks extra.txt,
	// so tree(development) != tree(test).
	checkout(t, r, "development")
	commitOn(t, r.Dir, "feature.txt", "feature\n", "add feature on development")

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}

	// Observe the edge directly to pin the deciding rung. The coordinator must
	// build a non-empty destination patch-id set keyed by patch-id (not SHA),
	// the candidate's patch-id must be present in it, and the trees must
	// differ so head-tree cannot mask a mis-keyed set.
	obs, err := c.observe(context.Background(), "development", "test", nil, nil)
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if len(obs.Candidates) != 1 {
		t.Fatalf("want 1 candidate on the source, got %d: %+v", len(obs.Candidates), obs.Candidates)
	}
	if obs.TreesEqual {
		t.Fatal("trees are equal; head-tree would mask a patch-identity regression")
	}
	if len(obs.DestinationPatchIDs) == 0 {
		t.Fatal("destination patch-id set is empty; the patch-identity rung cannot settle the edge")
	}
	candPID := obs.CandidatePatchIDs[obs.Candidates[0].SHA]
	if candPID == "" {
		t.Fatal("candidate has no patch-id")
	}
	if _, ok := obs.DestinationPatchIDs[candPID]; !ok {
		t.Fatalf("candidate patch-id %q absent from destination set %v", candPID, obs.DestinationPatchIDs)
	}
	if got := engine.EvaluateEdge(obs); got.Equivalence != engine.EquivalencePatchIdentity || len(got.Unpromoted) != 0 {
		t.Fatalf("edge settled as %q with %d unpromoted, want patch-identity with 0",
			got.Equivalence, len(got.Unpromoted))
	}

	// End-to-end: the plan must not propose promoting the already-rebased edge.
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for _, a := range plan.Actions {
		if a.From == "development" && a.To == "test" &&
			(a.Type == engine.ActionCreatePromotionRequest || a.Type == engine.ActionUpdateManagedRequest) {
			t.Errorf("patch-identity should settle development->test, got spurious %+v", a)
		}
	}
}

func TestPlanReportsDownstreamDivergence(t *testing.T) {
	// test gains a commit not present on development; development->test reports
	// divergence (test is drift-forbidden and NOT a backflow source, so it is
	// not returned by backflow). A backflow source in test->main's position
	// would backflow instead — covered by the backflow tests.
	r, _ := gitHarness(t)
	checkout(t, r, "test")
	commitOn(t, r.Dir, "drift.txt", "drift\n", "drift on test")

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	var reports int
	for _, a := range plan.Actions {
		if a.Type == engine.ActionReportDivergence && a.From == "development" && a.To == "test" {
			reports++
		}
	}
	if reports != 1 {
		t.Fatalf("want 1 report-divergence for development->test, got %d: %+v", reports, plan.Actions)
	}
}

// countReports tallies ActionReportDivergence actions for one edge and
// returns the count plus the last matching action for detail assertions.
func countReports(plan engine.Plan, from, to string) (int, engine.Action) {
	var n int
	var last engine.Action
	for _, a := range plan.Actions {
		if a.Type == engine.ActionReportDivergence && a.From == from && a.To == to {
			n++
			last = a
		}
	}
	return n, last
}

// TestPlanReportClearsBenignMergeResidue is the ADR-0002 Amendment 1
// migration scenario from issue #53: a repository moving off a hand-made
// merge-commit promotion process carries historical promotion merges on the
// destination that the source has since advanced past. The residue is content
// already represented upstream, so the report must clear it (exit 0), not
// surface a spurious divergence — and an empty commit is equally contentless.
func TestPlanReportClearsBenignMergeResidue(t *testing.T) {
	r, _ := gitHarness(t)

	// development advances to c1; a hand-made promotion merges it into test.
	checkout(t, r, "development")
	commitOn(t, r.Dir, "app.txt", "v1\n", "c1 on development")
	checkout(t, r, "test")
	gitExec(t, r.Dir, "merge", "-q", "--no-ff", "-m", "merge: Promote v1 into test", "development")
	gitExec(t, r.Dir, "commit", "-q", "--allow-empty", "-m", "empty ceremony commit")
	// development then advances past the promoted version.
	checkout(t, r, "development")
	commitOn(t, r.Dir, "app.txt", "v2\n", "c2 on development")

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if reports, a := countReports(plan, "development", "test"); reports != 0 {
		t.Fatalf("benign merge residue reported as divergence: %+v", a)
	}
	// The genuine forward promotion (c2) must still be planned — clearing the
	// report must not mask promotion detection.
	var promotions int
	for _, a := range plan.Actions {
		if a.Type == engine.ActionCreatePromotionRequest && a.From == "development" && a.To == "test" {
			promotions++
		}
	}
	if promotions != 1 {
		t.Fatalf("want 1 promotion request for development->test, got %d: %+v", promotions, plan.Actions)
	}
}

// TestPlanReportKeepsEvilMerge: a merge whose tree the mechanical re-merge of
// its parents does not produce (-s ours drops the source's content) carries
// real downstream content, so the report must keep it even though --no-merges
// alone would hide it.
func TestPlanReportKeepsEvilMerge(t *testing.T) {
	r, _ := gitHarness(t)

	checkout(t, r, "development")
	commitOn(t, r.Dir, "app.txt", "v1\n", "c1 on development")
	// test records a merge of development that discards its content: an evil
	// merge — reachability says promoted, the tree says otherwise.
	checkout(t, r, "test")
	gitExec(t, r.Dir, "merge", "-q", "-s", "ours", "--no-ff", "-m", "ours-merge development", "development")

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	reports, a := countReports(plan, "development", "test")
	if reports != 1 {
		t.Fatalf("want 1 report-divergence for the evil merge, got %d: %+v", reports, plan.Actions)
	}
	if a.Unpromoted != 1 {
		t.Errorf("reported count = %d, want 1 (the evil merge)", a.Unpromoted)
	}
}

// TestPlanReportClearsReturnedPatchID: a hotfix commit on the destination
// whose identical diff already exists on the source (returned by rebase or
// cherry-pick before the source advanced) is represented by content and must
// not be reported.
func TestPlanReportClearsReturnedPatchID(t *testing.T) {
	r, _ := gitHarness(t)

	checkout(t, r, "test")
	commitOn(t, r.Dir, "hotfix.txt", "h\n", "hotfix on test")
	// The same diff lands on development as a distinct commit, then
	// development advances.
	checkout(t, r, "development")
	commitOn(t, r.Dir, "hotfix.txt", "h\n", "hotfix returned to development")
	commitOn(t, r.Dir, "app.txt", "v1\n", "c1 on development")

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if reports, a := countReports(plan, "development", "test"); reports != 0 {
		t.Fatalf("content-returned hotfix reported as divergence: %+v", a)
	}
}

// TestPlanReportMixedResidueCountsOnlyDrift: benign merge residue and one
// genuine downstream-only commit coexist; the report survives but counts only
// the genuine drift, so the operator sees the real number, not the residue.
func TestPlanReportMixedResidueCountsOnlyDrift(t *testing.T) {
	r, _ := gitHarness(t)

	checkout(t, r, "development")
	commitOn(t, r.Dir, "app.txt", "v1\n", "c1 on development")
	checkout(t, r, "test")
	gitExec(t, r.Dir, "merge", "-q", "--no-ff", "-m", "merge: Promote v1 into test", "development")
	commitOn(t, r.Dir, "drift.txt", "drift\n", "genuine drift on test")
	checkout(t, r, "development")
	commitOn(t, r.Dir, "app.txt", "v2\n", "c2 on development")

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	reports, a := countReports(plan, "development", "test")
	if reports != 1 {
		t.Fatalf("want 1 report-divergence for development->test, got %d: %+v", reports, plan.Actions)
	}
	if a.Unpromoted != 1 {
		t.Errorf("reported count = %d, want 1 (residue must not inflate the count)", a.Unpromoted)
	}
}

func TestApplyCreateResolvesLiveHead(t *testing.T) {
	r, _ := gitHarness(t)
	checkout(t, r, "development")
	head := commitOn(t, r.Dir, "app.txt", "v1\n", "c1 on development")

	f := &fakeForge{createResult: engine.ChangeRequest{ID: "1"}}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	res, err := c.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Applied != 1 || res.Divergence {
		t.Errorf("result = %+v", res)
	}
	if len(f.created) != 1 {
		t.Fatalf("want 1 create, got %d", len(f.created))
	}
	got := f.created[0]
	if got.Source != "development" || got.Target != "test" || got.SourceHead != head {
		t.Errorf("create = %+v, want source=development target=test sourceHead=%s", got, head)
	}
	if got.Graph != "environments" || got.Type != engine.RequestTypePromotion {
		t.Errorf("create metadata = %+v", got)
	}
}

func TestApplyUpdateUsesLiveHead(t *testing.T) {
	r, _ := gitHarness(t)
	checkout(t, r, "development")
	head := commitOn(t, r.Dir, "app.txt", "v1\n", "c1 on development")

	// An open managed request records a stale sourceHead, so the plan wants
	// an update to the live head.
	f := &fakeForge{
		open: []engine.ChangeRequest{{
			ID: "42", Type: engine.RequestTypePromotion,
			Source: "development", Target: "test", SourceHead: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		}},
	}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := c.Apply(context.Background(), plan); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(f.updated) != 1 {
		t.Fatalf("want 1 update, got %d", len(f.updated))
	}
	if f.updated[0].ID != "42" || f.updated[0].SourceHead != head {
		t.Errorf("update = %+v, want ID=42 sourceHead=%s", f.updated[0], head)
	}
}

func TestApplyCloseObsoleteRequest(t *testing.T) {
	// Edge in sync (all branches at C0) but an open managed request exists →
	// close it as obsolete.
	r, _ := gitHarness(t)
	f := &fakeForge{
		open: []engine.ChangeRequest{{
			ID: "9", Type: engine.RequestTypePromotion,
			Source: "development", Target: "test", SourceHead: "cafe",
		}},
	}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := c.Apply(context.Background(), plan); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(f.closed) != 1 || f.closed[0] != "9" {
		t.Errorf("closed = %v, want [9]", f.closed)
	}
}

func TestApplyReportDivergenceSetsResultWithoutForgeCall(t *testing.T) {
	// The report-divergence arm is git-independent: it flips Divergence and
	// touches no forge. Feed it a single manual action to isolate the arm.
	f := &fakeForge{}
	c := &Coordinator{Git: &git.Runner{}, Forge: f, Graph: testGraph()}
	plan := engine.Plan{Actions: []engine.Action{{
		Type: engine.ActionReportDivergence, From: "development", To: "test", Reason: "drift",
	}}}
	res, err := c.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Divergence {
		t.Error("want Divergence true")
	}
	if len(f.created)+len(f.updated)+len(f.closed)+len(f.pushed) != 0 {
		t.Error("report-divergence must not call the forge")
	}
}

func TestApplyExecutesBackflowAction(t *testing.T) {
	// A hotfix lands on main (a backflow source) but not on development (the
	// backflow target). Apply must cherry-pick it back, force-push the
	// deterministic branch, and open a managed backflow request.
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	commit("hotfix.txt", "urgent\n", "hotfix on main")

	f := &fakeForge{createResult: engine.ChangeRequest{ID: "1", Type: engine.RequestTypeBackflow}}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// The plan carries exactly the backflow action (all promotion edges in
	// sync).
	if len(plan.Actions) != 1 || plan.Actions[0].Type != engine.ActionCreateBackflowRequest {
		t.Fatalf("plan actions = %+v, want one backflow action", plan.Actions)
	}

	res, err := c.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Applied != 1 || res.Divergence {
		t.Errorf("result = %+v, want Applied=1 Divergence=false", res)
	}

	short, err := r.ShortSHA(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	wantBranch := engine.BackflowBranchName("main", "development", short)

	if len(f.pushed) != 1 {
		t.Fatalf("want 1 push, got %d", len(f.pushed))
	}
	if f.pushed[0].Name != wantBranch || !f.pushed[0].Force {
		t.Errorf("push = %+v, want name=%s force=true", f.pushed[0], wantBranch)
	}
	mainHead, _ := r.Head(context.Background(), "main")
	if !oidHex.MatchString(f.pushed[0].SHA) || f.pushed[0].SHA == mainHead {
		t.Errorf("pushed SHA = %q, want a fresh cherry-picked commit distinct from main head", f.pushed[0].SHA)
	}

	if len(f.created) != 1 {
		t.Fatalf("want 1 create, got %d", len(f.created))
	}
	got := f.created[0]
	if got.Type != engine.RequestTypeBackflow || got.Target != "development" || got.Source != wantBranch {
		t.Errorf("create = %+v, want backflow head=%s base=development", got, wantBranch)
	}
	if got.Graph != "environments" || got.SourceHead != f.pushed[0].SHA {
		t.Errorf("create metadata = %+v", got)
	}
}

func TestApplyBackflowConflictReportsDivergence(t *testing.T) {
	// development and main touch the same file with different content, so the
	// hotfix cannot cherry-pick cleanly onto development. The backflow becomes
	// a reported divergence: no push, no request, worktree cleaned up.
	r, commit := gitHarness(t)
	checkout(t, r, "development")
	commit("app.txt", "dev-change\n", "dev edit")
	checkout(t, r, "main")
	commit("app.txt", "hotfix-change\n", "hotfix on main")

	short, err := r.ShortSHA(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	// Drive the backflow arm directly with the deterministic action so the
	// test isolates the conflict path from the promotion actions the diverged
	// development branch would otherwise produce.
	a := engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
		Unpromoted: 1, Branch: engine.BackflowBranchName("main", "development", short),
	}
	f := &fakeForge{}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Divergence {
		t.Error("want Divergence true on cherry-pick conflict")
	}
	if res.Applied != 0 {
		t.Errorf("Applied = %d, want 0 on conflict", res.Applied)
	}
	if len(f.pushed) != 0 || len(f.created) != 0 {
		t.Errorf("conflict must push nothing and create nothing: pushed=%d created=%d", len(f.pushed), len(f.created))
	}
}

func TestPlanBackflowAlreadyReturnedByContent(t *testing.T) {
	// The hotfix on main was already cherry-picked onto development (same
	// diff, distinct SHA). Its patch-id is present on the target, so ToReturn
	// is empty and the plan proposes no backflow action. development gains an
	// unrelated commit first so the cherry-pick lands on a distinct parent and
	// yields a distinct SHA (isolating the content/patch-id rung — no
	// cherry-pick -x provenance is used).
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	hotfix := commit("hotfix.txt", "urgent\n", "hotfix on main")
	checkout(t, r, "development")
	commit("dev.txt", "dev work\n", "unrelated dev commit")
	gitExec(t, r.Dir, "cherry-pick", hotfix)

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for _, a := range plan.Actions {
		if a.Type == engine.ActionCreateBackflowRequest {
			t.Errorf("already-returned hotfix must not backflow, got %+v", a)
		}
	}
	// The edge diagnostics name the excluding rung: returned by content.
	assertExclusionReason(t, plan, hotfix, engine.BackflowExcludedPatchID)
}

// assertExclusionReason finds the backflow exclusion for sha in the plan's
// edge diagnostics and asserts the rung that excluded it.
func assertExclusionReason(t *testing.T, plan engine.Plan, sha string, want engine.BackflowExclusionReason) {
	t.Helper()
	for _, e := range plan.Edges {
		for _, x := range e.Excluded {
			if x.SHA == sha {
				if x.Reason != want {
					t.Errorf("exclusion reason for %s = %q, want %q", sha, x.Reason, want)
				}
				return
			}
		}
	}
	t.Errorf("no exclusion recorded for %s in edges %+v", sha, plan.Edges)
}

// findLogRecord scans a JSON-handler log buffer for the record with the given
// msg and returns it decoded; it fails the test when no such record exists.
func findLogRecord(t *testing.T, buf *bytes.Buffer, msg string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %v\n%s", err, line)
		}
		if rec["msg"] == msg {
			return rec
		}
	}
	t.Fatalf("no %q record in logs:\n%s", msg, buf.String())
	return nil
}

// TestPlanEmitsPlanBuiltRecord asserts the per-run observability record: one
// "plan built" structured line with the edge, rung, and backflow counts.
func TestPlanEmitsPlanBuiltRecord(t *testing.T) {
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	commit("hotfix.txt", "urgent\n", "hotfix on main")

	var buf bytes.Buffer
	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph(),
		Log: slog.New(slog.NewJSONHandler(&buf, nil))}
	if _, err := c.Plan(context.Background()); err != nil {
		t.Fatalf("plan: %v", err)
	}

	rec := findLogRecord(t, &buf, "plan built")
	if got := rec["edges"]; got != float64(2) {
		t.Errorf("edges = %v, want 2", got)
	}
	settled, _ := rec["settledBy"].(map[string]any)
	if settled["reachability"] != float64(2) {
		t.Errorf("settledBy = %v, want reachability=2", rec["settledBy"])
	}
	backflow, _ := rec["backflow"].(map[string]any)
	if backflow["toReturn"] != float64(1) {
		t.Errorf("backflow = %v, want toReturn=1 (the hotfix)", rec["backflow"])
	}
}

// TestLogPlanCountsDedupesFanInBackflow guards the union semantics of the
// run-level backflow tallies: a backflow source with several incoming
// promotion edges carries the SAME downstream commit in each edge's view, and
// apply returns the union — so the "plan built" record must count each SHA
// once, not once per edge. A non-source destination's stale ToReturn view
// must not be counted at all.
func TestLogPlanCountsDedupesFanInBackflow(t *testing.T) {
	g := testGraph()
	hotfix := engine.Commit{SHA: "h1", Subject: "hotfix"}
	skipped := engine.BackflowExclusion{SHA: "s1", Subject: "skipped", Reason: engine.BackflowExcludedSkip}
	intoMain := func(from string) engine.EdgeState {
		return engine.EdgeState{
			From:           engine.BranchState{Name: from, Head: "h-" + from},
			To:             engine.BranchState{Name: "main", Head: "h-main"},
			Equivalence:    engine.EquivalenceReachability,
			DownstreamOnly: []engine.Commit{hotfix, {SHA: "s1", Subject: "skipped"}},
			ToReturn:       []engine.Commit{hotfix},
			Excluded:       []engine.BackflowExclusion{skipped},
		}
	}
	// A destination that is NOT a backflow source, with the degenerate
	// ToReturn view EvaluateEdge produces there: must not reach the tallies.
	nonSource := engine.EdgeState{
		From:           engine.BranchState{Name: "development", Head: "h-development"},
		To:             engine.BranchState{Name: "test", Head: "h-test"},
		Equivalence:    engine.EquivalenceReachability,
		DownstreamOnly: []engine.Commit{{SHA: "drift"}},
		ToReturn:       []engine.Commit{{SHA: "drift"}},
	}
	edges := []engine.EdgeState{intoMain("test"), intoMain("development"), nonSource}
	plan := engine.BuildPlan(g, edges)

	var buf bytes.Buffer
	c := &Coordinator{Graph: g, Log: slog.New(slog.NewJSONHandler(&buf, nil))}
	c.logPlanCounts(plan, edges, 7)

	rec := findLogRecord(t, &buf, "plan built")
	if rec["candidates"] != float64(7) {
		t.Errorf("candidates = %v, want 7", rec["candidates"])
	}
	backflow, _ := rec["backflow"].(map[string]any)
	if backflow["toReturn"] != float64(1) {
		t.Errorf("backflow.toReturn = %v, want 1 (union across fan-in edges, non-source ignored)", backflow["toReturn"])
	}
	if backflow["excludedSkip"] != float64(1) {
		t.Errorf("backflow.excludedSkip = %v, want 1 (deduped by SHA)", backflow["excludedSkip"])
	}
}

// TestApplyEmitsApplyCompleteRecord asserts the mutation-side per-run record
// and its field names.
func TestApplyEmitsApplyCompleteRecord(t *testing.T) {
	var buf bytes.Buffer
	c := &Coordinator{Graph: &engine.Graph{Name: "environments"}, Forge: &fakeForge{},
		Log: slog.New(slog.NewJSONHandler(&buf, nil))}
	if _, err := c.Apply(context.Background(), engine.Plan{Graph: "environments"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	rec := findLogRecord(t, &buf, "apply complete")
	if rec["applied"] != float64(0) || rec["superseded"] != float64(0) || rec["divergence"] != false {
		t.Errorf("record = %v, want applied=0 superseded=0 divergence=false", rec)
	}
}

func TestPlanBackflowSkipTrailerSuppresses(t *testing.T) {
	// A hotfix on main carrying the O6 'Oiax-Backflow: skip' trailer is
	// intentionally not returned: the identity ladder honors it, so ToReturn
	// is empty and no backflow action is planned.
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	skipped := commit("hotfix.txt", "urgent\n", "hotfix on main\n\nOiax-Backflow: skip")

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for _, a := range plan.Actions {
		if a.Type == engine.ActionCreateBackflowRequest {
			t.Errorf("skip-trailer hotfix must not backflow, got %+v", a)
		}
	}
	// The edge diagnostics name the excluding rung: the skip trailer.
	assertExclusionReason(t, plan, skipped, engine.BackflowExcludedSkip)
}

// TestPlanBackflowIdentityLookupResolvesOriginTrackingRefs reproduces the exact
// GitHub Action failure: the backflow source (main) and its upstream (test)
// exist only as refs/remotes/origin/<name>, because actions/checkout
// materializes just the triggering branch as a local head. A downstream-only
// hotfix on the source drives the O6 already-returned identity lookup
// (skip-trailer + cherry-pick provenance), which shells out to git with a raw
// rev-range. Before that lookup resolved origin-tracking refs it built
// refs/heads/test..refs/heads/main and died with git exit 128 ("ambiguous
// argument ... unknown revision"), failing reconcile on its first run. The
// hotfix carries no skip trailer, so it must still surface as a backflow
// request — proving the whole observation ran to completion, not just that the
// range read did not error.
func TestPlanBackflowIdentityLookupResolvesOriginTrackingRefs(t *testing.T) {
	r, _ := gitHarness(t)
	dir := r.Dir

	// A hotfix lands on main (the backflow source) that test never received.
	checkout(t, r, "main")
	commitOn(t, dir, "hotfix.txt", "urgent\n", "hotfix on main")

	// development is the triggering branch (a local head); test and main exist
	// only as remote-tracking refs, exactly as under actions/checkout.
	checkout(t, r, "development")
	for _, b := range []string{"test", "main"} {
		sha := gitExec(t, dir, "rev-parse", b)
		gitExec(t, dir, "update-ref", "refs/remotes/origin/"+b, sha)
		gitExec(t, dir, "branch", "-D", b)
	}

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan over origin-only backflow source: %v", err)
	}
	var sawBackflow bool
	for _, a := range plan.Actions {
		if a.Type == engine.ActionCreateBackflowRequest {
			sawBackflow = true
		}
	}
	if !sawBackflow {
		t.Fatalf("want a backflow request for the downstream-only hotfix, got %+v", plan.Actions)
	}
}

func TestPlanBackflowSkipMentionInProseStillReturns(t *testing.T) {
	// The O6 override is a git TRAILER, not any occurrence of the text. A hotfix
	// whose body merely mentions 'Oiax-Backflow: skip' in prose (with more text
	// after it, so it is not the last-paragraph trailer block) must still be
	// returned — otherwise a stray mention silently drops a legitimate hotfix.
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	commit("hotfix.txt", "urgent\n",
		"hotfix on main\n\nOiax-Backflow: skip\n\nWe considered skipping this but decided to return it.")

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	var found bool
	for _, a := range plan.Actions {
		if a.Type == engine.ActionCreateBackflowRequest {
			found = true
		}
	}
	if !found {
		t.Errorf("a prose mention of the trailer must not suppress backflow; actions=%+v", plan.Actions)
	}
}

func TestPlanBackflowExcludesMergeCommits(t *testing.T) {
	// A merge commit on the backflow source (the ordinary merge-PR shape) has no
	// mainline and cannot be cherry-picked; it must be excluded from the return
	// set so it never becomes a permanent false divergence that blocks the
	// genuine hotfix batched with it. The non-merge commits still return.
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	// A feature branch merged into main with a real merge commit, plus a hotfix.
	gitExec(t, r.Dir, "switch", "-q", "-c", "feature")
	commit("feature.txt", "feat\n", "feature work")
	checkout(t, r, "main")
	gitExec(t, r.Dir, "merge", "--no-ff", "-q", "-m", "Merge feature", "feature")
	commit("hotfix.txt", "urgent\n", "hotfix on main")

	f := &fakeForge{createResult: engine.ChangeRequest{ID: "1", Type: engine.RequestTypeBackflow}}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	var bf *engine.Action
	for i := range plan.Actions {
		if plan.Actions[i].Type == engine.ActionCreateBackflowRequest {
			bf = &plan.Actions[i]
		}
	}
	if bf == nil {
		t.Fatalf("want a backflow action, got %+v", plan.Actions)
	}
	// The feature commit and the hotfix return; the merge commit does not.
	if bf.Unpromoted != 2 {
		t.Errorf("backflow returns %d commits, want 2 (feature + hotfix, merge excluded)", bf.Unpromoted)
	}

	res, err := c.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Divergence {
		t.Error("a merge on the source must not produce a cherry-pick divergence")
	}
	if res.Applied != 1 || len(f.pushed) != 1 || len(f.created) != 1 {
		t.Errorf("want the backflow to converge: applied=%d pushed=%d created=%d", res.Applied, len(f.pushed), len(f.created))
	}
}

func TestApplyBackflowSupersedesOnlyStrictlyOlderRequest(t *testing.T) {
	// Two hotfixes land on main in sequence; the current head is the second.
	// An open managed backflow request encoding the FIRST head (an ancestor of
	// the current one) is stale and must be superseded. An open request
	// encoding an unrelated, non-ancestor head must be left alone — closing it
	// would let a run supersede work built on a head newer than its own.
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	h1 := commit("hotfix1.txt", "one\n", "first hotfix")
	commit("hotfix2.txt", "two\n", "second hotfix")

	// An unrelated divergent head that is neither ancestor nor descendant of
	// main's current head.
	gitExec(t, r.Dir, "switch", "-q", "-c", "side", "development")
	sideFull := commit("side.txt", "s\n", "unrelated side commit")
	checkout(t, r, "main")

	short := func(sha string) string { return gitExec(t, r.Dir, "rev-parse", "--short=12", sha) }
	olderBranch := engine.BackflowBranchName("main", "development", short(h1))
	sideBranch := engine.BackflowBranchName("main", "development", short(sideFull))

	f := &fakeForge{
		createResult: engine.ChangeRequest{ID: "new", Type: engine.RequestTypeBackflow},
		open: []engine.ChangeRequest{
			{ID: "older", Type: engine.RequestTypeBackflow, Source: olderBranch, Target: "development"},
			{ID: "unrelated", Type: engine.RequestTypeBackflow, Source: sideBranch, Target: "development"},
		},
	}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	res, err := c.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	if len(f.closed) != 1 || f.closed[0] != forge.RequestID("older") {
		t.Fatalf("closed = %v, want only the strictly-older request %q", f.closed, "older")
	}
	if res.Superseded != 1 {
		t.Errorf("Superseded = %d, want 1 (the strictly-older request)", res.Superseded)
	}
	// L11: superseding the older request also deletes its oiax/ head branch, so
	// orphan refs do not accumulate. The unrelated (non-ancestor) request is left
	// alone, so its branch is not deleted.
	if len(f.deleted) != 1 || f.deleted[0] != olderBranch {
		t.Fatalf("deleted = %v, want only the superseded head branch %q", f.deleted, olderBranch)
	}
}

// TestApplyBackflowSupersedeLeavesRequestOpenWhenBranchDeleteFails covers the
// L11 cleanup ordering: supersede deletes the stale head branch BEFORE closing
// the request. A genuine (non-idempotent) DeleteBranch failure must therefore
// return before the close, leaving the stale request OPEN so the next run
// re-observes it and retries the delete-then-close pair. Were the order
// reversed, a delete failure right after a successful close would drop the
// request from ListManagedRequests' open set forever, permanently leaking its
// oiax/ head branch.
func TestApplyBackflowSupersedeLeavesRequestOpenWhenBranchDeleteFails(t *testing.T) {
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	h1 := commit("hotfix1.txt", "one\n", "first hotfix")
	commit("hotfix2.txt", "two\n", "second hotfix")

	short := func(sha string) string { return gitExec(t, r.Dir, "rev-parse", "--short=12", sha) }
	olderBranch := engine.BackflowBranchName("main", "development", short(h1))

	// DeleteBranch fails transiently; the stale "older" request is a real
	// ancestor of main's current head, so supersede reaches its delete-then-close
	// pair for it.
	f := &fakeForge{
		createResult: engine.ChangeRequest{ID: "new", Type: engine.RequestTypeBackflow},
		deleteErr:    errors.New("500 branch delete failed"),
		open: []engine.ChangeRequest{
			{ID: "older", Type: engine.RequestTypeBackflow, Source: olderBranch, Target: "development"},
		},
	}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	// Apply surfaces the delete failure...
	if _, err := c.Apply(context.Background(), plan); err == nil {
		t.Fatal("apply: want error from failing DeleteBranch, got nil")
	}
	// ...and because the delete runs first, the stale request was never closed:
	// it stays open for a later run to retry, rather than being lost with its
	// branch leaked.
	if len(f.closed) != 0 {
		t.Fatalf("closed = %v, want no close when the branch delete failed first", f.closed)
	}
}

// diamondGraph is a diamond promotion graph in which the backflow source
// (main) has TWO incoming promotion edges (test->main and qa->main). It drives
// the multiple-incoming-edge backflow tests.
// diamondConfig is the diamond graph as the v1 document, so tests can run
// the canonical document validation; diamondGraph is its engine model.
func diamondConfig() *v1.PromotionGraph {
	return &v1.PromotionGraph{
		APIVersion: v1.APIVersion,
		Kind:       v1.KindPromotionGraph,
		Metadata:   v1.Metadata{Name: "environments"},
		Spec: v1.PromotionGraphSpec{
			Branches: map[string]v1.Branch{
				"development": {Role: v1.RoleSource},
				"test":        {},
				"qa":          {},
				"main":        {Role: v1.RoleTerminal},
			},
			Promotions: []v1.Promotion{
				{From: "development", To: "test"},
				{From: "development", To: "qa"},
				{From: "test", To: "main"},
				{From: "qa", To: "main"},
			},
			Backflow: &v1.Backflow{
				Sources:  []string{"main"},
				Target:   "development",
				Strategy: v1.BackflowStrategyCherryPick,
			},
		},
	}
}

func diamondGraph() *engine.Graph {
	return engine.FromConfig(diamondConfig())
}

func TestApplyBackflowAllCommitsDropConverges(t *testing.T) {
	// A hotfix on main (a backflow source) whose content already reached
	// development, but as part of a commit with a DIFFERENT patch-id (it also
	// touches another file), so the plan's patch-identity rung does not mark it
	// already-returned. Cherry-picking it onto development therefore drops every
	// commit as empty and returns the target head unchanged: the edge has
	// converged. Apply must push nothing, create nothing, and clobber no existing
	// backflow request (the pre-fix code force-pushed the target head over the
	// real backflow commit and 422-adopted the stale request, losing the hotfix).
	r, commit := gitHarness(t)

	// development carries the hotfix content plus an unrelated file in ONE commit,
	// giving that commit a patch-id distinct from main's single-file hotfix.
	checkout(t, r, "development")
	if err := os.WriteFile(filepath.Join(r.Dir, "other.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit("hotfix.txt", "urgent\n", "development already carries the hotfix content")

	checkout(t, r, "main")
	commit("hotfix.txt", "urgent\n", "hotfix on main")

	short, err := r.ShortSHA(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	// An existing open backflow request encoding an ANCESTOR head (the base
	// commit): the pre-fix supersede path would close it. The converged path must
	// leave it untouched.
	c0short := gitExec(t, r.Dir, "rev-parse", "--short=12", "main~1")
	f := &fakeForge{
		open: []engine.ChangeRequest{{
			ID: "existing", Type: engine.RequestTypeBackflow,
			Source: engine.BackflowBranchName("main", "development", c0short), Target: "development",
		}},
	}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	a := engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
		Unpromoted: 1, Branch: engine.BackflowBranchName("main", "development", short),
	}
	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Applied != 0 || res.Divergence {
		t.Errorf("result = %+v, want Applied=0 Divergence=false (converged)", res)
	}
	if len(f.pushed) != 0 || len(f.created) != 0 {
		t.Errorf("converged backflow must push nothing and create nothing: pushed=%d created=%d", len(f.pushed), len(f.created))
	}
	if len(f.closed) != 0 {
		t.Errorf("converged backflow must not clobber the existing request: closed=%v", f.closed)
	}
}

func TestBackflowMultipleIncomingEdgesReturnsCompleteSet(t *testing.T) {
	// A backflow source (main) with two incoming promotion edges. H1 lands
	// directly on main (downstream-only via both edges); H2 lands on test and
	// reaches main by merge (downstream-only via qa->main only, since test..main
	// hides it). The complete return set is {H1, H2}; deriving it from the first
	// incoming edge (test->main) alone yields only {H1}. Exactly one backflow
	// action must be planned across the two edges.
	if errs := diamondConfig().Validate(); len(errs) > 0 {
		t.Fatalf("diamond graph must be valid: %v", errs)
	}
	g := diamondGraph()

	r, commit := gitHarness(t)
	gitExec(t, r.Dir, "branch", "qa") // gitHarness creates development and test, not qa

	checkout(t, r, "main")
	h1 := commit("f1.txt", "one\n", "H1 direct hotfix on main")
	checkout(t, r, "test")
	h2 := commit("f2.txt", "two\n", "H2 on test")
	checkout(t, r, "main")
	gitExec(t, r.Dir, "merge", "--no-ff", "-q", "-m", "Merge test", "test")

	f := &fakeForge{}
	c := &Coordinator{Git: r, Forge: f, Graph: g}

	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	var backflow int
	for _, a := range plan.Actions {
		if a.Type == engine.ActionCreateBackflowRequest {
			backflow++
			if a.From != "main" || a.To != "development" {
				t.Errorf("backflow action = %q->%q, want main->development", a.From, a.To)
			}
		}
	}
	if backflow != 1 {
		t.Fatalf("planned %d backflow actions, want exactly 1 (deduped across incoming edges): %+v", backflow, plan.Actions)
	}

	// The complete return set spans both incoming edges.
	st, err := c.backflowActionState(context.Background(), engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
	})
	if err != nil {
		t.Fatalf("backflowActionState: %v", err)
	}
	if len(st.ToReturn) != 2 {
		t.Fatalf("ToReturn has %d commits, want 2 (H1 via both edges, H2 via qa->main only): %+v", len(st.ToReturn), st.ToReturn)
	}
	got := map[string]bool{}
	for _, cm := range st.ToReturn {
		got[cm.SHA] = true
	}
	if !got[h1] || !got[h2] {
		t.Errorf("ToReturn = %+v, want both H1 (%s) and H2 (%s)", st.ToReturn, h1, h2)
	}

	// A full Plan+Apply round trip must report the COMPLETE union in the created
	// request body, not the first incoming edge's partial count.
	if _, err := c.Apply(context.Background(), plan); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(f.created) != 1 {
		t.Fatalf("created %d backflow requests, want 1: %+v", len(f.created), f.created)
	}
	if !strings.Contains(f.created[0].Body, "2 downstream-only commit(s)") {
		t.Errorf("created body = %q, want it to report 2 downstream-only commit(s)", f.created[0].Body)
	}
}

func TestPlanBackflowProvenanceMatchesTrailerNotProse(t *testing.T) {
	// Cherry-pick -x provenance lives in the message's LAST paragraph. A target
	// commit that genuinely records "(cherry picked from commit <sha>)" there
	// suppresses that source commit's backflow; a target commit that merely
	// mentions the same phrase in an EARLIER prose paragraph must not. Otherwise a
	// stray prose mention silently drops a legitimate hotfix.
	r, commit := gitHarness(t)

	checkout(t, r, "main")
	hProse := commit("prose.txt", "p\n", "hotfix referenced only in prose")
	hProv := commit("prov.txt", "v\n", "hotfix recorded by real provenance")

	checkout(t, r, "development")
	// A prose paragraph (NOT the last) quoting the provenance phrase for hProse,
	// followed by another paragraph so it is not the trailer block.
	commit("doc.txt", "d\n",
		"Document the backport policy\n\nExample: cherry picked from commit "+hProse+"\n\nThis paragraph is prose, not a trailer.")
	// A genuine trailer-block provenance line for hProv, with unrelated content so
	// only the identity path (not patch-id) can suppress it.
	commit("note.txt", "u\n", "backport note\n\n(cherry picked from commit "+hProv+")")

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}

	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	var backflow int
	for _, a := range plan.Actions {
		if a.Type == engine.ActionCreateBackflowRequest {
			backflow++
		}
	}
	if backflow != 1 {
		t.Fatalf("planned %d backflow actions, want 1 (the prose-mentioned hotfix still returns)", backflow)
	}
	// The edge diagnostics name the excluding rung: genuine provenance.
	assertExclusionReason(t, plan, hProv, engine.BackflowExcludedProvenance)

	// The complete return set contains hProse (prose mention ignored) and not
	// hProv (genuine trailer provenance honored).
	st, err := c.backflowActionState(context.Background(), engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
	})
	if err != nil {
		t.Fatalf("backflowActionState: %v", err)
	}
	if len(st.ToReturn) != 1 || st.ToReturn[0].SHA != hProse {
		t.Fatalf("ToReturn = %+v, want exactly [%s] (hProse returns, hProv suppressed by provenance)", st.ToReturn, hProse)
	}
}

func TestPlanBackflowProvenanceRequiresStandaloneLine(t *testing.T) {
	// `git cherry-pick -x` writes provenance as its OWN standalone line. A target
	// commit that merely embeds the phrase inside a sentence — even in the same
	// last paragraph as a genuine trailer, with no blank line separating them — is
	// not real provenance and must not suppress a live hotfix's backflow.
	r, commit := gitHarness(t)

	checkout(t, r, "main")
	hProse := commit("prose.txt", "p\n", "live hotfix mentioned only inside a sentence")

	checkout(t, r, "development")
	// The fake mention sits in the SAME last paragraph as a genuine Signed-off-by
	// trailer, with no blank line separating them, embedded mid-sentence.
	commit("doc.txt", "d\n",
		"Restore prior work\n\nThis also restores work previously (cherry picked from commit "+hProse+") done in production.\nSigned-off-by: Dev <dev@example.invalid>")

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}

	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	var backflow int
	for _, a := range plan.Actions {
		if a.Type == engine.ActionCreateBackflowRequest {
			backflow++
		}
	}
	if backflow != 1 {
		t.Fatalf("planned %d backflow actions, want 1 (the embedded mention is not provenance)", backflow)
	}

	st, err := c.backflowActionState(context.Background(), engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
	})
	if err != nil {
		t.Fatalf("backflowActionState: %v", err)
	}
	if len(st.ToReturn) != 1 || st.ToReturn[0].SHA != hProse {
		t.Fatalf("ToReturn = %+v, want exactly [%s] (embedded mention ignored)", st.ToReturn, hProse)
	}
}

func TestPlanBackflowProvenanceMatchesSquashedReturn(t *testing.T) {
	// A backflow return branch squash-merged into the target collapses several
	// cherry-pick -x'd commits into ONE, so each original commit's
	// "(cherry picked from commit <sha>)" line survives at the tail of its own
	// block and only the LAST block's line falls in the body's final paragraph.
	// Provenance matching must scan the tail of every paragraph, or every
	// returned commit but the last is re-proposed on the next reconcile — a
	// spurious divergence (and, on replay, a permanent conflict).
	r, commit := gitHarness(t)

	checkout(t, r, "main")
	hA := commit("a.txt", "a\n", "hotfix A on main")
	hB := commit("b.txt", "b\n", "hotfix B on main")

	checkout(t, r, "development")
	// A GitHub-style squash of a return branch: both provenance lines sit at the
	// tail of a NON-final block; the final paragraph is a Co-authored-by trailer
	// that is not provenance. Touch an unrelated file so the squash's patch-id
	// matches neither hotfix and only the identity (provenance) rung can suppress
	// them.
	commit("returned.txt", "ab\n",
		"oiax: return main to development (#42)\n\n"+
			"* hotfix A on main\n\nSigned-off-by: Dev <dev@example.invalid>\n(cherry picked from commit "+hA+")\n\n"+
			"* hotfix B on main\n\nSigned-off-by: Dev <dev@example.invalid>\n(cherry picked from commit "+hB+")\n\n"+
			"---------\n\nCo-authored-by: Dev <dev@example.invalid>")

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}

	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for _, a := range plan.Actions {
		if a.Type == engine.ActionCreateBackflowRequest {
			t.Fatalf("planned a backflow action; want none (both hotfixes already returned by squash provenance)")
		}
	}
	// Both hotfixes are excluded, and the diagnostics name the provenance rung —
	// including hA, whose line is NOT in the body's final paragraph.
	assertExclusionReason(t, plan, hA, engine.BackflowExcludedProvenance)
	assertExclusionReason(t, plan, hB, engine.BackflowExcludedProvenance)

	st, err := c.backflowActionState(context.Background(), engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
	})
	if err != nil {
		t.Fatalf("backflowActionState: %v", err)
	}
	if len(st.ToReturn) != 0 {
		t.Fatalf("ToReturn = %+v, want empty (both returned by squash provenance)", st.ToReturn)
	}
}

// mergeGraph is testGraph switched to the merge backflow strategy (ADR-0006):
// main's downstream-only commits are returned to development by a single
// wholesale --no-ff merge instead of per-commit cherry-picks.
func mergeGraph() *engine.Graph {
	g := testGraph()
	g.Backflow.Strategy = v1.BackflowStrategyMerge
	g.Backflow.ExpectedMergeMethod = v1.MergeMethodMerge
	return g
}

// TestApplyExecutesMergeBackflowAndSettlesByAncestry covers the merge-strategy
// happy path end to end: a hotfix on main (a backflow source) returns to
// development as a single two-parent merge commit, and once that merge lands on
// development the source head is an ancestor of the target, so the next plan
// settles the edge by ancestry with no further backflow action.
func TestApplyExecutesMergeBackflowAndSettlesByAncestry(t *testing.T) {
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	commit("hotfix.txt", "urgent\n", "hotfix on main")

	f := &fakeForge{createResult: engine.ChangeRequest{ID: "1", Type: engine.RequestTypeBackflow}}
	c := &Coordinator{Git: r, Forge: f, Graph: mergeGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// Exactly the merge-strategy backflow action, tagged with the strategy.
	if len(plan.Actions) != 1 || plan.Actions[0].Type != engine.ActionCreateBackflowRequest {
		t.Fatalf("plan actions = %+v, want one backflow action", plan.Actions)
	}
	if plan.Actions[0].Strategy != v1.BackflowStrategyMerge {
		t.Errorf("action strategy = %q, want %q", plan.Actions[0].Strategy, v1.BackflowStrategyMerge)
	}

	res, err := c.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Applied != 1 || res.Divergence {
		t.Errorf("result = %+v, want Applied=1 Divergence=false", res)
	}
	if len(f.pushed) != 1 || len(f.created) != 1 {
		t.Fatalf("want 1 push and 1 create, got pushed=%d created=%d", len(f.pushed), len(f.created))
	}
	// The pushed head is a real two-parent merge commit, not a cherry-pick:
	// `rev-list --parents -n 1` prints the commit followed by its two parents.
	parents := gitExec(t, r.Dir, "rev-list", "--parents", "-n", "1", f.pushed[0].SHA)
	if got := len(strings.Fields(parents)); got != 3 {
		t.Errorf("pushed commit has %d rev-list fields, want 3 (a 2-parent merge): %q", got, parents)
	}
	// The request body describes the merge-commit mechanism, not cherry-pick.
	if !strings.Contains(f.created[0].Body, "by merge commit") {
		t.Errorf("created body = %q, want it to mention 'by merge commit'", f.created[0].Body)
	}

	// The return merges into development (the backflow target). main is now an
	// ancestor of development, so development..main is empty and the next plan
	// settles the edge by ancestry — no backflow action.
	checkout(t, r, "development")
	gitExec(t, r.Dir, "merge", "--no-ff", "-q", "-m", "Merge backflow into development", "main")
	plan2, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("re-plan: %v", err)
	}
	for _, a := range plan2.Actions {
		if a.Type == engine.ActionCreateBackflowRequest {
			t.Errorf("ancestry-settled merge edge must not backflow again, got %+v", a)
		}
	}
}

// TestPlanMergeBackflowRunsWhenEdgeLocalRangeEmpty guards that merge backflow is
// evaluated from the TARGET-relative range (target..source), not the edge-local
// promotion range (from..to). The pipeline is development -> test -> main, so the
// only promotion edge into the backflow source `main` is test..main. When `test`
// has already caught up to `main`, test..main is empty while development..main
// still holds the hotfix. A gate on the edge-local range would skip merge
// backflow (and its fence and skip scan) entirely and the hotfix would never
// return to development; the target-relative gate proposes the return.
func TestPlanMergeBackflowRunsWhenEdgeLocalRangeEmpty(t *testing.T) {
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	commit("hotfix.txt", "urgent\n", "hotfix on main")
	// The intermediate branch catches up to main, so the observed promotion edge
	// into main (test..main) is empty; development stays behind (development..main
	// = {hotfix}).
	gitExec(t, r.Dir, "branch", "-f", "test", "main")

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: mergeGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	var found *engine.Action
	for i := range plan.Actions {
		if a := &plan.Actions[i]; a.Type == engine.ActionCreateBackflowRequest && a.To == "development" {
			found = a
		}
	}
	if found == nil {
		t.Fatalf("expected a merge createBackflowRequest returning the hotfix to development even though test..main is empty; got actions %+v", plan.Actions)
	}
	if found.Strategy != v1.BackflowStrategyMerge {
		t.Errorf("action strategy = %q, want %q", found.Strategy, v1.BackflowStrategyMerge)
	}
	if found.Unpromoted < 1 {
		t.Errorf("unpromoted = %d, want >= 1 (the hotfix)", found.Unpromoted)
	}
}

// seedMergeAndEmptyBackflow builds a repo whose backflow source (main) carries,
// downstream of the backflow target (development): an ordinary diff-carrying
// hotfix, a real two-parent merge commit, and an empty commit. It returns the
// runner plus the merge and empty commit SHAs.
//
// The merge and empty commits carry NO patch-id, so the cherry-pick returnable
// filter (observeCherryPickBackflow) drops both while the merge path
// (observeMergeBackflow) must return the target..source range WHOLESALE and keep
// them — the exact "wholesale, not filtered" contrast ADR-0006 requires. The
// range development..main is {hotfix, side work, merge, empty} = 4 commits; the
// cherry-pick filter keeps only the two diff-carrying ones.
func seedMergeAndEmptyBackflow(t *testing.T) (r *git.Runner, mergeSHA, emptySHA string) {
	t.Helper()
	r, _ = gitHarness(t)
	checkout(t, r, "main")
	commitOn(t, r.Dir, "hotfix.txt", "urgent\n", "hotfix on main") // diff-carrying
	// A real two-parent merge commit: branch off, add a commit, --no-ff merge back.
	gitExec(t, r.Dir, "checkout", "-q", "-b", "side")
	commitOn(t, r.Dir, "side.txt", "s\n", "side work") // diff-carrying
	checkout(t, r, "main")
	gitExec(t, r.Dir, "merge", "--no-ff", "-q", "-m", "Merge side into main", "side")
	mergeSHA = gitExec(t, r.Dir, "rev-parse", "HEAD") // merge: no patch-id
	// An empty commit inside the range: carries no diff, so no patch-id either.
	gitExec(t, r.Dir, "commit", "--allow-empty", "-q", "-m", "empty marker on main")
	emptySHA = gitExec(t, r.Dir, "rev-parse", "HEAD") // empty: no patch-id
	return r, mergeSHA, emptySHA
}

func containsSHA(commits []engine.Commit, sha string) bool {
	for _, cm := range commits {
		if cm.SHA == sha {
			return true
		}
	}
	return false
}

// TestPlanMergeBackflowReturnsMergeAndEmptyCommitsWholesale guards the CRITICAL
// ADR-0006 property that the merge backflow path returns the target..source
// range WHOLESALE and does NOT apply the cherry-pick returnable patch-id filter.
// The range holds a real two-parent merge commit and an empty commit, neither of
// which carries a patch-id; a wholesale merge return must still count and return
// both. A regression that re-introduced the returnable filter on the merge path
// would drop them and under-report the count — this test fails loudly if it does.
func TestPlanMergeBackflowReturnsMergeAndEmptyCommitsWholesale(t *testing.T) {
	r, mergeSHA, emptySHA := seedMergeAndEmptyBackflow(t)

	f := &fakeForge{createResult: engine.ChangeRequest{ID: "1", Type: engine.RequestTypeBackflow}}
	c := &Coordinator{Git: r, Forge: f, Graph: mergeGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// Exactly one merge-strategy backflow action counting the WHOLE range (4):
	// the two diff-carrying commits PLUS the merge and empty commits.
	if len(plan.Actions) != 1 || plan.Actions[0].Type != engine.ActionCreateBackflowRequest {
		t.Fatalf("plan actions = %+v, want one backflow action", plan.Actions)
	}
	if plan.Actions[0].Unpromoted != 4 {
		t.Errorf("merge backflow counts %d commits, want 4 wholesale (incl. merge+empty commit)", plan.Actions[0].Unpromoted)
	}

	// The merge and empty commits are genuinely in the wholesale return set — the
	// cherry-pick returnable filter is skipped, not merely masked by the count.
	st, err := c.backflowActionState(context.Background(), engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
	})
	if err != nil {
		t.Fatalf("backflowActionState: %v", err)
	}
	if len(st.ToReturn) != 4 {
		t.Fatalf("ToReturn has %d commits, want 4 wholesale: %+v", len(st.ToReturn), st.ToReturn)
	}
	if !containsSHA(st.ToReturn, mergeSHA) {
		t.Errorf("wholesale merge return dropped the merge commit %s: %+v", mergeSHA, st.ToReturn)
	}
	if !containsSHA(st.ToReturn, emptySHA) {
		t.Errorf("wholesale merge return dropped the empty commit %s: %+v", emptySHA, st.ToReturn)
	}

	// Apply confirms the wholesale count reaches the created request body.
	if _, err := c.Apply(context.Background(), plan); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(f.created) != 1 {
		t.Fatalf("created %d backflow requests, want 1: %+v", len(f.created), f.created)
	}
	if !strings.Contains(f.created[0].Body, "4 downstream-only commit(s)") {
		t.Errorf("created body = %q, want it to report 4 downstream-only commit(s)", f.created[0].Body)
	}
}

// TestPlanCherryPickBackflowFiltersMergeAndEmptyCommits is the contrast that
// makes the wholesale-merge property above load-bearing: the SAME fixture under
// the cherry-pick strategy drops the merge and empty commits (cherry-pick can
// replay neither), returning only the two diff-carrying commits. Pinning the
// cherry-pick side at 2 proves the merge side's 4 is genuinely "filter skipped",
// not an accident of the fixture.
func TestPlanCherryPickBackflowFiltersMergeAndEmptyCommits(t *testing.T) {
	r, mergeSHA, emptySHA := seedMergeAndEmptyBackflow(t)

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()} // cherry-pick strategy
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Type != engine.ActionCreateBackflowRequest {
		t.Fatalf("plan actions = %+v, want one backflow action", plan.Actions)
	}
	if plan.Actions[0].Unpromoted != 2 {
		t.Errorf("cherry-pick backflow counts %d commits, want 2 (merge+empty filtered out)", plan.Actions[0].Unpromoted)
	}

	st, err := c.backflowActionState(context.Background(), engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
	})
	if err != nil {
		t.Fatalf("backflowActionState: %v", err)
	}
	if len(st.ToReturn) != 2 {
		t.Fatalf("ToReturn has %d commits, want 2 (merge+empty filtered): %+v", len(st.ToReturn), st.ToReturn)
	}
	if containsSHA(st.ToReturn, mergeSHA) {
		t.Errorf("cherry-pick returnable filter kept the merge commit %s: %+v", mergeSHA, st.ToReturn)
	}
	if containsSHA(st.ToReturn, emptySHA) {
		t.Errorf("cherry-pick returnable filter kept the empty commit %s: %+v", emptySHA, st.ToReturn)
	}
}

// TestPlanMergeBackflowForbiddenMergeCommitReportsDivergence covers ADR-0006
// Amendment 1: reconcile.observe reads the TARGET branch's live merge-commit
// capability every plan; when the branch forbids merge commits (linear history
// required) the wholesale return merge cannot land, so the plan reports
// divergence (exit 3) instead of a backflow action, and nothing is merged.
func TestPlanMergeBackflowForbiddenMergeCommitReportsDivergence(t *testing.T) {
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	commit("hotfix.txt", "urgent\n", "hotfix on main")

	// Merge is enabled at the repo level, but the target branch requires linear
	// history — the exact blind spot repo-level RepoMergeMethods cannot see.
	f := &fakeForge{targetMergeMethods: &forge.MergeMethods{Merge: true, RequiresLinearHistory: true}}
	c := &Coordinator{Git: r, Forge: f, Graph: mergeGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	reports, backflow := countBackflowOutcome(plan)
	if reports != 1 || backflow != 0 {
		t.Fatalf("want 1 divergence and 0 backflow for the forbidden-merge fence, got reports=%d backflow=%d: %+v", reports, backflow, plan.Actions)
	}
	// The read was live and target-branch-scoped.
	if f.targetMergeMethodsCalls == 0 || f.targetMergeBranch != "development" {
		t.Errorf("TargetMergeMethods calls=%d branch=%q, want a live read against development", f.targetMergeMethodsCalls, f.targetMergeBranch)
	}

	res, err := c.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Divergence {
		t.Error("want Divergence true (exit 3) on the merge-commit fence")
	}
	if len(f.pushed) != 0 || len(f.created) != 0 {
		t.Errorf("fenced backflow must push/create nothing: pushed=%d created=%d", len(f.pushed), len(f.created))
	}
}

// TestPlanMergeBackflowSquashOnlyReportsDivergence is the second half of the
// merge-commit fence (ADR-0006 Amendment 1): a target repo that allows only
// squash merges (Merge:false) forbids merge commits just as surely as a
// linear-history ruleset does, so MergeCommitAllowed() is false and the
// wholesale return merge cannot land. The plan reports divergence (exit 3) and
// applies nothing — the same outcome as the linear-history case above, reached
// through the other input to MergeCommitAllowed().
func TestPlanMergeBackflowSquashOnlyReportsDivergence(t *testing.T) {
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	commit("hotfix.txt", "urgent\n", "hotfix on main")

	// Squash and rebase allowed, merge commits disabled at the repo level.
	f := &fakeForge{targetMergeMethods: &forge.MergeMethods{Merge: false, Squash: true, Rebase: true}}
	c := &Coordinator{Git: r, Forge: f, Graph: mergeGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	reports, backflow := countBackflowOutcome(plan)
	if reports != 1 || backflow != 0 {
		t.Fatalf("want 1 divergence and 0 backflow for the squash-only fence, got reports=%d backflow=%d: %+v", reports, backflow, plan.Actions)
	}

	res, err := c.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Divergence {
		t.Error("want Divergence true (exit 3) on the squash-only fence")
	}
	if len(f.pushed) != 0 || len(f.created) != 0 {
		t.Errorf("fenced backflow must push/create nothing: pushed=%d created=%d", len(f.pushed), len(f.created))
	}
}

// TestPlanMergeBackflowTargetFetchErrorIsLoud covers the fail-loud contract of
// the fence (ADR-0006 Amendment 1): unlike the advisory promotion mergeMethod
// warning, a TargetMergeMethods FETCH error is an operational failure that must
// propagate out of Plan — never be swallowed to "merge not allowed".
func TestPlanMergeBackflowTargetFetchErrorIsLoud(t *testing.T) {
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	commit("hotfix.txt", "urgent\n", "hotfix on main")

	f := &fakeForge{targetMergeMethodsErr: errors.New("boom reading rules")}
	c := &Coordinator{Git: r, Forge: f, Graph: mergeGraph()}
	if _, err := c.Plan(context.Background()); err == nil {
		t.Fatal("plan: want a loud error from the TargetMergeMethods fetch failure, got nil")
	}
}

// TestPlanMergeBackflowSkipTrailerReportsDivergence covers ADR-0006 Amendment 2:
// under cherry-pick an Oiax-Backflow: skip trailer silently excludes the commit,
// but a wholesale merge cannot honor per-commit exclusion, so a skip inside the
// returnable range is a HARD ERROR (exit-3 divergence), NOT a silent
// suppression. This is the merge inverse of TestPlanBackflowSkipTrailerSuppresses.
func TestPlanMergeBackflowSkipTrailerReportsDivergence(t *testing.T) {
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	commit("hotfix.txt", "urgent\n", "hotfix on main\n\nOiax-Backflow: skip")

	f := &fakeForge{}
	c := &Coordinator{Git: r, Forge: f, Graph: mergeGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	reports, backflow := countBackflowOutcome(plan)
	if reports != 1 || backflow != 0 {
		t.Fatalf("skip-in-range under merge must be a divergence, not an exclusion: reports=%d backflow=%d: %+v", reports, backflow, plan.Actions)
	}

	res, err := c.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Divergence {
		t.Error("want Divergence true (exit 3) on skip-in-range under merge")
	}
	if len(f.pushed) != 0 || len(f.created) != 0 {
		t.Errorf("skip-in-range divergence must push/create nothing: pushed=%d created=%d", len(f.pushed), len(f.created))
	}
}

// TestApplyMergeBackflowConflictReportsDivergence covers the merge-conflict
// execution path: development and main edit the same file differently, so the
// wholesale merge of main onto development conflicts. Like a cherry-pick
// conflict it becomes a reported divergence (exit 3) — nothing pushed, nothing
// created, the ephemeral worktree cleaned up by git merge --abort.
func TestApplyMergeBackflowConflictReportsDivergence(t *testing.T) {
	r, commit := gitHarness(t)
	checkout(t, r, "development")
	commit("app.txt", "dev-change\n", "dev edit")
	checkout(t, r, "main")
	commit("app.txt", "hotfix-change\n", "hotfix on main")

	short, err := r.ShortSHA(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	// Drive the backflow arm directly so the test isolates the merge-conflict
	// path from the promotion actions the diverged development branch produces.
	a := engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
		Unpromoted: 1, Strategy: v1.BackflowStrategyMerge,
		Branch: engine.BackflowBranchName("main", "development", short),
	}
	f := &fakeForge{}
	c := &Coordinator{Git: r, Forge: f, Graph: mergeGraph()}

	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Divergence {
		t.Error("want Divergence true on merge conflict")
	}
	if res.Applied != 0 {
		t.Errorf("Applied = %d, want 0 on conflict", res.Applied)
	}
	if len(f.pushed) != 0 || len(f.created) != 0 {
		t.Errorf("conflict must push nothing and create nothing: pushed=%d created=%d", len(f.pushed), len(f.created))
	}
}

// countBackflowOutcome tallies the two backflow outcomes on the main->development
// edge in a merge-strategy plan: reported divergences and create-backflow actions.
func countBackflowOutcome(plan engine.Plan) (reports, backflow int) {
	for _, a := range plan.Actions {
		switch a.Type {
		case engine.ActionReportDivergence:
			if a.From == "main" && a.To == "development" {
				reports++
			}
		case engine.ActionCreateBackflowRequest:
			backflow++
		}
	}
	return reports, backflow
}

func TestWarnMergeMethodMismatch(t *testing.T) {
	cases := []struct {
		name    string
		method  v1.MergeMethod
		allowed *forge.MergeMethods
		err     error
		warn    bool
		called  bool
	}{
		{
			name: "disallowed method warns", method: v1.MergeMethodSquash,
			allowed: &forge.MergeMethods{Merge: true, Rebase: true}, warn: true, called: true,
		},
		{
			name: "allowed method is quiet", method: v1.MergeMethodSquash,
			allowed: &forge.MergeMethods{Merge: true, Squash: true, Rebase: true}, warn: false, called: true,
		},
		{name: "no expectation skips the forge call", method: "", warn: false, called: false},
		{
			name: "fetch error is advisory, not fatal", method: v1.MergeMethodSquash,
			err: errors.New("boom"), warn: false, called: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			f := &fakeForge{mergeMethods: tc.allowed, mergeMethodsErr: tc.err}
			c := &Coordinator{
				Forge: f,
				Graph: &engine.Graph{Name: "environments", Promotions: []engine.Promotion{
					{From: "development", To: "test", Expectations: engine.Expectations{MergeMethod: tc.method}},
				}},
				Log: slog.New(slog.NewTextHandler(&buf, nil)),
			}
			c.warnMergeMethodMismatch(context.Background())
			if warned := strings.Contains(buf.String(), "does not allow"); warned != tc.warn {
				t.Errorf("warned = %v, want %v (log: %q)", warned, tc.warn, buf.String())
			}
			if called := f.mergeMethodsCalls > 0; called != tc.called {
				t.Errorf("RepoMergeMethods called = %v, want %v", called, tc.called)
			}
		})
	}
}

// TestPlanShallowCloneWarns reproduces the actions/checkout default
// (fetch-depth: 1): a shallow clone silently disables the patch-identity and
// baseline rungs, so Plan must surface a clear warning naming fetch-depth: 0
// rather than proceed with degraded equivalence detection.
func TestPlanShallowCloneWarns(t *testing.T) {
	r, _ := gitHarness(t)
	head := gitExec(t, r.Dir, "rev-parse", "HEAD")
	// Write the grafts file a depth-limited fetch leaves behind.
	if err := os.WriteFile(filepath.Join(r.Dir, ".git", "shallow"), []byte(head+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var logBuf bytes.Buffer
	c := &Coordinator{
		Git:   r,
		Forge: &fakeForge{},
		Graph: testGraph(),
		Log:   NewLogger("text", AnnotateGitHub, nil, &logBuf),
	}
	if _, err := c.Plan(context.Background()); err != nil {
		t.Fatalf("plan: %v", err)
	}
	log := logBuf.String()
	if !strings.Contains(log, "shallow clone") || !strings.Contains(log, "fetch-depth: 0") {
		t.Fatalf("Plan did not warn about the shallow clone: %q", log)
	}
}

// TestPlanShallowCloneRefusedWhenPolicySet is the RefuseShallow counterpart to
// TestPlanShallowCloneWarns: under CI the CLI sets RefuseShallow, so the same
// shallow clone that only warns locally becomes a hard error that stops the run
// before it can open a spurious promotion request. The message still names
// fetch-depth: 0 so the operator has the recovery.
func TestPlanShallowCloneRefusedWhenPolicySet(t *testing.T) {
	r, _ := gitHarness(t)
	head := gitExec(t, r.Dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(r.Dir, ".git", "shallow"), []byte(head+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var logBuf bytes.Buffer
	c := &Coordinator{
		Git:           r,
		Forge:         &fakeForge{},
		Graph:         testGraph(),
		Log:           NewLogger("text", AnnotateGitHub, nil, &logBuf),
		RefuseShallow: true,
	}
	_, err := c.Plan(context.Background())
	if err == nil {
		t.Fatal("Plan on a shallow clone with RefuseShallow set succeeded, want a hard error")
	}
	if !strings.Contains(err.Error(), "shallow clone") ||
		!strings.Contains(err.Error(), "refusing under CI") ||
		!strings.Contains(err.Error(), "fetch-depth: 0") {
		t.Fatalf("error = %v, want a shallow-clone CI refusal naming fetch-depth: 0", err)
	}
}

// TestPlanWarnsWhenRepositoryDeletesSourceBranch covers the branch-per-env
// hazard that repository-wide auto-delete-on-merge creates: every promotion
// request is opened FROM a long-lived graph branch, so merging one deletes that
// branch and strands the graph. The warning must name the branches actually at
// risk (the promotion sources), so an operator can see the blast radius without
// re-deriving it from the config, and must name the remedy.
func TestPlanWarnsWhenRepositoryDeletesSourceBranch(t *testing.T) {
	r, _ := gitHarness(t)

	var logBuf bytes.Buffer
	c := &Coordinator{
		Git:   r,
		Forge: &fakeForge{deletesSourceOnMerge: true},
		Graph: testGraph(),
		Log:   NewLogger("text", AnnotateGitHub, nil, &logBuf),
	}
	if _, err := c.Plan(context.Background()); err != nil {
		t.Fatalf("plan: %v", err)
	}
	log := logBuf.String()
	for _, want := range []string{
		"deletes the source branch",
		"development", // promotion source, at risk
		"test",        // promotion source on the second edge, also at risk
		"bypass role",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("warning does not mention %q: %s", want, log)
		}
	}
	// "main" is a promotion TARGET only — it is never a request source, so
	// naming it would overstate the blast radius.
	if strings.Contains(log, "development, test, main") {
		t.Errorf("warning names a target-only branch as at risk: %s", log)
	}
}

// TestPlanSilentWhenRepositoryKeepsSourceBranch is the negative case: a
// correctly-configured repository must produce no branch-deletion warning at
// all. A warning that fires on a healthy repo trains operators to ignore it.
func TestPlanSilentWhenRepositoryKeepsSourceBranch(t *testing.T) {
	r, _ := gitHarness(t)

	var logBuf bytes.Buffer
	f := &fakeForge{deletesSourceOnMerge: false}
	c := &Coordinator{
		Git:   r,
		Forge: f,
		Graph: testGraph(),
		Log:   NewLogger("text", AnnotateGitHub, nil, &logBuf),
	}
	if _, err := c.Plan(context.Background()); err != nil {
		t.Fatalf("plan: %v", err)
	}
	if f.deletesSourceOnMergeCalls != 1 {
		t.Errorf("RepoDeletesSourceOnMerge called %d times, want exactly 1 per plan",
			f.deletesSourceOnMergeCalls)
	}
	if strings.Contains(logBuf.String(), "deletes the source branch") {
		t.Errorf("warned on a repository that keeps source branches: %s", logBuf.String())
	}
}

// TestPlanSourceBranchDeletionCheckFailureIsNotFatal pins the advisory
// contract: the check reads a repository setting, which a token without
// repository-read scope cannot see. An advisory warning must degrade to a debug
// line and let the reconcile proceed — never fail a plan that is otherwise
// correct.
func TestPlanSourceBranchDeletionCheckFailureIsNotFatal(t *testing.T) {
	r, _ := gitHarness(t)

	var logBuf bytes.Buffer
	c := &Coordinator{
		Git:   r,
		Forge: &fakeForge{deletesSourceOnMergeErr: errors.New("403 Resource not accessible by integration")},
		Graph: testGraph(),
		Log:   NewLogger("text", AnnotateGitHub, nil, &logBuf),
	}
	if _, err := c.Plan(context.Background()); err != nil {
		t.Fatalf("plan failed on an unreadable repository setting, want it to proceed: %v", err)
	}
	if strings.Contains(logBuf.String(), "deletes the source branch") {
		t.Errorf("warned despite being unable to read the setting: %s", logBuf.String())
	}
}

// TestPlanMissingGraphBranchExplainsDeletion covers the diagnosis half of the
// same hazard. Once auto-delete-on-merge HAS eaten a graph branch, the operator
// meets a resolution failure, not the warning. A bare "branch not found" sends
// them hunting for a config typo; the error must instead say the branch is
// declared in the graph and was most likely deleted by a merge.
func TestPlanMissingGraphBranchExplainsDeletion(t *testing.T) {
	r, _ := gitHarness(t)

	g := testGraph()
	// Stand in for a branch that auto-delete removed after a promotion merged.
	g.Promotions = []engine.Promotion{{From: "deleted-by-merge", To: "test"}}

	c := &Coordinator{
		Git:   r,
		Forge: &fakeForge{},
		Graph: g,
		Log:   NewLogger("text", AnnotateGitHub, nil, &bytes.Buffer{}),
	}
	_, err := c.Plan(context.Background())
	if err == nil {
		t.Fatal("Plan succeeded with a graph branch that does not exist, want an error")
	}
	if !errors.Is(err, git.ErrBranchNotFound) {
		t.Errorf("error does not wrap git.ErrBranchNotFound, so callers cannot classify it: %v", err)
	}
	for _, want := range []string{
		"edge deleted-by-merge->test", // which edge
		"declared in the promotion graph",
		"deleted when a promotion request merged", // the likely cause
		"restore the branch",                      // the remedy
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
}

// TestApplyBackflowSkipsUnchangedRepush covers the M6 churn guard: when the
// deterministic branch already carries the exact head this run replays, the
// run must skip the force-push so an unchanged replay does not re-trigger CI
// on the open request. CreateRequest is still called on every run — it is
// idempotent (adopts the existing open request) — because the branch head
// matching is not proof that a request currently references it; see
// TestApplyBackflowRecreatesRequestAfterCreateFailure for the case where it
// isn't.
func TestApplyBackflowSkipsUnchangedRepush(t *testing.T) {
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	commit("hotfix.txt", "urgent\n", "hotfix on main")

	f := &fakeForge{createResult: engine.ChangeRequest{ID: "1", Type: engine.RequestTypeBackflow}}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// First apply pushes the branch and opens the request.
	if _, err := c.Apply(context.Background(), plan); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if len(f.pushed) != 1 {
		t.Fatalf("first apply pushed %d, want 1", len(f.pushed))
	}
	branch, pushedSHA := f.pushed[0].Name, f.pushed[0].SHA

	// Record the pushed head as the branch's remote-tracking ref: the state the
	// next run's ref-prep fetch would observe.
	gitExec(t, r.Dir, "update-ref", "refs/remotes/origin/"+branch, pushedSHA)

	// Second apply over unchanged state: the deterministic replay yields the
	// same head, already on the branch, so the force-push is skipped. The
	// create call still happens (and is a safe no-op/adopt in the real forge).
	f.pushed = nil
	f.created = nil
	if _, err := c.Apply(context.Background(), plan); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if len(f.pushed) != 0 {
		t.Errorf("unchanged replay re-pushed: %+v", f.pushed)
	}
	if len(f.created) != 1 {
		t.Errorf("unchanged replay did not re-issue CreateRequest: %+v", f.created)
	}
}

// TestApplyBackflowRecreatesRequestAfterCreateFailure reproduces the gap the
// M6 guard used to leave open: a run whose PushBranch succeeds but whose
// CreateRequest then fails (e.g. a transient forge error) must not be
// mistaken, on the next run, for a run that already has an open request.
// RemoteTrackingHead only proves the branch carries this content — not that a
// request references it — so CreateRequest must still fire on the retry even
// though the deterministic replay reproduces the identical head.
func TestApplyBackflowRecreatesRequestAfterCreateFailure(t *testing.T) {
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	commit("hotfix.txt", "urgent\n", "hotfix on main")

	f := &fakeForge{
		createResult: engine.ChangeRequest{ID: "1", Type: engine.RequestTypeBackflow},
		createErr:    errors.New("503 service unavailable"),
	}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// First apply: PushBranch succeeds, then CreateRequest fails transiently.
	// Apply surfaces the error; nothing else in the plan runs.
	if _, err := c.Apply(context.Background(), plan); err == nil {
		t.Fatal("first apply: want error from failing CreateRequest, got nil")
	}
	if len(f.pushed) != 1 {
		t.Fatalf("first apply pushed %d, want 1", len(f.pushed))
	}
	branch, pushedSHA := f.pushed[0].Name, f.pushed[0].SHA

	// The pushed branch persists on the forge regardless of the later
	// CreateRequest failure, and the next run's ref-prep fetch observes it.
	gitExec(t, r.Dir, "update-ref", "refs/remotes/origin/"+branch, pushedSHA)

	// Second apply, forge healthy again, nothing else changed: CherryPick
	// deterministically reproduces the same head, so the branch's
	// remote-tracking head still matches. The hotfix must not be stranded —
	// CreateRequest must still be attempted and this time succeed.
	f.pushed = nil
	f.created = nil
	f.createErr = nil
	if _, err := c.Apply(context.Background(), plan); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if len(f.created) != 1 {
		t.Fatalf("second apply did not retry CreateRequest: created=%+v", f.created)
	}
	if len(f.pushed) != 0 {
		t.Errorf("second apply re-pushed an unchanged head: %+v", f.pushed)
	}
}

// TestApplyBackflowClosesOrphanedRequest covers L3: a still-open managed
// backflow request whose encoded source head no longer resolves to any commit
// (its source branch was history-rewritten) is a permanent orphan. Supersede
// closes it with an explanation and deletes its leftover oiax/ head branch —
// unlike a resolvable non-ancestor head, which is left strictly alone.
func TestApplyBackflowClosesOrphanedRequest(t *testing.T) {
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	commit("hotfix.txt", "urgent\n", "hotfix on main")

	// A well-formed but nonexistent 12-hex head encoded into a managed branch:
	// definitively unresolvable in this repository.
	orphanBranch := engine.BackflowBranchName("main", "development", "0123456789ab")
	f := &fakeForge{
		createResult: engine.ChangeRequest{ID: "new", Type: engine.RequestTypeBackflow},
		open: []engine.ChangeRequest{
			{ID: "orphan", Type: engine.RequestTypeBackflow, Source: orphanBranch, Target: "development"},
		},
	}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	res, err := c.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(f.closed) != 1 || f.closed[0] != forge.RequestID("orphan") {
		t.Fatalf("closed = %v, want the orphaned request %q closed", f.closed, "orphan")
	}
	if len(f.deleted) != 1 || f.deleted[0] != orphanBranch {
		t.Fatalf("deleted = %v, want the orphan head branch %q deleted", f.deleted, orphanBranch)
	}
	// The orphan close counts toward the run's Superseded tally, same as an
	// ancestry supersede.
	if res.Superseded != 1 {
		t.Errorf("Superseded = %d, want 1 (the orphan close)", res.Superseded)
	}
}

// TestApplyBackflowLeavesMalformedEncodedHeadAlone covers the other half of
// L3's classification: a still-open managed request whose branch-encoded
// segment is not a well-formed object id at all (e.g. a branch an external
// actor created sharing the oiax/backflow/ prefix) makes CommitExists fail
// its oidPattern guard and return an error, not a definitive not-found. The
// doc comment on supersedeBackflow promises this is left "strictly alone" --
// unlike TestApplyBackflowClosesOrphanedRequest's well-formed-but-absent head,
// this request must be neither closed nor have its branch deleted.
func TestApplyBackflowLeavesMalformedEncodedHeadAlone(t *testing.T) {
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	commit("hotfix.txt", "urgent\n", "hotfix on main")

	// Not a valid object id: fails oidPattern before any git lookup runs, so
	// CommitExists returns an error rather than exists=false.
	malformedBranch := engine.BackflowBranchName("main", "development", "not-a-real-sha")
	f := &fakeForge{
		createResult: engine.ChangeRequest{ID: "new", Type: engine.RequestTypeBackflow},
		open: []engine.ChangeRequest{
			{ID: "malformed", Type: engine.RequestTypeBackflow, Source: malformedBranch, Target: "development"},
		},
	}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := c.Apply(context.Background(), plan); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(f.closed) != 0 {
		t.Fatalf("closed = %v, want the malformed-head request left strictly alone", f.closed)
	}
	if len(f.deleted) != 0 {
		t.Fatalf("deleted = %v, want no branch deleted for the malformed-head request", f.deleted)
	}
}

// TestApplyBackflowShallowCloneLeavesAmbiguousOrphanAlone reproduces the exact
// actions/checkout default (fetch-depth: 1) condition M5's Plan warning flags:
// two sequential hotfixes land on main, and a genuine depth-1 clone plus
// action.yml's own ref-prep fetch (`git fetch ... refs/heads/*:refs/remotes/
// origin/*`, which does not deepen anything) leaves the FIRST hotfix's commit
// object genuinely absent locally — not because main's history was rewritten,
// but because a shallow fetch only ever brings down each ref's tip. That is the
// exact same "not found" CommitExists reports for a real rewrite, so the older,
// still-genuinely-ancestor request must be left alone (as an operational
// CommitExists error already is), never closed with a false "history was
// rewritten" claim. Unlike TestApplyBackflowClosesOrphanedRequest, this uses a
// REAL shallow clone (git clone --depth=1), not a fake .git/shallow graft, so
// the encoded head is truly missing from the object database.
func TestApplyBackflowShallowCloneLeavesAmbiguousOrphanAlone(t *testing.T) {
	origin, commit := gitHarness(t)
	checkout(t, origin, "main")
	h1 := commit("hotfix1.txt", "one\n", "first hotfix")
	commit("hotfix2.txt", "two\n", "second hotfix")
	h1Short := gitExec(t, origin.Dir, "rev-parse", "--short=12", h1)

	// A genuine depth-1 clone of origin, mirroring actions/checkout's default,
	// plus action.yml's own ref-prep fetch. Neither step deepens history: h1
	// (main's earlier hotfix, not any ref's tip) is never fetched.
	parent := t.TempDir()
	shallowDir := filepath.Join(parent, "shallow")
	gitExec(t, parent, "clone", "-q", "--depth=1", "file://"+origin.Dir, shallowDir)
	gitExec(t, shallowDir, "fetch", "--no-tags", "--prune", "origin",
		"+refs/heads/*:refs/remotes/origin/*")
	// development and test exist solely as origin-tracking refs, exactly as
	// under actions/checkout: only main (the triggering, shallow-cloned branch)
	// is a local head. The reconcile layer's O6 identity lookup resolves each
	// range endpoint to its local head or origin-tracking ref, so no local heads
	// need to be synthesized here -- main alone stays a true single-commit
	// shallow head, which is what this test exercises.
	shallowRunner := &git.Runner{Dir: shallowDir}

	// Confirm the premise directly: h1 is a genuine ancestor of main's current
	// (shallow) tip that origin still has in full, yet it does not resolve at
	// all in the shallow clone.
	if exists, err := shallowRunner.CommitExists(context.Background(), h1); err != nil {
		t.Fatalf("CommitExists(h1) in shallow clone: %v", err)
	} else if exists {
		t.Fatalf("premise violated: h1 (%s) resolves in the shallow clone; test no longer reproduces the ambiguity", h1)
	}

	// An open managed backflow request encoding h1 — stale relative to the new
	// hotfix but a REAL, non-rewritten ancestor, not an orphan.
	olderBranch := engine.BackflowBranchName("main", "development", h1Short)
	f := &fakeForge{
		createResult: engine.ChangeRequest{ID: "new", Type: engine.RequestTypeBackflow},
		open: []engine.ChangeRequest{
			{ID: "older", Type: engine.RequestTypeBackflow, Source: olderBranch, Target: "development"},
		},
	}
	c := &Coordinator{Git: shallowRunner, Forge: f, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := c.Apply(context.Background(), plan); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(f.closed) != 0 {
		t.Fatalf("closed = %v, want the ambiguous (shallow, not actually rewritten) request left alone", f.closed)
	}
	if len(f.deleted) != 0 {
		t.Fatalf("deleted = %v, want no branch deleted for the ambiguous request", f.deleted)
	}
}

// --- SKA-601: durable backflow-conflict artifact state machine -----------

// conflictHarness builds a repo where main (the backflow source) and
// development (the backflow target) edit the same file differently, so a
// backflow cherry-pick conflicts. It returns the runner, a commit helper that
// commits on the currently-checked-out branch (main, on return), and the
// deterministic backflow action for main -> development.
func conflictHarness(t *testing.T) (*git.Runner, func(file, content, msg string) string, engine.Action) {
	t.Helper()
	r, commit := gitHarness(t)
	checkout(t, r, "development")
	commitOn(t, r.Dir, "app.txt", "dev-change\n", "dev edit")
	checkout(t, r, "main")
	commit("app.txt", "hotfix-change\n", "hotfix on main")
	short, err := r.ShortSHA(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	a := engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
		Unpromoted: 1, Branch: engine.BackflowBranchName("main", "development", short),
	}
	return r, commit, a
}

func TestApplyBackflowConflictCreatesArtifact(t *testing.T) {
	// A cherry-pick conflict still reports divergence (exit 3, unchanged) AND
	// opens exactly one durable artifact keyed to the full live source head with
	// the clean count in the body.
	r, _, a := conflictHarness(t)
	f := &fakeForge{}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Divergence {
		t.Error("want Divergence true (exit 3) on a cherry-pick conflict")
	}
	if res.Conflicts != 1 {
		t.Errorf("Conflicts = %d, want 1 (one artifact created)", res.Conflicts)
	}
	if len(f.conflicts) != 1 {
		t.Fatalf("want 1 open artifact, got %d: %+v", len(f.conflicts), f.conflicts)
	}
	mainHead, _ := r.Head(context.Background(), "main")
	got := f.conflicts[0]
	if got.Source != "main" || got.Target != "development" || got.SourceHead != mainHead {
		t.Errorf("artifact = %+v, want source=main target=development sourceHead=%s", got, mainHead)
	}
	// The body reports the failing commit and the clean count (0: the very first
	// commit conflicts).
	if len(f.conflictCreated) != 1 || !strings.Contains(f.conflictCreated[0].Body, "applied 0 commit(s) cleanly") {
		t.Errorf("created body = %q, want it to report the clean count", f.conflictCreated)
	}
}

func TestApplyBackflowSquashConflictAddsGuidance(t *testing.T) {
	// When the failing commit is a squash rollup (its body names >= 2 cherry-pick
	// -x provenance lines), the conflict artifact body carries the squash-aware
	// guidance steering the operator to the skip escape hatch — instead of only
	// the opaque per-commit clean count.
	r, commit := gitHarness(t)
	checkout(t, r, "development")
	commitOn(t, r.Dir, "app.txt", "dev-change\n", "dev edit")
	checkout(t, r, "main")
	// A squash-shaped hotfix: two provenance lines, each at the tail of its own
	// block. The referenced SHAs need not exist — squashCommitCount only counts
	// the -x lines. Its app.txt edit conflicts with development's.
	commit("app.txt", "hotfix-change\n",
		"oiax: promote test to qa (#9)\n\n"+
			"* first squashed change\n\n(cherry picked from commit 1111111111111111111111111111111111111111)\n\n"+
			"* second squashed change\n\n(cherry picked from commit 2222222222222222222222222222222222222222)")
	short, err := r.ShortSHA(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	a := engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
		Unpromoted: 1, Branch: engine.BackflowBranchName("main", "development", short),
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
	if len(f.conflictCreated) != 1 {
		t.Fatalf("want 1 created artifact, got %d", len(f.conflictCreated))
	}
	body := f.conflictCreated[0].Body
	for _, want := range []string{"combines 2 cherry-picked commits", "squash merge", "`Oiax-Backflow: skip`"} {
		if !strings.Contains(body, want) {
			t.Errorf("squash conflict artifact body missing %q:\n%s", want, body)
		}
	}
}

func TestApplyMergeBackflowConflictCreatesArtifact(t *testing.T) {
	// The merge strategy attempts the whole source set, so the body notes the
	// whole set was attempted rather than a per-commit clean count.
	r, commit := gitHarness(t)
	checkout(t, r, "development")
	commitOn(t, r.Dir, "app.txt", "dev-change\n", "dev edit")
	checkout(t, r, "main")
	commit("app.txt", "hotfix-change\n", "hotfix on main")
	short, err := r.ShortSHA(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	a := engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
		Unpromoted: 1, Strategy: v1.BackflowStrategyMerge,
		Branch: engine.BackflowBranchName("main", "development", short),
	}
	f := &fakeForge{}
	c := &Coordinator{Git: r, Forge: f, Graph: mergeGraph()}

	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Divergence || res.Conflicts != 1 {
		t.Errorf("result = %+v, want Divergence=true Conflicts=1", res)
	}
	if len(f.conflictCreated) != 1 || !strings.Contains(f.conflictCreated[0].Body, "whole downstream source set") {
		t.Errorf("merge conflict body = %q, want a whole-set note", f.conflictCreated)
	}
}

func TestApplyBackflowConflictAdoptsSameHead(t *testing.T) {
	// A repeated identical run (same head, still conflicting) adopts the existing
	// artifact: no duplicate, no write, and Conflicts is not double-counted.
	r, _, a := conflictHarness(t)
	f := &fakeForge{}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	if _, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}}); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if len(f.conflicts) != 1 {
		t.Fatalf("first apply: want 1 artifact, got %d", len(f.conflicts))
	}
	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if len(f.conflicts) != 1 {
		t.Errorf("second apply created a duplicate: %d artifacts", len(f.conflicts))
	}
	if res.Conflicts != 0 {
		t.Errorf("adopt must not count a new conflict: Conflicts = %d, want 0", res.Conflicts)
	}
	if len(f.conflictUpdated) != 0 {
		t.Errorf("adopt must not write: %d updates", len(f.conflictUpdated))
	}
	if len(f.conflictClosed) != 0 {
		t.Errorf("adopt must not close a still-conflicting artifact: %v", f.conflictClosed)
	}
}

func TestApplyBackflowConflictAdvancesHeadInPlace(t *testing.T) {
	// The source head advances while still conflicting: the artifact is updated
	// in place (same id) to the live tip; no new artifact, Conflicts not bumped.
	r, commit, a := conflictHarness(t)
	f := &fakeForge{}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	if _, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}}); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	firstHead, _ := r.Head(context.Background(), "main")
	if len(f.conflicts) != 1 || f.conflicts[0].SourceHead != firstHead {
		t.Fatalf("first apply artifact = %+v, want head %s", f.conflicts, firstHead)
	}
	id := f.conflicts[0].ID

	// A second conflicting hotfix advances main; re-derive the branch name.
	commit("app.txt", "hotfix-change-2\n", "second conflicting hotfix on main")
	newHead, _ := r.Head(context.Background(), "main")
	short, _ := r.ShortSHA(context.Background(), "main")
	a2 := a
	a2.Branch = engine.BackflowBranchName("main", "development", short)

	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a2}})
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if len(f.conflicts) != 1 {
		t.Fatalf("advance created a duplicate: %d artifacts", len(f.conflicts))
	}
	if f.conflicts[0].ID != id {
		t.Errorf("advance changed the id: got %s, want %s (update in place)", f.conflicts[0].ID, id)
	}
	if f.conflicts[0].SourceHead != newHead {
		t.Errorf("advance recorded head = %s, want the live tip %s", f.conflicts[0].SourceHead, newHead)
	}
	if res.Conflicts != 0 {
		t.Errorf("an advance is not a new artifact: Conflicts = %d, want 0", res.Conflicts)
	}
	if len(f.conflictUpdated) != 1 {
		t.Errorf("advance must update in place: %d updates", len(f.conflictUpdated))
	}
}

func TestApplyBackflowConflictLeavesStalerRunAlone(t *testing.T) {
	// This run observes an OLDER head than an artifact already records (a racing
	// run got to the newer head first): never regress the record.
	r, commit := gitHarness(t)
	checkout(t, r, "development")
	commitOn(t, r.Dir, "app.txt", "dev-change\n", "dev edit")
	checkout(t, r, "main")
	h1 := commit("app.txt", "hotfix-change\n", "first conflicting hotfix")
	h2 := commit("app.txt", "hotfix-change-2\n", "second conflicting hotfix")
	// Reset main to h1: the live head is now the ancestor of the recorded h2.
	// Move off main first — a checked-out branch cannot be force-updated.
	checkout(t, r, "development")
	gitExec(t, r.Dir, "branch", "-f", "main", h1)

	short, _ := r.ShortSHA(context.Background(), "main")
	a := engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
		Unpromoted: 1, Branch: engine.BackflowBranchName("main", "development", short),
	}
	f := &fakeForge{}
	f.seedConflict("3", "main", "development", h2)
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Divergence {
		t.Error("want Divergence true")
	}
	if len(f.conflictUpdated) != 0 {
		t.Errorf("a staler run must not regress the record: %d updates", len(f.conflictUpdated))
	}
	if f.conflicts[0].SourceHead != h2 {
		t.Errorf("recorded head = %s, want it left at the newer %s", f.conflicts[0].SourceHead, h2)
	}
	if res.Conflicts != 0 {
		t.Errorf("leave-alone must not count a new conflict: %d", res.Conflicts)
	}
	// The sweep must not close it either — the edge is still conflicting.
	if len(f.conflictClosed) != 0 {
		t.Errorf("a still-conflicting edge must not be swept closed: %v", f.conflictClosed)
	}
}

func TestApplyBackflowConflictAdvancesDivergedHead(t *testing.T) {
	// The recorded head is neither an ancestor nor a descendant of the live head
	// (a diverged force-push) but still resolves: the live tip is authoritative,
	// so the artifact is updated in place to it.
	r, commit := gitHarness(t)
	// A divergent side commit off the base — neither ancestor nor descendant of
	// main's conflicting head.
	gitExec(t, r.Dir, "switch", "-q", "-c", "side", "development")
	sideHead := commitOn(t, r.Dir, "side.txt", "s\n", "divergent side commit")

	checkout(t, r, "development")
	commitOn(t, r.Dir, "app.txt", "dev-change\n", "dev edit")
	checkout(t, r, "main")
	h1 := commit("app.txt", "hotfix-change\n", "conflicting hotfix")

	short, _ := r.ShortSHA(context.Background(), "main")
	a := engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
		Unpromoted: 1, Branch: engine.BackflowBranchName("main", "development", short),
	}
	f := &fakeForge{}
	f.seedConflict("2", "main", "development", sideHead)
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Divergence {
		t.Error("want Divergence true")
	}
	if len(f.conflictUpdated) != 1 {
		t.Fatalf("a diverged-but-resolvable head must update in place to the live tip: %d updates", len(f.conflictUpdated))
	}
	if f.conflicts[0].SourceHead != h1 {
		t.Errorf("recorded head = %s, want the live tip %s (live-tip-authoritative)", f.conflicts[0].SourceHead, h1)
	}
}

func TestApplyBackflowConflictUpdatesRewrittenAwayHeadNonShallow(t *testing.T) {
	// The recorded head no longer resolves on a full clone (history rewritten):
	// the live tip is authoritative, so the artifact advances to it.
	r, _, a := conflictHarness(t)
	f := &fakeForge{}
	f.seedConflict("4", "main", "development", "0123456789abcdef0123456789abcdef01234567")
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	if _, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	mainHead, _ := r.Head(context.Background(), "main")
	if len(f.conflictUpdated) != 1 || f.conflicts[0].SourceHead != mainHead {
		t.Errorf("a rewritten-away head on a full clone must advance to the live tip %s: %+v", mainHead, f.conflicts)
	}
}

func TestApplyBackflowConflictLeavesRewrittenAwayHeadOnShallowClone(t *testing.T) {
	// On a shallow clone "absent" is indistinguishable from "never fetched", so a
	// recorded head that does not resolve is left alone (the supersede caveat).
	r, _, a := conflictHarness(t)
	head := gitExec(t, r.Dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(r.Dir, ".git", "shallow"), []byte(head+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeForge{}
	f.seedConflict("4", "main", "development", "0123456789abcdef0123456789abcdef01234567")
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	if _, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(f.conflictUpdated) != 0 {
		t.Errorf("a shallow clone cannot tell rewrite from never-fetched: must leave alone, got %d updates", len(f.conflictUpdated))
	}
	if f.conflicts[0].SourceHead != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("recorded head must be left untouched on a shallow clone, got %s", f.conflicts[0].SourceHead)
	}
}

func TestApplyBackflowConflictConsolidatesDuplicates(t *testing.T) {
	// Two open artifacts for the same (source, target): the run keeps the
	// lowest-numbered as canonical and closes the other with a consolidating
	// comment (the read-path consolidation guarantee).
	r, _, a := conflictHarness(t)
	mainHead, _ := r.Head(context.Background(), "main")
	f := &fakeForge{}
	f.seedConflict("2", "main", "development", mainHead)
	f.seedConflict("5", "main", "development", mainHead)
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Divergence {
		t.Error("want Divergence true")
	}
	closedDup := false
	for _, id := range f.conflictClosed {
		if id == "5" {
			closedDup = true
		}
		if id == "2" {
			t.Errorf("consolidation closed the canonical (lowest-numbered) artifact")
		}
	}
	if !closedDup {
		t.Errorf("consolidation did not close the duplicate id 5: closed=%v", f.conflictClosed)
	}
	if res.Conflicts != 0 {
		t.Errorf("Conflicts = %d, want 0 (adopt the canonical, close the dup)", res.Conflicts)
	}
	if len(f.conflicts) != 1 || f.conflicts[0].ID != "2" {
		t.Errorf("after consolidation want only the canonical id 2, got %+v", f.conflicts)
	}
}

func TestApplyBackflowConflictSweepConsolidatesDuplicates(t *testing.T) {
	// A quiescent run (no backflow action) with two open artifacts for the same
	// edge: the sweep's OWN read-path consolidation collapses the duplicate
	// (closes the higher-numbered) and then closes the canonical as resolved.
	// The record path never runs here, so this exercises canonicalConflictArtifacts
	// inside resolveBackflowConflicts — the path the record-path consolidation
	// test cannot reach.
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	mainHead := commit("hotfix.txt", "urgent\n", "hotfix on main")
	f := &fakeForge{}
	f.seedConflict("4", "main", "development", mainHead)
	f.seedConflict("7", "main", "development", mainHead)
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	if _, err := c.Apply(context.Background(), engine.Plan{}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	closed := map[forge.ConflictArtifactID]bool{}
	for _, id := range f.conflictClosed {
		closed[id] = true
	}
	if !closed["7"] {
		t.Errorf("sweep did not consolidate the duplicate id 7: closed=%v", f.conflictClosed)
	}
	if !closed["4"] {
		t.Errorf("sweep did not close the canonical id 4 as resolved: closed=%v", f.conflictClosed)
	}
	if len(f.conflicts) != 0 {
		t.Errorf("both artifacts should be closed, got open: %+v", f.conflicts)
	}
}

func TestApplyBackflowConflictSweepClosesOnResolveBySuccess(t *testing.T) {
	// A later run whose replay applies cleanly leaves the edge out of the
	// conflicted set; the end-of-Apply sweep closes the resolved artifact.
	r, _, a := conflictHarness(t)
	f := &fakeForge{createResult: engine.ChangeRequest{ID: "1", Type: engine.RequestTypeBackflow}}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	if _, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}}); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if len(f.conflicts) != 1 {
		t.Fatalf("run 1: want 1 open artifact, got %d", len(f.conflicts))
	}
	id := f.conflicts[0].ID

	// Resolve the conflict: development returns app.txt to the base content, so
	// the hotfix now cherry-picks cleanly.
	checkout(t, r, "development")
	commitOn(t, r.Dir, "app.txt", "v0\n", "revert dev edit so the hotfix applies")
	checkout(t, r, "main")

	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if res.Divergence {
		t.Error("run 2 replay applied cleanly; want Divergence false")
	}
	if len(f.conflictClosed) != 1 || f.conflictClosed[0] != id {
		t.Errorf("sweep did not close the resolved artifact %s: closed=%v", id, f.conflictClosed)
	}
	if len(f.conflicts) != 0 {
		t.Errorf("the resolved artifact should be closed/removed: %+v", f.conflicts)
	}
}

func TestApplyBackflowConflictSweepClosesOnConvergence(t *testing.T) {
	// The hotfix already reached development by content, so ToReturn is empty at
	// apply and applyBackflow converges (records no conflict); the sweep closes
	// the leftover artifact.
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	hotfix := commit("hotfix.txt", "urgent\n", "hotfix on main")
	checkout(t, r, "development")
	commitOn(t, r.Dir, "dev.txt", "dev\n", "unrelated dev commit")
	gitExec(t, r.Dir, "cherry-pick", hotfix) // development now carries the hotfix by content
	checkout(t, r, "main")

	mainHead, _ := r.Head(context.Background(), "main")
	f := &fakeForge{}
	f.seedConflict("3", "main", "development", mainHead)
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	a := engine.Action{
		Type: engine.ActionCreateBackflowRequest, From: "main", To: "development", Unpromoted: 1,
	}
	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Divergence {
		t.Error("a converged edge must not report divergence")
	}
	if len(f.conflictClosed) != 1 || f.conflictClosed[0] != "3" {
		t.Errorf("the converged edge's artifact should be swept closed: closed=%v", f.conflictClosed)
	}
}

func TestApplyBackflowConflictSweepClosesOnQuiescence(t *testing.T) {
	// A run that emits no backflow action for the edge (quiescence) still closes
	// a leftover artifact: the edge did not re-record a conflict this run.
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	mainHead := commit("hotfix.txt", "urgent\n", "hotfix on main")
	f := &fakeForge{}
	f.seedConflict("3", "main", "development", mainHead)
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	if _, err := c.Apply(context.Background(), engine.Plan{}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(f.conflictClosed) != 1 || f.conflictClosed[0] != "3" {
		t.Errorf("a quiescent edge's artifact should be swept closed: closed=%v", f.conflictClosed)
	}
}

func TestApplyBackflowConflictSweepClosesOrphanNonShallow(t *testing.T) {
	// The backflow source branch is gone and its recorded head no longer resolves
	// on a full clone: an orphan the sweep closes (it can never converge).
	r, _ := gitHarness(t)
	checkout(t, r, "development")
	gitExec(t, r.Dir, "branch", "-D", "main")

	f := &fakeForge{}
	f.seedConflict("3", "main", "development", "0123456789abcdef0123456789abcdef01234567")
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	if _, err := c.Apply(context.Background(), engine.Plan{}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(f.conflictClosed) != 1 || f.conflictClosed[0] != "3" {
		t.Errorf("an orphan (source gone, non-shallow) should be closed: closed=%v", f.conflictClosed)
	}
}

func TestApplyBackflowConflictSweepLeavesOrphanOnShallowClone(t *testing.T) {
	// The same orphan on a shallow clone is left alone: "absent" cannot be told
	// from "never fetched", so closing would risk a false "history rewritten".
	r, _ := gitHarness(t)
	checkout(t, r, "development")
	gitExec(t, r.Dir, "branch", "-D", "main")
	head := gitExec(t, r.Dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(r.Dir, ".git", "shallow"), []byte(head+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeForge{}
	f.seedConflict("3", "main", "development", "0123456789abcdef0123456789abcdef01234567")
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	if _, err := c.Apply(context.Background(), engine.Plan{}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(f.conflictClosed) != 0 {
		t.Errorf("an orphan on a shallow clone must be left alone: closed=%v", f.conflictClosed)
	}
}

func TestApplyBackflowConflictSweepLeavesUnconfiguredEdgeAlone(t *testing.T) {
	// An artifact for a source that is no longer a configured backflow source is
	// left for manual dismissal — a run that does not examine the edge must not
	// close its artifact on stale info.
	r, _ := gitHarness(t)
	f := &fakeForge{}
	f.seedConflict("3", "qa", "development", "0123456789abcdef0123456789abcdef01234567")
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	if _, err := c.Apply(context.Background(), engine.Plan{}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(f.conflictClosed) != 0 {
		t.Errorf("an artifact for an unconfigured edge must be left alone: closed=%v", f.conflictClosed)
	}
	if len(f.conflicts) != 1 {
		t.Errorf("the unconfigured artifact must remain open: %+v", f.conflicts)
	}
}

func TestApplyMergeBackflowReportDivergenceKeepsArtifactOpen(t *testing.T) {
	// A merge-strategy backflow edge can diverge via ActionReportDivergence
	// (planMergeBackflow's ADR-0006 amendments) without ever entering
	// applyBackflow. The edge is still conflicting, so the sweep must NOT close
	// its durable artifact even though the recorded head equals the live head.
	r, _ := gitHarness(t)
	checkout(t, r, "main")
	mainHead, err := r.Head(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeForge{}
	f.seedConflict("3", "main", "development", mainHead)
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	// Simulate the amendment divergence the merge planner emits for the backflow
	// edge (main -> development, the configured backflow source -> target).
	a := engine.Action{
		Type: engine.ActionReportDivergence, From: "main", To: "development",
		Reason: "backflow main->development: strategy merge cannot honor the Oiax-Backflow: skip trailer",
	}
	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Divergence {
		t.Error("a reported backflow divergence must set res.Divergence")
	}
	if len(f.conflictClosed) != 0 {
		t.Errorf("a still-diverging backflow edge's artifact must stay open: closed=%v", f.conflictClosed)
	}
	if len(f.conflicts) != 1 {
		t.Errorf("the artifact must remain open for the still-diverging edge: %+v", f.conflicts)
	}
}

func TestApplyBackflowConflictBestEffortForgeErrorPreservesExit3(t *testing.T) {
	// A best-effort forge write failure on the conflict path must NOT surface as
	// an Apply error (exit 1) and must NOT downgrade the exit-3 divergence.
	// Critically, a pre-existing artifact for the SAME edge is NOT false-closed
	// by the sweep: the edge was marked conflicted before the failing write.
	r, commit, a := conflictHarness(t)
	firstHead, _ := r.Head(context.Background(), "main")
	// A second conflicting hotfix advances main, so the record path takes the
	// in-place UPDATE branch — which the fake fails.
	commit("app.txt", "hotfix-change-2\n", "second conflicting hotfix")
	short, _ := r.ShortSHA(context.Background(), "main")
	a.Branch = engine.BackflowBranchName("main", "development", short)

	f := &fakeForge{updateConflictErr: errors.New("503 forge unavailable")}
	f.seedConflict("3", "main", "development", firstHead)
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatalf("apply must swallow the best-effort forge error, got %v", err)
	}
	if !res.Divergence {
		t.Error("the exit-3 divergence must be preserved despite the forge error")
	}
	if len(f.conflictClosed) != 0 {
		t.Errorf("a still-conflicting edge's artifact must never be swept closed: closed=%v", f.conflictClosed)
	}
	if len(f.conflicts) != 1 || f.conflicts[0].ID != "3" {
		t.Errorf("the pre-existing artifact must remain open: %+v", f.conflicts)
	}
}

func TestApplyBackflowConflictNilMapSafety(t *testing.T) {
	// A first-ever conflict on a fresh Apply records the artifact without a
	// nil-map write panic: the conflicted set is initialized in Apply, before any
	// conflict branch can write it.
	r, _, a := conflictHarness(t)
	f := &fakeForge{}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}

	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Divergence || res.Conflicts != 1 {
		t.Errorf("result = %+v, want Divergence=true Conflicts=1", res)
	}
}

// testTemplates resolves a template Set the way loadGraph does, from a bare
// document carrying only the template configuration (SKA-54).
func testTemplates(t *testing.T, ts *v1.Templates, promotions ...v1.Promotion) *tmpl.Set {
	t.Helper()
	s, err := tmpl.Resolve(&v1.PromotionGraph{Spec: v1.PromotionGraphSpec{
		Templates:  ts,
		Promotions: promotions,
	}}, nil)
	if err != nil {
		t.Fatalf("resolve templates: %v", err)
	}
	return s
}

// A customized promotion template gets the FULL variable context on create:
// the post-ladder commit list (subjects included), the live count, and the
// short source head — none of which the built-in text needs.
func TestApplyCreatePromotionRendersCustomTemplate(t *testing.T) {
	r, _ := gitHarness(t)
	checkout(t, r, "development")
	commitOn(t, r.Dir, "feature.txt", "new\n", "feat: custom text feature")

	f := &fakeForge{}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph(), Templates: testTemplates(t, &v1.Templates{
		Promotion: &v1.RequestTemplate{
			Title: "change record {{.From}} -> {{.To}}",
			Body:  "count={{.Count}}{{range .Commits}} [{{.ShortSHA}} {{.Subject}}]{{end}} head={{.SourceHeadShort}}",
		},
	})}

	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := c.Apply(context.Background(), plan); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(f.created) != 1 {
		t.Fatalf("created = %d requests, want 1: %+v", len(f.created), f.created)
	}
	req := f.created[0]
	if want := "change record development -> test"; req.Title != want {
		t.Errorf("title = %q, want %q", req.Title, want)
	}
	short, err := r.ShortSHA(context.Background(), "development")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.Body, "count=1") ||
		!strings.Contains(req.Body, "feat: custom text feature") ||
		!strings.Contains(req.Body, "head="+short) {
		t.Errorf("body = %q, want live count, commit subject, and short head", req.Body)
	}
}

// The default templates keep the promotion create path cheap: no commit
// derivation, and output identical to the pre-template text (also pinned in
// internal/tmpl; this guards the wiring end to end).
func TestApplyCreatePromotionDefaultTextUnchanged(t *testing.T) {
	r, _ := gitHarness(t)
	checkout(t, r, "development")
	commitOn(t, r.Dir, "feature.txt", "new\n", "feat: default text")

	f := &fakeForge{}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := c.Apply(context.Background(), plan); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(f.created) != 1 {
		t.Fatalf("created = %d, want 1", len(f.created))
	}
	if want := "oiax: promote development to test"; f.created[0].Title != want {
		t.Errorf("title = %q, want %q", f.created[0].Title, want)
	}
	wantBody := "Oiax opened this request to promote 1 commit(s) from `development` into `test`.\n\n" +
		"This request is managed by Oiax. Do not edit the metadata block below."
	if f.created[0].Body != wantBody {
		t.Errorf("body = %q, want the built-in text unchanged", f.created[0].Body)
	}
}

// The merge backflow strategy renders the configured merge-commit message
// into the --no-ff merge commit, and the custom backflow body renders with
// the return set's commit context.
func TestApplyMergeBackflowRendersMessageAndBodyTemplates(t *testing.T) {
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	commit("hotfix.txt", "urgent\n", "hotfix on main")

	f := &fakeForge{createResult: engine.ChangeRequest{ID: "1", Type: engine.RequestTypeBackflow}}
	c := &Coordinator{Git: r, Forge: f, Graph: mergeGraph(), Templates: testTemplates(t, &v1.Templates{
		Backflow: &v1.RequestTemplate{
			Body: "returning {{.Count}} via {{.Mechanism}}:{{range .Commits}} {{.Subject}};{{end}}",
		},
		BackflowMergeMessage: &v1.TextTemplate{
			Text: "backflow: {{.From}} -> {{.To}} ({{.Count}} commit(s))\n\nGraph: {{.Graph}}",
		},
	})}

	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := c.Apply(context.Background(), plan); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(f.pushed) != 1 || len(f.created) != 1 {
		t.Fatalf("want 1 push and 1 create, got pushed=%d created=%d", len(f.pushed), len(f.created))
	}
	msg := gitExec(t, r.Dir, "log", "-1", "--format=%B", f.pushed[0].SHA)
	if want := "backflow: main -> development (1 commit(s))\n\nGraph: environments"; strings.TrimRight(msg, "\n") != want {
		t.Errorf("merge commit message = %q, want %q", msg, want)
	}
	body := f.created[0].Body
	if !strings.Contains(body, "returning 1 via merge commit:") || !strings.Contains(body, "hotfix on main;") {
		t.Errorf("backflow body = %q, want mechanism and commit subject", body)
	}
}

// The durable conflict artifact renders the configured template with the
// failing-commit context; the untrusted subject stays in the human body
// (the marker fields the fake records are unchanged).
func TestBackflowConflictRendersCustomTemplate(t *testing.T) {
	r, _, a := conflictHarness(t)
	f := &fakeForge{}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph(), Templates: testTemplates(t, &v1.Templates{
		BackflowConflict: &v1.RequestTemplate{
			Title: "STUCK {{.From}} -> {{.To}}",
			Body:  "failing={{.Conflict.SHA}} subject={{.Conflict.Subject}} applied={{.Conflict.Applied}} whole={{.Conflict.Whole}}",
		},
	})}

	res, err := c.Apply(context.Background(), engine.Plan{Actions: []engine.Action{a}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Divergence || res.Conflicts != 1 || len(f.conflictCreated) != 1 {
		t.Fatalf("res=%+v conflictCreated=%d, want divergence with one artifact", res, len(f.conflictCreated))
	}
	spec := f.conflictCreated[0]
	if want := "STUCK main -> development"; spec.Title != want {
		t.Errorf("title = %q, want %q", spec.Title, want)
	}
	mainHead, err := r.Head(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(spec.Body, "failing="+mainHead) || !strings.Contains(spec.Body, "subject=hotfix on main") {
		t.Errorf("body = %q, want the failing commit context", spec.Body)
	}
	if !strings.Contains(spec.Body, "applied=0 whole=false") {
		t.Errorf("body = %q, want cherry-pick conflict details", spec.Body)
	}
}
