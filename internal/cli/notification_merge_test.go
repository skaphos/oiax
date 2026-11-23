package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/skaphos/oiax/v2/internal/forge/forgetest"
	mk "github.com/skaphos/oiax/v2/internal/forge/marker"
	"github.com/skaphos/oiax/v2/internal/gittest"
	"github.com/skaphos/oiax/v2/internal/notification"
	notificationstore "github.com/skaphos/oiax/v2/internal/notification/store"
)

// notificationBinary is the package's one fixture CLI. Every caller needs the
// same variant — one overlay, race-enabled exactly when this test binary is —
// so it is built at most once per package run, by whichever test asks first,
// and the rest wait for that result. Building per test let several parallel
// (race) builds contend on a loaded runner under a fixed deadline (#112).
var notificationBinary struct {
	once      sync.Once
	dir, path string
	err       error
}

// TestMain exists only to remove the shared fixture binary: it must outlive
// every test that uses it, so no single test's TempDir can own it.
func TestMain(m *testing.M) {
	code := m.Run()
	if notificationBinary.dir != "" {
		if err := os.RemoveAll(notificationBinary.dir); err != nil {
			fmt.Fprintln(os.Stderr, "remove fixture CLI:", err)
		}
	}
	os.Exit(code)
}

// Build the real entrypoint with fixture-only connection setup. See testdata's
// README for the exact boundary and the distinction from live acceptance.
//
// The build has no deadline of its own: it runs inside the package's go test
// -timeout, which reports the stuck test if it ever hangs, rather than under a
// fixed per-test limit that a slow, loaded runner can trip.
func buildNotificationBinary(t *testing.T) string {
	t.Helper()
	notificationBinary.once.Do(func() {
		notificationBinary.dir, notificationBinary.path, notificationBinary.err = compileNotificationBinary()
	})
	if notificationBinary.err != nil {
		t.Fatal(notificationBinary.err)
	}
	return notificationBinary.path
}

func compileNotificationBinary() (dir, binary string, err error) {
	root, err := filepath.Abs("../..")
	if err != nil {
		return "", "", err
	}
	dir, err = os.MkdirTemp("", "oiax-notification-fixture-")
	if err != nil {
		return "", "", err
	}
	runtimePath := filepath.Join(root, "internal/reconcile/notification_runtime.go")
	source, err := os.ReadFile(runtimePath)
	if err != nil {
		return dir, "", err
	}
	const constructor = "return delivery.NewClient(destination.Type, destination.AllowPrivateNetwork)"
	if strings.Count(string(source), constructor) != 1 {
		return dir, "", errors.New("notification constructor moved; review the fixture overlay boundary")
	}
	const clock = "Now:         func() time.Time { return time.Now().UTC() },"
	if strings.Count(string(source), clock) != 1 {
		return dir, "", errors.New("notification clock moved; review the fixture overlay boundary")
	}
	overridden := strings.Replace(string(source), constructor,
		"return delivery.NewFixtureClient(destination.Type, destination.AllowPrivateNetwork)", 1)
	overridden = strings.Replace(overridden, clock, "Now:         fixtureNotificationNow,", 1)
	// The pre-POST creation-origin stamp reads the clock directly rather than
	// through Now. It must be ordered against the cutoff that Now set earlier
	// in the same run.
	const originClock = "ObservedAt: time.Now().UTC(), LogicalSource: source"
	if strings.Count(overridden, originClock) != 1 {
		return dir, "", errors.New("notification origin clock moved; review the fixture overlay boundary")
	}
	overridden = strings.Replace(overridden, originClock, "ObservedAt: fixtureNotificationNow(), LogicalSource: source", 1)
	override := filepath.Join(dir, "notification_runtime.go")
	if err := os.WriteFile(override, []byte(overridden), 0o600); err != nil {
		return dir, "", err
	}
	overlay := map[string]map[string]string{"Replace": {
		runtimePath: override,
		filepath.Join(root, "internal/cli/notification_fixture.go"):                   filepath.Join(root, "internal/cli/testdata/notification-binary/forge.go"),
		filepath.Join(root, "internal/notification/delivery/notification_fixture.go"): filepath.Join(root, "internal/cli/testdata/notification-binary/delivery.go"),
		filepath.Join(root, "internal/reconcile/notification_fixture.go"):             filepath.Join(root, "internal/cli/testdata/notification-binary/clock.go"),
	}}
	data, err := json.Marshal(overlay)
	if err != nil {
		return dir, "", err
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayPath, data, 0o600); err != nil {
		return dir, "", err
	}
	binary = filepath.Join(dir, "oiax.exe")
	args := []string{"build", "-overlay", overlayPath, "-o", binary}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "-race" && setting.Value == "true" {
				args = append(args, "-race")
			}
		}
	}
	cmd := exec.Command("go", append(args, "./cmd/oiax")...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return dir, "", fmt.Errorf("build fixture CLI: %v\n%s", err, out)
	}
	return dir, binary, nil
}

func writeNotificationFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

const notificationBinaryConfig = `apiVersion: oiax.skaphos.dev/v1
kind: PromotionGraph
metadata: {name: graph}
spec:
  branches: {dev: {role: source}, test: {}, stage: {}, main: {}}
  promotions: [{from: dev, to: test}, {from: dev, to: stage}]
  backflow: {sources: [main], target: dev}
  notifications:
    destinations:
      - {name: ops, type: %s, endpointEnv: OIAX_FIXTURE_ENDPOINT}
`

type notificationReceived struct {
	Body   []byte
	Header http.Header
}

type notificationBinaryFixture struct {
	t                        *testing.T
	binary, dir, remote, ca  string
	provider, transport, oid string
	// configRef overrides the pinned configuration commit every invocation is
	// run against. Empty means oid — the commit the fixture graph was created
	// at — which is what every test but the revision-recovery one wants.
	configRef string
	// clockFloor, when set, is the earliest time any later invocation's
	// notification clock may read (see eventTime and launchFloor), as of the
	// monotonic reading floorSetAt. Guarded by mu: the forge raises it when it
	// stamps a creation.
	clockFloor      time.Time
	floorSetAt      time.Time
	server          *httptest.Server
	mu              sync.Mutex
	seeds           []forgetest.LifecycleSeed
	bodies          map[int]string
	received        []notificationReceived
	failDelivery    bool
	missingEndpoint bool
	failMetadata    bool
	failDiscovery   bool
	failDetails     bool
	failTarget      string
	createdTargets  []string
	lifecycleReads  int
}

func newNotificationBinaryFixture(t *testing.T, binary, provider, transport string, extraDestinations ...string) *notificationBinaryFixture {
	t.Helper()
	f := &notificationBinaryFixture{t: t, binary: binary, provider: provider, transport: transport, bodies: map[int]string{}}
	f.dir = filepath.Join(t.TempDir(), "checkout")
	gittest.InitRepo(t, f.dir)
	writeNotificationFixture(t, filepath.Join(f.dir, ".oiax.yaml"), []byte(fmt.Sprintf(notificationBinaryConfig, transport)+strings.Join(extraDestinations, "\n")))
	writeNotificationFixture(t, filepath.Join(f.dir, "app.txt"), []byte("base\n"))
	gittest.Run(t, f.dir, "add", ".")
	gittest.Run(t, f.dir, "commit", "-qm", "initial graph")
	f.oid = gittest.Run(t, f.dir, "rev-parse", "HEAD")
	for _, branch := range []string{"dev", "test", "stage"} {
		gittest.Run(t, f.dir, "branch", branch)
	}
	f.remote = filepath.Join(t.TempDir(), "remote.git")
	gittest.Run(t, "", "init", "--bare", "-q", f.remote)
	gittest.Run(t, f.dir, "remote", "add", "origin", f.remote)
	gittest.Run(t, f.dir, "push", "-q", "origin", "--all")
	f.server = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	f.ca = filepath.Join(t.TempDir(), "fixture.pem")
	writeNotificationFixture(t, f.ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.server.Certificate().Raw}))
	return f
}

// publish pushes local graph-branch changes to the fixture remote, which also
// moves their refs/remotes/origin/* tracking refs. A test that commits or
// moves a branch locally stands in for a change that landed on the forge, and
// Oiax plans against the origin-tracking ref (#108), so the change must be
// published before the next run can see it.
func (f *notificationBinaryFixture) publish(branches ...string) {
	f.t.Helper()
	gittest.Run(f.t, f.dir, append([]string{"push", "-q", "--force", "origin"}, branches...)...)
}

