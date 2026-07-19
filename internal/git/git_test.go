package git_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/git"
	"github.com/skaphos/oiax/internal/gittest"
)

// requireGit skips the test when the system git executable is unavailable.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable not available")
	}
}

// runGit runs a git command in dir with the shared hermetic environment
// (see internal/gittest), failing the test on error, and returns trimmed
// stdout.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return gittest.Run(t, dir, args...)
}

// writeCommit writes content to file, stages and commits it, and returns
// the new HEAD SHA.
func writeCommit(t *testing.T, dir, file, content, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", file)
	runGit(t, dir, "commit", "-q", "-m", msg)
	return runGit(t, dir, "rev-parse", "HEAD")
}

// newRepo initializes an empty repository on branch main and returns a
// Runner bound to it plus the working directory. gittest.InitRepo pins the
// local config (identity, core.autocrlf, commit.gpgsign, core.hooksPath) so
// commits made through the production Runner -- which, unlike runGit, does
// not inject GIT_AUTHOR_*/GIT_COMMITTER_* -- still succeed regardless of the
// host's own git configuration. Worktrees share this common config.
func newRepo(t *testing.T) (*git.Runner, string) {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	gittest.InitRepo(t, dir)
	return &git.Runner{Dir: dir}, dir
}

func TestDefaultBranchRef(t *testing.T) {
	t.Parallel()

	t.Run("resolves from origin/HEAD", func(t *testing.T) {
		t.Parallel()
		runner, dir := newRepo(t)
		head := writeCommit(t, dir, "app.txt", "v0\n", "c0")
		// Fabricate the remote-tracking default branch locally (no network):
		// point origin/main at HEAD, then make origin/HEAD symref to it.
		runGit(t, dir, "update-ref", "refs/remotes/origin/main", head)
		runGit(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

		ref, ok := runner.DefaultBranchRef(context.Background())
		if !ok {
			t.Fatal("DefaultBranchRef reported not resolved, want origin/main")
		}
		if ref != "origin/main" {
			t.Errorf("ref = %q, want origin/main", ref)
		}
	})

	t.Run("not resolved without origin/HEAD", func(t *testing.T) {
		t.Parallel()
		runner, dir := newRepo(t)
		writeCommit(t, dir, "app.txt", "v0\n", "c0")

		if ref, ok := runner.DefaultBranchRef(context.Background()); ok {
			t.Errorf("DefaultBranchRef resolved %q, want not resolved", ref)
		}
	})

	t.Run("resolves via ls-remote fallback when origin/HEAD unset", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()

		// The "remote": a repository whose own HEAD points at its default
		// branch main (init -b main). A second local repo stands in for a
		// network remote, so the fallback is exercised without any network.
		_, remoteDir := newRepo(t)
		writeCommit(t, remoteDir, "app.txt", "v0\n", "c0")

		// The working repo: `origin` points at that remote and is fetched, but
		// origin/HEAD is deliberately unset — the actions/checkout condition
		// where the primary symbolic-ref path yields nothing.
		runner, dir := newRepo(t)
		writeCommit(t, dir, "app.txt", "v0\n", "c0")
		runGit(t, dir, "remote", "add", "origin", remoteDir)
		runGit(t, dir, "fetch", "-q", "origin")
		// `git fetch` may record origin/HEAD; delete it so only the remote
		// query (git ls-remote --symref) can resolve the default branch.
		del := exec.CommandContext(ctx, "git", "symbolic-ref", "-d", "refs/remotes/origin/HEAD")
		del.Dir = dir
		del.Env = gittest.Env()
		_ = del.Run()

		ref, ok := runner.DefaultBranchRef(ctx)
		if !ok {
			t.Fatal("DefaultBranchRef not resolved; want origin/main via ls-remote fallback")
		}
		if ref != "origin/main" {
			t.Errorf("DefaultBranchRef = %q, want origin/main", ref)
		}
	})

	t.Run("not resolved when only a non-default branch is fetched", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()

		// The "remote": default branch main plus a non-default branch feature.
		_, remoteDir := newRepo(t)
		writeCommit(t, remoteDir, "app.txt", "v0\n", "c0")
		runGit(t, remoteDir, "branch", "feature")

		// The working repo fetches ONLY the non-default branch — the
		// single-branch actions/checkout shape: refs/remotes/origin/feature
		// exists, but refs/remotes/origin/main and origin/HEAD do not.
		runner, dir := newRepo(t)
		writeCommit(t, dir, "app.txt", "v0\n", "c0")
		runGit(t, dir, "remote", "add", "origin", remoteDir)
		runGit(t, dir, "fetch", "-q", "origin", "+refs/heads/feature:refs/remotes/origin/feature")

		// ls-remote still names main as the remote default, but its tracking
		// ref was never materialized, so `git show origin/main:<path>` would
		// fail 128. DefaultBranchRef must report not-resolved rather than
		// hand its sole consumer (config read) an unreadable ref.
		if ref, ok := runner.DefaultBranchRef(ctx); ok {
			t.Fatalf("DefaultBranchRef resolved %q, want not resolved (origin/main tracking ref absent)", ref)
		}
	})
}

