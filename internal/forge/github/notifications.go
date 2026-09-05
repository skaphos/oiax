package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/skaphos/oiax/internal/forge"
	mk "github.com/skaphos/oiax/internal/forge/marker"
	"github.com/skaphos/oiax/internal/notification"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

func (p *Provider) RepositoryIdentity(ctx context.Context) (notification.RepositoryIdentity, error) {
	p.notificationIdentityOnce.Do(func() {
		var repo struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		}
		if _, err := p.do(ctx, http.MethodGet, p.url("/repos/"+url.PathEscape(p.Owner)+"/"+url.PathEscape(p.Repo)), nil, &repo); err != nil || repo.ID <= 0 || repo.FullName == "" {
			p.notificationIdentityError = notification.ErrLifecycleUnavailable
			return
		}
		p.notificationIdentityValue = notification.RepositoryIdentity{Provider: "github", Host: gitRemoteHost, ID: strconv.FormatInt(repo.ID, 10), Name: repo.FullName}
	})
	return p.notificationIdentityValue, p.notificationIdentityError
}

// ListLifecyclePage walks all requests by immutable creation order. It never
// uses baseline lookback or updated/merged-order heuristics. The preceding page
// overlaps each continuation to recover boundary movement, with stable-ID dedup.
func (p *Provider) ListLifecyclePage(ctx context.Context, query forge.LifecycleQuery) (forge.LifecyclePage, error) {
	result := forge.LifecyclePage{Progress: notification.ScanProgress{Version: 1, From: query.From, Through: query.Through}}
	kind := query.Kind
	if kind == "" {
		kind = v1.NotificationRequestCreated
	}
	if kind != v1.NotificationRequestCreated && kind != v1.NotificationRequestMerged {
		return result, notification.ErrDiscoveryIncomplete
	}
	from := query.From
	if from.IsZero() {
		from = time.Unix(0, 0).UTC()
	}
	page := 1
	if query.Cursor != "" {
		var err error
		page, err = strconv.Atoi(query.Cursor)
		if err != nil || page < 1 || page > 1000000 {
			return result, notification.ErrDiscoveryIncomplete
		}
	}
	if query.Through.IsZero() || query.Graph == "" || query.Limit < 0 || query.Limit > 100 {
		return result, notification.ErrDiscoveryIncomplete
	}
	limit := query.Limit
	if limit == 0 {
		limit = 100
	}
	seen := map[string]bool{}
	for number := max(1, page-1); number <= page; number++ {
		var pulls []ghPull
		endpoint := p.url(fmt.Sprintf("/repos/%s/%s/pulls?state=all&sort=created&direction=asc&per_page=%d&page=%d", url.PathEscape(p.Owner), url.PathEscape(p.Repo), limit, number))
		headers, err := p.do(ctx, http.MethodGet, endpoint, nil, &pulls)
		result.Pages++
		if err != nil || len(pulls) > limit {
			result.Progress.Cursor = strconv.Itoa(page)
			return result, notification.ErrDiscoveryIncomplete
		}
		for _, listed := range pulls {
			id := strconv.Itoa(listed.Number)
			if listed.Number <= 0 || seen[id] {
				continue
			}
			seen[id] = true
			// The list representation is only an index. Ownership and lifecycle
			// facts come from the full detail response so a missing/truncated body
			// can never silently hide a managed request.
			req, err := p.GetLifecycleRequest(ctx, forge.RequestID(id))
			if errors.Is(err, notification.ErrNotManaged) {
				continue
			}
			if err != nil {
				result.Progress.Cursor = strconv.Itoa(page)
				return result, notification.ErrDiscoveryIncomplete
			}
			when := req.CreatedAt
			if kind == v1.NotificationRequestMerged {
				if req.State != notification.LifecycleMerged {
					continue
				}
				when = req.MergedAt
			}
			if req.Graph != query.Graph || when.Before(from) || when.After(query.Through) {
				continue
			}
			result.Requests = append(result.Requests, req)
		}
		if number == page {
			next := nextLink(headers.Get("Link"))
			if next != "" && !p.sameOrigin(next) {
				result.Progress.Cursor = strconv.Itoa(page)
				return result, notification.ErrDiscoveryIncomplete
			}
			result.Progress.Complete = next == ""
			if !result.Progress.Complete {
				result.Progress.Cursor = strconv.Itoa(page + 1)
			}
		}
	}
	sort.Slice(result.Requests, func(i, j int) bool { return result.Requests[i].Request.ID < result.Requests[j].Request.ID })
	return result, nil
}

func (p *Provider) GetLifecycleRequest(ctx context.Context, id forge.RequestID) (notification.LifecycleRequest, error) {
	number, err := strconv.Atoi(string(id))
	if err != nil || number <= 0 {
		return notification.LifecycleRequest{}, notification.ErrRequestMissing
	}
	pr, err := p.getPull(ctx, number)
	if err != nil {
		var api *apiError
		if errors.As(err, &api) && api.StatusCode == 404 {
			return notification.LifecycleRequest{}, notification.ErrRequestMissing
		}
		return notification.LifecycleRequest{}, notification.ErrLifecycleUnavailable
	}
	m, ok := managedMarker(pr)
	if !ok || pr.Head.Repo.FullName == "" || (m.Type != "promotion" && m.Type != "backflow") {
		return notification.LifecycleRequest{}, notification.ErrNotManaged
	}
	repo, err := p.RepositoryIdentity(ctx)
	if err != nil {
		return notification.LifecycleRequest{}, err
	}
	if !strings.EqualFold(pr.Base.Repo.FullName, repo.Name) {
		return notification.LifecycleRequest{}, notification.ErrNotManaged
	}
	created, err := time.Parse(time.RFC3339Nano, pr.CreatedAt)
	if err != nil {
		return notification.LifecycleRequest{}, notification.ErrLifecycleUnavailable
	}
	r := notification.LifecycleRequest{Repository: repo, Graph: m.Graph, CreatedAt: created.UTC(), SourceOID: pr.Head.SHA, BaseOID: pr.Base.SHA, MergeResultOID: pr.MergeCommitSHA,
		Request: notification.RequestV1{ID: strconv.Itoa(pr.Number), Type: v1.NotificationRequestType(m.Type), Source: pr.Head.Ref, Destination: pr.Base.Ref, URL: "https://" + gitRemoteHost + "/" + url.PathEscape(p.Owner) + "/" + url.PathEscape(p.Repo) + "/pull/" + strconv.Itoa(pr.Number)}}
	if m.Type == "promotion" {
		r.Request.LogicalSource = pr.Head.Ref
		r.Request.LogicalDestination = pr.Base.Ref
	}
	if origin, ok := mk.ParseNotificationOrigin(pr.Body); ok && mk.NotificationOriginMatches(origin, m) {
		r.Origin = &origin
		r.Request.LogicalSource, r.Request.LogicalDestination = origin.LogicalSource, origin.LogicalTarget
	}
	switch pr.State {
	case "open":
		r.State = notification.LifecycleOpen
	case "closed":
		r.State = notification.LifecycleClosed
		if pr.MergedAt != nil {
			merged, err := time.Parse(time.RFC3339Nano, *pr.MergedAt)
			if err != nil {
				return notification.LifecycleRequest{}, notification.ErrLifecycleUnavailable
			}
			r.State = notification.LifecycleMerged
			r.MergedAt = merged.UTC()
		}
	default:
		return notification.LifecycleRequest{}, notification.ErrLifecycleUnavailable
	}
	return r, nil
}

var _ forge.LifecycleReader = (*Provider)(nil)