func (f *notificationBinaryFixture) run(want int, args ...string) (string, string) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	configRef := f.configRef
	if configRef == "" {
		configRef = f.oid
	}
	args = append(args, "--config-ref", configRef, "--output", "json")
	cmd := exec.CommandContext(ctx, f.binary, args...)
	cmd.Dir = f.dir
	cmd.Env = append(gittest.Env(),
		"GITHUB_ACTIONS=", "GITHUB_STEP_SUMMARY=", "TF_BUILD=", "OIAX_LOG_FORMAT=json",
		"GITHUB_TOKEN=", "AZURE_DEVOPS_TOKEN=", "GITHUB_REPOSITORY=", "GORACE=atexit_sleep_ms=0",
		"OIAX_FORGE="+f.provider, "OIAX_FIXTURE_API="+f.server.URL,
		"OIAX_FIXTURE_REMOTE="+f.remote, "OIAX_FIXTURE_CA="+f.ca,
		"OIAX_FIXTURE_RECEIVER="+f.server.Listener.Addr().String(),
		"OIAX_FIXTURE_ENDPOINT=https://example.com/receiver/endpoint-canary",
		"OIAX_FIXTURE_AUDIT=https://example.com/receiver/healthy-canary")
	if f.missingEndpoint {
		cmd.Env = append(cmd.Env, "OIAX_FIXTURE_ENDPOINT=")
	}
	if floor := f.launchFloor(); !floor.IsZero() {
		cmd.Env = append(cmd.Env, "OIAX_FIXTURE_CLOCK_FLOOR="+floor.Format(time.RFC3339Nano))
	}
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			f.t.Fatalf("run fixture: %v", err)
		}
		code = exit.ExitCode()
	}
	if ctx.Err() != nil || code != want {
		f.t.Fatalf("%v: exit %d, want %d (context %v)\nstdout: %s\nstderr: %s", args, code, want, ctx.Err(), out.String(), stderr.String())
	}
	// The guarantee is that stdout carries exactly one JSON document and nothing
	// else — no annotations, no log lines. A successful invocation must always
	// produce that document; only a command that failed before reaching its
	// renderer is allowed to have written nothing.
	if want == 0 && out.Len() == 0 {
		f.t.Fatalf("%v exited 0 but wrote no JSON document to stdout\nstderr: %s", args, stderr.String())
	}
	if out.Len() > 0 && !json.Valid(out.Bytes()) {
		f.t.Fatalf("stdout is not a single JSON document: %s", out.String())
	}
	for _, canary := range []string{"fixture-forge-canary", "endpoint-canary", "healthy-canary"} {
		if strings.Contains(out.String()+stderr.String(), canary) {
			f.t.Fatal("credential canary leaked into command output")
		}
	}
	return out.String(), stderr.String()
}

func (f *notificationBinaryFixture) identity() notification.RepositoryIdentity {
	if f.provider == "azuredevops" {
		return notification.RepositoryIdentity{Provider: "azuredevops", Host: "dev.azure.com", ID: "repository-id", OrganizationID: "organization-id", ProjectID: "project-id", Name: "org/project/repo"}
	}
	return notification.RepositoryIdentity{Provider: "github", Host: "github.com", ID: "123", Name: "example/repo"}
}

