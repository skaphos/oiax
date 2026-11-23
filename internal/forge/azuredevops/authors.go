package azuredevops

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

// Author provenance (ADR 0020), the Azure DevOps half.
//
// A managed request's marker (description or PR property) and its source and
// target refs are chosen by whoever opens or edits the pull request, and a
// promotion request's source is an existing long-lived branch. The pull
// request's creator is the one field its author cannot choose, so recognition
// also requires createdBy to be the identity Oiax itself runs as, or one the
// operator has explicitly declared trusted. Identities are compared by their
// immutable GUID, never by display or unique name (both are user-editable or
// reusable).

// identityPattern is an Azure DevOps identity id: a GUID.
var identityPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// errNoIdentity means neither the token's own identity nor an
// operator-declared author is available, so no request can be attributed to
// Oiax.
var errNoIdentity = errors.New("cannot determine which pull request authors are oiax: the token identity lookup failed and OIAX_TRUSTED_AUTHORS is not set")

// trustedCreator reports whether pr was opened by a trusted Oiax identity. A
// request that carries a well-formed marker but fails this check is reported
// once as a warning and treated as any other unmanaged pull request.
func (p *Provider) trustedCreator(ctx context.Context, pr adoPull) (bool, error) {
	if p.anyAuthor {
		return true, nil
	}
	trusted, err := p.trustedAuthors(ctx)
	if err != nil {
		return false, err
	}
	id := strings.ToLower(pr.CreatedBy.ID)
	if _, ok := trusted[id]; id != "" && ok {
		return true, nil
	}
	p.warnUntrusted(pr, trusted)
	return false, nil
}

// trustedAuthors returns the lower-cased identity ids whose pull requests may
// be Oiax's own: the token's identity plus Provider.TrustedAuthors. Resolved on
// first need and cached only on success, so a transient failure or a cancelled
// context does not poison the rest of the process.
func (p *Provider) trustedAuthors(ctx context.Context) (map[string]struct{}, error) {
	p.authorsMu.Lock()
	defer p.authorsMu.Unlock()
	if p.authors != nil {
		return p.authors, nil
	}
	set := map[string]struct{}{}
	for _, id := range p.TrustedAuthors {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if !identityPattern.MatchString(id) {
			return nil, fmt.Errorf("invalid trusted author %q (want an Azure DevOps identity id, a GUID)", id)
		}
		set[strings.ToLower(id)] = struct{}{}
	}
	self, err := p.authenticatedID(ctx)
	switch {
	case err == nil:
		set[strings.ToLower(self)] = struct{}{}
	case len(set) == 0:
		return nil, fmt.Errorf("%w: %w", errNoIdentity, err)
	default:
		if p.Warn != nil {
			p.Warn("could not look up the token's own identity; recognizing managed requests by OIAX_TRUSTED_AUTHORS only")
		}
	}
	p.authors = set
	return set, nil
}

// authenticatedID asks Azure DevOps which identity the token acts as — the
// identity it records as createdBy on a pull request the token opens.
func (p *Provider) authenticatedID(ctx context.Context) (string, error) {
	var connection struct {
		AuthenticatedUser struct {
			ID string `json:"id"`
		} `json:"authenticatedUser"`
	}
	if _, err := p.do(ctx, http.MethodGet, p.apiBase()+"/_apis/connectionData", "", nil, &connection); err != nil {
		return "", fmt.Errorf("look up token identity: %w", err)
	}
	id := connection.AuthenticatedUser.ID
	if !identityPattern.MatchString(id) {
		return "", errors.New("look up token identity: no usable identity id in response")
	}
	return id, nil
}

// warnUntrusted reports, once per pull request, a request that carries a
// well-formed Oiax marker but was not opened by a trusted identity: either
// someone hand-wrote a marker, or the token identity changed and the previous
// one needs declaring. The creator id is echoed only when it is a GUID.
func (p *Provider) warnUntrusted(pr adoPull, trusted map[string]struct{}) {
	if p.Warn == nil {
		return
	}
	p.authorsMu.Lock()
	if p.untrustedWarned == nil {
		p.untrustedWarned = map[int]struct{}{}
	}
	_, seen := p.untrustedWarned[pr.PullRequestID]
	p.untrustedWarned[pr.PullRequestID] = struct{}{}
	p.authorsMu.Unlock()
	if seen {
		return
	}
	creator := "an unrecognized identity"
	if identityPattern.MatchString(pr.CreatedBy.ID) {
		creator = "identity " + strings.ToLower(pr.CreatedBy.ID)
	}
	ids := make([]string, 0, len(trusted))
	for id := range trusted {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	p.Warn(fmt.Sprintf(
		"pull request %d carries an oiax marker but was created by %s, not a trusted oiax identity (%s); treating it as unmanaged. "+
			"If it was created by a previous oiax token, add that identity id to OIAX_TRUSTED_AUTHORS.",
		pr.PullRequestID, creator, strings.Join(ids, ", ")))
}

// warnForeignAuthor fires when a request Oiax just created is recorded with a
// creator outside the trusted set — the token-identity lookup and the forge's
// recorded creator disagree. Left silent, the next run would not recognize its
// own request.
func (p *Provider) warnForeignAuthor(ctx context.Context, created adoPull) {
	if p.anyAuthor || p.Warn == nil || !identityPattern.MatchString(created.CreatedBy.ID) {
		return
	}
	trusted, err := p.trustedAuthors(ctx)
	if err != nil {
		return
	}
	id := strings.ToLower(created.CreatedBy.ID)
	if _, ok := trusted[id]; ok {
		return
	}
	p.Warn(fmt.Sprintf(
		"created pull request %d is recorded as created by identity %s, which is not a trusted oiax identity; set OIAX_TRUSTED_AUTHORS=%s or later runs will not recognize it",
		created.PullRequestID, id, id))
}
