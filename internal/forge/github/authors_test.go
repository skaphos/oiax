package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
	selfLogin     = "oiax-app[bot]"
	outsiderLogin = "mallory"
)

// authorAPI is a fake GitHub API holding a fixed set of pull requests. It
// answers the identity lookup as viewer (or fails it when viewer is empty) and
// records every write.
type authorAPI struct {
	mu          sync.Mutex
	viewer      string
	pulls       []map[string]any
	identityHit int
	writes      []string
}

func (a *authorAPI) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		switch {
		case r.URL.Path == "/graphql":
			a.identityHit++
			if r.Method != http.MethodPost {
				t.Errorf("identity lookup used %s, want POST", r.Method)
			}
			if a.viewer == "" {
				writeJSON(t, w, http.StatusForbidden, map[string]string{"message": "Resource not accessible by integration"})
				return
			}
			writeJSON(t, w, http.StatusOK, map[string]any{
				"data": map[string]any{"viewer": map[string]string{"login": a.viewer}},
			})
		case r.Method != http.MethodGet:
			a.writes = append(a.writes, r.Method+" "+r.URL.Path)
			writeJSON(t, w, http.StatusOK, map[string]any{})
		case r.URL.Path == "/repos/acme/widgets":
			writeJSON(t, w, http.StatusOK, map[string]any{"id": 7, "full_name": "acme/widgets"})
		case r.URL.Path == "/repos/acme/widgets/pulls":
			writeJSON(t, w, http.StatusOK, a.pulls)
		case strings.HasPrefix(r.URL.Path, "/repos/acme/widgets/pulls/"):
			for _, pr := range a.pulls {
				if strings.HasSuffix(r.URL.Path, "/"+itoa(pr["number"].(int))) {
					writeJSON(t, w, http.StatusOK, pr)
					return
				}
			}
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
}

func itoa(n int) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{digits[n%10]}, b...)
	}
	return string(b)
}

// promotionBy renders an open development -> test promotion request, shape-
// identical to oiax's own, opened by login.
func promotionBy(number int, login string) map[string]any {
	pr := prSpec{number: number, source: "development", dest: "test", graph: "environments",
		typ: "promotion", sourceHead: strings.Repeat("a", 40), authorLogin: login,
		createdAt: "2026-01-02T03:04:05Z"}.toPull()
	repo := map[string]any{"full_name": "acme/widgets"}
	pr["head"] = map[string]any{"ref": "development", "sha": strings.Repeat("a", 40), "repo": repo}
	pr["base"] = map[string]any{"ref": "test", "sha": strings.Repeat("b", 40), "repo": repo}
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
		Owner: "acme", Repo: "widgets", Token: testToken,
		BaseURL: srv.URL, HTTP: srv.Client(), retryBackoff: time.Microsecond,
		retryMax:       1,
		TrustedAuthors: trusted,
		Warn:           func(msg string) { warnings = append(warnings, msg) },
	}
	return p, &warnings
}

var promotionFilter = forge.RequestFilter{Graph: "environments", Type: "promotion"}

func TestListIgnoresMarkerFromUntrustedAuthor(t *testing.T) {
	t.Parallel()
	api := &authorAPI{viewer: selfLogin, pulls: []map[string]any{
		promotionBy(66, outsiderLogin),
		promotionBy(9, selfLogin),
	}}
	p, warnings := strictProvider(t, api)

	for range 2 {
		got, err := p.ListManagedRequests(context.Background(), promotionFilter)
		if err != nil {
			t.Fatalf("ListManagedRequests: %v", err)
		}
		if len(got) != 1 || got[0].ID != "9" {
			t.Fatalf("managed requests = %+v, want only #9 (opened by the token identity)", got)
		}
	}
	if api.identityHit != 1 {
		t.Errorf("identity lookups = %d, want 1 (resolved once, then cached)", api.identityHit)
	}
	if len(*warnings) != 1 || !strings.Contains((*warnings)[0], "#66") || !strings.Contains((*warnings)[0], outsiderLogin) {
		t.Errorf("warnings = %q, want exactly one naming #66 and its author", *warnings)
	}
}

func TestListMatchesAuthorCaseInsensitively(t *testing.T) {
	t.Parallel()
	api := &authorAPI{viewer: "Oiax-App[bot]", pulls: []map[string]any{promotionBy(9, selfLogin)}}
	p, _ := strictProvider(t, api)
	got, err := p.ListManagedRequests(context.Background(), promotionFilter)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListManagedRequests = %+v, %v; want #9", got, err)
	}
}

func TestListIgnoresMarkerWithNoAuthor(t *testing.T) {
	t.Parallel()
	// A deleted account leaves a request with no usable author; an empty
	// login must never match, whatever the trusted set holds.
	api := &authorAPI{viewer: selfLogin, pulls: []map[string]any{promotionBy(66, "")}}
	p, _ := strictProvider(t, api)
	got, err := p.ListManagedRequests(context.Background(), promotionFilter)
	if err != nil || len(got) != 0 {
		t.Fatalf("ListManagedRequests = %+v, %v; want none", got, err)
	}
}

