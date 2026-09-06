package github

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/skaphos/oiax/v2/internal/forge"
	"github.com/skaphos/oiax/v2/internal/notification"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

type notificationCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
	} `json:"commit"`
}

// GetCommitSnapshot reads the completed PR's review membership, not a moving
// branch. GitHub does not expose a historical first PR iteration: pre-POST
// origin OIDs alone cannot establish creation membership, even if they match
// today's head. Such creation details are deliberately unavailable.
func (p *Provider) GetCommitSnapshot(ctx context.Context, req notification.LifecycleRequest, rev forge.EventRevision) (notification.CommitSnapshot, error) {
	unavailable := notification.CommitSnapshot{CommitsUnavailable: true}
	if rev.Kind != v1.NotificationRequestMerged || req.State != notification.LifecycleMerged {
		return unavailable, nil
	}
	confirmed, err := p.GetLifecycleRequest(ctx, forge.RequestID(req.Request.ID))
	if err != nil || confirmed.State != notification.LifecycleMerged || !confirmed.Repository.Same(req.Repository) || confirmed.Graph != req.Graph || confirmed.Request != req.Request || !confirmed.MergedAt.Equal(req.MergedAt) {
		return unavailable, notification.ErrLifecycleUnavailable
	}
	if !notification.ValidOID(confirmed.SourceOID) || !notification.ValidOID(confirmed.BaseOID) || !notification.ValidOID(confirmed.MergeResultOID) {
		return unavailable, nil
	}
	if (rev.SourceOID != "" && rev.SourceOID != confirmed.SourceOID) || (rev.MergeResultOID != "" && rev.MergeResultOID != confirmed.MergeResultOID) {
		return unavailable, nil
	}
	number, err := strconv.Atoi(req.Request.ID)
	if err != nil || number <= 0 {
		return unavailable, notification.ErrLifecycleUnavailable
	}
	before, err := p.getPull(ctx, number)
	if err != nil || before.Commits == nil || *before.Commits < 0 || before.Head.SHA != confirmed.SourceOID || before.MergeCommitSHA != confirmed.MergeResultOID {
		return unavailable, nil
	}
	var commits []notificationCommit
	endpoint := p.url("/repos/" + url.PathEscape(p.Owner) + "/" + url.PathEscape(p.Repo) + "/pulls/" + strconv.Itoa(number) + "/commits?per_page=100&page=1")
	if _, err := p.do(ctx, http.MethodGet, endpoint, nil, &commits); err != nil || len(commits) > notification.MaxCommits {
		return p.immutableMergeSnapshot(ctx, confirmed, *before.Commits)
	}
	if len(commits) != min(*before.Commits, notification.MaxCommits) {
		return unavailable, nil
	}
	after, err := p.getPull(ctx, number)
	if err != nil || after.MergedAt == nil || after.State != "closed" || after.Head.SHA != before.Head.SHA || after.Base.SHA != before.Base.SHA || after.MergeCommitSHA != before.MergeCommitSHA || after.Commits == nil || *after.Commits != *before.Commits {
		return unavailable, nil
	}
	snapshot := notification.CommitSnapshot{SourceOID: confirmed.SourceOID, BaseOID: confirmed.BaseOID, MergeResultOID: confirmed.MergeResultOID, CommitCount: *before.Commits, CommitCountKnown: true}
	for _, commit := range commits {
		snapshot.Commits = append(snapshot.Commits, notification.CommitSummary{SHA: commit.SHA, Subject: commit.Commit.Message})
	}
	return notification.BoundSnapshot(snapshot), nil
}

// A real two-parent merge gives an immutable pre-merge base and reviewed head.
// Squash/rebase results do not supply that proof and must not use this fallback.
func (p *Provider) immutableMergeSnapshot(ctx context.Context, req notification.LifecycleRequest, total int) (notification.CommitSnapshot, error) {
	unavailable := notification.CommitSnapshot{CommitsUnavailable: true}
	root := "/repos/" + url.PathEscape(p.Owner) + "/" + url.PathEscape(p.Repo)
	var merged struct {
		SHA     string `json:"sha"`
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	if _, err := p.do(ctx, http.MethodGet, p.url(root+"/commits/"+req.MergeResultOID), nil, &merged); err != nil || merged.SHA != req.MergeResultOID || len(merged.Parents) != 2 || merged.Parents[1].SHA != req.SourceOID || !notification.ValidOID(merged.Parents[0].SHA) {
		return unavailable, notification.ErrLifecycleUnavailable
	}
	var comparison struct {
		Total   *int                 `json:"total_commits"`
		Commits []notificationCommit `json:"commits"`
	}
	base := merged.Parents[0].SHA
	if _, err := p.do(ctx, http.MethodGet, p.url(root+"/compare/"+base+"..."+req.SourceOID+"?per_page=100&page=1"), nil, &comparison); err != nil || comparison.Total == nil || *comparison.Total != total || len(comparison.Commits) != min(total, notification.MaxCommits) {
		return unavailable, notification.ErrLifecycleUnavailable
	}
	snapshot := notification.CommitSnapshot{SourceOID: req.SourceOID, BaseOID: base, MergeResultOID: req.MergeResultOID, CommitCount: total, CommitCountKnown: true}
	for _, commit := range comparison.Commits {
		snapshot.Commits = append(snapshot.Commits, notification.CommitSummary{SHA: commit.SHA, Subject: commit.Commit.Message})
	}
	return notification.BoundSnapshot(snapshot), nil
}

var _ forge.SnapshotReader = (*Provider)(nil)
