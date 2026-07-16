package engine

// Equivalence names the rung of the equivalence ladder that settled
// whether source content is represented in a destination. Detection is
// by content, not only by commit ancestry, so it survives squash and
// rebase merges that rewrite SHAs.
type Equivalence string

const (
	// EquivalenceReachability: commits in `rev-list to..from` decided the
	// edge. Exact when promotion edges merge with merge commits.
	EquivalenceReachability Equivalence = "reachability"
	// EquivalencePatchIdentity: stable patch-ids (`git patch-id --stable`)
	// matched candidate commits to the destination. Handles rebase merges
	// and 1:1 commit rewrites.
	EquivalencePatchIdentity Equivalence = "patch-identity"
	// EquivalenceHeadTree: the source and destination trees are equal.
	// Handles squash merges at the moment of merge.
	EquivalenceHeadTree Equivalence = "head-tree"
	// EquivalenceBaseline: the promotion baseline (the sourceHead recorded
	// on a merged managed request) settled the edge. Handles squash merges
	// after the source has advanced.
	EquivalenceBaseline Equivalence = "baseline"
)

// BackflowExclusionReason names the rung of the backflow exclusion ladder
// that resolved a downstream-only commit as already returned (or intentionally
// withheld), so it needs no backflow. Values are part of the plan JSON
// contract (additive within planFormatVersion 1).
type BackflowExclusionReason string

const (
	// BackflowExcludedSkip: the commit carries the 'Oiax-Backflow: skip'
	// trailer — the author declared it intentionally not backflowed.
	BackflowExcludedSkip BackflowExclusionReason = "skip"
	// BackflowExcludedProvenance: a backflow-target commit's
	// 'git cherry-pick -x' provenance line names this commit — it was
	// returned by identity even if conflict resolution rewrote its patch-id.
	BackflowExcludedProvenance BackflowExclusionReason = "provenance"
	// BackflowExcludedPatchID: the commit's stable patch-id is already
	// present on the backflow target — it was returned by content.
	BackflowExcludedPatchID BackflowExclusionReason = "patch-id"
)

// BackflowExclusion records one downstream-only commit the backflow
// exclusion ladder resolved as not needing return, and why.
type BackflowExclusion struct {
	SHA     string                  `json:"sha"`
	Subject string                  `json:"subject"`
	Reason  BackflowExclusionReason `json:"reason"`
}

// Commit is one observed commit.
type Commit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
}

// BranchState is the observed state of one long-lived branch.
type BranchState struct {
	Name string `json:"name"`
	Head string `json:"head"`
}

// RequestType distinguishes the two kinds of managed change requests.
type RequestType string

const (
	RequestTypePromotion RequestType = "promotion"
	RequestTypeBackflow  RequestType = "backflow"
)

// ChangeRequest is the engine's provider-neutral view of a managed
// change request (a pull request, on forges that call it that). Managed
// requests are identified by machine-readable metadata and branch
// relationship, never by title.
type ChangeRequest struct {
	ID     string      `json:"id"`
	Type   RequestType `json:"type"`
	Source string      `json:"source"`
	Target string      `json:"target"`
	// SourceHead is the source head SHA the request currently proposes —
	// the promotion baseline once the request merges.
	SourceHead string `json:"sourceHead"`
}

// EdgeState is the observed state of one promotion edge, after the
// equivalence ladder has been applied.
type EdgeState struct {
	From BranchState `json:"from"`
	To   BranchState `json:"to"`
	// Unpromoted holds source commits not represented in the destination
	// after the equivalence ladder — not raw rev-list output.
	Unpromoted []Commit `json:"unpromoted"`
	// Equivalence is the ladder rung that settled the edge.
	Equivalence Equivalence `json:"equivalence"`
	// DownstreamOnly holds destination content absent from the source.
	DownstreamOnly []Commit `json:"downstreamOnly"`
	// ToReturn holds the downstream-only commits that must be backflowed to
	// the backflow target: DownstreamOnly minus everything already returned
	// (matched by patch identity or by an explicit already-returned SHA,
	// which includes cherry-pick -x provenance and the O6 'Oiax-Backflow:
	// skip' trailer). It is meaningful only when To is a backflow source;
	// nil when nothing remains to return.
	ToReturn []Commit `json:"toReturn,omitempty"`
	// Excluded holds the downstream-only commits the backflow exclusion
	// ladder resolved as already returned or intentionally withheld, each
	// with the reason that excluded it. Order follows DownstreamOnly (newest
	// first). Populated only when To is a backflow source; nil when nothing
	// was excluded.
	Excluded []BackflowExclusion `json:"excluded,omitempty"`
	// SourceHeadShort is the short SHA of the backflow source head (the
	// downstream head, i.e. To.Head abbreviated). It is the trailing segment
	// of the deterministic backflow branch name and is populated by the
	// reconcile layer; empty for edges whose destination is not a backflow
	// source.
	SourceHeadShort string `json:"sourceHeadShort,omitempty"`
	// ManagedRequest is the existing managed promotion request for this
	// edge, if any.
	ManagedRequest *ChangeRequest `json:"managedRequest,omitempty"`
	// Mergeable is the forge-reported mergeability of the managed or
	// prospective request. It is advisory: forges compute mergeability
	// asynchronously and may report unknown (nil). The planner treats
	// unknown as "proceed and let the request surface the conflict."
	Mergeable *bool `json:"mergeable,omitempty"`
}

