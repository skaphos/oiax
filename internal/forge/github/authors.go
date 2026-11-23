package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

// Author provenance (ADR 0020).
//
// A managed request's body marker and its head/base branch names are all
// chosen by whoever opens the pull request, and a promotion request's head is
// an existing long-lived branch — so anyone allowed to open a pull request
// between two graph branches can produce a request that is shape-identical to
// one of oiax's own. The one field of a pull request its author cannot choose
// is the author. Recognition therefore also requires the request to have been
// opened by the identity oiax itself runs as, or by one the operator has
// explicitly declared trusted (a previous token identity, during a migration).

// loginPattern bounds what is accepted as a login from configuration and
// echoed into a warning: GitHub's own alphabet plus the "[bot]" suffix App and
// Actions identities carry.
var loginPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,99}(\[bot\])?$`)

// errNoIdentity means neither the token's own login nor an operator-declared
// author is available, so no request can be attributed to oiax.
var errNoIdentity = errors.New("cannot determine which pull request authors are oiax: the token identity lookup failed and OIAX_TRUSTED_AUTHORS is not set")

// managed is the full recognition rule: the shape check (managedMarker) plus
// author provenance. A request that passes the shape check but was opened by
// an untrusted author is reported once as a warning and treated exactly like
// any other unmanaged pull request — never listed, edited, or closed.
func (p *Provider) managed(ctx context.Context, pr ghPull) (marker, bool, error) {
	m, ok := managedMarker(pr)
	if !ok {
		return marker{}, false, nil
	}
	if p.anyAuthor {
		return m, true, nil
	}
	trusted, err := p.trustedAuthors(ctx)
	if err != nil {
		return marker{}, false, err
	}
	login := strings.ToLower(pr.User.Login)
	if _, ok := trusted[login]; login != "" && ok {
		return m, true, nil
	}
	p.warnUntrusted(pr, trusted)
	return marker{}, false, nil
}

// trustedAuthors returns the lower-cased logins whose pull requests may be
// oiax's own: the token's identity plus Provider.TrustedAuthors. The set is
// resolved on first need (a run that sees no marker-bearing pull request never
// pays for the lookup) and cached only on success, so a transient failure or a
// cancelled context does not poison the rest of the process.
func (p *Provider) trustedAuthors(ctx context.Context) (map[string]struct{}, error) {
	p.authorsMu.Lock()
	defer p.authorsMu.Unlock()
	if p.authors != nil {
		return p.authors, nil
	}
	set := map[string]struct{}{}
	for _, login := range p.TrustedAuthors {
		login = strings.TrimSpace(login)
		if login == "" {
			continue
		}
		if !loginPattern.MatchString(login) {
			return nil, fmt.Errorf("invalid trusted author %q (want a GitHub login such as my-app[bot])", login)
		}
		set[strings.ToLower(login)] = struct{}{}
	}
	self, err := p.viewerLogin(ctx)
	switch {
	case err == nil:
		set[strings.ToLower(self)] = struct{}{}
	case len(set) == 0:
		return nil, fmt.Errorf("%w: %w", errNoIdentity, err)
	default:
		// The operator named the authors explicitly; that is sufficient on
		// its own. Say so, because requests opened by this run's identity are
		// only recognized if that identity is in the list.
		if p.Warn != nil {
			p.Warn("could not look up the token's own identity; recognizing managed requests by OIAX_TRUSTED_AUTHORS only")
		}
	}
	p.authors = set
	return set, nil
}

// viewerLogin asks GitHub which identity the token acts as. REST has no such
// endpoint for installation tokens or GITHUB_TOKEN (GET /user is refused for
// them), so this is the GraphQL viewer — the actor GitHub records as the
// author of a pull request the token opens. The query is a constant: nothing
// is interpolated into it.
func (p *Provider) viewerLogin(ctx context.Context) (string, error) {
	var resp struct {
		Data struct {
			Viewer struct {
				Login string `json:"login"`
			} `json:"viewer"`
		} `json:"data"`
	}
	in := map[string]string{"query": "query { viewer { login } }"}
	// A read-only query is safe to retry.
	if _, err := p.send(ctx, http.MethodPost, p.url("/graphql"), in, &resp, true); err != nil {
		return "", fmt.Errorf("look up token identity: %w", err)
	}
	login := resp.Data.Viewer.Login
	if !loginPattern.MatchString(login) {
		return "", errors.New("look up token identity: no usable login in response")
	}
	return login, nil
}

// warnUntrusted reports, once per pull request, a request that carries a
// well-formed oiax marker but was not opened by a trusted author. It is the
// operator's signal in both cases that reach here: someone hand-wrote a marker,
// or the token identity changed and the previous one needs declaring. The
// login is echoed only when it has the shape of a login.
func (p *Provider) warnUntrusted(pr ghPull, trusted map[string]struct{}) {
	if p.Warn == nil {
		return
	}
	p.authorsMu.Lock()
	if p.untrustedWarned == nil {
		p.untrustedWarned = map[int]struct{}{}
	}
	_, seen := p.untrustedWarned[pr.Number]
	p.untrustedWarned[pr.Number] = struct{}{}
	p.authorsMu.Unlock()
	if seen {
		return
	}
	author := "an unrecognized author"
	if loginPattern.MatchString(pr.User.Login) {
		author = pr.User.Login
	}
	names := make([]string, 0, len(trusted))
	for login := range trusted {
		names = append(names, login)
	}
	sort.Strings(names)
	p.Warn(fmt.Sprintf(
		"pull request #%d carries an oiax marker but was opened by %s, not a trusted oiax identity (%s); treating it as unmanaged. "+
			"If it was opened by a previous oiax token, add that login to OIAX_TRUSTED_AUTHORS.",
		pr.Number, author, strings.Join(names, ", ")))
}

// warnForeignAuthor fires when a request oiax just created is authored by a
// login outside the trusted set — the token-identity lookup and the forge's
// recorded author disagree. Left silent, the next run would not recognize its
// own request.
func (p *Provider) warnForeignAuthor(ctx context.Context, created ghPull) {
	if p.anyAuthor || p.Warn == nil || !loginPattern.MatchString(created.User.Login) {
		return
	}
	trusted, err := p.trustedAuthors(ctx)
	if err != nil {
		return
	}
	if _, ok := trusted[strings.ToLower(created.User.Login)]; ok {
		return
	}
	p.Warn(fmt.Sprintf(
		"created pull request #%d is authored by %s, which is not a trusted oiax identity; set OIAX_TRUSTED_AUTHORS=%s or later runs will not recognize it",
		created.Number, created.User.Login, created.User.Login))
}
