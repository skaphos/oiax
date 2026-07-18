package azuredevops

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
	mk "github.com/skaphos/oiax/internal/forge/marker"
)

// mergedLookback bounds how far back merged-request discovery asks the API for
// completed pull requests (searchCriteria.minTime on closedDate), so a reconcile
// never pages the entire completed-PR history. The baseline rung only needs the
// most recent merged managed request per edge; a promotion that merged longer
// ago than this loses only that recorded-baseline shortcut (the other
// equivalence rungs still detect it). Generous on purpose.
const mergedLookback = 180 * 24 * time.Hour

// maxListPages caps pagination on any $skip-paged listing, a backstop so a
// misbehaving API cannot make discovery loop unboundedly. At 100 per page this
// is 10,000 pull requests — far past any real managed-request count.
const maxListPages = 100

// markerProperty is the Azure DevOps pull-request property key under which the
// durable copy of the managed-request marker is stored (dual-write, ADR 0009).
// The list path reads the marker from the (truncated) description; the
// single-PR mutation paths prefer this durable copy and fall back to the
// description, so a human editing the description out never makes Oiax lose
// track of its own request.
const markerProperty = "oiax.managedRequest"

// Provider is the Azure DevOps implementation of forge.Forge. It addresses a
// repository through the organization/project/name triple (Repo) and speaks the
// Azure DevOps REST API (api-version 7.1). Credentials never appear in any
// error, log, plan, or the process table.
type Provider struct {
	// Repo identifies the Azure DevOps repository (organization/project/name).
	Repo Repo
	// Token authenticates every REST call and the git push. A personal access
	// token authenticates as HTTP Basic; a $(System.AccessToken) JWT as Bearer
	// (looksLikeJWT decides). Never logged.
	Token string
	// WorkItemType is the Azure Boards work-item type durable conflict artifacts
	// are created as. Empty means defaultWorkItemType ("Issue", the Basic
	// process type); Agile/Scrum/CMMI projects must set OIAX_ADO_WORKITEM_TYPE.
	WorkItemType string
	// BaseURL overrides the REST root (default https://dev.azure.com/{org});
	// tests point it at an httptest.Server. Never logged.
	BaseURL string
	// APIVersion overrides the api-version query value (default 7.1).
	APIVersion string
	// HTTP is the client used for requests (default: a shared client carrying a
	// request timeout); injectable for tests.
	HTTP *http.Client
	// GitDir is the working directory PushBranch runs git from. It must share an
	// object database with the commit being pushed. Empty means the current
	// directory. It is a path, not a credential — safe to log.
	GitDir string
	// GitRemote overrides the push remote (default: the Azure DevOps https git
	// URL built from Repo; the credential is supplied via http.extraHeader in the
	// environment, not the URL). Tests point it at a local bare repository.
	GitRemote string
	// Warn, when set, receives a one-time degradation warning. Azure DevOps has
	// no analogue of the GitHub token-bot CI-skip today, so it is currently
	// unused; it exists for interface symmetry with the GitHub provider. The
	// warn-once guard (the GitHub provider's sync.Once) returns together with
	// the first warning path that needs it.
	Warn func(msg string)

	// Resilience tunables. Zero values use the production defaults; they exist
	// only so tests can shrink backoff and the response cap. Not part of the
	// public contract.
	retryMax     int
	retryBackoff time.Duration
	maxRespBytes int64

	// repoID memoizes the repository's immutable GUID so TargetMergeMethods,
	// called once per target branch during a reconcile, resolves it with a
	// single GET rather than one per edge.
	repoIDOnce sync.Once
	repoIDVal  string
	repoIDErr  error
}

var _ forge.Forge = (*Provider)(nil)

