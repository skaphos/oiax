// Package v1 defines the public configuration types for Oiax.
//
// A repository configures Oiax with a single PromotionGraph document,
// by default at `.oiax.yaml`, read from a pinned ref (the repository's
// default branch unless overridden with --config-ref). Everything outside
// this package is internal; these types are the compatibility surface for
// `apiVersion: oiax.skaphos.dev/v1`. The pre-1.0 literal
// `apiVersion: oiax.skaphos.dev/v1alpha1` is accepted as a deprecated
// alias for backward compatibility.
package v1

// APIVersion is the canonical configuration API version this module accepts.
const APIVersion = "oiax.skaphos.dev/v1"

// APIVersionV1Alpha1 is the deprecated pre-1.0 alias, still accepted for
// backward compatibility. Configuration declaring it parses identically to
// APIVersion but is warned once on load; migrate the apiVersion string to
// APIVersion.
const APIVersionV1Alpha1 = "oiax.skaphos.dev/v1alpha1"

// KindPromotionGraph is the only configuration kind this module accepts.
const KindPromotionGraph = "PromotionGraph"

// Role is validation metadata attached to a branch. Roles constrain the
// shape of the promotion graph; they do not change edge evaluation.
type Role string

const (
	// RoleNone marks an intermediate branch (the zero value).
	RoleNone Role = ""
	// RoleSource marks the authoritative entry branch. A source branch
	// must not be the destination of any promotion edge, and the backflow
	// target must have this role.
	RoleSource Role = "source"
	// RoleTerminal marks an exit branch. A terminal branch must not be
	// the source of any promotion edge.
	RoleTerminal Role = "terminal"
)

// DriftPolicy states whether downstream-only commits on a branch are
// steady state or a divergence worth reporting.
type DriftPolicy string

const (
	// DriftForbidden (the default) reports downstream-only content, and
	// returns it via backflow when the branch is a backflow source.
	DriftForbidden DriftPolicy = "forbidden"
	// DriftExpected acknowledges downstream-only content silently.
	// Promotion detection is unaffected: it looks only at source-side
	// content. A branch must not combine DriftExpected with membership
	// in backflow sources; the two are contradictory.
	DriftExpected DriftPolicy = "expected"
)

// MergeMethod names a forge merge method for edge expectations.
type MergeMethod string

const (
	MergeMethodMerge  MergeMethod = "merge"
	MergeMethodSquash MergeMethod = "squash"
	MergeMethodRebase MergeMethod = "rebase"
)

// BackflowStrategy names the mechanism used to return downstream-only
// commits to the backflow target.
type BackflowStrategy string

// BackflowStrategyCherryPick replays downstream-only commits onto a
// branch created from the target using `git cherry-pick -x`. The -x
// trailer gives every returned commit durable provenance and the
// backflow identity check its cheapest rung. The only strategy in v1.
const BackflowStrategyCherryPick BackflowStrategy = "cherry-pick"

// PromotionGraph is the root configuration document. v1 accepts exactly
// one PromotionGraph per configuration file; multi-document YAML is
// reserved for a future version.
type PromotionGraph struct {
	// APIVersion must be "oiax.skaphos.dev/v1" ("oiax.skaphos.dev/v1alpha1"
	// accepted, deprecated).
	APIVersion string `yaml:"apiVersion"`
	// Kind must be "PromotionGraph".
	Kind string `yaml:"kind"`
	// Metadata names the graph.
	Metadata Metadata `yaml:"metadata"`
	// Spec declares the branches, promotion edges, and backflow policy.
	Spec PromotionGraphSpec `yaml:"spec"`
}

// Metadata identifies a PromotionGraph.
type Metadata struct {
	// Name identifies the graph in managed-request metadata, plans, and
	// logs. Required.
	Name string `yaml:"name"`
}

// PromotionGraphSpec declares the promotion topology.
type PromotionGraphSpec struct {
	// Branches maps long-lived branch names to per-branch settings.
	// Every branch referenced by a promotion edge or the backflow policy
	// must appear here. Oiax never creates long-lived branches; each
	// configured branch must already exist as a ref.
	Branches map[string]Branch `yaml:"branches"`
	// Promotions declares the allowed promotion edges. The edges must
	// form a directed acyclic graph; disconnected components are allowed
	// (multiple independent promotion paths in one repository).
	Promotions []Promotion `yaml:"promotions"`
	// Backflow optionally declares how exceptional downstream changes
	// (hotfixes) are returned to the authoritative source branch.
	Backflow *Backflow `yaml:"backflow,omitempty"`
}

// Branch holds per-branch settings.
type Branch struct {
	// Role is optional validation metadata: "source", "terminal", or
	// unset for an intermediate branch.
	Role Role `yaml:"role,omitempty"`
	// Drift states whether downstream-only commits on this branch are
	// expected steady state ("expected") or reportable divergence
	// ("forbidden", the default).
	Drift DriftPolicy `yaml:"drift,omitempty"`
}

// Promotion declares one directed promotion edge.
type Promotion struct {
	// From is the source branch of the edge.
	From string `yaml:"from"`
	// To is the destination branch of the edge.
	To string `yaml:"to"`
	// Expectations optionally declares reporting-only expectations for
	// the edge.
	Expectations *Expectations `yaml:"expectations,omitempty"`
}

// Expectations are validation and reporting metadata for an edge. Oiax
// warns when repository settings contradict them; it never modifies
// repository settings.
type Expectations struct {
	// MergeMethod is the merge method promotion requests on this edge
	// are expected to merge with: "merge", "squash", or "rebase".
	MergeMethod MergeMethod `yaml:"mergeMethod,omitempty"`
}

// Backflow declares the hotfix-return policy: which downstream branches
// are watched for downstream-only content, and the single authoritative
// branch that content is returned to.
type Backflow struct {
	// Sources are the downstream branches whose downstream-only commits
	// are returned to Target. A source must not use drift "expected".
	Sources []string `yaml:"sources"`
	// Target is the authoritative branch backflow returns commits to.
	// It must be a configured branch with role "source". v1 supports
	// exactly one backflow target per graph.
	Target string `yaml:"target"`
	// Strategy selects the return mechanism. Only "cherry-pick" is
	// supported in v1; it is also the default when unset.
	Strategy BackflowStrategy `yaml:"strategy,omitempty"`
}
