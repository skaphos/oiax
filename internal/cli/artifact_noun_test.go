package cli

import (
	"testing"

	"github.com/skaphos/oiax/v2/internal/forge/azuredevops"
	"github.com/skaphos/oiax/v2/internal/forge/github"
)

// The coordinator names the conflict artifact by the wired provider: an issue
// on GitHub (under GitHub Actions or Azure Pipelines), a work item on Azure
// DevOps (spec 002 FR-012; T046).
func TestArtifactNounFollowsTheProvider(t *testing.T) {
	if got := artifactNoun(&github.Provider{}); got != "issue" {
		t.Errorf("GitHub provider noun = %q, want issue", got)
	}
	if got := artifactNoun(&azuredevops.Provider{}); got != "work item" {
		t.Errorf("Azure DevOps provider noun = %q, want work item", got)
	}
	if got := artifactNoun(nil); got != "" {
		t.Errorf("unknown forge noun = %q, want empty (the coordinator defaults to issue)", got)
	}
}
