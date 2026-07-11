package reconcile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/git"
	"github.com/skaphos/oiax/pkg/api/v1alpha1"
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

	createResult engine.ChangeRequest
	createErr    error
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

// testGraph is the three-branch graph the tests plan against.
func testGraph() *engine.Graph {
	return &engine.Graph{
		Name: "environments",
		Branches: map[string]engine.Branch{
			"development": {Role: v1alpha1.RoleSource, Drift: v1alpha1.DriftForbidden},
			"test":        {Drift: v1alpha1.DriftForbidden},
			"main":        {Role: v1alpha1.RoleTerminal, Drift: v1alpha1.DriftForbidden},
		},
		Promotions: []engine.Promotion{
			{From: "development", To: "test"},
			{From: "test", To: "main"},
		},
		Backflow: engine.BackflowPolicy{
			Enabled:  true,
			Sources:  []string{"main"},
			Target:   "development",
			Strategy: v1alpha1.BackflowStrategyCherryPick,
		},
	}
}

// gitHarness spins up a real repository so the coordinator observes actual
// git state. It returns the runner and a commit helper.
func gitHarness(t *testing.T) (*git.Runner, func(file, content, msg string) string) {
	t.Helper()
	dir := t.TempDir()
	runGit := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	runGit("init", "-q", "-b", "main")
	runGit("config", "user.name", "test")
	runGit("config", "user.email", "test@example.invalid")

	commit := func(file, content, msg string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit("add", ".")
		runGit("commit", "-q", "-m", msg)
		return runGit("rev-parse", "HEAD")
	}

	// Base commit shared by all three branches.
	commit("app.txt", "v0\n", "c0")
	runGit("branch", "development")
	runGit("branch", "test")

	// Expose the raw runner over the same directory.
	return &git.Runner{Dir: dir}, func(file, content, msg string) string {
		t.Helper()
		return commitOn(t, dir, file, content, msg)
	}
}

// commitOn commits on the currently checked-out branch of dir.
func commitOn(t *testing.T, dir, file, content, msg string) string {
	t.Helper()
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", msg)
	return run("rev-parse", "HEAD")
}

func checkout(t *testing.T, r *git.Runner, branch string) {
	t.Helper()
	cmd := exec.Command("git", "checkout", "-q", branch)
	cmd.Dir = r.Dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout %s: %v\n%s", branch, err, out)
	}
}

// gitExec runs a raw git command in dir, failing the test on error.
func gitExec(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
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
}