// TestHeadResolvesOriginTrackingRef reproduces the actions/checkout condition:
// a non-triggering branch exists only as refs/remotes/origin/<name>, its local
// head never materialized. Head (and the range functions) must still resolve
// it. Before the fix this failed with "refs/heads/feature not found" and a
// multi-branch reconcile exited 1 on its first run.
func TestHeadResolvesOriginTrackingRef(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, dir := newRepo(t)

	base := writeCommit(t, dir, "base.txt", "1\n", "base")
	runGit(t, dir, "branch", "feature")
	runGit(t, dir, "switch", "-q", "feature")
	feat := writeCommit(t, dir, "f.txt", "f\n", "feature work")
	runGit(t, dir, "switch", "-q", "main")

	// feature now exists ONLY as a remote-tracking ref; its local head is gone
	// (main stays the triggering local head). The commit object survives GC
	// because the remote-tracking ref keeps it reachable.
	runGit(t, dir, "update-ref", "refs/remotes/origin/feature", feat)
	runGit(t, dir, "branch", "-D", "feature")

	got, err := r.Head(ctx, "feature")
	if err != nil {
		t.Fatalf("Head(feature) with only an origin-tracking ref: %v", err)
	}
	if got != feat {
		t.Fatalf("Head(feature) = %q, want %q", got, feat)
	}

	// The range functions must resolve the origin-only branch on either side.
	ahead, err := r.UniqueCommits(ctx, "main", "feature")
	if err != nil {
		t.Fatalf("UniqueCommits(main, feature): %v", err)
	}
	if len(ahead) != 1 || ahead[0].SHA != feat {
		t.Fatalf("UniqueCommits(main, feature) = %+v, want the single commit %s", ahead, feat)
	}
	if mb, err := r.MergeBase(ctx, "main", "feature"); err != nil || mb != base {
		t.Fatalf("MergeBase(main, feature) = %q, %v; want %q", mb, err, base)
	}
}

// TestHeadPrefersLocalHeadOverOriginTracking asserts resolveBranchRef's
// precedence: when a branch exists both as a local head and an
// origin-tracking ref, the local head — the triggering branch's live state —
// wins.
func TestHeadPrefersLocalHeadOverOriginTracking(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, dir := newRepo(t)

	base := writeCommit(t, dir, "base.txt", "1\n", "base")
	runGit(t, dir, "branch", "feature")
	runGit(t, dir, "switch", "-q", "feature")
	localHead := writeCommit(t, dir, "local.txt", "local\n", "local head commit")
	runGit(t, dir, "switch", "-q", "main")

	// A divergent origin-tracking ref at an OLDER commit than the local head.
	runGit(t, dir, "update-ref", "refs/remotes/origin/feature", base)

	got, err := r.Head(ctx, "feature")
	if err != nil {
		t.Fatalf("Head(feature): %v", err)
	}
	if got != localHead {
		t.Fatalf("Head(feature) = %q, want the local head %q (local must win over origin-tracking)", got, localHead)
	}
}

// TestHeadBranchNotFound covers resolveBranchRef's terminal "not found" arm: a
// well-formed name (it passes CheckRefFormat) that resolves to neither a local
// head nor an origin-tracking ref — a .oiax.yaml branch typo, or a branch the
// ref-prep step did not fetch. It must surface the clean domain error, not a
// raw git failure, and is distinct from the invalid-name rejection every other
// negative test exercises.
func TestHeadBranchNotFound(t *testing.T) {
	t.Parallel()
	r, dir := newRepo(t)
	writeCommit(t, dir, "base.txt", "1\n", "base")

	_, err := r.Head(context.Background(), "ghost")
	if err == nil {
		t.Fatal("Head resolved a branch that exists as neither a local head nor an origin-tracking ref")
	}
	if !strings.Contains(err.Error(), "not found as a local head or origin-tracking ref") {
		t.Fatalf("Head error = %v, want the not-found domain error", err)
	}
}

// TestShowFile proves ShowFile reads a file's committed content at ref,
// not the working tree — the mechanism the pinned-configuration-ref rule
// (ADR-0003) relies on.
func TestShowFile(t *testing.T) {
	t.Parallel()
	r, dir := newRepo(t)
	writeCommit(t, dir, "config.yaml", "committed: true\n", "add config")

	// Diverge the working tree from what was committed.
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("committed: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := r.ShowFile(context.Background(), "main", "config.yaml")
	if err != nil {
		t.Fatalf("ShowFile: %v", err)
	}
	if got := string(out); got != "committed: true" {
		t.Errorf("ShowFile = %q, want the committed content, not the working tree", got)
	}
}

func TestMergeBase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T, dir string) (a, b, want string)
	}{
		{
			name: "common ancestor",
			setup: func(t *testing.T, dir string) (string, string, string) {
				base := writeCommit(t, dir, "base.txt", "1\n", "base")
				runGit(t, dir, "branch", "feature")
				writeCommit(t, dir, "main.txt", "m\n", "main work")
				runGit(t, dir, "switch", "-q", "feature")
				writeCommit(t, dir, "feat.txt", "f\n", "feature work")
				runGit(t, dir, "switch", "-q", "main")
				return "main", "feature", base
			},
		},
		{
			name: "no common ancestor",
			setup: func(t *testing.T, dir string) (string, string, string) {
				writeCommit(t, dir, "base.txt", "1\n", "base")
				runGit(t, dir, "checkout", "-q", "--orphan", "other")
				runGit(t, dir, "rm", "-rf", "-q", "--cached", ".")
				writeCommit(t, dir, "other.txt", "o\n", "orphan root")
				// No switch back to main: MergeBase resolves branch names
				// directly and base.txt is now an untracked working file
				// that a checkout of main would refuse to overwrite.
				return "main", "other", ""
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, dir := newRepo(t)
			a, b, want := tc.setup(t, dir)
			got, err := r.MergeBase(context.Background(), a, b)
			if err != nil {
				t.Fatalf("MergeBase: %v", err)
			}
			if got != want {
				t.Fatalf("MergeBase(%q,%q) = %q, want %q", a, b, got, want)
			}
		})
	}
}

func TestMergeBaseInvalidName(t *testing.T) {
	t.Parallel()
	r, dir := newRepo(t)
	writeCommit(t, dir, "base.txt", "1\n", "base")
	if _, err := r.MergeBase(context.Background(), "main", "bad..name"); err == nil {
		t.Fatal("MergeBase accepted an invalid branch name")
	}
}

