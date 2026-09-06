// Package delivery implements bounded outbound notification effects. Endpoint
// secrets are ephemeral inputs and never appear in returned diagnostics.
package delivery

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/skaphos/oiax/v2/internal/notification"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

type Client struct {
	kind    v1.NotificationTransport
	private bool
	http    *http.Client
}

func NewClient(kind v1.NotificationTransport, allowPrivate bool) *Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return newClient(kind, allowPrivate, net.DefaultResolver.LookupNetIP, dialer.DialContext)
}

type lookupNetIPFunc func(context.Context, string, string) ([]netip.Addr, error)
type dialContextFunc func(context.Context, string, string) (net.Conn, error)

func newClient(kind v1.NotificationTransport, allowPrivate bool, lookup lookupNetIPFunc, dial dialContextFunc) *Client {
	// A fresh transport's nil Proxy disables proxies. Do not clone
	// http.DefaultTransport: its environment proxy bypasses our direct dial policy.
	transport := &http.Transport{MaxConnsPerHost: 1, IdleConnTimeout: 10 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 10 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("invalid destination address")
		}
		addresses, err := lookup(ctx, "ip", host)
		if err != nil || len(addresses) == 0 {
			return nil, errors.New("destination lookup failed")
		}
		// Validate all answers immediately before dialing a numeric address.
		// No second DNS lookup is performed; TLS still verifies the URL hostname.
		for _, ip := range addresses {
			if !allowedAddress(ip, allowPrivate) {
				return nil, errors.New("destination address rejected")
			}
		}
		for _, ip := range addresses {
			conn, err := dial(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			if ctx.Err() != nil {
				break
			}
		}
		return nil, errors.New("destination connection failed")
	}
	return &Client{kind: kind, private: allowPrivate, http: &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func validateEndpoint(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.ContainsAny(raw, "\x00\r\n") {
		return nil, notification.ErrInvalidState
	}
	if p := u.Port(); p != "" {
		port, err := strconv.Atoi(p)
		if err != nil || port < 1 || port > 65535 {
			return nil, notification.ErrInvalidState
		}
	}
	return u, nil
}

func allowedAddress(ip netip.Addr, allowPrivate bool) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	// Shared-address, documentation, benchmarking, reserved, transition and
	// metadata ranges are not public receivers (even with private opt-in).
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "100:0:0:1::/64", "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20", "5f00::/16", "fec0::/10"} {
		if netip.MustParsePrefix(cidr).Contains(ip) {
			return false
		}
	}
	return !ip.IsPrivate() || allowPrivate
}

func (c *Client) Send(ctx context.Context, endpoint string, payload notification.DeliveryPayloadV1) notification.AttemptResult {
	result := func(code notification.OutcomeCode) notification.AttemptResult {
		return notification.AttemptResult{Code: code}
	}
	if ctx.Err() != nil {
		return result(notification.OutcomeCanceled)
	}
	if endpoint == "" {
		return result(notification.OutcomeMissingSecret)
	}
	if c.private && c.kind != v1.NotificationWebhook {
		return result(notification.OutcomeInvalidEndpoint)
	}
	u, err := validateEndpoint(endpoint)
	if err != nil {
		return result(notification.OutcomeInvalidEndpoint)
	}
	body, err := encode(c.kind, payload)
	if err != nil {
		if errors.Is(err, notification.ErrCapacity) {
			return result(notification.OutcomePayloadTooLarge)
		}
		return result(notification.OutcomeConfiguration)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return result(notification.OutcomeInvalidEndpoint)
	}
	request.Header.Set("Content-Type", "application/json")
	if c.kind == v1.NotificationWebhook {
		request.Header.Set("X-Oiax-Event-ID", payload.Event.ID)
	}
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return result(notification.OutcomeCanceled)
		}
		return result(notification.OutcomeNetwork)
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, (16<<10)+1))
	if err != nil {
		return result(notification.OutcomeNetwork)
	}
	if len(data) > 16<<10 {
		return result(notification.OutcomeResponseTooLarge)
	}
	status := response.StatusCode
	if status == http.StatusTooManyRequests {
		return notification.AttemptResult{Code: notification.OutcomeRateLimited, RetryAfter: retryAfter(response.Header.Get("Retry-After"), time.Now())}
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return result(notification.OutcomeService)
	}
	if status >= 300 && status < 400 {
		return result(notification.OutcomeRedirect)
	}
	if c.kind == v1.NotificationSlack {
		if status == 200 && strings.TrimSpace(string(data)) == "ok" {
			return result(notification.OutcomeAccepted)
		}
	} else if status >= 200 && status < 300 {
		return result(notification.OutcomeAccepted)
	}
	return result(notification.OutcomeConfiguration)
}

func retryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 {
			return 0
		}
		return time.Duration(min(seconds, int64(24*time.Hour/time.Second))) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		return min(max(when.Sub(now), 0), 24*time.Hour)
	}
	return 0
}

var _ notification.Sender = (*Client)(nil)
