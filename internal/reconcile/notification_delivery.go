package reconcile

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/skaphos/oiax/internal/notification"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

const notificationSendTimeout = 10 * time.Second

// Dispatch sends a bounded, fair snapshot of due notification work. Every
// message and claim is durable before the endpoint is resolved or contacted;
// results are reduced monotonically afterward. A failed destination does not
// prevent other destinations in the same bounded run from making progress.
func (r *NotificationRuntime) Dispatch(ctx context.Context) error {
	if !r.Policy.IsEnabled() {
		return nil
	}
	if r.Store == nil || r.LookupEnv == nil || r.Sender == nil || !notification.ValidOID(r.ConfigOID) {
		return notification.ErrInvalidState
	}
	templates, err := notification.ResolveTemplates(r.Policy)
	if err != nil {
		return err
	}
	operationID := ""
	if r.OperationID != nil {
		operationID = r.OperationID()
	}
	if operationID == "" || len(operationID) > 128 {
		return notification.ErrInvalidState
	}

	stageCtx, cancel := context.WithTimeout(ctx, notification.ClaimDuration)
	defer cancel()
	snapshot, err := r.Store.Read(stageCtx)
	if err != nil {
		return err
	}
	if snapshot.Ledger == nil {
		return notification.ErrAbsent
	}
	if snapshot.Ledger.PolicyRevision.ConfigOID != r.ConfigOID {
		return notification.ErrStaleRevision
	}
	destinations := enabledDestinations(r.Policy)
	keys := notification.DueDeliveries(snapshot.Ledger, r.now())
	type task struct {
		index       int
		key         string
		destination v1.NotificationDestination
	}
	groups := map[string][]task{}
	for index, key := range keys {
		destination, ok := destinationForKey(snapshot.Ledger, key, destinations)
		if !ok {
			continue
		}
		groups[destination.Name] = append(groups[destination.Name], task{index: index, key: key, destination: destination})
	}
	type taskResult struct {
		index       int
		err         error
		destination string
	}
	results := make(chan taskResult, len(keys))
	var workers sync.WaitGroup
	for _, tasks := range groups {
		tasks := tasks
		workers.Add(1)
		go func() {
			defer workers.Done()
			for _, current := range tasks {
				if err := stageCtx.Err(); err != nil {
					results <- taskResult{index: current.index, err: err, destination: current.destination.Name}
					return
				}
				attemptID := notification.Digest("attempt-v1", operationID, current.key)
				err := r.dispatchOne(stageCtx, current.key, attemptID, current.destination, templates)
				if errors.Is(err, notification.ErrNotDue) {
					continue
				}
				if err != nil {
					err = fmt.Errorf("notification destination %s: %w", current.destination.Name, err)
				}
				results <- taskResult{index: current.index, err: err, destination: current.destination.Name}
			}
		}()
	}
	workers.Wait()
	close(results)
	ordered := make([]taskResult, 0, len(results))
	for result := range results {
		ordered = append(ordered, result)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].index < ordered[j].index })
	problems := make([]error, 0, len(ordered))
	for _, result := range ordered {
		if r.Report != nil {
			d := NotificationProblem(result.err)
			d.Destination = result.destination
			r.Report(d)
		}
		if result.err != nil {
			problems = append(problems, result.err)
		}
	}
	return errors.Join(problems...)
}

func enabledDestinations(policy *v1.NotificationPolicy) map[string]v1.NotificationDestination {
	result := make(map[string]v1.NotificationDestination, len(policy.Destinations))
	for _, destination := range policy.Destinations {
		if destination.IsEnabled() {
			result[destination.Name] = destination
		}
	}
	return result
}

func destinationForKey(l *notification.LedgerV1, key string, configured map[string]v1.NotificationDestination) (v1.NotificationDestination, bool) {
	record, ok := l.Deliveries[key]
	if !ok {
		return v1.NotificationDestination{}, false
	}
	destination, ok := configured[record.Destination]
	return destination, ok
}