// adoPull is the subset of Azure DevOps's pull-request JSON the provider reads.
// In the list response the description is truncated to 400 characters; the
// marker is written first in the description so it survives that truncation.
type adoPull struct {
	PullRequestID int    `json:"pullRequestId"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	SourceRefName string `json:"sourceRefName"`
	TargetRefName string `json:"targetRefName"`
	Status        string `json:"status"`
	CreationDate  string `json:"creationDate"`
	ClosedDate    string `json:"closedDate"`
	// ForkSource is populated only when the pull request's source branch lives
	// in a fork. Oiax only ever opens requests branch-to-branch within the base
	// repository, so a fork PR is never one of its own (the provenance guard the
	// GitHub provider gets from head.repo == base.repo).
	ForkSource *forkRef `json:"forkSource"`
}

type forkRef struct {
	Name string `json:"name"`
}

type adoPullList struct {
	Value []adoPull `json:"value"`
	Count int       `json:"count"`
}

// propertiesCollection is the shape of the PR Properties GET response: a map
// keyed by property name, each value carrying a $type and $value.
type propertiesCollection struct {
	Value map[string]struct {
		Value string `json:"$value"`
	} `json:"value"`
}

// gitRef and refList model the refs GET response used to resolve a branch's
// current object id before deleting it.
type gitRef struct {
	Name     string `json:"name"`
	ObjectID string `json:"objectId"`
}

type refList struct {
	Value []gitRef `json:"value"`
}

// refUpdateResult is one element of the refs-update POST response. The HTTP call
// returns 200 even when an individual ref update fails, so the per-item success
// and updateStatus must be inspected.
type refUpdateResult struct {
	Success       bool   `json:"success"`
	UpdateStatus  string `json:"updateStatus"`
	CustomMessage string `json:"customMessage"`
}

type refUpdateResults struct {
	Value []refUpdateResult `json:"value"`
}

// refHead renders a bare branch name as the fully-qualified head ref Azure
// DevOps addresses branches by.
func refHead(branch string) string { return "refs/heads/" + branch }

// workItemType is the configured Azure Boards work-item type, or the default.
func (p *Provider) workItemType() string {
	if p.WorkItemType != "" {
		return p.WorkItemType
	}
	return defaultWorkItemType
}

// ListManagedRequests returns the managed change requests for the graph and type
// in filter, in the requested state. Open discovery asks for active pull
// requests; merged discovery asks for completed ones (completed = merged; an
// abandoned PR is a separate status Oiax never adopts) within the merged
// lookback window. Each candidate's marker is read from the description
// (marker-first, so the 400-character list truncation cannot drop it); title and
// branch labels are never consulted.
func (p *Provider) ListManagedRequests(ctx context.Context, filter forge.RequestFilter) ([]engine.ChangeRequest, error) {
	status := "active"
	base := p.gitPath("/pullrequests") + "?searchCriteria.status=" + status
	if filter.State == forge.RequestStateMerged {
		status = "completed"
		minTime := time.Now().Add(-mergedLookback).UTC().Format(time.RFC3339)
		base = p.gitPath("/pullrequests") + "?searchCriteria.status=" + status +
			"&searchCriteria.queryTimeRangeType=closed&searchCriteria.minTime=" + url.QueryEscape(minTime)
	}

	// Each match travels with its own PR's closedDate so the merged-discovery
	// sort keys off the right request. Sorting a positional index into the
	// unfiltered page would misalign the instant any PR is dropped by the
	// filters below (a human PR, another graph, another type — the common case).
	type matched struct {
		cr         engine.ChangeRequest
		closedDate string
	}
	var matches []matched
	for page := 0; page < maxListPages; page++ {
		pageURL := base + "&$top=100&$skip=" + strconv.Itoa(page*100)
		var list adoPullList
		if _, err := p.do(ctx, http.MethodGet, pageURL, "", nil, &list); err != nil {
			return nil, fmt.Errorf("list managed requests: %w", err)
		}
		for _, pr := range list.Value {
			m, ok := mk.Parse(pr.Description)
			if !ok {
				continue
			}
			m, ok = p.managed(pr, m)
			if !ok {
				continue
			}
			if m.Graph != filter.Graph {
				continue
			}
			if filter.Type != "" && m.Type != string(filter.Type) {
				continue
			}
			matches = append(matches, matched{changeRequest(pr, m), pr.ClosedDate})
		}
		if len(list.Value) < 100 {
			break
		}
	}
	// Merged discovery must return newest-merged first: the baseline rung
	// consumes the most recently merged managed request per edge, and the
	// reconcile layer takes the first match in list order. Azure DevOps does not
	// guarantee an order, so sort by closedDate descending. Open discovery needs
	// no order — Oiax allows at most one open request per edge.
	if filter.State == forge.RequestStateMerged {
		sort.SliceStable(matches, func(i, j int) bool {
			return matches[i].closedDate > matches[j].closedDate
		})
	}
	out := make([]engine.ChangeRequest, len(matches))
	for i := range matches {
		out[i] = matches[i].cr
	}
	return out, nil
}

// managed reports whether pr is one of Oiax's own managed requests, given a
// marker already parsed from its description or properties. Identity rests on a
// well-formed marker version, the branch relationship the marker declares
// (source ref == source, target ref == destination), and the request not
// originating from a fork (the provenance guard: Oiax only opens same-repo
// branch-to-branch requests). The tags are decorative — a human removing them
// must not make Oiax lose track of its own request.
func (p *Provider) managed(pr adoPull, m mk.Marker) (mk.Marker, bool) {
	if !mk.VersionPattern.MatchString(m.Version) {
		return mk.Marker{}, false
	}
	if pr.SourceRefName != refHead(m.Source) || pr.TargetRefName != refHead(m.Destination) {
		return mk.Marker{}, false
	}
	if pr.ForkSource != nil {
		return mk.Marker{}, false
	}
	return m, true
}

// managedRequest fetches a single pull request and resolves its marker,
// preferring the durable PR-properties copy and falling back to the description.
// It is the single-PR path (update, close); the list path parses the description
// directly since properties are not returned in a list.
func (p *Provider) managedRequest(ctx context.Context, id int) (adoPull, mk.Marker, error) {
	pr, err := p.getPull(ctx, id)
	if err != nil {
		return adoPull{}, mk.Marker{}, err
	}
	m, ok := p.markerFromProperties(ctx, id)
	if !ok {
		m, ok = mk.Parse(pr.Description)
		if !ok {
			return pr, mk.Marker{}, errNotManaged
		}
	}
	m, ok = p.managed(pr, m)
	if !ok {
		return pr, mk.Marker{}, errNotManaged
	}
	return pr, m, nil
}

// errNotManaged marks a pull request that is not one of Oiax's own managed
// requests. Callers wrap it with the operation and id.
var errNotManaged = errors.New("not a managed request")

// getPull fetches a single pull request by id (full, untruncated description).
func (p *Provider) getPull(ctx context.Context, id int) (adoPull, error) {
	var pr adoPull
	if _, err := p.do(ctx, http.MethodGet, p.gitPath("/pullrequests/"+strconv.Itoa(id)), "", nil, &pr); err != nil {
		return adoPull{}, err
	}
	return pr, nil
}

// markerFromProperties reads the durable marker copy from a pull request's
// properties. ok is false when the property is absent or does not parse — the
// caller then falls back to the description.
func (p *Provider) markerFromProperties(ctx context.Context, id int) (mk.Marker, bool) {
	var props propertiesCollection
	if _, err := p.do(ctx, http.MethodGet, p.gitPath("/pullrequests/"+strconv.Itoa(id)+"/properties"), "", nil, &props); err != nil {
		return mk.Marker{}, false
	}
	v, ok := props.Value[markerProperty]
	if !ok {
		return mk.Marker{}, false
	}
	return mk.Parse(v.Value)
}

// changeRequest maps a managed pull request and its marker to the engine's
// provider-neutral view. The marker's destination becomes the engine's Target.
func changeRequest(pr adoPull, m mk.Marker) engine.ChangeRequest {
	return engine.ChangeRequest{
		ID:         strconv.Itoa(pr.PullRequestID),
		Type:       engine.RequestType(m.Type),
		Source:     m.Source,
		Target:     m.Destination,
		SourceHead: m.SourceHead,
	}
}

// CreateRequest opens a managed request with the marker written first in the
// description (so the 400-character list truncation cannot drop it) and mirrored
// to PR properties (the durable copy), and the oiax + type labels attached. A
// duplicate active request (HTTP 409 TF401179) is adopted as success: the
// provider re-lists and returns the surviving request instead of erroring — the
// forge is the concurrency arbiter for promotion requests.
func (p *Provider) CreateRequest(ctx context.Context, req forge.CreateRequest) (engine.ChangeRequest, error) {
	m := mk.Marker{
		Version:     mk.Version,
		Graph:       req.Graph,
		Type:        string(req.Type),
		Source:      req.Source,
		Destination: req.Target,
		SourceHead:  req.SourceHead,
	}
	// Reject a marker value that could forge marker lines or break out of the
	// HTML comment before it is ever written to the forge.
	if err := mk.Validate(m); err != nil {
		return engine.ChangeRequest{}, fmt.Errorf("create request: %w", err)
	}
	serialized := mk.Serialize(m)
	description := serialized + "\n\n" + req.Body

	payload := map[string]any{
		"sourceRefName": refHead(req.Source),
		"targetRefName": refHead(req.Target),
		"title":         req.Title,
		"description":   description,
		"labels": []map[string]string{
			{"name": mk.LabelOiax},
			{"name": mk.TypeLabel(req.Type)},
		},
	}
	var created adoPull
	// The create POST is retried on transient failures: it is safely idempotent
	// because Azure DevOps rejects a duplicate active source/target pair with 409
	// TF401179, which the adopt path below turns into success — so a retry that
	// races a first attempt that actually landed re-lists and adopts rather than
	// double-opening.
	_, err := p.send(ctx, http.MethodPost, p.gitPath("/pullrequests"), contentTypeJSON, payload, &created, true)
	if err != nil {
		if isDuplicateActiveRequest(err) {
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

	// Mirror the marker to the durable PR-properties store. A failure here is
	// surfaced: the request exists with a valid description marker, so the next
	// reconcile re-lists, adopts it via the 409 path, and re-attempts the
	// properties write — the reconcile is idempotent.
	if err := p.putMarkerProperty(ctx, created.PullRequestID, serialized); err != nil {
		return engine.ChangeRequest{}, fmt.Errorf("create request: %w", err)
	}

	return engine.ChangeRequest{
		ID:         strconv.Itoa(created.PullRequestID),
		Type:       req.Type,
		Source:     req.Source,
		Target:     req.Target,
		SourceHead: req.SourceHead,
	}, nil
}

// adoptDuplicate re-lists open managed requests for the same graph and type and
// returns the one whose source/destination match req, if any.
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

// putMarkerProperty writes (adds or replaces) the durable marker property on a
// pull request via the JSON Patch properties API.
func (p *Provider) putMarkerProperty(ctx context.Context, id int, serialized string) error {
	patch := []map[string]any{
		{"op": "add", "path": "/" + markerProperty, "value": serialized},
	}
	if _, err := p.do(ctx, http.MethodPatch,
		p.gitPath("/pullrequests/"+strconv.Itoa(id)+"/properties"), contentTypeJSONPatch, patch, nil); err != nil {
		return fmt.Errorf("write managed-request property: %w", err)
	}
	return nil
}

// UpdateRequest rewrites the recorded sourceHead in a managed request's marker,
// in both the description and the durable property, leaving the human body text
// intact. It refuses a request that is not managed, or whose marker version this
// build does not support (rewriting an unknown schema could drop fields it
// cannot see).
func (p *Provider) UpdateRequest(ctx context.Context, req forge.UpdateRequest) error {
	id, err := requestID(string(req.ID))
	if err != nil {
		return fmt.Errorf("update request: %w", err)
	}
	pr, m, err := p.managedRequest(ctx, id)
	if err != nil {
		return fmt.Errorf("update request %s: %w", req.ID, err)
	}
	if !understood(m) {
		return fmt.Errorf("update request %s: marker version %q is not supported by this build; upgrade oiax", req.ID, m.Version)
	}
	m.SourceHead = req.SourceHead
	if err := mk.Validate(m); err != nil {
		return fmt.Errorf("update request %s: %w", req.ID, err)
	}
	serialized := mk.Serialize(m)
	newBody, ok := mk.Replace(pr.Description, m)
	if !ok {
		// The description no longer carries a marker block (a human edited it out);
		// the durable property still identified the request. Restore the marker,
		// first, ahead of the remaining human text.
		newBody = serialized + "\n\n" + pr.Description
	}
	if _, err := p.do(ctx, http.MethodPatch, p.gitPath("/pullrequests/"+strconv.Itoa(id)),
		contentTypeJSON, map[string]string{"description": newBody}, nil); err != nil {
		return fmt.Errorf("update request %s: %w", req.ID, err)
	}
	if err := p.putMarkerProperty(ctx, id, serialized); err != nil {
		return fmt.Errorf("update request %s: %w", req.ID, err)
	}
	return nil
}

// CloseRequest closes an obsolete managed request. It refuses to touch a request
// that is not managed, or whose marker version this build does not support; it
// comments the reason before abandoning, and never deletes.
func (p *Provider) CloseRequest(ctx context.Context, id forge.RequestID, reason forge.Reason) error {
	num, err := requestID(string(id))
	if err != nil {
		return fmt.Errorf("close request: %w", err)
	}
	_, m, err := p.managedRequest(ctx, num)
	if err != nil {
		return fmt.Errorf("close request %s: %w", id, err)
	}
	if !understood(m) {
		return fmt.Errorf("close request %s: marker version %q is not supported by this build; upgrade oiax", id, m.Version)
	}
	// A PR-level comment: a thread with a single text comment, marked closed.
	thread := map[string]any{
		"comments": []map[string]any{
			{"parentCommentId": 0, "content": reason.Summary, "commentType": "text"},
		},
		"status": "closed",
	}
	if _, err := p.do(ctx, http.MethodPost, p.gitPath("/pullrequests/"+strconv.Itoa(num)+"/threads"),
		contentTypeJSON, thread, nil); err != nil {
		return fmt.Errorf("close request %s: comment: %w", id, err)
	}
	if _, err := p.do(ctx, http.MethodPatch, p.gitPath("/pullrequests/"+strconv.Itoa(num)),
		contentTypeJSON, map[string]string{"status": "abandoned"}, nil); err != nil {
		return fmt.Errorf("close request %s: %w", id, err)
	}
	return nil
}

// PushBranch pushes push.SHA to refs/heads/<push.Name>, confined to the oiax/
// namespace: any name outside oiax/ is refused before git is touched, so
// force-pushing can never escape the namespace Oiax owns.
//
// The push shells out to git following the no-shell posture: arguments are exec
// args (never a shell string), the branch name is validated with
// git check-ref-format, the commit id is guarded with oidPattern, and both reach
// git as operands after --end-of-options. The credential is delivered via
// http.extraHeader in the environment (pushAuthEnv), never in argv or the remote
// URL, and is scrubbed from any surfaced git error.
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
	args = append(args, "--end-of-options", p.gitRemote(), push.SHA+":refs/heads/"+push.Name)

	cmd := gitCommand(ctx, p.GitDir, p.pushAuthEnv(), args...)
	var stderr capWriter
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("push branch %q: %w: %s", push.Name, err, p.scrubToken(msg))
		}
		return fmt.Errorf("push branch %q: %w", push.Name, err)
	}
	return nil
}

// DeleteBranch deletes refs/heads/<name>, confined to the oiax/ namespace. It
// removes the head branch left behind when a managed backflow request is
// superseded or closed. A branch that is already gone is treated as success,
// keeping the reconcile idempotent. Azure DevOps's refs-update API needs the
// ref's current object id (an optimistic-concurrency guard), so the current tip
// is resolved first; an absent ref, or an update reported as
// succeededNonExistentRef, is idempotent success.
func (p *Provider) DeleteBranch(ctx context.Context, name string) error {
	if !strings.HasPrefix(name, "oiax/") {
		return fmt.Errorf("delete branch %q: refused outside the oiax/ namespace", name)
	}
	if err := p.checkRefFormat(ctx, name); err != nil {
		return fmt.Errorf("delete branch %q: %w", name, err)
	}
	current, ok, err := p.resolveRef(ctx, name)
	if err != nil {
		return fmt.Errorf("delete branch %q: %w", name, err)
	}
	if !ok {
		// The branch is already gone: idempotent success.
		return nil
	}
	update := []map[string]string{
		{"name": refHead(name), "oldObjectId": current, "newObjectId": zeroObjectID},
	}
	var results refUpdateResults
	if _, err := p.do(ctx, http.MethodPost, p.gitPath("/refs"), contentTypeJSON, update, &results); err != nil {
		return fmt.Errorf("delete branch %q: %w", name, err)
	}
	if len(results.Value) == 0 {
		return fmt.Errorf("delete branch %q: refs update returned no result", name)
	}
	r := results.Value[0]
	if r.Success || r.UpdateStatus == "succeeded" || r.UpdateStatus == "succeededNonExistentRef" {
		return nil
	}
	msg := r.UpdateStatus
	if r.CustomMessage != "" {
		msg = r.UpdateStatus + ": " + r.CustomMessage
	}
	return fmt.Errorf("delete branch %q: refs update %s", name, msg)
}

// resolveRef returns the current object id of refs/heads/<name>. ok is false
// when the ref does not exist (an idempotent-delete signal). The filter is a
// prefix match, so the exact ref is selected from the results.
func (p *Provider) resolveRef(ctx context.Context, name string) (objectID string, ok bool, err error) {
	u := p.gitPath("/refs") + "?filter=" + url.QueryEscape("heads/"+name)
	var refs refList
	if _, err := p.do(ctx, http.MethodGet, u, "", nil, &refs); err != nil {
		return "", false, err
	}
	for _, r := range refs.Value {
		if r.Name == refHead(name) {
			return r.ObjectID, true, nil
		}
	}
	return "", false, nil
}

// understood reports whether this build understands the marker's schema version
// well enough to safely rewrite or close the request. A marker written by a
// newer release is recognized as managed (never duplicated) but not mutated.
func understood(m mk.Marker) bool {
	got, ok := mk.VersionNum(m.Version)
	if !ok {
		return false
	}
	cur, ok := mk.VersionNum(mk.Version)
	if !ok {
		return false
	}
	return got <= cur
}

// requestID parses a RequestID into a positive Azure DevOps pull-request id.
// Atoi both converts and guards the value against path injection.
func requestID(id string) (int, error) {
	n, err := strconv.Atoi(id)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid request id %q", id)
	}
	return n, nil
}

// htmlBody renders human text (which may carry an attacker-influenced failing-
// commit subject) for an HTML work-item description field: every value is
// HTML-escaped and newlines become <br/>, so no embedded markup can execute in
// the Azure Boards UI. The marker, which is Oiax's own validated content, is
// concatenated raw by the caller so it stays a parseable HTML comment.
func htmlBody(s string) string {
	return strings.ReplaceAll(html.EscapeString(s), "\n", "<br/>\n")
}
