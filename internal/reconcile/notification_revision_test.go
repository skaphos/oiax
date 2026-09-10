package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/v2/internal/git"
	"github.com/skaphos/oiax/v2/internal/gittest"
	"github.com/skaphos/oiax/v2/internal/notification"
	"github.com/skaphos/oiax/v2/internal/notification/notificationtest"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

// strandedRevisionHarness builds a repository holding a reachable configuration
// commit and the object id of one that was rewritten out of existence — the
// state a force-push or a GC of the configuration branch leaves behind. The
// stranded commit is created with commit-tree, so no ref or reflog entry ever
// names it and `gc --prune=now` really removes it.
func strandedRevisionHarness(t *testing.T) (runner *git.Runner, present, newer, stranded string) {
	t.Helper()
	dir := t.TempDir()
	gittest.InitRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte("v0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", ".")
	gittest.Run(t, dir, "commit", "-q", "-m", "configuration")
	present = gittest.Run(t, dir, "rev-parse", "HEAD")
	tree := gittest.Run(t, dir, "rev-parse", "HEAD^{tree}")
	stranded = gittest.Run(t, dir, "commit-tree", tree, "-p", present, "-m", "rewritten away")
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", ".")
	gittest.Run(t, dir, "commit", "-q", "-m", "reviewed descendant")
	newer = gittest.Run(t, dir, "rev-parse", "HEAD")
	gittest.Run(t, dir, "-c", "gc.reflogExpire=now", "-c", "gc.reflogExpireUnreachable=now", "gc", "--prune=now", "-q")
	runner = &git.Runner{Dir: dir}
	exists, err := runner.CommitExists(context.Background(), stranded)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatalf("harness did not strand %s; the unreachable path would not be exercised", stranded)
	}
	return runner, present, newer, stranded
}

// The bug: an accepted revision whose commit is gone makes merge-base fail, and
// every run deferred forever behind a generic "unordered" error whose documented
// fix (commit a descendant) cannot be performed. The relation must still fail
// closed, but say which failure it is.
func TestNotificationRevisionRelationNamesUnreachableAcceptedCommit(t *testing.T) {
	t.Parallel()
	runner, present, newer, stranded := strandedRevisionHarness(t)
	c := &Coordinator{Git: runner}

	relation, err := c.notificationRevisionRelation(context.Background(), stranded, present)
	if !errors.Is(err, notification.ErrRevisionUnreachable) {
		t.Fatalf("relation for a stranded accepted commit = %v, want ErrRevisionUnreachable", err)
	}
	// Callers decide on ErrUnorderedRevision and must keep deferring: the
	// narrower error only changes what the operator is told.
	if !errors.Is(err, notification.ErrUnorderedRevision) {
		t.Fatal("ErrRevisionUnreachable no longer wraps ErrUnorderedRevision; existing callers would stop failing closed")
	}
	if relation != notification.RevisionUnknown {
		t.Fatalf("relation = %q, want unknown", relation)
	}
	if got := NotificationProblem(err); got.Reason != "config-revision-unreachable" || !strings.Contains(got.Action, "notifications reset") {
		t.Fatalf("diagnostic = %+v, want the reset recovery", got)
	}

	// Resolvable pairs are unchanged.
	if relation, err := c.notificationRevisionRelation(context.Background(), present, newer); err != nil || relation != notification.RevisionDescendant {
		t.Fatalf("resolvable ancestry = %q, %v; want descendant", relation, err)
	}
	if relation, err := c.notificationRevisionRelation(context.Background(), newer, present); err != nil || relation != notification.RevisionAncestor {
		t.Fatalf("resolvable reverse ancestry = %q, %v; want ancestor", relation, err)
	}
}

// A missing commit is the only failure that never heals. An operational failure
// says nothing about reachability, and reporting it as a stranded commit would
// point an operator at a recovery that rewrites their accepted revision.
func TestNotificationRevisionRelationKeepsOperationalFailures(t *testing.T) {
	t.Parallel()
	runner, present, newer, _ := strandedRevisionHarness(t)
	c := &Coordinator{Git: runner}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.notificationRevisionRelation(ctx, present, newer)
	if err == nil {
		t.Fatal("a cancelled context must not resolve ancestry")
	}
	if errors.Is(err, notification.ErrRevisionUnreachable) {
		t.Fatal("a cancelled context was reported as a missing commit")
	}
}

