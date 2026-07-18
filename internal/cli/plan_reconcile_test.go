package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/gittest"
)

// fakeForge is an in-memory forge for exercising plan/reconcile wiring.
type fakeForge struct {
	open    []engine.ChangeRequest
	merged  []engine.ChangeRequest
	created []forge.CreateRequest
	updated []forge.UpdateRequest
	closed  []forge.RequestID
	pushed  []forge.BranchPush
	deleted []string
}

func (f *fakeForge) ListManagedRequests(_ context.Context, filter forge.RequestFilter) ([]engine.ChangeRequest, error) {
	if filter.State == forge.RequestStateMerged {
		return f.merged, nil
	}
	return f.open, nil
}

func (f *fakeForge) CreateRequest(_ context.Context, req forge.CreateRequest) (engine.ChangeRequest, error) {
	f.created = append(f.created, req)
	return engine.ChangeRequest{ID: "1", Type: req.Type, Source: req.Source, Target: req.Target, SourceHead: req.SourceHead}, nil
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

func (f *fakeForge) RepoMergeMethods(context.Context) (forge.MergeMethods, error) {
	return forge.MergeMethods{Merge: true, Squash: true, Rebase: true}, nil
}

func (f *fakeForge) TargetMergeMethods(context.Context, string) (forge.MergeMethods, error) {
	return forge.MergeMethods{Merge: true, Squash: true, Rebase: true}, nil
}

func (f *fakeForge) DeleteBranch(_ context.Context, name string) error {
	f.deleted = append(f.deleted, name)
	return nil
}

// Conflict-artifact stubs sufficient to satisfy forge.Forge.
func (f *fakeForge) ListConflictArtifacts(_ context.Context, _ string) ([]forge.ConflictArtifact, error) {
	return nil, nil
}

func (f *fakeForge) CreateConflictArtifact(_ context.Context, _ forge.ConflictArtifactSpec) (forge.ConflictArtifact, error) {
	return forge.ConflictArtifact{}, nil
}

func (f *fakeForge) UpdateConflictArtifact(_ context.Context, _ forge.ConflictArtifactID, _ forge.ConflictArtifactSpec) error {
	return nil
}

func (f *fakeForge) CloseConflictArtifact(_ context.Context, _ forge.ConflictArtifactID, _ forge.Reason) error {
	return nil
}

// useForge substitutes the package forge factory for the duration of a test.
func useForge(t *testing.T, f forge.Forge) {
	t.Helper()
	prev := newForge
	newForge = func(context.Context, *slog.Logger) (forge.Forge, error) { return f, nil }
	t.Cleanup(func() { newForge = prev })
}

// setupRepo creates a temp git repo with three branches (development, test,
// main) at a shared base commit, writes .oiax.yaml, chdirs into it, and
// clears Actions env so output stays deterministic. It returns a git runner.
func setupRepo(t *testing.T) func(args ...string) {
	t.Helper()
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	t.Setenv("OIAX_LOG_FORMAT", "")

	dir := t.TempDir()
	t.Chdir(dir)
	run := func(args ...string) {
		t.Helper()
		gittest.Run(t, dir, args...)
	}
	writeFile := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gittest.InitRepo(t, dir)
	writeFile("app.txt", "v0\n")
	run("add", ".")
	run("commit", "-q", "-m", "c0")
	run("branch", "development")
	run("branch", "test")
	writeFile(".oiax.yaml", exampleConfig)

	return func(args ...string) {
		if len(args) >= 2 && args[0] == "write" {
			writeFile(args[1], args[2])
			return
		}
		run(args...)
	}
}

func runCode(t *testing.T, args ...string) (string, int) {
	t.Helper()
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	if err == nil {
		return out.String(), 0
	}
	var ece exitCodeError
	if errors.As(err, &ece) {
		// Mirror Execute: a non-empty message is printed to stderr.
		if ece.msg != "" {
			out.WriteString(ece.msg + "\n")
		}
		return out.String(), ece.code
	}
	// Surface the generic error text so a failing test can explain itself.
	out.WriteString(err.Error() + "\n")
	return out.String(), 1
}

func TestPlanInSyncExitsZero(t *testing.T) {
	setupRepo(t)
	useForge(t, &fakeForge{})

	out, code := runCode(t, "plan")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "In sync, no actions.") {
		t.Errorf("output = %q", out)
	}

	// --detailed-exitcode is still 0 when in sync.
	if _, code := runCode(t, "plan", "--detailed-exitcode"); code != 0 {
		t.Errorf("detailed-exitcode in sync = %d, want 0", code)
	}
}

