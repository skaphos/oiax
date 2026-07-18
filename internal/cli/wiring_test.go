package cli

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/skaphos/oiax/internal/forge/azuredevops"
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
	t.Setenv("BUILD_REPOSITORY_PROVIDER", "")

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
	t.Setenv("BUILD_REPOSITORY_PROVIDER", "")

	dir := t.TempDir()
	t.Chdir(dir)
	gittest.InitRepo(t, dir)

	if _, _, err := resolveRepo(context.Background()); err == nil {
		t.Fatal("resolveRepo() = nil error, want a failure with no origin remote configured")
	}
}

// TestResolveRepoFromAzureGitHubBuild proves an Azure Pipelines build of a
// GitHub-hosted repository resolves owner/repo from BUILD_REPOSITORY_NAME
// — gated on BUILD_REPOSITORY_PROVIDER=GitHub, so an Azure Repos build
// (TfsGit, bare repository name) never misparses, and a malformed value
// falls through to the origin remote rather than erroring.
func TestResolveRepoFromAzureGitHubBuild(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("BUILD_REPOSITORY_PROVIDER", "GitHub")
	t.Setenv("BUILD_REPOSITORY_NAME", "skaphos/oiax")

	owner, repo, err := resolveRepo(context.Background())
	if err != nil {
		t.Fatalf("resolveRepo(): %v", err)
	}
	if owner != "skaphos" || repo != "oiax" {
		t.Errorf("resolveRepo() = (%q, %q), want (%q, %q)", owner, repo, "skaphos", "oiax")
	}

	// TfsGit publishes a bare repository name; it must not be consumed.
	t.Setenv("BUILD_REPOSITORY_PROVIDER", "TfsGit")
	t.Setenv("BUILD_REPOSITORY_NAME", "deploy")
	dir := t.TempDir()
	t.Chdir(dir)
	gittest.InitRepo(t, dir)
	gittest.Run(t, dir, "remote", "add", "origin", "https://github.com/from/remote.git")
	owner, repo, err = resolveRepo(context.Background())
	if err != nil {
		t.Fatalf("resolveRepo(): %v", err)
	}
	if owner != "from" || repo != "remote" {
		t.Errorf("resolveRepo() = (%q, %q), want the origin remote to win over a TfsGit repository name", owner, repo)
	}
}

// TestResolveForgeKind pins provider selection: the OIAX_FORGE override
// (including its rejection of unknown values), the environment signals
// from GitHub Actions and Azure Pipelines, remote-URL detection, and the
// github default.
func TestResolveForgeKind(t *testing.T) {
	clear := func(t *testing.T) {
		t.Setenv("OIAX_FORGE", "")
		t.Setenv("GITHUB_REPOSITORY", "")
		t.Setenv("BUILD_REPOSITORY_PROVIDER", "")
	}

	t.Run("explicit override", func(t *testing.T) {
		clear(t)
		for value, want := range map[string]forgeKind{
			"github":      forgeGitHub,
			"azuredevops": forgeAzureDevOps,
			"AzureDevOps": forgeAzureDevOps,
		} {
			t.Setenv("OIAX_FORGE", value)
			got, err := resolveForgeKind(context.Background())
			if err != nil {
				t.Fatalf("OIAX_FORGE=%s: %v", value, err)
			}
			if got != want {
				t.Errorf("OIAX_FORGE=%s → %s, want %s", value, got, want)
			}
		}
	})

	t.Run("invalid override", func(t *testing.T) {
		clear(t)
		t.Setenv("OIAX_FORGE", "gitlab")
		if _, err := resolveForgeKind(context.Background()); err == nil {
			t.Fatal("OIAX_FORGE=gitlab accepted, want an error")
		}
	})

	t.Run("github actions env", func(t *testing.T) {
		clear(t)
		t.Setenv("GITHUB_REPOSITORY", "skaphos/oiax")
		if got, _ := resolveForgeKind(context.Background()); got != forgeGitHub {
			t.Errorf("got %s, want github", got)
		}
	})

	t.Run("azure pipelines provider env", func(t *testing.T) {
		clear(t)
		t.Setenv("BUILD_REPOSITORY_PROVIDER", "TfsGit")
		if got, _ := resolveForgeKind(context.Background()); got != forgeAzureDevOps {
			t.Errorf("TfsGit → %s, want azuredevops", got)
		}
		t.Setenv("BUILD_REPOSITORY_PROVIDER", "GitHub")
		if got, _ := resolveForgeKind(context.Background()); got != forgeGitHub {
			t.Errorf("GitHub → %s, want github", got)
		}
	})

	t.Run("azure remote detection", func(t *testing.T) {
		clear(t)
		dir := t.TempDir()
		t.Chdir(dir)
		gittest.InitRepo(t, dir)
		gittest.Run(t, dir, "remote", "add", "origin", "https://dev.azure.com/acme/platform/_git/deploy")
		if got, _ := resolveForgeKind(context.Background()); got != forgeAzureDevOps {
			t.Errorf("dev.azure.com remote → %s, want azuredevops", got)
		}
	})

	t.Run("github default", func(t *testing.T) {
		clear(t)
		dir := t.TempDir() // no git repository at all: still github
		t.Chdir(dir)
		if got, err := resolveForgeKind(context.Background()); err != nil || got != forgeGitHub {
			t.Errorf("default = (%s, %v), want (github, nil)", got, err)
		}
	})
}

// TestNewForgeSelectsAzureDevOps proves that OIAX_FORGE=azuredevops now
// constructs the Azure DevOps provider — not the earlier not-implemented
// refusal, and not the GitHub provider — resolving the repository from the
// origin remote and wiring the token and work-item type from the environment.
// Construction touches no network (no API call is made until a command runs).
func TestNewForgeSelectsAzureDevOps(t *testing.T) {
	t.Setenv("OIAX_FORGE", "azuredevops")
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("BUILD_REPOSITORY_PROVIDER", "")
	t.Setenv("AZURE_DEVOPS_TOKEN", "secret-pat")
	t.Setenv("OIAX_ADO_WORKITEM_TYPE", "Bug")

	dir := t.TempDir()
	t.Chdir(dir)
	gittest.InitRepo(t, dir)
	gittest.Run(t, dir, "remote", "add", "origin", "https://dev.azure.com/acme/platform/_git/deploy")

	f, err := newForge(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("newForge(): %v", err)
	}
	p, ok := f.(*azuredevops.Provider)
	if !ok {
		t.Fatalf("newForge() = %T, want *azuredevops.Provider", f)
	}
	if p.Repo.Organization != "acme" || p.Repo.Project != "platform" || p.Repo.Name != "deploy" {
		t.Errorf("Repo = %+v, want acme/platform/deploy", p.Repo)
	}
	if p.Token != "secret-pat" {
		t.Error("Token not wired from AZURE_DEVOPS_TOKEN")
	}
	if p.WorkItemType != "Bug" {
		t.Errorf("WorkItemType = %q, want Bug", p.WorkItemType)
	}
}
