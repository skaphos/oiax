package cli

import (
	"bytes"
	"errors"
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
	joined := reconcile.NotificationProblem(errors.Join(
		errors.New("receiver-credential-canary"),
		errors.New(string(notification.OutcomeMissingSecret)),
	))
	cmd := &cobra.Command{}
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	writeNotificationSummary(cmd, []reconcile.NotificationDiagnostic{global, destination, joined})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"- all destinations: invalid-notification-state.", "- ops: delivered.", "- all destinations: missing-secret. Set the named runtime endpoint variable, then retry."} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %q in summary: %s", want, data)
		}
	}
	if strings.Contains(string(data), "- :") || stderr.Len() != 0 {
		t.Fatalf("blank scope or summary failure: %s %s", data, &stderr)
	}
	if strings.Contains(string(data), "credential-canary") {
		t.Fatal("joined error leaked into summary")
	}
}
