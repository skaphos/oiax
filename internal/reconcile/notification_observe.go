package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/skaphos/oiax/v2/internal/engine"
	"github.com/skaphos/oiax/v2/internal/forge"
	"github.com/skaphos/oiax/v2/internal/notification"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

// NotificationRuntime owns effects independently of the branch engine. All
// callback dependencies are installed before use; one instance is one invocation.
type NotificationRuntime struct {
	// hintMu guards revision, the last ledger tip this runtime read or wrote.
	// It is the hint passed to LedgerStore.Commit so a write needs no separate
	// pre-read.
	hintMu   sync.Mutex
	revision string
	// commitSlot serializes commits so concurrent destination batches do not
	// conflict against each other. It is acquired under the caller's context:
	// a bounded receipt write must not spend its budget queued behind another
	// batch's blocked notes operation, and then fail once it is admitted.
	commitOnce     sync.Once
	commitSlot     chan struct{}
	Store          notification.LedgerStore
	Reader         forge.LifecycleReader
	Repository     notification.RepositoryIdentity
	Graph          string
	Topology       *engine.Graph
	ConfigOID      string
	Policy         *v1.NotificationPolicy
	Now            func() time.Time
	OperationID    func() string
	LookupEnv      func(string) (string, bool)
	Sender         func(v1.NotificationDestination) notification.Sender
	VerifyRevision func(context.Context, string, string) (notification.RevisionRelation, error)
	Wait           func(context.Context, time.Duration) error
	Close          func() error
	Report         func(NotificationDiagnostic)
	Log            *slog.Logger
}

// discardLogger is shared so per-attempt logging never allocates a handler
// when no logger was installed.
var discardLogger = slog.New(slog.DiscardHandler)

func (r *NotificationRuntime) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return discardLogger
}

func (r *NotificationRuntime) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func policyDigest(p *v1.NotificationPolicy) string {
	// Canonicalize selection and destination ordering without changing caller
	// slices. Templates here are already resolved at the pinned configuration OID.
	data, _ := json.Marshal(p)
	var normalized v1.NotificationPolicy
	_ = json.Unmarshal(data, &normalized)
	normalized.Default()
	sort.Slice(normalized.Destinations, func(i, j int) bool { return normalized.Destinations[i].Name < normalized.Destinations[j].Name })
	for i := range normalized.Destinations {
		d := &normalized.Destinations[i]
		sort.Slice(d.Events, func(i, j int) bool { return d.Events[i] < d.Events[j] })
		sort.Slice(d.RequestTypes, func(i, j int) bool { return d.RequestTypes[i] < d.RequestTypes[j] })
	}
	data, _ = json.Marshal(normalized)
	return notification.Digest("policy-v1", string(data))
}

// read observes the durable ledger and remembers its tip for later commits.
func (r *NotificationRuntime) read(ctx context.Context) (notification.Snapshot, error) {
	snapshot, err := r.Store.Read(ctx)
	if err == nil || errors.Is(err, notification.ErrAbsent) {
		r.hintMu.Lock()
		r.revision = snapshot.Revision
		r.hintMu.Unlock()
	}
	return snapshot, err
}

// acquireCommit takes the commit slot or gives up when the context ends first.
func (r *NotificationRuntime) acquireCommit(ctx context.Context) error {
	r.commitOnce.Do(func() { r.commitSlot = make(chan struct{}, 1) })
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case r.commitSlot <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *NotificationRuntime) releaseCommit() { <-r.commitSlot }

// commit supplies the last observed revision as the hint LedgerStore requires
// and retries only explicit CAS conflicts, re-reading before each retry. The
// transition is deliberately passed through unchanged so every attempt reduces
// a freshly read snapshot; the store itself never trusts the hint.
func (r *NotificationRuntime) commit(ctx context.Context, transition notification.Transition) (notification.Snapshot, error) {
	if r.Store == nil || transition == nil {
		return notification.Snapshot{}, notification.ErrInvalidState
	}
	if err := r.acquireCommit(ctx); err != nil {
		return notification.Snapshot{}, err
	}
	defer r.releaseCommit()
	r.hintMu.Lock()
	expected := r.revision
	r.hintMu.Unlock()
	for attempt := range 5 {
		if attempt > 0 {
			current, err := r.Store.Read(ctx)
			if err != nil && !errors.Is(err, notification.ErrAbsent) {
				return notification.Snapshot{}, err
			}
			expected = current.Revision
		}
		next, err := r.Store.Commit(ctx, expected, transition)
		if errors.Is(err, notification.ErrConflict) {
			continue
		}
		if err == nil {
			r.hintMu.Lock()
			r.revision = next.Revision
			r.hintMu.Unlock()
		}
		return next, err
	}
	return notification.Snapshot{}, notification.ErrConflict
}

