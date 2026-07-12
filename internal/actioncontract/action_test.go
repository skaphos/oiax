// SPDX-FileCopyrightText: 2026 Skaphos
// SPDX-License-Identifier: MIT

package actioncontract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type actionMetadata struct {
	Description string `yaml:"description"`
	Runs        struct {
		Using string `yaml:"using"`
		Steps []struct {
			Name  string `yaml:"name"`
			Shell string `yaml:"shell"`
			Run   string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"runs"`
}

func TestPublishedActionRunnerContract(t *testing.T) {
	actionPath := filepath.Join("..", "..", "action.yml")
	b, err := os.ReadFile(actionPath)
	if err != nil {
		t.Fatalf("read %s: %v", actionPath, err)
	}

	var action actionMetadata
	if err := yaml.Unmarshal(b, &action); err != nil {
		t.Fatalf("parse %s: %v", actionPath, err)
	}
	if action.Runs.Using != "composite" {
		t.Fatalf("runs.using = %q, want composite", action.Runs.Using)
	}
	if !strings.Contains(action.Description, "Linux GitHub Actions runner") {
		t.Errorf("description does not advertise the Linux-only runner contract: %q", action.Description)
	}

	var download string
	for _, step := range action.Runs.Steps {
		if step.Name == "Download oiax" {
			if step.Shell != "bash" {
				t.Errorf("download shell = %q, want bash", step.Shell)
			}
			download = step.Run
			break
		}
	}
	if download == "" {
		t.Fatal("Download oiax step is missing")
	}

	for _, supported := range []string{"Linux-X64)", "Linux-ARM64)"} {
		if !strings.Contains(download, supported) {
			t.Errorf("download step does not support %s", supported)
		}
	}
	for _, unsupported := range []string{"macOS-", "darwin_"} {
		if strings.Contains(download, unsupported) {
			t.Errorf("download step still advertises unsupported macOS assets via %q", unsupported)
		}
	}
	if !strings.Contains(download, "sha256sum -c -") {
		t.Error("download step does not verify the selected release asset checksum")
	}
}
