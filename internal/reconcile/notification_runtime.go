package reconcile

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"time"

	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/notification"
	"github.com/skaphos/oiax/internal/notification/delivery"
	notificationstore "github.com/skaphos/oiax/internal/notification/store"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
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

func (c *Coordinator) withNotificationRuntime(ctx context.Context, run func(*NotificationRuntime) error) (err error) {
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
	ledger.VerifyRevision = c.notificationRevisionRelation
	runtime := &NotificationRuntime{
		Store:       ledger,
		Reader:      reader,
		Repository:  repository,
		Graph:       c.Graph.Name,
		Topology:    c.Graph,
		ConfigOID:   c.ConfigOID,
		Policy:      c.NotificationPolicy,
		Now:         func() time.Time { return time.Now().UTC() },
		OperationID: newNotificationOperationID,
		LookupEnv:   os.LookupEnv,
		Sender: func(destination v1.NotificationDestination) notification.Sender {
			return delivery.NewClient(destination.Type, destination.AllowPrivateNetwork)
		},
		VerifyRevision: c.notificationRevisionRelation,
	}
	return run(runtime)
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
		return notification.RevisionUnknown, err
	}
	if acceptedBeforeIncoming {
		return notification.RevisionDescendant, nil
	}
	incomingBeforeAccepted, err := c.Git.IsAncestor(ctx, incoming, accepted)
	if err != nil {
		return notification.RevisionUnknown, err
	}
	if incomingBeforeAccepted {
		return notification.RevisionAncestor, nil
	}
	return notification.RevisionDivergent, nil
}

func newNotificationOperationID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	return notification.Digest("operation-v1", time.Now().UTC().Format(time.RFC3339Nano))
}
