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
// "In sync, no actions."
func RenderText(w io.Writer, plan engine.Plan) error {
	fmt.Fprintf(w, "Promotion graph: %s\n", plan.Graph)
	if len(plan.Actions) == 0 {
		fmt.Fprintln(w, "In sync, no actions.")
		return nil
	}
	for _, a := range plan.Actions {
		fmt.Fprintf(w, "  %-8s %s -> %s (%d): %s\n",
			actionVerb(a.Type), a.From, a.To, a.Unpromoted, a.Reason)
	}
	return nil
}

// RenderMarkdown writes a Markdown rendering of the plan, used for the
// GitHub Actions step summary.
func RenderMarkdown(w io.Writer, plan engine.Plan) error {
	fmt.Fprintf(w, "## Oiax plan: %s\n\n", plan.Graph)
	if len(plan.Actions) == 0 {
		fmt.Fprintln(w, "In sync, no actions.")
		return nil
	}
	fmt.Fprintln(w, "| Action | From | To | Commits | Reason |")
	fmt.Fprintln(w, "| --- | --- | --- | --- | --- |")
	for _, a := range plan.Actions {
		fmt.Fprintf(w, "| %s | %s | %s | %d | %s |\n",
			actionVerb(a.Type), a.From, a.To, a.Unpromoted, a.Reason)
	}
	return nil
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
