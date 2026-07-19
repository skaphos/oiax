package config

import (
	"fmt"
	"os"
	"path/filepath"
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

// TestLoadRejectsOversizedFile proves Load caps the bytes it reads: a file
// at maxConfigSize+1 is rejected with a clear error rather than read into
// memory in full, so a pathological .oiax.yaml cannot exhaust memory.
func TestLoadRejectsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.yaml")
	if err := os.WriteFile(path, make([]byte, maxConfigSize+1), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load succeeded on an oversized file, want a size-limit error")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error = %v, want it to explain the size limit", err)
	}
}

// TestLoadAcceptsFileAtLimit proves the cap is inclusive of exactly
// maxConfigSize bytes (only maxConfigSize+1 and above are rejected).
func TestLoadAcceptsFileAtLimit(t *testing.T) {
	valid, err := os.ReadFile("testdata/environments.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(valid) >= maxConfigSize {
		t.Fatalf("fixture is %d bytes, too large to pad up to maxConfigSize %d", len(valid), maxConfigSize)
	}
	// Pad exactly to maxConfigSize with trailing comment-only lines, which
	// YAML ignores.
	padded := append([]byte{}, valid...)
	for len(padded) < maxConfigSize {
		padded = append(padded, '#', '\n')
	}
	padded = padded[:maxConfigSize]

	dir := t.TempDir()
	path := filepath.Join(dir, "at-limit.yaml")
	if err := os.WriteFile(path, padded, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err != nil {
		t.Fatalf("Load(file at exactly maxConfigSize) = %v, want nil", err)
	}
}

// TestParseRejectsOversizedData proves the size cap is enforced by Parse
// itself, not just by Load's pre-read: the pinned-ref path (loadGraph's
// configRef branch, via git.Runner.ShowFile) decodes bytes straight from
// `git show` and never goes through Load, so Parse has to be the
// authoritative guard.
func TestParseRejectsOversizedData(t *testing.T) {
	_, err := Parse(make([]byte, maxConfigSize+1))
	if err == nil {
		t.Fatal("Parse succeeded on oversized data, want a size-limit error")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error = %v, want it to explain the size limit", err)
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

// TestParseAcceptsTemplates locks the spec.templates configuration surface
// (SKA-54) against the strict KnownFields decoder: every documented key
// parses, and a typo inside a template block is still rejected.
func TestParseAcceptsTemplates(t *testing.T) {
	doc := []byte(`apiVersion: oiax.skaphos.dev/v1
kind: PromotionGraph
metadata:
  name: environments
spec:
  branches:
    development: {role: source}
    main: {role: terminal}
  promotions:
    - from: development
      to: main
      templates:
        title: "edge title {{.From}}"
  backflow:
    sources: [main]
    target: development
    strategy: merge
  templates:
    promotion:
      title: "t"
      bodyFile: .oiax/templates/promotion.md.tmpl
    backflow:
      body: "b"
    backflowConflict:
      title: "t"
    backflowMergeMessage:
      text: "m"
`)
	cfg, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	tp := cfg.Spec.Templates
	if tp == nil || tp.Promotion == nil || tp.Promotion.BodyFile != ".oiax/templates/promotion.md.tmpl" {
		t.Fatalf("templates = %+v, want the promotion bodyFile parsed", tp)
	}
	if tp.BackflowMergeMessage == nil || tp.BackflowMergeMessage.Text != "m" {
		t.Errorf("backflowMergeMessage = %+v", tp.BackflowMergeMessage)
	}
	if cfg.Spec.Promotions[0].Templates == nil || cfg.Spec.Promotions[0].Templates.Title == "" {
		t.Errorf("per-edge templates = %+v", cfg.Spec.Promotions[0].Templates)
	}

	typo := []byte(`apiVersion: oiax.skaphos.dev/v1
kind: PromotionGraph
metadata:
  name: environments
spec:
  branches:
    development: {role: source}
  promotions: []
  templates:
    promotion:
      bodyfile: wrong-case
`)
	if _, err := Parse(typo); err == nil {
		t.Fatal("Parse must reject an unknown template field (bodyfile)")
	}
}
