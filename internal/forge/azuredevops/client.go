package azuredevops

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultAPIHost is the Azure DevOps cloud host. The organization is the
	// first path segment (https://dev.azure.com/{org}); every org, including
	// ones still reachable at the legacy {org}.visualstudio.com, resolves
	// here. Tests override apiBase entirely (BaseURL).
	defaultAPIHost = "https://dev.azure.com"
	// defaultAPIVersion is the REST api-version every call carries as a query
	// parameter. Azure DevOps requires it on every request.
	defaultAPIVersion = "7.1"
	// defaultWorkItemType is the Azure Boards work-item type a durable conflict
	// artifact is created as when OIAX_ADO_WORKITEM_TYPE is unset. "Issue"
	// exists in the Basic process; Agile/Scrum/CMMI projects must set the env
	// var to a type their process defines (see docs/reference/configuration.md).
	defaultWorkItemType = "Issue"

	// mergeStrategyPolicyType is the Azure DevOps policy type id of the
	// "Require a merge strategy" branch policy. TargetMergeMethods filters
	// policy configurations to this type to learn which merge strategies a
	// branch permits. (The similar-looking …4906e5d171dd is a different policy —
	// "Minimum number of reviewers" — a deliberate near-collision to avoid.)
	mergeStrategyPolicyType = "fa4e907d-c16b-4a4c-9dfa-4916e5d171ab"

	// zeroObjectID is the 40-zero git object id Azure DevOps's refs-update API
	// uses as the newObjectId to delete a ref.
	zeroObjectID = "0000000000000000000000000000000000000000"

	// tokenRedaction replaces the credential in any git output surfaced in an
	// error, mirroring the guarantee that the REST surface never emits it.
	tokenRedaction = "[redacted]"

	// requestTimeout bounds a single HTTP attempt so a stalled connection cannot
	// hang a reconcile. The request context still cancels the call; this is the
	// backstop when nothing upstream set a deadline.
	requestTimeout = 30 * time.Second
	// defaultRetryMax caps retries for a safe request (attempts = the initial
	// call plus up to this many retries).
	defaultRetryMax = 4
	// defaultRetryBackoff is the base of the exponential backoff between
	// retries; the actual wait grows to maxRetryBackoff and carries jitter.
	defaultRetryBackoff = 500 * time.Millisecond
	// maxRetryBackoff caps a single computed backoff interval.
	maxRetryBackoff = 30 * time.Second
	// maxServerBackoff caps how long a server-provided Retry-After can make us
	// wait, so a hostile or absurd header cannot stall a reconcile indefinitely
	// (context cancellation still wins).
	maxServerBackoff = 2 * time.Minute
	// defaultMaxRespBytes caps a success-path response body so a compromised or
	// malfunctioning API cannot OOM the process.
	defaultMaxRespBytes = 32 << 20

	// contentTypeJSON and contentTypeJSONPatch are the two request media types
	// the Azure DevOps API takes. PR create/update and refs use plain JSON; PR
	// properties and work-item create/update use JSON Patch.
	contentTypeJSON      = "application/json"
	contentTypeJSONPatch = "application/json-patch+json"
)

// oidPattern matches a git object id (or an unambiguous abbreviation). A commit
// id pushed to a ref reaches git as data and is guarded with this before use,
// so a branch name can never masquerade as a revision.
var oidPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// looksLikeJWT reports whether tok is a JSON Web Token — three dot-separated
// base64url segments whose header decodes to a JSON object. Azure Pipelines'
// $(System.AccessToken) is a JWT and authenticates as Bearer; a personal access
// token is not and authenticates as HTTP Basic. Detecting the shape lets one
// AZURE_DEVOPS_TOKEN carry either credential (see ADR 0009). The header is
// actually decoded rather than prefix-matched so a PAT that merely happens to
// start with "eyJ" and contain two dots is not misrouted as a Bearer token.
func looksLikeJWT(tok string) bool {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return false
	}
	hdr, err := base64.RawURLEncoding.DecodeString(parts[0])
	return err == nil && len(hdr) > 0 && hdr[0] == '{'
}

// authorization returns the Authorization header value for a REST call: Bearer
// for a $(System.AccessToken) JWT, otherwise HTTP Basic with an empty username
// and the token as the password (the Azure DevOps PAT convention). The token is
// never logged; it appears only in this header.
func (p *Provider) authorization() string {
	if looksLikeJWT(p.Token) {
		return "Bearer " + p.Token
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(":"+p.Token))
}

