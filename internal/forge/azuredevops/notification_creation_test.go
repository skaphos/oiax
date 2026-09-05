package azuredevops

import (
	"net/http/httptest"
	"testing"

	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/forge/forgetest"
)

func TestNotificationCreationConformance(t *testing.T) {
	t.Parallel()
	forgetest.RunNotificationCreation(t, "azuredevops", func(t *testing.T, s *forgetest.CreationScenario) forge.Forge {
		t.Helper()
		server := httptest.NewServer(s)
		t.Cleanup(server.Close)
		return &Provider{Repo: Repo{Organization: "org", Project: "project", Name: "repo"}, BaseURL: server.URL, HTTP: server.Client()}
	})
}
