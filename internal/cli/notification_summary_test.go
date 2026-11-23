package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skaphos/oiax/v2/internal/notification"
	"github.com/skaphos/oiax/v2/internal/reconcile"
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

// Under Azure Pipelines the notification summary's upload path is escaped as
// command data like every other summary (spec 002 FR-012, research R7): an
// AGENT_TEMPDIRECTORY holding '%' or a newline cannot corrupt or split the
// ##vso[task.uploadsummary] command (T045).
func TestNotificationSummaryAzureUploadPathEscaped(t *testing.T) {
	clearCIHost(t)
	dir := filepath.Join(t.TempDir(), "agent%tmp\nsplit")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TF_BUILD", "True")
	t.Setenv("AGENT_TEMPDIRECTORY", dir)
	cmd := &cobra.Command{}
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	writeNotificationSummary(cmd, []reconcile.NotificationDiagnostic{reconcile.NotificationProblem(notification.ErrInvalidState)})
	out := stderr.String()
	if !strings.HasPrefix(out, "##vso[task.uploadsummary]") || strings.Count(out, "\n") != 1 {
		t.Fatalf("stderr = %q, want one uploadsummary command line", out)
	}
	if !strings.Contains(out, "agent%AZP25tmp%0Asplit") {
		t.Fatalf("upload path is not escaped as command data: %q", out)
	}
}