func TestTrustedAuthorsKeepPreviousIdentityManaged(t *testing.T) {
	t.Parallel()
	api := &authorAPI{viewer: selfLogin, pulls: []map[string]any{
		promotionBy(9, "github-actions[bot]"),
		promotionBy(66, outsiderLogin),
	}}
	p, _ := strictProvider(t, api, " GitHub-Actions[bot] ", "")
	got, err := p.ListManagedRequests(context.Background(), promotionFilter)
	if err != nil {
		t.Fatalf("ListManagedRequests: %v", err)
	}
	if len(got) != 1 || got[0].ID != "9" {
		t.Fatalf("managed requests = %+v, want only #9 (declared previous identity)", got)
	}
}

func TestNoIdentityLookupWithoutMarkedRequest(t *testing.T) {
	t.Parallel()
	plain := promotionBy(3, outsiderLogin)
	plain["body"] = "an ordinary pull request"
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
	api := &authorAPI{pulls: []map[string]any{promotionBy(9, selfLogin)}}
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
	api.viewer = selfLogin
	api.mu.Unlock()
	if got, err := p.ListManagedRequests(context.Background(), promotionFilter); err != nil || len(got) != 1 {
		t.Fatalf("after recovery ListManagedRequests = %+v, %v; want #9", got, err)
	}
}

func TestDeclaredAuthorsSufficeWhenLookupFails(t *testing.T) {
	t.Parallel()
	api := &authorAPI{pulls: []map[string]any{promotionBy(9, selfLogin), promotionBy(66, outsiderLogin)}}
	p, warnings := strictProvider(t, api, selfLogin)
	got, err := p.ListManagedRequests(context.Background(), promotionFilter)
	if err != nil || len(got) != 1 || got[0].ID != "9" {
		t.Fatalf("ListManagedRequests = %+v, %v; want only #9", got, err)
	}
	if len(*warnings) == 0 || !strings.Contains((*warnings)[0], "OIAX_TRUSTED_AUTHORS") {
		t.Errorf("warnings = %q, want the lookup-failure notice first", *warnings)
	}
}

func TestInvalidTrustedAuthorRejected(t *testing.T) {
	t.Parallel()
	api := &authorAPI{viewer: selfLogin, pulls: []map[string]any{promotionBy(9, selfLogin)}}
	p, _ := strictProvider(t, api, "not a login\n##vso[x]")
	if _, err := p.ListManagedRequests(context.Background(), promotionFilter); err == nil {
		t.Fatal("ListManagedRequests accepted a malformed trusted author")
	}
}

func TestMutationsRefuseUntrustedAuthor(t *testing.T) {
	t.Parallel()
	api := &authorAPI{viewer: selfLogin, pulls: []map[string]any{promotionBy(66, outsiderLogin)}}
	p, _ := strictProvider(t, api)
	ctx := context.Background()

	if err := p.UpdateRequest(ctx, forge.UpdateRequest{ID: "66", SourceHead: strings.Repeat("c", 40)}); err == nil ||
		!strings.Contains(err.Error(), "not a managed request") {
		t.Errorf("UpdateRequest err = %v, want not-managed refusal", err)
	}
	if err := p.CloseRequest(ctx, "66", forge.Reason{Summary: "obsolete"}); err == nil ||
		!strings.Contains(err.Error(), "not a managed request") {
		t.Errorf("CloseRequest err = %v, want not-managed refusal", err)
	}
	if len(api.writes) != 0 {
		t.Errorf("writes against a request oiax did not open: %q", api.writes)
	}
}

func TestLifecycleRefusesUntrustedAuthor(t *testing.T) {
	t.Parallel()
	api := &authorAPI{viewer: selfLogin, pulls: []map[string]any{
		promotionBy(66, outsiderLogin),
		promotionBy(9, selfLogin),
	}}
	p, _ := strictProvider(t, api)
	ctx := context.Background()

	if _, err := p.GetLifecycleRequest(ctx, "66"); !errors.Is(err, notification.ErrNotManaged) {
		t.Errorf("GetLifecycleRequest(#66) err = %v, want ErrNotManaged", err)
	}
	// Control: the same request shape opened by the token identity is admitted,
	// so the refusal above is the author check and nothing else.
	if req, err := p.GetLifecycleRequest(ctx, "9"); err != nil || req.Request.ID != "9" {
		t.Errorf("GetLifecycleRequest(#9) = %+v, %v; want admitted", req.Request, err)
	}
}

