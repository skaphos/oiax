package azuredevops

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/skaphos/oiax/v2/internal/forge"
	"github.com/skaphos/oiax/v2/internal/notification"
)

// Author provenance (ADR 0020). These tests run the real check: the provider
// is built without the anyAuthor fixture switch, and the fake API answers the
// token-identity lookup.

const (
	selfID     = "11111111-2222-3333-4444-555555555555"
	previousID = "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"
	outsiderID = "99999999-8888-7777-6666-555555555555"
)

// authorAPI is a fake Azure DevOps API holding a fixed set of pull requests.
// It answers connectionData as self (or fails it when self is empty) and
// records every write.
type authorAPI struct {
	mu          sync.Mutex
	self        string
	pulls       []map[string]any
	identityHit int
	writes      []string
	failStatus  int // status for a failed identity lookup; zero means 401
	// anonymous makes connectionData succeed without an authenticated user, so
	// the repository identity resolves while the token identity does not.
	anonymous bool
}

func (a *authorAPI) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/_apis/connectionData"):
			a.identityHit++
			if a.self == "" && a.anonymous {
				// The endpoint answers, but names no identity.
				writeJSON(t, w, http.StatusOK, map[string]any{"instanceId": "0f0f0f0f-0000-0000-0000-000000000001"})
				return
			}
			if a.self == "" {
				status := http.StatusUnauthorized
				if a.failStatus != 0 {
					status = a.failStatus
				}
				writeJSON(t, w, status, map[string]string{"message": "denied"})
				return
			}
			writeJSON(t, w, http.StatusOK, map[string]any{
				"instanceId":        "0f0f0f0f-0000-0000-0000-000000000001",
				"authenticatedUser": map[string]string{"id": a.self},
			})
		case r.Method != http.MethodGet:
			a.writes = append(a.writes, r.Method+" "+path)
			writeJSON(t, w, http.StatusOK, map[string]any{})
		case path == gitBase:
			writeJSON(t, w, http.StatusOK, map[string]any{
				"id": "0f0f0f0f-0000-0000-0000-000000000002", "name": "deploy",
				"project": map[string]string{"id": "0f0f0f0f-0000-0000-0000-000000000003"},
			})
		case path == gitBase+"/pullrequests":
			writeJSON(t, w, http.StatusOK, map[string]any{"value": a.pulls})
		case strings.HasSuffix(path, "/properties"):
			// No durable property copy: the description marker is the source.
			writeJSON(t, w, http.StatusOK, map[string]any{"value": map[string]any{}})
		case strings.HasPrefix(path, gitBase+"/pullrequests/"):
			for _, pr := range a.pulls {
				if strings.HasSuffix(path, "/"+strconv.Itoa(pr["pullRequestId"].(int))) {
					writeJSON(t, w, http.StatusOK, pr)
					return
				}
			}
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected request %s %s", r.Method, path)
			http.NotFound(w, r)
		}
	})
}

// promotionBy renders an active development -> test promotion request,
// shape-identical to Oiax's own, created by identity id.
func promotionBy(number int, id string) map[string]any {
	pr := pullSpec{id: number, source: "development", dest: "test", graph: "environments",
		typ: "promotion", sourceHead: strings.Repeat("a", 40)}.toPull()
	pr["creationDate"] = "2026-01-02T03:04:05Z"
	pr["lastMergeSourceCommit"] = map[string]string{"commitId": strings.Repeat("a", 40)}
	pr["lastMergeTargetCommit"] = map[string]string{"commitId": strings.Repeat("b", 40)}
	if id != "" {
		pr["createdBy"] = map[string]string{"id": id, "displayName": "Oiax Build Service"}
	}
	return pr
}

// strictProvider is a provider with the provenance check live, collecting
// warnings.
func strictProvider(t *testing.T, api *authorAPI, trusted ...string) (*Provider, *[]string) {
	t.Helper()
	srv := httptest.NewServer(api.handler(t))
	t.Cleanup(srv.Close)
	var warnings []string
	p := &Provider{
		Repo:  Repo{Organization: "acme", Project: "platform", Name: "deploy"},
		Token: testToken, BaseURL: srv.URL, HTTP: srv.Client(),
		retryBackoff: time.Microsecond, retryMax: 1,
		identityRetry:  time.Nanosecond,
		TrustedAuthors: trusted,
		Warn:           func(msg string) { warnings = append(warnings, msg) },
	}
	return p, &warnings
}

var promotionFilter = forge.RequestFilter{Graph: "environments", Type: "promotion"}

