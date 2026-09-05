package delivery

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/internal/notification"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

func TestNotificationEndpointPolicy(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "http://example.com/token", "https://user:password@example.com/token", "https://example.com/token#fragment", "file:///etc/passwd", "https://", "https://example.com:bad/token"} {
		if _, err := validateEndpoint(raw); err == nil {
			t.Fatal("invalid endpoint accepted")
		}
	}
	if _, err := validateEndpoint("https://example.com/callback?secret=value"); err != nil {
		t.Fatal("callback query rejected")
	}
	for _, address := range []string{"127.0.0.1", "::1", "169.254.169.254", "fe80::1", "224.0.0.1", "ff02::1", "0.0.0.0", "::", "::ffff:127.0.0.1", "100.100.100.200"} {
		for _, allow := range []bool{false, true} {
			if allowedAddress(netip.MustParseAddr(address), allow) {
				t.Fatalf("forbidden destination %s accepted", address)
			}
		}
	}
	for _, address := range []string{"10.0.0.1", "172.16.0.1", "192.168.1.1", "fd00::1"} {
		ip := netip.MustParseAddr(address)
		if allowedAddress(ip, false) || !allowedAddress(ip, true) {
			t.Fatal("private opt-in mismatch", address)
		}
	}
	if !allowedAddress(netip.MustParseAddr("8.8.8.8"), false) || !allowedAddress(netip.MustParseAddr("2606:4700:4700::1111"), false) {
		t.Fatal("public unicast rejected")
	}
}

type failTransport struct{}

func (failTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("https://secret.invalid/token")
}

func TestNotificationClientFailureBoundaries(t *testing.T) {
	t.Parallel()
	payload := adapterPayload()
	for _, kind := range []v1.NotificationTransport{v1.NotificationSlack, v1.NotificationTeams, v1.NotificationWebhook} {
		c := NewClient(kind, false)
		c.http.Transport = failTransport{}
		if got := c.Send(context.Background(), "https://receiver.invalid/secret", payload); got.Code != notification.OutcomeNetwork {
			t.Fatal("unsafe error outcome", got)
		}
	}
	c := NewClient(v1.NotificationWebhook, false)
	if c.http.Timeout != 10*time.Second {
		t.Fatal("wrong HTTP deadline")
	}
	if c.Send(context.Background(), "", payload).Code != notification.OutcomeMissingSecret {
		t.Fatal("missing secret not distinguished")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c.Send(ctx, "https://receiver.invalid/secret", payload).Code != notification.OutcomeCanceled {
		t.Fatal("cancellation not respected")
	}
	if NewClient(v1.NotificationSlack, true).Send(context.Background(), "https://receiver.invalid/secret", payload).Code != notification.OutcomeInvalidEndpoint {
		t.Fatal("private opt-in allowed for Slack")
	}
}

func TestNotificationClientAcknowledgmentsAndBounds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		kind   v1.NotificationTransport
		status int
		body   string
		want   notification.OutcomeCode
	}{
		{"slack ok", "slack", 200, "ok\n", notification.OutcomeAccepted},
		{"slack wrong body", "slack", 200, "not ok", notification.OutcomeConfiguration},
		{"slack wrong status", "slack", 202, "ok", notification.OutcomeConfiguration},
		{"teams async", "teams", 202, "", notification.OutcomeAccepted},
		{"webhook empty", "webhook", 204, "", notification.OutcomeAccepted},
		{"redirect", "webhook", 307, "", notification.OutcomeRedirect},
		{"throttle", "slack", 429, "secret response", notification.OutcomeRateLimited},
		{"server", "teams", 503, "secret response", notification.OutcomeService},
		{"auth", "webhook", 401, "secret response", notification.OutcomeConfiguration},
		{"oversized", "teams", 200, strings.Repeat("x", (16<<10)+1), notification.OutcomeResponseTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "" {
					t.Error("forge authentication forwarded")
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Error("missing JSON content type")
				}
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Retry-After", "120")
				w.Header().Set("Location", "/redirected")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(s.Close)
			c := NewClient(tc.kind, false)
			c.http.Transport = s.Client().Transport // test-only trusted local TLS
			result := c.Send(context.Background(), s.URL+"/secret", adapterPayload())
			if result.Code != tc.want || calls != 1 {
				t.Fatalf("got %+v calls %d", result, calls)
			}
			if tc.status == 429 && result.RetryAfter != 2*time.Minute {
				t.Fatal("Retry-After lost")
			}
		})
	}
}
