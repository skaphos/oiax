package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/v2/internal/git"
	"github.com/skaphos/oiax/v2/internal/gittest"
	"github.com/skaphos/oiax/v2/internal/reconcile"
)

// strandConfigRevision advances the ledger onto a second configuration commit
// and then rewrites that commit out of existence, reproducing what a force-push
// or a GC of the configuration branch does to a repository: the ledger goes on
// naming an accepted commit nobody has any more. It returns the stranded id.
func strandConfigRevision(t *testing.T, f *notificationBinaryFixture) string {
	t.Helper()
	gittest.Run(t, f.dir, "checkout", "-q", "-b", "config-rewrite")
	writeNotificationFixture(t, f.dir+"/app.txt", []byte("second revision\n"))
	gittest.Run(t, f.dir, "add", ".")
	gittest.Run(t, f.dir, "commit", "-q", "-m", "configuration revision two")
	stranded := gittest.Run(t, f.dir, "rev-parse", "HEAD")
	gittest.Run(t, f.dir, "checkout", "-q", "main")
	gittest.Run(t, f.dir, "push", "-q", "origin", "config-rewrite")

	// Accept it the ordinary way: it is a proven descendant while it exists.
	f.configRef = stranded
	f.run(0, "reconcile")
	if got := f.ledger().PolicyRevision.ConfigOID; got != stranded {
		t.Fatalf("accepted revision = %s, want %s", got, stranded)
	}

	// Now rewrite it away, on both sides and out of every reflog.
	gittest.Run(t, f.dir, "push", "-q", "origin", "--delete", "config-rewrite")
	gittest.Run(t, f.dir, "branch", "-q", "-D", "config-rewrite")
	gittest.Run(t, f.dir, "remote", "prune", "origin")
	gittest.Run(t, f.dir, "-c", "gc.reflogExpire=now", "-c", "gc.reflogExpireUnreachable=now", "gc", "--prune=now", "-q")
	gittest.Run(t, f.remote, "-c", "gc.reflogExpire=now", "-c", "gc.reflogExpireUnreachable=now", "gc", "--prune=now", "-q")
	exists, err := (&git.Runner{Dir: f.dir}).CommitExists(context.Background(), stranded)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatalf("the fixture did not strand %s; the unreachable path would not be exercised", stranded)
	}
	f.configRef = ""
	return stranded
}

