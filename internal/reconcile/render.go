package reconcile

import (
	"encoding/json"
	"fmt"
	"io"

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

// RenderText writes a human-readable plan summary: the graph name, then one
// line per action (verb, edge, commit count, reason). An empty plan renders
// "In sync, no actions." A write failure (closed pipe, full disk) is
// returned so callers fail predictably rather than emitting a partial plan.
func RenderText(w io.Writer, plan engine.Plan) error {
	ew := &errWriter{w: w}
	fmt.Fprintf(ew, "Promotion graph: %s\n", plan.Graph)
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

// RenderMarkdown writes a Markdown rendering of the plan, used for the
// GitHub Actions step summary. Write errors are returned so a failed
// summary write cannot silently produce a truncated table.
func RenderMarkdown(w io.Writer, plan engine.Plan) error {
	ew := &errWriter{w: w}
	fmt.Fprintf(ew, "## Oiax plan: %s\n\n", plan.Graph)
	if len(plan.Actions) == 0 {
		fmt.Fprintln(ew, "In sync, no actions.")
		return ew.err
	}
	fmt.Fprintln(ew, "| Action | From | To | Commits | Reason |")
	fmt.Fprintln(ew, "| --- | --- | --- | --- | --- |")
	for _, a := range plan.Actions {
		fmt.Fprintf(ew, "| %s | %s | %s | %d | %s |\n",
			actionVerb(a.Type), a.From, a.To, a.Unpromoted, a.Reason)
	}
	return ew.err
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
	case engine.ActionNoOp:
		return "noop"
	default:
		return string(t)
	}
}
