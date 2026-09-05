package azuredevops

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/forge/forgetest"
	mk "github.com/skaphos/oiax/internal/forge/marker"
	"github.com/skaphos/oiax/internal/notification"
)

func notificationAzurePull(id int, created time.Time) adoPull {
	body := mk.Serialize(mk.Marker{Version: "v1", Graph: "graph", Type: "promotion", Source: "dev", Destination: "test", SourceHead: strings.Repeat("a", 40)})
	return adoPull{PullRequestID: id, Status: "active", Description: body, CreationDate: created.Format(time.RFC3339Nano), SourceRefName: "refs/heads/dev", TargetRefName: "refs/heads/test"}
}

func serveNotificationAzureIdentity(w http.ResponseWriter, path string) bool {
	if strings.HasSuffix(path, "/_apis/connectionData") {
		_ = json.NewEncoder(w).Encode(map[string]string{"instanceId": "organization-id"})
		return true
	}
	if path == "/project/_apis/git/repositories/repo" {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "repository-id", "name": "repo", "project": map[string]string{"id": "project-id"}})
		return true
	}
	return false
}

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
				status := r.URL.Query().Get("searchCriteria.status")
				timeKind := r.URL.Query().Get("searchCriteria.queryTimeRangeType")
				if (status != "all" || timeKind != "created") && (status != "completed" || timeKind != "closed") {
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

func TestNotificationLifecyclePartitionsFrozenIntervals(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	through := from.Add(time.Hour)
	fullMin, fullMax := from.Format(time.RFC3339Nano), through.Format(time.RFC3339Nano)
	pull := notificationAzurePull(1, from.Add(time.Minute))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveNotificationAzureIdentity(w, r.URL.Path) {
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/pullrequests"):
			var listed []adoPull
			if r.URL.Query().Get("searchCriteria.minTime") == fullMin && r.URL.Query().Get("searchCriteria.maxTime") == fullMax {
				for id := 1; id <= 100; id++ {
					listed = append(listed, adoPull{PullRequestID: id})
				}
			} else {
				listed = []adoPull{{PullRequestID: 1}}
			}
			_ = json.NewEncoder(w).Encode(adoPullList{Value: listed, Count: len(listed)})
		case strings.HasSuffix(r.URL.Path, "/pullrequests/1/properties"):
			_ = json.NewEncoder(w).Encode(propertiesCollection{})
		case strings.HasSuffix(r.URL.Path, "/pullrequests/1"):
			_ = json.NewEncoder(w).Encode(pull)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	p := &Provider{Repo: Repo{Organization: "org", Project: "project", Name: "repo"}, BaseURL: server.URL, HTTP: server.Client()}
	query := forge.LifecycleQuery{Graph: "graph", Kind: "request-created", From: from, Through: through, Limit: 100}
	first, err := p.ListLifecyclePage(context.Background(), query)
	if err != nil || first.Progress.Complete || first.Progress.Cursor == "" || first.Pages != 1 || len(first.Requests) != 0 {
		t.Fatalf("partition start = (%+v, %d, %v)", first.Progress, len(first.Requests), err)
	}
	query.Cursor = first.Progress.Cursor
	second, err := p.ListLifecyclePage(context.Background(), query)
	if err != nil || second.Progress.Complete || second.Progress.Cursor == "" || second.Pages != 2 || len(second.Requests) != 1 {
		t.Fatalf("first partition = (%+v, %d, %v)", second.Progress, len(second.Requests), err)
	}
	query.Cursor = second.Progress.Cursor
	third, err := p.ListLifecyclePage(context.Background(), query)
	if err != nil || !third.Progress.Complete || third.Pages != 2 || len(third.Requests) != 0 {
		t.Fatalf("second partition = (%+v, %d, %v)", third.Progress, len(third.Requests), err)
	}
}

func TestNotificationLifecycleRejectsUnprovableIntervals(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		listed := make([]adoPull, 100)
		for i := range listed {
			listed[i].PullRequestID = i + 1
		}
		_ = json.NewEncoder(w).Encode(adoPullList{Value: listed, Count: len(listed)})
	}))
	t.Cleanup(server.Close)
	p := &Provider{Repo: Repo{Organization: "org", Project: "project", Name: "repo"}, BaseURL: server.URL, HTTP: server.Client()}
	page, err := p.ListLifecyclePage(context.Background(), forge.LifecycleQuery{Graph: "graph", Kind: "request-created", From: from, Through: from.Add(time.Millisecond), Limit: 100})
	if !errors.Is(err, notification.ErrDiscoveryIncomplete) || page.Progress.Complete || page.Progress.Cursor == "" || page.Pages != 1 {
		t.Fatalf("dense interval = (%+v, %v)", page.Progress, err)
	}
}

func TestNotificationLifecycleMovementAndDetailFailuresStayIncomplete(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	t.Run("movement", func(t *testing.T) {
		reads := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			reads++
			_ = json.NewEncoder(w).Encode(adoPullList{Value: []adoPull{{PullRequestID: reads}}, Count: 1})
		}))
		t.Cleanup(server.Close)
		p := &Provider{Repo: Repo{Organization: "org", Project: "project", Name: "repo"}, BaseURL: server.URL, HTTP: server.Client()}
		page, err := p.ListLifecyclePage(context.Background(), forge.LifecycleQuery{Graph: "graph", Kind: "request-created", Through: now, Limit: 100})
		if !errors.Is(err, notification.ErrDiscoveryIncomplete) || page.Progress.Complete || page.Progress.Cursor == "" || page.Pages != 2 {
			t.Fatalf("moving enumeration = (%+v, %v)", page.Progress, err)
		}
	})
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden} {
		status := status
		t.Run("detail-"+strconv.Itoa(status), func(t *testing.T) {
			pull := notificationAzurePull(1, now.Add(-time.Hour))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveNotificationAzureIdentity(w, r.URL.Path) {
					return
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "/pullrequests"):
					_ = json.NewEncoder(w).Encode(adoPullList{Value: []adoPull{{PullRequestID: 1}, {PullRequestID: 2}}, Count: 2})
				case strings.HasSuffix(r.URL.Path, "/pullrequests/1/properties"):
					_ = json.NewEncoder(w).Encode(propertiesCollection{})
				case strings.HasSuffix(r.URL.Path, "/pullrequests/1"):
					_ = json.NewEncoder(w).Encode(pull)
				case strings.HasSuffix(r.URL.Path, "/pullrequests/2"):
					w.WriteHeader(status)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(server.Close)
			p := &Provider{Repo: Repo{Organization: "org", Project: "project", Name: "repo"}, BaseURL: server.URL, HTTP: server.Client()}
			page, err := p.ListLifecyclePage(context.Background(), forge.LifecycleQuery{Graph: "graph", Kind: "request-created", Through: now, Limit: 100})
			if !errors.Is(err, notification.ErrDiscoveryIncomplete) || page.Progress.Complete || page.Progress.Cursor == "" || len(page.Requests) != 1 || page.Requests[0].Request.ID != "1" {
				t.Fatalf("detail failure = (%+v, %+v, %v)", page.Requests, page.Progress, err)
			}
		})
	}
}
