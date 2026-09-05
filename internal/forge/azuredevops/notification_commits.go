package azuredevops

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/notification"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

type adoCommitRef struct {
	CommitID         string `json:"commitId"`
	Comment          string `json:"comment"`
	CommentTruncated bool   `json:"commentTruncated"`
}

func (p *Provider) GetCommitSnapshot(ctx context.Context, req notification.LifecycleRequest, rev forge.EventRevision) (notification.CommitSnapshot, error) {
	unavailable := notification.CommitSnapshot{CommitsUnavailable: true}
	confirmed, err := p.GetLifecycleRequest(ctx, forge.RequestID(req.Request.ID))
	if err != nil || !confirmed.Repository.Same(req.Repository) || confirmed.Graph != req.Graph || confirmed.Request != req.Request {
		return unavailable, notification.ErrLifecycleUnavailable
	}
	snapshot := notification.CommitSnapshot{}
	switch rev.Kind {
	case v1.NotificationRequestCreated:
		var iteration struct {
			ID     int          `json:"id"`
			Source adoCommitRef `json:"sourceRefCommit"`
			Target adoCommitRef `json:"targetRefCommit"`
		}
		endpoint := p.gitPath("/pullrequests/" + url.PathEscape(req.Request.ID) + "/iterations/1")
		if _, err := p.do(ctx, http.MethodGet, endpoint, "", nil, &iteration); err != nil || iteration.ID != 1 {
			return unavailable, notification.ErrLifecycleUnavailable
		}
		// First-iteration server evidence supersedes potentially raced origin hints.
		snapshot.SourceOID, snapshot.BaseOID = iteration.Source.CommitID, iteration.Target.CommitID
	case v1.NotificationRequestMerged:
		if confirmed.State != notification.LifecycleMerged || !confirmed.MergedAt.Equal(req.MergedAt) {
			return unavailable, nil
		}
		snapshot.SourceOID, snapshot.BaseOID, snapshot.MergeResultOID = confirmed.SourceOID, confirmed.BaseOID, confirmed.MergeResultOID
		if (rev.SourceOID != "" && rev.SourceOID != snapshot.SourceOID) || (rev.MergeResultOID != "" && rev.MergeResultOID != snapshot.MergeResultOID) {
			return unavailable, nil
		}
	default:
		return unavailable, nil
	}
	if !notification.ValidOID(snapshot.SourceOID) || !notification.ValidOID(snapshot.BaseOID) {
		return unavailable, nil
	}
	query := url.Values{"searchCriteria.itemVersion.version": {snapshot.SourceOID}, "searchCriteria.itemVersion.versionType": {"commit"}, "searchCriteria.compareVersion.version": {snapshot.BaseOID}, "searchCriteria.compareVersion.versionType": {"commit"}, "searchCriteria.$top": {"101"}, "searchCriteria.$skip": {"0"}}
	// A bounded lookahead establishes truncation, but page count is not a total.
	for page := 0; page < 2 && len(snapshot.Commits) <= notification.MaxCommits; page++ {
		query.Set("searchCriteria.$skip", strconv.Itoa(len(snapshot.Commits)))
		query.Set("searchCriteria.$top", strconv.Itoa(101-len(snapshot.Commits)))
		var result struct {
			Value []adoCommitRef `json:"value"`
		}
		headers, err := p.do(ctx, http.MethodGet, p.gitPath("/commits")+"?"+query.Encode(), "", nil, &result)
		if err != nil || len(result.Value) > 101-len(snapshot.Commits) {
			return unavailable, notification.ErrLifecycleUnavailable
		}
		for _, commit := range result.Value {
			snapshot.Commits = append(snapshot.Commits, notification.CommitSummary{SHA: commit.CommitID, Subject: commit.Comment})
			snapshot.CommitsTruncated = snapshot.CommitsTruncated || commit.CommentTruncated
		}
		if headers.Get("x-ms-continuationtoken") == "" {
			break
		}
		snapshot.CommitsTruncated = true
		if len(result.Value) == 0 {
			break
		}
	}
	return notification.BoundSnapshot(snapshot), nil
}

var _ forge.SnapshotReader = (*Provider)(nil)