func TestLifecycleUnavailableWhenIdentityUnresolvable(t *testing.T) {
	t.Parallel()
	api := &authorAPI{pulls: []map[string]any{promotionBy(9, selfLogin)}}
	p, _ := strictProvider(t, api)
	if _, err := p.GetLifecycleRequest(context.Background(), "9"); !errors.Is(err, notification.ErrLifecycleUnavailable) {
		t.Errorf("GetLifecycleRequest err = %v, want ErrLifecycleUnavailable", err)
	}
}

func TestCreateWarnsWhenAuthorIsNotTrusted(t *testing.T) {
	t.Parallel()
	// The forge records a different author than the identity lookup reported:
	// left silent, the next run would not recognize the request it just made.
	created := promotionBy(12, "some-other-app[bot]")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/graphql":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"data": map[string]any{"viewer": map[string]string{"login": selfLogin}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/pulls":
			writeJSON(t, w, http.StatusCreated, created)
		default:
			writeJSON(t, w, http.StatusOK, []any{})
		}
	}))
	defer srv.Close()
	var warnings []string
	p := &Provider{Owner: "acme", Repo: "widgets", Token: testToken, BaseURL: srv.URL, HTTP: srv.Client(),
		Warn: func(msg string) { warnings = append(warnings, msg) }}

	_, err := p.CreateRequest(context.Background(), forge.CreateRequest{
		Graph: "environments", Type: "promotion", Source: "development", Target: "test",
		SourceHead: strings.Repeat("a", 40), Title: "Promote", Body: "body",
	})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "OIAX_TRUSTED_AUTHORS=some-other-app[bot]") {
		t.Errorf("warnings = %q, want one naming the author to declare", warnings)
	}
}

func TestGraphQLURL(t *testing.T) {
	t.Parallel()
	for base, want := range map[string]string{
		"":                                defaultBaseURL + "/graphql",
		"https://ghe.example.com/api/v3":  "https://ghe.example.com/api/graphql",
		"https://ghe.example.com/api/v3/": "https://ghe.example.com/api/graphql",
		"http://127.0.0.1:1234":           "http://127.0.0.1:1234/graphql",
	} {
		if got := (&Provider{BaseURL: base}).graphqlURL(); got != want {
			t.Errorf("graphqlURL() with BaseURL %q = %q, want %q", base, got, want)
		}
	}
}

func TestDeclaredAuthorsDoNotPinAFailedLookup(t *testing.T) {
	t.Parallel()
	// A declared previous identity lets discovery proceed while the token's
	// own identity cannot be looked up — but that partial set must not be
	// cached, or the current identity's requests stay unmanaged for the rest
	// of the run even after the lookup recovers.
	api := &authorAPI{pulls: []map[string]any{
		promotionBy(8, "github-actions[bot]"),
		promotionBy(9, selfLogin),
	}}
	p, warnings := strictProvider(t, api, "github-actions[bot]")
	ctx := context.Background()

	for range 2 {
		got, err := p.ListManagedRequests(ctx, promotionFilter)
		if err != nil || len(got) != 1 || got[0].ID != "8" {
			t.Fatalf("while the lookup fails: ListManagedRequests = %+v, %v; want only #8", got, err)
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
	api.viewer = selfLogin
	api.mu.Unlock()
	got, err := p.ListManagedRequests(ctx, promotionFilter)
	if err != nil || len(got) != 2 {
		t.Fatalf("after recovery: ListManagedRequests = %+v, %v; want #8 and #9", got, err)
	}
	// Mutation paths share the same set, so they recover too.
	if err := p.CloseRequest(ctx, "9", forge.Reason{Summary: "obsolete"}); err != nil {
		t.Errorf("after recovery: CloseRequest(#9) = %v, want success", err)
	}
	// Once resolved, the set is cached: no further lookups.
	api.mu.Lock()
	hits := api.identityHit
	api.mu.Unlock()
	if _, err := p.ListManagedRequests(ctx, promotionFilter); err != nil {
		t.Fatalf("ListManagedRequests: %v", err)
	}
	if api.identityHit != hits {
		t.Errorf("identity looked up again after it resolved (%d -> %d)", hits, api.identityHit)
	}
}

func TestCreateWarnsWhenIdentityUnresolvable(t *testing.T) {
	t.Parallel()
	// First run: nothing carried a marker, so the identity is first needed
	// after the request exists. A failed lookup must not pass silently.
	api := &authorAPI{}
	p, warnings := strictProvider(t, api)
	p.warnForeignAuthor(context.Background(), ghPull{Number: 12, User: ghUser{Login: selfLogin}})
	if len(*warnings) != 1 || !strings.Contains((*warnings)[0], "#12") ||
		!strings.Contains((*warnings)[0], "OIAX_TRUSTED_AUTHORS="+selfLogin) {
		t.Errorf("warnings = %q, want one naming #12 and the author to declare", *warnings)
	}
}