func TestListIgnoresMarkerFromUntrustedCreator(t *testing.T) {
	t.Parallel()
	api := &authorAPI{self: selfID, pulls: []map[string]any{
		promotionBy(66, outsiderID),
		promotionBy(9, strings.ToUpper(selfID)), // ids compare case-insensitively
		promotionBy(67, ""),                     // no creator recorded: never trusted
	}}
	p, warnings := strictProvider(t, api)

	for range 2 {
		got, err := p.ListManagedRequests(context.Background(), promotionFilter)
		if err != nil {
			t.Fatalf("ListManagedRequests: %v", err)
		}
		if len(got) != 1 || got[0].ID != "9" {
			t.Fatalf("managed requests = %+v, want only 9 (created by the token identity)", got)
		}
	}
	if api.identityHit != 1 {
		t.Errorf("identity lookups = %d, want 1 (resolved once, then cached)", api.identityHit)
	}
	if len(*warnings) != 2 || !strings.Contains((*warnings)[0], "pull request 66") || !strings.Contains((*warnings)[0], outsiderID) {
		t.Errorf("warnings = %q, want one per untrusted request, the first naming 66 and its creator", *warnings)
	}
}

func TestTrustedAuthorsKeepPreviousIdentityManaged(t *testing.T) {
	t.Parallel()
	api := &authorAPI{self: selfID, pulls: []map[string]any{
		promotionBy(9, strings.ToLower(previousID)),
		promotionBy(66, outsiderID),
	}}
	p, _ := strictProvider(t, api, " "+previousID+" ", "")
	got, err := p.ListManagedRequests(context.Background(), promotionFilter)
	if err != nil {
		t.Fatalf("ListManagedRequests: %v", err)
	}
	if len(got) != 1 || got[0].ID != "9" {
		t.Fatalf("managed requests = %+v, want only 9 (declared previous identity)", got)
	}
}

func TestNoIdentityLookupWithoutMarkedRequest(t *testing.T) {
	t.Parallel()
	plain := promotionBy(3, outsiderID)
	plain["description"] = "an ordinary pull request"
	api := &authorAPI{pulls: []map[string]any{plain}} // lookup would fail
	p, _ := strictProvider(t, api)
	got, err := p.ListManagedRequests(context.Background(), promotionFilter)
	if err != nil || len(got) != 0 {
		t.Fatalf("ListManagedRequests = %+v, %v; want none and no error", got, err)
	}
	if api.identityHit != 0 {
		t.Errorf("identity lookups = %d, want 0 when nothing carries a marker", api.identityHit)
	}
}

func TestUnresolvableIdentityFailsClosed(t *testing.T) {
	t.Parallel()
	api := &authorAPI{pulls: []map[string]any{promotionBy(9, selfID)}}
	p, _ := strictProvider(t, api)
	_, err := p.ListManagedRequests(context.Background(), promotionFilter)
	if !errors.Is(err, errNoIdentity) {
		t.Fatalf("ListManagedRequests err = %v, want errNoIdentity", err)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("error leaks the token: %v", err)
	}
	// The failure is not cached: once the lookup works, so does discovery.
	api.mu.Lock()
	api.self = selfID
	api.mu.Unlock()
	if got, err := p.ListManagedRequests(context.Background(), promotionFilter); err != nil || len(got) != 1 {
		t.Fatalf("after recovery ListManagedRequests = %+v, %v; want 9", got, err)
	}
}

func TestDeclaredAuthorsSufficeWhenLookupFails(t *testing.T) {
	t.Parallel()
	api := &authorAPI{pulls: []map[string]any{promotionBy(9, selfID), promotionBy(66, outsiderID)}}
	p, warnings := strictProvider(t, api, selfID)
	got, err := p.ListManagedRequests(context.Background(), promotionFilter)
	if err != nil || len(got) != 1 || got[0].ID != "9" {
		t.Fatalf("ListManagedRequests = %+v, %v; want only 9", got, err)
	}
	if len(*warnings) == 0 || !strings.Contains((*warnings)[0], "OIAX_TRUSTED_AUTHORS") {
		t.Errorf("warnings = %q, want the lookup-failure notice first", *warnings)
	}
}

func TestInvalidTrustedAuthorRejected(t *testing.T) {
	t.Parallel()
	api := &authorAPI{self: selfID, pulls: []map[string]any{promotionBy(9, selfID)}}
	p, _ := strictProvider(t, api, "Project Collection Build Service")
	if _, err := p.ListManagedRequests(context.Background(), promotionFilter); err == nil {
		t.Fatal("ListManagedRequests accepted a trusted author that is not an identity id")
	}
}

