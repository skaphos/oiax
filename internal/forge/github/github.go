// Package github implements the forge.Forge interface against the GitHub
// REST API. It is the first supported provider (v0.1 roadmap).
//
// Provider notes that shape this implementation:
//
//   - Managed requests are identified by the machine-readable marker in
//     the request body plus branch relationship plus the oiax label,
//     never by title. Unmanaged requests between the same branches are
//     never touched.
//   - GitHub rejects a second open pull request with the same head/base
//     pair (HTTP 422); that rejection is adopted as success (re-list and
//     continue), not treated as an error. The forge is the concurrency
//     arbiter for promotion requests.
//   - Pull requests created with the default GITHUB_TOKEN are authored by
//     github-actions[bot] and do not trigger `on: pull_request`
//     workflows. The provider warns when it observes that degraded
//     configuration; production guidance is a GitHub App installation
//     token.
//
// Only the standard library is used: the REST surface Oiax needs is small
// and stable, and a client library would add supply-chain surface for no
// real leverage. Credential values never appear in any error or output.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
)

const (
	defaultBaseURL = "https://api.github.com"
	acceptHeader   = "application/vnd.github+json"
	apiVersion     = "2022-11-28"
	// botLogin is the author of pull requests created with the default
	// GITHUB_TOKEN; it is exactly the condition under which
	// `on: pull_request` workflows do not fire.
	botLogin = "github-actions[bot]"
	// degradationWarning is emitted once when a created request will not
	// trigger CI.
	degradationWarning = "created pull request is authored by " + botLogin +
		"; on: pull_request workflows will not run for it. Configure a GitHub App " +
		"installation token so managed requests get CI."
)

// Provider is the GitHub implementation of forge.Forge.
type Provider struct {
	// Owner and Repo identify the repository.
	Owner string
	Repo  string
	// Token is the credential used for API calls: a GitHub App
	// installation token (recommended), fine-grained PAT, or GITHUB_TOKEN
	// (degraded; created requests get no CI). Never logged.
	Token string
	// BaseURL overrides the API root (default https://api.github.com);
	// tests point it at an httptest.Server. Never logged.
	BaseURL string
	// HTTP is the client used for requests (default http.DefaultClient);
	// injectable for tests.
	HTTP *http.Client
	// Warn, when set, receives a one-time degradation warning if a created
	// request will not trigger CI. The coordination layer wires this to
	// its annotation sink; a nil sink discards the warning.
	Warn func(msg string)

	warnOnce sync.Once
}

var _ forge.Forge = (*Provider)(nil)

