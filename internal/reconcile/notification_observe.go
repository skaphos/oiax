package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/notification"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

// NotificationRuntime owns effects independently of the branch engine. All
// callback dependencies are installed before use; one instance is one invocation.
type NotificationRuntime struct {
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

func (r *NotificationRuntime) Activate(ctx context.Context) error {
	if !r.Policy.IsEnabled() {
		return nil
	}
	if r.Store == nil || !notification.ValidOID(r.ConfigOID) {
		return notification.ErrInvalidState
	}
	revision := notification.PolicyRevisionV1{ConfigOID: r.ConfigOID, PolicyDigest: policyDigest(r.Policy)}
	now := r.now()
	_, err := r.Store.Commit(ctx, "", func(ctx context.Context, current *notification.LedgerV1) (*notification.LedgerV1, error) {
		if current == nil {
			current = notification.NewLedger(r.Repository, r.Graph, r.ConfigOID)
		}
		evidence := notification.RevisionEvidence{AcceptedOID: current.PolicyRevision.ConfigOID, IncomingOID: r.ConfigOID}
		if evidence.AcceptedOID != "" && evidence.AcceptedOID != r.ConfigOID && r.VerifyRevision != nil {
			relation, err := r.VerifyRevision(ctx, evidence.AcceptedOID, r.ConfigOID)
			if err != nil {
				return nil, notification.ErrUnorderedRevision
			}
			evidence.Relation = relation
		}
		return notification.AcceptPolicy(current, revision, r.Policy, now, evidence)
	})
	return err
}

func (r *NotificationRuntime) Admit(ctx context.Context, events []notification.EventV1) error {
	if !r.Policy.IsEnabled() || len(events) == 0 {
		return nil
	}
	_, err := r.Store.Commit(ctx, "", func(_ context.Context, l *notification.LedgerV1) (*notification.LedgerV1, error) {
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
	snapshot, err := r.Store.Read(ctx)
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
		if previous.Version != 0 {
			if previous.Complete {
				q.From = previous.Through
			} else {
				q.From = previous.From
				q.Through = previous.Through
				q.Cursor = previous.Cursor
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
	_, err := r.Store.Commit(ctx, "", func(_ context.Context, l *notification.LedgerV1) (*notification.LedgerV1, error) {
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
			if event, eligible := notification.MergeEvent(r.Topology, r.Policy, request, now); eligible {
				var err error
				l, err = notification.AdmitEvent(l, r.ConfigOID, event)
				if err != nil {
					return nil, err
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
