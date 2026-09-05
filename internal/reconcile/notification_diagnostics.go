package reconcile

import (
	"context"
	"errors"

	"github.com/skaphos/oiax/internal/notification"
)

type NotificationDiagnostic struct{ Destination, Reason, Action string }

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
		for current := err; current != nil; current = errors.Unwrap(current) {
			code := notification.OutcomeCode(current.Error())
			if notification.ValidOutcome(code) {
				d.Reason = string(code)
				switch code {
				case notification.OutcomeMissingSecret:
					d.Action = "Set the named runtime endpoint variable, then retry."
				case notification.OutcomeConfiguration, notification.OutcomeInvalidEndpoint, notification.OutcomeRedirect:
					d.Action = "Review endpoint HTTPS, TLS, DNS and private-network policy, then retry."
				case notification.OutcomePayloadTooLarge, notification.OutcomeResponseTooLarge:
					d.Action = "Reduce custom presentation or receiver response size, then retry."
				case notification.OutcomeRetired:
					d.Action = "This subscription was deliberately retired; no retry is scheduled."
				default:
					d.Action = "Retry when the saved backoff expires; the event ID and attempted payload remain unchanged."
				}
				break
			}
		}
	}
	return d
}
