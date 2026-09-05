package delivery

// Compiled only through the CLI test's overlay. Production NewClient is not
// changed: this fixture supplies DNS/dial/CA boundaries to the same safe client.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"os"

	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

func NewFixtureClient(kind v1.NotificationTransport, private bool) *Client {
	lookup := func(_ context.Context, _, host string) ([]netip.Addr, error) {
		if host != "example.com" {
			return nil, errors.New("unexpected fixture host")
		}
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "8.8.8.8:443" {
			return nil, errors.New("unexpected fixture address")
		}
		return (&net.Dialer{}).DialContext(ctx, network, os.Getenv("OIAX_FIXTURE_RECEIVER"))
	}
	client := newClient(kind, private, lookup, dial)
	cert, err := os.ReadFile(os.Getenv("OIAX_FIXTURE_CA"))
	if err != nil {
		panic("missing fixture CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(cert) {
		panic("invalid fixture CA")
	}
	client.http.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return client
}
