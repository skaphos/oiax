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

// GetCommitSnapshot reads immutable event membership, never a moving branch.
// Merges use the completed PR's review membership. Creation uses the request's
// own membership while it still stands at the head Oiax opened it at, so both
// kinds report what the forge attests and neither takes evidence from request
// text (see creationSnapshot).
func (p *Provider) GetCommitSnapshot(ctx context.Context, req notification.LifecycleRequest, rev forge.EventRevision) (notification.CommitSnapshot, error) {
	unavailable := notification.CommitSnapshot{CommitsUnavailable: true}
	if rev.Kind == v1.NotificationRequestCreated {
		return p.creationSnapshot(ctx, req)
	}
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
	commits, _, err := p.requestCommits(ctx, number)
	if err != nil {
		return p.immutableMergeSnapshot(ctx, confirmed, *before.Commits)
	}
	if len(commits) != min(*before.Commits, notification.MaxCommits) {
		return unavailable, nil
	}
	after, err := p.getPull(ctx, number)
	if err != nil || after.MergedAt == nil || after.State != "closed" || after.Head.SHA != before.Head.SHA || after.Base.SHA != before.Base.SHA || after.MergeCommitSHA != before.MergeCommitSHA || after.Commits == nil || *after.Commits != *before.Commits {
		return unavailable, nil
	}
	snapshot := notification.CommitSnapshot{SourceOID: confirmed.SourceOID, BaseOID: confirmed.BaseOID, MergeResultOID: confirmed.MergeResultOID, CommitCount: *before.Commits, CommitCountKnown: true, Commits: commitSummaries(commits)}
	return notification.BoundSnapshot(snapshot), nil
}

// creationSnapshot reports the commits the request proposes, as GitHub itself
// reports them. The origin block lives in the request body, which anyone with
// write access can rewrite, so it decides only whether this is still the
// request Oiax opened: it must be recoverable and unchanged, and the head the
// forge reports must still be the OID the creating run opened at. A head that
// has moved leaves creation membership unattested rather than describing a
// later range. Membership, its bound and its total then come from the request's
// own commit page and never from the body, so an edited block cannot list a
// range the request does not contain or claim a total GitHub did not report.
func (p *Provider) creationSnapshot(ctx context.Context, req notification.LifecycleRequest) (notification.CommitSnapshot, error) {
	unavailable := notification.CommitSnapshot{CommitsUnavailable: true}
	if req.Origin == nil || !notification.ValidOrigin(*req.Origin) {
		return unavailable, nil
	}
	number, err := strconv.Atoi(req.Request.ID)
	if err != nil || number <= 0 {
		return unavailable, notification.ErrLifecycleUnavailable
	}
	confirmed, err := p.GetLifecycleRequest(ctx, forge.RequestID(req.Request.ID))
	if err != nil || confirmed.Origin == nil || *confirmed.Origin != *req.Origin || !confirmed.Repository.Same(req.Repository) || confirmed.Graph != req.Graph || confirmed.Request != req.Request {
		return unavailable, notification.ErrLifecycleUnavailable
	}
	// confirmed's OIDs are the forge's head and base for the request, not body
	// text; the origin only has to agree with them.
	if confirmed.SourceOID != confirmed.Origin.SourceOID || !notification.ValidOID(confirmed.SourceOID) || !notification.ValidOID(confirmed.BaseOID) {
		return unavailable, nil
	}
	commits, truncated, err := p.requestCommits(ctx, number)
	if err != nil {
		return unavailable, notification.ErrLifecycleUnavailable
	}
	snapshot := notification.CommitSnapshot{SourceOID: confirmed.SourceOID, BaseOID: confirmed.BaseOID, Commits: commitSummaries(commits), CommitsTruncated: truncated}
	if !truncated {
		// A complete page is the whole membership; a truncated one is a page
		// count, never a total.
		snapshot.CommitCount, snapshot.CommitCountKnown = len(commits), true
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
	base := merged.Parents[0].SHA
	compared, commits, err := p.compareCommits(ctx, base, req.SourceOID)
	if err != nil || compared != total {
		return unavailable, notification.ErrLifecycleUnavailable
	}
	snapshot := notification.CommitSnapshot{SourceOID: req.SourceOID, BaseOID: base, MergeResultOID: req.MergeResultOID, CommitCount: total, CommitCountKnown: true, Commits: commitSummaries(commits)}
	return notification.BoundSnapshot(snapshot), nil
}

// requestCommits lists the commits a request proposes from its own commit page,
// bounded to one page of MaxCommits. The range is GitHub's, derived from the
// request's refs rather than from any OID a caller supplies. A continuation
// link reports truncation without inventing a total for what it did not read.
func (p *Provider) requestCommits(ctx context.Context, number int) ([]notificationCommit, bool, error) {
	var commits []notificationCommit
	endpoint := p.url("/repos/" + url.PathEscape(p.Owner) + "/" + url.PathEscape(p.Repo) + "/pulls/" + strconv.Itoa(number) + "/commits?per_page=" + strconv.Itoa(notification.MaxCommits) + "&page=1")
	headers, err := p.do(ctx, http.MethodGet, endpoint, nil, &commits)
	if err != nil || len(commits) > notification.MaxCommits {
		return nil, false, notification.ErrLifecycleUnavailable
	}
	return commits, nextLink(headers.Get("Link")) != "", nil
}

// compareCommits lists base..head from the compare endpoint, bounded to one
// page of MaxCommits. The reported total is returned so callers can tell a
// complete page from a truncated one; a page inconsistent with it is an error.
func (p *Provider) compareCommits(ctx context.Context, base, head string) (int, []notificationCommit, error) {
	var comparison struct {
		Total   *int                 `json:"total_commits"`
		Commits []notificationCommit `json:"commits"`
	}
	endpoint := p.url("/repos/" + url.PathEscape(p.Owner) + "/" + url.PathEscape(p.Repo) + "/compare/" + base + "..." + head + "?per_page=" + strconv.Itoa(notification.MaxCommits) + "&page=1")
	if _, err := p.do(ctx, http.MethodGet, endpoint, nil, &comparison); err != nil {
		return 0, nil, err
	}
	if comparison.Total == nil || *comparison.Total < 0 || len(comparison.Commits) != min(*comparison.Total, notification.MaxCommits) {
		return 0, nil, notification.ErrLifecycleUnavailable
	}
	return *comparison.Total, comparison.Commits, nil
}

func commitSummaries(commits []notificationCommit) []notification.CommitSummary {
	summaries := make([]notification.CommitSummary, 0, len(commits))
	for _, commit := range commits {
		summaries = append(summaries, notification.CommitSummary{SHA: commit.SHA, Subject: commit.Commit.Message})
	}
	return summaries
}

var _ forge.SnapshotReader = (*Provider)(nil)
