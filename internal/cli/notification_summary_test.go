package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/notification"
	"github.com/skaphos/oiax/internal/reconcile"
	"github.com/spf13/cobra"
)

func TestNotificationSummaryLabelsGlobalAndDestinationProblems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", path)
	global := reconcile.NotificationProblem(notification.ErrInvalidState)
	destination := reconcile.NotificationProblem(nil)
	destination.Destination = "ops"
	cmd := &cobra.Command{}
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	writeNotificationSummary(cmd, []reconcile.NotificationDiagnostic{global, destination})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"- all destinations: invalid-notification-state.", "- ops: delivered."} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %q in summary: %s", want, data)
		}
	}
	if strings.Contains(string(data), "- :") || stderr.Len() != 0 {
		t.Fatalf("blank scope or summary failure: %s %s", data, &stderr)
	}
}
