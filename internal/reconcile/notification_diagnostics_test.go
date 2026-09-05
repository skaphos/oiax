package reconcile

import (
	"errors"
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
