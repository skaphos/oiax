package reconcile

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/skaphos/oiax/internal/engine"
)

// RenderJSON writes the plan as indented JSON. The engine.Plan is already
// tagged (planFormatVersion:1); this is the compatibility-contract machine
// output, so the encode error is returned rather than dropped.
func RenderJSON(w io.Writer, plan engine.Plan) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(plan); err != nil {
		return fmt.Errorf("encode plan: %w", err)
	}
	return nil
}

// RenderText writes a human-readable plan summary: the graph name, one line
// per evaluated edge (sync status and the equivalence rung that settled it),
// then one line per action (verb, edge, commit count, reason). An empty plan
// renders "In sync, no actions." A write failure (closed pipe, full disk) is
// returned so callers fail predictably rather than emitting a partial plan.
func RenderText(w io.Writer, plan engine.Plan) error {
	ew := &errWriter{w: w}
	fmt.Fprintf(ew, "Promotion graph: %s\n", plan.Graph)
	for _, e := range plan.Edges {
		fmt.Fprintf(ew, "  edge %s -> %s: %s\n", e.From, e.To, edgeSummaryText(e))
	}
	if len(plan.Actions) == 0 {
		fmt.Fprintln(ew, "In sync, no actions.")
		return ew.err
	}
	for _, a := range plan.Actions {
		fmt.Fprintf(ew, "  %-8s %s -> %s (%d): %s\n",
			actionVerb(a.Type), a.From, a.To, a.Unpromoted, a.Reason)
	}
	return ew.err
}

// edgeSummaryText renders one edge's diagnostic tail: sync status with the
// settling rung, then the downstream/backflow counts that apply. The wording
// is human-facing and non-contractual — script against the plan JSON, not
// this text.
func edgeSummaryText(e engine.EdgeSummary) string {
	var b strings.Builder
	if e.InSync {
		b.WriteString("in sync")
	} else {
		fmt.Fprintf(&b, "%d unpromoted", e.Unpromoted)
	}
	fmt.Fprintf(&b, " (%s)", e.Equivalence)
	if e.DownstreamOnly > 0 {
		fmt.Fprintf(&b, ", %d downstream-only", e.DownstreamOnly)
	}
	if e.ToReturn > 0 {
		fmt.Fprintf(&b, ", %d to return", e.ToReturn)
	}
	if len(e.Excluded) > 0 {
		fmt.Fprintf(&b, ", %d excluded (%s)", len(e.Excluded), exclusionCounts(e.Excluded))
	}
	// A merge-strategy edge returns its downstream-only set wholesale
	// (all-or-nothing, ADR-0006). Strategy is populated only for such edges —
	// cherry-pick edges leave it empty, so their text is byte-identical to
	// before the merge strategy existed. Name the mechanism and, when there is
	// something to return, the wholesale set so the operator sees the
	// all-or-nothing scope at a glance.
	if e.Strategy != "" {
		fmt.Fprintf(&b, ", strategy: %s", e.Strategy)
		if len(e.Returned) > 0 {
			fmt.Fprintf(&b, " — returning %d wholesale: %s", len(e.Returned), returnedSubjects(e.Returned))
		}
	}
	return b.String()
}

// returnedSubjects renders a merge-strategy edge's wholesale return set as a
// comma-separated list of commit subjects, for the human-facing
// (non-contractual) plan summary.
func returnedSubjects(returned []engine.Commit) string {
	subjects := make([]string, len(returned))
	for i, c := range returned {
		subjects[i] = c.Subject
	}
	return strings.Join(subjects, ", ")
}

// exclusionCounts summarizes a backflow exclusion list as "N <reason>" terms
// in the ladder's precedence order (skip, provenance, patch-id), omitting
// absent reasons.
func exclusionCounts(excluded []engine.BackflowExclusion) string {
	byReason := make(map[engine.BackflowExclusionReason]int, len(excluded))
	for _, x := range excluded {
		byReason[x.Reason]++
	}
	var terms []string
	for _, r := range []engine.BackflowExclusionReason{
		engine.BackflowExcludedSkip,
		engine.BackflowExcludedProvenance,
		engine.BackflowExcludedPatchID,
	} {
		if n := byReason[r]; n > 0 {
			terms = append(terms, fmt.Sprintf("%d %s", n, r))
		}
	}
	return strings.Join(terms, ", ")
}