func (r *NotificationRuntime) Activate(ctx context.Context) error {
	if !r.Policy.IsEnabled() {
		return nil
	}
	if r.Store == nil || !notification.ValidOID(r.ConfigOID) {
		return notification.ErrInvalidState
	}
	revision := notification.PolicyRevisionV1{ConfigOID: r.ConfigOID, PolicyDigest: policyDigest(r.Policy)}
	_, initialErr := r.read(ctx)
	now := r.now()
	_, err := r.commit(ctx, func(ctx context.Context, current *notification.LedgerV1) (*notification.LedgerV1, error) {
		if current == nil {
			current = notification.NewLedger(r.Repository, r.Graph, r.ConfigOID)
		}
		evidence := notification.RevisionEvidence{AcceptedOID: current.PolicyRevision.ConfigOID, IncomingOID: r.ConfigOID}
		if evidence.AcceptedOID != "" && evidence.AcceptedOID != r.ConfigOID && r.VerifyRevision != nil {
			relation, err := r.VerifyRevision(ctx, evidence.AcceptedOID, r.ConfigOID)
			if err != nil {
				// Fail closed either way; preserve the one failure that names a
				// recovery instead of flattening it to a generic disorder.
				if errors.Is(err, notification.ErrRevisionUnreachable) {
					return nil, notification.ErrRevisionUnreachable
				}
				return nil, notification.ErrUnorderedRevision
			}
			evidence.Relation = relation
		}
		return notification.AcceptPolicy(current, revision, r.Policy, now, evidence)
	})
	if err == nil && errors.Is(initialErr, notification.ErrAbsent) && r.Report != nil {
		r.Report(NotificationDiagnostic{Reason: "notification-ledger-initialized", Action: "Established a current cutoff without historical backfill. If prior notes were lost, review and restore their receipts before further runs."})
	}
	return err
}

// ResetRevision records an operator-authorized acceptance of the pinned
// configuration revision for a ledger whose accepted revision can no longer be
// ordered against it, because the commit it names is gone from the repository.
// Without it that ledger defers on every run forever: no descendant can be
// committed onto a commit nobody has, and deleting the notes ref — the only
// other way out — destroys the delivery receipts that prevent duplicate sends.
//
// It is narrow on purpose. Immutable events and attempt/receipt evidence are
// preserved, while the accepted policy, subscriptions, cutoffs and retirement
// of now-ineligible nonterminal deliveries are applied by the ordinary policy
// reducer. It refuses outright while the accepted commit is still resolvable
// (VerifyRevision answers ErrRevisionReachable there, so ordinary ordering still
// governs an ordinary ledger); and it is a no-op returning a nil record when the
// pinned revision is already accepted, so a retried recovery is safe.
//
// "Already accepted" means the whole PolicyRevision, not just its OID. A
// matching OID carrying a different digest is ErrPolicyMismatch to every
// ordinary run, and a recovery command must not be the one path that reports
// success on a ledger the rest of the system refuses.
func (r *NotificationRuntime) ResetRevision(ctx context.Context) (*notification.RevisionOverrideV1, error) {
	if !r.Policy.IsEnabled() || r.Store == nil || r.VerifyRevision == nil || !notification.ValidOID(r.ConfigOID) {
		return nil, notification.ErrInvalidState
	}
	revision := notification.PolicyRevisionV1{ConfigOID: r.ConfigOID, PolicyDigest: policyDigest(r.Policy)}
	now := r.now()
	var applied *notification.RevisionOverrideV1
	_, err := r.commit(ctx, func(ctx context.Context, current *notification.LedgerV1) (*notification.LedgerV1, error) {
		// Every attempt reduces a freshly read snapshot, so the outcome of an
		// abandoned one must not survive into the next.
		applied = nil
		if current == nil {
			// Nothing to recover before the first activation, and inventing a
			// ledger here would establish a cutoff nobody asked for.
			return nil, notification.ErrAbsent
		}
		accepted := current.PolicyRevision
		if accepted == revision {
			// Already accepted, digest and all: a re-run of the recovery writes
			// nothing and records no second override.
			return current, nil
		}
		if accepted.ConfigOID == r.ConfigOID {
			// Same commit, different policy digest — the mixed-binary-version
			// state, not an unreachable revision. CheckRevision calls it
			// ErrPolicyMismatch and Activate refuses it; a recovery command that
			// reported success here would certify a ledger that every ordinary
			// run rejects.
			return nil, notification.ErrPolicyMismatch
		}
		if !notification.ValidOID(accepted.ConfigOID) {
			return nil, notification.ErrInvalidState
		}
		evidence := notification.RevisionEvidence{AcceptedOID: accepted.ConfigOID, IncomingOID: r.ConfigOID}
		// Recomputed against this attempt's snapshot, exactly as an ordinary
		// activation recomputes it: a concurrent worker that repaired the
		// revision first must make this reset fail, not replay stale evidence.
		relation, verifyErr := r.VerifyRevision(ctx, accepted.ConfigOID, r.ConfigOID)
		if verifyErr != nil {
			return nil, verifyErr
		}
		evidence.Relation = relation
		next, err := notification.OverrideRevision(current, revision, r.Policy, now, evidence)
		if err != nil {
			return nil, err
		}
		applied = &next.RevisionOverrides[len(next.RevisionOverrides)-1]
		return next, nil
	})
	if err != nil {
		return nil, err
	}
	if applied == nil {
		return nil, nil
	}
	record := *applied
	if r.Report != nil {
		r.Report(NotificationDiagnostic{Reason: "notification-revision-reset", Action: "An operator accepted the pinned configuration revision without ancestry because the previously accepted commit is gone. The override is recorded in the ledger; confirm the configuration history rewrite was intended."})
	}
	return &record, nil
}