func (f *notificationBinaryFixture) ledger() *notification.LedgerV1 {
	f.t.Helper()
	l, data, err := f.readLedger()
	if strings.Contains(data, "endpoint-canary") || strings.Contains(data, "fixture-forge-canary") || strings.Contains(data, "healthy-canary") {
		f.t.Fatal("credential canary persisted in notes")
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return l
}

// readLedger decodes the remote ledger without failing the test, because
// launchFloor peeks at it before every invocation, including in the modes that
// corrupt or delete it on purpose.
func (f *notificationBinaryFixture) readLedger() (*notification.LedgerV1, string, error) {
	git := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = f.remote, gittest.Env()
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	ref := "refs/notes/oiax/notifications/v1/" + notification.GraphKey(f.identity(), "graph")
	tree, err := git("ls-tree", "-r", ref)
	if err != nil {
		return nil, "", fmt.Errorf("read notes tree: %w", err)
	}
	entries := strings.Fields(tree)
	if len(entries) != 4 {
		return nil, "", fmt.Errorf("unexpected notes tree: %v", entries)
	}
	data, err := git("cat-file", "blob", entries[2])
	if err != nil {
		return nil, "", fmt.Errorf("read notes blob: %w", err)
	}
	l, err := notificationstore.Decode(strings.NewReader(data))
	return l, data, err
}

// ledgerWatermark is the latest subscription cutoff or scan bound the ledger
// records: the times a later invocation's clock must not read below.
func ledgerWatermark(l *notification.LedgerV1) time.Time {
	var latest time.Time
	later := func(t time.Time) {
		if t.After(latest) {
			latest = t
		}
	}
	for _, d := range l.Destinations {
		for _, sub := range d.Subscriptions {
			later(sub.Cutoff)
		}
	}
	for _, scan := range l.Scans {
		later(scan.From)
		later(scan.Through)
	}
	return latest.UTC()
}

// currentFloorLocked is the floor carried forward to now on this process's
// monotonic clock. Every reading a fixture binary took while floored is at most
// its launch floor plus its own runtime, and that runtime is already behind
// the elapsed time measured here. f.mu must be held.
func (f *notificationBinaryFixture) currentFloorLocked() time.Time {
	if f.clockFloor.IsZero() {
		return time.Time{}
	}
	return f.clockFloor.Add(time.Since(f.floorSetAt))
}

// raiseFloorLocked moves the floor up to at, never down. f.mu must be held.
func (f *notificationBinaryFixture) raiseFloorLocked(at time.Time) time.Time {
	floor := f.currentFloorLocked()
	if at.After(floor) {
		floor = at
	}
	f.clockFloor, f.floorSetAt = floor, time.Now()
	return floor
}

// launchFloor returns the floor for the next invocation, or zero when no test
// has asked for one. Each fixture binary measures elapsed time from its own
// start, so a single fixed floor would let a second run read earlier than the
// first run's recorded scan bound while the wall clock is still behind. Observe
// would then build an interval whose From is after its Through, and the store
// rejects that. The floor therefore advances on this process's monotonic clock,
// and it is raised past anything the ledger already records.
func (f *notificationBinaryFixture) launchFloor() time.Time {
	f.mu.Lock()
	enabled := !f.clockFloor.IsZero()
	f.mu.Unlock()
	if !enabled {
		return time.Time{}
	}
	var watermark time.Time
	if l, _, err := f.readLedger(); err == nil {
		watermark = ledgerWatermark(l).Add(time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.raiseFloorLocked(watermark)
}

// eventTime returns the forge timestamp for an event the next invocation must
// admit, and floors every later invocation's clock at it.
//
// The timestamp comes from this process's wall clock, but the fixture binary's
// clock set the subscription cutoff and scan watermark it must not precede.
// Both are the host's wall clock, so on a host whose wall clock gets stepped
// backwards a plain time.Now() can land before them. The event is then dropped
// as before-subscription-cutoff (#143). So the timestamp is never earlier than
// the latest time the ledger records, and the floor keeps the next scan's upper
// bound from falling behind it.
func (f *notificationBinaryFixture) eventTime(l *notification.LedgerV1) time.Time {
	at := time.Now().UTC()
	if floor := ledgerWatermark(l).Add(time.Millisecond); at.Before(floor) {
		at = floor
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// A floor already in force (a creation stamp, an earlier event) is never
	// lowered.
	return f.raiseFloorLocked(at)
}

func (f *notificationBinaryFixture) seed(id int, kind, state string, at time.Time, managed bool) forgetest.LifecycleSeed {
	s := forgetest.LifecycleSeed{ID: id, Graph: "graph", Type: "promotion", Source: "dev", Destination: "test", State: notification.LifecycleState(state), CreatedAt: at.Add(-24 * time.Hour), MergedAt: at, Managed: managed}
	if kind == "backflow" {
		s.Type, s.Source, s.Destination = "backflow", "oiax/backflow/deleted/abcdef0", "dev"
	}
	return s
}

func (f *notificationBinaryFixture) setSeeds(seeds ...forgetest.LifecycleSeed) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seeds = seeds
}

func (f *notificationBinaryFixture) messages() []notificationReceived {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]notificationReceived(nil), f.received...)
}

// The identity the fixture forge reports for the token and records as the
// author of every request it serves.
const (
	fixtureLogin      = "oiax-fixture[bot]"
	fixtureIdentityID = "0a0a0a0a-1111-2222-3333-444444444444"
)

func (f *notificationBinaryFixture) pull(s forgetest.LifecycleSeed) map[string]any {
	body := "Human-authored request"
	if s.Managed {
		body = mk.Serialize(mk.Marker{Version: "v1", Graph: s.Graph, Type: string(s.Type), Source: s.Source, Destination: s.Destination, SourceHead: f.oid})
	}
	if saved, ok := f.bodies[s.ID]; ok {
		body = saved
	}
	if f.provider == "azuredevops" {
		status := "active"
		switch s.State {
		case notification.LifecycleMerged:
			status = "completed"
		case notification.LifecycleClosed:
			status = "abandoned"
		}
		return map[string]any{"pullRequestId": s.ID, "status": status, "description": body,
			"sourceRefName": "refs/heads/" + s.Source, "targetRefName": "refs/heads/" + s.Destination,
			"creationDate": s.CreatedAt.Format(time.RFC3339Nano), "closedDate": s.MergedAt.Format(time.RFC3339Nano),
			"createdBy": map[string]string{"id": fixtureIdentityID}}
	}
	state := "closed"
	if s.State == notification.LifecycleOpen {
		state = "open"
	}
	pr := map[string]any{"number": s.ID, "state": state, "body": body, "created_at": s.CreatedAt.Format(time.RFC3339Nano),
		"user": map[string]string{"login": fixtureLogin},
		"head": map[string]any{"ref": s.Source, "sha": f.oid, "repo": map[string]string{"full_name": "example/repo"}},
		"base": map[string]any{"ref": s.Destination, "sha": f.oid, "repo": map[string]string{"full_name": "example/repo"}}}
	if s.State == notification.LifecycleMerged {
		pr["merged_at"] = s.MergedAt.Format(time.RFC3339Nano)
	}
	return pr
}

func (f *notificationBinaryFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	respond := func(value any) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(value); err != nil {
			f.t.Error(err)
		}
	}
	if strings.HasPrefix(r.URL.Path, "/receiver/") {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "" {
			f.t.Error("receiver method or forge-credential isolation violated")
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, (24<<10)+1))
		if err != nil || len(body) > 24<<10 || !json.Valid(body) {
			f.t.Error("invalid or oversized receiver payload", err)
		}
		f.received = append(f.received, notificationReceived{Body: body, Header: r.Header.Clone()})
		if f.failDelivery && strings.HasSuffix(r.URL.Path, "/endpoint-canary") {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "endpoint-canary must not be logged")
		} else if f.transport == "slack" {
			_, _ = io.WriteString(w, "ok")
		} else {
			w.WriteHeader(http.StatusAccepted)
		}
		return
	}
	collection := "/repos/example/repo/pulls"
	if f.provider == "azuredevops" {
		collection = "/project/_apis/git/repositories/repo/pullrequests"
	}
	if r.URL.Path == collection && r.Method == http.MethodPost {
		f.create(w, r, respond)
		return
	}
	// The token-identity lookup (ADR 0020): every fixture request is authored
	// by this identity, so the real provenance check runs and passes.
	if r.URL.Path == "/graphql" && r.Method == http.MethodPost {
		respond(map[string]any{"data": map[string]any{"viewer": map[string]string{"login": fixtureLogin}}})
		return
	}
	if r.URL.Path == "/project/_apis/wit/wiql" && r.Method == http.MethodPost {
		respond(map[string]any{"workItems": []any{}}) // read-only artifact query
		return
	}
	// Only fixture-created requests may receive baseline/metadata writes.
	if r.Method != http.MethodGet {
		for id := range f.bodies {
			requestPath := fmt.Sprintf("%s/%d", collection, id)
			if r.URL.Path == requestPath && r.Method == http.MethodPatch {
				var payload map[string]string
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					f.t.Error(err)
				}
				body := payload["body"]
				if f.provider == "azuredevops" {
					body = payload["description"]
				}
				f.bodies[id] = body
				respond(map[string]any{})
				return
			}
			if r.URL.Path == fmt.Sprintf("/repos/example/repo/issues/%d/labels", id) || r.URL.Path == requestPath+"/properties" {
				if f.failMetadata {
					w.WriteHeader(http.StatusBadRequest)
					respond(map[string]string{"message": "fixture metadata rejected"})
				} else {
					respond(map[string]any{})
				}
				return
			}
		}
		f.t.Errorf("unexpected forge mutation: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	switch {
	case r.URL.Path == "/repos/example/repo/issues":
		respond([]any{})
	case r.URL.Path == "/repos/example/repo":
		respond(map[string]any{"id": 123, "full_name": "example/repo", "allow_merge_commit": true, "allow_squash_merge": true, "allow_rebase_merge": true})
	case r.URL.Path == "/project/_apis/git/repositories/repo":
		respond(map[string]any{"id": "repository-id", "name": "repo", "project": map[string]string{"id": "project-id"}})
	case r.URL.Path == "/_apis/connectionData":
		respond(map[string]any{"instanceId": "organization-id", "authenticatedUser": map[string]string{"id": fixtureIdentityID}})
	case r.URL.Path == "/project/_apis/policy/configurations":
		respond(map[string]any{"value": []any{}})
	case strings.HasPrefix(r.URL.Path, "/repos/example/repo/rules/branches/"):
		respond([]any{})
	case strings.HasSuffix(r.URL.Path, "/protection"):
		w.WriteHeader(http.StatusNotFound)
	case strings.HasPrefix(r.URL.Path, "/repos/example/repo/labels/"):
		respond(map[string]string{"name": strings.TrimPrefix(r.URL.Path, "/repos/example/repo/labels/")})
	case strings.HasPrefix(r.URL.Path, "/repos/example/repo/compare/"):
		// Creation membership from the origin's OIDs: one fixture commit at the head.
		_, head, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/repos/example/repo/compare/"), "...")
		respond(map[string]any{"total_commits": 1, "commits": []map[string]any{{"sha": head, "commit": map[string]string{"message": "fixture commit"}}}})
	case r.URL.Path == collection:
		q := r.URL.Query()
		lifecycle := q.Get("state") == "all" || q.Get("searchCriteria.maxTime") != ""
		if lifecycle {
			f.lifecycleReads++
			if f.failDiscovery {
				w.WriteHeader(http.StatusBadRequest)
				respond(map[string]string{"message": "fixture scan unavailable"})
				return
			}
		}
		list := []any{}
		for _, seed := range f.seeds {
			if !lifecycle {
				open := q.Get("state") == "open" || q.Get("searchCriteria.status") == "active"
				if open != (seed.State == notification.LifecycleOpen) {
					continue
				}
			}
			list = append(list, f.pull(seed))
		}
		if f.provider == "azuredevops" {
			respond(map[string]any{"value": list, "count": len(list)})
		} else {
			respond(list)
		}
	case strings.HasPrefix(r.URL.Path, collection+"/"):
		if f.failDetails {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/properties") {
			respond(map[string]any{"value": map[string]any{}})
			return
		}
		id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, collection+"/"))
		for _, seed := range f.seeds {
			if seed.ID == id {
				respond(f.pull(seed))
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	default:
		f.t.Errorf("unexpected forge request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *notificationBinaryFixture) create(w http.ResponseWriter, r *http.Request, respond func(any)) {
	var input map[string]any
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		f.t.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	source, _ := input["head"].(string)
	target, _ := input["base"].(string)
	body, _ := input["body"].(string)
	if f.provider == "azuredevops" {
		source, _ = input["sourceRefName"].(string)
		target, _ = input["targetRefName"].(string)
		body, _ = input["description"].(string)
		source, target = strings.TrimPrefix(source, "refs/heads/"), strings.TrimPrefix(target, "refs/heads/")
	}
	if target == f.failTarget {
		w.WriteHeader(http.StatusBadRequest)
		respond(map[string]string{"message": "fixture create rejected"})
		return
	}
	f.createdTargets = append(f.createdTargets, target)
	s := f.seed(100+len(f.createdTargets), "promotion", "open", time.Now().UTC(), true)
	s.Source, s.Destination = source, target
	// A real forge stamps creation after the runner's pre-POST observation.
	// This one uses the test's clock, which a backward wall-clock step can put
	// behind that observation (#143), so it never stamps earlier than the
	// origin evidence in the body, and later invocations are floored there.
	created := time.Now().UTC()
	if origin, ok := mk.ParseNotificationOrigin(body); ok && created.Before(origin.ObservedAt) {
		created = origin.ObservedAt.UTC()
	}
	f.raiseFloorLocked(created)
	s.CreatedAt = created.Truncate(time.Second) // exercise coarse API timestamps
	f.seeds = append(f.seeds, s)
	f.bodies[s.ID] = body
	pr := f.pull(s)
	if f.provider == "azuredevops" {
		pr["description"] = body
	} else {
		pr["body"] = body
	}
	respond(pr)
}

func TestNotificationMergeBinary(t *testing.T) {
	t.Parallel()
	binary := buildNotificationBinary(t)
	for _, provider := range []string{"github", "azuredevops"} {
		for _, transport := range []string{"teams", "slack", "webhook"} {
			t.Run(provider+"/"+transport, func(t *testing.T) {
				t.Parallel()
				f := newNotificationBinaryFixture(t, binary, provider, transport)
				heads := gittest.Run(t, f.remote, "for-each-ref", "refs/heads/", "refs/tags/")
				old := f.seed(1, "promotion", "merged", time.Now().UTC().Add(-time.Hour), true)
				f.setSeeds(old)
				// Read-only invocation must not initialize notes or scan lifecycle.
				baseline, _ := f.run(0, "plan", "--detailed-exitcode")
				if refs := gittest.Run(t, f.remote, "for-each-ref", "refs/notes/"); refs != "" {
					t.Fatal("plan initialized notification state")
				}
				f.mu.Lock()
				reads := f.lifecycleReads
				f.mu.Unlock()
				if reads != 0 {
					t.Fatal("read-only invocation performed lifecycle discovery")
				}
				f.run(0, "reconcile")
				initial := f.ledger()
				if len(initial.Deliveries) != 0 || len(f.messages()) != 0 {
					t.Fatal("activation sent historical merge")
				}
				at := f.eventTime(initial)
				promotion := f.seed(42, "promotion", "merged", at, true)
				backflow := f.seed(43, "backflow", "merged", at, true)
				promotion.CreatedAt, backflow.CreatedAt = at, at // short-lived requests between runs
				f.setSeeds(old, promotion, backflow, f.seed(44, "promotion", "closed-unmerged", at, true), f.seed(45, "promotion", "merged", at, false))
				before := gittest.Run(t, f.remote, "for-each-ref", "refs/notes/")
				out, _ := f.run(0, "plan", "--detailed-exitcode")
				if notificationCorePlan(t, out) != notificationCorePlan(t, baseline) || before != gittest.Run(t, f.remote, "for-each-ref", "refs/notes/") || len(f.messages()) != 0 {
					t.Fatal("pending notifications changed the read-only plan, exit, remote notes, or receiver")
				}
				f.run(0, "reconcile")
				accepted := f.ledger()
				if len(accepted.Deliveries) != 2 || len(f.messages()) != 2 {
					t.Fatalf("want exactly promotion/backflow: deliveries=%+v messages=%d", accepted.Deliveries, len(f.messages()))
				}
				for _, receipt := range accepted.Deliveries {
					if receipt.Status != notification.StatusDelivered || receipt.Attempts != 1 || receipt.Message == nil {
						t.Fatalf("missing durable accepted payload/receipt: %+v", receipt)
					}
				}
				for _, msg := range f.messages() {
					text := string(msg.Body)
					if !strings.Contains(text, "request-merged") || !strings.Contains(text, "graph") || !strings.Contains(text, "sha256:") || strings.Contains(text, "fixture-forge-canary") {
						t.Fatalf("missing fixed event facts or leaked token: %s", text)
					}
					if transport == "webhook" {
						var envelope struct{ ID string }
						if err := json.Unmarshal(msg.Body, &envelope); err != nil || envelope.ID != msg.Header.Get("X-Oiax-Event-ID") {
							t.Fatal("webhook event ID/header mismatch", err)
						}
					}
				}
				// Fresh processes must reconstruct terminal receipts from remote Git.
				for range 3 {
					f.run(0, "reconcile")
				}
				if len(f.messages()) != 2 || !reflect.DeepEqual(accepted.Deliveries, f.ledger().Deliveries) {
					t.Fatal("fresh process resent or changed terminal receipts")
				}
				if got := gittest.Run(t, f.dir, "status", "--porcelain"); got != "" {
					t.Fatalf("notification processing changed checkout: %s", got)
				}
				if heads != gittest.Run(t, f.remote, "for-each-ref", "refs/heads/", "refs/tags/") {
					t.Fatal("notification processing changed remote branches or tags")
				}
			})
		}
		for _, coreExit := range []int{0, 1, 3} {
			t.Run(fmt.Sprintf("%s/core-exit-%d", provider, coreExit), func(t *testing.T) {
				t.Parallel()
				checkNotificationCoreExit(t, binary, provider, coreExit)
			})
		}
	}
}

// TestFixtureClockNeverDecreases runs the overlay clock's own unit tests
// (testdata/notification-binary/clock_test.go) deterministically. The clock
// lives in testdata and compiles only through an overlay, so this overlays
// clock.go and its tests into internal/reconcile, the package the binary
// fixture compiles them into, and runs just those tests. It covers the
// forward-then-backward wall reading from review on #153, where a floor
// without a high-water mark returned 103.5 s and then 101.1 s.
func TestFixtureClockNeverDecreases(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	overlay := map[string]map[string]string{"Replace": {
		filepath.Join(root, "internal/reconcile/notification_fixture.go"):      filepath.Join(root, "internal/cli/testdata/notification-binary/clock.go"),
		filepath.Join(root, "internal/reconcile/notification_fixture_test.go"): filepath.Join(root, "internal/cli/testdata/notification-binary/clock_test.go"),
	}}
	data, err := json.Marshal(overlay)
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-overlay", overlayPath, "-count=1", "-run", "^TestFixtureClock", "./internal/reconcile/")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture clock tests failed: %v\n%s", err, out)
	}
}

