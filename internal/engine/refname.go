package engine

import (
	"fmt"
	"strings"
)

// validateRefName checks a branch name against the subset of `git
// check-ref-format --branch` rules that apply to a bare branch name (no
// refs/heads/ prefix, no @{-N} shorthand resolution). It is a pure Go
// re-implementation, not a call into git, because the engine must never
// import internal/git: reconciling this once here means a malformed branch
// name in the config is reported at validate time, in the same round trip
// as every other configuration error, instead of surfacing as a raw git
// error deep inside a later plan or reconcile. The CLI's git layer enforces
// the identical rule set via the real git binary wherever a ref actually
// reaches a git subprocess, so the two checks agree by construction.
func validateRefName(name string) error {
	if name == "" {
		return fmt.Errorf("must not be empty")
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("must not begin with '-'")
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return fmt.Errorf("must not begin or end with '/'")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("must not contain '..'")
	}
	if strings.Contains(name, "@{") {
		return fmt.Errorf("must not contain '@{'")
	}
	if strings.HasSuffix(name, ".") {
		return fmt.Errorf("must not end with '.'")
	}
	for _, component := range strings.Split(name, "/") {
		if component == "" {
			return fmt.Errorf("must not contain consecutive '/'")
		}
		if strings.HasPrefix(component, ".") {
			return fmt.Errorf("no path component may begin with '.'")
		}
		if strings.HasSuffix(component, ".lock") {
			return fmt.Errorf("no path component may end with '.lock'")
		}
	}
	for _, r := range name {
		switch {
		case r < 0o40 || r == 0o177:
			return fmt.Errorf("must not contain control characters")
		case strings.ContainsRune(" ~^:?*[\\", r):
			return fmt.Errorf("must not contain %q", string(r))
		}
	}
	return nil
}
