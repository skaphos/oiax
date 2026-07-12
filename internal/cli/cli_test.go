package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/gittest"
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

const exampleConfig = `apiVersion: oiax.skaphos.dev/v1
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

func TestDeprecatedAPIVersionWarns(t *testing.T) {
	// The canonical v1 config loads without any deprecation warning.
	out, err := run(t, "validate", "--config", writeConfig(t, exampleConfig))
	if err != nil {
		t.Fatalf("validate: %v\n%s", err, out)
	}
	if strings.Contains(out, "deprecated") {
		t.Errorf("canonical v1 config warned about deprecation:\n%s", out)
	}

	// The deprecated v1alpha1 alias still validates but emits exactly one
	// warning naming both the deprecated string and the canonical target.
	alias := strings.Replace(exampleConfig, "oiax.skaphos.dev/v1", "oiax.skaphos.dev/v1alpha1", 1)
	out, err = run(t, "validate", "--config", writeConfig(t, alias))
	if err != nil {
		t.Fatalf("validate alias: %v\n%s", err, out)
	}
	for _, want := range []string{
		`warning: apiVersion "oiax.skaphos.dev/v1alpha1" is deprecated`,
		`migrate to "oiax.skaphos.dev/v1"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if got := strings.Count(out, "warning:"); got != 1 {
		t.Errorf("want exactly one warning line, got %d:\n%s", got, out)
	}
}

// M12: a config with a not-ref-format branch name must fail validate in one
// round trip instead of passing validate/graph and only failing later with
// a raw git error out of plan/reconcile.
func TestValidateRejectsMalformedBranchName(t *testing.T) {
	broken := strings.Replace(exampleConfig, "development:", "foo bar:", 1)
	broken = strings.Replace(broken, "from: development", "from: foo bar", 1)

	out, err := run(t, "validate", "--config", writeConfig(t, broken))
	if err == nil {
		t.Fatalf("validate succeeded, want error:\n%s", out)
	}
	if !strings.Contains(out, "invalid branch name") {
		t.Errorf("output missing ref-format rejection:\n%s", out)
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

	gittest.InitRepo(t, dir)

	if err := os.WriteFile(filepath.Join(dir, ".oiax.yaml"), []byte(exampleConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", ".oiax.yaml")
	gittest.Run(t, dir, "commit", "-q", "-m", "add config")

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

func TestVersionCommandJSON(t *testing.T) {
	out, err := run(t, "version", "--output", "json")
	if err != nil {
		t.Fatalf("version -o json: %v\n%s", err, out)
	}
	var got struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Date    string `json:"date"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if got.Version != "dev" {
		t.Errorf("version = %q, want %q", got.Version, "dev")
	}
}

func TestRootVersionFlag(t *testing.T) {
	out, err := run(t, "--version")
	if err != nil {
		t.Fatalf("--version: %v\n%s", err, out)
	}
	if !strings.Contains(out, "oiax dev") {
		t.Errorf("output = %q, want dev version", out)
	}
}

// M10: validate and graph have no JSON rendering; -o/--output must be
// rejected with a clear error rather than silently emitting text (the bug
// was `graph -o json` succeeding with plain-text output and no complaint).
func TestValidateAndGraphRejectJSONOutput(t *testing.T) {
	cfgPath := writeConfig(t, exampleConfig)
	for _, cmdName := range []string{"validate", "graph"} {
		out, err := run(t, cmdName, "--config", cfgPath, "--output", "json")
		if err == nil {
			t.Fatalf("%s --output json succeeded, want a clear rejection:\n%s", cmdName, out)
		}
		if !strings.Contains(err.Error(), "not supported") {
			t.Errorf("%s --output json error = %v, want a not-supported message", cmdName, err)
		}
	}
}

// M10: `oiax -o json` with no subcommand falls back to help, which has no JSON
// rendering; it must be rejected rather than silently ignore --output.
func TestRootRejectsJSONOutputWithoutSubcommand(t *testing.T) {
	out, err := run(t, "--output", "json")
	if err == nil {
		t.Fatalf("oiax --output json (no subcommand) succeeded, want a clear rejection:\n%s", out)
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error = %v, want a not-supported message", err)
	}
}

func TestInvalidOutputFlagRejected(t *testing.T) {
	out, err := run(t, "validate", "--config", writeConfig(t, exampleConfig), "--output", "yaml")
	if err == nil {
		t.Fatalf("--output yaml succeeded, want rejection:\n%s", out)
	}
	if !strings.Contains(err.Error(), `"yaml"`) {
		t.Errorf("error = %v, want it to name the invalid value", err)
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
