package azuredevops

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/notification"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

func (p *Provider) RepositoryIdentity(ctx context.Context) (notification.RepositoryIdentity, error) {
	p.notificationIdentityOnce.Do(func() {
		var repo struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Project struct {
				ID string `json:"id"`
			} `json:"project"`
		}
		var connection struct {
			InstanceID string `json:"instanceId"`
		}
		if _, err := p.do(ctx, http.MethodGet, p.gitPath(""), "", nil, &repo); err != nil {
			p.notificationIdentityError = notification.ErrLifecycleUnavailable
			return
		}
		if _, err := p.do(ctx, http.MethodGet, p.apiBase()+"/_apis/connectionData", "", nil, &connection); err != nil || connection.InstanceID == "" || repo.Project.ID == "" || repo.ID == "" {
			p.notificationIdentityError = notification.ErrLifecycleUnavailable
			return
		}
		p.notificationIdentityValue = notification.RepositoryIdentity{Provider: "azuredevops", Host: "dev.azure.com", ID: strings.ToLower(repo.ID), OrganizationID: strings.ToLower(connection.InstanceID), ProjectID: strings.ToLower(repo.Project.ID), Name: p.Repo.Organization + "/" + p.Repo.Project + "/" + repo.Name}
	})
	return p.notificationIdentityValue, p.notificationIdentityError
}

type notificationInterval struct {
	From    time.Time `json:"from"`
	Through time.Time `json:"through"`
}
type notificationCursor struct {
	Version int                    `json:"version"`
	Kind    v1.NotificationEvent   `json:"kind"`
	Pending []notificationInterval `json:"pending"`
}

// ListLifecyclePage partitions a frozen interval instead of trusting $skip
// ordering. A partition must fit strictly below the page limit and produce an
// identical ID set twice before its complete details are admitted. Dense equal-
// timestamp partitions fail closed, retaining their cursor for later recovery.
func (p *Provider) ListLifecyclePage(ctx context.Context, q forge.LifecycleQuery) (forge.LifecyclePage, error) {
	result := forge.LifecyclePage{Progress: notification.ScanProgress{Version: 1, From: q.From, Through: q.Through}}
	if q.Graph == "" || q.Through.IsZero() || q.Limit < 0 || q.Limit > 100 {
		return result, notification.ErrDiscoveryIncomplete
	}
	kind := q.Kind
	if kind == "" {
		kind = v1.NotificationRequestCreated
	}
	if kind != v1.NotificationRequestCreated && kind != v1.NotificationRequestMerged {
		return result, notification.ErrDiscoveryIncomplete
	}
	from := q.From
	if from.IsZero() {
		from = time.Unix(0, 0).UTC()
	}
	cursor := notificationCursor{Version: 1, Kind: kind, Pending: []notificationInterval{{From: from, Through: q.Through}}}
	if q.Cursor != "" {
		if len(q.Cursor) > 16384 || json.Unmarshal([]byte(q.Cursor), &cursor) != nil || cursor.Version != 1 || cursor.Kind != kind || len(cursor.Pending) == 0 || len(cursor.Pending) > 64 {
			return result, notification.ErrDiscoveryIncomplete
		}
	}
	save := func() { data, _ := json.Marshal(cursor); result.Progress.Cursor = string(data) }
	interval := cursor.Pending[0]
	if interval.From.Before(from) || interval.Through.After(q.Through) || interval.Through.Before(interval.From) {
		return result, notification.ErrDiscoveryIncomplete
	}
	status, timeKind := "all", "created"
	if kind == v1.NotificationRequestMerged {
		status, timeKind = "completed", "closed"
	}
	endpoint := p.gitPath("/pullrequests") + "?searchCriteria.status=" + status + "&searchCriteria.queryTimeRangeType=" + timeKind + "&searchCriteria.minTime=" + url.QueryEscape(interval.From.UTC().Format(time.RFC3339Nano)) + "&searchCriteria.maxTime=" + url.QueryEscape(interval.Through.UTC().Format(time.RFC3339Nano)) + "&$top=100&$skip=0"
	list := func() ([]adoPull, error) {
		var result adoPullList
		_, err := p.do(ctx, http.MethodGet, endpoint, "", nil, &result)
		return result.Value, err
	}
	first, err := list()
	result.Pages++
	if err != nil || len(first) > 100 {
		save()
		return result, notification.ErrDiscoveryIncomplete
	}
	if len(first) == 100 {
		if interval.Through.Sub(interval.From) <= time.Millisecond || len(cursor.Pending) >= 64 {
			save()
			return result, notification.ErrDiscoveryIncomplete
		}
		mid := interval.From.Add(interval.Through.Sub(interval.From) / 2).Truncate(time.Millisecond)
		if !mid.After(interval.From) || !mid.Before(interval.Through) {
			save()
			return result, notification.ErrDiscoveryIncomplete
		}
		cursor.Pending = append([]notificationInterval{{From: interval.From, Through: mid}, {From: mid, Through: interval.Through}}, cursor.Pending[1:]...)
		save()
		return result, nil
	}
	second, err := list()
	result.Pages++
	ids := func(pulls []adoPull) []int {
		out := make([]int, 0, len(pulls))
		for _, pr := range pulls {
			out = append(out, pr.PullRequestID)
		}
		sort.Ints(out)
		return out
	}
	if err != nil || !slices.Equal(ids(first), ids(second)) {
		save()
		return result, notification.ErrDiscoveryIncomplete
	}
	seen := map[int]bool{}
	for _, pr := range second {
		if pr.PullRequestID <= 0 || seen[pr.PullRequestID] {
			save()
			return result, notification.ErrDiscoveryIncomplete
		}
		seen[pr.PullRequestID] = true
		r, err := p.GetLifecycleRequest(ctx, forge.RequestID(strconv.Itoa(pr.PullRequestID)))
		if errors.Is(err, notification.ErrNotManaged) {
			continue
		}
		if err != nil {
			save()
			return result, notification.ErrDiscoveryIncomplete
		}
		when := r.CreatedAt
		if kind == v1.NotificationRequestMerged {
			when = r.MergedAt
		}
		if r.Graph != q.Graph || when.Before(interval.From) || when.After(interval.Through) {
			continue
		}
		result.Requests = append(result.Requests, r)
	}
	cursor.Pending = cursor.Pending[1:]
	result.Progress.Complete = len(cursor.Pending) == 0
	if !result.Progress.Complete {
		save()
	}
	sort.Slice(result.Requests, func(i, j int) bool { return result.Requests[i].Request.ID < result.Requests[j].Request.ID })
	return result, nil
}

