package forgetest

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/skaphos/oiax/v2/internal/engine"
	"github.com/skaphos/oiax/v2/internal/forge"
	mk "github.com/skaphos/oiax/v2/internal/forge/marker"
	"github.com/skaphos/oiax/v2/internal/notification"
)

// CreationScenario owns remote state for shared provider conformance. All
// mutations are local fixture HTTP requests, never live forge writes.
type CreationScenario struct {
	t        *testing.T
	mu       sync.Mutex
	Provider string
	Mode     string
	Request  forge.CreateRequest
	body     string
	posted   string
	writes   int
}

func (s *CreationScenario) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	respond := func(value any) {
		if err := json.NewEncoder(w).Encode(value); err != nil {
			s.t.Error(err)
		}
	}
	collection := "/repos/example/repo/pulls"
	if s.Provider == "azuredevops" {
		collection = "/project/_apis/git/repositories/repo/pullrequests"
	}
	if r.Method == http.MethodPost && r.URL.Path == collection {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.t.Error(err)
		}
		key := "body"
		if s.Provider == "azuredevops" {
			key = "description"
		}
		s.posted, _ = payload[key].(string)
		switch {
		case s.Mode == "post-failure":
			w.WriteHeader(http.StatusBadRequest)
			respond(map[string]string{"message": "create rejected"})
		case strings.HasPrefix(s.Mode, "adopt"):
			status := http.StatusUnprocessableEntity
			if s.Provider == "azuredevops" {
				status = http.StatusConflict
			}
			w.WriteHeader(status)
			respond(map[string]string{"message": "TF401179 duplicate", "typeKey": "GitPullRequestExistsException"})
		default:
			s.body = s.posted
			respond(s.pull())
		}
		return
	}
	if r.Method == http.MethodPatch && r.URL.Path == collection+"/42" {
		s.writes++
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.t.Error(err)
		}
		s.body = payload["body"]
		if s.Provider == "azuredevops" {
			s.body = payload["description"]
		}
		respond(s.pull())
		return
	}
	if r.Method != http.MethodGet {
		s.writes++
		if s.Mode == "followup-failure" {
			w.WriteHeader(http.StatusBadRequest)
			respond(map[string]string{"message": "follow-up rejected"})
		} else {
			respond(map[string]any{})
		}
		return
	}
	switch {
	case r.URL.Path == "/repos/example/repo":
		respond(map[string]any{"id": 123, "full_name": "example/repo"})
	case r.URL.Path == "/project/_apis/git/repositories/repo":
		respond(map[string]any{"id": "repository-id", "name": "repo", "project": map[string]string{"id": "project-id"}})
	case r.URL.Path == "/_apis/connectionData":
		respond(map[string]string{"instanceId": "organization-id"})
	case r.URL.Path == collection:
		if s.Provider == "azuredevops" {
			respond(map[string]any{"value": []any{s.pull()}, "count": 1})
		} else {
			respond([]any{s.pull()})
		}
	case r.URL.Path == collection+"/42":
		respond(s.pull())
	case strings.HasSuffix(r.URL.Path, "/properties"):
		respond(map[string]any{"value": map[string]any{}}) // no supplemental property required for recovery
	case strings.HasPrefix(r.URL.Path, "/repos/example/repo/labels/"):
		respond(map[string]string{"name": "oiax"})
	default:
		s.t.Errorf("unexpected creation fixture read: %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *CreationScenario) pull() map[string]any {
	created := "2026-09-05T12:00:01Z"
	if s.Provider == "azuredevops" {
		return map[string]any{"pullRequestId": 42, "status": "active", "description": s.body, "sourceRefName": "refs/heads/" + s.Request.Source, "targetRefName": "refs/heads/" + s.Request.Target, "creationDate": created}
	}
	// The live source has advanced since POST. Origin retains the pre-POST hint;
	// it must not silently become this moving SHA or claim verified membership.
	return map[string]any{"number": 42, "state": "open", "body": s.body, "created_at": created,
		"head": map[string]any{"ref": s.Request.Source, "sha": strings.Repeat("d", 40), "repo": map[string]string{"full_name": "example/repo"}},
		"base": map[string]any{"ref": s.Request.Target, "sha": strings.Repeat("e", 40), "repo": map[string]string{"full_name": "example/repo"}}}
}

func RunNotificationCreation(t *testing.T, provider string, factory func(*testing.T, *CreationScenario) forge.Forge) {
	t.Helper()
	for _, kind := range []engine.RequestType{engine.RequestTypePromotion, engine.RequestTypeBackflow} {
		for _, mode := range []string{"created", "followup-failure", "post-failure", "adopt-origin", "adopt-legacy"} {
			t.Run(string(kind)+"/"+mode, func(t *testing.T) {
				t.Parallel()
				o := notification.NotificationOriginV1{Version: 1, OperationID: "initial-operation", Graph: "graph", ConfigOID: strings.Repeat("a", 40), ObservedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), LogicalSource: "dev", LogicalTarget: "test", SourceOID: strings.Repeat("b", 40), BaseOID: strings.Repeat("c", 40)}
				req := forge.CreateRequest{Graph: "graph", Type: kind, Source: "dev", Target: "test", SourceHead: o.SourceOID, Body: "Human text"}
				if kind == engine.RequestTypeBackflow {
					req.Source, req.Target, o.LogicalSource, o.LogicalTarget = "oiax/backflow/main-to-dev/abcdef0", "dev", "main", "dev"
				}
				req.Origin = &o
				s := &CreationScenario{t: t, Provider: provider, Mode: mode, Request: req}
				m := mk.Marker{Version: "v1", Graph: req.Graph, Type: string(req.Type), Source: req.Source, Destination: req.Target, SourceHead: req.SourceHead}
				s.body = mk.Serialize(m) + "\n\nExisting human text"
				original := o
				original.OperationID = "original-adopted-operation"
				if mode == "adopt-origin" {
					var err error
					s.body, err = mk.AppendNotificationOrigin(s.body, &original)
					if err != nil {
						t.Fatal(err)
					}
				}
				before := s.body
				f := factory(t, s)
				out, err := f.CreateRequest(context.Background(), req)
				if mode == "post-failure" {
					if err == nil || out.Disposition != "" || out.Request.ID != "" || out.Origin != nil {
						t.Fatalf("failed POST became creation: %+v, %v", out, err)
					}
					return
				}
				if (err != nil) != (mode == "followup-failure") || out.Request.ID != "42" {
					t.Fatalf("creation result: %+v, %v", out, err)
				}
				wantDisposition := forge.RequestCreated
				wantOrigin := &o
				if strings.HasPrefix(mode, "adopt") {
					wantDisposition = forge.RequestAdopted
					wantOrigin = &original
					if mode == "adopt-legacy" {
						wantOrigin = nil
					}
					s.mu.Lock()
					unchanged := s.body == before && s.writes == 0
					s.mu.Unlock()
					if !unchanged {
						t.Fatal("adoption edited original provenance")
					}
				}
				if out.Disposition != wantDisposition || !reflect.DeepEqual(out.Origin, wantOrigin) {
					t.Fatalf("wrong disposition/origin: %+v", out)
				}
				reader := f.(forge.LifecycleReader)
				recovered, err := reader.GetLifecycleRequest(context.Background(), "42")
				if err != nil || !reflect.DeepEqual(recovered.Origin, wantOrigin) {
					t.Fatalf("full-body recovery: %+v, %v", recovered, err)
				}
				if wantOrigin != nil && (recovered.Request.LogicalSource != wantOrigin.LogicalSource || recovered.Request.LogicalDestination != wantOrigin.LogicalTarget) {
					t.Fatal("logical edge lost during recovery")
				}
				if mode == "created" {
					if err := f.UpdateRequest(context.Background(), forge.UpdateRequest{ID: "42", SourceHead: strings.Repeat("f", 40)}); err != nil {
						t.Fatal(err)
					}
					recovered, err = reader.GetLifecycleRequest(context.Background(), "42")
					if err != nil || !reflect.DeepEqual(recovered.Origin, &o) {
						t.Fatal("baseline update rewrote origin", err)
					}
				}
			})
		}
	}
}
