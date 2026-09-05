package delivery

import (
	"context"
	"errors"
	"io"
	"net"
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
	for _, raw := range []string{"", "http://example.com/token", "https://user:password@example.com/token", "https://example.com/token#fragment", "file:///etc/passwd", "https://", "https://example.com:bad/token", "https://example.com/token\r\nInjected: yes"} {
		if _, err := validateEndpoint(raw); err == nil {
			t.Fatal("invalid endpoint accepted")
		}
	}
	if _, err := validateEndpoint("https://example.com/callback?secret=value"); err != nil {
		t.Fatal("callback query rejected")
	}
	for _, address := range []string{
		"0.0.0.0", "127.0.0.1", "169.254.169.254", "224.0.0.1", "100.100.100.200",
		"192.0.0.1", "192.0.2.1", "192.88.99.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "240.0.0.1",
		"::", "::1", "::ffff:127.0.0.1", "fe80::1", "ff02::1", "64:ff9b::1", "64:ff9b:1::1", "100::1", "100:0:0:1::1", "2001::1", "2001:db8::1", "2002::1", "3fff::1", "5f00::1", "fec0::1",
	} {
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

func TestNotificationClientChecksEveryDNSAnswerAtConnectionTime(t *testing.T) {
	t.Parallel()
	lookups := 0
	dials := 0
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		lookups++
		if lookups == 1 {
			return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
		}
		// A rebinding/mixed response is rejected in full; the client never
		// chooses the apparently safe answer and ignores the loopback answer.
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}, nil
	}
	dial := func(context.Context, string, string) (net.Conn, error) {
		dials++
		return nil, errors.New("fixture connection refused")
	}
	c := newClient(v1.NotificationWebhook, false, lookup, dial)
	for range 2 {
		if got := c.Send(context.Background(), "https://receiver.example/hook", adapterPayload()); got.Code != notification.OutcomeNetwork {
			t.Fatalf("unexpected safe failure: %+v", got)
		}
	}
	if lookups != 2 || dials != 1 {
		t.Fatalf("connection-time checks: lookups=%d dials=%d", lookups, dials)
	}
}

func TestNotificationClientPreservesOriginalHostForTLS(t *testing.T) {
	t.Parallel()
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	if len(server.Certificate().DNSNames) == 0 {
		t.Fatal("httptest certificate has no DNS identity")
	}
	validHost := server.Certificate().DNSNames[0]
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	dialer := &net.Dialer{}
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, server.Listener.Addr().String())
	}
	client := func() *Client {
		c := newClient(v1.NotificationWebhook, false, lookup, dial)
		transport := c.http.Transport.(*http.Transport)
		transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
		return c
	}
	if got := client().Send(context.Background(), "https://not-"+validHost+":"+port+"/hook", adapterPayload()); got.Code != notification.OutcomeNetwork {
		t.Fatalf("mismatched TLS identity accepted: %+v", got)
	}
	if got := client().Send(context.Background(), "https://"+validHost+":"+port+"/hook", adapterPayload()); got.Code != notification.OutcomeAccepted {
		t.Fatalf("matching TLS identity rejected: %+v", got)
	}
	if calls != 1 {
		t.Fatalf("TLS mismatch reached receiver: calls=%d", calls)
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
	lookupCalled := false
	c = newClient(v1.NotificationWebhook, false, func(context.Context, string, string) ([]netip.Addr, error) {
		lookupCalled = true
		return nil, errors.New("must not resolve")
	}, func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("must not dial")
	})
	if c.Send(context.Background(), "", payload).Code != notification.OutcomeMissingSecret || lookupCalled {
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
	privateDialed := false
	privateWebhook := newClient(v1.NotificationWebhook, true, func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.0.0.1")}, nil
	}, func(context.Context, string, string) (net.Conn, error) {
		privateDialed = true
		return nil, errors.New("fixture connection refused")
	})
	if got := privateWebhook.Send(context.Background(), "https://internal.example/hook", payload); got.Code != notification.OutcomeNetwork || !privateDialed {
		t.Fatalf("generic private opt-in did not reach its approved address: %+v", got)
	}
}

func TestNotificationClientEncodingFailureOutcomes(t *testing.T) {
	t.Parallel()
	for _, kind := range []v1.NotificationTransport{v1.NotificationTeams, v1.NotificationSlack, v1.NotificationWebhook} {
		for _, tc := range []struct {
			name   string
			change func(*notification.DeliveryPayloadV1)
			want   notification.OutcomeCode
		}{
			{"schema", func(p *notification.DeliveryPayloadV1) { p.SchemaVersion = 99 }, notification.OutcomeConfiguration},
			{"facts", func(p *notification.DeliveryPayloadV1) { p.Event.Request.URL = "" }, notification.OutcomeConfiguration},
			{"capacity", func(p *notification.DeliveryPayloadV1) { p.Message.Body = strings.Repeat("x", 24<<10) }, notification.OutcomePayloadTooLarge},
		} {
			t.Run(string(kind)+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				c := newClient(kind, false, func(context.Context, string, string) ([]netip.Addr, error) {
					t.Error("invalid payload reached DNS")
					return nil, errors.New("must not resolve")
				}, nil)
				p := adapterPayload()
				tc.change(&p)
				if got := c.Send(context.Background(), "https://receiver.example/hook", p); got.Code != tc.want {
					t.Fatalf("got %s, want %s", got.Code, tc.want)
				}
			})
		}
	}
}

func TestNotificationClientDisablesEnvironmentProxies(t *testing.T) {
	// Environment changes intentionally run outside parallel tests.
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, "http://proxy.invalid:8080")
	}
	for _, kind := range []v1.NotificationTransport{v1.NotificationTeams, v1.NotificationSlack, v1.NotificationWebhook} {
		c := NewClient(kind, false)
		transport, ok := c.http.Transport.(*http.Transport)
		if !ok || transport.Proxy != nil {
			t.Fatalf("%s allows a proxy to bypass direct destination validation", kind)
		}
	}
}

func TestNotificationClientHonorsCallerDeadline(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	}))
	// Release the intentionally blocked fixture handler before server cleanup.
	t.Cleanup(func() {
		close(release)
		server.CloseClientConnections()
		server.Close()
	})
	c := NewClient(v1.NotificationWebhook, false)
	c.http.Transport = server.Client().Transport
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result := c.Send(ctx, server.URL+"/hook", adapterPayload())
	if result.Code != notification.OutcomeCanceled {
		t.Fatalf("deadline result = %+v", result)
	}
	select {
	case <-started:
	default:
		t.Fatal("deadline fixture never received request")
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
			payload := adapterPayload()
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "" {
					t.Error("forge authentication forwarded")
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Error("missing JSON content type")
				}
				if tc.kind == v1.NotificationWebhook && r.Header.Get("X-Oiax-Event-ID") != payload.Event.ID {
					t.Error("missing generic webhook event identity header")
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
			result := c.Send(context.Background(), s.URL+"/secret", payload)
			if result.Code != tc.want || calls != 1 {
				t.Fatalf("got %+v calls %d", result, calls)
			}
			if tc.status == 429 && result.RetryAfter != 2*time.Minute {
				t.Fatal("Retry-After lost")
			}
		})
	}
}
