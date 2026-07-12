// Package forge defines the provider-neutral interface Oiax uses to
// observe and mutate change requests on a Git forge.
//
// The provider-neutral term is "change request", not "pull request":
// GitHub and Azure DevOps providers present change requests as pull
// requests; a future provider may map the abstraction differently.
//
// Authentication belongs to provider implementations. The engine must
// not contain token or credential concepts, and credential values must
// never appear in any output.
package forge

import (
	"context"
	"errors"

	"github.com/skaphos/oiax/internal/engine"
)

// ErrNotImplemented marks provider capabilities that are declared but
// not yet built (see the roadmap in docs/architecture.md).
var ErrNotImplemented = errors.New("not implemented")

// RequestID identifies a change request within a provider.
type RequestID string

// RequestState narrows managed-request discovery to open or merged
// requests. The equivalence ladder's baseline rung needs merged managed
// requests (their recorded sourceHead is the promotion baseline), so
// discovery must be able to ask for them in addition to open ones.
type RequestState string

const (
	// RequestStateOpen is the zero value and the common case: only open
	// managed requests.
	RequestStateOpen RequestState = ""
	// RequestStateMerged selects closed requests that were merged, for the
	// baseline rung of the equivalence ladder.
	RequestStateMerged RequestState = "merged"
)

// RequestFilter narrows managed-request discovery.
type RequestFilter struct {
	// Graph restricts results to requests managed for a named graph.
	Graph string
	// Type restricts results to promotion or backflow requests.
	Type engine.RequestType
	// State selects open (zero value) or merged managed requests.
	State RequestState
}

// CreateRequest describes a change request to create. Providers attach
// the machine-readable Oiax metadata marker and default labels; titles
// are presentation only.
type CreateRequest struct {
	Graph      string
	Type       engine.RequestType
	Source     string
	Target     string
	SourceHead string
	Title      string
	Body       string
}

// UpdateRequest describes an update to a managed request's metadata.
type UpdateRequest struct {
	ID         RequestID
	SourceHead string
}

// Reason explains a close, both to humans (a comment on the request)
// and in structured output. Obsolete requests are closed with an
// explanatory comment, never silently, and never deleted.
type Reason struct {
	Summary string
}

// BranchPush pushes a branch in the oiax/ namespace. Force pushing is
// confined to that namespace; providers must refuse to force-push any
// ref outside it.
type BranchPush struct {
	Name  string
	SHA   string
	Force bool
}

// Forge is the capability surface a provider implements. Providers must
// treat "create failed because an equivalent managed request already
// exists" as success: re-list, adopt the surviving request, continue —
// the forge is the concurrency arbiter for promotion requests.
type Forge interface {
	ListManagedRequests(ctx context.Context, filter RequestFilter) ([]engine.ChangeRequest, error)
	CreateRequest(ctx context.Context, req CreateRequest) (engine.ChangeRequest, error)
	UpdateRequest(ctx context.Context, req UpdateRequest) error
	CloseRequest(ctx context.Context, id RequestID, reason Reason) error
	PushBranch(ctx context.Context, push BranchPush) error

	// RepoMergeMethods reports which merge methods the repository currently
	// permits, so the coordinator can warn when a configured mergeMethod
	// contradicts it. It reads settings only and never modifies them.
	RepoMergeMethods(ctx context.Context) (MergeMethods, error)

	// DeleteBranch removes a branch in the oiax/ namespace (a superseded or
	// closed backflow request's head branch). Deletion is confined to that
	// namespace — Oiax owns it, so removing an orphaned ref is in-contract;
	// providers must refuse any name outside oiax/. Deleting an
	// already-absent branch is not an error (the reconcile is idempotent).
	DeleteBranch(ctx context.Context, name string) error
}

// MergeMethods reports which merge methods a repository permits, mirroring
// GitHub's allow_merge_commit / allow_squash_merge / allow_rebase_merge
// repository settings. Oiax reads it only to warn on a contradicting
// mergeMethod expectation; it never changes settings.
type MergeMethods struct {
	Merge  bool
	Squash bool
	Rebase bool
}

// Allows reports whether the repository permits the named merge method
// ("merge", "squash", or "rebase"). An empty or unrecognized method is treated
// as allowed, so an edge that declares no mergeMethod expectation never warns.
func (m MergeMethods) Allows(method string) bool {
	switch method {
	case "merge":
		return m.Merge
	case "squash":
		return m.Squash
	case "rebase":
		return m.Rebase
	default:
		return true
	}
}
