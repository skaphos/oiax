package git_test

import (
	"context"
	"strings"
	"testing"
)

// TestCommonAncestors pins the semantics the backflow return scan bounds its
// walk with: the best common ancestors of ALL the given commits, so a walk of
// `tip ^bases...` reaches every commit that is not shared ancestry of every
// candidate.
func TestCommonAncestors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, dir := newRepo(t)

	base := writeCommit(t, dir, "base.txt", "1\n", "base")
	runGit(t, dir, "branch", "feature")
	m1 := writeCommit(t, dir, "m1.txt", "m\n", "main one")
	m2 := writeCommit(t, dir, "m2.txt", "m\n", "main two")
	runGit(t, dir, "switch", "-q", "feature")
	f1 := writeCommit(t, dir, "f1.txt", "f\n", "feature one")
	runGit(t, dir, "switch", "-q", "main")

	tests := []struct {
		name string
		shas []string
		want []string
	}{
		{name: "a single commit is its own base", shas: []string{m1}, want: []string{m1}},
		{name: "a chain resolves to its oldest commit", shas: []string{m2, m1}, want: []string{m1}},
		{name: "forked commits resolve to the fork point", shas: []string{m2, f1}, want: []string{base}},
		{name: "a chain plus a fork resolves to the fork point", shas: []string{m2, m1, f1}, want: []string{base}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.CommonAncestors(ctx, tc.shas...)
			if err != nil {
				t.Fatalf("CommonAncestors: %v", err)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("CommonAncestors(%v) = %v, want %v", tc.shas, got, tc.want)
			}
		})
	}

	t.Run("no common ancestor is nil, not an error", func(t *testing.T) {
		r, dir := newRepo(t)
		a := writeCommit(t, dir, "base.txt", "1\n", "base")
		runGit(t, dir, "checkout", "-q", "--orphan", "other")
		runGit(t, dir, "rm", "-rf", "-q", "--cached", ".")
		o := writeCommit(t, dir, "other.txt", "o\n", "orphan root")
		got, err := r.CommonAncestors(ctx, a, o)
		if err != nil {
			t.Fatalf("CommonAncestors: %v", err)
		}
		if got != nil {
			t.Fatalf("CommonAncestors = %v, want nil", got)
		}
	})
	t.Run("rejects a non-oid operand", func(t *testing.T) {
		if _, err := r.CommonAncestors(ctx, m1, "main"); err == nil {
			t.Fatal("CommonAncestors accepted a branch name")
		}
	})
	t.Run("rejects no commits", func(t *testing.T) {
		if _, err := r.CommonAncestors(ctx); err == nil {
			t.Fatal("CommonAncestors accepted an empty list")
		}
	})
}

// TestPatchIDsExcluding covers the property the backflow return scan relies
// on: a walk bounded by excluded revisions reaches a commit that sits AT the
// tip's merge base with another branch, which the base..tip form bounded at
// that merge base cannot.
func TestPatchIDsExcluding(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, dir := newRepo(t)

	writeCommit(t, dir, "base.txt", "1\n", "base")
	runGit(t, dir, "branch", "target")
	x := writeCommit(t, dir, "x.txt", "x\n", "hotfix on main")
	runGit(t, dir, "switch", "-q", "target")
	twin := writeCommit(t, dir, "x.txt", "x\n", "same diff returned to target")
	runGit(t, dir, "switch", "-q", "main")
	// The return promotes onto main: the twin is now the merge base.
	runGit(t, dir, "merge", "-q", "--no-ff", "-m", "promote target into main", "target")
	y := writeCommit(t, dir, "y.txt", "y\n", "later on main")

	mb, err := r.MergeBase(ctx, "main", "target")
	if err != nil {
		t.Fatalf("MergeBase: %v", err)
	}
	if mb != twin {
		t.Fatalf("merge-base = %s, want the twin %s", mb, twin)
	}
	// The range form bounded at the merge base is empty: it cannot see the twin.
	ranged, err := r.PatchIDs(ctx, mb, "target")
	if err != nil {
		t.Fatalf("PatchIDs: %v", err)
	}
	if len(ranged) != 0 {
		t.Fatalf("PatchIDs(mb..target) = %v, want empty", ranged)
	}

	xID, err := r.PatchIDs(ctx, "target", "main")
	if err != nil {
		t.Fatalf("PatchIDs: %v", err)
	}
	bases, err := r.CommonAncestors(ctx, x, y)
	if err != nil {
		t.Fatalf("CommonAncestors: %v", err)
	}
	got, err := r.PatchIDsExcluding(ctx, "target", bases...)
	if err != nil {
		t.Fatalf("PatchIDsExcluding: %v", err)
	}
	if got[twin] == "" || got[twin] != xID[x] {
		t.Fatalf("PatchIDsExcluding(target ^%v) = %v, want the twin with %s's patch-id %s", bases, got, x, xID[x])
	}

	// Several exclusions apply together: excluding the twin as well empties
	// the walk.
	none, err := r.PatchIDsExcluding(ctx, "target", x, twin)
	if err != nil {
		t.Fatalf("PatchIDsExcluding: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("PatchIDsExcluding(target ^x ^twin) = %v, want empty", none)
	}
	// No exclusion walks the whole history: base and twin.
	all, err := r.PatchIDsExcluding(ctx, "target")
	if err != nil {
		t.Fatalf("PatchIDsExcluding: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("PatchIDsExcluding(target) has %d entries, want 2: %v", len(all), all)
	}
	if _, err := r.PatchIDsExcluding(ctx, "target", "bad..name"); err == nil {
		t.Fatal("PatchIDsExcluding accepted an invalid excluded revision")
	}
}