// TestNotificationFixtureClockFloor pins the fixture clock floor's two
// guarantees while the wall clock stays behind it (a floor an hour ahead). The
// floor advances on the test's monotonic clock between launches, and it is
// raised past every scan bound and cutoff the ledger already records. Without
// the second, the forced-low floor below hands the second invocation a clock
// ten minutes before the first one's scan watermark. Observe would then build
// an interval with From after Through, which the store rejects, and scanning
// would stall.
func TestNotificationFixtureClockFloor(t *testing.T) {
	t.Parallel()
	binary := buildNotificationBinary(t)
	f := newNotificationBinaryFixture(t, binary, "github", "webhook")

	start := time.Now().UTC().Add(time.Hour)
	f.mu.Lock()
	f.raiseFloorLocked(start)
	f.mu.Unlock()
	first := f.launchFloor()
	time.Sleep(20 * time.Millisecond)
	if second := f.launchFloor(); second.Sub(first) < 20*time.Millisecond {
		t.Fatalf("floor did not advance with elapsed time: %s then %s", first, second)
	}

	f.run(0, "reconcile")
	before := f.ledger()
	for kind, scan := range before.Scans {
		if !scan.Through.After(start) {
			t.Fatalf("%s scan ignored the floor: through %s, floor %s", kind, scan.Through, start)
		}
	}
	if len(before.Scans) != 2 {
		t.Fatalf("activation run recorded scans %v", before.Scans)
	}

	// Stand in for a floor that fell behind: an anchor that ran slow, or a
	// floor set before a run that read further ahead than it.
	f.mu.Lock()
	f.clockFloor, f.floorSetAt = ledgerWatermark(before).Add(-10*time.Minute), time.Now()
	f.mu.Unlock()
	f.run(0, "reconcile")
	after := f.ledger()
	for kind, prev := range before.Scans {
		next, ok := after.Scans[kind]
		if !ok || !next.Complete || next.From.Before(prev.Through) || !next.Through.After(prev.Through) {
			t.Fatalf("%s scan did not progress from %+v: %+v", kind, prev, next)
		}
	}
}

