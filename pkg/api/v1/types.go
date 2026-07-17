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
// backflow identity check its cheapest rung. It is the default strategy
// when the backflow policy leaves Strategy unset.
const BackflowStrategyCherryPick BackflowStrategy = "cherry-pick"

// BackflowStrategyMerge returns downstream-only commits by merging the
// source head into a branch created from the target with a single
// no-fast-forward merge commit, preserving the commits' original SHAs
// and ancestry rather than replaying them. It cannot honor per-commit
// exclusion (the Oiax-Backflow: skip trailer) and requires the target
// branch to permit merge commits.
const BackflowStrategyMerge BackflowStrategy = "merge"

// PromotionGraph is the root configuration document. v1 accepts exactly
// one PromotionGraph per configuration file; multi-document YAML is
// reserved for a future version.
type PromotionGraph struct {
	// APIVersion must be "oiax.skaphos.dev/v1" ("oiax.skaphos.dev/v1alpha1"
	// accepted, deprecated).
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	// Kind must be "PromotionGraph".
	Kind string `yaml:"kind" json:"kind"`
	// Metadata names the graph.
	Metadata Metadata `yaml:"metadata" json:"metadata"`
	// Spec declares the branches, promotion edges, and backflow policy.
	Spec PromotionGraphSpec `yaml:"spec" json:"spec"`
}

// Metadata identifies a PromotionGraph.
type Metadata struct {
	// Name identifies the graph in managed-request metadata, plans, and
	// logs. Required.
	Name string `yaml:"name" json:"name"`
}

// PromotionGraphSpec declares the promotion topology.
type PromotionGraphSpec struct {
	// Branches maps long-lived branch names to per-branch settings.
	// Every branch referenced by a promotion edge or the backflow policy
	// must appear here. Oiax never creates long-lived branches; each
	// configured branch must already exist as a ref.
	Branches map[string]Branch `yaml:"branches" json:"branches"`
	// Promotions declares the allowed promotion edges. The edges must
	// form a directed acyclic graph; disconnected components are allowed
	// (multiple independent promotion paths in one repository).
	Promotions []Promotion `yaml:"promotions" json:"promotions"`
	// Backflow optionally declares how exceptional downstream changes
	// (hotfixes) are returned to the authoritative source branch.
	Backflow *Backflow `yaml:"backflow,omitempty" json:"backflow,omitempty"`
}

// Branch holds per-branch settings.
type Branch struct {
	// Role is optional validation metadata: "source", "terminal", or
	// unset for an intermediate branch.
	Role Role `yaml:"role,omitempty" json:"role,omitempty"`
	// Drift states whether downstream-only commits on this branch are
	// expected steady state ("expected") or reportable divergence
	// ("forbidden", the default).
	Drift DriftPolicy `yaml:"drift,omitempty" json:"drift,omitempty"`
}

// Promotion declares one directed promotion edge.
type Promotion struct {
	// From is the source branch of the edge.
	From string `yaml:"from" json:"from"`
	// To is the destination branch of the edge.
	To string `yaml:"to" json:"to"`
	// Expectations optionally declares reporting-only expectations for
	// the edge.
	Expectations *Expectations `yaml:"expectations,omitempty" json:"expectations,omitempty"`
}

// Expectations are validation and reporting metadata for an edge. Oiax
// validates the declared value against the closed MergeMethod enum and
// warns when the forge's repository settings do not permit the configured
// method; it never modifies repository settings.
type Expectations struct {
	// MergeMethod is the merge method promotion requests on this edge
	// are expected to merge with: "merge", "squash", or "rebase".
	MergeMethod MergeMethod `yaml:"mergeMethod,omitempty" json:"mergeMethod,omitempty"`
}

// Backflow declares the hotfix-return policy: which downstream branches
// are watched for downstream-only content, and the single authoritative
// branch that content is returned to.
type Backflow struct {
	// Sources are the downstream branches whose downstream-only commits
	// are returned to Target. A source must not use drift "expected".
	Sources []string `yaml:"sources" json:"sources"`
	// Target is the authoritative branch backflow returns commits to.
	// It must be a configured branch with role "source". v1 supports
	// exactly one backflow target per graph.
	Target string `yaml:"target" json:"target"`
	// Strategy selects the return mechanism: "cherry-pick" (the default
	// when unset) replays each downstream-only commit; "merge" returns
	// them wholesale with a single no-fast-forward merge commit.
	Strategy BackflowStrategy `yaml:"strategy,omitempty" json:"strategy,omitempty"`
	// ExpectedMergeMethod is the forge merge method the backflow request
	// is expected to merge with: "merge", "squash", or "rebase". It is
	// validated against the closed MergeMethod enum. With strategy
	// "merge" it must be "merge" (or left unset, which defaults to
	// "merge"), because only a merge commit preserves the returned
	// commits' original SHAs and ancestry.
	ExpectedMergeMethod MergeMethod `yaml:"expectedMergeMethod,omitempty" json:"expectedMergeMethod,omitempty"`
}