func TestPlanPendingDetailedExitCode(t *testing.T) {
	git := setupRepo(t)
	git("checkout", "-q", "development")
	git("write", "app.txt", "v1\n")
	git("add", ".")
	git("commit", "-q", "-m", "c1")
	useForge(t, &fakeForge{})

	// Without the flag: any successful plan is 0.
	if out, code := runCode(t, "plan"); code != 0 {
		t.Fatalf("plan without flag = %d, want 0\n%s", code, out)
	}
	// With the flag: pending actions are exit 2.
	out, code := runCode(t, "plan", "--detailed-exitcode")
	if code != 2 {
		t.Fatalf("plan --detailed-exitcode = %d, want 2\n%s", code, out)
	}
	if !strings.Contains(out, "create") || !strings.Contains(out, "development -> test") {
		t.Errorf("output = %q", out)
	}
}

func TestPlanExitCode(t *testing.T) {
	promo := engine.Action{Type: engine.ActionCreatePromotionRequest}
	backflow := engine.Action{Type: engine.ActionCreateBackflowRequest}
	diverge := engine.Action{Type: engine.ActionReportDivergence}
	cases := []struct {
		name    string
		actions []engine.Action
		want    int
	}{
		{"in sync", nil, 0},
		{"applyable promotion", []engine.Action{promo}, 2},
		{"applyable backflow", []engine.Action{backflow}, 2},
		{"report-only divergence", []engine.Action{diverge}, 3},
		{"divergence dominates applyable changes", []engine.Action{promo, diverge}, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := planExitCode(engine.Plan{Actions: tc.actions}); got != tc.want {
				t.Errorf("planExitCode = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestPlanReportDivergenceDetailedExitCode(t *testing.T) {
	// A commit made directly on a promotion DESTINATION (test) that its source
	// lacks is downstream-only content on a non-backflow branch with no expected
	// drift, so the plan carries a report-only divergence. plan --detailed-exitcode
	// must exit 3 (not 2), matching what reconcile exits for the same state — the
	// M11 alignment.
	git := setupRepo(t)
	git("checkout", "-q", "test")
	git("write", "drift.txt", "x\n")
	git("add", ".")
	git("commit", "-q", "-m", "direct edit on test")
	useForge(t, &fakeForge{})

	if out, code := runCode(t, "plan", "--detailed-exitcode"); code != 3 {
		t.Fatalf("plan --detailed-exitcode with a report-only divergence = %d, want 3\n%s", code, out)
	}
	if out, code := runCode(t, "reconcile"); code != 3 {
		t.Fatalf("reconcile for the same state = %d, want 3\n%s", code, out)
	}
}

func TestPlanJSONShape(t *testing.T) {
	git := setupRepo(t)
	git("checkout", "-q", "development")
	git("write", "app.txt", "v1\n")
	git("add", ".")
	git("commit", "-q", "-m", "c1")
	useForge(t, &fakeForge{})

	// Distinct buffers: structured logs land on stderr by design, and stdout
	// alone must parse as the frozen plan JSON document.
	root := NewRootCommand()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"plan", "--output", "json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("plan --output json: %v\nstderr:\n%s", err, stderr.String())
	}
	var plan struct {
		PlanFormatVersion int    `json:"planFormatVersion"`
		Graph             string `json:"graph"`
		Actions           []struct {
			Type string `json:"type"`
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"actions"`
		Edges []struct {
			From        string `json:"from"`
			To          string `json:"to"`
			Equivalence string `json:"equivalence"`
			InSync      bool   `json:"inSync"`
			Unpromoted  int    `json:"unpromoted"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}
	if plan.PlanFormatVersion != 1 {
		t.Errorf("planFormatVersion = %d, want 1", plan.PlanFormatVersion)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Type != "createPromotionRequest" {
		t.Errorf("actions = %+v", plan.Actions)
	}
	// Every promotion edge appears in the additive edges diagnostics, with the
	// settling rung named — including the in-sync edge that yields no action.
	if len(plan.Edges) != 2 {
		t.Fatalf("edges = %+v, want one summary per promotion edge", plan.Edges)
	}
	diverged, inSync := plan.Edges[0], plan.Edges[1]
	if diverged.From != "development" || diverged.To != "test" || diverged.InSync || diverged.Unpromoted != 1 {
		t.Errorf("diverged edge summary = %+v", diverged)
	}
	if !inSync.InSync || inSync.Equivalence == "" {
		t.Errorf("in-sync edge summary = %+v, want inSync with a settling rung", inSync)
	}
}

// TestPlanJSONWarnsOnStderrNotStdout guards the load-bearing combination the
// shared helpers cannot see: a deprecated v1alpha1 config with --output json.
// ADR 0005 places the deprecation warning on stderr precisely so stdout stays
// machine-clean JSON; runCode merges the two streams, so this test wires
// distinct stdout/stderr buffers to prove the warning never reaches stdout.
func TestPlanJSONWarnsOnStderrNotStdout(t *testing.T) {
	git := setupRepo(t)
	git("checkout", "-q", "development")
	git("write", "app.txt", "v1\n")
	git("add", ".")
	git("commit", "-q", "-m", "c1")
	// Swap the canonical v1 config for the deprecated v1alpha1 alias in the
	// working tree (the ref plan reads when no default branch resolves).
	git("write", ".oiax.yaml", strings.Replace(exampleConfig, "oiax.skaphos.dev/v1", "oiax.skaphos.dev/v1alpha1", 1))
	useForge(t, &fakeForge{})

	root := NewRootCommand()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"plan", "--output", "json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("plan --output json: %v\nstderr:\n%s", err, stderr.String())
	}

	// The deprecation warning lands on stderr, never stdout.
	if !strings.Contains(stderr.String(), "is deprecated") {
		t.Errorf("stderr missing deprecation warning:\n%s", stderr.String())
	}
	if strings.Contains(stdout.String(), "warning:") {
		t.Errorf("warning leaked onto stdout, corrupting JSON:\n%s", stdout.String())
	}
	// stdout alone must parse as valid JSON.
	var plan struct {
		PlanFormatVersion int `json:"planFormatVersion"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}
	if plan.PlanFormatVersion != 1 {
		t.Errorf("planFormatVersion = %d, want 1", plan.PlanFormatVersion)
	}
}

func TestReconcileConvergedExitsZero(t *testing.T) {
	git := setupRepo(t)
	git("checkout", "-q", "development")
	git("write", "app.txt", "v1\n")
	git("add", ".")
	git("commit", "-q", "-m", "c1")
	f := &fakeForge{}
	useForge(t, f)

	out, code := runCode(t, "reconcile")
	if code != 0 {
		t.Fatalf("reconcile exit = %d, want 0\n%s", code, out)
	}
	if len(f.created) != 1 {
		t.Fatalf("want 1 create, got %d", len(f.created))
	}
	if f.created[0].Source != "development" || f.created[0].Target != "test" {
		t.Errorf("create = %+v", f.created[0])
	}
}

func TestReconcileBackflowsHotfixExitsZero(t *testing.T) {
	// A hotfix on main — a backflow source in exampleConfig — is returned to
	// development: reconcile cherry-picks it, force-pushes the deterministic
	// branch, opens a managed backflow request, and exits 0. (This flipped
	// from the old exit-3 report-divergence semantics now that backflow is
	// wired; a genuine exit-3 case is built from a cherry-pick conflict below.)
	git := setupRepo(t)
	git("checkout", "-q", "main")
	git("write", "hotfix.txt", "urgent\n")
	git("add", ".")
	git("commit", "-q", "-m", "hotfix")
	f := &fakeForge{}
	useForge(t, f)

	out, code := runCode(t, "reconcile")
	if code != 0 {
		t.Fatalf("reconcile exit = %d, want 0\n%s", code, out)
	}
	var backflow int
	for _, c := range f.created {
		if c.Type == engine.RequestTypeBackflow {
			backflow++
			if c.Target != "development" {
				t.Errorf("backflow target = %q, want development", c.Target)
			}
			if !strings.HasPrefix(c.Source, "oiax/backflow/main-to-development/") {
				t.Errorf("backflow head branch = %q, want oiax/backflow/main-to-development/ prefix", c.Source)
			}
		}
	}
	if backflow != 1 {
		t.Fatalf("want 1 backflow create, got %d: %+v", backflow, f.created)
	}
	if len(f.pushed) != 1 {
		t.Fatalf("want 1 push, got %d", len(f.pushed))
	}
}

func TestReconcileBackflowConflictExitsThree(t *testing.T) {
	// development and main touch the same file with divergent content, so the
	// hotfix cannot cherry-pick cleanly back onto development. The backflow
	// becomes a reported divergence: exit 3, nothing pushed, no backflow
	// request opened.
	git := setupRepo(t)
	// Add only app.txt (not the untracked .oiax.yaml, which must survive the
	// final checkout so reconcile can read the config from the working tree).
	git("checkout", "-q", "development")
	git("write", "app.txt", "dev-change\n")
	git("add", "app.txt")
	git("commit", "-q", "-m", "dev edit")
	git("checkout", "-q", "main")
	git("write", "app.txt", "hotfix-change\n")
	git("add", "app.txt")
	git("commit", "-q", "-m", "hotfix")
	f := &fakeForge{}
	useForge(t, f)

	out, code := runCode(t, "reconcile")
	if code != 3 {
		t.Fatalf("reconcile exit = %d, want 3\n%s", code, out)
	}
	if !strings.Contains(out, "converged with reported divergence") {
		t.Errorf("output missing divergence message:\n%s", out)
	}
	for _, c := range f.created {
		if c.Type == engine.RequestTypeBackflow {
			t.Errorf("backflow request created despite conflict: %+v", c)
		}
	}
	if len(f.pushed) != 0 {
		t.Errorf("conflict must push nothing, got %d", len(f.pushed))
	}
}

// TestReconcileMergeBackflowExitsZero exercises the FULL CLI plan->apply path
// for a merge-strategy backflow edge: a hotfix on main is returned to
// development as a single two-parent merge commit, the managed backflow request
// is opened, and reconcile exits 0. This is the merge-strategy analogue of
// TestReconcileBackflowsHotfixExitsZero.
func TestReconcileMergeBackflowExitsZero(t *testing.T) {
	git := setupRepo(t)
	git("write", ".oiax.yaml", mergeExampleConfig)
	git("checkout", "-q", "main")
	git("write", "hotfix.txt", "urgent\n")
	git("add", "hotfix.txt")
	git("commit", "-q", "-m", "hotfix")
	f := &fakeForge{}
	useForge(t, f)

	out, code := runCode(t, "reconcile")
	if code != 0 {
		t.Fatalf("reconcile exit = %d, want 0\n%s", code, out)
	}
	if len(f.pushed) != 1 {
		t.Fatalf("want 1 push, got %d", len(f.pushed))
	}
	var backflow int
	for _, c := range f.created {
		if c.Type == engine.RequestTypeBackflow {
			backflow++
			if !strings.Contains(c.Body, "by merge commit") {
				t.Errorf("backflow body = %q, want it to mention 'by merge commit'", c.Body)
			}
		}
	}
	if backflow != 1 {
		t.Fatalf("want 1 backflow create, got %d: %+v", backflow, f.created)
	}
}

// TestReconcileMergeBackflowConflictExitsThree covers a merge-strategy backflow
// whose wholesale merge conflicts: development and main edit the same file
// divergently, so the merge of main onto development cannot apply. Like a
// cherry-pick conflict it is a reported divergence — exit 3, nothing pushed, no
// request opened.
func TestReconcileMergeBackflowConflictExitsThree(t *testing.T) {
	git := setupRepo(t)
	git("write", ".oiax.yaml", mergeExampleConfig)
	git("checkout", "-q", "development")
	git("write", "app.txt", "dev-change\n")
	git("add", "app.txt")
	git("commit", "-q", "-m", "dev edit")
	git("checkout", "-q", "main")
	git("write", "app.txt", "hotfix-change\n")
	git("add", "app.txt")
	git("commit", "-q", "-m", "hotfix")
	f := &fakeForge{}
	useForge(t, f)

	out, code := runCode(t, "reconcile")
	if code != 3 {
		t.Fatalf("reconcile exit = %d, want 3\n%s", code, out)
	}
	if len(f.pushed) != 0 {
		t.Errorf("conflict must push nothing, got %d", len(f.pushed))
	}
	for _, c := range f.created {
		if c.Type == engine.RequestTypeBackflow {
			t.Errorf("backflow request created despite merge conflict: %+v", c)
		}
	}
}

// TestReconcileJSONAnnotationNotOnStdout guards the GitHub Actions
// combination M7 fixed: reconcile -o json used to route the Actions
// ::warning:: annotation for a reported divergence to the same stdout
// stream as the JSON plan document, corrupting it for machine consumers
// that parse stdout. The backflow-conflict fixture reliably logs a Warn
// (the annotation trigger); this proves the annotation lands on stderr
// only and stdout stays exactly the JSON plan, mirroring
// TestPlanJSONWarnsOnStderrNotStdout for the deprecation-warning case.
func TestReconcileJSONAnnotationNotOnStdout(t *testing.T) {
	// Build the repo directly (rather than via setupRepo) so .oiax.yaml is
	// committed at main's HEAD: under GITHUB_ACTIONS=true, config must
	// resolve through the default branch (ADR 0003), which requires a
	// committed config, not setupRepo's untracked working-tree copy.
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	t.Setenv("OIAX_LOG_FORMAT", "")
	t.Setenv("GITHUB_ACTIONS", "true")

	dir := t.TempDir()
	t.Chdir(dir)
	gitCmd := func(args ...string) {
		t.Helper()
		gittest.Run(t, dir, args...)
	}
	writeFile := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gittest.InitRepo(t, dir)
	writeFile("app.txt", "v0\n")
	writeFile(".oiax.yaml", exampleConfig)
	gitCmd("add", ".")
	gitCmd("commit", "-q", "-m", "c0")
	gitCmd("branch", "development")
	gitCmd("branch", "test")

	// development and main diverge on the same file with conflicting
	// content, so the backflow cherry-pick cannot apply cleanly: a
	// reported divergence, which logs the Warn that becomes the
	// ::warning:: annotation this test is guarding.
	gitCmd("checkout", "-q", "development")
	writeFile("app.txt", "dev-change\n")
	gitCmd("add", "app.txt")
	gitCmd("commit", "-q", "-m", "dev edit")
	gitCmd("checkout", "-q", "main")
	writeFile("app.txt", "hotfix-change\n")
	gitCmd("add", "app.txt")
	gitCmd("commit", "-q", "-m", "hotfix")

	// Fabricate the remote-tracking default branch locally so
	// DefaultBranchRef resolves without a network remote, and reconcile
	// reads the config committed at main's HEAD.
	head := gittest.Run(t, dir, "rev-parse", "main")
	gitCmd("update-ref", "refs/remotes/origin/main", head)
	gitCmd("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	f := &fakeForge{}
	useForge(t, f)

	root := NewRootCommand()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"reconcile", "--output", "json"})
	err := root.Execute()

	var ece exitCodeError
	if !errors.As(err, &ece) || ece.code != 3 {
		t.Fatalf("reconcile --output json err = %v, want exit code 3\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	// The reported-divergence warning must annotate stderr, not stdout.
	if !strings.Contains(stderr.String(), "::warning::") {
		t.Errorf("stderr missing the ::warning:: annotation:\n%s", stderr.String())
	}
	if strings.Contains(stdout.String(), "::warning::") {
		t.Errorf("annotation leaked onto stdout, corrupting JSON:\n%s", stdout.String())
	}
	// stdout alone must parse as valid JSON — no interleaved annotation line.
	var plan struct {
		PlanFormatVersion int `json:"planFormatVersion"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}
	if plan.PlanFormatVersion != 1 {
		t.Errorf("planFormatVersion = %d, want 1", plan.PlanFormatVersion)
	}
}

func TestPlanResolvesDefaultBranchConfig(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	t.Setenv("OIAX_LOG_FORMAT", "")

	dir := t.TempDir()
	t.Chdir(dir)
	gitCmd := func(args ...string) {
		t.Helper()
		gittest.Run(t, dir, args...)
	}
	writeFile := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gittest.InitRepo(t, dir)
	writeFile("app.txt", "v0\n")
	writeFile(".oiax.yaml", exampleConfig)
	gitCmd("add", ".")
	gitCmd("commit", "-q", "-m", "c0")
	gitCmd("branch", "development")
	gitCmd("branch", "test")

	// Fabricate the remote-tracking default branch locally so
	// DefaultBranchRef resolves without a network remote.
	head := gittest.Run(t, dir, "rev-parse", "HEAD")
	gitCmd("update-ref", "refs/remotes/origin/main", head)
	gitCmd("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	// Break the working-tree copy: plan must read the committed
	// default-branch config, not this.
	writeFile(".oiax.yaml", "not: valid oiax config")

	useForge(t, &fakeForge{})

	out, code := runCode(t, "plan")
	if code != 0 {
		t.Fatalf("plan exit = %d, want 0 (committed default-branch config should be read)\n%s", code, out)
	}
	if !strings.Contains(out, "In sync, no actions.") {
		t.Errorf("output = %q, want in-sync plan from the committed config", out)
	}
}

func TestPlanRefusesUnresolvableDefaultBranchUnderActions(t *testing.T) {
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	t.Setenv("OIAX_LOG_FORMAT", "")
	t.Setenv("GITHUB_ACTIONS", "true")

	dir := t.TempDir()
	t.Chdir(dir)
	gitCmd := func(args ...string) {
		t.Helper()
		gittest.Run(t, dir, args...)
	}
	gittest.InitRepo(t, dir)
	// origin exists but origin/HEAD is unset (the shallow-CI-checkout shape),
	// so the default branch cannot be resolved. A working-tree .oiax.yaml is
	// present; under Actions plan must refuse rather than read it.
	gitCmd("remote", "add", "origin", "https://github.com/example/repo.git")
	if err := os.WriteFile(filepath.Join(dir, ".oiax.yaml"), []byte(exampleConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, "plan")
	if err == nil {
		t.Fatalf("plan succeeded, want refusal under Actions:\n%s", out)
	}
	if !strings.Contains(err.Error(), "pin --config-ref") {
		t.Errorf("error = %v, want guidance to pin --config-ref", err)
	}
}

// TestPlanAssertsGitFloorBeforeConfigRead proves the version floor is asserted
// before any other git subprocess. With --config-ref set, config resolution
// runs `git show --end-of-options <ref>:<path>` during loadGraph; a fake git
// below the floor reports its version but fails that show the way an old git
// rejecting a modern option would. If the floor were checked only inside
// buildCoordinator (after loadGraph), plan would surface the raw git error;
// asserting the floor first yields the clear "or newer is required" refusal.
func TestPlanAssertsGitFloorBeforeConfigRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		// The below-floor git is a POSIX shell script; Windows cannot exec an
		// extension-less script as `git`. The ordering this proves (floor
		// asserted before any other git subprocess) is platform-independent Go
		// control flow, fully exercised on the linux and macos matrix legs.
		t.Skip("fake-git harness is POSIX-only; floor-ordering logic is platform-independent")
	}

	setupRepo(t)
	useForge(t, &fakeForge{})

	fakeDir := t.TempDir()
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = version ]; then echo 'git version 2.20.0'; exit 0; fi\n" +
		"echo 'error: unknown option end-of-options' >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(fakeDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir)

	out, code := runCode(t, "plan", "--config-ref", "origin/main")
	if code != 1 {
		t.Fatalf("plan exit = %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, "or newer is required") {
		t.Errorf("output = %q, want the git version-floor refusal, not a raw git error", out)
	}
}

func TestPlanForgeErrorExitsOne(t *testing.T) {
	setupRepo(t)
	prev := newForge
	newForge = func(context.Context, *slog.Logger) (forge.Forge, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { newForge = prev })

	out, code := runCode(t, "plan")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, out)
	}
}