func TestUniqueCommits(t *testing.T) {
	t.Parallel()
	r, dir := newRepo(t)

	writeCommit(t, dir, "base.txt", "1\n", "base")
	runGit(t, dir, "branch", "feature")
	runGit(t, dir, "switch", "-q", "feature")
	c1 := writeCommit(t, dir, "a.txt", "a\n", "first change here")
	c2 := writeCommit(t, dir, "b.txt", "b\n", "second change here")
	runGit(t, dir, "switch", "-q", "main")

	tests := []struct {
		name         string
		base, branch string
		want         []git.Commit
	}{
		{
			name:   "ahead by two newest first",
			base:   "main",
			branch: "feature",
			want: []git.Commit{
				{SHA: c2, Subject: "second change here"},
				{SHA: c1, Subject: "first change here"},
			},
		},
		{
			name:   "behind is empty",
			base:   "feature",
			branch: "main",
			want:   nil,
		},
		{
			name:   "identical is empty",
			base:   "main",
			branch: "main",
			want:   nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.UniqueCommits(context.Background(), tc.base, tc.branch)
			if err != nil {
				t.Fatalf("UniqueCommits: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("UniqueCommits(%q,%q) = %+v, want %+v", tc.base, tc.branch, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("commit[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestPatchIDsAcrossRebase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, dir := newRepo(t)

	writeCommit(t, dir, "base.txt", "base\n", "base")
	runGit(t, dir, "branch", "feature")
	runGit(t, dir, "switch", "-q", "feature")
	origSHA := writeCommit(t, dir, "feature.txt", "hello\n", "add feature")

	before, err := r.PatchIDs(ctx, "main", "feature")
	if err != nil {
		t.Fatalf("PatchIDs before rebase: %v", err)
	}
	if len(before) != 1 {
		t.Fatalf("PatchIDs before rebase = %v, want one entry", before)
	}
	pidBefore := before[origSHA]
	if pidBefore == "" {
		t.Fatalf("no patch-id for original commit %s in %v", origSHA, before)
	}

	// Advance main with an unrelated change, then rebase feature onto it:
	// the commit is rewritten (new SHA) but its diff is unchanged.
	runGit(t, dir, "switch", "-q", "main")
	writeCommit(t, dir, "other.txt", "other\n", "unrelated main work")
	runGit(t, dir, "rebase", "-q", "main", "feature")
	rebasedSHA := runGit(t, dir, "rev-parse", "feature")
	if rebasedSHA == origSHA {
		t.Fatal("rebase did not rewrite the commit SHA")
	}

	after, err := r.PatchIDs(ctx, "main", "feature")
	if err != nil {
		t.Fatalf("PatchIDs after rebase: %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("PatchIDs after rebase = %v, want one entry", after)
	}
	pidAfter := after[rebasedSHA]
	if pidAfter == "" {
		t.Fatalf("no patch-id for rebased commit %s in %v", rebasedSHA, after)
	}
	if pidBefore != pidAfter {
		t.Fatalf("patch-id changed across rebase: %q != %q", pidBefore, pidAfter)
	}

	// The base endpoint may also be a merge-base object id, not a branch.
	mb, err := r.MergeBase(ctx, "feature", "main")
	if err != nil {
		t.Fatalf("MergeBase: %v", err)
	}
	if !oidLike(mb) {
		t.Fatalf("MergeBase returned non-oid %q", mb)
	}
	byOID, err := r.PatchIDs(ctx, mb, "feature")
	if err != nil {
		t.Fatalf("PatchIDs with oid base: %v", err)
	}
	if byOID[rebasedSHA] != pidAfter {
		t.Fatalf("oid-base PatchIDs = %v, want patch-id %q for %s", byOID, pidAfter, rebasedSHA)
	}
}

func TestPatchIDsInvalidTip(t *testing.T) {
	t.Parallel()
	r, dir := newRepo(t)
	writeCommit(t, dir, "base.txt", "1\n", "base")
	if _, err := r.PatchIDs(context.Background(), "main", "bad..tip"); err == nil {
		t.Fatal("PatchIDs accepted an invalid tip name")
	}
}

func TestTreeSHAAcrossSquash(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, dir := newRepo(t)

	writeCommit(t, dir, "base.txt", "base\n", "base")
	runGit(t, dir, "branch", "feature")
	runGit(t, dir, "switch", "-q", "feature")
	writeCommit(t, dir, "work.txt", "line1\n", "f1")
	writeCommit(t, dir, "work.txt", "line1\nline2\n", "f2")
	featTree, err := r.TreeSHA(ctx, "feature")
	if err != nil {
		t.Fatalf("TreeSHA feature: %v", err)
	}

	// Before the squash the trees differ.
	mainTreeBefore, err := r.TreeSHA(ctx, "main")
	if err != nil {
		t.Fatalf("TreeSHA main: %v", err)
	}
	if mainTreeBefore == featTree {
		t.Fatal("trees matched before squash; test setup is wrong")
	}

	// Squash-merge feature into main: the commits differ but the resulting
	// tree is identical to feature's.
	runGit(t, dir, "switch", "-q", "main")
	runGit(t, dir, "merge", "-q", "--squash", "feature")
	runGit(t, dir, "commit", "-q", "-m", "squash feature")

	mainTree, err := r.TreeSHA(ctx, "main")
	if err != nil {
		t.Fatalf("TreeSHA main after squash: %v", err)
	}
	if mainTree != featTree {
		t.Fatalf("tree(main)=%q != tree(feature)=%q after squash", mainTree, featTree)
	}

	mainHead := runGit(t, dir, "rev-parse", "main")
	featHead := runGit(t, dir, "rev-parse", "feature")
	if mainHead == featHead {
		t.Fatal("expected distinct commits despite equal trees")
	}
}

func TestIsAncestor(t *testing.T) {
	t.Parallel()
	r, dir := newRepo(t)

	aSHA := writeCommit(t, dir, "a.txt", "a\n", "A")
	bSHA := writeCommit(t, dir, "b.txt", "b\n", "B")

	tests := []struct {
		name                 string
		ancestor, descendant string
		want                 bool
	}{
		{name: "ancestor precedes descendant", ancestor: aSHA, descendant: bSHA, want: true},
		{name: "descendant is not ancestor", ancestor: bSHA, descendant: aSHA, want: false},
		{name: "commit is its own ancestor", ancestor: aSHA, descendant: aSHA, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.IsAncestor(context.Background(), tc.ancestor, tc.descendant)
			if err != nil {
				t.Fatalf("IsAncestor: %v", err)
			}
			if got != tc.want {
				t.Fatalf("IsAncestor(%s,%s) = %v, want %v", tc.ancestor, tc.descendant, got, tc.want)
			}
		})
	}
}

func TestIsAncestorRejectsNonOID(t *testing.T) {
	t.Parallel()
	r, dir := newRepo(t)
	sha := writeCommit(t, dir, "a.txt", "a\n", "A")

	if _, err := r.IsAncestor(context.Background(), "main", sha); err == nil {
		t.Fatal("IsAncestor accepted a branch name as ancestor")
	}
	if _, err := r.IsAncestor(context.Background(), sha, "main"); err == nil {
		t.Fatal("IsAncestor accepted a branch name as descendant")
	}
}

func TestShortSHA(t *testing.T) {
	t.Parallel()
	r, dir := newRepo(t)
	full := writeCommit(t, dir, "a.txt", "a\n", "A")

	short, err := r.ShortSHA(context.Background(), "main")
	if err != nil {
		t.Fatalf("ShortSHA: %v", err)
	}
	// The abbreviation length is fixed (not git's minimum-unique length, which
	// depends on the local object database), so the backflow branch name it
	// feeds is deterministic across environments.
	if len(short) != 12 {
		t.Fatalf("ShortSHA = %q (len %d), want a fixed 12-char abbreviation", short, len(short))
	}
	if !strings.HasPrefix(full, short) {
		t.Fatalf("ShortSHA = %q is not a prefix of full sha %q", short, full)
	}
	if !oidLike(short) {
		t.Fatalf("ShortSHA = %q is not object-id shaped", short)
	}
}

func TestShortSHAInvalidName(t *testing.T) {
	t.Parallel()
	r, dir := newRepo(t)
	writeCommit(t, dir, "a.txt", "a\n", "A")
	if _, err := r.ShortSHA(context.Background(), "bad..name"); err == nil {
		t.Fatal("ShortSHA accepted an invalid branch name")
	}
}

func TestWorktreeCreateCleanup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, dir := newRepo(t)
	base := writeCommit(t, dir, "base.txt", "base\n", "base")

	wt, cleanup, err := r.Worktree(ctx, "main")
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}

	// The worktree directory exists and holds a detached checkout of main.
	if fi, err := os.Stat(wt.Dir); err != nil || !fi.IsDir() {
		t.Fatalf("worktree dir %q not present: %v", wt.Dir, err)
	}
	if got := runGit(t, wt.Dir, "rev-parse", "HEAD"); got != base {
		t.Fatalf("worktree HEAD = %q, want %q", got, base)
	}
	if _, err := os.Stat(filepath.Join(wt.Dir, "base.txt")); err != nil {
		t.Fatalf("worktree missing checked-out file: %v", err)
	}

	// The original checkout is untouched: still on main at the same commit.
	if got := runGit(t, dir, "rev-parse", "HEAD"); got != base {
		t.Fatalf("original HEAD moved to %q, want %q", got, base)
	}
	if got := runGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Fatalf("original branch = %q, want main", got)
	}

	cleanup()

	// The worktree directory is gone and no longer registered.
	if _, err := os.Stat(wt.Dir); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present after cleanup: %v", err)
	}
	if list := runGit(t, dir, "worktree", "list", "--porcelain"); strings.Contains(list, wt.Dir) {
		t.Fatalf("worktree still registered after cleanup:\n%s", list)
	}
}

func TestWorktreeInvalidRef(t *testing.T) {
	t.Parallel()
	r, dir := newRepo(t)
	writeCommit(t, dir, "base.txt", "base\n", "base")
	if _, _, err := r.Worktree(context.Background(), "bad..name"); err == nil {
		t.Fatal("Worktree accepted an invalid ref")
	}
}

func TestCherryPickHappyPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, dir := newRepo(t)

	// base is shared; target branches from it. main then gains two
	// downstream-only commits to be returned to target.
	base := writeCommit(t, dir, "base.txt", "base\n", "base")
	runGit(t, dir, "branch", "target")
	c1 := writeCommit(t, dir, "hotfix1.txt", "one\n", "first hotfix")
	c2 := writeCommit(t, dir, "hotfix2.txt", "two\n", "second hotfix")

	wt, cleanup, err := r.Worktree(ctx, "target")
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	defer cleanup()

	head, err := wt.CherryPick(ctx, []string{c1, c2})
	if err != nil {
		t.Fatalf("CherryPick: %v", err)
	}
	if head == base {
		t.Fatal("CherryPick returned the unchanged target head")
	}

	// Both commits' content replayed into the worktree.
	for _, f := range []struct{ name, want string }{
		{"hotfix1.txt", "one\n"},
		{"hotfix2.txt", "two\n"},
	} {
		got, err := os.ReadFile(filepath.Join(wt.Dir, f.name))
		if err != nil {
			t.Fatalf("read %s: %v", f.name, err)
		}
		if string(got) != f.want {
			t.Fatalf("%s = %q, want %q", f.name, got, f.want)
		}
	}

	// The -x provenance trailer records both original commits.
	bodies := runGit(t, wt.Dir, "log", "--format=%B", base+"..HEAD")
	for _, orig := range []string{c1, c2} {
		want := "(cherry picked from commit " + orig
		if !strings.Contains(bodies, want) {
			t.Fatalf("cherry-pick log missing %q:\n%s", want, bodies)
		}
	}
}

func TestCherryPickConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, dir := newRepo(t)

	writeCommit(t, dir, "conflict.txt", "base\n", "base")
	runGit(t, dir, "branch", "target")

	// main: one commit that applies cleanly, then one that edits the same
	// line target will have changed independently.
	cClean := writeCommit(t, dir, "other.txt", "x\n", "clean commit")
	cConflict := writeCommit(t, dir, "conflict.txt", "main-change\n", "conflicting edit")

	// target diverges the same line so the second pick cannot apply.
	runGit(t, dir, "switch", "-q", "target")
	writeCommit(t, dir, "conflict.txt", "target-change\n", "target edit")
	runGit(t, dir, "switch", "-q", "main")

	wt, cleanup, err := r.Worktree(ctx, "target")
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	defer cleanup()

	_, err = wt.CherryPick(ctx, []string{cClean, cConflict})
	var conflict *git.CherryPickConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("CherryPick error = %v, want *CherryPickConflict", err)
	}
	if conflict.SHA != cConflict {
		t.Errorf("conflict SHA = %q, want %q", conflict.SHA, cConflict)
	}
	if conflict.Subject != "conflicting edit" {
		t.Errorf("conflict Subject = %q, want %q", conflict.Subject, "conflicting edit")
	}
	if conflict.Applied != 1 {
		t.Errorf("conflict Applied = %d, want 1", conflict.Applied)
	}

	// The worktree was aborted back to a clean state (no conflict markers,
	// no in-progress cherry-pick).
	if status := runGit(t, wt.Dir, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after abort:\n%s", status)
	}
}

