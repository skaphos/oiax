package cli

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

// azurePipelines sets Azure Pipelines detection with a fresh agent temp
// directory and returns a reader for every summary file written there.
func azurePipelines(t *testing.T) func() string {
	t.Helper()
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	dir := t.TempDir()
	t.Setenv("TF_BUILD", "True")
	t.Setenv("AGENT_TEMPDIRECTORY", dir)
	return func() string {
		matches, _ := filepath.Glob(filepath.Join(dir, "oiax-conflicts-*.md"))
		if len(matches) != 1 {
			t.Fatalf("conflict summary files = %v, want one", matches)
		}
		return readFile(t, matches[0])
	}
}

// Under Azure Pipelines the guidance is the same, with either forge: one
// task.logissue warning ending with the address, one uploaded summary file
// with the section, exit 3; the sections differ only in the artifact noun
// and address (spec 002 US3; T044, T046).
func TestReconcileConflictAzureSurfaces(t *testing.T) {
	const workItemURL = "https://dev.azure.com/acme/My%20Platform/_workitems/edit/7"
	sections := map[string]string{}
	for _, tt := range []struct {
		name, noun, url string
	}{
		{"github forge", "", fakeIssueURL},
		{"azure devops forge", "work item", workItemURL},
	} {
		t.Run(tt.name, func(t *testing.T) {
			conflictRepo(t, exampleConfig, "main")
			summary := azurePipelines(t)
			useForge(t, &fakeForge{noun: tt.noun, conflictURL: tt.url})

			out, code := runCode(t, "reconcile")
			if code != 3 {
				t.Fatalf("reconcile exit = %d, want 3\n%s", code, out)
			}
			var warnings, uploads int
			for _, line := range strings.Split(out, "\n") {
				if strings.HasPrefix(line, "##vso[task.logissue type=warning]") && strings.Contains(line, "halted on") {
					warnings++
					// The agent decodes command data (%AZP25 is '%').
					line = strings.ReplaceAll(line, "%AZP25", "%")
					if !strings.HasSuffix(line, ". Resolution steps: "+tt.url) {
						t.Fatalf("warning = %q, want it to end with %s", line, tt.url)
					}
				}
				if strings.HasPrefix(line, "##vso[task.uploadsummary]") && strings.Contains(line, "oiax-conflicts-") {
					uploads++
				}
			}
			if warnings != 1 || uploads != 1 {
				t.Fatalf("warnings = %d, conflict uploads = %d; want 1 and 1\n%s", warnings, uploads, out)
			}
			sec := summary()
			noun := tt.noun
			if noun == "" {
				noun = "issue"
			}
			if !strings.Contains(sec, "["+noun+" 7]("+tt.url+")") {
				t.Fatalf("section lacks the %s link:\n%s", noun, sec)
			}
			// Each subtest builds its own repository, so mask the commit SHAs too.
			sec = regexp.MustCompile(`[0-9a-f]{40}|[0-9a-f]{7}\b`).ReplaceAllString(sec, "<sha>")
			sections[tt.name] = strings.ReplaceAll(strings.ReplaceAll(sec, tt.url, "<address>"), noun+" 7", "<noun> 7")
		})
	}
	if a, b := sections["github forge"], sections["azure devops forge"]; a == "" || a != b {
		t.Fatalf("sections differ beyond the noun and address:\n%s\n---\n%s", a, b)
	}
}

