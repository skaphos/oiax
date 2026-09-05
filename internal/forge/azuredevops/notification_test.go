package azuredevops

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/forge/forgetest"
	mk "github.com/skaphos/oiax/internal/forge/marker"
	"github.com/skaphos/oiax/internal/notification"
)

func TestNotificationLifecycleConformance(t *testing.T) {
	t.Parallel()
	forgetest.RunLifecycle(t, func(t *testing.T, seeds []forgetest.LifecycleSeed) forge.Forge {
		t.Helper()
		pulls := map[int]adoPull{}
		for _, seed := range seeds {
			body := "Human request"
			if seed.Managed {
				body = strings.Repeat("Full detail required. ", 50) + mk.Serialize(mk.Marker{Version: "v1", Graph: seed.Graph, Type: string(seed.Type), Source: seed.Source, Destination: seed.Destination, SourceHead: strings.Repeat("a", 40)})
			}
			status := "active"
			switch seed.State {
			case notification.LifecycleMerged:
				status = "completed"
			case notification.LifecycleClosed:
				status = "abandoned"
			}
			pr := adoPull{PullRequestID: seed.ID, Status: status, Description: body, CreationDate: seed.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), ClosedDate: seed.MergedAt.Format("2006-01-02T15:04:05Z07:00"), SourceRefName: "refs/heads/" + seed.Source, TargetRefName: "refs/heads/" + seed.Destination}
			if seed.Fork {
				pr.ForkSource = &forkRef{Name: "fork"}
			}
			pulls[seed.ID] = pr
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Error("lifecycle mutated remote")
				w.WriteHeader(405)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/_apis/connectionData") {
				_ = json.NewEncoder(w).Encode(map[string]string{"instanceId": "organization-id"})
				return
			}
			if r.URL.Path == "/project/_apis/git/repositories/repo" {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "repository-id", "name": "repo", "project": map[string]string{"id": "project-id"}})
				return
			}
			if strings.HasSuffix(r.URL.Path, "/properties") {
				_ = json.NewEncoder(w).Encode(propertiesCollection{})
				return
			}
			if strings.HasSuffix(r.URL.Path, "/pullrequests") {
				if r.URL.Query().Get("searchCriteria.status") != "all" {
					t.Error("notification discovery reused baseline status filter")
				}
				var list adoPullList
				for _, seed := range seeds {
					pr := pulls[seed.ID]
					if len(pr.Description) > 400 {
						pr.Description = pr.Description[:400]
					}
					list.Value = append(list.Value, pr)
				}
				list.Count = len(list.Value)
				_ = json.NewEncoder(w).Encode(list)
				return
			}
			_, tail, ok := strings.Cut(r.URL.Path, "/pullrequests/")
			id, _ := strconv.Atoi(tail)
			pr, found := pulls[id]
			if !ok || !found {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(pr)
		}))
		t.Cleanup(server.Close)
		return &Provider{Repo: Repo{Organization: "org", Project: "project", Name: "repo"}, BaseURL: server.URL, HTTP: server.Client()}
	})
}
