package reconcile

import (
	"bytes"
	"strings"
	"testing"
)

func knownReport(from, to string, whole bool) DivergenceReport {
	return DivergenceReport{From: from, To: to, Kind: KindConflict, Conflict: &ConflictGuidance{
		Strategy:     "cherry-pick",
		SHA:          "cccccccccccccccccccccccccccccccccccccccc",
		Subject:      "fix: conflicting hotfix",
		Applied:      2,
		Whole:        whole,
		Artifact:     ArtifactKnown,
		ArtifactID:   "12",
		ArtifactURL:  "https://github.com/acme/widgets/issues/12",
		ArtifactNoun: "issue",
		SummaryBody:  "GUIDANCE BODY",
	}}
}

func renderSections(t *testing.T, reports []DivergenceReport, budget int) (string, bool) {
	t.Helper()
	var buf bytes.Buffer
	written, err := RenderConflictSections(&buf, reports, budget)
	if err != nil {
		t.Fatalf("RenderConflictSections: %v", err)
	}
	return buf.String(), written
}

// One cherry-pick section, golden (contracts/surfaces.md §2).
func TestRenderConflictSectionCherryPick(t *testing.T) {
	got, written := renderSections(t, []DivergenceReport{knownReport("main", "development", false)}, SummaryLimit)
	if !written {
		t.Fatal("written = false")
	}
	want := "\n### Backflow conflict: main → development\n\n" +
		"No request was created for this edge.\n\n" +
		"- **Failing commit:** `cccccccccccccccccccccccccccccccccccccccc` — fix: conflicting hotfix\n" +
		"- **Applied cleanly before the conflict:** 2\n" +
		"- **Conflict record:** [issue 12](https://github.com/acme/widgets/issues/12)\n" +
		"\nGUIDANCE BODY\n" +
		"\nSee [When a replay conflicts](" + BackflowGuideURL + ") in the backflow guide.\n"
	if got != want {
		t.Fatalf("section =\n%q\nwant\n%q", got, want)
	}
}

// The merge section names the whole-set conflict instead of a count.
func TestRenderConflictSectionMerge(t *testing.T) {
	got, _ := renderSections(t, []DivergenceReport{knownReport("main", "development", true)}, SummaryLimit)
	if !strings.Contains(got, "- **The whole set conflicted** (merge strategy)\n") {
		t.Fatalf("merge section lacks the whole-set line:\n%s", got)
	}
	if strings.Contains(got, "Applied cleanly") {
		t.Fatalf("merge section has an applied count:\n%s", got)
	}
}

// Branch names are trusted but may hold Markdown syntax; the header and
// subject are contained (spec 002 "Branch names", FR-016).
func TestRenderConflictSectionContainsHeaderText(t *testing.T) {
	r := knownReport("rel|ease_`x`*", "<dev>@team", false)
	r.Conflict.Subject = "[steps](https://attacker.example) <img src=x> @org"
	got, _ := renderSections(t, []DivergenceReport{r}, SummaryLimit)
	header := got[:strings.Index(got, "\n\nGUIDANCE BODY")]
	for _, bad := range []string{"|", "`x`", "_`", "<dev>", "@team", "[steps]", "](https://attacker", "<img", "@org", "https://attacker"} {
		if strings.Contains(header, bad) {
			t.Fatalf("header keeps %q:\n%s", bad, header)
		}
	}
	if !strings.Contains(header, "rel｜ease＿ˋxˋ∗ → ‹dev›＠team") {
		t.Fatalf("header does not show the contained branch names:\n%s", header)
	}
	if !strings.Contains(header, "[issue 12](https://github.com/acme/widgets/issues/12)") {
		t.Fatalf("header lost the artifact link:\n%s", header)
	}
}

// The FR-005 note replaces the body after a render failure; the FR-016 note
// precedes the built-in body after the template gate fell back.
func TestRenderConflictSectionNotes(t *testing.T) {
	failed := knownReport("main", "development", false)
	failed.Conflict.RenderFailed = true
	failed.Conflict.SummaryBody = ""
	got, _ := renderSections(t, []DivergenceReport{failed}, SummaryLimit)
	if !strings.Contains(got, "\n"+renderFailedNote+"\n") {
		t.Fatalf("render-failure note missing:\n%s", got)
	}

	fallback := knownReport("main", "development", false)
	fallback.Conflict.BuiltinFallback = true
	got, _ = renderSections(t, []DivergenceReport{fallback}, SummaryLimit)
	if !strings.Contains(got, builtinFallbackNote+"\n\nGUIDANCE BODY\n") {
		t.Fatalf("fallback note missing or misplaced:\n%s", got)
	}
}

