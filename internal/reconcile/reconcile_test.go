package reconcile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/git"
	"github.com/skaphos/oiax/pkg/api/v1alpha1"
)

// fakeForge records mutations and serves canned managed-request lists.
type fakeForge struct {
	open   []engine.ChangeRequest
	merged []engine.ChangeRequest

	created []forge.CreateRequest
	updated []forge.UpdateRequest
	closed  []forge.RequestID

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

func (f *fakeForge) PushBranch(context.Context, forge.BranchPush) error {
	return forge.ErrNotImplemented
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
	// main gains a hotfix commit not present on test; test->main reports
	// divergence (main has drift forbidden).
	r, _ := gitHarness(t)
	checkout(t, r, "main")
	commitOn(t, r.Dir, "hotfix.txt", "urgent\n", "hotfix on main")

	c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	var reports int
	for _, a := range plan.Actions {
		if a.Type == engine.ActionReportDivergence && a.From == "test" && a.To == "main" {
			reports++
		}
	}
	if reports != 1 {
		t.Fatalf("want 1 report-divergence for test->main, got %d: %+v", reports, plan.Actions)
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
	r, _ := gitHarness(t)
	checkout(t, r, "main")
	commitOn(t, r.Dir, "hotfix.txt", "urgent\n", "hotfix on main")

	f := &fakeForge{}
	c := &Coordinator{Git: r, Forge: f, Graph: testGraph()}
	plan, err := c.Plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	res, err := c.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Divergence {
		t.Error("want Divergence true")
	}
	if len(f.created)+len(f.updated)+len(f.closed) != 0 {
		t.Error("report-divergence must not call the forge")
	}
}

func TestApplyRejectsBackflowAction(t *testing.T) {
	c := &Coordinator{Git: &git.Runner{}, Forge: &fakeForge{}, Graph: testGraph()}
	plan := engine.Plan{Actions: []engine.Action{{Type: engine.ActionCreateBackflowRequest, From: "main", To: "development"}}}
	if _, err := c.Apply(context.Background(), plan); err == nil {
		t.Fatal("want error for backflow action in v0.1 plan")
	}
}