// The override verifier is the only producer of RevisionOverride evidence, so
// its refusals are what keep the reset from becoming a general way around the
// ordering guarantee.
func TestNotificationRevisionOverrideRequiresAnAbsentAcceptedCommit(t *testing.T) {
	t.Parallel()
	runner, present, _, stranded := strandedRevisionHarness(t)
	c := &Coordinator{Git: runner}
	for _, tc := range []struct {
		name               string
		accepted, incoming string
		want               notification.RevisionRelation
		wantErr            error
	}{
		{name: "stranded accepted commit", accepted: stranded, incoming: present, want: notification.RevisionOverride},
		{name: "resolvable accepted commit", accepted: present, incoming: present, want: notification.RevisionUnknown, wantErr: notification.ErrRevisionReachable},
		{name: "incoming also stranded", accepted: stranded, incoming: strings.Repeat("b", 40), want: notification.RevisionUnknown, wantErr: notification.ErrRevisionUnreachable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			relation, err := c.notificationRevisionOverride(context.Background(), tc.accepted, tc.incoming)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if relation != tc.want {
				t.Fatalf("relation = %q, want %q", relation, tc.want)
			}
		})
	}
}

func revisionResetRuntime(t *testing.T, store notification.LedgerStore, policy *v1.NotificationPolicy, at time.Time, oid string) (*NotificationRuntime, *[]NotificationDiagnostic) {
	t.Helper()
	runtime := mergeRuntime(func() time.Time { return at }, store, policy)
	runtime.ConfigOID = oid
	var reported []NotificationDiagnostic
	runtime.Report = func(d NotificationDiagnostic) { reported = append(reported, d) }
	return runtime, &reported
}

// The acceptance criterion: a ledger whose accepted OID is unreachable has a
// non-destructive recovery. Receipts are what make it non-destructive.
func TestNotificationResetRevisionRecoversAndPreservesReceipts(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "ENDPOINT"}}}
	strandedOID, pinnedOID := strings.Repeat("a", 40), strings.Repeat("b", 40)
	store := &notificationtest.MemoryStore{}

	seeded, _ := revisionResetRuntime(t, store, policy, now, strandedOID)
	if err := seeded.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	event := mergeEvent(seeded.Repository, "42", now.Add(time.Minute))
	if err := seeded.Admit(context.Background(), []notification.EventV1{event}); err != nil {
		t.Fatal(err)
	}
	before, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Every ordinary run now defers: nothing can be proved a descendant of a
	// commit the repository does not have.
	stuck, _ := revisionResetRuntime(t, store, policy, now.Add(2*time.Minute), pinnedOID)
	stuck.VerifyRevision = func(context.Context, string, string) (notification.RevisionRelation, error) {
		return notification.RevisionUnknown, notification.ErrRevisionUnreachable
	}
	if err := stuck.Activate(context.Background()); !errors.Is(err, notification.ErrRevisionUnreachable) {
		t.Fatalf("stuck activation = %v, want ErrRevisionUnreachable", err)
	}

	reset, reported := revisionResetRuntime(t, store, policy, now.Add(3*time.Minute), pinnedOID)
	reset.VerifyRevision = func(_ context.Context, accepted, incoming string) (notification.RevisionRelation, error) {
		if accepted != strandedOID || incoming != pinnedOID {
			t.Fatalf("verified the wrong pair: %s -> %s", accepted, incoming)
		}
		return notification.RevisionOverride, nil
	}
	record, err := reset.ResetRevision(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.PriorOID != strandedOID || record.AcceptedOID != pinnedOID || record.Version != 1 {
		t.Fatalf("override record = %+v", record)
	}
	if len(*reported) != 1 || (*reported)[0].Reason != "notification-revision-reset" {
		t.Fatalf("diagnostics = %+v, want one revision-reset report", *reported)
	}

	after, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after.Ledger.PolicyRevision.ConfigOID != pinnedOID {
		t.Fatalf("accepted revision = %s, want %s", after.Ledger.PolicyRevision.ConfigOID, pinnedOID)
	}
	if len(after.Ledger.RevisionOverrides) != 1 || after.Ledger.RevisionOverrides[0] != *record {
		t.Fatalf("ledger audit = %+v", after.Ledger.RevisionOverrides)
	}
	// Non-destructive: the reset repairs ordering, never delivery evidence.
	if len(after.Ledger.Events) != len(before.Ledger.Events) || len(after.Ledger.Deliveries) != len(before.Ledger.Deliveries) {
		t.Fatal("reset discarded events or delivery receipts")
	}
	for id, e := range before.Ledger.Events {
		if after.Ledger.Events[id].ID != e.ID {
			t.Fatalf("event %s was not carried across", id)
		}
	}

	// The ordinary path works again, and the audit record survives it.
	healthy, _ := revisionResetRuntime(t, store, policy, now.Add(4*time.Minute), pinnedOID)
	if err := healthy.Activate(context.Background()); err != nil {
		t.Fatalf("activation after reset = %v", err)
	}
	final, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(final.Ledger.RevisionOverrides) != 1 {
		t.Fatalf("audit record lost by a later run: %+v", final.Ledger.RevisionOverrides)
	}

	// Re-running the recovery is a no-op, not a second audit record.
	repeat, repeatReported := revisionResetRuntime(t, store, policy, now.Add(5*time.Minute), pinnedOID)
	repeat.VerifyRevision = reset.VerifyRevision
	again, err := repeat.ResetRevision(context.Background())
	if err != nil || again != nil {
		t.Fatalf("repeat reset = %+v, %v; want no-op", again, err)
	}
	if len(*repeatReported) != 0 {
		t.Fatalf("repeat reset reported %+v", *repeatReported)
	}
}

