package cli

import (
	"context"
	"testing"

	"github.com/skaphos/oiax/internal/gittest"
)

// TestParseRemoteURL covers the remote URL variants resolveRepo's fallback
// path must handle: https with and without .git, the scp-like SSH form,
// ssh://, trailing slashes, and malformed input.
func TestParseRemoteURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		remote    string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "https with .git suffix",
			remote:    "https://github.com/skaphos/oiax.git",
			wantOwner: "skaphos",
			wantRepo:  "oiax",
		},
		{
			name:      "https without .git suffix",
			remote:    "https://github.com/skaphos/oiax",
			wantOwner: "skaphos",
			wantRepo:  "oiax",
		},
		{
			name:      "https with trailing slash",
			remote:    "https://github.com/skaphos/oiax/",
			wantOwner: "skaphos",
			wantRepo:  "oiax",
		},
		{
			name:      "scp-like ssh with .git suffix",
			remote:    "git@github.com:skaphos/oiax.git",
			wantOwner: "skaphos",
			wantRepo:  "oiax",
		},
		{
			name:      "scp-like ssh without .git suffix",
			remote:    "git@github.com:skaphos/oiax",
			wantOwner: "skaphos",
			wantRepo:  "oiax",
		},
		{
			name:      "ssh:// scheme",
			remote:    "ssh://git@github.com/skaphos/oiax.git",
			wantOwner: "skaphos",
			wantRepo:  "oiax",
		},
		{
			name:    "malformed: no path separator",
			remote:  "not-a-url",
			wantErr: true,
		},
		{
			name:    "malformed: owner only, no repo",
			remote:  "https://github.com/skaphos",
			wantErr: true,
		},
		{
			name:    "malformed: empty",
			remote:  "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			owner, repo, err := parseRemoteURL(tc.remote)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseRemoteURL(%q) = (%q, %q, nil), want an error", tc.remote, owner, repo)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRemoteURL(%q): %v", tc.remote, err)
			}
			if owner != tc.wantOwner || repo != tc.wantRepo {
				t.Errorf("parseRemoteURL(%q) = (%q, %q), want (%q, %q)", tc.remote, owner, repo, tc.wantOwner, tc.wantRepo)
			}
		})
	}
}

// TestResolveRepoFromEnv proves GITHUB_REPOSITORY wins over the origin
// remote when set, and that a malformed value (no "owner/repo" slash, or an
// empty owner/repo half) is rejected rather than silently misparsed.
func TestResolveRepoFromEnv(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{name: "well-formed", value: "skaphos/oiax", wantOwner: "skaphos", wantRepo: "oiax"},
		{name: "missing slash", value: "skaphos-oiax", wantErr: true},
		{name: "empty owner", value: "/oiax", wantErr: true},
		{name: "empty repo", value: "skaphos/", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GITHUB_REPOSITORY", tc.value)
			owner, repo, err := resolveRepo(context.Background())
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveRepo() = (%q, %q, nil), want an error", owner, repo)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveRepo(): %v", err)
			}
			if owner != tc.wantOwner || repo != tc.wantRepo {
				t.Errorf("resolveRepo() = (%q, %q), want (%q, %q)", owner, repo, tc.wantOwner, tc.wantRepo)
			}
		})
	}
}

// TestResolveRepoFallsBackToOriginRemote proves that with GITHUB_REPOSITORY
// unset, resolveRepo parses owner/repo from the origin remote URL of the
// current directory's repository.
func TestResolveRepoFallsBackToOriginRemote(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "")

	dir := t.TempDir()
	t.Chdir(dir)
	gittest.InitRepo(t, dir)
	gittest.Run(t, dir, "remote", "add", "origin", "https://github.com/skaphos/oiax.git")

	owner, repo, err := resolveRepo(context.Background())
	if err != nil {
		t.Fatalf("resolveRepo(): %v", err)
	}
	if owner != "skaphos" || repo != "oiax" {
		t.Errorf("resolveRepo() = (%q, %q), want (%q, %q)", owner, repo, "skaphos", "oiax")
	}
}

// TestResolveRepoNoOriginRemote proves resolveRepo surfaces a clear error,
// not a raw git failure, when neither GITHUB_REPOSITORY nor an origin remote
// is available.
func TestResolveRepoNoOriginRemote(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "")

	dir := t.TempDir()
	t.Chdir(dir)
	gittest.InitRepo(t, dir)

	if _, _, err := resolveRepo(context.Background()); err == nil {
		t.Fatal("resolveRepo() = nil error, want a failure with no origin remote configured")
	}
}
