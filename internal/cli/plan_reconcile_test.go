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

func TestPlanJSONShape(t *testing.T) {
	git := setupRepo(t)
	git("checkout", "-q", "development")
	git("write", "app.txt", "v1\n")
	git("add", ".")
	git("commit", "-q", "-m", "c1")
	useForge(t, &fakeForge{})

	out, code := runCode(t, "plan", "--output", "json")
	if code != 0 {
		t.Fatalf("plan json exit = %d, want 0\n%s", code, out)
	}
	var plan struct {
		PlanFormatVersion int    `json:"planFormatVersion"`
		Graph             string `json:"graph"`
		Actions           []struct {
			Type string `json:"type"`
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if plan.PlanFormatVersion != 1 {
		t.Errorf("planFormatVersion = %d, want 1", plan.PlanFormatVersion)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Type != "createPromotionRequest" {
		t.Errorf("actions = %+v", plan.Actions)
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
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	writeFile := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitCmd("init", "-q", "-b", "main")
	gitCmd("config", "user.name", "test")
	gitCmd("config", "user.email", "test@example.invalid")
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
	headOut, err := exec.Command("git", "-C", dir, "rev-parse", "main").Output()
	if err != nil {
		t.Fatal(err)
	}
	head := strings.TrimSpace(string(headOut))
	gitCmd("update-ref", "refs/remotes/origin/main", head)
	gitCmd("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	f := &fakeForge{}
	useForge(t, f)

	root := NewRootCommand()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"reconcile", "--output", "json"})
	err = root.Execute()

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