// ghPull is the subset of GitHub's pull-request JSON the provider reads.
type ghPull struct {
	Number    int       `json:"number"`
	State     string    `json:"state"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	MergedAt  *string   `json:"merged_at"`
	Mergeable *bool     `json:"mergeable"`
	Head      ghRef     `json:"head"`
	Base      ghRef     `json:"base"`
	Labels    []ghLabel `json:"labels"`
	User      ghUser    `json:"user"`
}

type ghRef struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type ghLabel struct {
	Name string `json:"name"`
}

type ghUser struct {
	Login string `json:"login"`
}

// ListManagedRequests returns the managed change requests for the graph
// and type in filter, in the requested state. Discovery keeps only pull
// requests whose body marker parses, is version v1, whose head/base
// branches equal the marker's source/destination, and that carry the
// oiax label — title is never consulted.
func (p *Provider) ListManagedRequests(ctx context.Context, filter forge.RequestFilter) ([]engine.ChangeRequest, error) {
	state := "open"
	if filter.State == forge.RequestStateMerged {
		state = "closed"
	}
	next := p.url(fmt.Sprintf("/repos/%s/%s/pulls?state=%s&per_page=100",
		url.PathEscape(p.Owner), url.PathEscape(p.Repo), state))

	var out []engine.ChangeRequest
	for next != "" {
		var pulls []ghPull
		hdr, err := p.do(ctx, http.MethodGet, next, nil, &pulls)
		if err != nil {
			return nil, fmt.Errorf("list managed requests: %w", err)
		}
		for _, pr := range pulls {
			// Merged discovery asks GitHub for closed PRs; keep only the
			// ones that actually merged.
			if filter.State == forge.RequestStateMerged && pr.MergedAt == nil {
				continue
			}
			m, ok := managedMarker(pr)
			if !ok {
				continue
			}
			if m.Graph != filter.Graph {
				continue
			}
			if filter.Type != "" && m.Type != string(filter.Type) {
				continue
			}
			out = append(out, changeRequest(pr, m))
		}
		next = nextLink(hdr.Get("Link"))
	}
	return out, nil
}

// CreateRequest opens a managed request with the marker appended to the
// body and the default labels attached. An HTTP 422 (GitHub refusing a
// duplicate head/base pair) is adopted as success: the provider re-lists
// and returns the surviving request instead of erroring.
func (p *Provider) CreateRequest(ctx context.Context, req forge.CreateRequest) (engine.ChangeRequest, error) {
	m := marker{
		Version:     markerVersion,
		Graph:       req.Graph,
		Type:        string(req.Type),
		Source:      req.Source,
		Destination: req.Target,
		SourceHead:  req.SourceHead,
	}
	body := req.Body + "\n\n" + serializeMarker(m)

	payload := map[string]string{
		"title": req.Title,
		"head":  req.Source,
		"base":  req.Target,
		"body":  body,
	}
	var created ghPull
	_, err := p.do(ctx, http.MethodPost,
		p.url(fmt.Sprintf("/repos/%s/%s/pulls", url.PathEscape(p.Owner), url.PathEscape(p.Repo))),
		payload, &created)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.StatusCode == http.StatusUnprocessableEntity {
			adopted, aerr := p.adoptDuplicate(ctx, req)
			if aerr != nil {
				return engine.ChangeRequest{}, fmt.Errorf("create request: adopt duplicate: %w", errors.Join(err, aerr))
			}
			if adopted != nil {
				return *adopted, nil
			}
		}
		return engine.ChangeRequest{}, fmt.Errorf("create request: %w", err)
	}

	if err := p.addLabels(ctx, created.Number, LabelOiax, typeLabel(req.Type)); err != nil {
		return engine.ChangeRequest{}, fmt.Errorf("create request: %w", err)
	}

	// Observable degradation signal: a PR authored by the token bot will
	// not run CI. This is a fact about the created request, not a guess
	// about the token type.
	if created.User.Login == botLogin {
		p.warn(degradationWarning)
	}

	return engine.ChangeRequest{
		ID:         strconv.Itoa(created.Number),
		Type:       req.Type,
		Source:     req.Source,
		Target:     req.Target,
		SourceHead: req.SourceHead,
	}, nil
}

// UpdateRequest rewrites the recorded sourceHead in a managed request's
// marker, leaving the human body text intact. It refuses any request that
// is not a valid v1 managed request.
func (p *Provider) UpdateRequest(ctx context.Context, req forge.UpdateRequest) error {
	num, err := prNumber(string(req.ID))
	if err != nil {
		return fmt.Errorf("update request: %w", err)
	}
	pr, err := p.getPull(ctx, num)
	if err != nil {
		return fmt.Errorf("update request %s: %w", req.ID, err)
	}
	m, ok := managedMarker(pr)
	if !ok {
		return fmt.Errorf("update request %s: not a managed request", req.ID)
	}
	m.SourceHead = req.SourceHead
	newBody, ok := replaceMarker(pr.Body, m)
	if !ok {
		return fmt.Errorf("update request %s: marker not found in body", req.ID)
	}
	_, err = p.do(ctx, http.MethodPatch, p.pullURL(num), map[string]string{"body": newBody}, nil)
	if err != nil {
		return fmt.Errorf("update request %s: %w", req.ID, err)
	}
	return nil
}

// CloseRequest closes an obsolete managed request. It refuses to touch a
// request that is not a valid v1 managed request, comments the reason
// before closing, and never deletes.
func (p *Provider) CloseRequest(ctx context.Context, id forge.RequestID, reason forge.Reason) error {
	num, err := prNumber(string(id))
	if err != nil {
		return fmt.Errorf("close request: %w", err)
	}
	pr, err := p.getPull(ctx, num)
	if err != nil {
		return fmt.Errorf("close request %s: %w", id, err)
	}
	if _, ok := managedMarker(pr); !ok {
		return fmt.Errorf("close request %s: not a managed request", id)
	}
	commentURL := p.url(fmt.Sprintf("/repos/%s/%s/issues/%d/comments",
		url.PathEscape(p.Owner), url.PathEscape(p.Repo), num))
	if _, err := p.do(ctx, http.MethodPost, commentURL, map[string]string{"body": reason.Summary}, nil); err != nil {
		return fmt.Errorf("close request %s: comment: %w", id, err)
	}
	if _, err := p.do(ctx, http.MethodPatch, p.pullURL(num), map[string]string{"state": "closed"}, nil); err != nil {
		return fmt.Errorf("close request %s: %w", id, err)
	}
	return nil
}

// PushBranch refuses any branch name outside the oiax/ namespace before
// touching git. The actual push is v0.2 scope (backflow); in v0.1 no
// planned action emits one, so the confined push returns
// forge.ErrNotImplemented while the namespace guard is real.
func (p *Provider) PushBranch(ctx context.Context, push forge.BranchPush) error {
	if !strings.HasPrefix(push.Name, "oiax/") {
		return fmt.Errorf("push branch %q: refused outside the oiax/ namespace", push.Name)
	}
	return forge.ErrNotImplemented
}

// adoptDuplicate re-lists open managed requests for the same graph and
// type and returns the one whose source/destination match req, if any.
func (p *Provider) adoptDuplicate(ctx context.Context, req forge.CreateRequest) (*engine.ChangeRequest, error) {
	existing, err := p.ListManagedRequests(ctx, forge.RequestFilter{Graph: req.Graph, Type: req.Type})
	if err != nil {
		return nil, err
	}
	for i := range existing {
		if existing[i].Source == req.Source && existing[i].Target == req.Target {
			return &existing[i], nil
		}
	}
	return nil, nil
}

// addLabels attaches labels to a request (a PR is an issue for the labels
// endpoint).
func (p *Provider) addLabels(ctx context.Context, number int, labels ...string) error {
	labelURL := p.url(fmt.Sprintf("/repos/%s/%s/issues/%d/labels",
		url.PathEscape(p.Owner), url.PathEscape(p.Repo), number))
	if _, err := p.do(ctx, http.MethodPost, labelURL, map[string][]string{"labels": labels}, nil); err != nil {
		return fmt.Errorf("add labels: %w", err)
	}
	return nil
}

// getPull fetches a single pull request by number.
func (p *Provider) getPull(ctx context.Context, number int) (ghPull, error) {
	var pr ghPull
	if _, err := p.do(ctx, http.MethodGet, p.pullURL(number), nil, &pr); err != nil {
		return ghPull{}, err
	}
	return pr, nil
}

// warn delivers the degradation warning at most once and only when a sink
// is configured.
func (p *Provider) warn(msg string) {
	p.warnOnce.Do(func() {
		if p.Warn != nil {
			p.Warn(msg)
		}
	})
}

func (p *Provider) pullURL(number int) string {
	return p.url(fmt.Sprintf("/repos/%s/%s/pulls/%d",
		url.PathEscape(p.Owner), url.PathEscape(p.Repo), number))
}

func (p *Provider) url(path string) string {
	base := p.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	return base + path
}

func (p *Provider) httpClient() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return http.DefaultClient
}

// do issues one REST call. It sets auth and API headers, JSON-encodes in
// (when non-nil), decodes a 2xx body into out (when non-nil), and turns a
// non-2xx response into an *apiError carrying GitHub's message and the
// status code — never the token. It returns the response header so
// callers can follow pagination Link headers.
func (p *Provider) do(ctx context.Context, method, urlStr string, in, out any) (http.Header, error) {
	var reqBody io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, urlStr, reqBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.Token)
	req.Header.Set("Accept", acceptHeader)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.Header, parseAPIError(resp)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.Header, fmt.Errorf("decode response: %w", err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return resp.Header, nil
}

// apiError is a non-2xx GitHub response. It carries the status code so
// callers can branch (e.g. adopt a 422 duplicate) with errors.As, and
// GitHub's human message — never the credential.
type apiError struct {
	StatusCode int
	Message    string
	Details    []string
}

func (e *apiError) Error() string {
	if len(e.Details) > 0 {
		return fmt.Sprintf("github api %d: %s (%s)", e.StatusCode, e.Message, strings.Join(e.Details, "; "))
	}
	return fmt.Sprintf("github api %d: %s", e.StatusCode, e.Message)
}

// parseAPIError decodes GitHub's {message, errors[]} error envelope. A
// body that does not decode still yields a status-only error.
func parseAPIError(resp *http.Response) error {
	var env struct {
		Message string `json:"message"`
		Errors  []struct {
			Resource string `json:"resource"`
			Field    string `json:"field"`
			Code     string `json:"code"`
			Message  string `json:"message"`
		} `json:"errors"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = json.Unmarshal(data, &env)
	ae := &apiError{StatusCode: resp.StatusCode, Message: env.Message}
	if ae.Message == "" {
		ae.Message = http.StatusText(resp.StatusCode)
	}
	for _, d := range env.Errors {
		if d.Message != "" {
			ae.Details = append(ae.Details, d.Message)
		} else if d.Field != "" {
			ae.Details = append(ae.Details, d.Field+": "+d.Code)
		}
	}
	return ae
}

