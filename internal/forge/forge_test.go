package forge_test

import (
	"testing"

	"github.com/skaphos/oiax/internal/forge"
)

func TestMergeMethodsAllows(t *testing.T) {
	t.Parallel()
	all := forge.MergeMethods{Merge: true, Squash: true, Rebase: true}
	none := forge.MergeMethods{}
	cases := []struct {
		name    string
		methods forge.MergeMethods
		method  string
		want    bool
	}{
		{"merge allowed", all, "merge", true},
		{"squash allowed", all, "squash", true},
		{"rebase allowed", all, "rebase", true},
		{"merge forbidden", none, "merge", false},
		{"squash forbidden", none, "squash", false},
		{"rebase forbidden", none, "rebase", false},
		{"only squash permits squash", forge.MergeMethods{Squash: true}, "squash", true},
		{"only squash forbids merge", forge.MergeMethods{Squash: true}, "merge", false},
		// No declared expectation must never warn, even when nothing is allowed.
		{"empty method is always allowed", none, "", true},
		{"unrecognized method is always allowed", none, "fast-forward", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.methods.Allows(tc.method); got != tc.want {
				t.Errorf("%+v.Allows(%q) = %v, want %v", tc.methods, tc.method, got, tc.want)
			}
		})
	}
}

func TestMergeCommitAllowed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		methods forge.MergeMethods
		want    bool
	}{
		{"repo allows merge commits", forge.MergeMethods{Merge: true}, true},
		{"repo forbids merge commits", forge.MergeMethods{}, false},
		// The fence this method backs (ADR-0006 Amendment 1): a linear-history
		// rule vetoes the repo-level merge button.
		{"linear history overrides repo setting", forge.MergeMethods{Merge: true, RequiresLinearHistory: true}, false},
		{"linear history with merge off", forge.MergeMethods{RequiresLinearHistory: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.methods.MergeCommitAllowed(); got != tc.want {
				t.Errorf("%+v.MergeCommitAllowed() = %v, want %v", tc.methods, got, tc.want)
			}
		})
	}
}