// No conflict report, no section.
func TestRenderConflictSectionsNothingToWrite(t *testing.T) {
	got, written := renderSections(t, []DivergenceReport{{From: "a", To: "b", Kind: KindOther}}, SummaryLimit)
	if written || got != "" {
		t.Fatalf("wrote %q for a report with no conflict", got)
	}
}

// The warning ends with exactly one Oiax-derived pointer (contracts §1).
func TestConflictWarningPointers(t *testing.T) {
	base := knownReport("main", "development", false)
	base.Conflict.Subject = "see https://attacker.example ::error::x"
	lead := `backflow main -> development halted on cherry-pick conflict at cccccccccccccccccccccccccccccccccccccccc ` +
		`"see https:` + "\u2060" + `//attacker.example ::error::x" after 2 applied cleanly; no request created`
	tests := []struct {
		name    string
		status  ArtifactStatus
		written bool
		want    string
	}{
		{"known", ArtifactKnown, false, ". Resolution steps: https://github.com/acme/widgets/issues/12"},
		{"known with summary", ArtifactKnown, true, ". Resolution steps: https://github.com/acme/widgets/issues/12"},
		{"unhostable with summary", ArtifactUnhostable, true,
			". No durable conflict record can be kept (Issues are disabled); resolution steps are in this run's summary"},
		{"unhostable without summary", ArtifactUnhostable, false,
			". No durable conflict record can be kept (Issues are disabled); resolution steps: " + BackflowGuideURL},
		{"not recorded with summary", ArtifactNotRecorded, true,
			". The conflict issue could not be recorded or confirmed this run and will be retried next run; resolution steps are in this run's summary"},
		{"not recorded without summary", ArtifactNotRecorded, false,
			". The conflict issue could not be recorded or confirmed this run and will be retried next run; resolution steps: " + BackflowGuideURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := base
			g := *base.Conflict
			g.Artifact = tt.status
			r.Conflict = &g
			if got := ConflictWarning(r, tt.written); got != lead+tt.want {
				t.Fatalf("warning =\n%q\nwant\n%q", got, lead+tt.want)
			}
		})
	}

	merge := knownReport("main", "development", true)
	if got := ConflictWarning(merge, false); !strings.HasPrefix(got,
		`backflow main -> development halted on merge conflict at cccccccccccccccccccccccccccccccccccccccc "fix: conflicting hotfix"; no request created. `) {
		t.Fatalf("merge warning = %q", got)
	}
}

// With no artifact the section states why and still gives the steps
// (spec 002 FR-008, contracts/surfaces.md §2; T050).
func TestRenderConflictSectionNoArtifact(t *testing.T) {
	unhostable := knownReport("main", "development", false)
	unhostable.Conflict.Artifact = ArtifactUnhostable
	unhostable.Conflict.ArtifactID, unhostable.Conflict.ArtifactURL = "", ""
	got, _ := renderSections(t, []DivergenceReport{unhostable}, SummaryLimit)
	for _, want := range []string{
		"- **Conflict record:** none. No durable conflict record can be kept (Issues are disabled), so this section " +
			"is the only guidance for this conflict, and it lasts only as long as the CI host keeps this run. " +
			"See [Keep Issues enabled](" + keepIssuesURL + ").\n",
		"\nGUIDANCE BODY\n",
		"(" + BackflowGuideURL + ")",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("unhostable section lacks %q:\n%s", want, got)
		}
	}

	notRecorded := knownReport("main", "development", false)
	notRecorded.Conflict.Artifact = ArtifactNotRecorded
	notRecorded.Conflict.ArtifactNoun = "work item"
	notRecorded.Conflict.ArtifactID, notRecorded.Conflict.ArtifactURL = "", ""
	got, _ = renderSections(t, []DivergenceReport{notRecorded}, SummaryLimit)
	if want := "- **Conflict record:** the conflict work item could not be recorded or confirmed this run and will be retried next run.\n"; !strings.Contains(got, want) {
		t.Fatalf("not-recorded section lacks %q:\n%s", want, got)
	}
	if strings.Contains(got, "](https://github.com/acme") {
		t.Fatalf("not-recorded section links an artifact:\n%s", got)
	}

	failed := notRecorded
	g := *notRecorded.Conflict
	g.RenderFailed, g.SummaryBody = true, ""
	failed.Conflict = &g
	got, _ = renderSections(t, []DivergenceReport{failed}, SummaryLimit)
	if !strings.Contains(got, "\n"+renderFailedNote+"\n") || !strings.Contains(got, "will be retried next run") {
		t.Fatalf("render-failure section lacks the note or the statement:\n%s", got)
	}
}
