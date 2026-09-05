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

// FinalizeNotifications runs the optional effect stage after core apply. Its
// caller owns exit semantics: notification diagnostics must never replace the
// core reconciliation result or error.
func (c *Coordinator) FinalizeNotifications(ctx context.Context) (err error) {
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
	if err := runtime.Activate(ctx); err != nil {
		return err
	}
	observeErr := runtime.Observe(ctx)
	dispatchErr := runtime.Dispatch(ctx)
	return errors.Join(observeErr, dispatchErr)
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