// With no artifact, the warning and the exit-3 line point at this run's
// summary only when it was written; otherwise both point at the backflow
// guide and never mention a summary (spec 002 FR-010, US4 AS3; T051).
func TestReconcileConflictNoArtifactPointers(t *testing.T) {
	forges := map[string]struct {
		mk        func() *fakeForge
		statement string
		exitLine  string
	}{
		"unhostable": {func() *fakeForge { return &fakeForge{artifactsUnsupported: true} },
			"No durable conflict record can be kept (Issues are disabled); ",
			"backflow conflict; no durable conflict record (Issues are disabled); "},
		"not recorded": {func() *fakeForge { return &fakeForge{createConflictErr: errors.New("502")} },
			"The conflict issue could not be recorded or confirmed this run and will be retried next run; ",
			"backflow conflict; conflict issue not recorded this run, will be retried next run; "},
	}
	summaries := map[string]func(t *testing.T) (written bool){
		"summary written":        func(t *testing.T) bool { githubActions(t); return true },
		"no summary (local run)": func(t *testing.T) bool { return false },
		"unwritable summary path": func(t *testing.T) bool {
			t.Setenv("GITHUB_ACTIONS", "true")
			t.Setenv("GITHUB_STEP_SUMMARY", filepath.Join(t.TempDir(), "missing", "summary.md"))
			return false
		},
	}
	for fname, fc := range forges {
		for sname, setSummary := range summaries {
			t.Run(fname+"/"+sname, func(t *testing.T) {
				conflictRepo(t, exampleConfig, "main")
				written := setSummary(t)
				useForge(t, fc.mk())
				out, code := runCode(t, "reconcile")
				if code != 3 {
					t.Fatalf("exit %d, want 3\n%s", code, out)
				}
				pointer := "resolution steps: " + reconcile.BackflowGuideURL
				if written {
					pointer = "resolution steps are in this run's summary"
				}
				var warning string
				for _, line := range strings.Split(out, "\n") {
					if strings.Contains(line, "halted on") && !strings.HasPrefix(line, "::") {
						warning = line
					}
				}
				if !strings.Contains(warning, fc.statement+pointer) {
					t.Fatalf("plain warning = %q, want it to end with %q", warning, fc.statement+pointer)
				}
				if !strings.Contains(out, "  main -> development: "+fc.exitLine+pointer+"\n") {
					t.Fatalf("exit-3 line lacks %q:\n%s", fc.exitLine+pointer, out)
				}
				if !written && strings.Contains(out, "summary") && strings.Contains(out, "this run's summary") {
					t.Fatalf("a run with no summary mentions one:\n%s", out)
				}
			})
		}
	}
}

// twoSourceConfig backflows both main and test into development.
const twoSourceConfig = `apiVersion: oiax.skaphos.dev/v1
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
    sources: [test, main]
    target: development
`

// twoConflictRepo makes both backflow sources conflict with development.
func twoConflictRepo(t *testing.T) {
	t.Helper()
	git := conflictRepo(t, twoSourceConfig, "main")
	git("checkout", "-q", "test")
	git("write", "app.txt", "test-hotfix\n")
	git("add", "app.txt")
	git("commit", "-q", "-m", "fix: test hotfix")
	git("checkout", "-q", "main")
}

// Several conflicting edges: one warning and one self-contained section per
// conflict, and an exit-3 message naming every diverging edge with its own
// pointer, in plan order (spec 002 US5, FR-011; T055).
func TestReconcileMultipleConflictsOrdered(t *testing.T) {
	twoConflictRepo(t)
	summary := githubActions(t)
	useForge(t, &fakeForge{})

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
	if len(warnings) != 2 || !strings.Contains(warnings[0], "backflow test -> development") ||
		!strings.Contains(warnings[1], "backflow main -> development") {
		t.Fatalf("want the test then main conflict warnings, got:\n%s", strings.Join(warnings, "\n"))
	}
	if strings.HasSuffix(warnings[0], "/issues/7") == strings.HasSuffix(warnings[1], "/issues/7") {
		t.Fatalf("both warnings point at the same record:\n%s", strings.Join(warnings, "\n"))
	}
	sum := readFile(t, summary)
	if strings.Count(sum, "### Backflow conflict:") != 2 ||
		strings.Index(sum, "### Backflow conflict: test") > strings.Index(sum, "### Backflow conflict: main") {
		t.Fatalf("want two sections, test before main:\n%s", sum)
	}

	msg := out[strings.Index(out, "converged with reported divergence on "):]
	lines := strings.Split(strings.TrimSpace(msg), "\n")
	wantFirst := "converged with reported divergence on " + strconv.Itoa(len(lines)-1) + " edges:"
	if lines[0] != wantFirst {
		t.Fatalf("first line = %q, want %q", lines[0], wantFirst)
	}
	conflicts := 0
	for _, l := range lines[1:] {
		switch {
		case strings.Contains(l, ": backflow conflict; resolution steps: https://github.com/acme/widgets/issues/"):
			conflicts++
		case strings.HasSuffix(l, ": reported divergence"):
		default:
			t.Fatalf("unexpected exit-3 line %q", l)
		}
	}
	if conflicts != 2 || strings.Index(msg, "test -> development: backflow") > strings.Index(msg, "main -> development: backflow") {
		t.Fatalf("want both conflicts with their own pointers, test first:\n%s", msg)
	}
}

