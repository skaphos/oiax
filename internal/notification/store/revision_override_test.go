package store

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/v2/internal/git"
	"github.com/skaphos/oiax/v2/internal/notification"
)

func seededNotes(t *testing.T, l *notification.LedgerV1) *conflictNotes {
	t.Helper()
	data, err := Encode(l)
	if err != nil {
		t.Fatal(err)
	}
	return &conflictNotes{snapshot: git.NoteSnapshot{Tip: strings.Repeat("a", 40), AnchorOID: l.AnchorOID, Data: data}}
}

// Every verification failure must still fail closed. The one that has
// positively established the accepted commit is gone is preserved rather than
// flattened, because it is the only one an operator can act on.
func TestNotificationStorePreservesUnreachableRevision(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		verifyErr error
		want      error
	}{
		{name: "unreachable accepted commit", verifyErr: notification.ErrRevisionUnreachable, want: notification.ErrRevisionUnreachable},
		{name: "operational failure", verifyErr: errors.New("ancestry unavailable"), want: notification.ErrUnorderedRevision},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := codecLedger(t)
			notes := seededNotes(t, l)
			s := New(notes, l.Repository, l.Graph)
			s.VerifyRevision = func(context.Context, string, string) (notification.RevisionRelation, error) {
				return notification.RevisionUnknown, tc.verifyErr
			}
			_, err := s.Commit(context.Background(), notes.snapshot.Tip, func(_ context.Context, latest *notification.LedgerV1) (*notification.LedgerV1, error) {
				latest.PolicyRevision = notification.PolicyRevisionV1{ConfigOID: strings.Repeat("b", 40), PolicyDigest: strings.Repeat("b", 64)}
				return latest, nil
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("commit = %v, want %v", err, tc.want)
			}
			// Both deferrals are still unordered as far as any decision goes.
			if !errors.Is(err, notification.ErrUnorderedRevision) {
				t.Fatal("a failed verification stopped failing closed")
			}
			if notes.writes != 0 {
				t.Fatal("a failed verification wrote to the ledger")
			}
		})
	}
}

// An operator-authorized override is the ledger's only record that ordering was
// once repaired by hand. A transition that could drop or rewrite it would make a
// repaired ledger indistinguishable from an unbroken one.
func TestNotificationStoreRevisionOverridesAreAppendOnly(t *testing.T) {
	t.Parallel()
	recorded := notification.RevisionOverrideV1{Version: 1, PriorOID: strings.Repeat("d", 40), AcceptedOID: strings.Repeat("a", 40), RecordedAt: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)}
	base := codecLedger(t)
	base.RevisionOverrides = []notification.RevisionOverrideV1{recorded}
	for _, tc := range []struct {
		name    string
		mutate  func(*notification.LedgerV1)
		wantErr bool
	}{
		{name: "drop", mutate: func(l *notification.LedgerV1) { l.RevisionOverrides = nil }, wantErr: true},
		{name: "rewrite", mutate: func(l *notification.LedgerV1) {
			l.RevisionOverrides[0].PriorOID = strings.Repeat("e", 40)
		}, wantErr: true},
		{name: "reorder by prepending", mutate: func(l *notification.LedgerV1) {
			l.RevisionOverrides = append([]notification.RevisionOverrideV1{{Version: 1, PriorOID: strings.Repeat("f", 40), AcceptedOID: strings.Repeat("a", 40), RecordedAt: recorded.RecordedAt}}, l.RevisionOverrides...)
		}, wantErr: true},
		{name: "append", mutate: func(l *notification.LedgerV1) {
			l.RevisionOverrides = append(l.RevisionOverrides, notification.RevisionOverrideV1{Version: 1, PriorOID: strings.Repeat("f", 40), AcceptedOID: strings.Repeat("a", 40), RecordedAt: recorded.RecordedAt})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			notes := seededNotes(t, base)
			s := New(notes, base.Repository, base.Graph)
			_, err := s.Commit(context.Background(), notes.snapshot.Tip, func(_ context.Context, latest *notification.LedgerV1) (*notification.LedgerV1, error) {
				tc.mutate(latest)
				return latest, nil
			})
			if tc.wantErr {
				if !errors.Is(err, notification.ErrInvalidState) {
					t.Fatalf("commit = %v, want ErrInvalidState", err)
				}
				if notes.writes != 0 {
					t.Fatal("the audit record was rewritten in the durable ledger")
				}
				return
			}
			if err != nil {
				t.Fatalf("appending a record = %v", err)
			}
		})
	}
}

// The audit survives the note format: a record that does not name two distinct
// real commits is not an audit entry anybody could act on.
func TestNotificationLedgerCodecValidatesRevisionOverrides(t *testing.T) {
	t.Parallel()
	valid := notification.RevisionOverrideV1{Version: 1, PriorOID: strings.Repeat("d", 40), AcceptedOID: strings.Repeat("e", 40), RecordedAt: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)}
	l := codecLedger(t)
	l.RevisionOverrides = []notification.RevisionOverrideV1{valid}
	data, err := Encode(l)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.RevisionOverrides) != 1 || out.RevisionOverrides[0] != valid {
		t.Fatalf("round trip = %+v", out.RevisionOverrides)
	}
	// An untouched ledger must stay byte-identical to one written before the
	// field existed, so an ordinary ledger is still readable by older binaries.
	plain := codecLedger(t)
	raw, err := Encode(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("revisionOverrides")) {
		t.Fatal("an unrepaired ledger carries the override field")
	}
	for _, tc := range []struct {
		name   string
		record notification.RevisionOverrideV1
	}{
		{name: "unknown version", record: notification.RevisionOverrideV1{Version: 2, PriorOID: valid.PriorOID, AcceptedOID: valid.AcceptedOID, RecordedAt: valid.RecordedAt}},
		{name: "malformed prior", record: notification.RevisionOverrideV1{Version: 1, PriorOID: "HEAD~1", AcceptedOID: valid.AcceptedOID, RecordedAt: valid.RecordedAt}},
		{name: "same commit twice", record: notification.RevisionOverrideV1{Version: 1, PriorOID: valid.PriorOID, AcceptedOID: valid.PriorOID, RecordedAt: valid.RecordedAt}},
		{name: "no timestamp", record: notification.RevisionOverrideV1{Version: 1, PriorOID: valid.PriorOID, AcceptedOID: valid.AcceptedOID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bad := codecLedger(t)
			bad.RevisionOverrides = []notification.RevisionOverrideV1{tc.record}
			if _, err := Encode(bad); !errors.Is(err, notification.ErrInvalidState) {
				t.Fatalf("encode = %v, want ErrInvalidState", err)
			}
		})
	}
	over := codecLedger(t)
	for range notification.MaxRevisionOverrides + 1 {
		over.RevisionOverrides = append(over.RevisionOverrides, valid)
	}
	if _, err := Encode(over); !errors.Is(err, notification.ErrCapacity) {
		t.Fatalf("unbounded audit = %v, want ErrCapacity", err)
	}
}
