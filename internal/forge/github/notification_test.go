package github

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
	"github.com/skaphos/oiax/internal/notification"
)

func notificationPull(id int, created time.Time) ghPull {
	body := serializeMarker(marker{Version: "v1", Graph: "graph", Type: "promotion", Source: "dev", Destination: "test", SourceHead: strings.Repeat("a", 40)})
	return ghPull{Number: id, State: "open", Body: body, CreatedAt: created.Format(time.RFC3339Nano), Head: ghRef{Ref: "dev", SHA: strings.Repeat("a", 40), Repo: ghRepo{FullName: "example/repo"}}, Base: ghRef{Ref: "test", SHA: strings.Repeat("b", 40), Repo: ghRepo{FullName: "example/repo"}}}
}

func serveNotificationIdentity(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{"id": 123, "full_name": "example/repo"})
}

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

func TestNotificationLifecyclePaginationOverlapsAndDeduplicatesMovement(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	pulls := make(map[int]ghPull, 101)
	for id := 1; id <= 101; id++ {
		pulls[id] = notificationPull(id, now.Add(-400*24*time.Hour).Add(time.Duration(id)*time.Second))
	}
	pageOneReads := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/example/repo":
			serveNotificationIdentity(w)
		case r.URL.Path == "/repos/example/repo/pulls":
			if r.URL.Query().Get("state") != "all" || r.URL.Query().Get("sort") != "created" || r.URL.Query().Get("direction") != "asc" || r.URL.Query().Get("per_page") != "100" {
				t.Errorf("unexpected lifecycle query: %s", r.URL.RawQuery)
			}
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			var listed []ghPull
			switch page {
			case 1:
				pageOneReads++
				first := 1
				if pageOneReads > 1 {
					first = 2 // page boundary moved between continuations
				}
				for id := first; id < first+100; id++ {
					listed = append(listed, ghPull{Number: id})
				}
				w.Header().Set("Link", "<"+server.URL+"/repos/example/repo/pulls?page=2>; rel=\"next\"")
			case 2:
				listed = []ghPull{{Number: 101}} // overlaps moved page one
			default:
				t.Errorf("unexpected page %d", page)
			}
			_ = json.NewEncoder(w).Encode(listed)
		case strings.HasPrefix(r.URL.Path, "/repos/example/repo/pulls/"):
			id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/repos/example/repo/pulls/"))
			_ = json.NewEncoder(w).Encode(pulls[id])
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	p := &Provider{Owner: "example", Repo: "repo", BaseURL: server.URL, HTTP: server.Client()}
	query := forge.LifecycleQuery{Graph: "graph", Kind: "request-created", From: now.Add(-500 * 24 * time.Hour), Through: now, Limit: 100}
	first, err := p.ListLifecyclePage(context.Background(), query)
	if err != nil || first.Progress.Complete || first.Progress.Cursor != "2" || len(first.Requests) != 100 {
		t.Fatalf("first page = (%d, %+v, %v)", len(first.Requests), first.Progress, err)
	}
	query.Cursor = first.Progress.Cursor
	second, err := p.ListLifecyclePage(context.Background(), query)
	got := make(map[string]bool, len(second.Requests))
	for _, request := range second.Requests {
		got[request.Request.ID] = true
	}
	if err != nil || !second.Progress.Complete || len(second.Requests) != 100 || len(got) != 100 || got["1"] || !got["2"] || !got["101"] {
		t.Fatalf("overlap page = (%d, %+v, %v)", len(second.Requests), second.Progress, err)
	}
}

