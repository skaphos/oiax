package azuredevops_test

import (
	"context"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/forge/azuredevops"
	"github.com/skaphos/oiax/internal/gittest"
)

func TestParseRemoteURL(t *testing.T) {
	cases := []struct {
		name   string
		remote string
		want   azuredevops.Repo
	}{
		{
			name:   "https",
			remote: "https://dev.azure.com/acme/platform/_git/deploy",
			want:   azuredevops.Repo{Organization: "acme", Project: "platform", Name: "deploy"},
		},
		{
			name:   "https with user",
			remote: "https://acme@dev.azure.com/acme/platform/_git/deploy",
			want:   azuredevops.Repo{Organization: "acme", Project: "platform", Name: "deploy"},
		},
		{
			name:   "ssh scp-like v3",
			remote: "git@ssh.dev.azure.com:v3/acme/platform/deploy",
			want:   azuredevops.Repo{Organization: "acme", Project: "platform", Name: "deploy"},
		},
		{
			name:   "ssh url v3",
			remote: "ssh://git@ssh.dev.azure.com/v3/acme/platform/deploy",
			want:   azuredevops.Repo{Organization: "acme", Project: "platform", Name: "deploy"},
		},
		{
			name:   "legacy visualstudio.com",
			remote: "https://acme.visualstudio.com/platform/_git/deploy",
			want:   azuredevops.Repo{Organization: "acme", Project: "platform", Name: "deploy"},
		},
		{
			name:   "legacy DefaultCollection",
			remote: "https://acme.visualstudio.com/DefaultCollection/platform/_git/deploy",
			want:   azuredevops.Repo{Organization: "acme", Project: "platform", Name: "deploy"},
		},
		{
			name:   "legacy vs-ssh",
			remote: "acme@vs-ssh.visualstudio.com:v3/acme/platform/deploy",
			want:   azuredevops.Repo{Organization: "acme", Project: "platform", Name: "deploy"},
		},
		{
			name:   "percent-encoded project",
			remote: "https://dev.azure.com/acme/My%20Project/_git/deploy",
			want:   azuredevops.Repo{Organization: "acme", Project: "My Project", Name: "deploy"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := azuredevops.ParseRemoteURL(tc.remote)
			if err != nil {
				t.Fatalf("ParseRemoteURL(%q) error: %v", tc.remote, err)
			}
			if got != tc.want {
				t.Errorf("ParseRemoteURL(%q) = %+v, want %+v", tc.remote, got, tc.want)
			}
		})
	}
}

func TestParseRemoteURLRejectsNonAzureRemotes(t *testing.T) {
	for _, remote := range []string{
		"https://github.com/acme/deploy.git",
		"git@github.com:acme/deploy.git",
		"https://dev.azure.com/acme/only-two-segments",
		"https://example.com/acme/platform/_git/deploy",
		"nonsense",
	} {
		if got, err := azuredevops.ParseRemoteURL(remote); err == nil {
			t.Errorf("ParseRemoteURL(%q) = %+v, want error", remote, got)
		}
	}
}

// TestParseRemoteURLNeverEchoesUserinfo proves an embedded credential (the
// common PAT-in-URL shape) cannot leak through a parse error.
func TestParseRemoteURLNeverEchoesUserinfo(t *testing.T) {
	const secret = "supersecretpat"
	for _, remote := range []string{
		"https://user:" + secret + "@dev.azure.com/acme/wrong-shape",
		"user:" + secret + "@dev.azure.com:acme/wrong/shape/extra",
	} {
		_, err := azuredevops.ParseRemoteURL(remote)
		if err == nil {
			t.Fatalf("ParseRemoteURL(%q) succeeded, want error", remote)
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error echoes the credential: %v", err)
		}
	}
}

func TestResolveRepoFromEnv(t *testing.T) {
	t.Setenv("BUILD_REPOSITORY_PROVIDER", "TfsGit")
	t.Setenv("SYSTEM_TEAMFOUNDATIONCOLLECTIONURI", "https://dev.azure.com/acme/")
	t.Setenv("SYSTEM_TEAMPROJECT", "platform")
	t.Setenv("BUILD_REPOSITORY_NAME", "deploy")

	got, err := azuredevops.ResolveRepo(context.Background())
	if err != nil {
		t.Fatalf("ResolveRepo: %v", err)
	}
	want := azuredevops.Repo{Organization: "acme", Project: "platform", Name: "deploy"}
	if got != want {
		t.Errorf("ResolveRepo = %+v, want %+v", got, want)
	}
}

func TestResolveRepoLegacyCollectionURI(t *testing.T) {
	t.Setenv("BUILD_REPOSITORY_PROVIDER", "TfsGit")
	t.Setenv("SYSTEM_TEAMFOUNDATIONCOLLECTIONURI", "https://acme.visualstudio.com/")
	t.Setenv("SYSTEM_TEAMPROJECT", "platform")
	t.Setenv("BUILD_REPOSITORY_NAME", "deploy")

	got, err := azuredevops.ResolveRepo(context.Background())
	if err != nil {
		t.Fatalf("ResolveRepo: %v", err)
	}
	if got.Organization != "acme" {
		t.Errorf("Organization = %q, want acme", got.Organization)
	}
}

// TestResolveRepoFallsBackToOriginRemote proves that outside an Azure
// Repos build (no TfsGit provider env) the identity comes from the
// origin remote URL.
func TestResolveRepoFallsBackToOriginRemote(t *testing.T) {
	t.Setenv("BUILD_REPOSITORY_PROVIDER", "")
	t.Setenv("SYSTEM_TEAMFOUNDATIONCOLLECTIONURI", "")
	t.Setenv("SYSTEM_TEAMPROJECT", "")
	t.Setenv("BUILD_REPOSITORY_NAME", "")

	dir := t.TempDir()
	t.Chdir(dir)
	gittest.InitRepo(t, dir)
	gittest.Run(t, dir, "remote", "add", "origin", "https://dev.azure.com/acme/platform/_git/deploy")

	got, err := azuredevops.ResolveRepo(context.Background())
	if err != nil {
		t.Fatalf("ResolveRepo: %v", err)
	}
	want := azuredevops.Repo{Organization: "acme", Project: "platform", Name: "deploy"}
	if got != want {
		t.Errorf("ResolveRepo = %+v, want %+v", got, want)
	}
}
