package cli

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/skaphos/oiax/v2/internal/cienv"
	"github.com/skaphos/oiax/v2/internal/reconcile"
	"github.com/spf13/cobra"
)

// divergenceMessage builds reconcile's exit-3 message (spec 002 FR-009,
// FR-010, contracts/surfaces.md §3): a first line naming how many edges
// diverged, then one line per edge in plan order. Only trusted text appears:
// branch names, artifact addresses, and fixed wording, never a commit
// subject or a plan reason.
func divergenceMessage(reports []reconcile.DivergenceReport, summaryWritten bool) string {
	const first = "converged with reported divergence"
	if len(reports) == 0 {
		return first
	}
	unit := "edges"
	if len(reports) == 1 {
		unit = "edge"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s on %d %s:", first, len(reports), unit)
	steps := "resolution steps: " + reconcile.BackflowGuideURL
	if summaryWritten {
		steps = "resolution steps are in this run's summary"
	}
	for _, r := range reports {
		fmt.Fprintf(&b, "\n  %s -> %s: ", r.From, r.To)
		g := r.Conflict
		if r.Kind != reconcile.KindConflict || g == nil {
			b.WriteString("reported divergence")
			continue
		}
		switch g.Artifact {
		case reconcile.ArtifactKnown:
			b.WriteString("backflow conflict; resolution steps: " + g.ArtifactURL)
		case reconcile.ArtifactUnhostable:
			b.WriteString("backflow conflict; no durable conflict record (Issues are disabled); " + steps)
		default:
			noun := g.ArtifactNoun
			if noun == "" {
				noun = "issue"
			}
			b.WriteString("backflow conflict; conflict " + noun + " not recorded this run, will be retried next run; " + steps)
		}
	}
	return b.String()
}

// publishConflicts writes the conflict summary sections, then emits one
// warning per conflicting edge, choosing each pointer by whether the
// sections were written (spec 002 FR-001, FR-002, FR-010). It runs after
// every other summary write, so the size budget holds (research R8), and it
// returns whether the sections were written for the exit-3 message.
func publishConflicts(cmd *cobra.Command, log *slog.Logger, reports []reconcile.DivergenceReport) bool {
	written := writeConflictSummary(cmd, reports)
	if log == nil {
		return written
	}
	for _, r := range reports {
		if r.Kind == reconcile.KindConflict && r.Conflict != nil {
			log.Warn(reconcile.ConflictWarning(r, written))
		}
	}
	return written
}

// writeConflictSummary renders the conflict sections within the host's
// remaining summary budget and writes them to the summary target. It returns
// true only when the sections reached the summary.
func writeConflictSummary(cmd *cobra.Command, reports []reconcile.DivergenceReport) bool {
	budget, ok := conflictSummaryBudget()
	if !ok {
		return false
	}
	var buf bytes.Buffer
	written, err := reconcile.RenderConflictSections(&buf, reports, budget)
	if err != nil || !written {
		return false
	}
	w, finish, ok := summaryTarget(cmd, "oiax-conflicts-*.md")
	if !ok {
		return false
	}
	_, werr := w.Write(buf.Bytes())
	if err := finish(werr); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "oiax: step summary: %v\n", err)
		return false
	}
	return true
}

// conflictSummaryBudget is the space left for the conflict sections: on
// GitHub the step-summary file is shared with the plan and notification
// summaries, so it is the limit minus the file's current size; on Azure each
// uploaded file gets the whole limit. ok is false with no summary
// destination.
func conflictSummaryBudget() (budget int, ok bool) {
	if path := os.Getenv("GITHUB_STEP_SUMMARY"); path != "" {
		var size int64
		if fi, err := os.Stat(path); err == nil {
			size = fi.Size()
		}
		return max(0, reconcile.SummaryLimit-int(size)), true
	}
	if cienv.Detect() == cienv.AzurePipelines {
		return reconcile.SummaryLimit, true
	}
	return 0, false
}