// TestCherryPickCancelledContextIsOperationalError pins the exit-code
// discrimination that keeps a transient failure from being misreported as a
// human-actionable content conflict (SKA-602): a cancelled context is an
// OPERATIONAL failure, not exit code 1, so CherryPick must surface an ordinary
// error that wraps context.Canceled — never a *CherryPickConflict — even when
// the pick would otherwise conflict on content. Backflow maps only
// *CherryPickConflict to reported divergence (exit 3); an operational error
// must propagate so a retry, not a spurious conflict artifact, follows.
func TestCherryPickCancelledContextIsOperationalError(t *testing.T) {
	t.Parallel()
	r, dir := newRepo(t)

	// Arrange a genuine content conflict so that, absent the cancellation, this
	// would produce a *CherryPickConflict. The cancellation must win.
	writeCommit(t, dir, "conflict.txt", "base\n", "base")
	runGit(t, dir, "branch", "target")
	src := writeCommit(t, dir, "conflict.txt", "main-change\n", "conflicting edit")
	runGit(t, dir, "switch", "-q", "target")
	writeCommit(t, dir, "conflict.txt", "target-change\n", "target edit")
	runGit(t, dir, "switch", "-q", "main")

	wt, cleanup, err := r.Worktree(context.Background(), "target")
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the first git invocation

	_, err = wt.CherryPick(ctx, []string{src})
	if err == nil {
		t.Fatal("CherryPick on a cancelled context returned nil, want an operational error")
	}
	var conflict *git.CherryPickConflict
	if errors.As(err, &conflict) {
		t.Fatalf("cancelled context misreported as a content conflict: %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want it to wrap context.Canceled", err)
	}
}

func TestCherryPickIsDeterministic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, dir := newRepo(t)

	base := writeCommit(t, dir, "base.txt", "base\n", "base")
	runGit(t, dir, "branch", "target")
	c1 := writeCommit(t, dir, "hotfix.txt", "one\n", "hotfix")

	// Replaying the same commit onto the same target head in two independent
	// worktrees must yield the same HEAD SHA: the committer identity and date
	// are pinned to the original commit, so the replay does not depend on the
	// wall clock. Without the pin cherry-pick stamps committer=now and the two
	// runs would differ, so the following force-push would never be a no-op.
	pick := func() string {
		wt, cleanup, err := r.Worktree(ctx, "target")
		if err != nil {
			t.Fatalf("Worktree: %v", err)
		}
		defer cleanup()
		head, err := wt.CherryPick(ctx, []string{c1})
		if err != nil {
			t.Fatalf("CherryPick: %v", err)
		}
		return head
	}
	first := pick()
	second := pick()
	if first != second {
		t.Fatalf("cherry-pick not deterministic: %q vs %q", first, second)
	}
	if first == base {
		t.Fatal("cherry-pick returned the unchanged target head")
	}

	// The replayed commit's committer date equals the original's, not "now".
	wantDate := runGit(t, dir, "show", "-s", "--format=%cI", c1)
	gotDate := runGit(t, dir, "show", "-s", "--format=%cI", first)
	if gotDate != wantDate {
		t.Fatalf("replayed committer date = %q, want the original's %q", gotDate, wantDate)
	}
}

func TestCherryPickDropsRedundant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, dir := newRepo(t)

	writeCommit(t, dir, "f.txt", "a\n", "base")
	runGit(t, dir, "branch", "target")
	// main introduces the change a->b.
	c1 := writeCommit(t, dir, "f.txt", "b\n", "change on main")
	// target already reaches the same content by a different commit, so the
	// pick reduces to an empty diff. --empty=drop must skip it and converge,
	// never report it as a conflict.
	runGit(t, dir, "switch", "-q", "target")
	writeCommit(t, dir, "f.txt", "b\n", "same change on target")
	runGit(t, dir, "switch", "-q", "main")

	wt, cleanup, err := r.Worktree(ctx, "target")
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	defer cleanup()

	targetHead := runGit(t, dir, "rev-parse", "target")
	head, err := wt.CherryPick(ctx, []string{c1})
	if err != nil {
		t.Fatalf("CherryPick of a redundant commit must not error: %v", err)
	}
	// The redundant commit was dropped, so HEAD did not advance.
	if head != targetHead {
		t.Fatalf("HEAD = %q, want unchanged target head %q (redundant pick dropped)", head, targetHead)
	}
}

func TestMergeHappyPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, dir := newRepo(t)

	// base is shared; target branches from it. main (the backflow source) then
	// gains downstream-only commits to be returned to target by a merge commit.
	base := writeCommit(t, dir, "base.txt", "base\n", "base")
	runGit(t, dir, "branch", "target")
	writeCommit(t, dir, "hotfix1.txt", "one\n", "first hotfix")
	sourceHead := writeCommit(t, dir, "hotfix2.txt", "two\n", "second hotfix")

	wt, cleanup, err := r.Worktree(ctx, "target")
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	defer cleanup()

	head, err := wt.Merge(ctx, sourceHead, "")
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if head == base || head == sourceHead {
		t.Fatalf("Merge returned %q, want a fresh merge commit distinct from base %q and source %q", head, base, sourceHead)
	}

	// --no-ff produced a real two-parent merge commit whose parents are the
	// target head (base) and the source head.
	parents := strings.Fields(runGit(t, wt.Dir, "rev-list", "--parents", "-n", "1", head))
	if len(parents) != 3 {
		t.Fatalf("merge commit parents = %v, want two parents (three fields incl. self)", parents)
	}
	if parents[1] != base || parents[2] != sourceHead {
		t.Fatalf("merge parents = [%s %s], want [%s %s]", parents[1], parents[2], base, sourceHead)
	}

	// Both source commits' content is present after the merge.
	for _, f := range []struct{ name, want string }{
		{"hotfix1.txt", "one\n"},
		{"hotfix2.txt", "two\n"},
	} {
		got, err := os.ReadFile(filepath.Join(wt.Dir, f.name))
		if err != nil {
			t.Fatalf("read %s: %v", f.name, err)
		}
		if string(got) != f.want {
			t.Fatalf("%s = %q, want %q", f.name, got, f.want)
		}
	}

	// Identity is pinned to the source head's committer for byte-identical
	// determinism: BOTH the committer and the author of the merge commit carry
	// the source head's committer name, email and date, not "now".
	wantName := runGit(t, dir, "show", "-s", "--format=%cn", sourceHead)
	wantEmail := runGit(t, dir, "show", "-s", "--format=%ce", sourceHead)
	wantDate := runGit(t, dir, "show", "-s", "--format=%cI", sourceHead)
	for _, f := range []struct{ format, want, role string }{
		{"%cn", wantName, "committer name"},
		{"%ce", wantEmail, "committer email"},
		{"%cI", wantDate, "committer date"},
		{"%an", wantName, "author name"},
		{"%ae", wantEmail, "author email"},
		{"%aI", wantDate, "author date"},
	} {
		if got := runGit(t, wt.Dir, "show", "-s", "--format="+f.format, head); got != f.want {
			t.Errorf("merge %s = %q, want the source head's committer value %q", f.role, got, f.want)
		}
	}

	// Env was reset after the merge (not left pinned on the Runner).
	if wt.Env != nil {
		t.Errorf("Runner.Env = %v after Merge, want nil", wt.Env)
	}

	// Replaying the same merge onto the same target head in a second worktree
	// yields the same HEAD SHA — the whole point of pinning identity+date.
	wt2, cleanup2, err := r.Worktree(ctx, "target")
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	defer cleanup2()
	head2, err := wt2.Merge(ctx, sourceHead, "")
	if err != nil {
		t.Fatalf("Merge (second run): %v", err)
	}
	if head2 != head {
		t.Fatalf("merge not deterministic: %q vs %q", head, head2)
	}
}

func TestMergeConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, dir := newRepo(t)

	writeCommit(t, dir, "conflict.txt", "base\n", "base")
	runGit(t, dir, "branch", "target")

	// main (source) edits a line that target will have changed independently.
	sourceHead := writeCommit(t, dir, "conflict.txt", "main-change\n", "conflicting edit")

	// target diverges the same line so the merge cannot apply cleanly.
	runGit(t, dir, "switch", "-q", "target")
	writeCommit(t, dir, "conflict.txt", "target-change\n", "target edit")
	runGit(t, dir, "switch", "-q", "main")

	wt, cleanup, err := r.Worktree(ctx, "target")
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	defer cleanup()

	_, err = wt.Merge(ctx, sourceHead, "")
	var conflict *git.MergeConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("Merge error = %v, want *MergeConflict", err)
	}
	if conflict.SHA != sourceHead {
		t.Errorf("conflict SHA = %q, want %q", conflict.SHA, sourceHead)
	}
	if conflict.Subject != "conflicting edit" {
		t.Errorf("conflict Subject = %q, want %q", conflict.Subject, "conflicting edit")
	}

	// The worktree was aborted back to a clean state: no conflict markers or
	// staged changes, and no in-progress merge (MERGE_HEAD resolves to nothing,
	// which `rev-parse --verify` reports as exit 1 / empty output).
	if status := runGit(t, wt.Dir, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after abort:\n%s", status)
	}
	mergeHead := exec.Command("git", "rev-parse", "-q", "--verify", "MERGE_HEAD")
	mergeHead.Dir = wt.Dir
	mergeHead.Env = gittest.Env()
	if out, _ := mergeHead.Output(); strings.TrimSpace(string(out)) != "" {
		t.Fatalf("MERGE_HEAD still present after abort: %s", out)
	}

	// Env was reset even on the conflict path.
	if wt.Env != nil {
		t.Errorf("Runner.Env = %v after conflicting Merge, want nil", wt.Env)
	}
}

// TestMergeCancelledContextIsOperationalError is the merge-strategy analog of
// TestCherryPickCancelledContextIsOperationalError (SKA-602): a cancelled
// context must surface as an ordinary error wrapping context.Canceled, never a
// *MergeConflict, even against a source head whose content genuinely conflicts.
func TestMergeCancelledContextIsOperationalError(t *testing.T) {
	t.Parallel()
	r, dir := newRepo(t)

	writeCommit(t, dir, "conflict.txt", "base\n", "base")
	runGit(t, dir, "branch", "target")
	src := writeCommit(t, dir, "conflict.txt", "main-change\n", "conflicting edit")
	runGit(t, dir, "switch", "-q", "target")
	writeCommit(t, dir, "conflict.txt", "target-change\n", "target edit")
	runGit(t, dir, "switch", "-q", "main")

	wt, cleanup, err := r.Worktree(context.Background(), "target")
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = wt.Merge(ctx, src, "")
	if err == nil {
		t.Fatal("Merge on a cancelled context returned nil, want an operational error")
	}
	var conflict *git.MergeConflict
	if errors.As(err, &conflict) {
		t.Fatalf("cancelled context misreported as a content conflict: %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want it to wrap context.Canceled", err)
	}
}