func notificationCorePlan(t *testing.T, document string) string {
	t.Helper()
	var value map[string]json.RawMessage
	if err := json.Unmarshal([]byte(document), &value); err != nil {
		t.Fatal(err)
	}
	delete(value, "notifications")
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func checkNotificationCoreExit(t *testing.T, binary, provider string, coreExit int) {
	t.Helper()
	f := newNotificationBinaryFixture(t, binary, provider, "webhook",
		"      - {name: audit, type: webhook, endpointEnv: OIAX_FIXTURE_AUDIT}\n")
	f.run(0, "reconcile")
	f.setSeeds(f.seed(42, "promotion", "merged", f.eventTime(f.ledger()), true))
	f.mu.Lock()
	f.failDelivery = true
	if coreExit == 1 {
		f.failTarget = "stage" // dev->test succeeds before dev->stage fails
	}
	f.mu.Unlock()
	planExit := 0
	if coreExit != 0 {
		branch := "stage" // unmanaged downstream drift requires human attention
		planExit = 3
		if coreExit == 1 {
			branch, planExit = "dev", 2
		}
		gittest.Run(t, f.dir, "checkout", "-q", branch)
		writeNotificationFixture(t, filepath.Join(f.dir, "app.txt"), []byte("new content\n"))
		gittest.Run(t, f.dir, "add", "app.txt")
		gittest.Run(t, f.dir, "commit", "-qm", "branch change")
		f.publish(branch)
	}
	before := gittest.Run(t, f.remote, "for-each-ref", "refs/notes/")
	planJSON, _ := f.run(planExit, "plan", "--detailed-exitcode")
	if len(f.messages()) != 0 || before != gittest.Run(t, f.remote, "for-each-ref", "refs/notes/") {
		t.Fatal("detailed plan mutated notification state")
	}
	applyJSON, stderr := f.run(coreExit, "reconcile")
	if applyJSON != planJSON {
		t.Fatal("notification failure changed the core JSON plan")
	}
	if !strings.Contains(stderr, "service-failure") {
		t.Fatalf("missing safe notification failure diagnostic: %s", stderr)
	}
	if coreExit == 1 {
		f.mu.Lock()
		created := append([]string(nil), f.createdTargets...)
		f.mu.Unlock()
		if !reflect.DeepEqual(created, []string{"test"}) || !strings.Contains(stderr, "apply create dev->stage") {
			t.Fatalf("expected actual partial core progress and original error: created=%v stderr=%s", created, stderr)
		}
	}
	l := f.ledger()
	if len(l.Deliveries) != 2 || len(f.messages()) != 2 {
		t.Fatalf("failure starved eligible dispatch: %+v, messages=%d", l.Deliveries, len(f.messages()))
	}
	for _, receipt := range l.Deliveries {
		if receipt.Attempts != 1 || receipt.Message == nil {
			t.Fatalf("missing persisted attempt/message: %+v", receipt)
		}
		if receipt.Destination == "ops" {
			if receipt.Status != notification.StatusRetryable || receipt.Code != notification.OutcomeService || receipt.NextAttemptAt.IsZero() {
				t.Fatalf("failure lost retry state: %+v", receipt)
			}
		} else if receipt.Status != notification.StatusDelivered || receipt.Code != notification.OutcomeAccepted {
			t.Fatalf("independent receiver not delivered: %+v", receipt)
		}
	}
	// Both a not-yet-due failure and a terminal success survive a new process.
	// The partial-failure case has an open core PR now; that request's update
	// lifecycle is outside this fixture, so its repeat is covered by the other
	// two exit cases and the merge/receipt subprocess matrix above.
	if coreExit != 1 {
		f.run(coreExit, "reconcile")
		if len(f.messages()) != 2 || !reflect.DeepEqual(l.Deliveries, f.ledger().Deliveries) {
			t.Fatal("fresh process resent before retry was due or changed receipts")
		}
	}
}
