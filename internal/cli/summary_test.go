package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// summaryCmd is a command whose stderr the test can read.
func summaryCmd() (*cobra.Command, *bytes.Buffer) {
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&errOut)
	return cmd, &errOut
}

// clearCIHost removes every variable that selects a CI host.
func clearCIHost(t *testing.T) {
	t.Helper()
	for _, k := range []string{"GITHUB_ACTIONS", "GITHUB_STEP_SUMMARY", "TF_BUILD", "AGENT_TEMPDIRECTORY"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
}

// Under GitHub Actions the target appends to $GITHUB_STEP_SUMMARY.
func TestSummaryTargetGitHubAppends(t *testing.T) {
	clearCIHost(t)
	path := filepath.Join(t.TempDir(), "summary.md")
	if err := os.WriteFile(path, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_STEP_SUMMARY", path)
	cmd, errOut := summaryCmd()

	w, finish, ok := summaryTarget(cmd, "oiax-test-*.md")
	if !ok {
		t.Fatal("summaryTarget returned ok == false under GitHub Actions")
	}
	_, werr := io.WriteString(w, "appended\n")
	if err := finish(werr); err != nil {
		t.Fatalf("finish: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "existing\nappended\n" {
		t.Fatalf("summary = %q, want the existing content then the append", got)
	}
	if errOut.Len() != 0 {
		t.Fatalf("GitHub summary wrote to stderr: %q", errOut.String())
	}
}

// Under Azure Pipelines the target is a temp file in AGENT_TEMPDIRECTORY,
// attached by finish with an escaped ##vso[task.uploadsummary] path.
func TestSummaryTargetAzureUploadsEscapedPath(t *testing.T) {
	clearCIHost(t)
	dir := filepath.Join(t.TempDir(), "agent%tmp\r\nx")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TF_BUILD", "True")
	t.Setenv("AGENT_TEMPDIRECTORY", dir)
	cmd, errOut := summaryCmd()

	w, finish, ok := summaryTarget(cmd, "oiax-test-*.md")
	if !ok {
		t.Fatal("summaryTarget returned ok == false under Azure Pipelines")
	}
	_, werr := io.WriteString(w, "body\n")
	if err := finish(werr); err != nil {
		t.Fatalf("finish: %v", err)
	}
	line := errOut.String()
	if !strings.HasPrefix(line, "##vso[task.uploadsummary]") || strings.Count(line, "\n") != 1 {
		t.Fatalf("stderr = %q, want one uploadsummary command line", line)
	}
	data := strings.TrimSuffix(strings.TrimPrefix(line, "##vso[task.uploadsummary]"), "\n")
	if strings.ContainsAny(data, "\r\n") || !strings.Contains(data, "agent%AZP25tmp%0D%0Ax") {
		t.Fatalf("uploaded path %q is not escaped as command data", data)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "oiax-test-*.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("temp summary files = %v (%v), want one", matches, err)
	}
	if got, _ := os.ReadFile(matches[0]); string(got) != "body\n" {
		t.Fatalf("temp summary = %q", got)
	}
}

// A failed write is reported by finish, and Azure does not attach a file
// that was not fully written.
func TestSummaryTargetAzureWriteErrorNotUploaded(t *testing.T) {
	clearCIHost(t)
	t.Setenv("TF_BUILD", "True")
	t.Setenv("AGENT_TEMPDIRECTORY", t.TempDir())
	cmd, errOut := summaryCmd()
	_, finish, ok := summaryTarget(cmd, "oiax-test-*.md")
	if !ok {
		t.Fatal("summaryTarget returned ok == false")
	}
	boom := errors.New("boom")
	if err := finish(boom); !errors.Is(err, boom) {
		t.Fatalf("finish = %v, want the write error", err)
	}
	if strings.Contains(errOut.String(), "uploadsummary") {
		t.Fatalf("a failed summary was attached: %q", errOut.String())
	}
}

// With no CI host, or a destination that cannot be opened, there is no
// target.
func TestSummaryTargetNoDestination(t *testing.T) {
	clearCIHost(t)
	cmd, errOut := summaryCmd()
	if _, _, ok := summaryTarget(cmd, "oiax-test-*.md"); ok {
		t.Fatal("summaryTarget returned ok with no CI host")
	}
	if errOut.Len() != 0 {
		t.Fatalf("no-host case wrote to stderr: %q", errOut.String())
	}
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_STEP_SUMMARY", filepath.Join(t.TempDir(), "missing", "summary.md"))
	if _, _, ok := summaryTarget(cmd, "oiax-test-*.md"); ok {
		t.Fatal("summaryTarget returned ok for an unopenable path")
	}
	if !strings.Contains(errOut.String(), "oiax: step summary:") {
		t.Fatalf("open failure not reported: %q", errOut.String())
	}
}
