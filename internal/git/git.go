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
	"regexp"
	"strings"
)

// oidPattern matches a git object id (or an unambiguous abbreviation of
// one). Object ids that reach git as data — merge-base output, commit ids
// from patch-id — are guarded with this before use so a branch name can
// never masquerade as a revision or vice versa.
var oidPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// Runner executes git commands in one repository working directory.
type Runner struct {
	// Dir is the repository working directory. Empty means the current
	// directory.
	Dir string
}

// Commit is one observed commit. It is git-local; the coordination layer
// maps it to the engine's own commit type.
type Commit struct {
	SHA     string
	Subject string
}

// run executes git with args directly (no shell) and returns trimmed
// stdout.
func (r *Runner) run(ctx context.Context, args ...string) (string, error) {
	return r.runStdin(ctx, nil, args...)
}

// runStdin executes git with args directly (no shell), optionally feeding
// stdin, and returns trimmed stdout. It is the shared implementation
// behind run; pass a non-nil stdin to pipe data into the subcommand (as
// PatchIDs does for `git patch-id`).
func (r *Runner) runStdin(ctx context.Context, stdin []byte, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Dir
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// validRev resolves a caller-supplied revision to a form safe to hand git
// as data. A CheckRefFormat-valid branch name becomes refs/heads/<name>; a
// bare object id passes through unchanged; anything else is rejected. This
// lets range endpoints accept either a configured branch or an object id
// produced by a prior git call (e.g. a merge base).
func (r *Runner) validRev(ctx context.Context, s string) (string, error) {
	if oidPattern.MatchString(s) {
		return s, nil
	}
	if err := r.CheckRefFormat(ctx, s); err != nil {
		return "", fmt.Errorf("invalid rev %q: %w", s, err)
	}
	return "refs/heads/" + s, nil
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

// MergeBase returns the best common ancestor of two branches. It returns
// ("", nil) when the branches share no common ancestor (git exit code 1),
// mirroring BranchExists; any other failure is an error.
func (r *Runner) MergeBase(ctx context.Context, a, b string) (string, error) {
	if err := r.CheckRefFormat(ctx, a); err != nil {
		return "", err
	}
	if err := r.CheckRefFormat(ctx, b); err != nil {
		return "", err
	}
	out, err := r.run(ctx, "merge-base", "refs/heads/"+a, "refs/heads/"+b)
	if err == nil {
		return out, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return "", nil
	}
	return "", err
}

// UniqueCommits returns commits reachable from branch but not from base
// (git range base..branch), newest first, with subjects. An empty slice
// means the range is empty — the reachability rung's "in sync" signal.
func (r *Runner) UniqueCommits(ctx context.Context, base, branch string) ([]Commit, error) {
	if err := r.CheckRefFormat(ctx, base); err != nil {
		return nil, err
	}
	if err := r.CheckRefFormat(ctx, branch); err != nil {
		return nil, err
	}
	// %x1f is the ASCII unit separator: subjects contain spaces, so a
	// plain space would be an ambiguous field delimiter.
	rng := fmt.Sprintf("refs/heads/%s..refs/heads/%s", base, branch)
	out, err := r.run(ctx, "log", "--no-color", "--format=%H%x1f%s", rng)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	lines := strings.Split(out, "\n")
	commits := make([]Commit, 0, len(lines))
	for _, line := range lines {
		sha, subject, ok := strings.Cut(line, "\x1f")
		if !ok {
			return nil, fmt.Errorf("unexpected git log line %q", line)
		}
		commits = append(commits, Commit{SHA: sha, Subject: subject})
	}
	return commits, nil
}

// PatchIDs returns the stable patch-id of every non-merge commit in the
// range base..tip, keyed by commit SHA. Merge commits contribute no entry.
// tip must be a branch name; base may be a branch name or an object id
// (e.g. a merge base). The patch-id is content-based, so it survives a
// rebase that rewrites commit SHAs without changing the diff.
func (r *Runner) PatchIDs(ctx context.Context, base, tip string) (map[string]string, error) {
	if err := r.CheckRefFormat(ctx, tip); err != nil {
		return nil, err
	}
	baseRev, err := r.validRev(ctx, base)
	if err != nil {
		return nil, err
	}
	// Two steps, no shell pipe: capture the diff, then feed it to
	// `git patch-id` on stdin.
	rng := fmt.Sprintf("%s..refs/heads/%s", baseRev, tip)
	diff, err := r.run(ctx, "log", "-p", "--no-color", rng)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]string)
	if diff == "" {
		return ids, nil
	}
	out, err := r.runStdin(ctx, []byte(diff), "patch-id", "--stable")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return ids, nil
	}
	for _, line := range strings.Split(out, "\n") {
		// Each line is "<patch-id> <commit-id>".
		patchID, commitID, ok := strings.Cut(line, " ")
		if !ok {
			return nil, fmt.Errorf("unexpected patch-id line %q", line)
		}
		ids[commitID] = patchID
	}
	return ids, nil
}

// TreeSHA returns the tree object SHA of a branch head. Equal trees on two
// branches mean identical content even when the commits differ (the
// head-tree rung, which detects a squash at the moment of merge).
func (r *Runner) TreeSHA(ctx context.Context, branch string) (string, error) {
	if err := r.CheckRefFormat(ctx, branch); err != nil {
		return "", err
	}
	return r.run(ctx, "rev-parse", "--verify", "refs/heads/"+branch+"^{tree}")
}

// IsAncestor reports whether ancestor is reachable from descendant
// (git merge-base --is-ancestor). Both arguments are object ids from prior
// git output and are guarded as such. Exit 0 ⇒ true, exit 1 ⇒ false, any
// other failure is an error.
func (r *Runner) IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error) {
	if !oidPattern.MatchString(ancestor) {
		return false, fmt.Errorf("invalid ancestor oid %q", ancestor)
	}
	if !oidPattern.MatchString(descendant) {
		return false, fmt.Errorf("invalid descendant oid %q", descendant)
	}
	_, err := r.run(ctx, "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}
