package reconcile

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"time"

	"github.com/skaphos/oiax/v2/internal/forge"
	"github.com/skaphos/oiax/v2/internal/notification"
	"github.com/skaphos/oiax/v2/internal/notification/delivery"
	notificationstore "github.com/skaphos/oiax/v2/internal/notification/store"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

// PrepareNotifications establishes durable activation cutoffs before any PR
// POST, so a first enabled invocation can truthfully announce its own creates.
// Failure defers notifications only; the caller must still run core apply.
func (c *Coordinator) PrepareNotifications(ctx context.Context) error {
	return c.withNotificationRuntime(ctx, func(r *NotificationRuntime) error { return r.Activate(ctx) })
}

// FinalizeNotifications recovers actual POST outcomes independently of scans,
// then observes lifecycle and dispatches. It never replaces the core result.
func (c *Coordinator) FinalizeNotifications(ctx context.Context, outcomes ...forge.CreateOutcome) error {
	ctx, cancel := context.WithTimeout(ctx, notification.FinalizeBudget)
	defer cancel()
	return c.withNotificationRuntime(ctx, func(r *NotificationRuntime) error {
		if err := r.Activate(ctx); err != nil {
			return err
		}
		var problems []error
		for _, outcome := range outcomes {
			if outcome.Request.ID == "" || outcome.Origin == nil || (outcome.Disposition != forge.RequestCreated && outcome.Disposition != forge.RequestAdopted) {
				continue
			}
			// No event is made from the attempted POST alone. The provider
			// confirms ownership, immutable origin and the real creation time.
			request, err := r.Reader.GetLifecycleRequest(ctx, forge.RequestID(outcome.Request.ID))
			if err != nil {
				problems = append(problems, notification.ErrLifecycleUnavailable)
				continue
			}
			if err := r.recordObservation(ctx, []notification.LifecycleRequest{request}, "", notification.ScanProgress{}, r.now()); err != nil {
				problems = append(problems, err)
			}
		}
		problems = append(problems, r.Observe(ctx), r.Dispatch(ctx))
		return errors.Join(problems...)
	})
}

// revisionVerifier proves the relation between the accepted and the incoming
// configuration revision. It is a parameter of runtime construction rather than
// a fixed dependency because exactly one command — the operator reset — runs
// with different evidence rules, and the store and the runtime must agree on
// the same rules within one attempt.
type revisionVerifier func(context.Context, string, string) (notification.RevisionRelation, error)

func (c *Coordinator) withNotificationRuntime(ctx context.Context, run func(*NotificationRuntime) error) error {
	return c.withVerifiedNotificationRuntime(ctx, c.notificationRevisionRelation, run)
}

