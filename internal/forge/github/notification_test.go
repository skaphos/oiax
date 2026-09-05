package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/forge/forgetest"
	"github.com/skaphos/oiax/internal/notification"
)

func TestNotificationLifecycleConformance(t *testing.T) {
	t.Parallel()
	forgetest.RunLifecycle(t, func(t *testing.T, seeds []forgetest.LifecycleSeed) forge.Forge {
		t.Helper()
		pulls := map[int]ghPull{}
		for _, seed := range seeds {
			body := "Human request"
			if seed.Managed {
				body = serializeMarker(marker{Version: "v1", Graph: seed.Graph, Type: string(seed.Type), Source: seed.Source, Destination: seed.Destination, SourceHead: strings.Repeat("a", 40)})
			}
			state := "closed"
			if seed.State == notification.LifecycleOpen {
				state = "open"
			}
			pr := ghPull{Number: seed.ID, State: state, Body: body, CreatedAt: seed.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), Head: ghRef{Ref: seed.Source, SHA: strings.Repeat("a", 40), Repo: ghRepo{FullName: "example/repo"}}, Base: ghRef{Ref: seed.Destination, SHA: strings.Repeat("b", 40), Repo: ghRepo{FullName: "example/repo"}}}
			if seed.State == notification.LifecycleMerged {
				merged := seed.MergedAt.Format("2006-01-02T15:04:05Z07:00")
				pr.MergedAt = &merged
			}
			if seed.Fork {
				pr.Head.Repo.FullName = "outsider/fork"
			}
			pulls[seed.ID] = pr
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Error("lifecycle mutated remote")
				w.WriteHeader(405)
				return
			}
			if r.URL.Path == "/repos/example/repo" {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": 123, "full_name": "example/repo", "html_url": "https://github.com/example/repo"})
				return
			}
			if r.URL.Path == "/repos/example/repo/pulls" {
				if r.URL.Query().Get("state") != "all" || r.URL.Query().Get("sort") != "created" || r.URL.Query().Get("direction") != "asc" {
					t.Error("notification discovery used baseline ordering/filter")
				}
				var page []ghPull
				for _, seed := range seeds {
					page = append(page, pulls[seed.ID])
				}
				_ = json.NewEncoder(w).Encode(page)
				return
			}
			id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/repos/example/repo/pulls/"))
			pr, ok := pulls[id]
			if !ok {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(pr)
		}))
		t.Cleanup(server.Close)
		return &Provider{Owner: "example", Repo: "repo", BaseURL: server.URL, HTTP: server.Client()}
	})
}