func TestNotificationResetRevisionRefusesWithoutOverrideEvidence(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "ENDPOINT"}}}
	strandedOID, pinnedOID := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, tc := range []struct {
		name   string
		verify func(context.Context, string, string) (notification.RevisionRelation, error)
		want   error
	}{
		{name: "accepted commit is still resolvable", verify: func(context.Context, string, string) (notification.RevisionRelation, error) {
			return notification.RevisionUnknown, notification.ErrRevisionReachable
		}, want: notification.ErrRevisionReachable},
		// A verifier that answered with a real relation has not authorized an
		// override; the reducer must refuse rather than accept the advance.
		{name: "divergent relation is not an authorization", verify: func(context.Context, string, string) (notification.RevisionRelation, error) {
			return notification.RevisionDivergent, nil
		}, want: notification.ErrInvalidState},
		{name: "descendant relation is not an authorization", verify: func(context.Context, string, string) (notification.RevisionRelation, error) {
			return notification.RevisionDescendant, nil
		}, want: notification.ErrInvalidState},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &notificationtest.MemoryStore{}
			seeded, _ := revisionResetRuntime(t, store, policy, now, strandedOID)
			if err := seeded.Activate(context.Background()); err != nil {
				t.Fatal(err)
			}
			before, err := store.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			reset, _ := revisionResetRuntime(t, store, policy, now.Add(time.Minute), pinnedOID)
			reset.VerifyRevision = tc.verify
			record, err := reset.ResetRevision(context.Background())
			if !errors.Is(err, tc.want) {
				t.Fatalf("reset = %v, want %v", err, tc.want)
			}
			if record != nil {
				t.Fatalf("refused reset returned a record: %+v", record)
			}
			after, err := store.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if after.Ledger.PolicyRevision != before.Ledger.PolicyRevision || len(after.Ledger.RevisionOverrides) != 0 {
				t.Fatal("refused reset mutated durable state")
			}
		})
	}
}

// Nothing exists to repair before the first activation, and inventing a ledger
// here would establish a cutoff an operator did not ask for.
func TestNotificationResetRevisionRequiresALedger(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	policy := &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "ENDPOINT"}}}
	store := &notificationtest.MemoryStore{}
	reset, _ := revisionResetRuntime(t, store, policy, now, strings.Repeat("b", 40))
	reset.VerifyRevision = func(context.Context, string, string) (notification.RevisionRelation, error) {
		return notification.RevisionOverride, nil
	}
	if _, err := reset.ResetRevision(context.Background()); !errors.Is(err, notification.ErrAbsent) {
		t.Fatalf("reset without a ledger = %v, want ErrAbsent", err)
	}
	if _, err := store.Read(context.Background()); !errors.Is(err, notification.ErrAbsent) {
		t.Fatal("reset created a ledger")
	}
}
