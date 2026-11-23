package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
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
// echoed into a warning. It is GitHub's alphabet as GitHub actually issues it:
// alphanumerics and hyphens, the underscore that joins an Enterprise Managed
// User's handle to its enterprise shortcode (octocat_acme), and the "[bot]"
// suffix App and Actions identities carry. Logins are case-insensitive, the
// suffix included. The pattern stays closed — no whitespace, control
// characters or brackets beyond that suffix — because a matching value is
// printed in warnings.
var loginPattern = regexp.MustCompile(`(?i)^[a-z0-9][a-z0-9_-]{0,99}(\[bot\])?$`)

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

// identityResolved reports whether the token's own identity is known. Until it
// is, the trusted set is only the declared authors, and "not opened by a
// trusted author" cannot be told apart from "opened by this token".
func (p *Provider) identityResolved() bool {
	p.authorsMu.Lock()
	defer p.authorsMu.Unlock()
	return p.authors != nil
}

// provenanceUnknown reports whether pr has the shape of a managed request but
// its authorship could not be judged because the token identity is unresolved.
// Core discovery treats such a request as unmanaged (it is never touched); a
// caller that records progress past a request — notification lifecycle scans —
// must instead treat it as not yet readable, or a transient lookup failure
// would permanently skip one of oiax's own requests.
func (p *Provider) provenanceUnknown(pr ghPull) bool {
	if p.anyAuthor {
		return false
	}
	_, shaped := managedMarker(pr)
	return shaped && !p.identityResolved()
}

// defaultIdentityRetryInterval is how long a failed token-identity lookup is
// left alone before it is tried again, when declared authors cover the gap.
const defaultIdentityRetryInterval = 30 * time.Second

func (p *Provider) identityRetryInterval() time.Duration {
	if p.identityRetry > 0 {
		return p.identityRetry
	}
	return defaultIdentityRetryInterval
}

// trustedAuthors returns the lower-cased logins whose pull requests may be
// oiax's own: the token's identity plus Provider.TrustedAuthors. The set is
// resolved on first need (a run that sees no marker-bearing pull request never
// pays for the lookup) and cached only once the token identity itself has
// resolved, so a transient failure or a cancelled context does not poison the
// rest of the process — including when declared authors let the call succeed
// without it.
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
	// A lookup that just failed is not repeated for a short while when the
	// declared authors can stand in. Recognition is asked once per
	// marker-bearing request, so without this a page of such requests would
	// spend a full retry budget on each one while the lookup stays down.
	if len(set) > 0 && time.Now().Before(p.lookupRetryAt) {
		return set, nil
	}
	self, err := p.viewerLogin(ctx)
	if err != nil {
		if len(set) == 0 {
			return nil, fmt.Errorf("%w: %w", errNoIdentity, err)
		}
		// The operator named the authors explicitly; that is sufficient on
		// its own for now. The partial set is never cached as the answer:
		// the lookup is retried after identityRetryInterval, and the token's
		// own identity joins the set as soon as it resolves. Say so once:
		// until then, requests opened by this run's identity are recognized
		// only if that identity is in the list.
		p.lookupRetryAt = time.Now().Add(p.identityRetryInterval())
		if p.Warn != nil && !p.lookupWarned {
			p.lookupWarned = true
			p.Warn("could not look up the token's own identity; recognizing managed requests by OIAX_TRUSTED_AUTHORS only")
		}
		return set, nil
	}
	set[strings.ToLower(self)] = struct{}{}
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
	if _, err := p.send(ctx, http.MethodPost, p.graphqlURL(), in, &resp, true); err != nil {
		return "", fmt.Errorf("look up token identity: %w", err)
	}
	login := resp.Data.Viewer.Login
	if !loginPattern.MatchString(login) {
		return "", errors.New("look up token identity: no usable login in response")
	}
	return login, nil
}

// graphqlURL is the GraphQL endpoint for the configured API root. It is not
// simply the REST root plus /graphql: github.com serves REST at
// https://api.github.com and GraphQL at https://api.github.com/graphql, but
// GitHub Enterprise Server serves REST under /api/v3 and GraphQL at
// /api/graphql — a sibling of the REST root, not a child of it.
func (p *Provider) graphqlURL() string {
	base := strings.TrimRight(p.baseURL(), "/")
	if root, ok := strings.CutSuffix(base, "/api/v3"); ok {
		return root + "/api/graphql"
	}
	return base + "/graphql"
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
		// On a first run nothing carried a marker yet, so this is where the
		// identity is first needed — after the request already exists. The
		// create succeeded; what the operator must know is that later runs
		// cannot recognize it until the lookup works or the author is declared.
		p.Warn(fmt.Sprintf(
			"created pull request #%d is authored by %s, but the token's own identity could not be looked up; "+
				"later runs will not recognize it while that lookup fails. Set OIAX_TRUSTED_AUTHORS=%s to recognize it regardless.",
			created.Number, created.User.Login, created.User.Login))
		return
	}
	if _, ok := trusted[strings.ToLower(created.User.Login)]; ok {
		return
	}
	p.Warn(fmt.Sprintf(
		"created pull request #%d is authored by %s, which is not a trusted oiax identity; set OIAX_TRUSTED_AUTHORS=%s or later runs will not recognize it",
		created.Number, created.User.Login, created.User.Login))
}