func (r *NotificationRuntime) dispatchOne(ctx context.Context, key, attemptID string, destination v1.NotificationDestination, templates *notification.TemplateSet) error {
	prepared, err := r.commit(ctx, func(_ context.Context, l *notification.LedgerV1) (*notification.LedgerV1, error) {
		if l == nil {
			return nil, notification.ErrAbsent
		}
		record, ok := l.Deliveries[key]
		if !ok {
			return nil, notification.ErrNotDue
		}
		if record.Message != nil {
			return l.Clone(), nil
		}
		message, err := templates.Render(destination.Name, l.Events[record.EventID])
		if err != nil {
			return nil, err
		}
		return notification.SaveMessage(l, r.ConfigOID, key, message)
	})
	if err != nil {
		return err
	}

	now := r.now()
	waitFor := claimDelay(prepared.Ledger, key, now)
	if waitFor > 0 {
		if err := r.wait(ctx, waitFor); err != nil {
			return err
		}
		now = r.now()
	}
	_, err = r.commit(ctx, func(_ context.Context, l *notification.LedgerV1) (*notification.LedgerV1, error) {
		if l == nil {
			return nil, notification.ErrAbsent
		}
		return notification.Claim(l, r.ConfigOID, key, attemptID, now)
	})
	if err != nil {
		return err
	}

	// Renewing through the current accepted revision is the final durable fence
	// before resolving a secret and allowing the sender to reach the network.
	now = r.now()
	renewed, err := r.commit(ctx, func(_ context.Context, l *notification.LedgerV1) (*notification.LedgerV1, error) {
		if l == nil {
			return nil, notification.ErrAbsent
		}
		return notification.RenewClaim(l, r.ConfigOID, key, attemptID, now)
	})
	if err != nil {
		return err
	}
	payload, err := deliveryPayload(renewed.Ledger, key, attemptID, r.ConfigOID)
	if err != nil {
		return err
	}
	endpoint, _ := r.LookupEnv(destination.EndpointEnv)
	sender := r.Sender(destination)
	if sender == nil {
		result := notification.AttemptResult{Code: notification.OutcomeConfiguration}
		if err := r.recordResult(ctx, key, attemptID, result); err != nil {
			return err
		}
		return errors.New(string(result.Code))
	}
	sendCtx, cancel := context.WithTimeout(ctx, notificationSendTimeout)
	result := sender.Send(sendCtx, endpoint, payload)
	cancel()
	if err := r.recordResult(ctx, key, attemptID, result); err != nil {
		if result.Code == notification.OutcomeAccepted {
			return errors.Join(notification.ErrReceiptUncertain, err)
		}
		return err
	}
	if result.Code != notification.OutcomeAccepted {
		return errors.New(string(result.Code))
	}
	return nil
}

func deliveryPayload(l *notification.LedgerV1, key, attemptID, configOID string) (notification.DeliveryPayloadV1, error) {
	if l == nil || l.PolicyRevision.ConfigOID != configOID {
		return notification.DeliveryPayloadV1{}, notification.ErrStaleRevision
	}
	record, ok := l.Deliveries[key]
	if !ok || record.Status != notification.StatusClaimed || record.Lease.AttemptID != attemptID || record.Message == nil {
		return notification.DeliveryPayloadV1{}, notification.ErrNotDue
	}
	event, ok := l.Events[record.EventID]
	if !ok {
		return notification.DeliveryPayloadV1{}, notification.ErrInvalidState
	}
	return notification.DeliveryPayloadV1{SchemaVersion: notification.SchemaVersion, Event: event, Message: *record.Message}, nil
}

func claimDelay(l *notification.LedgerV1, key string, now time.Time) time.Duration {
	if l == nil {
		return 0
	}
	record, ok := l.Deliveries[key]
	if !ok {
		return 0
	}
	destination := l.Destinations[record.Destination]
	if destination.Lease.AttemptID != "" || !record.Lease.Until.IsZero() {
		return 0
	}
	return max(destination.NextSendAt.Sub(now), 0)
}

func (r *NotificationRuntime) recordResult(ctx context.Context, key, attemptID string, result notification.AttemptResult) error {
	_, err := r.commit(ctx, func(_ context.Context, l *notification.LedgerV1) (*notification.LedgerV1, error) {
		if l == nil {
			return nil, notification.ErrAbsent
		}
		return notification.RecordResult(l, key, attemptID, result, r.now())
	})
	return err
}

func (r *NotificationRuntime) wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	if r.Wait != nil {
		return r.Wait(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
