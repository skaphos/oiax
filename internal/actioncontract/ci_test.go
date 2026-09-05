package actioncontract_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestCIPlatformSupportTiers(t *testing.T) {
	t.Parallel()
	var workflow struct {
		Jobs map[string]struct {
			Name     string   `yaml:"name"`
			RunsOn   string   `yaml:"runs-on"`
			Timeout  string   `yaml:"timeout-minutes"`
			Needs    []string `yaml:"needs"`
			Strategy struct {
				Matrix struct {
					OS []string `yaml:"os"`
				} `yaml:"matrix"`
			} `yaml:"strategy"`
			Steps []struct {
				If  string `yaml:"if"`
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	readRepoYAML(t, ".github/workflows/ci.yml", &workflow)
	job := workflow.Jobs["test"]
	if job.Name != "Test" || job.RunsOn != "${{ matrix.os }}" ||
		!slices.Equal(job.Strategy.Matrix.OS, []string{"ubuntu-24.04", "macos-26", "windows-2025"}) {
		t.Fatal("test check names or native platform matrix changed")
	}
	if job.Timeout != "${{ matrix.os == 'ubuntu-24.04' && 25 || 10 }}" {
		t.Fatal("test job budgets no longer match the support policy")
	}
	want := map[string]string{
		"go -C tools tool task test-cover":       "matrix.os == 'ubuntu-24.04'",
		"go -C tools tool task test-race":        "matrix.os == 'ubuntu-24.04'",
		"go -C tools tool task test-portability": "matrix.os != 'ubuntu-24.04'",
	}
	found := map[string]bool{}
	for _, step := range job.Steps {
		if condition, ok := want[step.Run]; ok {
			if step.If != condition {
				t.Errorf("%s has condition %q, want %q", step.Run, step.If, condition)
			}
			if found[step.Run] {
				t.Errorf("duplicate test step: %s", step.Run)
			}
			found[step.Run] = true
		}
	}
	for run := range want {
		if !found[run] {
			t.Errorf("missing platform test gate: %s", run)
		}
	}
	if !slices.Contains(workflow.Jobs["build"].Needs, "test") {
		t.Fatal("release snapshot build no longer waits for platform checks")
	}
}

func TestPortabilityTaskKeepsBoundedUnitSelection(t *testing.T) {
	t.Parallel()
	var taskfile struct {
		Tasks map[string]yaml.Node `yaml:"tasks"`
	}
	readRepoYAML(t, "Taskfile.yml", &taskfile)
	var task struct {
		Cmds []string `yaml:"cmds"`
	}
	node, ok := taskfile.Tasks["test-portability"]
	if !ok {
		t.Fatal("missing portability task")
	}
	if err := node.Decode(&task); err != nil {
		t.Fatal(err)
	}
	if len(task.Cmds) != 2 || task.Cmds[0] != "go build ./..." {
		t.Fatal("portability must compile all packages before running unit tests")
	}
	args := strings.Fields(task.Cmds[1])
	if len(args) < 4 || !slices.Equal(args[:3], []string{"go", "test", "-timeout=3m"}) {
		t.Fatal("portability unit tests must have a three-minute deadline")
	}
	// Full integration packages cannot sneak into this task through broad
	// recursive globs; additions require an explicit support-tier review.
	want := []string{"./cmd/oiax", "./internal/actioncontract", "./internal/cienv",
		"./internal/config", "./internal/engine", "./internal/forge",
		"./internal/forge/marker", "./internal/tmpl", "./pkg/api/..."}
	if !slices.Equal(args[3:], want) {
		t.Fatalf("review changes to the portability unit selection: %v", args[3:])
	}
}

func readRepoYAML(t *testing.T, name string, value any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, value); err != nil {
		t.Fatal(err)
	}
}
