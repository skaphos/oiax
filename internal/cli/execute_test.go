package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/gittest"
)

func TestExitCodeErrorMessage(t *testing.T) {
	t.Parallel()
	if got := (exitCodeError{code: 3, msg: "converged with reported divergence"}).Error(); got != "converged with reported divergence" {
		t.Errorf("Error() = %q, want the message verbatim", got)
	}
	if got := (exitCodeError{code: 2}).Error(); got != "exit code 2" {
		t.Errorf("Error() without msg = %q, want %q", got, "exit code 2")
	}
}

// captureProcessStreams swaps the process stdout/stderr — which Execute's
// freshly built root command writes to — for temp files, restoring them on
// cleanup. Callers must stay serial (the streams are process globals; the
// setupRepo t.Setenv already forces that).
func captureProcessStreams(t *testing.T) (readOut, readErr func() string) {
	t.Helper()
	dir := t.TempDir()
	open := func(name string) *os.File {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	outF, errF := open("stdout"), open("stderr")
	prevOut, prevErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outF, errF
	t.Cleanup(func() {
		os.Stdout, os.Stderr = prevOut, prevErr
		_ = outF.Close()
		_ = errF.Close()
	})
	read := func(f *os.File) func() string {
		return func() string {
			b, err := os.ReadFile(f.Name())
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
	}
	return read(outF), read(errF)
}

func TestExecuteExitCodes(t *testing.T) {
	git := setupRepo(t)
	useForge(t, &fakeForge{})
	readOut, readErr := captureProcessStreams(t)
	ctx := context.Background()

	// Success is exit 0.
	if code := Execute(ctx, []string{"plan"}); code != 0 {
		t.Fatalf("Execute(plan) = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, readOut(), readErr())
	}
	if !strings.Contains(readOut(), "In sync, no actions.") {
		t.Errorf("stdout = %q, want the in-sync plan", readOut())
	}

	// A generic error is exit 1 with the "oiax:" framing on stderr.
	if code := Execute(ctx, []string{"no-such-command"}); code != 1 {
		t.Fatalf("Execute(no-such-command) = %d, want 1", code)
	}
	if !strings.Contains(readErr(), "oiax:") {
		t.Errorf("stderr = %q, want the oiax: framing", readErr())
	}

	// A pending action under --detailed-exitcode is a SILENT exit 2: the
	// exitCodeError carries no message, so no "oiax:" error framing may be
	// printed (structured log lines on stderr are fine).
	git("checkout", "-q", "development")
	git("write", "app.txt", "v1\n")
	git("add", ".")
	git("commit", "-q", "-m", "c1")
	stderrBefore := readErr()
	if code := Execute(ctx, []string{"plan", "--detailed-exitcode"}); code != 2 {
		t.Fatalf("Execute(plan --detailed-exitcode) = %d, want 2\nstdout:\n%s", code, readOut())
	}
	if delta := strings.TrimPrefix(readErr(), stderrBefore); strings.Contains(delta, "oiax:") {
		t.Errorf("detailed-exitcode printed an error message: %q", delta)
	}
}

// TestExecuteDivergenceMessage exercises the exitCodeError-with-message path:
// a reconcile that converges with reported divergence exits 3 and prints the
// explanation to stderr.
func TestExecuteDivergenceMessage(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	t.Setenv("TF_BUILD", "")
	t.Setenv("OIAX_LOG_FORMAT", "")

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

	// development and main diverge on the same file with conflicting content,
	// so the backflow cannot apply cleanly: a reported divergence, exit 3.
	gitCmd("checkout", "-q", "development")
	writeFile("app.txt", "dev-change\n")
	gitCmd("add", "app.txt")
	gitCmd("commit", "-q", "-m", "dev edit")
	gitCmd("checkout", "-q", "main")
	writeFile("app.txt", "hotfix-change\n")
	gitCmd("add", "app.txt")
	gitCmd("commit", "-q", "-m", "hotfix")

	// Fabricate the remote-tracking default branch locally so the pinned
	// config ref (ADR 0003) resolves without a network remote.
	head := gittest.Run(t, dir, "rev-parse", "main")
	gitCmd("update-ref", "refs/remotes/origin/main", head)
	gitCmd("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	useForge(t, &fakeForge{})
	readOut, readErr := captureProcessStreams(t)

	if code := Execute(context.Background(), []string{"reconcile"}); code != 3 {
		t.Fatalf("Execute(reconcile) = %d, want 3\nstdout:\n%s\nstderr:\n%s", code, readOut(), readErr())
	}
	if !strings.Contains(readErr(), "converged with reported divergence") {
		t.Errorf("stderr = %q, want the divergence message", readErr())
	}
}

func TestWriteStepSummary(t *testing.T) {
	newCmd := func() (*cobra.Command, *bytes.Buffer) {
		cmd := &cobra.Command{}
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)
		return cmd, &stderr
	}

	t.Run("appends to the GitHub summary file", func(t *testing.T) {
		t.Setenv("TF_BUILD", "")
		path := filepath.Join(t.TempDir(), "summary.md")
		t.Setenv("GITHUB_STEP_SUMMARY", path)
		cmd, stderr := newCmd()

		writeStepSummary(cmd, engine.Plan{})
		first, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(first) == 0 {
			t.Error("summary file is empty after the first write")
		}
		if stderr.Len() != 0 {
			t.Errorf("stderr = %q, want empty", stderr.String())
		}

		// A second write appends — the runner concatenates step summaries.
		writeStepSummary(cmd, engine.Plan{})
		second, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(second) != 2*len(first) {
			t.Errorf("summary length after second write = %d, want %d (append, not truncate)", len(second), 2*len(first))
		}
	})

	t.Run("a GitHub summary open failure is reported, not fatal", func(t *testing.T) {
		t.Setenv("TF_BUILD", "")
		t.Setenv("GITHUB_STEP_SUMMARY", filepath.Join(t.TempDir(), "missing-dir", "summary.md"))
		cmd, stderr := newCmd()
		writeStepSummary(cmd, engine.Plan{})
		if !strings.Contains(stderr.String(), "step summary") {
			t.Errorf("stderr = %q, want a step summary error", stderr.String())
		}
	})

	t.Run("an Azure temp-file failure is reported, not fatal", func(t *testing.T) {
		t.Setenv("GITHUB_ACTIONS", "")
		t.Setenv("GITHUB_STEP_SUMMARY", "")
		t.Setenv("TF_BUILD", "True")
		t.Setenv("AGENT_TEMPDIRECTORY", filepath.Join(t.TempDir(), "missing-dir"))
		cmd, stderr := newCmd()
		writeStepSummary(cmd, engine.Plan{})
		if !strings.Contains(stderr.String(), "step summary") {
			t.Errorf("stderr = %q, want a step summary error", stderr.String())
		}
		if strings.Contains(stderr.String(), "##vso[task.uploadsummary]") {
			t.Errorf("uploadsummary announced despite the failure:\n%s", stderr.String())
		}
	})

	t.Run("no CI environment writes nothing", func(t *testing.T) {
		t.Setenv("GITHUB_ACTIONS", "")
		t.Setenv("GITHUB_STEP_SUMMARY", "")
		t.Setenv("TF_BUILD", "")
		cmd, stderr := newCmd()
		writeStepSummary(cmd, engine.Plan{})
		if stderr.Len() != 0 {
			t.Errorf("stderr = %q, want empty", stderr.String())
		}
	})
}
