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

// maxConfigSize bounds the bytes Load will read from a configuration file,
// so a pathological .oiax.yaml cannot exhaust memory. It is far larger than
// any real promotion graph needs.
const maxConfigSize = 4 << 20 // 4 MiB

// Load reads and parses the configuration file at path. Reading is capped at
// maxConfigSize; a file at or past the cap is rejected with a clear error
// rather than read into memory in full.
func Load(path string) (*v1.PromotionGraph, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, maxConfigSize+1))
	if err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}
	if len(data) > maxConfigSize {
		return nil, fmt.Errorf("read configuration: %s exceeds the %d byte limit", path, maxConfigSize)
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
// Parse rejects data longer than maxConfigSize before decoding. This is the
// authoritative size bound: Load's own pre-read cap only protects the
// working-tree path, while the pinned-ref read (loadGraph's configRef
// branch, via git.Runner.ShowFile) decodes bytes straight from `git show`
// and calls Parse directly, so the guard has to live here to cover both.
//
// The canonical apiVersion is oiax.skaphos.dev/v1; the pre-1.0
// oiax.skaphos.dev/v1alpha1 is accepted as a deprecated alias. Parse is a
// pure byte->struct decoder and emits no I/O: callers detect the alias with
// IsDeprecatedAPIVersion and warn.
func Parse(data []byte) (*v1.PromotionGraph, error) {
	if len(data) > maxConfigSize {
		return nil, fmt.Errorf("configuration exceeds the %d byte limit", maxConfigSize)
	}

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