func TestMutationsRefuseUntrustedCreator(t *testing.T) {
	t.Parallel()
	api := &authorAPI{self: selfID, pulls: []map[string]any{promotionBy(66, outsiderID)}}
	p, _ := strictProvider(t, api)
	ctx := context.Background()

	if err := p.UpdateRequest(ctx, forge.UpdateRequest{ID: "66", SourceHead: strings.Repeat("c", 40)}); !errors.Is(err, errNotManaged) {
		t.Errorf("UpdateRequest err = %v, want errNotManaged", err)
	}
	if err := p.CloseRequest(ctx, "66", forge.Reason{Summary: "obsolete"}); !errors.Is(err, errNotManaged) {
		t.Errorf("CloseRequest err = %v, want errNotManaged", err)
	}
	if len(api.writes) != 0 {
		t.Errorf("writes against a request oiax did not create: %q", api.writes)
	}
}

func TestLifecycleRefusesUntrustedCreator(t *testing.T) {
	t.Parallel()
	api := &authorAPI{self: selfID, pulls: []map[string]any{
		promotionBy(66, outsiderID),
		promotionBy(9, selfID),
	}}
	p, _ := strictProvider(t, api)
	ctx := context.Background()

	if _, err := p.GetLifecycleRequest(ctx, "66"); !errors.Is(err, notification.ErrNotManaged) {
		t.Errorf("GetLifecycleRequest(66) err = %v, want ErrNotManaged", err)
	}
	// Control: the same request shape created by the token identity is
	// admitted, so the refusal above is the creator check and nothing else.
	if req, err := p.GetLifecycleRequest(ctx, "9"); err != nil || req.Request.ID != "9" {
		t.Errorf("GetLifecycleRequest(9) = %+v, %v; want admitted", req.Request, err)
	}
}

func TestDeclaredAuthorsDoNotPinAFailedLookup(t *testing.T) {
	t.Parallel()
	// A declared previous identity lets discovery proceed while the token's
	// own identity cannot be looked up — but that partial set must not be
	// cached, or the current identity's requests stay unmanaged for the rest
	// of the run even after the lookup recovers.
	api := &authorAPI{pulls: []map[string]any{
		promotionBy(8, previousID),
		promotionBy(9, selfID),
	}}
	p, warnings := strictProvider(t, api, previousID)
	ctx := context.Background()

	for range 2 {
		got, err := p.ListManagedRequests(ctx, promotionFilter)
		if err != nil || len(got) != 1 || got[0].ID != "8" {
			t.Fatalf("while the lookup fails: ListManagedRequests = %+v, %v; want only 8", got, err)
		}
	}
	lookupNotices := 0
	for _, w := range *warnings {
		if strings.Contains(w, "could not look up the token's own identity") {
			lookupNotices++
		}
	}
	if lookupNotices != 1 {
		t.Errorf("lookup-failure notices = %d, want 1 across repeated calls; warnings: %q", lookupNotices, *warnings)
	}

	api.mu.Lock()
	api.self = selfID
	api.mu.Unlock()
	got, err := p.ListManagedRequests(ctx, promotionFilter)
	if err != nil || len(got) != 2 {
		t.Fatalf("after recovery: ListManagedRequests = %+v, %v; want 8 and 9", got, err)
	}
	// The single-request paths share the same set, so they recover too.
	if req, err := p.GetLifecycleRequest(ctx, "9"); err != nil || req.Request.ID != "9" {
		t.Errorf("after recovery: GetLifecycleRequest(9) = %+v, %v; want admitted", req.Request, err)
	}
}

func TestCreateWarnsWhenIdentityUnresolvable(t *testing.T) {
	t.Parallel()
	// First run: nothing carried a marker, so the identity is first needed
	// after the request exists. A failed lookup must not pass silently.
	api := &authorAPI{}
	p, warnings := strictProvider(t, api)
	p.warnForeignAuthor(context.Background(), adoPull{PullRequestID: 42, CreatedBy: adoIdentityRef{ID: strings.ToUpper(selfID)}})
	if len(*warnings) != 1 || !strings.Contains((*warnings)[0], "pull request 42") ||
		!strings.Contains((*warnings)[0], "OIAX_TRUSTED_AUTHORS="+selfID) {
		t.Errorf("warnings = %q, want one naming 42 and the identity to declare", *warnings)
	}
}

