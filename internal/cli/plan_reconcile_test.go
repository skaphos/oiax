package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
)

// fakeForge is an in-memory forge for exercising plan/reconcile wiring.
type fakeForge struct {
	open    []engine.ChangeRequest
	merged  []engine.ChangeRequest
	created []forge.CreateRequest
	updated []forge.UpdateRequest
	closed  []forge.RequestID
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

func (f *fakeForge) PushBranch(context.Context, forge.BranchPush) error {
	return forge.ErrNotImplemented
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
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	writeFile := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.name", "test")
	run("config", "user.email", "test@example.invalid")
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

func TestReconcileDivergenceExitsThree(t *testing.T) {
	git := setupRepo(t)
	git("checkout", "-q", "main")
	git("write", "hotfix.txt", "urgent\n")
	git("add", ".")
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
	if len(f.created)+len(f.updated)+len(f.closed) != 0 {
		t.Error("divergence must not mutate the forge")
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
