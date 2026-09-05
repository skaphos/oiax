package notification

import (
	"context"
	"errors"
)

var (
	ErrAbsent               = errors.New("notification-ledger-absent")
	ErrConflict             = errors.New("notification-ledger-conflict")
	ErrUnavailable          = errors.New("notification-ledger-unavailable")
	ErrLifecycleUnavailable = errors.New("notification-lifecycle-unavailable")
	ErrNotManaged           = errors.New("notification-request-not-managed")
	ErrRequestMissing       = errors.New("notification-request-missing")
	ErrDiscoveryIncomplete  = errors.New("notification-discovery-incomplete")
	ErrReceiptUncertain     = errors.New("notification-accepted-receipt-uncertain")
)

type Snapshot struct {
	Ledger   *LedgerV1
	Revision string
}

// Transition is re-evaluated on a fresh snapshot after every conflict. Policy
// callbacks must refresh their ancestry evidence, not capture a replacement.
type Transition func(context.Context, *LedgerV1) (*LedgerV1, error)

type LedgerStore interface {
	Read(context.Context) (Snapshot, error)
	// Commit takes the caller's observed revision as a hint, not a precondition:
	// it re-reduces fresh state and fences each write against that exact read tip.
	// Transitions must enforce their own policy/claim preconditions on that state.
	Commit(context.Context, string, Transition) (Snapshot, error)
}

// Sender receives persisted presentation, not policy, templates or a store.
// The separately supplied endpoint is ephemeral and must never be recorded.
type Sender interface {
	Send(context.Context, string, DeliveryPayloadV1) AttemptResult
}
