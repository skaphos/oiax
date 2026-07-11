package git_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/git"
)

// requireGit skips the test when the system git executable is unavailable.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable not available")
	}
}

// gitEnv returns a hermetic environment for the setup commands: global and
// system config are neutralized and a fixed identity is supplied so commits
// succeed regardless of the developer's own git configuration.
func gitEnv() []string {
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
	)
}

// runGit runs a git command in dir, failing the test on error, and returns
// trimmed stdout (stderr is surfaced only in the failure message).
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(stdout.String())
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
// Runner bound to it plus the working directory.
func newRepo(t *testing.T) (*git.Runner, string) {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
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
