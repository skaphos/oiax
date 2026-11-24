package reconcile

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/skaphos/oiax/v2/internal/textsafe"
)

// The conflict surfaces the CLI publishes after apply (spec 002,
// contracts/surfaces.md): the summary sections, and the warning text. The
// formats are documented behavior, not compatibility contracts.

const (
	// BackflowGuideURL is the playbook every no-artifact pointer names.
	BackflowGuideURL = "https://github.com/skaphos/oiax/blob/main/docs/guides/backflow.md#when-a-replay-conflicts"
	// keepIssuesURL explains why a GitHub repository should keep Issues on.
	keepIssuesURL = "https://github.com/skaphos/oiax/blob/main/docs/guides/operating.md#keep-issues-enabled-github-backflow-only"

	// SummaryLimit is the job-summary size limit Oiax budgets its conflict
	// sections against: GitHub's documented 1 MiB per step, also used for
	// Azure Pipelines, whose limit is undocumented (research R8).
	SummaryLimit = 1 << 20

	renderFailedNote    = "The configured conflict text could not be rendered; see the run log."
	builtinFallbackNote = "The configured conflict text includes commit text, so Oiax shows its built-in steps here."
	truncationNote      = "Some conflict guidance was shortened to fit the summary size limit."
)

// ConflictWarning is the one warning line for a conflicting edge: today's
// leading text, then exactly one pointer chosen by the artifact status and
// whether the summary sections were written. The subject is cleaned and
// made non-linkable; the line always ends with Oiax-derived text (spec 002
// FR-001, FR-014, FR-015). Host-command neutralization happens in the
// logger (research R6).
func ConflictWarning(r DivergenceReport, summaryWritten bool) string {
	g := r.Conflict
	// Quoted like %q for '"' and '\\', but without %q's \u escapes: the word
	// joiners PlainLink and the logger insert must stay invisible.
	subject := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(textsafe.PlainLink(textsafe.Subject(g.Subject))) + `"`
	var lead string
	if g.Whole {
		lead = fmt.Sprintf("backflow %s -> %s halted on merge conflict at %s %s; no request created",
			r.From, r.To, g.SHA, subject)
	} else {
		lead = fmt.Sprintf("backflow %s -> %s halted on cherry-pick conflict at %s %s after %d applied cleanly; no request created",
			r.From, r.To, g.SHA, subject, g.Applied)
	}
	steps := "resolution steps: " + BackflowGuideURL
	if summaryWritten {
		steps = "resolution steps are in this run's summary"
	}
	switch g.Artifact {
	case ArtifactKnown:
		return lead + ". Resolution steps: " + g.ArtifactURL
	case ArtifactUnhostable:
		return lead + ". No durable conflict record can be kept (Issues are disabled); " + steps
	default:
		return lead + ". The conflict " + g.noun() + " could not be recorded or confirmed this run and will be retried next run; " + steps
	}
}

func (g *ConflictGuidance) noun() string {
	if g.ArtifactNoun != "" {
		return g.ArtifactNoun
	}
	return "issue"
}

// RenderConflictSections writes one summary section per KindConflict report,
// in report order, within budget bytes (spec 002 FR-002, FR-005, research
// R8). When every full section fits, all are written. Otherwise each
// section's reduced form (everything but the guidance body) and one
// truncation note are reserved first, and sections are upgraded to their full
// form in order while they fit. When even the reduced sections and the note
// do not fit, nothing is written and written is false. written is also false
// when there is no conflict to report.
func RenderConflictSections(w io.Writer, reports []DivergenceReport, budget int) (written bool, err error) {
	var full, reduced []string
	for _, r := range reports {
		if r.Kind != KindConflict || r.Conflict == nil {
			continue
		}
		full = append(full, conflictSection(r, true))
		reduced = append(reduced, conflictSection(r, false))
	}
	if len(full) == 0 || budget <= 0 {
		return false, nil
	}
	total := 0
	for _, s := range full {
		total += len(s)
	}
	var out bytes.Buffer
	if total <= budget {
		for _, s := range full {
			out.WriteString(s)
		}
	} else {
		note := "\n" + truncationNote + "\n"
		reserve := len(note)
		for _, s := range reduced {
			reserve += len(s)
		}
		if reserve > budget {
			return false, nil
		}
		left := budget - reserve
		upgrading := true
		for i := range full {
			if upgrading && len(full[i])-len(reduced[i]) <= left {
				left -= len(full[i]) - len(reduced[i])
				out.WriteString(full[i])
				continue
			}
			upgrading = false
			out.WriteString(reduced[i])
		}
		out.WriteString(note)
	}
	if _, err := w.Write(out.Bytes()); err != nil {
		return false, err
	}
	return true, nil
}

// conflictSection renders one edge's section; withBody includes the guidance
// body (and its notes), the reduced form omits it.
func conflictSection(r DivergenceReport, withBody bool) string {
	g := r.Conflict
	var b strings.Builder
	fmt.Fprintf(&b, "\n### Backflow conflict: %s → %s\n\n", textsafe.Markdown(r.From), textsafe.Markdown(r.To))
	b.WriteString("No request was created for this edge.\n\n")
	fmt.Fprintf(&b, "- **Failing commit:** `%s` — %s\n", g.SHA, textsafe.Markdown(textsafe.Subject(g.Subject)))
	if g.Whole {
		b.WriteString("- **The whole set conflicted** (merge strategy)\n")
	} else {
		fmt.Fprintf(&b, "- **Applied cleanly before the conflict:** %d\n", g.Applied)
	}
	switch g.Artifact {
	case ArtifactKnown:
		fmt.Fprintf(&b, "- **Conflict record:** [%s %s](%s)\n", g.noun(), textsafe.Markdown(g.ArtifactID), g.ArtifactURL)
	case ArtifactUnhostable:
		fmt.Fprintf(&b, "- **Conflict record:** none. No durable conflict record can be kept (Issues are disabled), so "+
			"this section is the only guidance for this conflict, and it lasts only as long as the CI host keeps "+
			"this run. See [Keep Issues enabled](%s).\n", keepIssuesURL)
	default:
		fmt.Fprintf(&b, "- **Conflict record:** the conflict %s could not be recorded or confirmed this run "+
			"and will be retried next run.\n", g.noun())
	}
	if withBody {
		b.WriteString("\n")
		switch {
		case g.RenderFailed:
			b.WriteString(renderFailedNote + "\n")
		case g.BuiltinFallback:
			b.WriteString(builtinFallbackNote + "\n\n" + g.SummaryBody + "\n")
		default:
			b.WriteString(g.SummaryBody + "\n")
		}
	}
	fmt.Fprintf(&b, "\nSee [When a replay conflicts](%s) in the backflow guide.\n", BackflowGuideURL)
	return b.String()
}
