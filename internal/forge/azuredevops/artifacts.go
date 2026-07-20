package azuredevops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/skaphos/oiax/internal/forge"
	mk "github.com/skaphos/oiax/internal/forge/marker"
)

// workItem is the subset of an Azure Boards work item the provider reads. Fields
// are a reference-name → raw-JSON map (System.Description, System.Tags,
// System.State, System.WorkItemType).
type workItem struct {
	ID     int                        `json:"id"`
	Fields map[string]json.RawMessage `json:"fields"`
}

type workItemBatch struct {
	Value []workItem `json:"value"`
}

// wiqlResult is the WIQL query response: work-item ids only (no field values).
type wiqlResult struct {
	WorkItems []struct {
		ID int `json:"id"`
	} `json:"workItems"`
}

// wiState is one state of a work-item type, with the process-independent
// category that decides open vs. closed.
type wiState struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

type wiStates struct {
	Value []wiState `json:"value"`
}

// fieldString reads a string work-item field, returning "" when it is absent or
// not a string.
func fieldString(wi workItem, key string) string {
	raw, ok := wi.Fields[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// hasTag reports whether a System.Tags value ("a; b; c") contains want.
func hasTag(tags, want string) bool {
	for _, t := range strings.Split(tags, ";") {
		if strings.TrimSpace(t) == want {
			return true
		}
	}
	return false
}

// conflictMarker reports whether wi is one of Oiax's own conflict artifacts and
// returns its marker. A work item has no head/base provenance, so identity rests
// on the body marker (a well-formed version, type == conflict) PLUS both
// required tags (oiax AND oiax/conflict). The tags are the authorization
// substitute: only a contributor can tag an in-project work item, so an outsider
// cannot forge a recognized artifact by hand-crafting a marker in a description.
func (p *Provider) conflictMarker(wi workItem) (mk.Marker, bool) {
	m, ok := mk.Parse(fieldString(wi, "System.Description"))
	if !ok || !mk.VersionPattern.MatchString(m.Version) {
		return mk.Marker{}, false
	}
	if m.Type != mk.ConflictType {
		return mk.Marker{}, false
	}
	tags := fieldString(wi, "System.Tags")
	if !hasTag(tags, mk.LabelOiax) || !hasTag(tags, mk.LabelConflict) {
		return mk.Marker{}, false
	}
	return m, true
}

// ListConflictArtifacts returns the open durable conflict artifacts for graph,
// sorted ascending by work-item id. A WIQL query finds every work item tagged
// oiax + oiax/conflict; the results are hydrated and kept only when the marker's
// graph matches and the item's state is not in the Completed or Removed
// category. The ascending sort makes the reconcile layer's
// lowest-id-canonical duplicate-consolidation rule deterministic.
func (p *Provider) ListConflictArtifacts(ctx context.Context, graph string) ([]forge.ConflictArtifact, error) {
	query := "SELECT [System.Id] FROM WorkItems " +
		"WHERE [System.TeamProject] = @project " +
		"AND [System.Tags] CONTAINS '" + mk.LabelOiax + "' " +
		"AND [System.Tags] CONTAINS '" + mk.LabelConflict + "' " +
		"ORDER BY [System.Id] ASC"
	var result wiqlResult
	if _, err := p.do(ctx, http.MethodPost, p.projectPath("/wit/wiql"),
		contentTypeJSON, map[string]string{"query": query}, &result); err != nil {
		return nil, fmt.Errorf("list conflict artifacts: %w", err)
	}
	if len(result.WorkItems) == 0 {
		return nil, nil
	}
	ids := make([]int, 0, len(result.WorkItems))
	for _, w := range result.WorkItems {
		ids = append(ids, w.ID)
	}
	items, err := p.getWorkItems(ctx, ids,
		"System.Description", "System.Tags", "System.State", "System.WorkItemType")
	if err != nil {
		return nil, fmt.Errorf("list conflict artifacts: %w", err)
	}

	stateCache := map[string]map[string]string{}
	var out []forge.ConflictArtifact
	for _, wi := range items {
		m, ok := p.conflictMarker(wi)
		if !ok || m.Graph != graph {
			continue
		}
		open, err := p.isOpenState(ctx, fieldString(wi, "System.WorkItemType"), fieldString(wi, "System.State"), stateCache)
		if err != nil {
			return nil, fmt.Errorf("list conflict artifacts: %w", err)
		}
		if !open {
			continue
		}
		out = append(out, forge.ConflictArtifact{
			ID:         forge.ConflictArtifactID(strconv.Itoa(wi.ID)),
			Source:     m.Source,
			Target:     m.Destination,
			SourceHead: m.SourceHead,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := strconv.Atoi(string(out[i].ID))
		b, _ := strconv.Atoi(string(out[j].ID))
		return a < b
	})
	return out, nil
}

// CreateConflictArtifact opens a durable conflict artifact: a work item of the
// configured type carrying the marker (type: conflict) and the operator playbook
// in its HTML description, and both the oiax and oiax/conflict tags. The human
// body (which may carry an attacker-influenced failing-commit subject) is
// HTML-escaped; the marker, Oiax's own validated content, is written raw so it
// stays a parseable HTML comment. It deliberately does NOT collapse duplicates —
// the reconcile layer consolidates on every run.
func (p *Provider) CreateConflictArtifact(ctx context.Context, spec forge.ConflictArtifactSpec) (forge.ConflictArtifact, error) {
	m := mk.Marker{
		Version:     mk.Version,
		Graph:       spec.Graph,
		Type:        mk.ConflictType,
		Source:      spec.Source,
		Destination: spec.Target,
		SourceHead:  spec.SourceHead,
	}
	if err := mk.Validate(m); err != nil {
		return forge.ConflictArtifact{}, fmt.Errorf("create conflict artifact: %w", err)
	}
	description := mk.Serialize(m) + "<br/><br/>\n" + htmlBody(spec.Body)

	patch := []map[string]any{
		{"op": "add", "path": "/fields/System.Title", "value": spec.Title},
		{"op": "add", "path": "/fields/System.Description", "value": description},
		{"op": "add", "path": "/fields/System.Tags", "value": mk.LabelOiax + "; " + mk.LabelConflict},
	}
	var created workItem
	if _, err := p.do(ctx, http.MethodPost,
		p.projectPath("/wit/workitems/$"+url.PathEscape(p.workItemType())),
		contentTypeJSONPatch, patch, &created); err != nil {
		return forge.ConflictArtifact{}, fmt.Errorf("create conflict artifact: %w", err)
	}
	return forge.ConflictArtifact{
		ID:         forge.ConflictArtifactID(strconv.Itoa(created.ID)),
		Source:     spec.Source,
		Target:     spec.Target,
		SourceHead: spec.SourceHead,
	}, nil
}

// UpdateConflictArtifact refreshes an existing conflict artifact in place: it
// rewrites the whole description (the human failing-commit text and the marker
// with the new sourceHead). It refuses a work item that is not a conflict
// artifact, or whose marker version this build does not understand. No comment
// is posted (deliberate: comments only on close/consolidate).
func (p *Provider) UpdateConflictArtifact(ctx context.Context, id forge.ConflictArtifactID, spec forge.ConflictArtifactSpec) error {
	wid, err := artifactID(id)
	if err != nil {
		return fmt.Errorf("update conflict artifact: %w", err)
	}
	wi, err := p.getWorkItem(ctx, wid, "System.Description", "System.Tags")
	if err != nil {
		return fmt.Errorf("update conflict artifact %s: %w", id, err)
	}
	m, ok := p.conflictMarker(wi)
	if !ok {
		return fmt.Errorf("update conflict artifact %s: not a conflict artifact", id)
	}
	if !understood(m) {
		return fmt.Errorf("update conflict artifact %s: marker version %q is not supported by this build; upgrade oiax", id, m.Version)
	}
	m = mk.Marker{
		Version:     mk.Version,
		Graph:       spec.Graph,
		Type:        mk.ConflictType,
		Source:      spec.Source,
		Destination: spec.Target,
		SourceHead:  spec.SourceHead,
	}
	if err := mk.Validate(m); err != nil {
		return fmt.Errorf("update conflict artifact %s: %w", id, err)
	}
	description := mk.Serialize(m) + "<br/><br/>\n" + htmlBody(spec.Body)
	patch := []map[string]any{
		{"op": "add", "path": "/fields/System.Description", "value": description},
	}
	if _, err := p.do(ctx, http.MethodPatch, p.projectPath("/wit/workitems/"+strconv.Itoa(wid)),
		contentTypeJSONPatch, patch, nil); err != nil {
		return fmt.Errorf("update conflict artifact %s: %w", id, err)
	}
	return nil
}

// CloseConflictArtifact closes a resolved conflict artifact: it adds the reason
// as a discussion comment (System.History) and sets the state to the work-item
// type's Completed-category state, in a single patch. It refuses a work item
// that is not a conflict artifact, or whose marker version this build does not
// understand, and never deletes.
func (p *Provider) CloseConflictArtifact(ctx context.Context, id forge.ConflictArtifactID, reason forge.Reason) error {
	wid, err := artifactID(id)
	if err != nil {
		return fmt.Errorf("close conflict artifact: %w", err)
	}
	wi, err := p.getWorkItem(ctx, wid, "System.Description", "System.Tags", "System.WorkItemType")
	if err != nil {
		return fmt.Errorf("close conflict artifact %s: %w", id, err)
	}
	m, ok := p.conflictMarker(wi)
	if !ok {
		return fmt.Errorf("close conflict artifact %s: not a conflict artifact", id)
	}
	if !understood(m) {
		return fmt.Errorf("close conflict artifact %s: marker version %q is not supported by this build; upgrade oiax", id, m.Version)
	}
	typ := fieldString(wi, "System.WorkItemType")
	if typ == "" {
		typ = p.workItemType()
	}
	completed, err := p.completedState(ctx, typ)
	if err != nil {
		return fmt.Errorf("close conflict artifact %s: %w", id, err)
	}
	patch := []map[string]any{
		{"op": "add", "path": "/fields/System.History", "value": htmlBody(reason.Summary)},
		{"op": "add", "path": "/fields/System.State", "value": completed},
	}
	if _, err := p.do(ctx, http.MethodPatch, p.projectPath("/wit/workitems/"+strconv.Itoa(wid)),
		contentTypeJSONPatch, patch, nil); err != nil {
		return fmt.Errorf("close conflict artifact %s: %w", id, err)
	}
	return nil
}

// getWorkItems hydrates the given ids in batches of 200 (the API cap), skipping
// ids that no longer resolve (errorPolicy=omit) so a concurrently-deleted item
// cannot fail the whole listing.
func (p *Provider) getWorkItems(ctx context.Context, ids []int, fields ...string) ([]workItem, error) {
	const batch = 200
	var out []workItem
	for start := 0; start < len(ids); start += batch {
		end := start + batch
		if end > len(ids) {
			end = len(ids)
		}
		idStrs := make([]string, 0, end-start)
		for _, id := range ids[start:end] {
			idStrs = append(idStrs, strconv.Itoa(id))
		}
		u := p.projectPath("/wit/workitems") + "?ids=" + strings.Join(idStrs, ",") +
			"&fields=" + url.QueryEscape(strings.Join(fields, ",")) + "&errorPolicy=omit"
		var resp workItemBatch
		if _, err := p.do(ctx, http.MethodGet, u, "", nil, &resp); err != nil {
			return nil, err
		}
		out = append(out, resp.Value...)
	}
	return out, nil
}

// getWorkItem fetches a single work item with the given fields.
func (p *Provider) getWorkItem(ctx context.Context, id int, fields ...string) (workItem, error) {
	u := p.projectPath("/wit/workitems/"+strconv.Itoa(id)) +
		"?fields=" + url.QueryEscape(strings.Join(fields, ","))
	var wi workItem
	if _, err := p.do(ctx, http.MethodGet, u, "", nil, &wi); err != nil {
		return workItem{}, err
	}
	return wi, nil
}

// workItemStates returns a state-name → category map for a work-item type. The
// category (Proposed/InProgress/Resolved/Completed/Removed) is
// process-independent, so open/closed decisions never hard-code a state name.
func (p *Provider) workItemStates(ctx context.Context, typ string) (map[string]string, error) {
	u := p.projectPath("/wit/workitemtypes/" + url.PathEscape(typ) + "/states")
	var resp wiStates
	if _, err := p.do(ctx, http.MethodGet, u, "", nil, &resp); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(resp.Value))
	for _, s := range resp.Value {
		out[s.Name] = s.Category
	}
	return out, nil
}

// isOpenState reports whether a work item in the given state is open — its state
// category is neither Completed nor Removed. cache memoizes the per-type state
// lookup across a single listing. An unknown state (not in the type's states) is
// treated as open, so a state Oiax cannot classify never hides an artifact.
func (p *Provider) isOpenState(ctx context.Context, typ, state string, cache map[string]map[string]string) (bool, error) {
	states, ok := cache[typ]
	if !ok {
		var err error
		states, err = p.workItemStates(ctx, typ)
		if err != nil {
			return false, err
		}
		cache[typ] = states
	}
	switch states[state] {
	case "Completed", "Removed":
		return false, nil
	default:
		return true, nil
	}
}

// completedState returns the work-item type's state whose category is Completed
// — the correct-by-construction close target across process templates (Agile
// "Closed", Scrum/Basic "Done"). When several qualify, the lowest name sorts
// first for determinism. An error names the type when no such state exists.
func (p *Provider) completedState(ctx context.Context, typ string) (string, error) {
	states, err := p.workItemStates(ctx, typ)
	if err != nil {
		return "", err
	}
	var completed []string
	for name, cat := range states {
		if cat == "Completed" {
			completed = append(completed, name)
		}
	}
	if len(completed) == 0 {
		return "", fmt.Errorf("work-item type %q has no state in the Completed category; cannot close a conflict artifact", typ)
	}
	sort.Strings(completed)
	return completed[0], nil
}

// artifactID parses a ConflictArtifactID into a positive work-item id. Atoi both
// converts and guards the value against path injection.
func artifactID(id forge.ConflictArtifactID) (int, error) {
	n, err := strconv.Atoi(string(id))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid conflict artifact id %q", id)
	}
	return n, nil
}

// RepoMergeMethods reports which merge methods the repository permits. Azure
// DevOps has no repository-level merge-button settings — merge strategy is
// constrained only per-branch, by the "Require a merge strategy" branch policy
// (see TargetMergeMethods) — so absent that policy every method is allowed. It
// reads nothing and never mutates.
func (p *Provider) RepoMergeMethods(ctx context.Context) (forge.MergeMethods, error) {
	return forge.MergeMethods{Merge: true, Squash: true, Rebase: true}, nil
}

// RepoDeletesSourceOnMerge reports whether the repository deletes a merged
// request's source branch automatically. Azure DevOps has no repository-wide
// equivalent of GitHub's delete_branch_on_merge: source-branch deletion is a
// per-request completion option chosen by whoever completes the pull request,
// so there is no repository setting to read and nothing to warn about ahead of
// time. It reports false, reads nothing, and never mutates.
//
// The underlying hazard still exists on Azure DevOps — completing a promotion
// request with "Delete source branch" ticked removes a graph branch just the
// same — but it is a per-completion choice, not a standing misconfiguration,
// so it is documented for operators rather than detected here.
func (p *Provider) RepoDeletesSourceOnMerge(ctx context.Context) (bool, error) {
	return false, nil
}

// policyConfiguration and its settings model the "Require a merge strategy"
// branch policy: which merge strategies a scoped branch permits.
type policyConfiguration struct {
	IsEnabled bool           `json:"isEnabled"`
	Settings  policySettings `json:"settings"`
}

type policySettings struct {
	AllowNoFastForward *bool         `json:"allowNoFastForward"`
	AllowSquash        *bool         `json:"allowSquash"`
	AllowRebase        *bool         `json:"allowRebase"`
	AllowRebaseMerge   *bool         `json:"allowRebaseMerge"`
	UseSquashMerge     *bool         `json:"useSquashMerge"`
	Scope              []policyScope `json:"scope"`
}

type policyScope struct {
	RefName      string `json:"refName"`
	MatchKind    string `json:"matchKind"`
	RepositoryID string `json:"repositoryId"`
}

type policyList struct {
	Value []policyConfiguration `json:"value"`
}

// TargetMergeMethods reports the merge methods permitted for a specific target
// branch: the "Require a merge strategy" branch policy scoped to it, if any. A
// policy that forbids no-fast-forward (merge commits) sets RequiresLinearHistory
// — the blind spot RepoMergeMethods alone cannot see. Absent a policy, every
// method is allowed. It is GET-only and never mutates.
func (p *Provider) TargetMergeMethods(ctx context.Context, branch string) (forge.MergeMethods, error) {
	repoID, err := p.repositoryID(ctx)
	if err != nil {
		return forge.MergeMethods{}, fmt.Errorf("read target merge rules: %w", err)
	}
	u := p.projectPath("/policy/configurations") + "?policyType=" + url.QueryEscape(mergeStrategyPolicyType)
	var list policyList
	if _, err := p.do(ctx, http.MethodGet, u, "", nil, &list); err != nil {
		return forge.MergeMethods{}, fmt.Errorf("read target merge rules: %w", err)
	}
	for _, cfg := range list.Value {
		if !cfg.IsEnabled {
			continue
		}
		for _, sc := range cfg.Settings.Scope {
			if scopeMatches(sc, branch, repoID) {
				return mergeMethodsFromPolicy(cfg.Settings), nil
			}
		}
	}
	return forge.MergeMethods{Merge: true, Squash: true, Rebase: true}, nil
}

// repositoryID resolves the repository's GUID, needed to tell a policy scoped to
// this repository from one scoped to a sibling repository in the same project.
// The GUID is immutable, so it is fetched once and memoized (do already
// exhausts transient retries, so a cached error is non-transient for the life of
// this short-lived reconcile).
func (p *Provider) repositoryID(ctx context.Context) (string, error) {
	p.repoIDOnce.Do(func() {
		var repo struct {
			ID string `json:"id"`
		}
		if _, err := p.do(ctx, http.MethodGet, p.gitPath(""), "", nil, &repo); err != nil {
			p.repoIDErr = err
			return
		}
		p.repoIDVal = repo.ID
	})
	return p.repoIDVal, p.repoIDErr
}

// scopeMatches reports whether a policy scope applies to branch in this
// repository. A scope with an empty repositoryId applies to every repository in
// the project; a set repositoryId must equal ours. The ref is matched Exact or
// Prefix per the scope's matchKind.
func scopeMatches(sc policyScope, branch, repoID string) bool {
	if sc.RepositoryID != "" && !strings.EqualFold(sc.RepositoryID, repoID) {
		return false
	}
	ref := refHead(branch)
	switch strings.ToLower(sc.MatchKind) {
	case "prefix":
		return strings.HasPrefix(ref, sc.RefName)
	case "exact", "":
		return sc.RefName == ref
	default:
		return false
	}
}

// mergeMethodsFromPolicy maps a merge-strategy policy's settings to the allowed
// merge methods. The modern schema exposes the allow* quartet; an older policy
// used a single useSquashMerge boolean. A policy that disallows no-fast-forward
// requires linear history. A policy whose settings express no constraint leaves
// everything allowed.
func mergeMethodsFromPolicy(s policySettings) forge.MergeMethods {
	if s.AllowNoFastForward != nil || s.AllowSquash != nil || s.AllowRebase != nil || s.AllowRebaseMerge != nil {
		merge := boolValue(s.AllowNoFastForward)
		return forge.MergeMethods{
			Merge:                 merge,
			Squash:                boolValue(s.AllowSquash),
			Rebase:                boolValue(s.AllowRebase) || boolValue(s.AllowRebaseMerge),
			RequiresLinearHistory: !merge,
		}
	}
	if s.UseSquashMerge != nil {
		if *s.UseSquashMerge {
			return forge.MergeMethods{Squash: true, RequiresLinearHistory: true}
		}
		return forge.MergeMethods{Merge: true}
	}
	return forge.MergeMethods{Merge: true, Squash: true, Rebase: true}
}

// boolValue dereferences an optional bool, treating absent as false.
func boolValue(b *bool) bool {
	return b != nil && *b
}
