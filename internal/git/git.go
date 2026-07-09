// Package git shells out to the system git executable for ref state,
// reachability, patch identity, and backflow branch construction. Oiax
// does not implement Git object semantics itself — Git is already
// exceptionally good at being Git.
//
// Security invariant: branch and ref names are passed to git as data
// (after `--` separators where the subcommand accepts one, and validated
// with `git check-ref-format`), never interpolated into a shell.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Runner executes git commands in one repository working directory.
type Runner struct {
	// Dir is the repository working directory. Empty means the current
	// directory.
	Dir string
}

// run executes git with args directly (no shell) and returns trimmed
// stdout.
func (r *Runner) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Version reports the system git version, primarily for diagnostics.
func (r *Runner) Version(ctx context.Context) (string, error) {
	return r.run(ctx, "version")
}

// CheckRefFormat rejects names that are not well-formed branch names.
// Every configured branch name passes through here before being used in
// any other git invocation.
func (r *Runner) CheckRefFormat(ctx context.Context, name string) error {
	if _, err := r.run(ctx, "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("invalid branch name %q: %w", name, err)
	}
	return nil
}

// BranchExists reports whether a local branch ref exists.
func (r *Runner) BranchExists(ctx context.Context, name string) (bool, error) {
	if err := r.CheckRefFormat(ctx, name); err != nil {
		return false, err
	}
	_, err := r.run(ctx, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

// Head resolves a branch name to its commit SHA.
func (r *Runner) Head(ctx context.Context, name string) (string, error) {
	if err := r.CheckRefFormat(ctx, name); err != nil {
		return "", err
	}
	return r.run(ctx, "rev-parse", "--verify", "refs/heads/"+name)
}

// ShowFile returns the contents of path as committed at ref
// (`git show <ref>:<path>`). This is how the pinned-configuration-ref
// rule is implemented: configuration is read from one ref per
// invocation, never from the working tree of whatever triggered the
// run.
func (r *Runner) ShowFile(ctx context.Context, ref, path string) ([]byte, error) {
	// ref reaches git after --end-of-options, but reject option-shaped
	// and empty refs outright rather than trusting downstream parsing.
	if ref == "" || strings.HasPrefix(ref, "-") {
		return nil, fmt.Errorf("invalid ref %q", ref)
	}
	out, err := r.run(ctx, "show", "--end-of-options", ref+":"+path)
	if err != nil {
		return nil, fmt.Errorf("read %s at ref %s: %w", path, ref, err)
	}
	return []byte(out), nil
}