// apiBase is the REST root: BaseURL when injected (tests point it at an
// httptest.Server), otherwise https://dev.azure.com/{org}. It never carries a
// credential.
func (p *Provider) apiBase() string {
	if p.BaseURL != "" {
		return p.BaseURL
	}
	return defaultAPIHost + "/" + url.PathEscape(p.Repo.Organization)
}

// apiVersion is the api-version query value, overridable for tests.
func (p *Provider) apiVersion() string {
	if p.APIVersion != "" {
		return p.APIVersion
	}
	return defaultAPIVersion
}

// withVersion appends the required api-version query parameter to a path that
// already carries the base and any other query parameters.
func (p *Provider) withVersion(rawURL string) string {
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	return rawURL + sep + "api-version=" + url.QueryEscape(p.apiVersion())
}

// gitPath is the project-scoped Git REST prefix
// (.../{project}/_apis/git/repositories/{repo}), with each identity segment
// percent-escaped.
func (p *Provider) gitPath(suffix string) string {
	return p.apiBase() + "/" + url.PathEscape(p.Repo.Project) +
		"/_apis/git/repositories/" + url.PathEscape(p.Repo.Name) + suffix
}

// projectPath is the project-scoped REST prefix (.../{project}/_apis) for
// work-item and policy endpoints.
func (p *Provider) projectPath(suffix string) string {
	return p.apiBase() + "/" + url.PathEscape(p.Repo.Project) + "/_apis" + suffix
}

// defaultHTTPClient carries a request timeout so a stalled connection cannot
// hang a reconcile. It is shared: http.Client is safe for concurrent use.
var defaultHTTPClient = &http.Client{Timeout: requestTimeout}

func (p *Provider) httpClient() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return defaultHTTPClient
}

// do issues one REST call with bounded retry for SAFE requests. A GET is always
// safe to retry; other methods are retried only when the caller opts in via
// send (CreateRequest does, because a duplicate create is adopted through the
// 409/TF401179 path). Non-idempotent mutations are never retried.
func (p *Provider) do(ctx context.Context, method, urlStr, contentType string, in, out any) (http.Header, error) {
	return p.send(ctx, method, urlStr, contentType, in, out, method == http.MethodGet)
}

// send performs a REST call, retrying transient failures with bounded
// exponential backoff and jitter when retryable is set. It retries on transport
// errors and on 429 / 5xx / rate-limited 403 responses, honoring a
// server-provided Retry-After, and respects context cancellation between
// attempts. A non-retryable request, an exhausted attempt budget, or a
// cancelled context returns the last result unchanged — so a 409 from a create
// still reaches the adopt path.
func (p *Provider) send(ctx context.Context, method, urlStr, contentType string, in, out any, retryable bool) (http.Header, error) {
	attempts := p.retryMax
	if attempts <= 0 {
		attempts = defaultRetryMax
	}
	for attempt := 0; ; attempt++ {
		hdr, err := p.doOnce(ctx, method, urlStr, contentType, in, out)
		if err == nil {
			return hdr, nil
		}
		if !retryable || attempt >= attempts || ctx.Err() != nil {
			return hdr, err
		}
		wait, ok := retryDelay(err, hdr, p.backoff(attempt))
		if !ok {
			return hdr, err
		}
		if serr := sleepCtx(ctx, wait); serr != nil {
			// Context cancelled during backoff: surface the API/transport error
			// rather than the sleep's cancellation, so callers see why the call
			// failed.
			return hdr, err
		}
	}
}

