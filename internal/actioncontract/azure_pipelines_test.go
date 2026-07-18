// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: MIT

package actioncontract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type pipelineTemplate struct {
	Parameters []struct {
		Name    string   `yaml:"name"`
		Type    string   `yaml:"type"`
		Default *string  `yaml:"default"`
		Values  []string `yaml:"values"`
	} `yaml:"parameters"`
	Steps []struct {
		Bash        string            `yaml:"bash"`
		DisplayName string            `yaml:"displayName"`
		Env         map[string]string `yaml:"env"`
	} `yaml:"steps"`
}

// TestPublishedAzurePipelinesTemplateContract pins the published Azure
// Pipelines steps template to the same wrapper contract as the composite
// Action: a checksum-verified Linux release binary download, ref
// preparation, and a mode-validated run — with no promotion logic in
// YAML. The template targets GitHub-hosted repositories; the forge is
// still GitHub.
func TestPublishedAzurePipelinesTemplateContract(t *testing.T) {
	path := filepath.Join("..", "..", "templates", "azure-pipelines", "oiax.yml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var tpl pipelineTemplate
	if err := yaml.Unmarshal(b, &tpl); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	params := map[string]struct {
		Type    string
		Default *string
		Values  []string
	}{}
	for _, p := range tpl.Parameters {
		params[p.Name] = struct {
			Type    string
			Default *string
			Values  []string
		}{p.Type, p.Default, p.Values}
	}

	// A template ref cannot read the release manifest the way the Action
	// ref does, so version must be an explicit, required parameter (no
	// default): consumers pin template ref and binary version together.
	version, ok := params["version"]
	if !ok {
		t.Fatal("version parameter is missing")
	}
	if version.Default != nil {
		t.Errorf("version parameter has default %q; it must be required so template ref and binary pin together", *version.Default)
	}

	mode, ok := params["mode"]
	if !ok {
		t.Fatal("mode parameter is missing")
	}
	if mode.Default == nil || *mode.Default != "reconcile" {
		t.Errorf("mode default = %v, want reconcile", mode.Default)
	}
	if want := []string{"validate", "plan", "reconcile"}; strings.Join(mode.Values, ",") != strings.Join(want, ",") {
		t.Errorf("mode values = %v, want %v", mode.Values, want)
	}

	if cfg, ok := params["config"]; !ok || cfg.Default == nil || *cfg.Default != ".oiax.yaml" {
		t.Errorf("config parameter must default to .oiax.yaml, got %+v", cfg)
	}
	// Both forge tokens are optional (empty default): a GitHub-hosted
	// consumer passes githubToken, an Azure Repos consumer passes
	// azureDevOpsToken; forge selection in the binary is automatic.
	for _, name := range []string{"githubToken", "azureDevOpsToken", "workItemType", "forge"} {
		p, ok := params[name]
		if !ok {
			t.Errorf("%s parameter is missing", name)
			continue
		}
		if p.Default == nil || *p.Default != "" {
			t.Errorf("%s parameter must default to empty, got %+v", name, p.Default)
		}
	}

	steps := map[string]string{}
	envs := map[string]map[string]string{}
	for _, s := range tpl.Steps {
		steps[s.DisplayName] = s.Bash
		envs[s.DisplayName] = s.Env
	}

	download, ok := steps["Download oiax"]
	if !ok {
		t.Fatal("Download oiax step is missing")
	}
	for _, want := range []string{"linux_amd64", "linux_arm64"} {
		if !strings.Contains(download, want) {
			t.Errorf("download step does not support %s", want)
		}
	}
	if !strings.Contains(download, `if [ "${AGENT_OS}" != "Linux" ]`) {
		t.Error("download step does not enforce the Linux-only agent contract")
	}
	// --ignore-missing verifies exactly the downloaded asset with no grep
	// preselection: the asset name never becomes a regex, and an asset
	// absent from checksums.txt fails ("no file was verified") instead of
	// passing silently. Mirrors the action.yml contract.
	if !strings.Contains(download, "sha256sum --check --ignore-missing checksums.txt") {
		t.Error("download step does not verify the downloaded release asset checksum")
	}
	if !strings.Contains(download, "##vso[task.prependpath]") {
		t.Error("download step does not prepend the binary to the pipeline PATH")
	}

	prepare, ok := steps["Prepare git refs"]
	if !ok {
		t.Fatal("Prepare git refs step is missing")
	}
	for _, want := range []string{"+refs/heads/*:refs/remotes/origin/*", "git remote set-head origin --auto"} {
		if !strings.Contains(prepare, want) {
			t.Errorf("prepare step is missing %q", want)
		}
	}

	run, ok := steps["Run oiax"]
	if !ok {
		t.Fatal("Run oiax step is missing")
	}
	if !strings.Contains(run, "validate|plan|reconcile)") {
		t.Error("run step does not validate the mode")
	}
	for env, param := range map[string]string{
		"GITHUB_TOKEN":           "parameters.githubToken",
		"AZURE_DEVOPS_TOKEN":     "parameters.azureDevOpsToken",
		"OIAX_ADO_WORKITEM_TYPE": "parameters.workItemType",
		"OIAX_FORGE":             "parameters.forge",
	} {
		if got := envs["Run oiax"][env]; !strings.Contains(got, param) {
			t.Errorf("run step %s env = %q, want it wired to %s", env, got, param)
		}
	}
}
