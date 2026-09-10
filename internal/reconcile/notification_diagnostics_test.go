package reconcile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/skaphos/oiax/v2/internal/notification"
)

func TestNotificationDiagnosticsAreSafe(t *testing.T) {
	for _, err := range []error{errors.New("https://receiver.invalid/credential-canary"), notification.ErrReceiptUncertain, notification.ErrReceiptNotPersisted, notification.ErrStaleRevision, notification.ErrUnorderedRevision, notification.ErrInvalidState, notification.ErrCapacity} {
		d := NotificationProblem(err)
		if d.Reason == "" || d.Action == "" || strings.Contains(d.Reason+d.Action, "credential-canary") || strings.Contains(d.Action, "delete") {
			t.Fatalf("unsafe or unactionable: %+v", d)
		}
	}
	if NotificationProblem(notification.ErrReceiptUncertain).Reason == NotificationProblem(nil).Reason {
		t.Fatal("uncertain acceptance reported as durable success")
	}
}

func TestNotificationDiagnosticScope(t *testing.T) {
	t.Parallel()
	d := NotificationProblem(notification.ErrInvalidState)
	if d.Destination != "" || d.Scope() != "all destinations" {
		t.Fatalf("global problem invented a recipient or omitted its scope: %+v", d)
	}
	d.Destination = "ops"
	if d.Scope() != "ops" {
		t.Fatal("destination-specific scope lost")
	}
}

func TestNotificationDiagnosticsJoinedOutcomes(t *testing.T) {
	t.Parallel()
	missing := errors.New(string(notification.OutcomeMissingSecret))
	network := errors.New(string(notification.OutcomeNetwork))
	unknown := errors.New("https://receiver.invalid/credential-canary")
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"wrapped", fmt.Errorf("destination ops: %w", missing), missing},
		{"single join", errors.Join(nil, missing), missing},
		{"unknown before outcome", errors.Join(unknown, missing), missing},
		{"nested joins and wraps", fmt.Errorf("dispatch: %w", errors.Join(unknown, fmt.Errorf("ops: %w", errors.Join(network, missing)))), network},
		{"first missing", errors.Join(missing, network), missing},
		{"first network", errors.Join(network, missing), network},
		{"no substring match", errors.Join(errors.New("prefix missing-secret suffix"), network), network},
		{"receipt priority", errors.Join(missing, notification.ErrReceiptUncertain), notification.ErrReceiptUncertain},
		{"state priority", errors.Join(network, notification.ErrInvalidState), notification.ErrInvalidState},
		{"cancellation priority", errors.Join(network, context.Canceled), context.Canceled},
		{"unknown only", errors.Join(unknown, errors.New("prefix network-failure suffix")), unknown},
		{"nil join", errors.Join(nil, nil), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, want := NotificationProblem(tc.err), NotificationProblem(tc.want)
			if got != want {
				t.Fatalf("diagnostic = %+v, want %+v", got, want)
			}
			if strings.Contains(got.Reason+got.Action+got.Scope(), "credential-canary") {
				t.Fatal("joined error leaked a credential")
			}
		})
	}
}

func TestNotificationPresentationRedactsAddresses(t *testing.T) {
	canary := "https://receiver.invalid/credential-canary?token=value"
	if got := notification.SafeDisplayText("subject "+canary, true, ""); strings.Contains(got, "credential-canary") || !strings.Contains(got, "[redacted URL]") {
		t.Fatal("address survived sanitization")
	}
	for _, code := range []notification.OutcomeCode{notification.OutcomeMissingSecret, notification.OutcomeNetwork, notification.OutcomeRateLimited, notification.OutcomePayloadTooLarge, notification.OutcomeRetired, notification.OutcomeAbandoned} {
		if d := NotificationProblem(errors.New(string(code))); d.Reason != string(code) || d.Action == "" {
			t.Fatalf("outcome lost: %+v", d)
		}
	}
	// A terminal record must not be reported with retry-when-backoff-expires
	// advice, and abandonment is not the same event as a deliberate retirement.
	abandoned := NotificationProblem(errors.New(string(notification.OutcomeAbandoned)))
	if abandoned == NotificationProblem(errors.New(string(notification.OutcomeRetired))) || strings.Contains(abandoned.Action, "Retry when the saved backoff expires") || strings.Contains(abandoned.Action, "new generation") || !strings.Contains(abandoned.Action, "not automatically resent") || !strings.Contains(abandoned.Action, "future messages") {
		t.Fatalf("abandonment is indistinguishable or suggests a retry: %+v", abandoned)
	}
	payload := NotificationProblem(errors.New(string(notification.OutcomePayloadTooLarge)))
	if !strings.Contains(payload.Action, "terminal") || !strings.Contains(payload.Action, "not automatically resent") || !strings.Contains(payload.Action, "future messages") {
		t.Fatalf("payload terminality is unclear: %+v", payload)
	}
}

