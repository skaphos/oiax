package reconcile

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/v2/internal/engine"
	"github.com/skaphos/oiax/v2/internal/forge"
	"github.com/skaphos/oiax/v2/internal/notification"
	"github.com/skaphos/oiax/v2/internal/notification/notificationtest"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

type observationReader struct {
	identity notification.RepositoryIdentity
	list     func(forge.LifecycleQuery) (forge.LifecyclePage, error)
	get      func(forge.RequestID) (notification.LifecycleRequest, error)
}

func (r *observationReader) RepositoryIdentity(context.Context) (notification.RepositoryIdentity, error) {
	return r.identity, nil
}

func (r *observationReader) ListLifecyclePage(_ context.Context, query forge.LifecycleQuery) (forge.LifecyclePage, error) {
	return r.list(query)
}

func (r *observationReader) GetLifecycleRequest(_ context.Context, id forge.RequestID) (notification.LifecycleRequest, error) {
	return r.get(id)
}

func observationGraph() *engine.Graph {
	return &engine.Graph{Name: "graph", Promotions: []engine.Promotion{{From: "dev", To: "test"}}, Backflow: engine.BackflowPolicy{Enabled: true, Sources: []string{"test"}, Target: "dev"}}
}

func lifecycleMerge(repo notification.RepositoryIdentity, id string, created, merged time.Time) notification.LifecycleRequest {
	return notification.LifecycleRequest{Repository: repo, Graph: "graph", State: notification.LifecycleMerged, CreatedAt: created, MergedAt: merged, Request: notification.RequestV1{ID: id, Type: v1.NotificationPromotion, Source: "dev", Destination: "test", LogicalSource: "dev", LogicalDestination: "test", URL: "https://github.com/example/repo/pull/" + id}}
}

func TestNotificationObservePersistsPartialDiscoveryAndDispatchesPending(t *testing.T) {
	t.Parallel()
	activatedAt := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	clock := notificationtest.NewClock(activatedAt)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "ENDPOINT"}}}
	store := &notificationtest.MemoryStore{}
	runtime := mergeRuntime(clock.Now, store, policy)
	runtime.Topology = observationGraph()
	request := lifecycleMerge(runtime.Repository, "42", activatedAt.Add(-365*24*time.Hour), activatedAt.Add(time.Minute))
	reader := &observationReader{identity: runtime.Repository}
	reader.get = func(forge.RequestID) (notification.LifecycleRequest, error) {
		return notification.LifecycleRequest{}, notification.ErrRequestMissing
	}
	reader.list = func(query forge.LifecycleQuery) (forge.LifecyclePage, error) {
		progress := notification.ScanProgress{Version: 1, From: query.From, Through: query.Through, Complete: true}
		if query.Kind == v1.NotificationRequestMerged {
			progress.Complete = false
			progress.Cursor = "resume-merge"
			return forge.LifecyclePage{Requests: []notification.LifecycleRequest{request}, Progress: progress, Pages: 1}, notification.ErrDiscoveryIncomplete
		}
		return forge.LifecyclePage{Progress: progress, Pages: 1}, nil
	}
	runtime.Reader = reader
	lookupCalls := 0
	runtime.LookupEnv = func(name string) (string, bool) {
		lookupCalls++
		if name != "ENDPOINT" {
			t.Errorf("looked up unexpected endpoint variable %q", name)
		}
		return "https://receiver.invalid/secret", true
	}
	sender := &notificationtest.Recorder{Result: notification.AttemptResult{Code: notification.OutcomeAccepted}}
	runtime.Sender = func(v1.NotificationDestination) notification.Sender { return sender }
	if err := runtime.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Minute)
	if err := runtime.Observe(context.Background()); !errors.Is(err, notification.ErrDiscoveryIncomplete) {
		t.Fatalf("incomplete observation = %v", err)
	}
	if lookupCalls != 0 {
		t.Fatal("observation resolved a delivery secret")
	}
	snapshot, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mergeScan := snapshot.Ledger.Scans[string(v1.NotificationRequestMerged)]
	if mergeScan.Complete || mergeScan.Cursor != "resume-merge" || snapshot.Ledger.KnownRequests["42"].State != notification.LifecycleMerged || len(snapshot.Ledger.Events) != 1 || len(snapshot.Ledger.Deliveries) != 1 {
		t.Fatalf("partial lifecycle facts were not committed: scan=%+v ledger=%+v", mergeScan, snapshot.Ledger)
	}
	if err := runtime.Dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if lookupCalls != 1 || len(sender.Payloads()) != 1 {
		t.Fatalf("incomplete scan blocked pending dispatch: lookups=%d sends=%d", lookupCalls, len(sender.Payloads()))
	}
	snapshot, err = store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, delivery := range snapshot.Ledger.Deliveries {
		if delivery.Status != notification.StatusDelivered || delivery.DeliveredAt.IsZero() {
			t.Fatalf("receipt not durable: %+v", delivery)
		}
	}
}

