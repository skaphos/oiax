package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skaphos/oiax/v2/internal/gittest"
	"github.com/skaphos/oiax/v2/internal/reconcile"
)

// conflictRepo builds a repository whose backflow replay conflicts, with
// .oiax.yaml committed and origin/HEAD fabricated so a CI host can resolve
// the pinned config ref (ADR 0003). source is the backflow source branch:
// it and development edit app.txt divergently. The CI environment is
// cleared; callers set the host they test.
func conflictRepo(t *testing.T, config, source string) func(args ...string) {
	t.Helper()
	for _, k := range []string{"GITHUB_ACTIONS", "GITHUB_STEP_SUMMARY", "TF_BUILD", "AGENT_TEMPDIRECTORY", "OIAX_LOG_FORMAT"} {
		t.Setenv(k, "")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	git := func(args ...string) {
		t.Helper()
		if len(args) == 3 && args[0] == "write" {
			if err := os.WriteFile(filepath.Join(dir, args[1]), []byte(args[2]), 0o644); err != nil {
				t.Fatal(err)
			}
			return
		}
		gittest.Run(t, dir, args...)
	}
	gittest.InitRepo(t, dir)
	git("write", "app.txt", "v0\n")
	git("write", ".oiax.yaml", config)
	git("add", ".")
	git("commit", "-q", "-m", "c0")
	git("branch", "development")
	git("branch", "test")
	git("checkout", "-q", "development")
	git("write", "app.txt", "dev-change\n")
	git("add", "app.txt")
	git("commit", "-q", "-m", "dev edit")
	git("checkout", "-q", source)
	git("write", "app.txt", "hotfix-change\n")
	git("add", "app.txt")
	git("commit", "-q", "-m", "fix: conflicting hotfix")
	git("checkout", "-q", "main")
	head := gittest.Run(t, dir, "rev-parse", "main")
	git("update-ref", "refs/remotes/origin/main", head)
	git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	return git
}

// githubActions sets GitHub Actions detection with a fresh summary file and
// returns its path.
func githubActions(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "step-summary.md")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_STEP_SUMMARY", path)
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// On GitHub Actions with Issues enabled, a conflicting backflow edge gives one
// warning ending with the issue address, one summary section after the plan
// summary, and an exit-3 message naming the edge and address (spec 002 US1;
// T024).
func TestReconcileConflictGitHubSurfaces(t *testing.T) {
	conflictRepo(t, exampleConfig, "main")
	summary := githubActions(t)
	f := &fakeForge{}
	useForge(t, f)

	out, code := runCode(t, "reconcile")
	if code != 3 {
		t.Fatalf("reconcile exit = %d, want 3\n%s", code, out)
	}

	var warnings []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "::warning::") && strings.Contains(line, "halted on") {
			warnings = append(warnings, line)
		}
	}
	if len(warnings) != 1 {
		t.Fatalf("want exactly one conflict warning, got %d:\n%s", len(warnings), out)
	}
	w := warnings[0]
	if !strings.Contains(w, "backflow main -> development halted on cherry-pick conflict at ") ||
		!strings.HasSuffix(w, ". Resolution steps: "+fakeIssueURL) {
		t.Fatalf("warning = %q, want the cherry-pick lead and the issue address", w)
	}

	sum := readFile(t, summary)
	planAt := strings.Index(sum, "## ")
	sectionAt := strings.Index(sum, "### Backflow conflict: main → development")
	if planAt < 0 || sectionAt < planAt || strings.Count(sum, "### Backflow conflict:") != 1 {
		t.Fatalf("summary does not have the plan then one conflict section:\n%s", sum)
	}
	if !strings.Contains(sum, "[issue 7]("+fakeIssueURL+")") {
		t.Fatalf("summary section lacks the issue link:\n%s", sum)
	}

	wantTail := "converged with reported divergence on 1 edge:\n  main -> development: backflow conflict; resolution steps: " + fakeIssueURL + "\n"
	if !strings.HasSuffix(out, wantTail) {
		t.Fatalf("output does not end with the exit-3 message %q:\n%s", wantTail, out)
	}
}

