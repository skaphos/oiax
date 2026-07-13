package reconcile

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/git"
	"github.com/skaphos/oiax/internal/gittest"
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

func (f *fakeForge) ListManagedRequests(_ context.Context, filter forge.RequestFilter) ([]engine.ChangeRequest, error) {
	if filter.State == forge.RequestStateMerged {
		return f.merged, nil
	}
	return f.open, nil
}

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
}

func TestPlanBackflowSkipTrailerSuppresses(t *testing.T) {
	// A hotfix on main carrying the O6 'Oiax-Backflow: skip' trailer is
	// intentionally not returned: the identity ladder honors it, so ToReturn
	// is empty and no backflow action is planned.
	r, commit := gitHarness(t)
	checkout(t, r, "main")
	commit("hotfix.txt", "urgent\n", "hotfix on main\n\nOiax-Backflow: skip")

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
	if _, err := c.Apply(context.Background(), plan); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if len(f.closed) != 1 || f.closed[0] != forge.RequestID("older") {
		t.Fatalf("closed = %v, want only the strictly-older request %q", f.closed, "older")
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
func diamondGraph() *engine.Graph {
	return &engine.Graph{
		Name: "environments",
		Branches: map[string]engine.Branch{
			"development": {Role: v1.RoleSource, Drift: v1.DriftForbidden},
			"test":        {Drift: v1.DriftForbidden},
			"qa":          {Drift: v1.DriftForbidden},
			"main":        {Role: v1.RoleTerminal, Drift: v1.DriftForbidden},
		},
		Promotions: []engine.Promotion{
			{From: "development", To: "test"},
			{From: "development", To: "qa"},
			{From: "test", To: "main"},
			{From: "qa", To: "main"},
		},
		Backflow: engine.BackflowPolicy{
			Enabled:  true,
			Sources:  []string{"main"},
			Target:   "development",
			Strategy: v1.BackflowStrategyCherryPick,
		},
	}
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
	g := diamondGraph()
	if errs := g.Validate(); len(errs) > 0 {
		t.Fatalf("diamond graph must be valid: %v", errs)
	}

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
		Log:   NewLogger("text", nil, &logBuf),
	}
	if _, err := c.Plan(context.Background()); err != nil {
		t.Fatalf("plan: %v", err)
	}
	log := logBuf.String()
	if !strings.Contains(log, "shallow clone") || !strings.Contains(log, "fetch-depth: 0") {
		t.Fatalf("Plan did not warn about the shallow clone: %q", log)
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
	if _, err := c.Apply(context.Background(), plan); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(f.closed) != 1 || f.closed[0] != forge.RequestID("orphan") {
		t.Fatalf("closed = %v, want the orphaned request %q closed", f.closed, "orphan")
	}
	if len(f.deleted) != 1 || f.deleted[0] != orphanBranch {
		t.Fatalf("deleted = %v, want the orphan head branch %q deleted", f.deleted, orphanBranch)
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
