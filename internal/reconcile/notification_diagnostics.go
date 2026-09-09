package reconcile

import (
	"context"
	"errors"
	"fmt"

	"github.com/skaphos/oiax/v2/internal/notification"
)

// NotificationDiagnostic carries only closed reason/action text plus the
// receiver's integer HTTP status when an exchange completed.
type NotificationDiagnostic struct {
	Destination, Reason, Action string
	Status                      int
}

// Scope labels global failures without inventing a configured destination.
func (d NotificationDiagnostic) Scope() string {
	if d.Destination == "" {
		return "all destinations"
	}
	return d.Destination
}

// NotificationProblem never formats the supplied error. Unknown providers and
// transports may embed endpoint credentials in their errors.
func NotificationProblem(err error) NotificationDiagnostic {
	d := NotificationDiagnostic{Reason: "notification-deferred", Action: "Retry reconciliation; inspect provider and notes permissions."}
	switch {
	case err == nil:
		d.Reason, d.Action = "delivered", "Delivery has a persisted receipt; no retry is needed."
	case errors.Is(err, notification.ErrReceiptUncertain):
		d.Reason, d.Action = "accepted-receipt-uncertain", "Receiver may have accepted this event; retry preserves its ID but may duplicate visibility."
	case errors.Is(err, notification.ErrStaleRevision):
		d.Reason, d.Action = "stale-config-revision", "Run the latest reviewed descendant configuration commit."
	case errors.Is(err, notification.ErrUnorderedRevision):
		d.Reason, d.Action = "config-revision-unordered", "Restore a reviewed descendant configuration commit; preserve notification notes."
	case errors.Is(err, notification.ErrPolicyMismatch):
		d.Reason, d.Action = "policy-revision-mismatch", "Use the pinned configuration and template files from the same reviewed commit."
	case errors.Is(err, notification.ErrInvalidState):
		d.Reason, d.Action = "invalid-notification-state", "Suspend sends and restore a reviewed valid notes history or use a compatible binary."
	case errors.Is(err, notification.ErrCapacity):
		d.Reason, d.Action = "notification-capacity-exhausted", "Preserve receipts; reduce pending workload or upgrade capacity before retrying."
	case errors.Is(err, notification.ErrTemplateInvalid):
		d.Reason, d.Action = "notification-template-invalid", "Review the closed template fields and reduce rendered output to the documented limits."
	case errors.Is(err, notification.ErrAbsent):
		d.Reason, d.Action = "notification-ledger-absent", "The next reconcile establishes a current cutoff; restore lost notes before running if prior receipts must be retained."
	case errors.Is(err, notification.ErrDiscoveryIncomplete):
		d.Reason, d.Action = "notification-discovery-incomplete", "Retry reconciliation to resume bounded discovery; existing eligible deliveries can proceed."
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		d.Reason, d.Action = "notification-canceled", "Retry on the next scheduled run; saved receipts and payloads are retained."
	}
	// Compare complete leaf codes, never interpolate an arbitrary error string.
	if d.Reason == "notification-deferred" {
		if code, status := notificationOutcome(err); code != "" {
			d.Reason = string(code)
			d.Status = status
			switch code {
			case notification.OutcomeMissingSecret:
				d.Action = "Set the named runtime endpoint variable, then retry."
			case notification.OutcomeConfiguration:
				// The request reached the receiver and was refused. Only the
				// integer status is ever interpolated.
				d.Action = "Receiver refused the request; check the webhook URL and signature, that the flow is enabled, and that its trigger schema accepts the documented payload."
				if status != 0 {
					d.Action = fmt.Sprintf("Receiver refused the request (HTTP %d); check the webhook URL and signature, that the flow is enabled, and that its trigger schema accepts the documented payload.", status)
				}
			case notification.OutcomeInvalidEndpoint, notification.OutcomeRedirect:
				d.Action = "Review endpoint HTTPS, TLS, DNS and private-network policy, then retry."
			case notification.OutcomePayloadTooLarge, notification.OutcomeResponseTooLarge:
				d.Action = "Reduce custom presentation or receiver response size, then retry."
			case notification.OutcomeRetired:
				d.Action = "This subscription was deliberately retired; no retry is scheduled."
			default:
				d.Action = "Retry when the saved backoff expires; the event ID and attempted payload remain unchanged."
			}
		}
	}
	return d
}

// notificationOutcome walks wrapped and joined errors depth-first, left-to-right.
// A summary selects the first recognized leaf; per-destination diagnostics are
// still reported separately. Never classify a wrapper's combined error text.
func notificationOutcome(err error) (notification.OutcomeCode, int) {
	if err == nil {
		return "", 0
	}
	switch current := err.(type) {
	case interface{ Unwrap() []error }:
		for _, child := range current.Unwrap() {
			if code, status := notificationOutcome(child); code != "" {
				return code, status
			}
		}
	case interface{ Unwrap() error }:
		return notificationOutcome(current.Unwrap())
	default:
		var outcome notification.OutcomeError
		if errors.As(err, &outcome) && notification.ValidOutcome(outcome.Code) {
			return outcome.Code, outcome.Status
		}
		code := notification.OutcomeCode(err.Error())
		if notification.ValidOutcome(code) {
			return code, 0
		}
	}
	return "", 0
}
