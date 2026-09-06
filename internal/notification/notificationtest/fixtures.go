// Package notificationtest supplies explicit, race-safe effect doubles. It is
// not imported by production notification code.
package notificationtest

import (
	"context"
	"sync"
	"time"

	"github.com/skaphos/oiax/v2/internal/forge"
	"github.com/skaphos/oiax/v2/internal/notification"
)

type Clock struct {
	mu      sync.Mutex
	instant time.Time
}

func NewClock(now time.Time) *Clock { return &Clock{instant: now.UTC()} }
func (c *Clock) Now() time.Time     { c.mu.Lock(); defer c.mu.Unlock(); return c.instant }
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.instant = c.instant.Add(d)
}

// MemoryStore models an atomic compare-and-swap without executing callbacks under
// a lock. It exposes conflicts so coordinator tests can exercise fresh reduction.
type MemoryStore struct {
	mu                    sync.Mutex
	ledger                *notification.LedgerV1
	revision              string
	version               int
	ReadError, WriteError error
}

func (s *MemoryStore) Read(ctx context.Context) (notification.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return notification.Snapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ReadError != nil {
		return notification.Snapshot{}, s.ReadError
	}
	if s.ledger == nil {
		return notification.Snapshot{}, notification.ErrAbsent
	}
	return notification.Snapshot{Ledger: s.ledger.Clone(), Revision: s.revision}, nil
}
func (s *MemoryStore) Commit(ctx context.Context, expected string, transition notification.Transition) (notification.Snapshot, error) {
	current, err := s.Read(ctx)
	if err != nil && err != notification.ErrAbsent {
		return notification.Snapshot{}, err
	}
	if current.Revision != expected {
		return notification.Snapshot{}, notification.ErrConflict
	}
	next, err := transition(ctx, current.Ledger)
	if err != nil {
		return notification.Snapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.WriteError != nil {
		return notification.Snapshot{}, s.WriteError
	}
	if s.revision != expected {
		return notification.Snapshot{}, notification.ErrConflict
	}
	s.version++
	s.revision = notification.Digest(expected, next.PolicyRevision.ConfigOID)
	s.ledger = next.Clone()
	return notification.Snapshot{Ledger: s.ledger.Clone(), Revision: s.revision}, nil
}

// Recorder intentionally discards endpoints: even test diagnostics cannot leak a
// callback URL. Set Result before concurrent use, then inspect copied payloads.
type Recorder struct {
	mu       sync.Mutex
	Result   notification.AttemptResult
	payloads []notification.DeliveryPayloadV1
}

func (r *Recorder) Send(ctx context.Context, _ string, p notification.DeliveryPayloadV1) notification.AttemptResult {
	if ctx.Err() != nil {
		return notification.AttemptResult{Code: notification.OutcomeCanceled}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p.Event.Snapshot.Commits = append([]notification.CommitSummary(nil), p.Event.Snapshot.Commits...)
	r.payloads = append(r.payloads, p)
	return r.Result
}
func (r *Recorder) Payloads() []notification.DeliveryPayloadV1 {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := append([]notification.DeliveryPayloadV1(nil), r.payloads...)
	for i := range result {
		result[i].Event.Snapshot.Commits = append([]notification.CommitSummary(nil), result[i].Event.Snapshot.Commits...)
	}
	return result
}

type Lifecycle struct {
	Identity  notification.RepositoryIdentity
	Pages     map[string]forge.LifecyclePage
	Requests  map[forge.RequestID]notification.LifecycleRequest
	Snapshots map[string]notification.CommitSnapshot
	Error     error
}

func (l *Lifecycle) RepositoryIdentity(context.Context) (notification.RepositoryIdentity, error) {
	return l.Identity, l.Error
}
func (l *Lifecycle) ListLifecyclePage(_ context.Context, q forge.LifecycleQuery) (forge.LifecyclePage, error) {
	return l.Pages[q.Cursor], l.Error
}
func (l *Lifecycle) GetLifecycleRequest(_ context.Context, id forge.RequestID) (notification.LifecycleRequest, error) {
	return l.Requests[id], l.Error
}
func (l *Lifecycle) GetCommitSnapshot(_ context.Context, req notification.LifecycleRequest, rev forge.EventRevision) (notification.CommitSnapshot, error) {
	return l.Snapshots[req.Request.ID+"/"+string(rev.Kind)], l.Error
}

var _ notification.LedgerStore = (*MemoryStore)(nil)
var _ notification.Sender = (*Recorder)(nil)
var _ forge.LifecycleReader = (*Lifecycle)(nil)
var _ forge.SnapshotReader = (*Lifecycle)(nil)
