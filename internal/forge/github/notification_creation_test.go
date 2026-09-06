package github

import (
	"net/http/httptest"
	"testing"

	"github.com/skaphos/oiax/v2/internal/forge"
	"github.com/skaphos/oiax/v2/internal/forge/forgetest"
)

func TestNotificationCreationConformance(t *testing.T) {
	t.Parallel()
	forgetest.RunNotificationCreation(t, "github", func(t *testing.T, s *forgetest.CreationScenario) forge.Forge {
		t.Helper()
		server := httptest.NewServer(s)
		t.Cleanup(server.Close)
		return &Provider{Owner: "example", Repo: "repo", BaseURL: server.URL, HTTP: server.Client()}
	})
}
