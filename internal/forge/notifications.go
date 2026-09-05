package forge

import (
	"context"
	"time"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/git"
	"github.com/skaphos/oiax/internal/notification"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

// LifecycleQuery is independent of the branch engine's baseline lookback.
// Cursor is provider-owned bounded data; a frozen interval never advances until
// all pages and required request details are durably admitted.
type LifecycleQuery struct {
	Graph   string
	Cursor  string
	From    time.Time
	Through time.Time
	Limit   int
	Kind    v1.NotificationEvent
}

type LifecyclePage struct {
	Requests []notification.LifecycleRequest
	Progress notification.ScanProgress
	// Pages counts actual list requests, including overlap/verification reads.
	Pages int
}

// LifecycleReader is optional. Implementations construct lifecycle values only
// for positively owned requests; missing/denied detail is not a merge signal.
type LifecycleReader interface {
	RepositoryIdentity(context.Context) (notification.RepositoryIdentity, error)
	ListLifecyclePage(context.Context, LifecycleQuery) (LifecyclePage, error)
	GetLifecycleRequest(context.Context, RequestID) (notification.LifecycleRequest, error)
}

// NotificationNotesProvider reuses forge-owned Git authentication without
// exposing tokens to the pure notification model or putting them in URLs.
type NotificationNotesProvider interface {
	OpenNotificationNotes(context.Context, string) (*git.NotificationNotes, error)
}

type EventRevision struct {
	Kind           v1.NotificationEvent
	SourceOID      string
	BaseOID        string
	MergeResultOID string
}

type SnapshotReader interface {
	GetCommitSnapshot(context.Context, notification.LifecycleRequest, EventRevision) (notification.CommitSnapshot, error)
}

type CreateDisposition string

const (
	RequestCreated CreateDisposition = "created"
	RequestAdopted CreateDisposition = "adopted"
)

// CreateOutcome retains an actual successful POST even if follow-up metadata
// writes fail. Adoption never invents origin or reports a new creation event.
type CreateOutcome struct {
	Request     engine.ChangeRequest
	Disposition CreateDisposition
	Origin      *notification.NotificationOriginV1
}
