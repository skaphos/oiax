package cli

// This file is included only in the fixture binary through go build -overlay.
// The real providers parse HTTP responses and use their production notes writer;
// only their construction points at a local HTTPS server and bare Git remote.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/forge/azuredevops"
	"github.com/skaphos/oiax/internal/forge/github"
)

func init() {
	newForge = func(context.Context, *slog.Logger) (forge.Forge, error) {
		cert, err := os.ReadFile(os.Getenv("OIAX_FIXTURE_CA"))
		if err != nil {
			return nil, err
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(cert) {
			return nil, errors.New("invalid fixture CA")
		}
		client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		}}
		if os.Getenv("OIAX_FORGE") == "azuredevops" {
			return &azuredevops.Provider{
				Repo:    azuredevops.Repo{Organization: "org", Project: "project", Name: "repo"},
				BaseURL: os.Getenv("OIAX_FIXTURE_API"), HTTP: client,
				GitRemote: os.Getenv("OIAX_FIXTURE_REMOTE"), Token: "fixture-forge-canary",
			}, nil
		}
		return &github.Provider{
			Owner: "example", Repo: "repo", BaseURL: os.Getenv("OIAX_FIXTURE_API"), HTTP: client,
			GitRemote: os.Getenv("OIAX_FIXTURE_REMOTE"), Token: "fixture-forge-canary",
		}, nil
	}
}