func (c *Coordinator) withVerifiedNotificationRuntime(ctx context.Context, verify revisionVerifier, run func(*NotificationRuntime) error) (err error) {
	if !c.NotificationPolicy.IsEnabled() {
		return nil
	}
	if c.Git == nil || c.Graph == nil || !notification.ValidOID(c.ConfigOID) {
		return notification.ErrInvalidState
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	reader, ok := c.Forge.(forge.LifecycleReader)
	if !ok {
		return notification.ErrLifecycleUnavailable
	}
	notesProvider, ok := c.Forge.(forge.NotificationNotesProvider)
	if !ok {
		return notification.ErrUnavailable
	}
	repository, err := reader.RepositoryIdentity(ctx)
	if err != nil {
		return notification.ErrLifecycleUnavailable
	}
	notes, err := notesProvider.OpenNotificationNotes(ctx, notification.GraphKey(repository, c.Graph.Name))
	if err != nil {
		return notification.ErrUnavailable
	}
	defer func() { err = errors.Join(err, notes.Close()) }()

	ledger := notificationstore.New(notes, repository, c.Graph.Name)
	ledger.VerifyRevision = verify
	runtime := &NotificationRuntime{
		Store:       ledger,
		Reader:      reader,
		Repository:  repository,
		Graph:       c.Graph.Name,
		Topology:    c.Graph,
		ConfigOID:   c.ConfigOID,
		Policy:      c.NotificationPolicy,
		Report:      func(d NotificationDiagnostic) { c.NotificationDiagnostics = append(c.NotificationDiagnostics, d) },
		Now:         func() time.Time { return time.Now().UTC() },
		OperationID: newNotificationOperationID,
		LookupEnv:   os.LookupEnv,
		Sender: func(destination v1.NotificationDestination) notification.Sender {
			return delivery.NewClient(destination.Type, destination.AllowPrivateNetwork)
		},
		VerifyRevision: verify,
		Log:            c.log(),
	}
	return run(runtime)
}

// ResetNotificationRevision performs the operator-authorized recovery for a
// ledger whose accepted configuration commit is gone. It returns the audit
// record it appended, or a nil record when the ledger already accepts the
// pinned revision and nothing had to be repaired.
//
// It is never called by plan or reconcile. The pinned configuration OID, the
// policy digest and the recorded revision all come from the same reviewed
// commit, exactly as an ordinary activation does; the single difference is the
// evidence that authorizes the advance.
func (c *Coordinator) ResetNotificationRevision(ctx context.Context) (*notification.RevisionOverrideV1, error) {
	var record *notification.RevisionOverrideV1
	err := c.withVerifiedNotificationRuntime(ctx, c.notificationRevisionOverride, func(r *NotificationRuntime) error {
		var err error
		record, err = r.ResetRevision(ctx)
		return err
	})
	return record, err
}

// notificationOrigin captures pre-POST hints without changing core error
// semantics. Config and branch state are already pinned/validated by the CLI;
// unavailable optional evidence means no creation provenance for this attempt.
func (c *Coordinator) notificationOrigin(ctx context.Context, source, target, head string) *notification.NotificationOriginV1 {
	if !c.NotificationPolicy.IsEnabled() || !notification.ValidOID(c.ConfigOID) || !notification.ValidOID(head) || c.Git == nil {
		return nil
	}
	base, err := c.Git.Head(ctx, target)
	if err != nil || !notification.ValidOID(base) {
		c.log().Warn("notification creation provenance unavailable")
		return nil
	}
	return &notification.NotificationOriginV1{Version: 1, OperationID: newNotificationOperationID(), Graph: c.Graph.Name, ConfigOID: c.ConfigOID, ObservedAt: time.Now().UTC(), LogicalSource: source, LogicalTarget: target, SourceOID: head, BaseOID: base}
}

func (c *Coordinator) notificationRevisionRelation(ctx context.Context, accepted, incoming string) (notification.RevisionRelation, error) {
	acceptedBeforeIncoming, err := c.Git.IsAncestor(ctx, accepted, incoming)
	if err != nil {
		return notification.RevisionUnknown, c.classifyRevisionFailure(ctx, accepted, err)
	}
	if acceptedBeforeIncoming {
		return notification.RevisionDescendant, nil
	}
	incomingBeforeAccepted, err := c.Git.IsAncestor(ctx, incoming, accepted)
	if err != nil {
		return notification.RevisionUnknown, c.classifyRevisionFailure(ctx, accepted, err)
	}
	if incomingBeforeAccepted {
		return notification.RevisionAncestor, nil
	}
	return notification.RevisionDivergent, nil
}

// classifyRevisionFailure names the ancestry failure that never heals.
// `merge-base --is-ancestor` cannot answer for an object the repository does
// not have, and the accepted OID is the one that can be stranded permanently: a
// force-push, a branch rewrite or a GC of the configuration history removes it
// while the ledger goes on naming it, so every later run defers on the same
// error forever.
//
// A DEFINITIVE local absence (CommitExists exit 1) is reported as
// ErrRevisionUnreachable. That changes no decision — it wraps
// ErrUnorderedRevision, so every caller still defers — only the diagnostic,
// which can then name a recovery instead of asking for a descendant of a commit
// nobody has. Anything else keeps the original failure: a cancelled context or
// a broken git must never be read as a missing commit.
func (c *Coordinator) classifyRevisionFailure(ctx context.Context, accepted string, err error) error {
	if present, existsErr := c.Git.CommitExists(ctx, accepted); existsErr == nil && !present {
		return notification.ErrRevisionUnreachable
	}
	return err
}

// notificationRevisionOverride is the verifier installed for `oiax
// notifications reset`, and the only producer of RevisionOverride evidence.
//
// It overrides ordering ONLY when the accepted commit is definitively absent —
// the state that has no other exit. Whenever the commit is present, ordering is
// decidable and the ordinary rules are applied unchanged, so the reset can never
// be used to install a stale or divergent revision over a healthy ledger. Local
// absence alone is weak evidence (a shallow or partial checkout simply lacks
// objects the remote still has), which is why the command layer refuses to run
// in a shallow repository and requires the operator to name the revision being
// accepted; this function is reached only after a human has attested to both.
func (c *Coordinator) notificationRevisionOverride(ctx context.Context, accepted, incoming string) (notification.RevisionRelation, error) {
	present, err := c.Git.CommitExists(ctx, accepted)
	if err != nil {
		return notification.RevisionUnknown, err
	}
	if present {
		return notification.RevisionUnknown, notification.ErrRevisionReachable
	}
	// Accepting a revision whose own commit is missing would strand the ledger
	// again on the very next run.
	incomingPresent, err := c.Git.CommitExists(ctx, incoming)
	if err != nil {
		return notification.RevisionUnknown, err
	}
	if !incomingPresent {
		return notification.RevisionUnknown, notification.ErrRevisionUnreachable
	}
	return notification.RevisionOverride, nil
}

func newNotificationOperationID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	return notification.Digest("operation-v1", time.Now().UTC().Format(time.RFC3339Nano))
}
