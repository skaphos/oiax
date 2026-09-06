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

// Build the real entrypoint with fixture-only connection setup. See testdata's
// README for the exact boundary and the distinction from live acceptance.
func buildNotificationBinary(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	runtimePath := filepath.Join(root, "internal/reconcile/notification_runtime.go")
	source, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	const constructor = "return delivery.NewClient(destination.Type, destination.AllowPrivateNetwork)"
	if strings.Count(string(source), constructor) != 1 {
		t.Fatal("notification constructor moved; review the fixture overlay boundary")
	}
	override := filepath.Join(dir, "notification_runtime.go")
	writeNotificationFixture(t, override, []byte(strings.Replace(string(source), constructor,
		"return delivery.NewFixtureClient(destination.Type, destination.AllowPrivateNetwork)", 1)))
	overlay := map[string]map[string]string{"Replace": {
		runtimePath: override,
		filepath.Join(root, "internal/cli/notification_fixture.go"):                   filepath.Join(root, "internal/cli/testdata/notification-binary/forge.go"),
		filepath.Join(root, "internal/notification/delivery/notification_fixture.go"): filepath.Join(root, "internal/cli/testdata/notification-binary/delivery.go"),
	}}
	data, err := json.Marshal(overlay)
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	writeNotificationFixture(t, overlayPath, data)
	binary := filepath.Join(dir, "oiax.exe")
	args := []string{"build", "-overlay", overlayPath, "-o", binary}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "-race" && setting.Value == "true" {
				args = append(args, "-race")
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", append(args, "./cmd/oiax")...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fixture CLI: %v\n%s", err, out)
	}
	return binary
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
	server                   *httptest.Server
	mu                       sync.Mutex
	seeds                    []forgetest.LifecycleSeed
	bodies                   map[int]string
	received                 []notificationReceived
	failDelivery             bool
	missingEndpoint          bool
	failMetadata             bool
	failDiscovery            bool
	failDetails              bool
	failTarget               string
	createdTargets           []string
	lifecycleReads           int
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

func (f *notificationBinaryFixture) run(want int, args ...string) (string, string) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	args = append(args, "--config-ref", f.oid, "--output", "json")
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
	if !json.Valid(out.Bytes()) {
		f.t.Fatalf("stdout is not a single JSON plan: %s", out.String())
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
	ref := "refs/notes/oiax/notifications/v1/" + notification.GraphKey(f.identity(), "graph")
	entries := strings.Fields(gittest.Run(f.t, f.remote, "ls-tree", "-r", ref))
	if len(entries) != 4 {
		f.t.Fatalf("unexpected notes tree: %v", entries)
	}
	data := gittest.Run(f.t, f.remote, "cat-file", "blob", entries[2])
	if strings.Contains(data, "endpoint-canary") || strings.Contains(data, "fixture-forge-canary") || strings.Contains(data, "healthy-canary") {
		f.t.Fatal("credential canary persisted in notes")
	}
	l, err := notificationstore.Decode(strings.NewReader(data))
	if err != nil {
		f.t.Fatal(err)
	}
	return l
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
			"creationDate": s.CreatedAt.Format(time.RFC3339Nano), "closedDate": s.MergedAt.Format(time.RFC3339Nano)}
	}
	state := "closed"
	if s.State == notification.LifecycleOpen {
		state = "open"
	}
	pr := map[string]any{"number": s.ID, "state": state, "body": body, "created_at": s.CreatedAt.Format(time.RFC3339Nano),
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
		respond(map[string]string{"instanceId": "organization-id"})
	case r.URL.Path == "/project/_apis/policy/configurations":
		respond(map[string]any{"value": []any{}})
	case strings.HasPrefix(r.URL.Path, "/repos/example/repo/rules/branches/"):
		respond([]any{})
	case strings.HasSuffix(r.URL.Path, "/protection"):
		w.WriteHeader(http.StatusNotFound)
	case strings.HasPrefix(r.URL.Path, "/repos/example/repo/labels/"):
		respond(map[string]string{"name": strings.TrimPrefix(r.URL.Path, "/repos/example/repo/labels/")})
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
	s.CreatedAt = time.Now().UTC().Truncate(time.Second) // exercise coarse API timestamps
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
				at := time.Now().UTC()
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
	f.setSeeds(f.seed(42, "promotion", "merged", time.Now().UTC(), true))
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
