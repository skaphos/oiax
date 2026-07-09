package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// run executes the command tree with args and returns stdout+stderr and
// the returned error.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".oiax.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const exampleConfig = `apiVersion: oiax.skaphos.dev/v1alpha1
kind: PromotionGraph
metadata:
  name: environments
spec:
  branches:
    development:
      role: source
    test: {}
    main:
      role: terminal
  promotions:
    - from: development
      to: test
    - from: test
      to: main
  backflow:
    sources: [main]
    target: development
`

func TestValidateCommand(t *testing.T) {
	out, err := run(t, "validate", "--config", writeConfig(t, exampleConfig))
	if err != nil {
		t.Fatalf("validate: %v\n%s", err, out)
	}
	for _, want := range []string{"Configuration valid", `"environments"`, "3 branches", "2 promotion edges", "backflow enabled"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestValidateCommandReportsEveryViolation(t *testing.T) {
	broken := strings.Replace(exampleConfig, "name: environments", "name: \"\"", 1)
	broken = strings.Replace(broken, "sources: [main]", "sources: [main, development]", 1)

	out, err := run(t, "validate", "--config", writeConfig(t, broken))
	if err == nil {
		t.Fatalf("validate succeeded, want error:\n%s", out)
	}
	for _, want := range []string{"metadata.name is required", "backflow source and the backflow target"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestGraphCommand(t *testing.T) {
	out, err := run(t, "graph", "--config", writeConfig(t, exampleConfig))
	if err != nil {
		t.Fatalf("graph: %v\n%s", err, out)
	}
	for _, want := range []string{
		"Promotion graph: environments",
		"development  (source)",
		"development -> test",
		"Backflow (cherry-pick):",
		"main -> development",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestValidateWithConfigRefReadsPinnedRef(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	gitCmd := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitCmd("init", "-q", "-b", "main")
	gitCmd("config", "user.name", "test")
	gitCmd("config", "user.email", "test@example.invalid")

	if err := os.WriteFile(filepath.Join(dir, ".oiax.yaml"), []byte(exampleConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd("add", ".oiax.yaml")
	gitCmd("commit", "-q", "-m", "add config")

	// Break the working-tree copy: --config-ref must read the committed
	// version and still validate, proving the pinned-ref boundary.
	if err := os.WriteFile(filepath.Join(dir, ".oiax.yaml"), []byte("not: valid oiax config"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, "validate", "--config-ref", "main")
	if err != nil {
		t.Fatalf("validate --config-ref main: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Configuration valid") {
		t.Errorf("output missing validation success:\n%s", out)
	}

	// Without --config-ref the broken working-tree file is read.
	if _, err := run(t, "validate"); err == nil {
		t.Error("validate without --config-ref read the pinned version, want working-tree read to fail")
	}

	// Option-shaped refs are rejected before reaching git.
	if _, err := run(t, "validate", "--config-ref", "--output=/tmp/x"); err == nil {
		t.Error("validate accepted an option-shaped ref")
	}
}

func TestVersionCommand(t *testing.T) {
	out, err := run(t, "version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if !strings.Contains(out, "oiax dev") {
		t.Errorf("output = %q, want dev version", out)
	}
}

func TestGenDocs(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "cli.md")
	if _, err := run(t, "gen", "docs", "--out", outPath); err != nil {
		t.Fatalf("gen docs: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# CLI reference", "## oiax validate", "## oiax reconcile", "--detailed-exitcode"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("generated reference missing %q", want)
		}
	}
}
