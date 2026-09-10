package reconcile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/skaphos/oiax/v2/internal/notification"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

const (
	notificationSendTimeout = 10 * time.Second
	notificationSendSpacing = time.Second
)

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

	stageCtx, cancel := context.WithTimeout(ctx, notification.FinalizeBudget)
	defer cancel()
	snapshot, err := r.read(stageCtx)
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
	groups := map[string][]deliveryTask{}
	for index, key := range keys {
		destination, ok := destinationForKey(snapshot.Ledger, key, destinations)
		if !ok {
			continue
		}
		groups[destination.Name] = append(groups[destination.Name], deliveryTask{index: index, key: key, destination: destination})
	}
	results := make(chan deliveryOutcome, len(keys))
	var workers sync.WaitGroup
	for _, tasks := range groups {
		workers.Add(1)
		go func() {
			defer workers.Done()
			r.dispatchBatch(stageCtx, operationID, tasks, templates, results)
		}()
	}
	workers.Wait()
	close(results)
	ordered := make([]deliveryOutcome, 0, len(results))
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

type deliveryTask struct {
	index       int
	key         string
	destination v1.NotificationDestination
}

type deliveryOutcome struct {
	index       int
	err         error
	destination string
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

// dispatchBatch performs one destination's work with two ledger writes: one
// that saves missing messages and claims every due record under a batch lease,
// and one that records every receipt. A renewal is written only while the
// batch is still sending as its lease approaches expiry. Every send first
// re-observes the durable ledger and proves the batch still owns the
// destination and record at this run's revision, so a policy accepted by
// another run between the claim and any POST stops the stale payloads before
// they reach the network. Once a record is claimed its receipt is always written, even
// after the stage budget expires, so a POST that was issued can never be
// replayed as if it had not happened.
func (r *NotificationRuntime) dispatchBatch(ctx context.Context, operationID string, tasks []deliveryTask, templates *notification.TemplateSet, results chan<- deliveryOutcome) {
	destination := tasks[0].destination
	name := destination.Name
	batchID := notification.BatchID(operationID, name)
	attempts := make(map[string]string, len(tasks))
	indexes := make(map[string]int, len(tasks))
	keys := make([]string, 0, len(tasks))
	for _, task := range tasks {
		attempts[task.key] = notification.Digest("attempt-v1", operationID, task.key)
		indexes[task.key] = task.index
		keys = append(keys, task.key)
	}
	failure := func(err error) error {
		if err == nil {
			return nil
		}
		return fmt.Errorf("notification destination %s: %w", name, err)
	}
	if err := ctx.Err(); err != nil {
		results <- deliveryOutcome{index: tasks[0].index, err: err, destination: name}
		return
	}
	sender := r.Sender(destination)
	if sender == nil {
		// A runtime wired without a sender is a programming error, never a
		// receiver refusal. Nothing has been claimed, so no lease is held.
		results <- deliveryOutcome{index: tasks[0].index, err: failure(notification.ErrInvalidState), destination: name}
		return
	}

	var claimed []string
	var unrenderable map[string]error
	prepared, err := r.commit(ctx, func(_ context.Context, l *notification.LedgerV1) (*notification.LedgerV1, error) {
		if l == nil {
			return nil, notification.ErrAbsent
		}
		// The transition is re-evaluated on every conflict, so per-record
		// failures are rebuilt from the fresh snapshot each time.
		failed := map[string]error{}
		present := make([]string, 0, len(keys))
		for _, key := range keys {
			record, ok := l.Deliveries[key]
			if !ok {
				continue
			}
			if record.Message == nil {
				// A record whose message cannot be rendered or saved stays
				// pending on its own, exactly as the per-record loop left it;
				// it must not block the destination's other due work, and a
				// failed save must leave the ledger being built untouched.
				message, err := templates.Render(name, l.Events[record.EventID])
				if err != nil {
					failed[key] = err
					continue
				}
				saved, err := notification.SaveMessage(l, r.ConfigOID, key, message)
				if err != nil {
					failed[key] = err
					continue
				}
				l = saved
			}
			present = append(present, key)
		}
		unrenderable = failed
		var err error
		l, claimed, err = notification.ClaimBatch(l, r.ConfigOID, name, batchID, present, attempts, r.now())
		return l, err
	})
	if err == nil || errors.Is(err, notification.ErrNotDue) {
		for key, renderErr := range unrenderable {
			results <- deliveryOutcome{index: indexes[key], err: failure(renderErr), destination: name}
		}
	}
	if errors.Is(err, notification.ErrNotDue) {
		return
	}
	if err != nil {
		results <- deliveryOutcome{index: tasks[0].index, err: failure(err), destination: name}
		return
	}

	leaseUntil := prepared.Ledger.Destinations[name].Lease.Until
	receipts := make(map[string]notification.AttemptReceipt, len(claimed))
	for _, key := range claimed {
		receipts[key] = notification.AttemptReceipt{AttemptID: attempts[key], Result: notification.AttemptResult{Code: notification.OutcomeCanceled}}
	}
	attempted := map[string]error{}
	endpoint, _ := r.LookupEnv(destination.EndpointEnv)
	var stop error
	current := prepared
	for i, key := range claimed {
		if i > 0 {
			if err := r.wait(ctx, notificationSendSpacing); err != nil {
				stop = err
				break
			}
		}
		if err := ctx.Err(); err != nil {
			stop = err
			break
		}
		switch {
		case leaseUntil.Sub(r.now()) < notification.ClaimDuration/2:
			renewed, err := r.commit(ctx, func(_ context.Context, l *notification.LedgerV1) (*notification.LedgerV1, error) {
				if l == nil {
					return nil, notification.ErrAbsent
				}
				return notification.RenewBatch(l, r.ConfigOID, name, batchID, attempts, r.now())
			})
			if err != nil {
				stop = err
				break
			}
			current = renewed
			leaseUntil = renewed.Ledger.Destinations[name].Lease.Until
		default:
			// Every send observes the ledger again first; with the tip cache
			// that costs one advertisement when nothing moved.
			observed, err := r.read(ctx)
			if err != nil {
				stop = err
				break
			}
			current = observed
		}
		if stop != nil {
			break
		}
		if err := notification.CheckBatchSend(current.Ledger, r.ConfigOID, name, batchID, key, attempts[key], r.now()); err != nil {
			if errors.Is(err, notification.ErrClaimLost) {
				// Another attempt settled this record; its durable state is
				// authoritative and the canceled receipt below is a no-op on it.
				// The batch still owns the destination, so the rest proceeds.
				attempted[key] = err
				continue
			}
			stop = err
			break
		}
		payload, err := deliveryPayload(current.Ledger, key, attempts[key], r.ConfigOID)
		if err != nil {
			attempted[key] = err
			continue
		}
		sendCtx, cancel := context.WithTimeout(ctx, notificationSendTimeout)
		started := time.Now()
		result := sender.Send(sendCtx, endpoint, payload)
		cancel()
		r.logAttempt(name, result, time.Since(started))
		receipts[key] = notification.AttemptReceipt{AttemptID: attempts[key], Result: result}
		attempted[key] = nil
	}

	// The receipt write is detached from stage cancellation and bounded on its
	// own. Abandoned claims are recorded as canceled so no record keeps an
	// earlier attempt's code while waiting for its lease to expire.
	receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), notification.ReceiptWriteTimeout)
	defer cancel()
	recorded, writeErr := r.commit(receiptCtx, func(_ context.Context, l *notification.LedgerV1) (*notification.LedgerV1, error) {
		if l == nil {
			return nil, notification.ErrAbsent
		}
		return notification.RecordResults(l, name, batchID, receipts, r.now())
	})
	for _, key := range claimed {
		result := receipts[key].Result
		err, sent := attempted[key]
		switch {
		case !sent && errors.Is(stop, notification.ErrNotDue):
			err = notification.OutcomeError{Code: notification.OutcomeCanceled}
		case !sent:
			err = stop
		case err != nil:
		case writeErr != nil && result.Code == notification.OutcomeAccepted:
			// The receiver's status is kept so the uncertainty diagnostic can
			// still say what the receiver answered.
			err = errors.Join(notification.ErrReceiptUncertain, notification.OutcomeError{Code: result.Code, Status: result.Status}, writeErr)
		case writeErr != nil:
			// The receiver's verdict is kept alongside the write failure so the
			// diagnostic can still carry the outcome and status.
			err = errors.Join(notification.OutcomeError{Code: result.Code, Status: result.Status}, writeErr)
		case result.Code != notification.OutcomeAccepted:
			// The receiver's status stays attached to the durable terminal code,
			// including exhaustion and an intrinsically oversize payload.
			err = notification.OutcomeError{Code: recordedOutcome(recorded.Ledger, key, result.Code), Status: result.Status}
		}
		results <- deliveryOutcome{index: indexes[key], err: failure(err), destination: name}
	}
}

// recordedOutcome prefers a durable terminal code over the transport code, so
// diagnostics distinguish exhaustion while preserving an intrinsically terminal
// transport result such as payload-too-large.
func recordedOutcome(l *notification.LedgerV1, key string, sent notification.OutcomeCode) notification.OutcomeCode {
	if l == nil {
		return sent
	}
	if record, ok := l.Deliveries[key]; ok && record.Status == notification.StatusSkipped && record.Code != notification.OutcomeRetired {
		return record.Code
	}
	return sent
}

// logAttempt reports one attempt's cost and classification. Endpoints, payload
// text and receiver bodies are never logged.
func (r *NotificationRuntime) logAttempt(destination string, result notification.AttemptResult, elapsed time.Duration) {
	attrs := []slog.Attr{slog.String("destination", destination), slog.String("reason", string(result.Code))}
	if result.Status != 0 {
		attrs = append(attrs, slog.Int("status", result.Status))
	}
	attrs = append(attrs, slog.Int64("elapsed_ms", elapsed.Milliseconds()))
	r.log().LogAttrs(context.Background(), slog.LevelInfo, "notification attempt", attrs...)
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
