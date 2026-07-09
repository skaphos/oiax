package config

import (
	"strings"
	"testing"

	"github.com/skaphos/oiax/pkg/api/v1alpha1"
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
	if cfg.Spec.Backflow.Strategy != v1alpha1.BackflowStrategyCherryPick {
		t.Errorf("backflow.strategy = %q, want %q", cfg.Spec.Backflow.Strategy, v1alpha1.BackflowStrategyCherryPick)
	}
	if cfg.Spec.Branches["development"].Role != v1alpha1.RoleSource {
		t.Errorf("development role = %q, want %q", cfg.Spec.Branches["development"].Role, v1alpha1.RoleSource)
	}
}

func TestParseRejections(t *testing.T) {
	valid := `apiVersion: oiax.skaphos.dev/v1alpha1
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
			input:   strings.Replace(valid, "oiax.skaphos.dev/v1alpha1", "oiax.skaphos.dev/v9", 1),
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
