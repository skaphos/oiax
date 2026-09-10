package store

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"slices"

	"github.com/skaphos/oiax/v2/internal/git"
	"github.com/skaphos/oiax/v2/internal/notification"
)

// Notes is the narrow effect boundary; the production implementation cannot
// write branches, accept arbitrary refs or replace history with caller commits.
type Notes interface {
	Read(context.Context) (git.NoteSnapshot, error)
	Write(context.Context, string, string, []byte) (string, error)
}

type Store struct {
	notes      Notes
	repository notification.RepositoryIdentity
	graph      string
	// VerifyRevision must prove ancestry for every policy advancement against
	// the snapshot read in this attempt. Nil fails closed for changed OIDs.
	VerifyRevision func(context.Context, string, string) (notification.RevisionRelation, error)
}

func New(notes Notes, repository notification.RepositoryIdentity, graph string) *Store {
	return &Store{notes: notes, repository: repository, graph: graph}
}

func (s *Store) Read(ctx context.Context) (notification.Snapshot, error) {
	note, err := s.notes.Read(ctx)
	if err != nil {
		return notification.Snapshot{}, mapError(err)
	}
	l, err := Decode(bytes.NewReader(note.Data))
	if err != nil {
		return notification.Snapshot{}, err
	}
	if !l.Repository.Same(s.repository) || l.Graph != s.graph || l.AnchorOID != note.AnchorOID || !notification.ValidOID(note.Tip) {
		return notification.Snapshot{}, notification.ErrInvalidState
	}
	return notification.Snapshot{Ledger: l, Revision: note.Tip}, nil
}

// Commit recomputes the transition after each fresh read, at most five times.
// The expected argument is the caller's observed tip, not an implicit tracking
// ref. A mismatch never replays a replacement: the callback sees current state.
func (s *Store) Commit(ctx context.Context, expected string, transition notification.Transition) (notification.Snapshot, error) {
	if transition == nil || expected != "" && !notification.ValidOID(expected) {
		return notification.Snapshot{}, notification.ErrInvalidState
	}
	for range 5 {
		if err := ctx.Err(); err != nil {
			return notification.Snapshot{}, err
		}
		current, err := s.Read(ctx)
		if err != nil && !errors.Is(err, notification.ErrAbsent) {
			return notification.Snapshot{}, err
		}
		// Always reduce current state, including when another worker advanced
		// beyond the caller's expected tip before the first read.
		writeExpected := current.Revision
		var input *notification.LedgerV1
		if current.Ledger != nil {
			input = current.Ledger.Clone()
		}
		next, err := transition(ctx, input)
		if err != nil {
			return notification.Snapshot{}, err
		}
		if next == nil || !next.Repository.Same(s.repository) || next.Graph != s.graph {
			return notification.Snapshot{}, notification.ErrInvalidState
		}
		// Revision overrides can only repair an existing ledger whose accepted
		// commit became unreachable. Initial creation has no prior revision to
		// override, so it must begin with an empty audit.
		if current.Ledger == nil && len(next.RevisionOverrides) != 0 {
			return notification.Snapshot{}, notification.ErrInvalidState
		}
		if current.Ledger != nil {
			accepted, incoming := current.Ledger.PolicyRevision, next.PolicyRevision
			evidence := notification.RevisionEvidence{AcceptedOID: accepted.ConfigOID, IncomingOID: incoming.ConfigOID}
			if accepted.ConfigOID != incoming.ConfigOID && s.VerifyRevision != nil {
				relation, verifyErr := s.VerifyRevision(ctx, accepted.ConfigOID, incoming.ConfigOID)
				if verifyErr != nil {
					// Every verification failure fails closed. A verifier that
					// has positively established the accepted commit is gone
					// says so, and that answer is preserved: it wraps
					// ErrUnorderedRevision, so this still defers, but the
					// diagnostic can name the one case with a recovery.
					if errors.Is(verifyErr, notification.ErrRevisionUnreachable) {
						return notification.Snapshot{}, notification.ErrRevisionUnreachable
					}
					return notification.Snapshot{}, notification.ErrUnorderedRevision
				}
				evidence.Relation = relation
			}
			if err := notification.CheckRevision(accepted, incoming, evidence); err != nil {
				return notification.Snapshot{}, err
			}
			if err := validateAppend(current.Ledger, next, evidence); err != nil {
				return notification.Snapshot{}, err
			}
			if reflect.DeepEqual(current.Ledger, next) {
				return current, nil
			}
		}
		data, err := Encode(next)
		if err != nil {
			return notification.Snapshot{}, err
		}
		tip, err := s.notes.Write(ctx, writeExpected, next.AnchorOID, data)
		if errors.Is(err, git.ErrNotesConflict) {
			continue
		}
		if err != nil {
			return notification.Snapshot{}, mapError(err)
		}
		return notification.Snapshot{Ledger: next.Clone(), Revision: tip}, nil
	}
	return notification.Snapshot{}, notification.ErrConflict
}