func (r *NotificationRuntime) Admit(ctx context.Context, events []notification.EventV1) error {
	if !r.Policy.IsEnabled() || len(events) == 0 {
		return nil
	}
	_, err := r.commit(ctx, func(_ context.Context, l *notification.LedgerV1) (*notification.LedgerV1, error) {
		if l == nil {
			return nil, notification.ErrAbsent
		}
		var err error
		for _, event := range events {
			l, err = notification.AdmitEvent(l, r.ConfigOID, event)
			if err != nil {
				return nil, err
			}
		}
		return l, nil
	})
	return err
}

// Observe polls known open requests independently of bounded catch-up scans.
// Partial discovery is committed without advancing its completed watermark;
// pending deliveries are handled even if this returns a discovery diagnostic.
func (r *NotificationRuntime) Observe(ctx context.Context) error {
	if !r.Policy.IsEnabled() {
		return nil
	}
	if r.Reader == nil || r.Topology == nil {
		return notification.ErrLifecycleUnavailable
	}
	snapshot, err := r.read(ctx)
	if err != nil {
		return err
	}
	if snapshot.Ledger.PolicyRevision.ConfigOID != r.ConfigOID {
		return notification.ErrStaleRevision
	}
	now := r.now()
	var problems []error
	for _, known := range snapshot.Ledger.KnownRequests {
		if known.State != notification.LifecycleOpen {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		request, err := r.Reader.GetLifecycleRequest(ctx, forge.RequestID(known.Request.ID))
		if err != nil {
			problems = append(problems, notification.ErrLifecycleUnavailable)
			continue
		}
		if err := r.recordObservation(ctx, []notification.LifecycleRequest{request}, "", notification.ScanProgress{}, now); err != nil {
			return err
		}
	}
	pages := 0
	for _, kind := range []v1.NotificationEvent{v1.NotificationRequestCreated, v1.NotificationRequestMerged} {
		name := string(kind)
		previous := snapshot.Ledger.Scans[name]
		q := forge.LifecycleQuery{Graph: r.Graph, Kind: kind, Limit: 100, Through: now}
		if previous.Version != 0 && !previous.Complete {
			// A frozen interval already in progress is resumed exactly as
			// recorded; only its cursor advances.
			q.From, q.Through, q.Cursor = previous.From, previous.Through, previous.Cursor
		} else {
			if previous.Version != 0 {
				q.From = previous.Through
			}
			// A new interval never starts below the earliest cutoff that could
			// still admit this kind: AdmitEvent discards everything older, so
			// scanning it only buys provider calls and known-request bytes for
			// events that can never be delivered. With no subscription for the
			// kind at all, only what happens from here on can ever qualify.
			bound, subscribed := notification.EarliestAdmissibleTime(snapshot.Ledger, kind)
			if !subscribed {
				bound = q.Through
			}
			if q.From.Before(bound) {
				q.From = bound
			}
		}
		for pages < 98 {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			page, scanErr := r.Reader.ListLifecyclePage(ctx, q)
			pages += max(page.Pages, 1)
			if page.Progress.Version == 0 {
				page.Progress = notification.ScanProgress{Version: 1, From: q.From, Through: q.Through, Cursor: q.Cursor}
			}
			if scanErr != nil {
				page.Progress.Complete = false
			}
			if err := r.recordObservation(ctx, page.Requests, name, page.Progress, now); err != nil {
				return err
			}
			if scanErr != nil {
				problems = append(problems, notification.ErrDiscoveryIncomplete)
				break
			}
			if page.Progress.Complete {
				break
			}
			q.Cursor = page.Progress.Cursor
		}
		if pages >= 98 {
			problems = append(problems, notification.ErrDiscoveryIncomplete)
			break
		}
	}
	return errors.Join(problems...)
}

func (r *NotificationRuntime) recordObservation(ctx context.Context, requests []notification.LifecycleRequest, scan string, progress notification.ScanProgress, now time.Time) error {
	// Provider enrichment happens outside CAS callbacks. Conflicts re-reduce
	// captured facts, never reread a moving remote inside a state transition.
	snapshot, err := r.read(ctx)
	if err != nil {
		return err
	}
	if snapshot.Ledger == nil {
		return notification.ErrAbsent
	}
	if snapshot.Ledger.PolicyRevision.ConfigOID != r.ConfigOID {
		return notification.ErrStaleRevision
	}
	events := map[string]notification.EventV1{}
	reader, canEnrich := r.Reader.(forge.SnapshotReader)
	enrichmentCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for _, request := range requests {
		if !request.Repository.Same(r.Repository) || request.Graph != r.Graph {
			continue
		}
		for _, normalize := range []func(*engine.Graph, *v1.NotificationPolicy, notification.LifecycleRequest, time.Time) (notification.EventV1, bool){notification.CreationEvent, notification.MergeEvent} {
			event, eligible := normalize(r.Topology, r.Policy, request, now)
			if !eligible {
				continue
			}
			if existing, ok := snapshot.Ledger.Events[event.ID]; ok {
				events[event.ID] = existing
				continue
			}
			if canEnrich && enrichmentCtx.Err() == nil {
				revision := forge.EventRevision{Kind: event.Kind, SourceOID: request.SourceOID, BaseOID: request.BaseOID, MergeResultOID: request.MergeResultOID}
				if enriched, err := reader.GetCommitSnapshot(enrichmentCtx, request, revision); err == nil {
					event.Snapshot = notification.BoundSnapshot(enriched)
				}
			}
			events[event.ID] = event
		}
	}
	_, err = r.commit(ctx, func(_ context.Context, l *notification.LedgerV1) (*notification.LedgerV1, error) {
		if l == nil {
			return nil, notification.ErrAbsent
		}
		if l.PolicyRevision.ConfigOID != r.ConfigOID {
			return nil, notification.ErrStaleRevision
		}
		l = l.Clone()
		for _, request := range requests {
			if !request.Repository.Same(r.Repository) || request.Graph != r.Graph {
				continue
			}
			l.KnownRequests[request.Request.ID] = request
			for _, kind := range []v1.NotificationEvent{v1.NotificationRequestCreated, v1.NotificationRequestMerged} {
				if event, eligible := events[notification.EventID(request.Repository, request.Request.ID, kind)]; eligible {
					var err error
					l, err = notification.AdmitEvent(l, r.ConfigOID, event)
					if err != nil {
						return nil, err
					}
				}
			}
		}
		if scan != "" {
			l.Scans[scan] = progress
		}
		if err := notification.CheckCapacity(l, true); err != nil {
			return nil, err
		}
		return l, nil
	})
	return err
}