func TestCreateWarnsWhenCreatorIsNotTrusted(t *testing.T) {
	t.Parallel()
	api := &authorAPI{self: selfID}
	p, warnings := strictProvider(t, api)
	p.warnForeignAuthor(context.Background(), adoPull{PullRequestID: 42, CreatedBy: adoIdentityRef{ID: outsiderID}})
	if len(*warnings) != 1 || !strings.Contains((*warnings)[0], "OIAX_TRUSTED_AUTHORS="+outsiderID) {
		t.Errorf("warnings = %q, want one naming the identity to declare", *warnings)
	}
	// The token's own identity is trusted: no warning.
	p.warnForeignAuthor(context.Background(), adoPull{PullRequestID: 43, CreatedBy: adoIdentityRef{ID: selfID}})
	if len(*warnings) != 1 {
		t.Errorf("warned about a request created by the token identity: %q", *warnings)
	}
}

func TestLifecycleIdentity404IsUnavailableNotMissing(t *testing.T) {
	t.Parallel()
	// The request itself reads fine; it is the identity lookup that answers
	// 404. That must not be reported as the request being missing.
	api := &authorAPI{failStatus: http.StatusNotFound, pulls: []map[string]any{promotionBy(9, selfID)}}
	p, _ := strictProvider(t, api)
	_, err := p.GetLifecycleRequest(context.Background(), "9")
	if !errors.Is(err, notification.ErrLifecycleUnavailable) {
		t.Errorf("GetLifecycleRequest err = %v, want ErrLifecycleUnavailable", err)
	}
	// Control: a request that really is absent is still reported missing.
	if _, err := p.GetLifecycleRequest(context.Background(), "404"); !errors.Is(err, notification.ErrRequestMissing) {
		t.Errorf("GetLifecycleRequest(absent) err = %v, want ErrRequestMissing", err)
	}
}

func TestFailedLookupIsNotRepeatedPerRequest(t *testing.T) {
	t.Parallel()
	// With declared authors covering the gap, a lookup that just failed is
	// left alone for the retry interval instead of being repeated for every
	// marker-bearing request on the page.
	api := &authorAPI{pulls: []map[string]any{
		promotionBy(7, previousID), promotionBy(8, previousID), promotionBy(9, previousID),
	}}
	p, _ := strictProvider(t, api, previousID)
	p.identityRetry = time.Hour
	for range 2 {
		got, err := p.ListManagedRequests(context.Background(), promotionFilter)
		if err != nil || len(got) != 3 {
			t.Fatalf("ListManagedRequests = %+v, %v; want all three", got, err)
		}
	}
	if api.identityHit != 1 {
		t.Errorf("identity lookups = %d, want 1 within the retry interval", api.identityHit)
	}
}

func TestLifecycleDoesNotSkipWhileIdentityUnresolved(t *testing.T) {
	t.Parallel()
	// Declared authors keep core discovery working while the token identity
	// cannot be looked up, but a lifecycle scan records progress past every
	// request it rules out. A request that may be this token's own must read
	// as unavailable, not as unmanaged, until the identity resolves.
	api := &authorAPI{anonymous: true, pulls: []map[string]any{promotionBy(9, selfID), promotionBy(8, previousID)}}
	p, _ := strictProvider(t, api, previousID)
	ctx := context.Background()

	if _, err := p.GetLifecycleRequest(ctx, "9"); !errors.Is(err, notification.ErrLifecycleUnavailable) {
		t.Errorf("while unresolved: GetLifecycleRequest(9) err = %v, want ErrLifecycleUnavailable", err)
	}
	// A declared author is a positive match and needs no lookup.
	if req, err := p.GetLifecycleRequest(ctx, "8"); err != nil || req.Request.ID != "8" {
		t.Errorf("while unresolved: GetLifecycleRequest(8) = %+v, %v; want admitted", req.Request, err)
	}
	// Mutations still refuse: unknown provenance is never permission.
	if err := p.CloseRequest(ctx, "9", forge.Reason{Summary: "obsolete"}); err == nil {
		t.Error("while unresolved: CloseRequest(9) succeeded")
	}
	if len(api.writes) != 0 {
		t.Errorf("writes while provenance was unknown: %q", api.writes)
	}

	api.mu.Lock()
	api.self = selfID
	api.mu.Unlock()
	if req, err := p.GetLifecycleRequest(ctx, "9"); err != nil || req.Request.ID != "9" {
		t.Errorf("after recovery: GetLifecycleRequest(9) = %+v, %v; want admitted", req.Request, err)
	}
}