// Guard immutable facts and terminal receipts even if a caller implements a bad
// transition. Revisions themselves require freshly verified reducer evidence.
func validateAppend(old, next *notification.LedgerV1, evidence notification.RevisionEvidence) error {
	if old.AnchorOID != next.AnchorOID || !old.Repository.Same(next.Repository) || old.Graph != next.Graph {
		return notification.ErrInvalidState
	}
	for id, event := range old.Events {
		if !reflect.DeepEqual(next.Events[id], event) {
			return notification.ErrInvalidState
		}
	}
	// The revision-override audit is append-only prefix-wise: a transition may
	// add a record but can never drop, reorder or rewrite one. Erasing the
	// evidence that ordering was once repaired by hand would make a repaired
	// ledger indistinguishable from an unbroken one.
	if len(next.RevisionOverrides) < len(old.RevisionOverrides) {
		return notification.ErrInvalidState
	}
	for i, record := range old.RevisionOverrides {
		if next.RevisionOverrides[i] != record {
			return notification.ErrInvalidState
		}
	}
	addedOverrides := next.RevisionOverrides[len(old.RevisionOverrides):]
	revisionChanged := old.PolicyRevision.ConfigOID != next.PolicyRevision.ConfigOID
	if evidence.Relation == notification.RevisionOverride {
		if !revisionChanged || len(addedOverrides) != 1 || addedOverrides[0].PriorOID != old.PolicyRevision.ConfigOID || addedOverrides[0].AcceptedOID != next.PolicyRevision.ConfigOID {
			return notification.ErrInvalidState
		}
	} else if len(addedOverrides) != 0 {
		// An audit record is evidence of an operator-authorized ordering gap. It
		// cannot be invented by a no-op or an ordinary descendant transition.
		return notification.ErrInvalidState
	}
	for key, before := range old.Deliveries {
		after, ok := next.Deliveries[key]
		skippedPreserved := before.Status != notification.StatusSkipped || reflect.DeepEqual(before, after) || (after.Status == notification.StatusDelivered && after.Attempts == before.Attempts && slices.Equal(before.AttemptIDs, after.AttemptIDs))
		if !ok || (before.Status == notification.StatusDelivered && !reflect.DeepEqual(before, after)) || !skippedPreserved || (before.Message != nil && !reflect.DeepEqual(before.Message, after.Message)) || before.EventID != after.EventID || before.Destination != after.Destination || before.Generation != after.Generation || after.Attempts < before.Attempts || len(after.AttemptIDs) < len(before.AttemptIDs) || !slices.Equal(before.AttemptIDs, after.AttemptIDs[:len(before.AttemptIDs)]) {
			return notification.ErrInvalidState
		}
	}
	return nil
}

func mapError(err error) error {
	switch {
	case errors.Is(err, git.ErrNotesAbsent):
		return notification.ErrAbsent
	case errors.Is(err, git.ErrNotesConflict):
		return notification.ErrConflict
	case errors.Is(err, git.ErrNotesInvalid):
		return notification.ErrInvalidState
	case errors.Is(err, git.ErrNotesCapacity):
		return notification.ErrCapacity
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	default:
		return notification.ErrUnavailable
	}
}

var _ notification.LedgerStore = (*Store)(nil)