func TestNotificationObserveRetainsUnknownOpenRequestAndContinuesScanning(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "ENDPOINT"}}}
	store := &notificationtest.MemoryStore{}
	runtime := mergeRuntime(func() time.Time { return now }, store, policy)
	runtime.Topology = observationGraph()
	if err := runtime.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	open := lifecycleMerge(runtime.Repository, "41", now.Add(-time.Hour), now)
	open.State = notification.LifecycleOpen
	open.MergedAt = time.Time{}
	if err := runtime.recordObservation(context.Background(), []notification.LifecycleRequest{open}, "", notification.ScanProgress{}, now); err != nil {
		t.Fatal(err)
	}
	lists := 0
	runtime.Reader = &observationReader{
		identity: runtime.Repository,
		get: func(forge.RequestID) (notification.LifecycleRequest, error) {
			return notification.LifecycleRequest{}, notification.ErrLifecycleUnavailable
		},
		list: func(query forge.LifecycleQuery) (forge.LifecyclePage, error) {
			lists++
			return forge.LifecyclePage{Progress: notification.ScanProgress{Version: 1, From: query.From, Through: query.Through, Complete: true}, Pages: 1}, nil
		},
	}
	if err := runtime.Observe(context.Background()); !errors.Is(err, notification.ErrLifecycleUnavailable) {
		t.Fatalf("unknown direct refresh = %v", err)
	}
	if lists != 2 {
		t.Fatalf("known-request failure stopped independent scans: %d", lists)
	}
	snapshot, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Ledger.KnownRequests["41"].State != notification.LifecycleOpen || len(snapshot.Ledger.Events) != 0 {
		t.Fatalf("unknown refresh fabricated lifecycle state: %+v", snapshot.Ledger.KnownRequests["41"])
	}
}

// A first scan must not walk history that no subscription can ever admit (#93):
// AdmitEvent discards anything older than the activation cutoff, so scanning it
// only costs provider calls and fills the ledger with requests that can never
// produce a delivery. Later scans resume from their own completed watermark.
func TestNotificationObserveBoundsFirstScanByActivationCutoff(t *testing.T) {
	t.Parallel()
	activatedAt := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	clock := notificationtest.NewClock(activatedAt)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "ENDPOINT", Events: []v1.NotificationEvent{v1.NotificationRequestCreated, v1.NotificationRequestMerged}}}}
	store := &notificationtest.MemoryStore{}
	runtime := mergeRuntime(clock.Now, store, policy)
	runtime.Topology = observationGraph()
	// A managed request from long before activation. A provider that honors the
	// query interval never reports it, so nothing about it reaches the ledger.
	historic := lifecycleMerge(runtime.Repository, "7", activatedAt.Add(-365*24*time.Hour), activatedAt.Add(-364*24*time.Hour))
	windows := map[v1.NotificationEvent]forge.LifecycleQuery{}
	runtime.Reader = &observationReader{
		identity: runtime.Repository,
		get: func(forge.RequestID) (notification.LifecycleRequest, error) {
			return notification.LifecycleRequest{}, notification.ErrRequestMissing
		},
		list: func(query forge.LifecycleQuery) (forge.LifecyclePage, error) {
			windows[query.Kind] = query
			page := forge.LifecyclePage{Progress: notification.ScanProgress{Version: 1, From: query.From, Through: query.Through, Complete: true}, Pages: 1}
			if !historic.MergedAt.Before(query.From) {
				page.Requests = append(page.Requests, historic)
			}
			return page, nil
		},
	}
	if err := runtime.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Minute)
	if err := runtime.Observe(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []v1.NotificationEvent{v1.NotificationRequestCreated, v1.NotificationRequestMerged} {
		// One second below the cutoff, because same-second creation evidence can
		// move admission that far past a recorded occurrence (EventAdmissionTime).
		if want := activatedAt.Add(-time.Second); !windows[kind].From.Equal(want) {
			t.Fatalf("%s first scan from %s, want the activation cutoff %s", kind, windows[kind].From, want)
		}
	}
	snapshot, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Ledger.KnownRequests) != 0 || len(snapshot.Ledger.Events) != 0 || len(snapshot.Ledger.Deliveries) != 0 {
		t.Fatalf("first activation persisted unadmittable history: %+v", snapshot.Ledger)
	}
	clock.Advance(time.Minute)
	if err := runtime.Observe(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []v1.NotificationEvent{v1.NotificationRequestCreated, v1.NotificationRequestMerged} {
		if want := activatedAt.Add(time.Minute); !windows[kind].From.Equal(want) {
			t.Fatalf("%s resumed from %s, want its completed watermark %s", kind, windows[kind].From, want)
		}
	}
}

