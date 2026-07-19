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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// oidPattern matches a git object id (or an unambiguous abbreviation of
// one). Object ids that reach git as data — merge-base output, commit ids
// from patch-id — are guarded with this before use so a branch name can
// never masquerade as a revision or vice versa.
var oidPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// maxShowFileSize bounds the bytes ShowFile will buffer from `git show`, so
// a pathological blob committed at the pinned configuration ref cannot
// exhaust memory before config.Parse's own size check ever runs. This
// package must not import internal/config (engine/git never reaches
// upward), so the value is duplicated here rather than shared; keep it in
// sync with config.maxConfigSize.
const maxShowFileSize = 4 << 20 // 4 MiB, matching config.maxConfigSize

// Runner executes git commands in one repository working directory.
type Runner struct {
	// Dir is the repository working directory. Empty means the current
	// directory.
	Dir string
	// Env, when non-empty, is appended to the inherited process environment
	// for every git invocation. It exists so a cherry-pick can pin the
	// committer identity and date (making the replayed SHA a deterministic
	// function of its inputs); it is not safe for concurrent use, so set it
	// only on a Runner bound to an ephemeral worktree.
	Env []string
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
	if len(r.Env) > 0 {
		cmd.Env = append(os.Environ(), r.Env...)
	}
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

// capWriter is an io.Writer that fails once more than limit bytes have been
// written to it, so a subprocess's stdout can be rejected as it streams in
// rather than after it has already been buffered into memory in full.
type capWriter struct {
	limit int
	buf   bytes.Buffer
}

func (w *capWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.limit {
		return 0, fmt.Errorf("output exceeds %d byte limit", w.limit)
	}
	return w.buf.Write(p)
}

// runCapped executes git with args directly (no shell), like run, but caps
// stdout at maxBytes so a subcommand whose output size is determined by
// repository content (ShowFile's `git show <ref>:<path>`, unlike run's
// normally-small metadata output) cannot be fully buffered into memory
// before its size is known. Returned bytes are trimmed of surrounding
// whitespace, matching run/runStdin.
func (r *Runner) runCapped(ctx context.Context, maxBytes int, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Dir
	if len(r.Env) > 0 {
		cmd.Env = append(os.Environ(), r.Env...)
	}
	stdout := capWriter{limit: maxBytes}
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return bytes.TrimSpace(stdout.buf.Bytes()), nil
}

// validRev resolves a caller-supplied revision to a form safe to hand git
// as data. A bare object id passes through unchanged; otherwise the value is
// treated as a branch name and resolved (via resolveBranchRef) to its local
// head or origin-tracking ref. This lets range endpoints accept either a
// configured branch or an object id produced by a prior git call (e.g. a
// merge base).
func (r *Runner) validRev(ctx context.Context, s string) (string, error) {
	if oidPattern.MatchString(s) {
		return s, nil
	}
	return r.resolveBranchRef(ctx, s)
}

// ResolveRev exposes validRev to callers outside this package that must build
// their own rev-ranges. The reconcile layer shells out to git directly for the
// commit-body and trailer reads the Runner does not expose (see
// commitBodies/backflowSkipTrailers); it uses this to resolve each range
// endpoint to the ref that actually holds it — the local head or, under
// actions/checkout where non-triggering branches are only remote-tracking, the
// origin-tracking ref — instead of hard-coding refs/heads/<name> and failing on
// every branch but the one that triggered the workflow. A bare object id passes
// through unchanged; a branch name still passes CheckRefFormat and resolves
// only to refs/heads/<name> or refs/remotes/origin/<name>, preserving the
// no-shell and ref-format guarantees.
func (r *Runner) ResolveRev(ctx context.Context, s string) (string, error) {
	return r.validRev(ctx, s)
}

// resolveBranchRef validates a branch name and resolves it to the
// fully-qualified ref that actually holds it, preferring the local head
// (refs/heads/<name>) and falling back to the origin-tracking ref
// (refs/remotes/origin/<name>).
//
// It exists because under actions/checkout only the branch that triggered
// the workflow is materialized as a local head; every other branch in a
// promotion graph exists solely as a remote-tracking ref, and origin/HEAD is
// not set. The layer previously constructed 'refs/heads/'+name
// unconditionally, so Head/UniqueCommits/etc. failed on any non-triggering
// branch and a multi-branch reconcile exited 1 on its very first run.
//
// The local head wins when both refs exist: the triggering branch's
// checked-out state is the authoritative live value. Only
// refs/heads/<validated> or refs/remotes/origin/<validated> are ever
// constructed — the name still passes CheckRefFormat first — so the no-shell
// posture and ref-format guarantees are preserved. A name that resolves to
// neither ref is a genuine "not found"; any operational git failure (a
// show-ref exit other than 1) propagates as an error rather than being
// misread as absence.
func (r *Runner) resolveBranchRef(ctx context.Context, name string) (string, error) {
	if err := r.CheckRefFormat(ctx, name); err != nil {
		return "", err
	}
	for _, ref := range []string{"refs/heads/" + name, "refs/remotes/origin/" + name} {
		_, err := r.run(ctx, "show-ref", "--verify", "--quiet", ref)
		if err == nil {
			return ref, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			continue
		}
		return "", err
	}
	return "", fmt.Errorf("branch %q not found as a local head or origin-tracking ref", name)
}

// minGitMajor and minGitMinor are the minimum system git version oiax
// requires: git 2.45 (2024). Backflow convergence replays commits with
// `git cherry-pick --empty=drop`, an option that only exists from git 2.45;
// older git (Ubuntu 22.04's 2.34, Debian bookworm's 2.39, and many GHES and
// self-hosted runners) rejects it with "unknown option `empty=drop`" and
// every backflow fails. The floor is asserted once at startup so an
// unsupported runner produces a clear up-front error, not a mid-reconcile
// surprise.
const (
	minGitMajor = 2
	minGitMinor = 45
)

// gitVersionPattern captures the leading major.minor pair of a `git version`
// string. It anchors only on the numeric prefix, so vendor suffixes —
// "2.39.5 (Apple Git-154)", "2.40.0.windows.1" — still parse to their real
// version.
var gitVersionPattern = regexp.MustCompile(`([0-9]+)\.([0-9]+)`)

// Version reports the raw `git version` line ("git version 2.45.1").
// RequireMinVersion parses it to assert the required floor; it is also useful
// for diagnostics.
func (r *Runner) Version(ctx context.Context) (string, error) {
	return r.run(ctx, "version")
}

// RequireMinVersion asserts the system git is at least the version oiax needs
// (see minGitMajor/minGitMinor) and returns a clear error naming both the
// required floor and the detected version otherwise. It is meant to run once
// at startup, before any git-dependent work, so an unsupported runner fails
// fast rather than deep inside a backflow cherry-pick.
func (r *Runner) RequireMinVersion(ctx context.Context) error {
	out, err := r.Version(ctx)
	if err != nil {
		return fmt.Errorf("determine git version: %w", err)
	}
	return checkMinVersion(out)
}

// checkMinVersion is the pure parse-and-compare behind RequireMinVersion,
// split out so the floor logic is testable without a system git of a
// particular version. out is the raw `git version` line.
func checkMinVersion(out string) error {
	major, minor, ok := parseGitVersion(out)
	if !ok {
		return fmt.Errorf("cannot parse git version from %q", out)
	}
	if major < minGitMajor || (major == minGitMajor && minor < minGitMinor) {
		return fmt.Errorf("git %d.%d or newer is required (backflow uses cherry-pick --empty=drop); detected %q",
			minGitMajor, minGitMinor, out)
	}
	return nil
}

// parseGitVersion extracts the major and minor components from a `git version`
// string ("git version 2.45.1"). It tolerates vendor suffixes by reading only
// the leading numeric major.minor pair. ok is false when no version-shaped
// number is present.
func parseGitVersion(out string) (major, minor int, ok bool) {
	m := gitVersionPattern.FindStringSubmatch(out)
	if m == nil {
		return 0, 0, false
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, false
	}
	minor, err = strconv.Atoi(m[2])
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// IsShallowRepository reports whether the working repository is a shallow
// clone — the state actions/checkout produces by default (fetch-depth: 1),
// where only a truncated slice of history is present. A shallow clone has no
// merge base for branches whose fork point predates the fetch depth, so the
// patch-identity and baseline rungs of the equivalence ladder silently switch
// off and already-promoted content looks unpromoted. The coordinator surfaces
// a warning on this signal rather than emit spurious promotion requests. It
// reads `git rev-parse --is-shallow-repository`, which prints "true"/"false".
func (r *Runner) IsShallowRepository(ctx context.Context) (bool, error) {
	out, err := r.run(ctx, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return false, err
	}
	return out == "true", nil
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

// BranchExists reports whether a local branch ref exists. It is deliberately
// local-only (unlike resolveBranchRef, which also considers origin-tracking
// refs); it has no callers today and its strictly-local semantics are kept
// for the case a caller needs exactly "is this a materialized local head".
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

// Head resolves a branch name to its commit SHA. The name is resolved to its
// local head or origin-tracking ref (resolveBranchRef), so a branch that
// actions/checkout left only as refs/remotes/origin/<name> still resolves.
func (r *Runner) Head(ctx context.Context, name string) (string, error) {
	ref, err := r.resolveBranchRef(ctx, name)
	if err != nil {
		return "", err
	}
	return r.run(ctx, "rev-parse", "--verify", ref)
}

// RemoteTrackingHead resolves a branch's origin-tracking ref
// (refs/remotes/origin/<name>) to its commit SHA, reporting ok=false when the
// tracking ref does not exist. Unlike resolveBranchRef it never falls back to a
// local head: it reports specifically the branch's last-fetched remote state.
// It backs the backflow push guard — before force-pushing the deterministic
// backflow branch, the coordinator compares the freshly replayed head against
// the branch's already-pushed head and skips an unchanged re-push, so a steady
// replay does not churn the open request. The name is validated with
// CheckRefFormat and only refs/remotes/origin/<validated> is constructed,
// preserving the no-shell and ref-format guarantees. A resolution failure that
// is git's clean "no such ref" (exit 1) is reported as ok=false; any other
// failure propagates.
func (r *Runner) RemoteTrackingHead(ctx context.Context, name string) (string, bool, error) {
	if err := r.CheckRefFormat(ctx, name); err != nil {
		return "", false, err
	}
	out, err := r.run(ctx, "rev-parse", "--verify", "--quiet", "--end-of-options",
		"refs/remotes/origin/"+name)
	if err == nil {
		return out, true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return "", false, nil
	}
	return "", false, err
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
	out, err := r.runCapped(ctx, maxShowFileSize, "show", "--end-of-options", ref+":"+path)
	if err != nil {
		return nil, fmt.Errorf("read %s at ref %s: %w", path, ref, err)
	}
	return out, nil
}

// DefaultBranchRef resolves the repository's default branch to its
// remote-tracking ref (for example "origin/main"). It first reads
// origin/HEAD, the symbolic ref git records locally for the remote's default
// branch. When that is unset — a checkout that never ran `git remote
// set-head`, as under actions/checkout — it falls back to asking the remote
// directly with `git ls-remote --symref origin HEAD`, whose "ref:
// refs/heads/<name>\tHEAD" line names the default branch without depending on
// any local ref. The second return is false when both the local symref and the
// remote query fail (a remote-less repository), leaving the choice of fallback
// to the caller. It is also false when the remote query names a branch whose
// local tracking ref was never fetched: the resolved ref is only useful if
// `git show origin/<name>:<path>` can read it, and ls-remote confirms the
// remote's default without materializing refs/remotes/origin/<name> — so a
// single-branch checkout of a non-default branch would otherwise report a ref
// the sole consumer (config read) cannot open. A remote-tracking ref (not the
// local branch of the same name) is returned deliberately: it is the
// authoritative committed state of the default branch, independent of any
// stale local branch.
func (r *Runner) DefaultBranchRef(ctx context.Context) (string, bool) {
	if out, err := r.run(ctx, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && out != "" {
		// symbolic-ref does not guarantee the target tracking ref exists.
		if _, err := r.run(ctx, "show-ref", "--verify", "--quiet", "refs/remotes/"+out); err == nil {
			return out, true
		}
	}
	// origin/HEAD is not recorded locally; ask the remote which branch its
	// HEAD points at. The symref line looks like "ref: refs/heads/main\tHEAD".
	out, err := r.run(ctx, "ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(line, "ref:")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) != 2 || fields[1] != "HEAD" {
			continue
		}
		name, ok := strings.CutPrefix(fields[0], "refs/heads/")
		if !ok {
			continue
		}
		// ls-remote resolved the remote's default without touching local
		// refs. Report resolved only if the tracking ref was actually
		// fetched, so the returned ref is readable via `git show`.
		if _, err := r.run(ctx, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+name); err != nil {
			return "", false
		}
		return "origin/" + name, true
	}
	return "", false
}

// MergeBase returns the best common ancestor of two branches. It returns
// ("", nil) when the branches share no common ancestor (git exit code 1),
// mirroring BranchExists; any other failure is an error.
func (r *Runner) MergeBase(ctx context.Context, a, b string) (string, error) {
	aRef, err := r.resolveBranchRef(ctx, a)
	if err != nil {
		return "", err
	}
	bRef, err := r.resolveBranchRef(ctx, b)
	if err != nil {
		return "", err
	}
	out, err := r.run(ctx, "merge-base", aRef, bRef)
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
// (git range base..branch), newest first, with subjects. A nil slice
// means the range is empty — the reachability rung's "in sync" signal.
func (r *Runner) UniqueCommits(ctx context.Context, base, branch string) ([]Commit, error) {
	baseRef, err := r.resolveBranchRef(ctx, base)
	if err != nil {
		return nil, err
	}
	branchRef, err := r.resolveBranchRef(ctx, branch)
	if err != nil {
		return nil, err
	}
	// %x1f is the ASCII unit separator: subjects contain spaces, so a
	// plain space would be an ambiguous field delimiter.
	rng := fmt.Sprintf("%s..%s", baseRef, branchRef)
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
	tipRef, err := r.resolveBranchRef(ctx, tip)
	if err != nil {
		return nil, err
	}
	baseRev, err := r.validRev(ctx, base)
	if err != nil {
		return nil, err
	}
	// Two steps, no shell pipe: capture the diff, then feed it to
	// `git patch-id` on stdin.
	rng := fmt.Sprintf("%s..%s", baseRev, tipRef)
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

// MergeCommitSHAs returns the set of merge commits (two or more parents) in
// the range base..branch. The divergence-report content filter needs it to
// tell merge residue — which PatchIDs cannot fingerprint, a merge having no
// single diff — apart from empty non-merge commits, which carry no content
// at all.
func (r *Runner) MergeCommitSHAs(ctx context.Context, base, branch string) (map[string]struct{}, error) {
	baseRef, err := r.resolveBranchRef(ctx, base)
	if err != nil {
		return nil, err
	}
	branchRef, err := r.resolveBranchRef(ctx, branch)
	if err != nil {
		return nil, err
	}
	out, err := r.run(ctx, "rev-list", "--merges", fmt.Sprintf("%s..%s", baseRef, branchRef))
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{})
	if out == "" {
		return set, nil
	}
	for _, sha := range strings.Split(out, "\n") {
		set[sha] = struct{}{}
	}
	return set, nil
}

// MergeReproducible reports whether a merge commit is reproduced exactly by
// mechanically re-merging its two parents (`git merge-tree --write-tree`).
// True means the commit's tree is what the automatic merge produces, so the
// commit introduces no content of its own — benign residue of a merge-commit
// promotion process. False means the recorded tree is one the mechanical
// merge does not produce — an "evil merge" whose conflict-resolution or
// strategy-option edits are real content — or the re-merge conflicts, in
// which case the recorded resolution is necessarily hand-made. Octopus merges
// (three or more parents) and non-merge commits cannot be re-merged pairwise
// and report false, the conservative direction: unverifiable content stays
// visible as drift.
//
// sha must be an object id from prior git output (e.g. MergeCommitSHAs) and
// is guarded as such.
func (r *Runner) MergeReproducible(ctx context.Context, sha string) (bool, error) {
	if !oidPattern.MatchString(sha) {
		return false, fmt.Errorf("invalid merge commit oid %q", sha)
	}
	parents, err := r.run(ctx, "rev-list", "--parents", "-n", "1", sha)
	if err != nil {
		return false, err
	}
	// One line: "<commit> <parent>...". Exactly two parents or bust.
	fields := strings.Fields(parents)
	if len(fields) != 3 {
		return false, nil
	}
	actual, err := r.run(ctx, "rev-parse", "--verify", sha+"^{tree}")
	if err != nil {
		return false, err
	}
	// --allow-unrelated-histories mirrors whatever the original merge did: if
	// its parents share no ancestor the mechanical merge still runs instead of
	// erroring out, and the tree comparison decides.
	merged, err := r.run(ctx, "merge-tree", "--write-tree", "--allow-unrelated-histories", fields[1], fields[2])
	if err != nil {
		// Exit 1 is a content conflict: the mechanical merge does not apply
		// cleanly, so the recorded resolution is hand-made content. Any other
		// failure is operational and propagates.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	// On success the first output line is the merged tree oid.
	mergedTree, _, _ := strings.Cut(merged, "\n")
	return mergedTree == actual, nil
}

// TreeSHA returns the tree object SHA of a branch head. Equal trees on two
// branches mean identical content even when the commits differ (the
// head-tree rung, which detects a squash at the moment of merge).
func (r *Runner) TreeSHA(ctx context.Context, branch string) (string, error) {
	ref, err := r.resolveBranchRef(ctx, branch)
	if err != nil {
		return "", err
	}
	return r.run(ctx, "rev-parse", "--verify", ref+"^{tree}")
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

// shortSHALen is the fixed abbreviation length for backflow branch names.
// A bare `git rev-parse --short` chooses the minimum-unique length for the
// local object database (min 7, growing with object count and differing
// between shallow and full clones), so the same head could yield different
// branch names across environments and break the force-push idempotency the
// deterministic name exists to guarantee. A fixed length removes that
// dependence on local repository state. Twelve hex digits keep the collision
// probability negligible while remaining object-id-shaped.
const shortSHALen = 12

// ShortSHA resolves a branch head to a fixed-length abbreviated object id
// (git rev-parse --short=12). Backflow branch names embed the short SHA of
// the downstream source head so a given head deterministically yields one
// branch name (the concurrency strategy); the abbreviation length is fixed
// so that name depends only on the head, not on local object-database state.
// The branch name is validated through CheckRefFormat and passed as a
// fully-qualified ref.
func (r *Runner) ShortSHA(ctx context.Context, branch string) (string, error) {
	ref, err := r.resolveBranchRef(ctx, branch)
	if err != nil {
		return "", err
	}
	return r.run(ctx, "rev-parse", fmt.Sprintf("--short=%d", shortSHALen), ref)
}

// ResolveCommit resolves an object id (or an unambiguous abbreviation of one)
// to its full commit SHA. The input is guarded with oidPattern so it can only
// be a hex object id, never an option or a ref expression; it is used to turn
// the short SHA encoded in a backflow branch name back into a commit for an
// ancestry test. A resolution failure (unknown/ambiguous object) is returned
// as an error so callers can treat an unresolvable prior head conservatively.
func (r *Runner) ResolveCommit(ctx context.Context, oid string) (string, error) {
	if !oidPattern.MatchString(oid) {
		return "", fmt.Errorf("invalid oid %q", oid)
	}
	return r.run(ctx, "rev-parse", "--verify", "--end-of-options", oid+"^{commit}")
}

// CommitExists reports whether oid resolves to a commit object present in the
// local repository. It exists to distinguish a DEFINITIVE not-found — git's
// `rev-parse --verify --quiet` exit 1, meaning the object is simply not in the
// object database — from an operational failure (any other error), which
// propagates. A caller can then act on a genuinely absent commit (e.g. a
// backflow request whose encoded source head was history-rewritten out of
// existence) without mistaking a transient git failure for absence. oid is
// guarded with oidPattern, exactly as ResolveCommit guards it.
func (r *Runner) CommitExists(ctx context.Context, oid string) (bool, error) {
	if !oidPattern.MatchString(oid) {
		return false, fmt.Errorf("invalid oid %q", oid)
	}
	_, err := r.run(ctx, "rev-parse", "--verify", "--quiet", "--end-of-options", oid+"^{commit}")
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

// Worktree creates an ephemeral, detached working tree checked out at ref
// (git worktree add --detach). It exists so mutating operations — chiefly a
// cherry-pick sequence — never touch the caller's checked-out branch or
// index. It returns a Runner bound to the new tree and a cleanup func that
// removes the worktree registration and its directory; the caller MUST
// invoke cleanup on every exit path (defer it). ref accepts either a branch
// name (validated and fully qualified) or a bare object id, mirroring
// validRev, so the target head may be supplied as either.
func (r *Runner) Worktree(ctx context.Context, ref string) (*Runner, func(), error) {
	rev, err := r.validRev(ctx, ref)
	if err != nil {
		return nil, nil, err
	}
	// A parent temp dir holds a not-yet-existing child path: git worktree
	// add insists the target path not already exist, and MkdirTemp creates
	// the directory it returns.
	parent, err := os.MkdirTemp("", "oiax-worktree-")
	if err != nil {
		return nil, nil, fmt.Errorf("create worktree tempdir: %w", err)
	}
	dir := filepath.Join(parent, "wt")
	if _, err := r.run(ctx, "worktree", "add", "--detach", dir, rev); err != nil {
		_ = os.RemoveAll(parent)
		return nil, nil, fmt.Errorf("add worktree at %s: %w", ref, err)
	}
	cleanup := func() {
		// Use a fresh context: cleanup must run even when the caller's
		// context is already cancelled or timed out. The worktree metadata
		// lives in the parent repo, so run the removal there before deleting
		// the directory tree.
		_, _ = r.run(context.Background(), "worktree", "remove", "--force", dir)
		_ = os.RemoveAll(parent)
	}
	return &Runner{Dir: dir}, cleanup, nil
}

// CherryPickConflict reports that a cherry-pick sequence stopped on a commit
// whose content genuinely conflicted with the target (git exit code 1). It
// carries enough to surface a reported divergence without any git state: the
// failing commit, its subject, and how many earlier commits applied cleanly
// before it. The worktree has been reset with `git cherry-pick --abort` by the
// time this error is returned. Operational failures (a killed subprocess, a
// cancelled context, a structural refusal such as a merge commit) are NOT
// CherryPickConflicts — they propagate as ordinary errors so a transient
// problem is not misreported as a human-actionable content conflict.
type CherryPickConflict struct {
	// SHA is the object id of the commit that failed to apply.
	SHA string
	// Subject is the failing commit's subject line (git show -s --format=%s).
	Subject string
	// Applied is the number of commits that cherry-picked cleanly before the
	// failure (0 means the very first commit conflicted).
	Applied int
}

func (e *CherryPickConflict) Error() string {
	return fmt.Sprintf("cherry-pick conflict on %s %q after %d applied cleanly",
		e.SHA, e.Subject, e.Applied)
}

// CherryPick replays shas onto the Runner's checked-out HEAD one at a time
// with `git cherry-pick -x` (each replayed commit records a
// "(cherry picked from commit <sha>)" provenance trailer). shas must be in
// application order, oldest first. It is intended to run against a Runner
// bound to an ephemeral Worktree, never the caller's checkout.
//
// Each pick pins the committer identity and date to the ORIGINAL commit's, so
// the replayed HEAD is a deterministic function of its inputs: cherry-pick
// otherwise stamps the committer date to "now", giving a different HEAD SHA on
// every run and making the force-push that follows never a no-op (it would
// churn the managed branch and the open request's head on every reconcile).
//
// `--empty=drop` skips a pick that reduces to an empty diff because its change
// is already present on the target (a redundant return): that is convergence,
// not a conflict.
//
// On full success it returns the new HEAD object id. On the first commit whose
// content genuinely conflicts (git exit code 1) it runs `git cherry-pick
// --abort` (leaving the worktree clean) and returns a *CherryPickConflict
// naming that commit and the count of commits applied cleanly before it;
// nothing is pushed. Any other failure (exit code other than 1: a cancelled
// context, a killed subprocess, a structural refusal) propagates as an
// ordinary error rather than a *CherryPickConflict. Each sha is guarded with
// oidPattern before use.
func (r *Runner) CherryPick(ctx context.Context, shas []string) (string, error) {
	for i, sha := range shas {
		if !oidPattern.MatchString(sha) {
			return "", fmt.Errorf("invalid commit oid %q", sha)
		}
		// Pin the committer to the original commit's name, email and date so
		// the replayed SHA is reproducible across runs and environments.
		ci, err := r.run(ctx, "show", "-s", "--format=%cn%x1f%ce%x1f%cI", "--end-of-options", sha)
		if err != nil {
			return "", fmt.Errorf("read committer of %s: %w", sha, err)
		}
		name, rest, _ := strings.Cut(ci, "\x1f")
		email, date, _ := strings.Cut(rest, "\x1f")
		r.Env = []string{
			"GIT_COMMITTER_NAME=" + name,
			"GIT_COMMITTER_EMAIL=" + email,
			"GIT_COMMITTER_DATE=" + date,
		}
		_, err = r.run(ctx, "cherry-pick", "-x", "--empty=drop", sha)
		r.Env = nil
		if err == nil {
			continue
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			// Exit 1 is git's genuine-content-conflict signal. Capture the
			// subject before aborting (the commit object survives the abort).
			subject, _ := r.run(ctx, "show", "-s", "--format=%s", "--end-of-options", sha)
			_, _ = r.run(ctx, "cherry-pick", "--abort")
			return "", &CherryPickConflict{SHA: sha, Subject: subject, Applied: i}
		}
		// Any other exit code is an operational or structural failure, not a
		// content conflict. Best-effort abort with a fresh context (the caller's
		// may be cancelled), then surface the real error.
		_, _ = r.run(context.Background(), "cherry-pick", "--abort")
		return "", fmt.Errorf("cherry-pick %s: %w", sha, err)
	}
	return r.run(ctx, "rev-parse", "HEAD")
}

// MergeConflict reports that a `git merge --no-ff` of the backflow source head
// stopped because its content genuinely conflicts with the target (git exit
// code 1). It is the merge analog of CherryPickConflict: it carries enough to
// surface a reported divergence without any git state — the source head object
// id and its subject line. The worktree has been reset with `git merge --abort`
// by the time this error is returned, leaving it clean. Operational failures (a
// killed subprocess, a cancelled context, any structural refusal) are NOT
// MergeConflicts — they propagate as ordinary errors so a transient problem is
// not misreported as a human-actionable content conflict.
type MergeConflict struct {
	// SHA is the object id of the source head that failed to merge.
	SHA string
	// Subject is the source head's subject line (git show -s --format=%s).
	Subject string
}

func (e *MergeConflict) Error() string {
	return fmt.Sprintf("merge conflict merging %s %q", e.SHA, e.Subject)
}

// Merge performs a `git merge --no-ff` of sourceHead onto the Runner's
// checked-out HEAD, producing a two-parent merge commit. It is the
// execution primitive behind backflow.strategy: merge (ADR-0006) and is
// intended to run against a Runner bound to an ephemeral Worktree, never the
// caller's checkout.
//
// message, when non-empty, is the merge commit's message (`-m`), the
// templatable surface of SKA-54; empty keeps git's default merge message
// (`--no-edit`). The message is part of the commit object, so determinism
// requires the caller to pass a deterministic function of the merge inputs:
// a changed message (e.g. an edited template) changes the replayed HEAD SHA
// and re-pushes the managed branch once — bounded, self-healing churn.
//
// Both the committer AND the author identity+date are pinned to sourceHead's
// committer identity+date, so the resulting merge commit is a deterministic
// function of its inputs (ADR-0004 byte-identical determinism): git otherwise
// stamps the committer date — and, for a merge commit it creates, the author
// date — to "now", giving a different HEAD SHA on every run and making the
// force-push that follows never a no-op (it would churn the managed branch and
// the open request's head on every reconcile).
//
// On success it returns the new HEAD object id (the merge commit). On a genuine
// content conflict (git exit code 1) it runs `git merge --abort` (leaving the
// worktree clean) and returns a *MergeConflict naming the source head and its
// subject; nothing is pushed. Any other failure (exit code other than 1: a
// cancelled context, a killed subprocess, a structural refusal) is best-effort
// aborted and propagates as an ordinary error rather than a *MergeConflict.
// sourceHead is guarded with oidPattern before use.
func (r *Runner) Merge(ctx context.Context, sourceHead, message string) (string, error) {
	if !oidPattern.MatchString(sourceHead) {
		return "", fmt.Errorf("invalid commit oid %q", sourceHead)
	}
	// Pin both committer and author to the source head's committer name, email
	// and date so the merge commit is reproducible across runs and environments.
	ci, err := r.run(ctx, "show", "-s", "--format=%cn%x1f%ce%x1f%cI", "--end-of-options", sourceHead)
	if err != nil {
		return "", fmt.Errorf("read committer of %s: %w", sourceHead, err)
	}
	name, rest, _ := strings.Cut(ci, "\x1f")
	email, date, _ := strings.Cut(rest, "\x1f")
	r.Env = []string{
		"GIT_COMMITTER_NAME=" + name,
		"GIT_COMMITTER_EMAIL=" + email,
		"GIT_COMMITTER_DATE=" + date,
		"GIT_AUTHOR_NAME=" + name,
		"GIT_AUTHOR_EMAIL=" + email,
		"GIT_AUTHOR_DATE=" + date,
	}
	// The message reaches git as the argument value of -m (never a shell
	// string); an empty message keeps git's default via --no-edit.
	args := []string{"merge", "--no-ff", "--no-edit"}
	if message != "" {
		args = []string{"merge", "--no-ff", "-m", message}
	}
	_, err = r.run(ctx, append(args, "--end-of-options", sourceHead)...)
	r.Env = nil
	if err == nil {
		return r.run(ctx, "rev-parse", "HEAD")
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		// Exit 1 is git's genuine-content-conflict signal. Capture the subject
		// before aborting (the commit object survives the abort).
		subject, _ := r.run(ctx, "show", "-s", "--format=%s", "--end-of-options", sourceHead)
		_, _ = r.run(ctx, "merge", "--abort")
		return "", &MergeConflict{SHA: sourceHead, Subject: subject}
	}
	// Any other exit code is an operational or structural failure, not a
	// content conflict. Best-effort abort with a fresh context (the caller's
	// may be cancelled), then surface the real error.
	_, _ = r.run(context.Background(), "merge", "--abort")
	return "", fmt.Errorf("merge %s: %w", sourceHead, err)
}