// RenderMarkdown writes a Markdown rendering of the plan, used for the
// GitHub Actions step summary. Write errors are returned so a failed
// summary write cannot silently produce a truncated table.
func RenderMarkdown(w io.Writer, plan engine.Plan) error {
	ew := &errWriter{w: w}
	fmt.Fprintf(ew, "## Oiax plan: %s\n\n", plan.Graph)
	if len(plan.Edges) > 0 {
		fmt.Fprintln(ew, "| Edge | State | Settled by | Downstream-only | To return | Excluded |")
		fmt.Fprintln(ew, "| --- | --- | --- | --- | --- | --- |")
		for _, e := range plan.Edges {
			state := "in sync"
			if !e.InSync {
				state = fmt.Sprintf("%d unpromoted", e.Unpromoted)
			}
			// Note the merge strategy and its wholesale return count inside the
			// State cell rather than as a new column, so cherry-pick rows (whose
			// Strategy is empty) stay byte-identical and the table keeps its
			// column count.
			if e.Strategy != "" {
				state += fmt.Sprintf(" (%s: returns %d)", e.Strategy, len(e.Returned))
			}
			excluded := ""
			if len(e.Excluded) > 0 {
				excluded = exclusionCounts(e.Excluded)
			}
			fmt.Fprintf(ew, "| %s -> %s | %s | %s | %d | %d | %s |\n",
				mdCell(e.From), mdCell(e.To), mdCell(state), mdCell(string(e.Equivalence)),
				e.DownstreamOnly, e.ToReturn, mdCell(excluded))
		}
		fmt.Fprintln(ew)
	}
	if len(plan.Actions) == 0 {
		fmt.Fprintln(ew, "In sync, no actions.")
		return ew.err
	}
	fmt.Fprintln(ew, "| Action | From | To | Commits | Reason |")
	fmt.Fprintln(ew, "| --- | --- | --- | --- | --- |")
	for _, a := range plan.Actions {
		fmt.Fprintf(ew, "| %s | %s | %s | %d | %s |\n",
			mdCell(actionVerb(a.Type)), mdCell(a.From), mdCell(a.To), a.Unpromoted, mdCell(a.Reason))
	}
	return ew.err
}

// mdCell escapes a value for interpolation into a Markdown table cell. An
// unescaped '|' opens a new column, so it silently corrupts the step-summary
// table — and it is reachable: `git check-ref-format` accepts a branch named
// "feat|bar", and the v1 config validator rejects only " ~^:?*[\" and control
// characters, so such a name passes validation and reaches these cells (both
// directly and inside an action's reason). Newlines would end the row early;
// no ref name can carry one (both validators reject control characters) and
// reasons are single-line, but they are collapsed anyway so the helper is
// correct for any caller rather than only for today's.
func mdCell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.ReplaceAll(s, "\n", " ")
}

// errWriter accumulates the first write error so a sequence of fmt.Fprint
// calls can be issued without interleaved error checks; callers inspect
// err once at the end. After a write fails, further writes are skipped.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	var n int
	n, e.err = e.w.Write(p)
	return n, e.err
}

// actionVerb maps an action type to a compact verb for text output.
func actionVerb(t engine.ActionType) string {
	switch t {
	case engine.ActionCreatePromotionRequest:
		return "create"
	case engine.ActionCreateBackflowRequest:
		return "backflow"
	case engine.ActionUpdateManagedRequest:
		return "update"
	case engine.ActionCloseObsoleteRequest:
		return "close"
	case engine.ActionReportDivergence:
		return "report"
	default:
		return string(t)
	}
}