func TestNotificationObserveBoundsProviderPagesAndRetainsCursor(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "ENDPOINT"}}}
	store := &notificationtest.MemoryStore{}
	runtime := mergeRuntime(func() time.Time { return now }, store, policy)
	runtime.Topology = observationGraph()
	if err := runtime.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := 0
	runtime.Reader = &observationReader{
		identity: runtime.Repository,
		get: func(forge.RequestID) (notification.LifecycleRequest, error) {
			return notification.LifecycleRequest{}, notification.ErrRequestMissing
		},
		list: func(query forge.LifecycleQuery) (forge.LifecyclePage, error) {
			calls++
			return forge.LifecyclePage{Progress: notification.ScanProgress{Version: 1, From: query.From, Through: query.Through, Cursor: strconv.Itoa(calls)}, Pages: 2}, nil
		},
	}
	if err := runtime.Observe(context.Background()); !errors.Is(err, notification.ErrDiscoveryIncomplete) {
		t.Fatalf("bounded observation = %v", err)
	}
	if calls == 0 || calls*2 > 100 {
		t.Fatalf("provider page budget exceeded: calls=%d pages=%d", calls, calls*2)
	}
	snapshot, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	progress := snapshot.Ledger.Scans[string(v1.NotificationRequestCreated)]
	if progress.Complete || progress.Cursor != strconv.Itoa(calls) {
		t.Fatalf("bounded scan cursor lost: %+v", progress)
	}
}

func TestNotificationStaleRuntimeCannotObserveOrDispatch(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "ENDPOINT"}}}
	store := &notificationtest.MemoryStore{}
	old := mergeRuntime(func() time.Time { return now }, store, policy)
	old.Topology = observationGraph()
	listCalls := 0
	old.Reader = &observationReader{identity: old.Repository, get: func(forge.RequestID) (notification.LifecycleRequest, error) {
		return notification.LifecycleRequest{}, notification.ErrRequestMissing
	}, list: func(query forge.LifecycleQuery) (forge.LifecyclePage, error) {
		listCalls++
		return forge.LifecyclePage{Progress: notification.ScanProgress{Version: 1, From: query.From, Through: query.Through, Complete: true}, Pages: 1}, nil
	}}
	lookupCalls := 0
	old.LookupEnv = func(string) (string, bool) { lookupCalls++; return "", false }
	old.Sender = func(v1.NotificationDestination) notification.Sender {
		t.Fatal("stale runtime created sender")
		return nil
	}
	if err := old.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	newer := mergeRuntime(func() time.Time { return now.Add(time.Minute) }, store, policy)
	newer.ConfigOID = strings.Repeat("b", 40)
	newer.VerifyRevision = func(_ context.Context, accepted, incoming string) (notification.RevisionRelation, error) {
		if accepted != old.ConfigOID || incoming != newer.ConfigOID {
			t.Fatalf("wrong revision comparison: %s -> %s", accepted, incoming)
		}
		return notification.RevisionDescendant, nil
	}
	if err := newer.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := old.Observe(context.Background()); !errors.Is(err, notification.ErrStaleRevision) {
		t.Fatalf("stale observation = %v", err)
	}
	if err := old.Dispatch(context.Background()); !errors.Is(err, notification.ErrStaleRevision) {
		t.Fatalf("stale dispatch = %v", err)
	}
	if listCalls != 0 || lookupCalls != 0 {
		t.Fatalf("stale runtime reached effects: lists=%d lookups=%d", listCalls, lookupCalls)
	}
}