// ActionType enumerates everything a plan can do.
type ActionType string

const (
	ActionCreatePromotionRequest ActionType = "createPromotionRequest"
	ActionCreateBackflowRequest  ActionType = "createBackflowRequest"
	ActionUpdateManagedRequest   ActionType = "updateManagedRequest"
	ActionCloseObsoleteRequest   ActionType = "closeObsoleteRequest"
	ActionReportDivergence       ActionType = "reportDivergence"
	// ActionNoOp is reserved: part of the frozen type enum (see
	// docs/reference/plan-format.md) so consumers must accept it as a
	// no-effect action, but BuildPlan never emits it today.
	ActionNoOp ActionType = "noOp"
)

// Action is one planned step, with enough context to explain itself.
type Action struct {
	Type ActionType `json:"type"`
	From string     `json:"from"`
	To   string     `json:"to"`
	// Unpromoted counts the commits the action moves (promotion) or
	// returns (backflow).
	Unpromoted int `json:"unpromoted,omitempty"`
	// Equivalence records which ladder rung produced the decision.
	Equivalence Equivalence `json:"equivalence,omitempty"`
	// Request identifies the managed request an update or close acts on.
	Request *ChangeRequest `json:"request,omitempty"`
	// Branch is the deterministic backflow branch name a create-backflow
	// action pushes to (oiax/backflow/<source>-to-<target>/<shortSHA>);
	// empty for promotion actions.
	Branch string `json:"branch,omitempty"`
	// Reason is a human-readable explanation of why the action exists.
	Reason string `json:"reason"`
}

// PlanFormatVersion is the JSON plan compatibility version.
const PlanFormatVersion = 1

// EdgeSummary is the per-edge diagnostic record a plan carries for every
// promotion edge — including edges fully in sync, which produce no action.
// It answers "which equivalence rung settled this edge" and carries the
// counts the observability surfaces render.
type EdgeSummary struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Equivalence is the ladder rung that settled the edge.
	Equivalence Equivalence `json:"equivalence"`
	// InSync is true when no unpromoted commits survived the ladder.
	InSync bool `json:"inSync"`
	// Unpromoted counts the source commits not represented in the
	// destination after the ladder. Absent when zero.
	Unpromoted int `json:"unpromoted,omitempty"`
	// DownstreamOnly counts destination content absent from the source.
	// Absent when zero.
	DownstreamOnly int `json:"downstreamOnly,omitempty"`
	// ToReturn counts the downstream-only commits still to backflow.
	// Meaningful only when To is a backflow source; absent when zero.
	ToReturn int `json:"toReturn,omitempty"`
	// Excluded lists the downstream-only commits the backflow exclusion
	// ladder resolved as not needing return, with reasons. Absent when
	// nothing was excluded.
	Excluded []BackflowExclusion `json:"excluded,omitempty"`
}

// Plan is the ordered set of actions required to converge the graph.
type Plan struct {
	PlanFormatVersion int      `json:"planFormatVersion"`
	Graph             string   `json:"graph"`
	Actions           []Action `json:"actions"`
	// Edges summarizes every evaluated promotion edge, in graph declaration
	// order — diagnostics only, additive within planFormatVersion 1. Absent
	// (never null) when no edges were evaluated.
	Edges []EdgeSummary `json:"edges,omitempty"`
}