// doOnce issues a single REST call. It sets auth and the api-version, encodes in
// (when non-nil) with the given content type, decodes a 2xx body into out (when
// non-nil, capped by an io.LimitReader so a compromised API cannot OOM the
// process), and turns a non-2xx response into an *apiError carrying Azure
// DevOps's message and type key — never the token.
func (p *Provider) doOnce(ctx context.Context, method, urlStr, contentType string, in, out any) (http.Header, error) {
	var reqBody io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.withVersion(urlStr), reqBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", p.authorization())
	req.Header.Set("Accept", contentTypeJSON)
	if in != nil {
		ct := contentType
		if ct == "" {
			ct = contentTypeJSON
		}
		req.Header.Set("Content-Type", ct)
	}

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, &errNoResponse{fmt.Errorf("http request: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.Header, parseAPIError(resp)
	}
	if out != nil {
		limit := p.maxRespBytes
		if limit <= 0 {
			limit = defaultMaxRespBytes
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, limit)).Decode(out); err != nil {
			return resp.Header, fmt.Errorf("decode response: %w", err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return resp.Header, nil
}

// backoff returns the base exponential delay for a retry attempt, clamped to
// maxRetryBackoff and spread with full jitter (a uniform value in [d/2, d]) so
// concurrent reconciles do not synchronize on the same schedule.
func (p *Provider) backoff(attempt int) time.Duration {
	base := p.retryBackoff
	if base <= 0 {
		base = defaultRetryBackoff
	}
	d := base << attempt
	if d > maxRetryBackoff || d <= 0 { // d <= 0 guards a shift overflow
		d = maxRetryBackoff
	}
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

// errNoResponse marks a transport-level failure whose round trip did not
// complete, so no HTTP response was received. It is distinct from the
// deterministic construction errors that precede the round trip and from a
// decode failure (a real 2xx was received). retryDelay treats only
// errNoResponse as unconditionally transient.
type errNoResponse struct{ err error }

func (e *errNoResponse) Error() string { return e.err.Error() }
func (e *errNoResponse) Unwrap() error { return e.err }

// retryDelay decides whether err is transient and how long to wait before
// retrying. Only errNoResponse — a transport failure whose round trip did not
// complete — is unconditionally transient. An *apiError is transient only for
// 429, 5xx, and a 403 that carries rate-limit signals; a server-provided
// Retry-After then wins over the computed backoff. Every other error is not
// retried: a construction error would fail identically, and a decode failure on
// a 2xx body means the request already reached the server.
func retryDelay(err error, hdr http.Header, backoff time.Duration) (time.Duration, bool) {
	var noResp *errNoResponse
	if errors.As(err, &noResp) {
		return backoff, true
	}
	var ae *apiError
	if !errors.As(err, &ae) {
		return 0, false
	}
	if !retryableStatus(ae.StatusCode, hdr) {
		return 0, false
	}
	if d, ok := serverBackoff(hdr); ok {
		return d, true
	}
	return backoff, true
}

// retryableStatus reports whether an HTTP status is worth retrying. 429 and the
// common 5xx are always retryable; a 403 is retryable only when it carries
// rate-limit signals, so a genuine permission denial is not retried.
func retryableStatus(code int, hdr http.Header) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	case http.StatusForbidden:
		if hdr.Get("Retry-After") != "" {
			return true
		}
		return hdr.Get("X-RateLimit-Remaining") == "0"
	default:
		return false
	}
}

// serverBackoff reads a server-directed wait from Retry-After (delta-seconds or
// an HTTP-date) or X-RateLimit-Reset (a Unix epoch), clamped to maxServerBackoff
// so an absurd header cannot stall a reconcile. ok is false when neither header
// gives a usable value.
func serverBackoff(hdr http.Header) (time.Duration, bool) {
	clamp := func(d time.Duration) time.Duration {
		if d < 0 {
			return 0
		}
		if d > maxServerBackoff {
			return maxServerBackoff
		}
		return d
	}
	if ra := strings.TrimSpace(hdr.Get("Retry-After")); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil {
			return clamp(time.Duration(secs) * time.Second), true
		}
		if t, err := http.ParseTime(ra); err == nil {
			return clamp(time.Until(t)), true
		}
	}
	if reset := strings.TrimSpace(hdr.Get("X-RateLimit-Reset")); reset != "" {
		if epoch, err := strconv.ParseInt(reset, 10, 64); err == nil {
			return clamp(time.Until(time.Unix(epoch, 0))), true
		}
	}
	return 0, false
}

// sleepCtx waits for d or until ctx is cancelled, returning ctx.Err() if the
// context ends first (or is already ended).
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// apiError is a non-2xx Azure DevOps response. It carries the status code so
// callers can branch (e.g. adopt a 409 duplicate) with errors.As, and Azure
// DevOps's human message and typeKey — never the credential.
type apiError struct {
	StatusCode int
	Message    string
	TypeKey    string
}

func (e *apiError) Error() string {
	if e.TypeKey != "" {
		return fmt.Sprintf("azure devops api %d: %s (%s)", e.StatusCode, e.Message, e.TypeKey)
	}
	return fmt.Sprintf("azure devops api %d: %s", e.StatusCode, e.Message)
}

// parseAPIError decodes Azure DevOps's {message, typeKey} error envelope. A body
// that does not decode still yields a status-only error.
func parseAPIError(resp *http.Response) error {
	var env struct {
		Message string `json:"message"`
		TypeKey string `json:"typeKey"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = json.Unmarshal(data, &env)
	ae := &apiError{StatusCode: resp.StatusCode, Message: env.Message, TypeKey: env.TypeKey}
	if ae.Message == "" {
		ae.Message = http.StatusText(resp.StatusCode)
	}
	return ae
}

// isDuplicateActiveRequest reports whether err is Azure DevOps refusing a second
// active pull request for the same source/target pair — HTTP 409 carrying the
// TF401179 signal (in the type key or the message). That refusal is adopted as
// success, exactly as the GitHub provider adopts a 422, because the forge is the
// concurrency arbiter for promotion requests.
func isDuplicateActiveRequest(err error) bool {
	var ae *apiError
	if !errors.As(err, &ae) || ae.StatusCode != http.StatusConflict {
		return false
	}
	return strings.Contains(ae.Message, "TF401179") ||
		strings.Contains(ae.TypeKey, "PullRequestExists")
}

// checkRefFormat validates a branch name with git before it is used as a ref,
// mirroring the internal/git posture (names reach git as data, not a shell). It
// runs in GitDir. The error carries the name (caller data, never a credential),
// never git's raw output.
func (p *Provider) checkRefFormat(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "git", "check-ref-format", "--branch", name)
	cmd.Dir = p.GitDir
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("invalid branch name %q", name)
	}
	return nil
}

// gitRemote returns the push remote: GitRemote verbatim when injected (tests
// point it at a local bare repo), else the Azure DevOps https git URL built from
// the identity. The default remote carries NO credential: the token is delivered
// out of band via pushAuthEnv (http.extraHeader in the environment), so it never
// appears in argv or in the remote URL git may echo on failure.
func (p *Provider) gitRemote() string {
	if p.GitRemote != "" {
		return p.GitRemote
	}
	return defaultAPIHost + "/" + url.PathEscape(p.Repo.Organization) +
		"/" + url.PathEscape(p.Repo.Project) + "/_git/" + url.PathEscape(p.Repo.Name)
}

// pushAuthEnv returns the git configuration that authenticates a push, delivered
// through the environment (GIT_CONFIG_COUNT/KEY/VALUE) rather than argv so the
// token is never visible in the process table. It sets http.extraHeader with the
// same scheme the REST surface uses — bearer for a $(System.AccessToken) JWT,
// otherwise HTTP Basic (":"+token, base64-encoded) — keeping the credential out
// of the remote URL entirely. It is empty when the remote is not an http(s) URL
// (tests push to a local bare repo that needs no credential) or no token is set.
func (p *Provider) pushAuthEnv() []string {
	remote := p.gitRemote()
	if p.Token == "" || (!strings.HasPrefix(remote, "https://") && !strings.HasPrefix(remote, "http://")) {
		return nil
	}
	var header string
	if looksLikeJWT(p.Token) {
		header = "AUTHORIZATION: bearer " + p.Token
	} else {
		header = "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte(":"+p.Token))
	}
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.extraHeader",
		"GIT_CONFIG_VALUE_0=" + header,
	}
}

// scrubToken removes the credential from a string surfaced to callers, so a git
// failure that echoes the remote URL cannot leak the token.
func (p *Provider) scrubToken(s string) string {
	if p.Token == "" {
		return s
	}
	return strings.ReplaceAll(s, p.Token, tokenRedaction)
}

// gitCommand builds an exec.Cmd for a git invocation in dir with the interactive
// credential prompt disabled (never block on a bad token — fail fast) and the
// given extra environment (the http.extraHeader credential config) appended, so
// callers do not repeat the env plumbing.
func gitCommand(ctx context.Context, dir string, extraEnv []string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Env = append(cmd.Env, extraEnv...)
	return cmd
}

// maxGitStderr caps captured git stderr so a pathological error cannot balloon
// memory before it is scrubbed and surfaced.
const maxGitStderr = 64 << 10

// capWriter captures up to maxGitStderr bytes of output and discards the rest,
// keeping the informative head of a git failure without an unbounded buffer.
type capWriter struct{ b []byte }

func (w *capWriter) Write(p []byte) (int, error) {
	if room := maxGitStderr - len(w.b); room > 0 {
		if len(p) > room {
			w.b = append(w.b, p[:room]...)
		} else {
			w.b = append(w.b, p...)
		}
	}
	return len(p), nil
}

func (w *capWriter) String() string { return string(w.b) }
