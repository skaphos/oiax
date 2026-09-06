package cli

import (
	"fmt"
	"os"

	"github.com/skaphos/oiax/v2/internal/cienv"
	"github.com/skaphos/oiax/v2/internal/reconcile"
	"github.com/spf13/cobra"
)

func writeNotificationSummary(cmd *cobra.Command, diagnostics []reconcile.NotificationDiagnostic) {
	if len(diagnostics) == 0 {
		return
	}
	path := os.Getenv("GITHUB_STEP_SUMMARY")
	azure := path == "" && cienv.Detect() == cienv.AzurePipelines
	if path == "" && !azure {
		return
	}
	var f *os.File
	var err error
	if azure {
		f, err = os.CreateTemp(os.Getenv("AGENT_TEMPDIRECTORY"), "oiax-notifications-*.md")
	} else {
		f, err = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	}
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "oiax: notification summary unavailable")
		return
	}
	_, writeErr := fmt.Fprint(f, "\n## Notification delivery\n\n")
	for _, d := range diagnostics {
		if writeErr == nil {
			_, writeErr = fmt.Fprintf(f, "- %s: %s. %s\n", d.Scope(), d.Reason, d.Action)
		}
	}
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "oiax: notification summary unavailable")
		return
	}
	if azure {
		fmt.Fprintf(cmd.ErrOrStderr(), "##vso[task.uploadsummary]%s\n", f.Name())
	}
}
