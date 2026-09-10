package git_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/skaphos/oiax/v2/internal/git"
	"github.com/skaphos/oiax/v2/internal/gittest"
)

// incompleteOrigin builds a repository with a commit that is reachable only
// from a second branch, so a checkout that never fetched that branch is missing
// an object origin still has — the exact shape `oiax notifications reset` must
// not mistake for a rewritten history.
func incompleteOrigin(t *testing.T) (dir, otherBranchOID string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "origin")
	gittest.InitRepo(t, dir)
	gittest.Run(t, dir, "commit", "-q", "--allow-empty", "-m", "base")
	gittest.Run(t, dir, "checkout", "-q", "-b", "other")
	gittest.Run(t, dir, "commit", "-q", "--allow-empty", "-m", "only on other")
	otherBranchOID = gittest.Run(t, dir, "rev-parse", "HEAD")
	gittest.Run(t, dir, "checkout", "-q", "main")
	gittest.Run(t, dir, "commit", "-q", "--allow-empty", "-m", "tip")
	return dir, otherBranchOID
}

// A local-path clone hardlinks the whole object database and silently ignores
// --depth, so it cannot reproduce transport-level scoping at all; file:// forces
// real pack negotiation. Getting this wrong makes every case below look
// complete.
func cloneOverTransport(t *testing.T, origin, name string, extra ...string) *git.Runner {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	args := append([]string{"clone", "-q"}, extra...)
	gittest.Run(t, "", append(args, "file://"+origin, dir)...)
	return &git.Runner{Dir: dir}
}

func TestIncompleteObjectDatabase(t *testing.T) {
	t.Parallel()
	origin, otherBranchOID := incompleteOrigin(t)
	for _, tc := range []struct {
		name string
		args []string
		want string
		// hasOtherBranchCommit records what the checkout can actually see. It is
		// asserted, not assumed: the reason a mode is (or is not) reported is
		// that a commit alive on origin is (or is not) missing locally.
		hasOtherBranchCommit bool
	}{
		{name: "full clone", want: "", hasOtherBranchCommit: true},
		// Reported today, and already reported before this guard existed.
		{name: "shallow", args: []string{"--depth", "1", "--branch", "main"}, want: git.DatabaseShallow},
		// The gap this guard closes: not shallow, but scoped.
		{name: "single branch", args: []string{"--single-branch", "--branch", "main"}, want: git.DatabaseSingleBranch},
		// Deliberately accepted: commit reachability is complete, and a filtered
		// object is lazily fetched, so no false positive is possible.
		{name: "partial blob filter", args: []string{"--filter=blob:none"}, want: "", hasOtherBranchCommit: true},
		{name: "partial tree filter", args: []string{"--filter=tree:0"}, want: "", hasOtherBranchCommit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := cloneOverTransport(t, origin, "checkout", tc.args...)
			got, err := r.IncompleteObjectDatabase(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("IncompleteObjectDatabase = %q, want %q", got, tc.want)
			}
			present, err := r.CommitExists(context.Background(), otherBranchOID)
			if err != nil {
				t.Fatal(err)
			}
			if present != tc.hasOtherBranchCommit {
				t.Fatalf("commit alive on origin present locally = %v, want %v", present, tc.hasOtherBranchCommit)
			}
			// The property that matters: a checkout is only allowed to call a
			// missing commit "gone" when nothing is known to be scoped away.
			if got == "" && !present {
				t.Fatal("an unreported object database is missing a commit origin still has; a reset here would record a false override")
			}
		})
	}
}

// Testing shallowness alone was not enough, and this is the case that proves
// it: actions/checkout's default is a single-branch fetch, which reports
// --is-shallow-repository=false while genuinely lacking other branches.
func TestIncompleteObjectDatabaseCatchesNonShallowScoping(t *testing.T) {
	t.Parallel()
	origin, otherBranchOID := incompleteOrigin(t)
	r := cloneOverTransport(t, origin, "single", "--single-branch", "--branch", "main")
	shallow, err := r.IsShallowRepository(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if shallow {
		t.Fatal("a single-branch clone reported itself shallow; this test no longer covers the gap")
	}
	present, err := r.CommitExists(context.Background(), otherBranchOID)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("a single-branch clone holds the other branch; this test no longer covers the gap")
	}
	reason, err := r.IncompleteObjectDatabase(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reason != git.DatabaseSingleBranch {
		t.Fatalf("reason = %q, want the single-branch reason", reason)
	}
}

// Widening the refspec and re-fetching is the documented way out, so it must
// actually clear the refusal.
func TestIncompleteObjectDatabaseClearsAfterFullFetch(t *testing.T) {
	t.Parallel()
	origin, otherBranchOID := incompleteOrigin(t)
	r := cloneOverTransport(t, origin, "single", "--single-branch", "--branch", "main")
	gittest.Run(t, r.Dir, "remote", "set-branches", "origin", "*")
	gittest.Run(t, r.Dir, "fetch", "-q", "--prune", "origin")
	reason, err := r.IncompleteObjectDatabase(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reason != "" {
		t.Fatalf("reason after widening the refspec = %q, want none", reason)
	}
	if present, err := r.CommitExists(context.Background(), otherBranchOID); err != nil || !present {
		t.Fatalf("commit still missing after a full fetch: %v, %v", present, err)
	}
}

// A repository with no origin has no remote to be incomplete against, and an
// unset key must read as "not configured" rather than as a git failure.
func TestIncompleteObjectDatabaseWithoutOrigin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gittest.InitRepo(t, dir)
	gittest.Run(t, dir, "commit", "-q", "--allow-empty", "-m", "base")
	reason, err := (&git.Runner{Dir: dir}).IncompleteObjectDatabase(context.Background())
	if err != nil {
		t.Fatalf("an unset remote.origin.fetch was read as an error: %v", err)
	}
	if reason != "" {
		t.Fatalf("reason = %q, want none", reason)
	}
}