func TestMergeRejectsNonOID(t *testing.T) {
	t.Parallel()
	r, dir := newRepo(t)
	writeCommit(t, dir, "a.txt", "a\n", "A")
	if _, err := r.Merge(context.Background(), "main", ""); err == nil {
		t.Fatal("Merge accepted a branch name as source head")
	}
}

// TestRequireMinVersion exercises the startup floor assertion end to end
// against the system git (Version + checkMinVersion). The test suite already
// requires a git executable, and the whole point of the floor is that oiax's
// supported runners meet it, so a passing check on the host git is the
// happy-path guarantee; the below-floor rejection is covered purely by
// checkMinVersion in version_internal_test.go, needing no old git binary.
func TestRequireMinVersion(t *testing.T) {
	t.Parallel()
	r, _ := newRepo(t)
	if err := r.RequireMinVersion(context.Background()); err != nil {
		t.Fatalf("RequireMinVersion on the system git: %v", err)
	}
}

// oidLike is a test-local check that a string looks like a git object id.
func oidLike(s string) bool {
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// TestIsShallowRepository covers shallow-clone detection: a full clone reports
// false, and a repository carrying the grafts file a depth-limited fetch writes
// reports true. It backs the coordinator's warning that equivalence detection
// is degraded under actions/checkout's default fetch-depth: 1.
func TestIsShallowRepository(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, dir := newRepo(t)
	head := writeCommit(t, dir, "a.txt", "a\n", "A")

	shallow, err := r.IsShallowRepository(ctx)
	if err != nil {
		t.Fatalf("IsShallowRepository: %v", err)
	}
	if shallow {
		t.Fatal("a full clone reported shallow")
	}

	// Mark the repo shallow exactly as a depth-limited fetch does: write the
	// grafts file git consults for --is-shallow-repository.
	if err := os.WriteFile(filepath.Join(dir, ".git", "shallow"), []byte(head+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shallow, err = r.IsShallowRepository(ctx)
	if err != nil {
		t.Fatalf("IsShallowRepository after graft: %v", err)
	}
	if !shallow {
		t.Fatal("a shallow clone reported full")
	}
}

// TestRemoteTrackingHead covers the backflow push guard's lookup: an absent
// origin-tracking ref reports ok=false with no error, a present one resolves to
// its SHA, and a malformed name is rejected before any ref lookup.
func TestRemoteTrackingHead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, dir := newRepo(t)
	sha := writeCommit(t, dir, "a.txt", "a\n", "A")

	const branch = "oiax/backflow/main-to-development/abc123def456"
	if _, ok, err := r.RemoteTrackingHead(ctx, branch); err != nil || ok {
		t.Fatalf("RemoteTrackingHead(absent) = ok %v, err %v; want ok=false, err=nil", ok, err)
	}

	runGit(t, dir, "update-ref", "refs/remotes/origin/"+branch, sha)
	got, ok, err := r.RemoteTrackingHead(ctx, branch)
	if err != nil || !ok {
		t.Fatalf("RemoteTrackingHead(present) = ok %v, err %v; want ok=true, err=nil", ok, err)
	}
	if got != sha {
		t.Fatalf("RemoteTrackingHead = %q, want %q", got, sha)
	}

	if _, _, err := r.RemoteTrackingHead(ctx, "bad..name"); err == nil {
		t.Fatal("RemoteTrackingHead accepted an invalid branch name")
	}
}

// TestCommitExists covers the orphan-detection primitive: a present object is
// true, a well-formed but absent object id is a DEFINITIVE not-found (false, no
// error — never mistaken for a transient failure), and a non-oid input is
// rejected outright.
func TestCommitExists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, dir := newRepo(t)
	sha := writeCommit(t, dir, "a.txt", "a\n", "A")

	if ok, err := r.CommitExists(ctx, sha); err != nil || !ok {
		t.Fatalf("CommitExists(present) = %v, %v; want true, nil", ok, err)
	}
	if ok, err := r.CommitExists(ctx, "0123456789ab"); err != nil || ok {
		t.Fatalf("CommitExists(absent) = %v, %v; want false, nil", ok, err)
	}
	if _, err := r.CommitExists(ctx, "main"); err == nil {
		t.Fatal("CommitExists accepted a non-oid input")
	}
}

// TestMergeCommitSHAs covers the report filter's merge/empty discriminator:
// only merge commits in base..branch appear in the set.
func TestMergeCommitSHAs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, dir := newRepo(t)
	writeCommit(t, dir, "app.txt", "v0\n", "c0")
	runGit(t, dir, "branch", "dev")

	// main gains an ordinary commit, an empty commit, and a merge of dev.
	runGit(t, dir, "checkout", "-q", "dev")
	writeCommit(t, dir, "feature.txt", "f\n", "feature on dev")
	runGit(t, dir, "checkout", "-q", "main")
	writeCommit(t, dir, "app.txt", "v1\n", "c1 on main")
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", "empty on main")
	runGit(t, dir, "merge", "-q", "--no-ff", "-m", "merge dev into main", "dev")
	mergeSHA := runGit(t, dir, "rev-parse", "HEAD")

	set, err := r.MergeCommitSHAs(ctx, "dev", "main")
	if err != nil {
		t.Fatalf("MergeCommitSHAs: %v", err)
	}
	if len(set) != 1 {
		t.Fatalf("merge set = %v, want exactly the merge commit", set)
	}
	if _, ok := set[mergeSHA]; !ok {
		t.Fatalf("merge set %v does not contain merge commit %s", set, mergeSHA)
	}

	empty, err := r.MergeCommitSHAs(ctx, "main", "dev")
	if err != nil {
		t.Fatalf("MergeCommitSHAs(empty range): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("merge set for merge-free range = %v, want empty", empty)
	}
}

// TestMergeReproducible covers the evil-merge discriminator behind ADR-0002
// Amendment 1: a clean mechanical merge is benign residue (true); a tree the
// mechanical merge does not produce (-s ours), a conflicted re-merge (hand
// resolution), an octopus merge, and a non-merge commit all report false.
func TestMergeReproducible(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("benign merge is reproducible", func(t *testing.T) {
		t.Parallel()
		r, dir := newRepo(t)
		writeCommit(t, dir, "app.txt", "v0\n", "c0")
		runGit(t, dir, "branch", "dev")
		runGit(t, dir, "checkout", "-q", "dev")
		writeCommit(t, dir, "feature.txt", "f\n", "feature on dev")
		runGit(t, dir, "checkout", "-q", "main")
		writeCommit(t, dir, "other.txt", "o\n", "c1 on main")
		runGit(t, dir, "merge", "-q", "--no-ff", "-m", "merge dev", "dev")
		sha := runGit(t, dir, "rev-parse", "HEAD")

		ok, err := r.MergeReproducible(ctx, sha)
		if err != nil {
			t.Fatalf("MergeReproducible: %v", err)
		}
		if !ok {
			t.Fatal("clean --no-ff merge reported not reproducible, want benign")
		}
	})

	t.Run("strategy-option merge is not", func(t *testing.T) {
		t.Parallel()
		r, dir := newRepo(t)
		writeCommit(t, dir, "app.txt", "v0\n", "c0")
		runGit(t, dir, "branch", "dev")
		runGit(t, dir, "checkout", "-q", "dev")
		writeCommit(t, dir, "feature.txt", "f\n", "feature on dev")
		runGit(t, dir, "checkout", "-q", "main")
		// -s ours records a merge whose tree drops dev's content — a tree the
		// mechanical merge would never produce.
		runGit(t, dir, "merge", "-q", "-s", "ours", "--no-ff", "-m", "ours-merge dev", "dev")
		sha := runGit(t, dir, "rev-parse", "HEAD")

		ok, err := r.MergeReproducible(ctx, sha)
		if err != nil {
			t.Fatalf("MergeReproducible: %v", err)
		}
		if ok {
			t.Fatal("-s ours merge reported reproducible, want evil")
		}
	})

	t.Run("conflicted resolution is not", func(t *testing.T) {
		t.Parallel()
		r, dir := newRepo(t)
		writeCommit(t, dir, "app.txt", "v0\n", "c0")
		runGit(t, dir, "branch", "dev")
		runGit(t, dir, "checkout", "-q", "dev")
		writeCommit(t, dir, "app.txt", "dev\n", "edit on dev")
		runGit(t, dir, "checkout", "-q", "main")
		writeCommit(t, dir, "app.txt", "main\n", "edit on main")
		// The merge conflicts; the recorded resolution is hand-made content.
		mergeCmd := exec.CommandContext(ctx, "git", "merge", "--no-ff", "-m", "resolve", "dev")
		mergeCmd.Dir = dir
		mergeCmd.Env = gittest.Env()
		_ = mergeCmd.Run() // conflict expected
		writeCommit(t, dir, "app.txt", "resolved\n", "resolve conflict")
		sha := runGit(t, dir, "rev-parse", "HEAD")

		ok, err := r.MergeReproducible(ctx, sha)
		if err != nil {
			t.Fatalf("MergeReproducible: %v", err)
		}
		if ok {
			t.Fatal("conflict-resolution merge reported reproducible, want evil")
		}
	})

	t.Run("octopus and non-merge report false", func(t *testing.T) {
		t.Parallel()
		r, dir := newRepo(t)
		plain := writeCommit(t, dir, "app.txt", "v0\n", "c0")
		for _, b := range []string{"a", "b"} {
			runGit(t, dir, "checkout", "-q", "-b", b, "main")
			writeCommit(t, dir, b+".txt", b+"\n", "on "+b)
		}
		runGit(t, dir, "checkout", "-q", "main")
		runGit(t, dir, "merge", "-q", "--no-ff", "-m", "octopus", "a", "b")
		octopus := runGit(t, dir, "rev-parse", "HEAD")

		if ok, err := r.MergeReproducible(ctx, octopus); err != nil || ok {
			t.Fatalf("MergeReproducible(octopus) = %v, %v; want false, nil", ok, err)
		}
		if ok, err := r.MergeReproducible(ctx, plain); err != nil || ok {
			t.Fatalf("MergeReproducible(non-merge) = %v, %v; want false, nil", ok, err)
		}
		if _, err := r.MergeReproducible(ctx, "main"); err == nil {
			t.Fatal("MergeReproducible accepted a non-oid input")
		}
	})
}

// TestMergeWithMessage pins the templatable merge-commit message (SKA-54):
// a non-empty message becomes the merge commit's message verbatim, and the
// merge stays deterministic — a repeated run from the same inputs with the
// same message produces the identical SHA.
func TestMergeWithMessage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, dir := newRepo(t)

	writeCommit(t, dir, "base.txt", "base\n", "base")
	runGit(t, dir, "branch", "target")
	sourceHead := writeCommit(t, dir, "hotfix.txt", "one\n", "first hotfix")

	wt, cleanup, err := r.Worktree(ctx, "target")
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	defer cleanup()

	const msg = "backflow: return main to target\n\nGraph: environments"
	head, err := wt.Merge(ctx, sourceHead, msg)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if got := strings.TrimRight(runGit(t, dir, "log", "-1", "--format=%B", head), "\n"); got != msg {
		t.Errorf("merge commit message = %q, want %q", got, msg)
	}

	// Determinism: a second worktree over the same target re-merging the same
	// source with the same message reproduces the identical SHA.
	wt2, cleanup2, err := r.Worktree(ctx, "target")
	if err != nil {
		t.Fatalf("second Worktree: %v", err)
	}
	defer cleanup2()
	head2, err := wt2.Merge(ctx, sourceHead, msg)
	if err != nil {
		t.Fatalf("second Merge: %v", err)
	}
	if head2 != head {
		t.Errorf("repeated merge with message = %q, want deterministic %q", head2, head)
	}
}