func TestNotificationDiagnosticsCarryReceiverStatus(t *testing.T) {
	t.Parallel()
	const refused = "Receiver refused the request (HTTP 400); check the webhook URL and signature, that the flow is enabled, and that its trigger schema accepts the documented payload."
	const refusedNoStatus = "The request was never exchanged with the receiver: the payload could not be encoded for this transport. Review custom presentation and the destination type, then retry."
	const transport = "Review endpoint HTTPS, TLS, DNS and private-network policy, then retry."
	for _, tc := range []struct {
		name   string
		err    error
		reason string
		status int
		action string
	}{
		{"typed configuration", notification.OutcomeError{Code: notification.OutcomeConfiguration, Status: 400}, "configuration-failure", 400, refused},
		{"wrapped and joined", fmt.Errorf("destination ops: %w", errors.Join(errors.New("https://receiver.invalid/credential-canary"), notification.OutcomeError{Code: notification.OutcomeConfiguration, Status: 400})), "configuration-failure", 400, refused},
		{"text-only configuration", errors.New(string(notification.OutcomeConfiguration)), "configuration-failure", 0, refusedNoStatus},
		{"configuration without exchange", notification.OutcomeError{Code: notification.OutcomeConfiguration}, "configuration-failure", 0, refusedNoStatus},
		{"invalid endpoint keeps transport text", notification.OutcomeError{Code: notification.OutcomeInvalidEndpoint}, "invalid-endpoint", 0, transport},
		{"redirect keeps transport text", notification.OutcomeError{Code: notification.OutcomeRedirect, Status: 307}, "redirect-rejected", 307, transport},
		{"service status", notification.OutcomeError{Code: notification.OutcomeService, Status: 503}, "service-failure", 503, "Retry when the saved backoff expires; the event ID and attempted payload remain unchanged."},
		{"unknown code is not classified", notification.OutcomeError{Code: "https://receiver.invalid/credential-canary", Status: 400}, "notification-deferred", 0, "Retry reconciliation; inspect provider and notes permissions."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := NotificationProblem(tc.err)
			if d.Reason != tc.reason || d.Status != tc.status || d.Action != tc.action {
				t.Fatalf("diagnostic = %+v", d)
			}
			if strings.Contains(d.Reason+d.Action, "credential-canary") {
				t.Fatal("diagnostic leaked receiver text")
			}
		})
	}
}

func TestNotificationConfigurationActionDistinguishesExchangedRequests(t *testing.T) {
	t.Parallel()
	refused := NotificationProblem(notification.OutcomeError{Code: notification.OutcomeConfiguration, Status: 401})
	if refused.Status != 401 || !strings.Contains(refused.Action, "HTTP 401") || !strings.Contains(refused.Action, "refused") {
		t.Fatalf("refused request diagnostic = %+v", refused)
	}
	local := NotificationProblem(notification.OutcomeError{Code: notification.OutcomeConfiguration})
	if local.Status != 0 || strings.Contains(local.Action, "refused") || !strings.Contains(local.Action, "never exchanged") {
		t.Fatalf("local failure diagnostic = %+v", local)
	}
	uncertain := NotificationProblem(errors.Join(notification.ErrReceiptUncertain, notification.OutcomeError{Code: notification.OutcomeAccepted, Status: 202}, notification.ErrUnavailable))
	if uncertain.Reason != "accepted-receipt-uncertain" || uncertain.Status != 202 {
		t.Fatalf("uncertain receipt diagnostic = %+v", uncertain)
	}
	lost := NotificationProblem(fmt.Errorf("destination ops: %w", notification.ErrClaimLost))
	if lost.Reason != "delivery-claim-lost" || lost.Action == "" {
		t.Fatalf("claim lost diagnostic = %+v", lost)
	}
}

func TestNotificationDiagnosticsKeepStatusWhenReceiptWriteFails(t *testing.T) {
	t.Parallel()
	refused := NotificationProblem(errors.Join(notification.ErrReceiptNotPersisted, notification.OutcomeError{Code: notification.OutcomeConfiguration, Status: 400}, notification.ErrUnavailable))
	if refused.Reason != "delivery-receipt-not-persisted" || refused.Status != 400 || !strings.Contains(refused.Action, string(notification.OutcomeConfiguration)) || strings.Contains(refused.Action, "terminal") {
		t.Fatalf("refused with failed receipt = %+v", refused)
	}
	capacity := NotificationProblem(errors.Join(notification.ErrReceiptNotPersisted, notification.OutcomeError{Code: notification.OutcomeService, Status: 503}, notification.ErrCapacity))
	if capacity.Reason != "notification-capacity-exhausted" || capacity.Status != 503 {
		t.Fatalf("sentinel outranks outcome but must keep the status: %+v", capacity)
	}
}
