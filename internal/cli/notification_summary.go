package cli

import (
	"fmt"
	"io"

	"github.com/skaphos/oiax/v2/internal/reconcile"
	"github.com/spf13/cobra"
)

// writeNotificationSummary appends the notification delivery diagnostics to
// the CI job summary through the shared summaryTarget, so the Azure upload
// path is escaped like every other summary (spec 002 FR-012, research R7).
func writeNotificationSummary(cmd *cobra.Command, diagnostics []reconcile.NotificationDiagnostic) {
	if len(diagnostics) == 0 {
		return
	}
	w, finish, ok := summaryTarget(cmd, "oiax-notifications-*.md")
	if !ok {
		return
	}
	if err := finish(renderNotificationSummary(w, diagnostics)); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "oiax: notification summary unavailable")
	}
}

func renderNotificationSummary(w io.Writer, diagnostics []reconcile.NotificationDiagnostic) error {
	if _, err := fmt.Fprint(w, "\n## Notification delivery\n\n"); err != nil {
		return err
	}
	for _, d := range diagnostics {
		if _, err := fmt.Fprintf(w, "- %s: %s. %s\n", d.Scope(), d.Reason, d.Action); err != nil {
			return err
		}
	}
	return nil
}
