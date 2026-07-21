// Package github implements the forge.Forge interface against the GitHub
// REST API. It is the first supported provider (v0.1 roadmap).
//
// Provider notes that shape this implementation:
//
//   - Managed requests are identified by the machine-readable marker in
//     the request body plus the branch relationship it declares (head ==
//     source, base == destination), never by title or label. The oiax
//     label is decorative: it is written for humans, but a human removing
//     it must not make oiax lose track of its own request. Recognition is
//     forward-compatible — a marker written by a newer release is still
//     identified as managed, so an older oiax never opens a duplicate.
//     Unmanaged requests between the same branches are never touched.
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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
)

const (
	defaultBaseURL = "https://api.github.com"
	acceptHeader   = "application/vnd.github+json"
	apiVersion     = "2022-11-28"
	// gitRemoteHost is the host of the authenticated push remote built when
	// GitRemote is not injected.
	gitRemoteHost = "github.com"
	// tokenRedaction replaces the credential in any git output surfaced in an
	// error, mirroring the assertNoToken guarantee for the REST surface.
	tokenRedaction = "[redacted]"
	// botLogin is the author of pull requests created with the default
	// GITHUB_TOKEN; it is exactly the condition under which
	// `on: pull_request` workflows do not fire.
	botLogin = "github-actions[bot]"
	// degradationWarning is emitted once when a created request will not
	// trigger CI.
	degradationWarning = "created pull request is authored by " + botLogin +
		"; on: pull_request workflows will not run for it. Configure a GitHub App " +
		"installation token so managed requests get CI."

	// requestTimeout bounds a single HTTP attempt so a stalled connection
	// cannot hang a reconcile. The request context still cancels the call
	// (http.NewRequestWithContext); this is the backstop when nothing upstream
	// set a deadline.
	requestTimeout = 30 * time.Second
	// defaultRetryMax caps the number of retries for a safe request (attempts =
	// the initial call plus up to this many retries).
	defaultRetryMax = 4
	// defaultRetryBackoff is the base of the exponential backoff between
	// retries; the actual wait grows to maxRetryBackoff and carries jitter.
	defaultRetryBackoff = 500 * time.Millisecond
	// maxRetryBackoff caps a single computed backoff interval.
	maxRetryBackoff = 30 * time.Second
	// maxServerBackoff caps how long a server-provided Retry-After /
	// X-RateLimit-Reset can make us wait, so a hostile or absurd header cannot
	// stall a reconcile indefinitely (context cancellation still wins).
	maxServerBackoff = 2 * time.Minute
	// mergedLookback bounds how far back merged-request discovery pages closed
	// history; see ListManagedRequests.
	mergedLookback = 180 * 24 * time.Hour
	// defaultMaxRespBytes caps a success-path response body so a compromised or
	// malfunctioning API cannot OOM the process. Generous: a full page of 100
	// pull requests with long bodies is a few MB.
	defaultMaxRespBytes = 32 << 20
)

// oidPattern matches a git object id (or an unambiguous abbreviation). A
// commit id pushed to a ref reaches git as data and is guarded with this
// before use, so a branch name can never masquerade as a revision.
var oidPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

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
	// HTTP is the client used for requests (default: a shared client carrying a
	// request timeout, defaultHTTPClient); injectable for tests.
	HTTP *http.Client
	// GitDir is the working directory PushBranch runs git from. It must
	// share an object database with the commit being pushed (the ephemeral
	// cherry-pick worktree's repository). Empty means the current directory.
	// It is a path, not a credential — safe to log.
	GitDir string
	// GitRemote overrides the push remote (default: the GitHub https URL built
	// from Owner/Repo; the credential is supplied via http.extraHeader in the
	// environment, not the URL). Tests point it at a local bare repository so
	// the push touches no network and carries no token. When it embeds a
	// credential it is never logged.
	GitRemote string
	// Warn, when set, receives a one-time degradation warning if a created
	// request will not trigger CI. The coordination layer wires this to
	// its annotation sink; a nil sink discards the warning.
	Warn func(msg string)

	warnOnce sync.Once

	// repoSettingsCache holds the first successfully decoded repository
	// settings object, so every settings reader in a run shares one GET.
	// Failures are never cached: settings warnings are advisory, and a
	// transient error (or a cancelled context) must not silence them for
	// the rest of the process.
	repoSettingsMu    sync.Mutex
	repoSettingsCache *repoSettings

	// Resilience tunables. Zero values use the production defaults above; they
	// exist only so tests can shrink backoff and the response cap without a
	// process-global. They are not part of the public contract.
	retryMax     int
	retryBackoff time.Duration
	maxRespBytes int64
}

var _ forge.Forge = (*Provider)(nil)

