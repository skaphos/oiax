// Package github will implement the forge.Forge interface against the
// GitHub REST API. It is the first supported provider (v0.1 roadmap).
//
// Provider notes that shape this implementation:
//
//   - Managed requests are identified by the machine-readable marker in
//     the request body plus branch relationship, never by title.
//     Unmanaged requests between the same branches are never touched.
//   - GitHub rejects a second open pull request with the same head/base
//     pair (HTTP 422); that rejection is adopted as success (re-list and
//     continue), not treated as an error.
//   - Pull requests created with the default GITHUB_TOKEN do not trigger
//     `on: pull_request` workflows. The provider warns when it detects
//     that degraded configuration; production guidance is a GitHub App
//     installation token.
package github

import (
	"context"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
)

// Provider is the GitHub implementation of forge.Forge.
type Provider struct {
	// Owner and Repo identify the repository.
	Owner string
	Repo  string
	// Token is the credential used for API calls: a GitHub App
	// installation token (recommended), fine-grained PAT, or
	// GITHUB_TOKEN (degraded; created requests get no CI).
	Token string
}

var _ forge.Forge = (*Provider)(nil)

func (p *Provider) ListManagedRequests(ctx context.Context, filter forge.RequestFilter) ([]engine.ChangeRequest, error) {
	return nil, forge.ErrNotImplemented
}

func (p *Provider) CreateRequest(ctx context.Context, req forge.CreateRequest) (engine.ChangeRequest, error) {
	return engine.ChangeRequest{}, forge.ErrNotImplemented
}

func (p *Provider) UpdateRequest(ctx context.Context, req forge.UpdateRequest) error {
	return forge.ErrNotImplemented
}

func (p *Provider) CloseRequest(ctx context.Context, id forge.RequestID, reason forge.Reason) error {
	return forge.ErrNotImplemented
}

func (p *Provider) PushBranch(ctx context.Context, push forge.BranchPush) error {
	return forge.ErrNotImplemented
}