func (p *Provider) GetLifecycleRequest(ctx context.Context, id forge.RequestID) (notification.LifecycleRequest, error) {
	number, err := strconv.Atoi(string(id))
	if err != nil || number <= 0 {
		return notification.LifecycleRequest{}, notification.ErrRequestMissing
	}
	pr, m, err := p.managedRequest(ctx, number)
	if err != nil {
		if errors.Is(err, errNotManaged) {
			return notification.LifecycleRequest{}, notification.ErrNotManaged
		}
		var api *apiError
		if errors.As(err, &api) && api.StatusCode == 404 {
			return notification.LifecycleRequest{}, notification.ErrRequestMissing
		}
		return notification.LifecycleRequest{}, notification.ErrLifecycleUnavailable
	}
	if m.Type != "promotion" && m.Type != "backflow" {
		return notification.LifecycleRequest{}, notification.ErrNotManaged
	}
	repo, err := p.RepositoryIdentity(ctx)
	if err != nil {
		return notification.LifecycleRequest{}, err
	}
	created, err := time.Parse(time.RFC3339Nano, pr.CreationDate)
	if err != nil {
		return notification.LifecycleRequest{}, notification.ErrLifecycleUnavailable
	}
	r := notification.LifecycleRequest{Repository: repo, Graph: m.Graph, CreatedAt: created.UTC(), Request: notification.RequestV1{ID: strconv.Itoa(pr.PullRequestID), Type: v1.NotificationRequestType(m.Type), Source: m.Source, Destination: m.Destination, URL: "https://dev.azure.com/" + url.PathEscape(p.Repo.Organization) + "/" + url.PathEscape(p.Repo.Project) + "/_git/" + url.PathEscape(p.Repo.Name) + "/pullrequest/" + strconv.Itoa(pr.PullRequestID)}}
	if m.Type == "promotion" {
		r.Request.LogicalSource = m.Source
		r.Request.LogicalDestination = m.Destination
	}
	switch pr.Status {
	case "active":
		r.State = notification.LifecycleOpen
	case "abandoned":
		r.State = notification.LifecycleClosed
	case "completed":
		merged, err := time.Parse(time.RFC3339Nano, pr.ClosedDate)
		if err != nil {
			return notification.LifecycleRequest{}, notification.ErrLifecycleUnavailable
		}
		r.State = notification.LifecycleMerged
		r.MergedAt = merged.UTC()
	default:
		return notification.LifecycleRequest{}, notification.ErrLifecycleUnavailable
	}
	return r, nil
}

var _ forge.LifecycleReader = (*Provider)(nil)