func TestNotificationLifecycleDetailFailureRetainsPartialProgress(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden} {
		status := status
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()
			now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/example/repo":
					serveNotificationIdentity(w)
				case "/repos/example/repo/pulls":
					_ = json.NewEncoder(w).Encode([]ghPull{{Number: 1}, {Number: 2}})
				case "/repos/example/repo/pulls/1":
					_ = json.NewEncoder(w).Encode(notificationPull(1, now.Add(-time.Hour)))
				case "/repos/example/repo/pulls/2":
					w.WriteHeader(status)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(server.Close)
			p := &Provider{Owner: "example", Repo: "repo", BaseURL: server.URL, HTTP: server.Client()}
			page, err := p.ListLifecyclePage(context.Background(), forge.LifecycleQuery{Graph: "graph", Kind: "request-created", Through: now, Limit: 100})
			if !errors.Is(err, notification.ErrDiscoveryIncomplete) || page.Progress.Complete || page.Progress.Cursor != "1" || len(page.Requests) != 1 || page.Requests[0].Request.ID != "1" {
				t.Fatalf("detail failure = (%+v, %+v, %v)", page.Requests, page.Progress, err)
			}
		})
	}
}

func TestNotificationLifecycleRejectsCrossOriginContinuation(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", `<https://attacker.invalid/pulls?page=2>; rel="next"`)
		_ = json.NewEncoder(w).Encode([]ghPull{})
	}))
	t.Cleanup(server.Close)
	p := &Provider{Owner: "example", Repo: "repo", BaseURL: server.URL, HTTP: server.Client()}
	page, err := p.ListLifecyclePage(context.Background(), forge.LifecycleQuery{Graph: "graph", Kind: "request-created", Through: time.Now(), Limit: 100})
	if !errors.Is(err, notification.ErrDiscoveryIncomplete) || page.Progress.Complete || page.Progress.Cursor != "1" {
		t.Fatalf("cross-origin continuation = (%+v, %v)", page.Progress, err)
	}
}

func TestNotificationLifecycleHonorsPageLimit(t *testing.T) {
	t.Parallel()
	for _, oversized := range []bool{false, true} {
		t.Run(strconv.FormatBool(oversized), func(t *testing.T) {
			t.Parallel()
			now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/example/repo":
					serveNotificationIdentity(w)
				case "/repos/example/repo/pulls":
					if r.URL.Query().Get("per_page") != "2" {
						t.Error("ignored caller page limit")
					}
					page := []ghPull{{Number: 2}, {Number: 10}}
					if oversized {
						page = append(page, ghPull{Number: 11})
					}
					if r.URL.Query().Get("page") == "1" {
						w.Header().Set("Link", "<"+server.URL+"/repos/example/repo/pulls?page=2>; rel=\"next\"")
					} else {
						page = []ghPull{{Number: 11}}
					}
					_ = json.NewEncoder(w).Encode(page)
				default:
					if oversized {
						t.Error("oversized page reached detail reads")
					}
					id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/repos/example/repo/pulls/"))
					_ = json.NewEncoder(w).Encode(notificationPull(id, now.Add(-time.Hour)))
				}
			}))
			t.Cleanup(server.Close)
			p := &Provider{Owner: "example", Repo: "repo", BaseURL: server.URL, HTTP: server.Client()}
			query := forge.LifecycleQuery{Graph: "graph", Through: now, Limit: 2}
			first, err := p.ListLifecyclePage(context.Background(), query)
			if oversized {
				if !errors.Is(err, notification.ErrDiscoveryIncomplete) || first.Progress.Cursor != "1" || first.Progress.Complete {
					t.Fatalf("oversized page accepted: %+v, %v", first, err)
				}
				return
			}
			if err != nil || first.Progress.Cursor != "2" || len(first.Requests) != 2 {
				t.Fatalf("first page: %+v, %v", first, err)
			}
			// IDs are opaque strings; lexical ordering is deterministic even for
			// numeric-looking IDs. Pagination itself uses provider creation order.
			if first.Requests[0].Request.ID != "10" || first.Requests[1].Request.ID != "2" {
				t.Fatal("unstable ID ordering")
			}
			query.Cursor = first.Progress.Cursor
			second, err := p.ListLifecyclePage(context.Background(), query)
			if err != nil || !second.Progress.Complete || second.Pages != 2 || len(second.Requests) != 3 {
				t.Fatalf("bounded overlapping continuation: %+v, %v", second, err)
			}
		})
	}
}
