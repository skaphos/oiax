package reconcile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/notification"
)

func TestNotificationDiagnosticsAreSafe(t *testing.T) {
	for _, err := range []error{errors.New("https://receiver.invalid/credential-canary"), notification.ErrReceiptUncertain, notification.ErrStaleRevision, notification.ErrUnorderedRevision, notification.ErrInvalidState, notification.ErrCapacity} {
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
	for _, code := range []notification.OutcomeCode{notification.OutcomeMissingSecret, notification.OutcomeNetwork, notification.OutcomeRateLimited, notification.OutcomePayloadTooLarge, notification.OutcomeRetired} {
		if d := NotificationProblem(errors.New(string(code))); d.Reason != string(code) || d.Action == "" {
			t.Fatalf("outcome lost: %+v", d)
		}
	}
}