// Apply records a conflict on one edge and then fails a later step: exit 1,
// and the conflict still reaches the summary and the log, with no exit-3
// message (spec 002 FR-002, edge case "Apply later aborts"; T026).
func TestReconcileConflictThenApplyAbort(t *testing.T) {
	const config = `apiVersion: oiax.skaphos.dev/v1
kind: PromotionGraph
metadata:
  name: environments
spec:
  branches:
    development:
      role: source
    test: {}
    main:
      role: terminal
  promotions:
    - from: development
      to: test
    - from: test
      to: main
  backflow:
    sources: [test]
    target: development
`
	conflictRepo(t, config, "test")
	summary := githubActions(t)
	f := &fakeForge{createRequestErr: map[string]error{"test->main": errors.New("forge unavailable")}}
	useForge(t, f)

	out, code := runCode(t, "reconcile")
	if code != 1 {
		t.Fatalf("reconcile exit = %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, "backflow test -> development halted on cherry-pick conflict") {
		t.Fatalf("conflict warning missing after the apply failure:\n%s", out)
	}
	if !strings.Contains(readFile(t, summary), "### Backflow conflict: test → development") {
		t.Fatalf("conflict section missing after the apply failure:\n%s", readFile(t, summary))
	}
	if strings.Contains(out, "converged with reported divergence") {
		t.Fatalf("exit-3 message printed on an exit-1 run:\n%s", out)
	}
}

// The exit-3 message names each edge with trusted text only (spec 002
// FR-009, FR-010, contracts/surfaces.md §3).
func TestDivergenceMessage(t *testing.T) {
	known := reconcile.DivergenceReport{From: "main", To: "development", Kind: reconcile.KindConflict,
		Conflict: &reconcile.ConflictGuidance{Artifact: reconcile.ArtifactKnown, ArtifactURL: fakeIssueURL, Subject: "secret subject"}}
	unhostable := reconcile.DivergenceReport{From: "hotfix", To: "development", Kind: reconcile.KindConflict,
		Conflict: &reconcile.ConflictGuidance{Artifact: reconcile.ArtifactUnhostable}}
	notRecorded := reconcile.DivergenceReport{From: "release", To: "development", Kind: reconcile.KindConflict,
		Conflict: &reconcile.ConflictGuidance{Artifact: reconcile.ArtifactNotRecorded, ArtifactNoun: "work item"}}
	other := reconcile.DivergenceReport{From: "test", To: "main", Kind: reconcile.KindOther}

	if got := divergenceMessage(nil, false); got != "converged with reported divergence" {
		t.Fatalf("no reports: %q", got)
	}
	got := divergenceMessage([]reconcile.DivergenceReport{known, unhostable, notRecorded, other}, true)
	want := "converged with reported divergence on 4 edges:\n" +
		"  main -> development: backflow conflict; resolution steps: " + fakeIssueURL + "\n" +
		"  hotfix -> development: backflow conflict; no durable conflict record (Issues are disabled); resolution steps are in this run's summary\n" +
		"  release -> development: backflow conflict; conflict work item not recorded this run, will be retried next run; resolution steps are in this run's summary\n" +
		"  test -> main: reported divergence"
	if got != want {
		t.Fatalf("message =\n%s\nwant\n%s", got, want)
	}
	got = divergenceMessage([]reconcile.DivergenceReport{unhostable}, false)
	if !strings.HasPrefix(got, "converged with reported divergence on 1 edge:\n") ||
		!strings.HasSuffix(got, "resolution steps: "+reconcile.BackflowGuideURL) {
		t.Fatalf("one edge without a summary: %q", got)
	}
	if strings.Contains(divergenceMessage([]reconcile.DivergenceReport{known}, false), "secret subject") {
		t.Fatal("exit-3 message contains a commit subject")
	}
}
