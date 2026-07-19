package git_test

import (
	"context"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/git"
)

func TestResolveRev(t *testing.T) {
	t.Parallel()

	t.Run("object id passes through unchanged", func(t *testing.T) {
		t.Parallel()
		runner, dir := newRepo(t)
		head := writeCommit(t, dir, "app.txt", "v0\n", "c0")
		got, err := runner.ResolveRev(context.Background(), head)
		if err != nil {
			t.Fatal(err)
		}
		if got != head {
			t.Errorf("ResolveRev(%q) = %q, want the oid unchanged", head, got)
		}
	})

	t.Run("local head wins", func(t *testing.T) {
		t.Parallel()
		runner, dir := newRepo(t)
		head := writeCommit(t, dir, "app.txt", "v0\n", "c0")
		// Even with an origin-tracking ref present, the local head is the
		// authoritative live value.
		runGit(t, dir, "update-ref", "refs/remotes/origin/main", head)
		got, err := runner.ResolveRev(context.Background(), "main")
		if err != nil {
			t.Fatal(err)
		}
		if got != "refs/heads/main" {
			t.Errorf("ResolveRev(main) = %q, want refs/heads/main", got)
		}
	})

	t.Run("falls back to the origin-tracking ref", func(t *testing.T) {
		t.Parallel()
		runner, dir := newRepo(t)
		head := writeCommit(t, dir, "app.txt", "v0\n", "c0")
		// The actions/checkout shape: the branch exists only as a
		// remote-tracking ref, never as a local head.
		runGit(t, dir, "update-ref", "refs/remotes/origin/feature", head)
		got, err := runner.ResolveRev(context.Background(), "feature")
		if err != nil {
			t.Fatal(err)
		}
		if got != "refs/remotes/origin/feature" {
			t.Errorf("ResolveRev(feature) = %q, want refs/remotes/origin/feature", got)
		}
	})

	t.Run("unknown branch is an error", func(t *testing.T) {
		t.Parallel()
		runner, dir := newRepo(t)
		writeCommit(t, dir, "app.txt", "v0\n", "c0")
		_, err := runner.ResolveRev(context.Background(), "ghost")
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("ResolveRev(ghost) err = %v, want a not-found error", err)
		}
	})

	t.Run("invalid branch name is refused", func(t *testing.T) {
		t.Parallel()
		runner, dir := newRepo(t)
		writeCommit(t, dir, "app.txt", "v0\n", "c0")
		if _, err := runner.ResolveRev(context.Background(), "bad..name"); err == nil {
			t.Error("ResolveRev(bad..name) = nil error, want a ref-format refusal")
		}
	})
}

func TestBranchExists(t *testing.T) {
	t.Parallel()

	t.Run("local head exists", func(t *testing.T) {
		t.Parallel()
		runner, dir := newRepo(t)
		writeCommit(t, dir, "app.txt", "v0\n", "c0")
		ok, err := runner.BranchExists(context.Background(), "main")
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Error("BranchExists(main) = false, want true")
		}
	})

	t.Run("absent branch is a definitive false, not an error", func(t *testing.T) {
		t.Parallel()
		runner, dir := newRepo(t)
		writeCommit(t, dir, "app.txt", "v0\n", "c0")
		ok, err := runner.BranchExists(context.Background(), "ghost")
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Error("BranchExists(ghost) = true, want false")
		}
	})

	t.Run("origin-only branch does not count", func(t *testing.T) {
		t.Parallel()
		runner, dir := newRepo(t)
		head := writeCommit(t, dir, "app.txt", "v0\n", "c0")
		// BranchExists is deliberately local-only, unlike ResolveRev.
		runGit(t, dir, "update-ref", "refs/remotes/origin/feature", head)
		ok, err := runner.BranchExists(context.Background(), "feature")
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Error("BranchExists(feature) = true for an origin-only ref, want false")
		}
	})

	t.Run("invalid branch name is refused", func(t *testing.T) {
		t.Parallel()
		runner, dir := newRepo(t)
		writeCommit(t, dir, "app.txt", "v0\n", "c0")
		if _, err := runner.BranchExists(context.Background(), "bad..name"); err == nil {
			t.Error("BranchExists(bad..name) = nil error, want a ref-format refusal")
		}
	})
}

func TestResolveCommit(t *testing.T) {
	t.Parallel()

	t.Run("short oid resolves to the full commit", func(t *testing.T) {
		t.Parallel()
		runner, dir := newRepo(t)
		head := writeCommit(t, dir, "app.txt", "v0\n", "c0")
		got, err := runner.ResolveCommit(context.Background(), head[:8])
		if err != nil {
			t.Fatal(err)
		}
		if got != head {
			t.Errorf("ResolveCommit(%q) = %q, want %q", head[:8], got, head)
		}
	})

	t.Run("non-oid input is refused before git runs", func(t *testing.T) {
		t.Parallel()
		runner, dir := newRepo(t)
		writeCommit(t, dir, "app.txt", "v0\n", "c0")
		for _, oid := range []string{"main", "HEAD^{commit}", "abc", ""} {
			if _, err := runner.ResolveCommit(context.Background(), oid); err == nil {
				t.Errorf("ResolveCommit(%q) = nil error, want an invalid-oid refusal", oid)
			}
		}
	})

	t.Run("unknown oid is an error", func(t *testing.T) {
		t.Parallel()
		runner, dir := newRepo(t)
		writeCommit(t, dir, "app.txt", "v0\n", "c0")
		if _, err := runner.ResolveCommit(context.Background(), strings.Repeat("d", 40)); err == nil {
			t.Error("ResolveCommit of an absent oid = nil error, want an error")
		}
	})
}

func TestConflictErrorStrings(t *testing.T) {
	t.Parallel()
	cp := &git.CherryPickConflict{SHA: "abc1234", Subject: "fix: hotfix", Applied: 2}
	if got, want := cp.Error(), `cherry-pick conflict on abc1234 "fix: hotfix" after 2 applied cleanly`; got != want {
		t.Errorf("CherryPickConflict.Error() = %q, want %q", got, want)
	}
	mc := &git.MergeConflict{SHA: "abc1234", Subject: "fix: hotfix"}
	if got, want := mc.Error(), `merge conflict merging abc1234 "fix: hotfix"`; got != want {
		t.Errorf("MergeConflict.Error() = %q, want %q", got, want)
	}
}