// A step summary already at the 1 MiB limit gets no conflict section, and the
// pointers never claim one: a known artifact keeps its address, no artifact
// points at the guide (spec 002 FR-005, FR-010; T056).
func TestReconcileConflictSummaryAtLimit(t *testing.T) {
	for _, tt := range []struct {
		name    string
		forge   func() *fakeForge
		pointer string
	}{
		{"known artifact", func() *fakeForge { return &fakeForge{} }, ". Resolution steps: " + fakeIssueURL},
		{"no artifact", func() *fakeForge { return &fakeForge{artifactsUnsupported: true} }, "; resolution steps: " + reconcile.BackflowGuideURL},
	} {
		t.Run(tt.name, func(t *testing.T) {
			conflictRepo(t, exampleConfig, "main")
			path := githubActions(t)
			if err := os.WriteFile(path, []byte(strings.Repeat("x", reconcile.SummaryLimit)), 0o600); err != nil {
				t.Fatal(err)
			}
			useForge(t, tt.forge())
			out, code := runCode(t, "reconcile")
			if code != 3 {
				t.Fatalf("exit %d, want 3\n%s", code, out)
			}
			if strings.Contains(readFile(t, path), "### Backflow conflict:") {
				t.Fatal("a conflict section was appended to a full summary")
			}
			if strings.Contains(out, "this run's summary") {
				t.Fatalf("pointers claim a summary that was not written:\n%s", out)
			}
			if !strings.Contains(out, tt.pointer) {
				t.Fatalf("output lacks the pointer %q:\n%s", tt.pointer, out)
			}
		})
	}
}

// The plan summary, notification diagnostics, and conflict sections share
// one step-summary file. Starting near the limit, the conflict sections never
// take the file past 1 MiB, and the pointers match what was written
// (spec 002 research R8; T056).
func TestReconcileConflictSummaryCombinedWriters(t *testing.T) {
	conflictRepo(t, exampleConfig, "main")
	path := githubActions(t)
	// Leave room for the plan summary but not for full conflict guidance.
	if err := os.WriteFile(path, []byte(strings.Repeat("x", reconcile.SummaryLimit-6000)), 0o600); err != nil {
		t.Fatal(err)
	}
	useForge(t, &fakeForge{})
	out, code := runCode(t, "reconcile")
	if code != 3 {
		t.Fatalf("exit %d, want 3\n%s", code, out)
	}
	sum := readFile(t, path)
	wrote := strings.Contains(sum, "### Backflow conflict:")
	if wrote && len(sum) > reconcile.SummaryLimit {
		t.Fatalf("conflict sections took the summary to %d bytes (limit %d)", len(sum), reconcile.SummaryLimit)
	}
	// A known artifact's pointer is its address whether or not the section
	// fit; nothing may claim a summary.
	if strings.Contains(out, "this run's summary") || !strings.Contains(out, ". Resolution steps: "+fakeIssueURL) {
		t.Fatalf("pointers do not match what was written (section written: %v):\n%s", wrote, out)
	}
}
