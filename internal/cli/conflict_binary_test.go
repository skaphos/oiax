package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skaphos/oiax/v2/internal/gittest"
)

// The built binary, run as GitHub Actions against the real GitHub provider
// and a fixture forge, publishes the conflict guidance end to end: one
// warning ending with the issue address, the plan summary followed by one
// conflict section, and exit 3 with the multi-line message (spec 002
// research R10, constitution: built-binary verification; T060).
func TestConflictGuidanceBinary(t *testing.T) {
	t.Parallel()
	binary := buildNotificationBinary(t)
	f := newNotificationBinaryFixture(t, binary, "github", "webhook")
	f.allowIssues = true

	// The fixture graph backflows main, which no promotion edge reaches; pin a
	// config that backflows stage (reached by dev -> stage) into dev instead.
	config := strings.Replace(strings.Replace(notificationBinaryConfig, "%s", "webhook", 1),
		"backflow: {sources: [main], target: dev}", "backflow: {sources: [stage], target: dev}", 1)
	writeNotificationFixture(t, filepath.Join(f.dir, ".oiax.yaml"), []byte(config))
	gittest.Run(t, f.dir, "commit", "-qam", "backflow stage")
	f.configRef = gittest.Run(t, f.dir, "rev-parse", "HEAD")

	// stage and dev edit app.txt divergently: the stage -> dev backflow
	// cherry-pick conflicts.
	gittest.Run(t, f.dir, "checkout", "-q", "dev")
	writeNotificationFixture(t, filepath.Join(f.dir, "app.txt"), []byte("dev change\n"))
	gittest.Run(t, f.dir, "commit", "-qam", "dev edit")
	gittest.Run(t, f.dir, "checkout", "-q", "stage")
	writeNotificationFixture(t, filepath.Join(f.dir, "app.txt"), []byte("hotfix change\n"))
	gittest.Run(t, f.dir, "commit", "-qam", "fix: conflicting hotfix")
	gittest.Run(t, f.dir, "checkout", "-q", "main")
	f.publish("dev", "stage")

	summary := filepath.Join(t.TempDir(), "step-summary.md")
	f.extraEnv = []string{"GITHUB_ACTIONS=true", "GITHUB_STEP_SUMMARY=" + summary, "OIAX_LOG_FORMAT="}
	_, stderr := f.run(3, "reconcile")

	const issue = "https://github.com/example/repo/issues/77"
	var warnings []string
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, "::warning::") && strings.Contains(line, "halted on") {
			warnings = append(warnings, line)
		}
	}
	if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "::warning::backflow stage -> dev halted on cherry-pick conflict at ") ||
		!strings.HasSuffix(warnings[0], ". Resolution steps: "+issue) {
		t.Fatalf("want one conflict warning ending with %s, got:\n%s", issue, stderr)
	}
	if !strings.Contains(stderr, "converged with reported divergence on 1 edge:\n  stage -> dev: backflow conflict; resolution steps: "+issue) {
		t.Fatalf("stderr lacks the exit-3 message:\n%s", stderr)
	}
	data, err := os.ReadFile(summary)
	if err != nil {
		t.Fatal(err)
	}
	sum := string(data)
	plan := strings.Index(sum, "## ")
	section := strings.Index(sum, "### Backflow conflict: stage → dev")
	if plan < 0 || section < plan || strings.Count(sum, "### Backflow conflict:") != 1 || !strings.Contains(sum, "[issue 77]("+issue+")") {
		t.Fatalf("summary does not have the plan then one conflict section with the issue link:\n%s", sum)
	}
	f.mu.Lock()
	created := len(f.createdIssues)
	f.mu.Unlock()
	if created != 1 {
		t.Fatalf("created %d conflict issues, want 1", created)
	}
	if strings.Contains(stderr+sum, "fixture-forge-canary") {
		t.Fatal("the forge token reached a conflict surface")
	}
}