// Issue #95: an accepted configuration OID that is no longer reachable deferred
// every run forever with no documented recovery — the advised action (commit a
// reviewed descendant) is impossible once the commit is gone, and deleting the
// notes ref is forbidden because it destroys the duplicate-prevention receipts.
func TestNotificationUnreachableRevisionRecovery(t *testing.T) {
	t.Parallel()
	binary := buildNotificationBinary(t)
	f := newNotificationBinaryFixture(t, binary, "github", "webhook")

	// A healthy ledger with a real delivery receipt: the recovery must not cost
	// the operator any of this.
	f.run(0, "reconcile")
	f.setSeeds(f.seed(42, "promotion", "merged", time.Now().UTC(), true))
	f.run(0, "reconcile")
	seeded := f.ledger()
	if len(seeded.Events) == 0 || len(seeded.Deliveries) == 0 {
		t.Fatalf("fixture produced no delivery evidence to protect: %+v", seeded)
	}
	if len(f.messages()) != 1 {
		t.Fatalf("expected one delivered notification, got %d", len(f.messages()))
	}

	stranded := strandConfigRevision(t, f)

	// Reproduce the bug: the run defers, and says why in terms an operator can
	// act on rather than asking for a descendant that cannot be produced.
	planJSON, planStderr := f.run(0, "plan", "--detailed-exitcode")
	var doc struct {
		Notifications reconcile.NotificationPreview `json:"notifications"`
	}
	if err := json.Unmarshal([]byte(planJSON), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Notifications.Reason != "config-revision-unreachable" {
		t.Fatalf("plan preview reason = %q (stderr %s)", doc.Notifications.Reason, planStderr)
	}
	_, stderr := f.run(0, "reconcile")
	if !strings.Contains(stderr, "config-revision-unreachable") || !strings.Contains(stderr, "notifications reset") {
		t.Fatalf("stuck reconcile did not name the recovery: %s", stderr)
	}
	if got := f.ledger().PolicyRevision.ConfigOID; got != stranded {
		t.Fatalf("a deferred run advanced the revision to %s", got)
	}

	// The recovery is refused unless the operator names the exact revision.
	f.run(1, "notifications", "reset", "--accept-revision", strings.Repeat("0", 40))
	f.run(1, "notifications", "reset")

	out, _ := f.run(0, "notifications", "reset", "--accept-revision", f.oid)
	var result struct {
		Reset       bool   `json:"reset"`
		PriorOID    string `json:"priorOID"`
		AcceptedOID string `json:"acceptedOID"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Reset || result.PriorOID != stranded || result.AcceptedOID != f.oid {
		t.Fatalf("reset result = %+v (stranded %s, pinned %s)", result, stranded, f.oid)
	}

	repaired := f.ledger()
	if repaired.PolicyRevision.ConfigOID != f.oid {
		t.Fatalf("accepted revision after reset = %s, want %s", repaired.PolicyRevision.ConfigOID, f.oid)
	}
	if len(repaired.RevisionOverrides) != 1 || repaired.RevisionOverrides[0].PriorOID != stranded || repaired.RevisionOverrides[0].AcceptedOID != f.oid {
		t.Fatalf("ledger audit = %+v", repaired.RevisionOverrides)
	}
	// Non-destructive: the same receipts, so no message is sent twice.
	if len(repaired.Events) != len(seeded.Events) || len(repaired.Deliveries) != len(seeded.Deliveries) {
		t.Fatalf("recovery discarded delivery evidence: %d/%d events, %d/%d deliveries",
			len(repaired.Events), len(seeded.Events), len(repaired.Deliveries), len(seeded.Deliveries))
	}

	// Runs work again, nothing is re-delivered, and the audit record survives.
	_, stderr = f.run(0, "reconcile")
	if strings.Contains(stderr, "config-revision-unreachable") {
		t.Fatalf("still deferring after the recovery: %s", stderr)
	}
	if len(f.messages()) != 1 {
		t.Fatalf("recovery re-sent a delivered notification: %d messages", len(f.messages()))
	}
	if len(f.ledger().RevisionOverrides) != 1 {
		t.Fatal("a later run dropped the override audit record")
	}

	// Re-running the recovery on a healthy ledger changes nothing, and asking
	// for it while the accepted commit resolves is refused outright.
	repeat, _ := f.run(0, "notifications", "reset", "--accept-revision", f.oid)
	if err := json.Unmarshal([]byte(repeat), &result); err != nil {
		t.Fatal(err)
	}
	if result.Reset {
		t.Fatalf("repeat reset recorded a second override: %+v", result)
	}
	f.configRef = strandConfigRevisionSuccessor(t, f)
	_, refusal := f.run(1, "notifications", "reset", "--accept-revision", f.configRef)
	if !strings.Contains(refusal, "config-revision-reachable") {
		t.Fatalf("reset was not refused for a resolvable accepted commit: %s", refusal)
	}
}

// A checkout scoped to part of origin cannot tell a rewritten commit from one
// it simply never fetched, so the reset must refuse before it records an
// override attesting to something it cannot know. actions/checkout produces
// exactly this state by default, and it is NOT shallow.
func TestNotificationResetRefusesIncompleteCheckout(t *testing.T) {
	t.Parallel()
	binary := buildNotificationBinary(t)
	f := newNotificationBinaryFixture(t, binary, "github", "webhook")
	f.run(0, "reconcile")

	for _, tc := range []struct {
		name, want string
		scope      func()
	}{
		{
			name: "single-branch checkout",
			want: "single-branch checkout",
			// Narrow the refspec the way `git clone --single-branch` and
			// actions/checkout do. The repository stays non-shallow.
			scope: func() {
				gittest.Run(t, f.dir, "config", "remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main")
			},
		},
		{
			name:  "shallow checkout",
			want:  "shallow clone",
			scope: func() { writeNotificationFixture(t, filepath.Join(f.dir, ".git", "shallow"), []byte(f.oid+"\n")) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := gittest.Run(t, f.dir, "config", "--get", "remote.origin.fetch")
			tc.scope()
			t.Cleanup(func() {
				gittest.Run(t, f.dir, "config", "remote.origin.fetch", original)
				_ = os.Remove(filepath.Join(f.dir, ".git", "shallow"))
			})
			_, refusal := f.run(1, "notifications", "reset", "--accept-revision", f.oid)
			if !strings.Contains(refusal, tc.want) {
				t.Fatalf("refusal did not name the scope (%s): %s", tc.want, refusal)
			}
			if !strings.Contains(refusal, "not evidence") {
				t.Fatalf("refusal did not explain why absence proves nothing: %s", refusal)
			}
		})
	}
}

// strandConfigRevisionSuccessor commits a real descendant configuration commit
// and leaves it reachable, so a reset attempt against it must be refused: the
// accepted commit still resolves, so ordinary ordering governs.
func strandConfigRevisionSuccessor(t *testing.T, f *notificationBinaryFixture) string {
	t.Helper()
	writeNotificationFixture(t, f.dir+"/app.txt", []byte("third revision\n"))
	gittest.Run(t, f.dir, "add", ".")
	gittest.Run(t, f.dir, "commit", "-q", "-m", "configuration revision three")
	gittest.Run(t, f.dir, "push", "-q", "origin", "main")
	return gittest.Run(t, f.dir, "rev-parse", "HEAD")
}
