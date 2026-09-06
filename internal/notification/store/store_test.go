package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/skaphos/oiax/v2/internal/git"
	"github.com/skaphos/oiax/v2/internal/notification"
)

type conflictNotes struct {
	snapshot          git.NoteSnapshot
	conflicts, writes int
	readError         error
	advance           func()
}

func (n *conflictNotes) Read(context.Context) (git.NoteSnapshot, error) {
	if n.readError != nil {
		return git.NoteSnapshot{}, n.readError
	}
	if n.snapshot.Tip == "" {
		return git.NoteSnapshot{}, git.ErrNotesAbsent
	}
	return n.snapshot, nil
}
func (n *conflictNotes) Write(_ context.Context, expected, anchor string, data []byte) (string, error) {
	n.writes++
	if n.conflicts > 0 {
		n.conflicts--
		if n.advance != nil {
			n.advance()
		}
		return "", git.ErrNotesConflict
	}
	if expected != n.snapshot.Tip {
		return "", git.ErrNotesConflict
	}
	n.snapshot = git.NoteSnapshot{Tip: strings.Repeat("b", 40), AnchorOID: anchor, Data: append([]byte{}, data...)}
	return n.snapshot.Tip, nil
}

func TestNotificationStoreConflictsAndIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	l := codecLedger(t)
	notes := &conflictNotes{conflicts: 4}
	s := New(notes, l.Repository, l.Graph)
	s.VerifyRevision = func(context.Context, string, string) (notification.RevisionRelation, error) {
		return notification.RevisionDescendant, nil
	}
	if _, err := s.Read(ctx); !errors.Is(err, notification.ErrAbsent) {
		t.Fatal(err)
	}
	called := 0
	got, err := s.Commit(ctx, "", func(context.Context, *notification.LedgerV1) (*notification.LedgerV1, error) {
		called++
		return l.Clone(), nil
	})
	if err != nil || called != 5 || notes.writes != 5 || got.Revision == "" {
		t.Fatal("conflict retry", err, called, notes.writes)
	}
	wrong := New(notes, l.Repository, "another-graph")
	if _, err := wrong.Read(ctx); !errors.Is(err, notification.ErrInvalidState) {
		t.Fatal("wrong graph accepted", err)
	}
	notes.conflicts = 5
	called = 0
	_, err = s.Commit(ctx, got.Revision, func(_ context.Context, latest *notification.LedgerV1) (*notification.LedgerV1, error) {
		called++
		latest.PolicyRevision = notification.PolicyRevisionV1{ConfigOID: strings.Repeat("c", 40), PolicyDigest: strings.Repeat("c", 64)}
		return latest, nil
	})
	if !errors.Is(err, notification.ErrConflict) || called != 5 {
		t.Fatal("retry bound", err, called)
	}
}

func TestNotificationStoreRefreshesRevisionAfterConflict(t *testing.T) {
	t.Parallel()
	l := codecLedger(t)
	data, err := Encode(l)
	if err != nil {
		t.Fatal(err)
	}
	notes := &conflictNotes{snapshot: git.NoteSnapshot{Tip: strings.Repeat("a", 40), AnchorOID: l.AnchorOID, Data: data}, conflicts: 1}
	s := New(notes, l.Repository, l.Graph)
	s.VerifyRevision = func(context.Context, string, string) (notification.RevisionRelation, error) {
		return notification.RevisionDescendant, nil
	}
	notes.advance = func() {
		newer := l.Clone()
		newer.PolicyRevision = notification.PolicyRevisionV1{ConfigOID: strings.Repeat("c", 40), PolicyDigest: strings.Repeat("c", 64)}
		raw, encodeErr := Encode(newer)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		notes.snapshot.Data = raw
		notes.snapshot.Tip = strings.Repeat("c", 40)
	}
	var revisions []string
	_, err = s.Commit(context.Background(), notes.snapshot.Tip, func(_ context.Context, latest *notification.LedgerV1) (*notification.LedgerV1, error) {
		revisions = append(revisions, latest.PolicyRevision.ConfigOID)
		if len(revisions) == 2 {
			return nil, notification.ErrStaleRevision
		}
		latest.PolicyRevision = notification.PolicyRevisionV1{ConfigOID: strings.Repeat("b", 40), PolicyDigest: strings.Repeat("b", 64)}
		return latest, nil
	})
	if !errors.Is(err, notification.ErrStaleRevision) || len(revisions) != 2 || revisions[1] != strings.Repeat("c", 40) || notes.writes != 1 {
		t.Fatal("stale transition replayed", err, revisions, notes.writes)
	}
}

func TestNotificationStoreReducesFreshStateWhenCallerTipIsStale(t *testing.T) {
	t.Parallel()
	l := codecLedger(t)
	data, err := Encode(l)
	if err != nil {
		t.Fatal(err)
	}
	notes := &conflictNotes{snapshot: git.NoteSnapshot{Tip: strings.Repeat("c", 40), AnchorOID: l.AnchorOID, Data: data}}
	s := New(notes, l.Repository, l.Graph)
	got, err := s.Commit(context.Background(), strings.Repeat("a", 40), func(_ context.Context, latest *notification.LedgerV1) (*notification.LedgerV1, error) {
		if latest == nil || latest.PolicyRevision != l.PolicyRevision {
			t.Fatal("transition did not receive current durable state")
		}
		latest.Scans["created"] = notification.ScanProgress{Version: 1, Complete: true}
		return latest, nil
	})
	// The fake Notes writer rejects anything but its exact current tip. Success
	// proves fresh reduction uses that tip, never the stale caller's replacement.
	if err != nil || notes.writes != 1 || !got.Ledger.Scans["created"].Complete {
		t.Fatalf("fresh transition failed: %+v, %v, writes=%d", got, err, notes.writes)
	}
	s.VerifyRevision = func(context.Context, string, string) (notification.RevisionRelation, error) {
		return notification.RevisionAncestor, nil
	}
	_, err = s.Commit(context.Background(), strings.Repeat("a", 40), func(_ context.Context, latest *notification.LedgerV1) (*notification.LedgerV1, error) {
		latest.PolicyRevision.ConfigOID = strings.Repeat("0", 40)
		return latest, nil
	})
	if !errors.Is(err, notification.ErrStaleRevision) || notes.writes != 1 {
		t.Fatalf("stale policy wrote through fresh reduction: %v, writes=%d", err, notes.writes)
	}
}

func TestNotificationStoreDenialAndCorruption(t *testing.T) {
	t.Parallel()
	l := codecLedger(t)
	notes := &conflictNotes{readError: errors.New("https://secret.invalid/token")}
	s := New(notes, l.Repository, l.Graph)
	if _, err := s.Read(context.Background()); !errors.Is(err, notification.ErrUnavailable) {
		t.Fatal("raw error leaked", err)
	}
	notes.readError = nil
	notes.snapshot = git.NoteSnapshot{Tip: strings.Repeat("a", 40), AnchorOID: l.AnchorOID, Data: []byte(`{"version":99}`)}
	if _, err := s.Read(context.Background()); !errors.Is(err, notification.ErrInvalidState) {
		t.Fatal(err)
	}
}