// managedMarker reports whether pr is a valid managed request and returns
// its marker. Identity is: marker present and parses, version v1, head
// branch == source, base branch == destination, and the oiax label
// present. Any failure means unmanaged — the caller must not touch it.
func managedMarker(pr ghPull) (marker, bool) {
	m, ok := parseMarker(pr.Body)
	if !ok || m.Version != markerVersion {
		return marker{}, false
	}
	if pr.Head.Ref != m.Source || pr.Base.Ref != m.Destination {
		return marker{}, false
	}
	if !hasLabel(pr, LabelOiax) {
		return marker{}, false
	}
	return m, true
}

func hasLabel(pr ghPull, name string) bool {
	for _, l := range pr.Labels {
		if l.Name == name {
			return true
		}
	}
	return false
}

// changeRequest maps a managed pull request and its marker to the
// engine's provider-neutral view. The marker's destination becomes the
// engine's Target.
func changeRequest(pr ghPull, m marker) engine.ChangeRequest {
	return engine.ChangeRequest{
		ID:         strconv.Itoa(pr.Number),
		Type:       engine.RequestType(m.Type),
		Source:     m.Source,
		Target:     m.Destination,
		SourceHead: m.SourceHead,
	}
}

// prNumber parses a RequestID into a positive GitHub PR number. Atoi both
// converts and guards the value against path injection.
func prNumber(id string) (int, error) {
	n, err := strconv.Atoi(id)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid request id %q", id)
	}
	return n, nil
}

// nextLink extracts the rel="next" URL from a GitHub Link header, or ""
// when there is no next page.
func nextLink(header string) string {
	if header == "" {
		return ""
	}
	for _, part := range strings.Split(header, ",") {
		segs := strings.Split(strings.TrimSpace(part), ";")
		if len(segs) < 2 {
			continue
		}
		raw := strings.TrimSpace(segs[0])
		if !strings.HasPrefix(raw, "<") || !strings.HasSuffix(raw, ">") {
			continue
		}
		for _, param := range segs[1:] {
			if strings.TrimSpace(param) == `rel="next"` {
				return raw[1 : len(raw)-1]
			}
		}
	}
	return ""
}
