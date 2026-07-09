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
	ActionNoOp                   ActionType = "noOp"
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
	// Reason is a human-readable explanation of why the action exists.
	Reason string `json:"reason"`
}

// PlanFormatVersion is the JSON plan compatibility version.
const PlanFormatVersion = 1

// Plan is the ordered set of actions required to converge the graph.
type Plan struct {
	PlanFormatVersion int      `json:"planFormatVersion"`
	Graph             string   `json:"graph"`
	Actions           []Action `json:"actions"`
}
