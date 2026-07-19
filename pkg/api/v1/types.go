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
	// Templates optionally customizes the human-facing text Oiax authors
	// on the requests and artifacts it manages. Templates are Go
	// text/template documents rendered with a documented variable context
	// and a curated function set only — configuration stays declarative
	// data, never executable code. Unset slots use Oiax's built-in text.
	Templates *Templates `yaml:"templates,omitempty" json:"templates,omitempty"`
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
	// Templates optionally overrides spec.templates.promotion for
	// promotion requests created on this edge only — a prod-facing edge
	// can carry a heavier change-record scaffold than a dev edge.
	Templates *RequestTemplate `yaml:"templates,omitempty" json:"templates,omitempty"`
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

// Templates customizes the human-facing text Oiax authors: managed-request
// titles and bodies, the durable backflow-conflict artifact, and — for the
// merge backflow strategy — the --no-ff merge-commit message. Each slot is
// optional; an unset slot renders Oiax's built-in text.
//
// Oiax authors only the HUMAN text: the machine-readable marker block is
// appended by the provider after the rendered body and is never
// templatable. A template whose output could be mistaken for (or swallow)
// the marker is rejected. See docs/reference/templates.md for the variable
// context and the constraints templates must respect.
type Templates struct {
	// Promotion is the graph-wide template for promotion requests.
	// spec.promotions[].templates overrides it per edge.
	Promotion *RequestTemplate `yaml:"promotion,omitempty" json:"promotion,omitempty"`
	// Backflow is the template for managed backflow requests.
	Backflow *RequestTemplate `yaml:"backflow,omitempty" json:"backflow,omitempty"`
	// BackflowConflict is the template for the durable backflow-conflict
	// artifact (a forge issue or work item).
	BackflowConflict *RequestTemplate `yaml:"backflowConflict,omitempty" json:"backflowConflict,omitempty"`
	// BackflowMergeMessage is the template for the --no-ff merge-commit
	// message the merge backflow strategy authors. It requires
	// spec.backflow.strategy "merge". Unset keeps git's default merge
	// message. The rendered message is deterministic for fixed inputs, so
	// re-runs still push identical SHAs; changing the template (or the
	// inputs) changes the replayed merge commit's SHA, which re-pushes the
	// managed branch once — bounded, self-healing churn.
	BackflowMergeMessage *TextTemplate `yaml:"backflowMergeMessage,omitempty" json:"backflowMergeMessage,omitempty"`
}

// RequestTemplate customizes one managed request's (or conflict
// artifact's) title and body. Title is always inline (titles are a single
// line); the body is inline (body) or read from a repository file
// (bodyFile) — the two are mutually exclusive. A referenced file is read
// from the same pinned source as the configuration itself (ADR 0003): the
// pinned config ref for plan/reconcile, the working tree for
// validate/graph.
type RequestTemplate struct {
	// Title is an inline text/template for the request title. Rendered
	// output is reduced to a single line and length-capped.
	Title string `yaml:"title,omitempty" json:"title,omitempty"`
	// Body is an inline text/template for the human body above the
	// marker block. Mutually exclusive with BodyFile.
	Body string `yaml:"body,omitempty" json:"body,omitempty"`
	// BodyFile is a repository-relative path (forward slashes, no "..",
	// not absolute) to a text/template file for the body. Mutually
	// exclusive with Body.
	BodyFile string `yaml:"bodyFile,omitempty" json:"bodyFile,omitempty"`
}

// TextTemplate is a single-text template slot: inline (text) or a
// repository file reference (file), mutually exclusive.
type TextTemplate struct {
	// Text is the inline text/template.
	Text string `yaml:"text,omitempty" json:"text,omitempty"`
	// File is a repository-relative path (forward slashes, no "..", not
	// absolute) to a text/template file. Mutually exclusive with Text.
	File string `yaml:"file,omitempty" json:"file,omitempty"`
}