// ghPull is the subset of GitHub's pull-request JSON the provider reads.
type ghPull struct {
	Number    int       `json:"number"`
	State     string    `json:"state"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	MergedAt  *string   `json:"merged_at"`
	CreatedAt string    `json:"created_at"`
	Mergeable *bool     `json:"mergeable"`
	Head      ghRef     `json:"head"`
	Base      ghRef     `json:"base"`
	Labels    []ghLabel `json:"labels"`
	User      ghUser    `json:"user"`
}

type ghRef struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
	// Repo is the repository the branch lives in. GitHub sets it from
	// server-side state (never the PR author), so it is the provenance signal
	// managedMarker uses to tell a same-repo request from a fork PR.
	Repo ghRepo `json:"repo"`
}

type ghRepo struct {
	FullName string `json:"full_name"`
}

type ghLabel struct {
	Name string `json:"name"`
}

type ghUser struct {
	Login string `json:"login"`
}

// ghIssue is the subset of GitHub's issue JSON the provider reads.
// GitHub's /issues route returns PRs too; PullRequest is non-nil exactly
// when this issue IS a pull request, and such entries are dropped.
type ghIssue struct {
	Number      int       `json:"number"`
	State       string    `json:"state"`
	Body        string    `json:"body"`
	Labels      []ghLabel `json:"labels"`
	PullRequest *struct{} `json:"pull_request"`
}

// ListManagedRequests returns the managed change requests for the graph
// and type in filter, in the requested state. Discovery keeps only pull
// requests whose body marker parses, carries a recognized version (any
// vN), and whose head/base branches equal the marker's source/destination
// — title and label are never consulted.
func (p *Provider) ListManagedRequests(ctx context.Context, filter forge.RequestFilter) ([]engine.ChangeRequest, error) {
	state := "open"
	if filter.State == forge.RequestStateMerged {
		state = "closed"
	}
	// Sort and direction are explicit rather than relying on GitHub's default
	// (M13). The baseline rung consumes the NEWEST merged managed request per
	// edge, and the reconcile layer's matchRequest takes the first match in list
	// order — so discovery must return newest-first. Open discovery sorts by
	// created&desc (harmless: oiax allows at most one open request per edge, so
	// order cannot matter there). Merged discovery sorts by updated&desc instead
	// of created&desc: GitHub cannot sort by merged_at directly, but a merged
	// PR's updated_at is set at merge time (and only moves later, from further
	// activity), which stays close to merge recency even when created_at does
	// not — a promotion that sat through a long review keeps an old created_at
	// long after it merges. created&desc would rank that survivor by when review
	// started rather than when it landed, which is exactly what pageOlderThan's
	// cutoff below must not do.
	sortField := "created"
	if filter.State == forge.RequestStateMerged {
		sortField = "updated"
	}
	next := p.url(fmt.Sprintf("/repos/%s/%s/pulls?state=%s&per_page=100&sort=%s&direction=desc",
		url.PathEscape(p.Owner), url.PathEscape(p.Repo), state, sortField))

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
		// Bound merged discovery so it does not page the entire closed-PR
		// history every run (M4). Results are newest-updated-first, which tracks
		// merge recency (see the sort rationale above); once a page's oldest
		// entry's own merged_at predates the lookback window, every request on
		// any later page was updated even longer ago, so no more-recently-merged
		// baseline for any edge can be hiding there — stop. Gating on merged_at
		// here (not created_at) is what makes that true: a request created long
		// ago but merged recently — a long review cycle — sorts near the front by
		// updated_at and is seen well before this cutoff fires. DECISION: an edge
		// whose last promotion actually merged longer ago than mergedLookback
		// loses its recorded baseline optimization; the other equivalence rungs
		// still detect the promotion, and the window is generous, so this trades
		// a dormant-edge shortcut for a bounded scan.
		if filter.State == forge.RequestStateMerged && pageOlderThan(pulls, mergedLookback) {
			break
		}
		next = nextLink(hdr.Get("Link"))
		// The bearer token rides on every request, including the one that
		// follows the next-page link. Never follow pagination to a host the API
		// base did not vouch for, or the token would be sent to a redirected
		// origin (L2).
		if next != "" && !p.sameOrigin(next) {
			return nil, fmt.Errorf("list managed requests: refusing cross-origin pagination to %q", next)
		}
	}
	return out, nil
}

// pageOlderThan reports whether every request on a newest-updated-first page
// predates the window — true when the oldest (last) entry's merged_at is
// beyond it. Gating on merged_at (not created_at) is required for
// correctness: a request opened long ago but merged recently — a long review
// cycle — must not be mistaken for stale just because it is old by creation.
// An entry that has not merged, or whose timestamp is unparseable, is treated
// as recent (do not early-exit), so a missing field can never truncate
// discovery.
func pageOlderThan(pulls []ghPull, window time.Duration) bool {
	if len(pulls) == 0 {
		return false
	}
	oldest := pulls[len(pulls)-1].MergedAt
	if oldest == nil {
		return false
	}
	t, err := time.Parse(time.RFC3339, *oldest)
	if err != nil {
		return false
	}
	return time.Since(t) > window
}

// sameOrigin reports whether rawURL has the same scheme and host as the API
// base. Pagination links are server-controlled, so a followed link is confined
// to the API origin before the credential-bearing request goes out (L2).
func (p *Provider) sameOrigin(rawURL string) bool {
	base, err := url.Parse(p.baseURL())
	if err != nil {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return u.Scheme == base.Scheme && u.Host == base.Host
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
	// Reject a marker value that could forge marker lines or break out of the
	// HTML comment before it is ever written to the forge (M14).
	if err := validateMarker(m); err != nil {
		return engine.ChangeRequest{}, fmt.Errorf("create request: %w", err)
	}
	body := req.Body + "\n\n" + serializeMarker(m)

	payload := map[string]string{
		"title": req.Title,
		"head":  req.Source,
		"base":  req.Target,
		"body":  body,
	}
	var created ghPull
	// The create POST is retried on transient failures (M3): it is safely
	// idempotent because GitHub rejects a duplicate head/base pair with 422,
	// which the adopt path below turns into success — so a retry that races a
	// first attempt that actually landed re-lists and adopts rather than
	// double-opening.
	_, err := p.send(ctx, http.MethodPost,
		p.url(fmt.Sprintf("/repos/%s/%s/pulls", url.PathEscape(p.Owner), url.PathEscape(p.Repo))),
		payload, &created, true)
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
// marker, leaving the human body text intact. It refuses a request that is
// not managed, or whose marker version this build does not support
// (rewriting an unknown schema could drop fields it cannot see).
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
	if !understoodMarker(m) {
		return fmt.Errorf("update request %s: marker version %q is not supported by this build; upgrade oiax", req.ID, m.Version)
	}
	m.SourceHead = req.SourceHead
	// The rewritten sourceHead must not be able to forge marker lines or close
	// the comment (M14); reject before the marker is re-serialized.
	if err := validateMarker(m); err != nil {
		return fmt.Errorf("update request %s: %w", req.ID, err)
	}
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
// request that is not managed, or whose marker version this build does
// not support; it comments the reason before closing, and never
// deletes.
func (p *Provider) CloseRequest(ctx context.Context, id forge.RequestID, reason forge.Reason) error {
	num, err := prNumber(string(id))
	if err != nil {
		return fmt.Errorf("close request: %w", err)
	}
	pr, err := p.getPull(ctx, num)
	if err != nil {
		return fmt.Errorf("close request %s: %w", id, err)
	}
	m, ok := managedMarker(pr)
	if !ok {
		return fmt.Errorf("close request %s: not a managed request", id)
	}
	if !understoodMarker(m) {
		return fmt.Errorf("close request %s: marker version %q is not supported by this build; upgrade oiax", id, m.Version)
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

// ListConflictArtifacts returns the open durable conflict artifacts for
// graph, sorted ascending by issue number. Discovery keeps only issues
// carrying both required labels and a well-formed conflict marker whose
// graph matches; PR entries the /issues route returns are dropped. The
// ascending sort makes the reconcile layer's lowest-numbered-canonical
// duplicate-consolidation rule deterministic.
func (p *Provider) ListConflictArtifacts(ctx context.Context, graph string) ([]forge.ConflictArtifact, error) {
	next := p.url(fmt.Sprintf("/repos/%s/%s/issues?labels=%s,%s&state=open&per_page=100",
		url.PathEscape(p.Owner), url.PathEscape(p.Repo),
		url.QueryEscape(LabelOiax), url.QueryEscape(LabelConflict)))

	var issues []ghIssue
	for next != "" {
		var page []ghIssue
		hdr, err := p.do(ctx, http.MethodGet, next, nil, &page)
		if err != nil {
			return nil, fmt.Errorf("list conflict artifacts: %w", err)
		}
		for _, iss := range page {
			m, ok := conflictMarker(iss)
			if !ok {
				continue
			}
			if m.Graph != graph {
				continue
			}
			issues = append(issues, iss)
		}
		next = nextLink(hdr.Get("Link"))
		// The bearer token rides on the next-page request too; never follow a
		// server-supplied pagination link off the API origin (L2).
		if next != "" && !p.sameOrigin(next) {
			return nil, fmt.Errorf("list conflict artifacts: refusing cross-origin pagination to %q", next)
		}
	}
	// The lowest-numbered issue is the canonical artifact for its edge; the
	// caller relies on ascending order to pick it deterministically.
	sort.Slice(issues, func(i, j int) bool { return issues[i].Number < issues[j].Number })

	out := make([]forge.ConflictArtifact, 0, len(issues))
	for _, iss := range issues {
		m, _ := conflictMarker(iss)
		out = append(out, forge.ConflictArtifact{
			ID:         forge.ConflictArtifactID(strconv.Itoa(iss.Number)),
			Source:     m.Source,
			Target:     m.Destination,
			SourceHead: m.SourceHead,
		})
	}
	return out, nil
}

// CreateConflictArtifact opens a durable conflict artifact: an issue
// carrying the marker (type: conflict) appended to the body and both the
// oiax and oiax/conflict labels applied through the labels endpoint after
// creation — NOT inline in the create payload. GitHub silently drops the
// `labels` field of a create-issue request made by an actor without push
// access ("Only users with push access can set labels for new issues"),
// and the reconcile-time App token that runs oiax deliberately holds only
// issues:write, not contents:write. The labels are load-bearing for
// artifact identity (ADR 0008: an issue has no head/base provenance, so
// ListConflictArtifacts filters on them), so a dropped label makes every
// run mint a fresh unlabeled duplicate. addLabels POSTs to the issue's
// /labels route, which requires only issues:write (triage) — exactly how
// CreateRequest labels a managed PR. It deliberately does NOT re-list and
// collapse duplicates — the reconcile layer consolidates on every run (the
// single, always-runs consolidation point), so a create-time collapse
// would be a second, windowed, best-effort mechanism.
func (p *Provider) CreateConflictArtifact(ctx context.Context, spec forge.ConflictArtifactSpec) (forge.ConflictArtifact, error) {
	m := marker{
		Version:     markerVersion,
		Graph:       spec.Graph,
		Type:        conflictMarkerType,
		Source:      spec.Source,
		Destination: spec.Target,
		SourceHead:  spec.SourceHead,
	}
	// Reject a marker value that could forge marker lines or break out of the
	// HTML comment before it is ever written to the forge (M14).
	if err := validateMarker(m); err != nil {
		return forge.ConflictArtifact{}, fmt.Errorf("create conflict artifact: %w", err)
	}
	body := spec.Body + "\n\n" + serializeMarker(m)

	payload := map[string]any{
		"title": spec.Title,
		"body":  body,
	}
	var created ghIssue
	_, err := p.do(ctx, http.MethodPost,
		p.url(fmt.Sprintf("/repos/%s/%s/issues", url.PathEscape(p.Owner), url.PathEscape(p.Repo))),
		payload, &created)
	if err != nil {
		return forge.ConflictArtifact{}, fmt.Errorf("create conflict artifact: %w", err)
	}
	// Apply the identity labels via the triage-only route. A failure here
	// leaves an unlabeled issue ListConflictArtifacts cannot find, so surface
	// it rather than return a "created" artifact the next run would duplicate;
	// the caller (recordBackflowConflict) treats the error as best-effort and
	// retakes the whole record path on the next run. Before surfacing, close
	// the just-created issue best-effort: a *persisting* labeling failure
	// (revoked triage permission, say) would otherwise mint one OPEN unlabeled
	// issue per run — the accumulation this method exists to prevent. Issues
	// cannot be deleted through the REST API, so closed-not_planned is the
	// strongest cleanup available; a failure of the cleanup itself is
	// swallowed so the labeling error stays the one surfaced.
	if err := p.addLabels(ctx, created.Number, LabelOiax, LabelConflict); err != nil {
		_, _ = p.do(ctx, http.MethodPatch, p.issueURL(created.Number),
			map[string]string{"state": "closed", "state_reason": "not_planned"}, nil)
		return forge.ConflictArtifact{}, fmt.Errorf("create conflict artifact: %w", err)
	}
	return forge.ConflictArtifact{
		ID:         forge.ConflictArtifactID(strconv.Itoa(created.Number)),
		Source:     spec.Source,
		Target:     spec.Target,
		SourceHead: spec.SourceHead,
	}, nil
}

// UpdateConflictArtifact refreshes an existing conflict artifact in place:
// it rewrites the whole body (the human failing-commit text and the marker
// with the new sourceHead). It refuses an issue that is not a conflict
// artifact, or whose marker version this build does not understand. No
// comment is posted (deliberate: comments only on close/consolidate, to
// avoid notification spam).
func (p *Provider) UpdateConflictArtifact(ctx context.Context, id forge.ConflictArtifactID, spec forge.ConflictArtifactSpec) error {
	num, err := issueNumber(string(id))
	if err != nil {
		return fmt.Errorf("update conflict artifact: %w", err)
	}
	iss, err := p.getIssue(ctx, num)
	if err != nil {
		return fmt.Errorf("update conflict artifact %s: %w", id, err)
	}
	m, ok := conflictMarker(iss)
	if !ok {
		return fmt.Errorf("update conflict artifact %s: not a conflict artifact", id)
	}
	if !understoodMarker(m) {
		return fmt.Errorf("update conflict artifact %s: marker version %q is not supported by this build; upgrade oiax", id, m.Version)
	}
	m = marker{
		Version:     markerVersion,
		Graph:       spec.Graph,
		Type:        conflictMarkerType,
		Source:      spec.Source,
		Destination: spec.Target,
		SourceHead:  spec.SourceHead,
	}
	if err := validateMarker(m); err != nil {
		return fmt.Errorf("update conflict artifact %s: %w", id, err)
	}
	newBody := spec.Body + "\n\n" + serializeMarker(m)
	if _, err := p.do(ctx, http.MethodPatch, p.issueURL(num), map[string]string{"body": newBody}, nil); err != nil {
		return fmt.Errorf("update conflict artifact %s: %w", id, err)
	}
	return nil
}

// CloseConflictArtifact closes a resolved conflict artifact: it comments
// the reason, then closes the issue with state_reason: completed. It
// refuses an issue that is not a conflict artifact, or whose marker
// version this build does not understand, and never deletes. Close targets
// /issues/{n} (an issue, not a pull request).
func (p *Provider) CloseConflictArtifact(ctx context.Context, id forge.ConflictArtifactID, reason forge.Reason) error {
	num, err := issueNumber(string(id))
	if err != nil {
		return fmt.Errorf("close conflict artifact: %w", err)
	}
	iss, err := p.getIssue(ctx, num)
	if err != nil {
		return fmt.Errorf("close conflict artifact %s: %w", id, err)
	}
	m, ok := conflictMarker(iss)
	if !ok {
		return fmt.Errorf("close conflict artifact %s: not a conflict artifact", id)
	}
	if !understoodMarker(m) {
		return fmt.Errorf("close conflict artifact %s: marker version %q is not supported by this build; upgrade oiax", id, m.Version)
	}
	commentURL := p.url(fmt.Sprintf("/repos/%s/%s/issues/%d/comments",
		url.PathEscape(p.Owner), url.PathEscape(p.Repo), num))
	if _, err := p.do(ctx, http.MethodPost, commentURL, map[string]string{"body": reason.Summary}, nil); err != nil {
		return fmt.Errorf("close conflict artifact %s: comment: %w", id, err)
	}
	if _, err := p.do(ctx, http.MethodPatch, p.issueURL(num),
		map[string]string{"state": "closed", "state_reason": "completed"}, nil); err != nil {
		return fmt.Errorf("close conflict artifact %s: %w", id, err)
	}
	return nil
}

// PushBranch pushes push.SHA to refs/heads/<push.Name>, confined to the
// oiax/ namespace: any name outside oiax/ is refused before git is touched,
// so force-pushing can never escape the namespace Oiax owns.
//
// The GitHub REST refs API cannot upload git objects, so the push shells
// out to git following the no-shell posture: arguments are passed as exec
// args (never a shell string), the branch name is validated with
// git check-ref-format, the commit id is guarded with oidPattern, and both
// reach git as operands after --end-of-options. The push runs in GitDir,
// which must share an object database with the commit.
//
// The remote is GitRemote when set (tests point it at a local bare repo),
// otherwise the GitHub https URL built from Owner/Repo. The credential is NOT
// in the URL: it is supplied via http.extraHeader in the environment
// (pushAuthEnv) so the token never reaches argv or the process table. The
// credential never appears in a returned error either: git's output is scrubbed
// of the token before it is surfaced. push.Force selects a force push (the
// determinism strategy: the same source head yields the same branch, so
// re-pushing is idempotent).
func (p *Provider) PushBranch(ctx context.Context, push forge.BranchPush) error {
	if !strings.HasPrefix(push.Name, "oiax/") {
		return fmt.Errorf("push branch %q: refused outside the oiax/ namespace", push.Name)
	}
	if !oidPattern.MatchString(push.SHA) {
		return fmt.Errorf("push branch %q: invalid commit id", push.Name)
	}
	if err := p.checkRefFormat(ctx, push.Name); err != nil {
		return err
	}

	args := []string{"push"}
	if push.Force {
		args = append(args, "--force")
	}
	// Everything after --end-of-options is an operand, never a flag: the
	// remote (trusted config) and the refspec (a hex id + a validated ref)
	// reach git as data.
	args = append(args, "--end-of-options", p.gitRemote(), push.SHA+":refs/heads/"+push.Name)

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = p.GitDir
	// Never block on an interactive credential prompt (e.g. a bad token):
	// fail fast instead of hanging.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	// The credential travels via git config in the environment (http.extraHeader),
	// never in argv, so the token is not visible in the process table
	// (/proc/<pid>/cmdline) during the push (M1).
	cmd.Env = append(cmd.Env, p.pushAuthEnv()...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("push branch %q: %w: %s", push.Name, err, p.scrubToken(msg))
		}
		return fmt.Errorf("push branch %q: %w", push.Name, err)
	}
	return nil
}

// DeleteBranch deletes refs/heads/<name>, confined to the oiax/ namespace: any
// name outside oiax/ is refused before the API is touched, so deletion can
// never escape the namespace Oiax owns. It removes the head branch left behind
// when a managed backflow request is superseded or closed. A branch that is
// already gone (GitHub answers 404, or 422 for an unprocessable ref) is treated
// as success, keeping the reconcile idempotent. The name is validated with git
// check-ref-format and its slashes are preserved as path separators, so the
// multi-segment oiax/backflow/... ref reaches the refs API intact.
func (p *Provider) DeleteBranch(ctx context.Context, name string) error {
	if !strings.HasPrefix(name, "oiax/") {
		return fmt.Errorf("delete branch %q: refused outside the oiax/ namespace", name)
	}
	if err := p.checkRefFormat(ctx, name); err != nil {
		return fmt.Errorf("delete branch %q: %w", name, err)
	}
	// The refs API addresses the branch as git/refs/heads/<name>; the name's
	// slashes are ref-hierarchy separators, not characters to encode, so each
	// segment is percent-escaped individually while the slashes are preserved.
	// check-ref-format already rejects most hostile input, but a ref may still
	// carry a byte like "%" that an unescaped URL path would misread.
	refURL := p.url(fmt.Sprintf("/repos/%s/%s/git/refs/heads/%s",
		url.PathEscape(p.Owner), url.PathEscape(p.Repo), escapeRefPath(name)))
	if _, err := p.do(ctx, http.MethodDelete, refURL, nil, nil); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && refDeleteMeansAbsent(ae) {
			// The branch is already gone (or never existed): idempotent success.
			return nil
		}
		return fmt.Errorf("delete branch %q: %w", name, err)
	}
	return nil
}

// escapeRefPath percent-escapes each slash-separated segment of a git ref name
// for safe use in a URL path, preserving the "/" separators (ref hierarchy, not
// characters to encode).
func escapeRefPath(ref string) string {
	segs := strings.Split(ref, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// refDeleteMeansAbsent reports whether a failed ref delete indicates the ref
// was already absent — an idempotent success — rather than a genuine failure.
// GitHub's git-refs API returns 422 "Reference does not exist" for a missing
// ref (and 404 on some paths); a 422 for any other reason (a validation error,
// a protected ref) is a real failure that must propagate, so the caller never
// closes a managed request while leaving its head branch behind.
func refDeleteMeansAbsent(ae *apiError) bool {
	if ae.StatusCode == http.StatusNotFound {
		return true
	}
	return ae.StatusCode == http.StatusUnprocessableEntity &&
		strings.Contains(strings.ToLower(ae.Message), "does not exist")
}

// checkRefFormat validates a branch name with git before it is used as a
// ref, mirroring the internal/git posture (names reach git as data, not a
// shell). It runs in GitDir. The error carries the name (caller data, never
// a credential), never git's raw output.
func (p *Provider) checkRefFormat(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "git", "check-ref-format", "--branch", name)
	cmd.Dir = p.GitDir
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("invalid branch name %q", name)
	}
	return nil
}

// gitRemote returns the push remote: GitRemote verbatim when injected, else the
// GitHub https URL. The default remote carries NO credential (M1): the token is
// delivered out of band via pushAuthEnv (http.extraHeader in the environment),
// so it never appears in argv or in the remote URL git may echo on failure.
func (p *Provider) gitRemote() string {
	if p.GitRemote != "" {
		return p.GitRemote
	}
	return "https://" + gitRemoteHost + "/" +
		url.PathEscape(p.Owner) + "/" + url.PathEscape(p.Repo) + ".git"
}

// pushAuthEnv returns the git configuration that authenticates a push, delivered
// through the environment (GIT_CONFIG_COUNT/KEY/VALUE) rather than argv so the
// token is never visible in the process table. It sets http.extraHeader with
// HTTP Basic (x-access-token:<token>, base64-encoded), keeping the credential
// out of the remote URL entirely. Basic is the scheme GitHub's git-over-HTTPS
// smart protocol (git-receive-pack/upload-pack against github.com) actually
// accepts for both classic/fine-grained PATs and App installation tokens —
// GitHub's own git-auth-helper tooling (e.g. actions/checkout) authenticates
// git operations the same way. This is a different surface from the REST API
// (doOnce), which does take a Bearer Authorization; a Bearer scheme here is
// rejected with 401. It is empty when the remote is not an http(s) URL (tests
// push to a local bare repo that needs no credential) or no token is set.
func (p *Provider) pushAuthEnv() []string {
	remote := p.gitRemote()
	if p.Token == "" || (!strings.HasPrefix(remote, "https://") && !strings.HasPrefix(remote, "http://")) {
		return nil
	}
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + p.Token))
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.extraHeader",
		"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic " + basic,
	}
}

// scrubToken removes the credential from a string surfaced to callers, so a
// git failure that echoes the remote URL cannot leak the token.
func (p *Provider) scrubToken(s string) string {
	if p.Token == "" {
		return s
	}
	return strings.ReplaceAll(s, p.Token, tokenRedaction)
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
// RepoMergeMethods reads the repository's merge-button settings
// (allow_merge_commit / allow_squash_merge / allow_rebase_merge) so the
// coordinator can warn when a configured mergeMethod contradicts them. It is
// read-only and never changes repository settings.
func (p *Provider) RepoMergeMethods(ctx context.Context) (forge.MergeMethods, error) {
	repo, err := p.repoSettings(ctx)
	if err != nil {
		return forge.MergeMethods{}, fmt.Errorf("read repository merge settings: %w", err)
	}
	return forge.MergeMethods{
		Merge:  repo.AllowMergeCommit,
		Squash: repo.AllowSquashMerge,
		Rebase: repo.AllowRebaseMerge,
	}, nil
}

// RepoDeletesSourceOnMerge reads the repository's delete_branch_on_merge
// setting ("Automatically delete head branches"), which deletes a merged pull
// request's head branch. Oiax opens every promotion request FROM a long-lived
// graph branch, so this setting removes graph branches on merge; the
// coordinator warns on it. Read-only; never changes repository settings.
func (p *Provider) RepoDeletesSourceOnMerge(ctx context.Context) (bool, error) {
	repo, err := p.repoSettings(ctx)
	if err != nil {
		return false, fmt.Errorf("read repository branch settings: %w", err)
	}
	return repo.DeleteBranchOnMerge, nil
}

// repoSettings returns the settings subset of the repository object, GETting
// it at most once per Provider: the first successful read is memoized, so a
// plan that consults merge methods and branch auto-deletion — or checks a
// target's merge methods per edge — costs one request total. Caching for the
// Provider's lifetime is sound because a Provider lives for a single
// reconcile run and every field is advisory input to a warning. Only a
// successful decode is cached; an error (including context cancellation) is
// returned without poisoning later reads.
func (p *Provider) repoSettings(ctx context.Context) (repoSettings, error) {
	p.repoSettingsMu.Lock()
	defer p.repoSettingsMu.Unlock()
	if p.repoSettingsCache != nil {
		return *p.repoSettingsCache, nil
	}
	var repo repoSettings
	u := p.url(fmt.Sprintf("/repos/%s/%s", url.PathEscape(p.Owner), url.PathEscape(p.Repo)))
	if _, err := p.do(ctx, http.MethodGet, u, nil, &repo); err != nil {
		return repoSettings{}, err
	}
	p.repoSettingsCache = &repo
	return repo, nil
}

// repoSettings is the subset of GitHub's repository object Oiax reads. Every
// field is advisory input to a warning; none of them gate reconcile.
type repoSettings struct {
	AllowMergeCommit    bool `json:"allow_merge_commit"`
	AllowSquashMerge    bool `json:"allow_squash_merge"`
	AllowRebaseMerge    bool `json:"allow_rebase_merge"`
	DeleteBranchOnMerge bool `json:"delete_branch_on_merge"`
}

// TargetMergeMethods reports the merge methods permitted for a specific target
// branch: the repository's allow_* settings (via RepoMergeMethods) composed
// with the branch's required-linear-history signal. A branch that requires
// linear history forbids merge commits regardless of the repo-level buttons, a
// blind spot RepoMergeMethods alone cannot see. It is GET-only and never
// mutates. Errors carry GitHub's message and status, never the token.
func (p *Provider) TargetMergeMethods(ctx context.Context, branch string) (forge.MergeMethods, error) {
	methods, err := p.RepoMergeMethods(ctx)
	if err != nil {
		return forge.MergeMethods{}, err
	}
	linear, err := p.requiresLinearHistory(ctx, branch)
	if err != nil {
		return forge.MergeMethods{}, fmt.Errorf("read target merge rules: %w", err)
	}
	methods.RequiresLinearHistory = linear
	return methods, nil
}

// requiresLinearHistory reports whether the target branch forbids merge commits
// via a repository ruleset or classic branch protection. It first consults the
// branch-rules endpoint (repository rulesets) for a "required_linear_history"
// rule, then falls back to classic branch protection's
// required_linear_history.enabled. A missing ruleset or unprotected branch
// answers 404, which means "no such rule", not a failure. The branch name is
// escaped differently per endpoint (see below) so a multi-segment name
// (release/1.x) reaches each API route intact.
func (p *Provider) requiresLinearHistory(ctx context.Context, branch string) (bool, error) {
	// The rules endpoint takes the branch as a trailing catch-all path, so its
	// "/" separators are preserved (escapeRefPath escapes each segment). Classic
	// branch protection takes the branch as a single {branch} path parameter
	// before a fixed /protection suffix, so any "/" must be percent-encoded
	// (url.PathEscape) — a literal slash routes GitHub to a different path and
	// 404s (release/1.x -> .../branches/release/1.x/protection). google/go-github
	// escapes these two endpoints the same two ways.
	rulesBranch := escapeRefPath(branch)
	protBranch := url.PathEscape(branch)

	// Repository rulesets: this endpoint returns every active rule applying to
	// the branch. A "required_linear_history" rule forbids merge commits. The
	// applicable-rules list paginates — layered org + repo rulesets can push the
	// count past a single page — so follow the Link header the way
	// ListManagedRequests does. A single-page read could miss a linear-history
	// rule sitting on page 2 and wrongly clear the merge-commit fence for a
	// branch GitHub will reject at push time.
	rulesURL := p.url(fmt.Sprintf("/repos/%s/%s/rules/branches/%s?per_page=100",
		url.PathEscape(p.Owner), url.PathEscape(p.Repo), rulesBranch))
	for rulesURL != "" {
		var rules []struct {
			Type string `json:"type"`
		}
		hdr, err := p.do(ctx, http.MethodGet, rulesURL, nil, &rules)
		if err != nil {
			if isNotFound(err) {
				break
			}
			return false, err
		}
		for _, r := range rules {
			if r.Type == "required_linear_history" {
				return true, nil
			}
		}
		next := nextLink(hdr.Get("Link"))
		// The bearer token rides on the next-page request too; never follow a
		// server-supplied pagination link off the API origin (L2).
		if next != "" && !p.sameOrigin(next) {
			return false, fmt.Errorf("refusing cross-origin pagination to %q", next)
		}
		rulesURL = next
	}

	// Classic branch protection: required_linear_history.enabled. An unprotected
	// branch answers 404 — no such rule. This endpoint also requires admin
	// rights, so a least-privilege token or App installation is refused with 403
	// ("Resource not accessible by integration") even where merge commits are
	// allowed. That 403 is "cannot read the setting", not "target forbids merge
	// commits", so it is treated as benign like the 404 — the read-scoped rules
	// endpoint above is the authoritative signal, and failing the plan here
	// would break every non-admin caller on a branch that actually permits
	// merges.
	var prot struct {
		RequiredLinearHistory struct {
			Enabled bool `json:"enabled"`
		} `json:"required_linear_history"`
	}
	protURL := p.url(fmt.Sprintf("/repos/%s/%s/branches/%s/protection",
		url.PathEscape(p.Owner), url.PathEscape(p.Repo), protBranch))
	if _, err := p.do(ctx, http.MethodGet, protURL, nil, &prot); err != nil {
		if isNotFound(err) || isForbidden(err) {
			return false, nil
		}
		return false, err
	}
	return prot.RequiredLinearHistory.Enabled, nil
}

// isNotFound reports whether err is a GitHub 404, which the merge-rules read
// treats as "no such rule" rather than a failure. It mirrors the errors.As
// posture of refDeleteMeansAbsent.
func isNotFound(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.StatusCode == http.StatusNotFound
}

// isForbidden reports whether err is a GitHub 403. The classic
// branch-protection read requires admin rights, so a least-privilege caller is
// refused with 403 even when merge commits are permitted; requiresLinearHistory
// treats that "cannot read the setting" the same as a 404 "no such rule" rather
// than failing the merge-method fence. A rate-limit 403 is retried before it
// reaches a caller (see retryableStatus), so a 403 that surfaces here is a
// genuine permission denial, not throttling.
func isForbidden(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.StatusCode == http.StatusForbidden
}

func (p *Provider) getPull(ctx context.Context, number int) (ghPull, error) {
	var pr ghPull
	if _, err := p.do(ctx, http.MethodGet, p.pullURL(number), nil, &pr); err != nil {
		return ghPull{}, err
	}
	return pr, nil
}

// getIssue fetches a single issue by number (the /issues route, not
// /pulls), for the conflict-artifact update/close gates.
func (p *Provider) getIssue(ctx context.Context, number int) (ghIssue, error) {
	var iss ghIssue
	if _, err := p.do(ctx, http.MethodGet, p.issueURL(number), nil, &iss); err != nil {
		return ghIssue{}, err
	}
	return iss, nil
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

func (p *Provider) issueURL(number int) string {
	return p.url(fmt.Sprintf("/repos/%s/%s/issues/%d",
		url.PathEscape(p.Owner), url.PathEscape(p.Repo), number))
}

func (p *Provider) baseURL() string {
	if p.BaseURL == "" {
		return defaultBaseURL
	}
	return p.BaseURL
}

func (p *Provider) url(path string) string {
	return p.baseURL() + path
}

// defaultHTTPClient carries a request timeout so a stalled connection cannot
// hang a reconcile (M2). It is shared: http.Client is safe for concurrent use.
var defaultHTTPClient = &http.Client{Timeout: requestTimeout}

func (p *Provider) httpClient() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return defaultHTTPClient
}

// do issues one REST call with bounded retry for SAFE requests. A GET is always
// safe to retry; other methods are retried only when the caller opts in via
// send (CreateRequest does, because a duplicate create is adopted through the
// 422 path). Non-idempotent mutations — labels, comment, close, marker rewrite —
// go through do with a non-GET method and so are never retried.
func (p *Provider) do(ctx context.Context, method, urlStr string, in, out any) (http.Header, error) {
	return p.send(ctx, method, urlStr, in, out, method == http.MethodGet)
}

// send performs a REST call, retrying transient failures with bounded
// exponential backoff and jitter when retryable is set (M3). It retries on
// transport errors and on 429 / 5xx / rate-limited 403 responses, honoring a
// server-provided Retry-After / X-RateLimit-Reset, and respects context
// cancellation between attempts. A non-retryable request, an exhausted attempt
// budget, or a cancelled context returns the last result unchanged — so a 422
// from a create still reaches the adopt path.
func (p *Provider) send(ctx context.Context, method, urlStr string, in, out any, retryable bool) (http.Header, error) {
	attempts := p.retryMax
	if attempts <= 0 {
		attempts = defaultRetryMax
	}
	for attempt := 0; ; attempt++ {
		hdr, err := p.doOnce(ctx, method, urlStr, in, out)
		if err == nil {
			return hdr, nil
		}
		if !retryable || attempt >= attempts || ctx.Err() != nil {
			return hdr, err
		}
		wait, ok := retryDelay(err, hdr, p.backoff(attempt))
		if !ok {
			return hdr, err
		}
		if serr := sleepCtx(ctx, wait); serr != nil {
			// Context cancelled during backoff: surface the API/transport error
			// rather than the sleep's cancellation, so callers see why the call
			// failed.
			return hdr, err
		}
	}
}

// doOnce issues a single REST call. It sets auth and API headers, JSON-encodes
// in (when non-nil), decodes a 2xx body into out (when non-nil, capped by an
// io.LimitReader so a compromised API cannot OOM the process — L1), and turns a
// non-2xx response into an *apiError carrying GitHub's message and the status
// code — never the token. It returns the response header so callers can follow
// pagination Link headers and read Retry-After / X-RateLimit-Reset.
func (p *Provider) doOnce(ctx context.Context, method, urlStr string, in, out any) (http.Header, error) {
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
		return nil, &errNoResponse{fmt.Errorf("http request: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.Header, parseAPIError(resp)
	}
	if out != nil {
		limit := p.maxRespBytes
		if limit <= 0 {
			limit = defaultMaxRespBytes
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, limit)).Decode(out); err != nil {
			return resp.Header, fmt.Errorf("decode response: %w", err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return resp.Header, nil
}

// backoff returns the base exponential delay for a retry attempt, clamped to
// maxRetryBackoff and spread with full jitter (a uniform value in [d/2, d]) so
// concurrent reconciles do not synchronize on the same schedule.
func (p *Provider) backoff(attempt int) time.Duration {
	base := p.retryBackoff
	if base <= 0 {
		base = defaultRetryBackoff
	}
	d := base << attempt
	if d > maxRetryBackoff || d <= 0 { // d <= 0 guards a shift overflow
		d = maxRetryBackoff
	}
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

// errNoResponse marks a transport-level failure whose round trip did not
// complete, so no HTTP response was received (a stalled or reset connection, a
// DNS failure, a refused connection). It is deliberately distinct from the
// deterministic construction errors that precede the round trip — encoding the
// body, building the request — which fail fast because a retry would fail
// identically, and from a decode failure, where a response with a real 2xx
// status was in fact received but its body did not parse. retryDelay treats
// only errNoResponse as unconditionally transient.
type errNoResponse struct{ err error }

func (e *errNoResponse) Error() string { return e.err.Error() }
func (e *errNoResponse) Unwrap() error { return e.err }

// retryDelay decides whether err is transient and how long to wait before
// retrying. Only errNoResponse — a transport failure whose round trip did not
// complete (a stalled or reset connection, a DNS failure, a refused connection)
// — is unconditionally transient. An *apiError is transient only for 429, 5xx,
// and a 403 that carries rate-limit signals; a server-provided Retry-After /
// X-RateLimit-Reset then wins over the computed backoff. Every other error is
// NOT retried: a deterministic construction error (encoding the body, building
// the request) would fail identically, and a decode failure on a 2xx body means
// the request already reached the server — re-sending a non-idempotent mutation
// could double-apply it (CreateRequest's create POST is the one retryable
// non-GET call; a blind retry there could open a duplicate PR once the original
// is no longer open to trip GitHub's 422 adopt guard).
func retryDelay(err error, hdr http.Header, backoff time.Duration) (time.Duration, bool) {
	var noResp *errNoResponse
	if errors.As(err, &noResp) {
		return backoff, true
	}
	var ae *apiError
	if !errors.As(err, &ae) {
		// A response was received but decoding (or similar) failed: do not retry.
		return 0, false
	}
	if !retryableStatus(ae.StatusCode, hdr) {
		return 0, false
	}
	if d, ok := serverBackoff(hdr); ok {
		return d, true
	}
	return backoff, true
}

// retryableStatus reports whether an HTTP status is worth retrying. 429 and the
// common 5xx are always retryable; a 403 is retryable only when it carries
// rate-limit signals (Retry-After, or an exhausted X-RateLimit-Remaining), so a
// genuine permission denial is not retried.
func retryableStatus(code int, hdr http.Header) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	case http.StatusForbidden:
		if hdr.Get("Retry-After") != "" {
			return true
		}
		return hdr.Get("X-RateLimit-Remaining") == "0"
	default:
		return false
	}
}

// serverBackoff reads a server-directed wait from Retry-After (delta-seconds or
// an HTTP-date) or X-RateLimit-Reset (a Unix epoch), clamped to maxServerBackoff
// so an absurd header cannot stall a reconcile. ok is false when neither header
// gives a usable value.
func serverBackoff(hdr http.Header) (time.Duration, bool) {
	clamp := func(d time.Duration) time.Duration {
		if d < 0 {
			return 0
		}
		if d > maxServerBackoff {
			return maxServerBackoff
		}
		return d
	}
	if ra := strings.TrimSpace(hdr.Get("Retry-After")); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil {
			return clamp(time.Duration(secs) * time.Second), true
		}
		if t, err := http.ParseTime(ra); err == nil {
			return clamp(time.Until(t)), true
		}
	}
	if reset := strings.TrimSpace(hdr.Get("X-RateLimit-Reset")); reset != "" {
		if epoch, err := strconv.ParseInt(reset, 10, 64); err == nil {
			return clamp(time.Until(time.Unix(epoch, 0))), true
		}
	}
	return 0, false
}

// sleepCtx waits for d or until ctx is cancelled, returning ctx.Err() if the
// context ends first (or is already ended). A non-positive d is an immediate
// return that still reports a cancelled context.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
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

// managedMarker reports whether pr is one of oiax's own managed requests
// and returns its marker. Identity rests on facts a third party who can open a
// pull request cannot forge: a well-formed marker in the body (an HTML-comment
// block whose version matches markerVersionPattern — prose that merely mentions
// oiax is never a marker), the branch relationship the marker declares (head ==
// source, base == destination), and — the provenance signal — the head and base
// branches living in the same repository. The oiax label is deliberately NOT
// part of identity: it is decorative, and a human removing it must not make oiax
// lose track of its own request (which would duplicate on backflow and exit 1 on
// promotion).
//
// The marker body and the head/base branch names are wholly author-controlled:
// anyone who can open a PR from a fork writes the body and chooses those names.
// Requiring head and base to share a repository restores the authorization
// boundary the decorative label never enforced — only a collaborator with push
// access can create a branch directly in the base repo, so a fork PR (head repo
// != base repo) is never treated as managed even with a hand-written marker.
//
// Recognition is forward-compatible on purpose: a marker written by a newer
// release (version v2 or higher) is still recognized as managed, so an older
// oiax running concurrently during a rollout never mistakes it for unmanaged
// and opens a duplicate. Whether this build understands the schema well
// enough to mutate the request is the separate, stricter question
// understoodMarker answers.
func managedMarker(pr ghPull) (marker, bool) {
	m, ok := parseMarker(pr.Body)
	if !ok || !markerVersionPattern.MatchString(m.Version) {
		return marker{}, false
	}
	if pr.Head.Ref != m.Source || pr.Base.Ref != m.Destination {
		return marker{}, false
	}
	// A PR whose head branch lives in a different repository than its base —
	// i.e. opened from a fork — is never oiax's own: oiax only ever opens
	// requests branch-to-branch within the base repo, and only push access can
	// put a branch there. This is the provenance the marker text and branch
	// names cannot supply, since a PR author controls both.
	if pr.Head.Repo.FullName != pr.Base.Repo.FullName {
		return marker{}, false
	}
	return m, true
}

// conflictMarker reports whether iss is one of oiax's own conflict
// artifacts and returns its marker. Unlike managedMarker, an issue has no
// head/base+same-repo provenance, so identity rests on the body marker (a
// well-formed version, type == conflictMarkerType) PLUS both required
// labels (oiax AND oiax/conflict). The labels are the authorization
// substitute: only a collaborator can label an in-repo issue, so an
// outsider cannot forge a recognized artifact by hand-crafting a marker in
// an issue body. An issue that is actually a pull request (the /issues
// route returns PRs too) is never an artifact.
func conflictMarker(iss ghIssue) (marker, bool) {
	if iss.PullRequest != nil {
		return marker{}, false
	}
	m, ok := parseMarker(iss.Body)
	if !ok || !markerVersionPattern.MatchString(m.Version) {
		return marker{}, false
	}
	if m.Type != conflictMarkerType {
		return marker{}, false
	}
	if !hasLabel(iss.Labels, LabelOiax) || !hasLabel(iss.Labels, LabelConflict) {
		return marker{}, false
	}
	return m, true
}

// hasLabel reports whether labels contains a label with the given name.
func hasLabel(labels []ghLabel, name string) bool {
	for _, l := range labels {
		if l.Name == name {
			return true
		}
	}
	return false
}

// understoodMarker reports whether this build understands the marker's
// schema version well enough to safely rewrite or close the request. A
// marker written by a newer release (a higher vN) is recognized as managed
// — so it is never duplicated — but this build must not mutate it:
// re-serializing with an older schema could silently drop fields it cannot
// see. Acting is therefore gated on understanding; recognition
// (managedMarker) is not.
func understoodMarker(m marker) bool {
	got, ok := markerVersionNum(m.Version)
	if !ok {
		return false
	}
	cur, ok := markerVersionNum(markerVersion)
	if !ok {
		return false
	}
	return got <= cur
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

// issueNumber parses a ConflictArtifactID into a positive GitHub issue
// number. It is a copy of prNumber, kept separate for clarity: an issue
// number and a PR number share GitHub's numbering but are distinct
// identities. Atoi both converts and guards the value against path
// injection.
func issueNumber(id string) (int, error) {
	n, err := strconv.Atoi(id)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid conflict artifact id %q", id)
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
