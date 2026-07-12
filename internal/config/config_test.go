package config

import (
	"fmt"
	"strings"
	"testing"

	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

func TestLoadExample(t *testing.T) {
	cfg, err := Load("testdata/environments.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Metadata.Name != "environments" {
		t.Errorf("metadata.name = %q, want %q", cfg.Metadata.Name, "environments")
	}
	if got := len(cfg.Spec.Branches); got != 5 {
		t.Errorf("len(branches) = %d, want 5", got)
	}
	if got := len(cfg.Spec.Promotions); got != 4 {
		t.Errorf("len(promotions) = %d, want 4", got)
	}
	if cfg.Spec.Backflow == nil {
		t.Fatal("backflow = nil, want configured")
	}
	if cfg.Spec.Backflow.Target != "development" {
		t.Errorf("backflow.target = %q, want %q", cfg.Spec.Backflow.Target, "development")
	}
	if cfg.Spec.Backflow.Strategy != v1.BackflowStrategyCherryPick {
		t.Errorf("backflow.strategy = %q, want %q", cfg.Spec.Backflow.Strategy, v1.BackflowStrategyCherryPick)
	}
	if cfg.Spec.Branches["development"].Role != v1.RoleSource {
		t.Errorf("development role = %q, want %q", cfg.Spec.Branches["development"].Role, v1.RoleSource)
	}
}

func TestParseRejections(t *testing.T) {
	valid := `apiVersion: oiax.skaphos.dev/v1
kind: PromotionGraph
metadata:
  name: g
spec:
  branches:
    a: {}
    b: {}
  promotions:
    - from: a
      to: b
`

	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "empty document",
			input:   "",
			wantErr: "empty",
		},
		{
			name:    "wrong apiVersion",
			input:   strings.Replace(valid, "oiax.skaphos.dev/v1", "oiax.skaphos.dev/v9", 1),
			wantErr: "unsupported apiVersion",
		},
		{
			name:    "wrong kind",
			input:   strings.Replace(valid, "PromotionGraph", "Promotion", 1),
			wantErr: "unsupported kind",
		},
		{
			name:    "unknown field",
			input:   strings.Replace(valid, "spec:", "spec:\n  hooks: [run-me]", 1),
			wantErr: "field hooks not found",
		},
		{
			name:    "multiple documents",
			input:   valid + "---\n" + valid,
			wantErr: "multiple YAML documents",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.input))
			if err == nil {
				t.Fatal("Parse succeeded, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}

	if _, err := Parse([]byte(valid)); err != nil {
		t.Errorf("Parse(valid) = %v, want nil", err)
	}
}

// TestParseAPIVersions pins the apiVersion contract: the canonical
// oiax.skaphos.dev/v1 parses, the deprecated oiax.skaphos.dev/v1alpha1 alias
// still parses for backward compatibility, and only the alias is reported
// deprecated. Rejection of an unknown apiVersion is covered by
// TestParseRejections.
func TestParseAPIVersions(t *testing.T) {
	tmpl := `apiVersion: %s
kind: PromotionGraph
metadata:
  name: g
spec:
  branches:
    a: {}
    b: {}
  promotions:
    - from: a
      to: b
`

	tests := []struct {
		name           string
		apiVersion     string
		wantDeprecated bool
	}{
		{name: "canonical v1", apiVersion: v1.APIVersion, wantDeprecated: false},
		{name: "deprecated v1alpha1 alias", apiVersion: v1.APIVersionV1Alpha1, wantDeprecated: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse([]byte(fmt.Sprintf(tmpl, tt.apiVersion)))
			if err != nil {
				t.Fatalf("Parse(%s) = %v, want nil", tt.apiVersion, err)
			}
			if cfg.APIVersion != tt.apiVersion {
				t.Errorf("apiVersion = %q, want %q", cfg.APIVersion, tt.apiVersion)
			}
			if got := IsDeprecatedAPIVersion(cfg.APIVersion); got != tt.wantDeprecated {
				t.Errorf("IsDeprecatedAPIVersion(%q) = %v, want %v", cfg.APIVersion, got, tt.wantDeprecated)
			}
		})
	}
}
