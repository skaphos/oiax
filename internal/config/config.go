// Package config loads and syntactically validates Oiax configuration.
//
// The configuration ref is pinned: callers read `.oiax.yaml` from exactly
// one ref per invocation (the repository default branch unless overridden
// with --config-ref), never from whatever ref triggered the run. That rule
// is both behavioral determinism and a security boundary — configuration
// proposed in an untrusted pull request is never executed with privileged
// credentials. This package parses bytes; resolving the pinned ref is the
// caller's job.
//
// Semantic validation of the promotion graph lives in internal/engine.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"go.yaml.in/yaml/v3"

	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

// DefaultPath is the default repository-local configuration path.
const DefaultPath = ".oiax.yaml"

// Load reads and parses the configuration file at path.
func Load(path string) (*v1.PromotionGraph, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Parse decodes a single PromotionGraph document.
//
// Unknown fields are rejected — configuration is declarative data, not a
// scripting surface, and silent typos would change promotion behavior.
// Multi-document YAML is rejected: v1 accepts exactly one PromotionGraph
// per file (multiple graphs are reserved for a future version).
//
// The canonical apiVersion is oiax.skaphos.dev/v1; the pre-1.0
// oiax.skaphos.dev/v1alpha1 is accepted as a deprecated alias. Parse is a
// pure byte->struct decoder and emits no I/O: callers detect the alias with
// IsDeprecatedAPIVersion and warn.
func Parse(data []byte) (*v1.PromotionGraph, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var cfg v1.PromotionGraph
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("configuration is empty")
		}
		return nil, fmt.Errorf("parse configuration: %w", err)
	}

	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("configuration contains multiple YAML documents; v1 accepts exactly one PromotionGraph")
	}

	if cfg.APIVersion != v1.APIVersion && cfg.APIVersion != v1.APIVersionV1Alpha1 {
		return nil, fmt.Errorf("unsupported apiVersion %q (want %q)", cfg.APIVersion, v1.APIVersion)
	}
	if cfg.Kind != v1.KindPromotionGraph {
		return nil, fmt.Errorf("unsupported kind %q (want %q)", cfg.Kind, v1.KindPromotionGraph)
	}
	return &cfg, nil
}

// IsDeprecatedAPIVersion reports whether apiVersion is the deprecated
// pre-1.0 alias (oiax.skaphos.dev/v1alpha1) that Parse still accepts.
// Callers use it to emit a one-line migration warning; Parse itself stays a
// pure decoder and emits nothing.
func IsDeprecatedAPIVersion(apiVersion string) bool {
	return apiVersion == v1.APIVersionV1Alpha1
}
