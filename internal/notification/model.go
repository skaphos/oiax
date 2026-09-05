// Package notification contains deterministic notification facts and state
// transitions. Clocks, ancestry, lifecycle observations and operation IDs are
// explicit inputs; Git, forge, environment and network effects live elsewhere.
package notification

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"maps"
	"slices"
	"strings"
	"time"

	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

const (
	SchemaVersion  = 1
	MaxLedgerBytes = 8 << 20
	MaxDeliveries  = 50000
	MaxCommits     = 100
	ClaimDuration  = 120 * time.Second
	// Reserve worst-case serialized claim/result metadata for every pending
	// delivery, including a bounded operation ID and timestamps, before sending.
	ResultReserveBytes = 2048
)

// RepositoryIdentity uses immutable IDs; Name is display-only.
type RepositoryIdentity struct {
	Provider       string `json:"provider"`
	Host           string `json:"host"`
	ID             string `json:"id"`
	OrganizationID string `json:"organizationId,omitempty"`
	ProjectID      string `json:"projectId,omitempty"`
	Name           string `json:"name"`
}

func (r RepositoryIdentity) identityParts() []string {
	return []string{r.Provider, strings.ToLower(strings.TrimSuffix(r.Host, ".")), r.OrganizationID, r.ProjectID, r.ID}
}

func (r RepositoryIdentity) Same(other RepositoryIdentity) bool {
	return slices.Equal(r.identityParts(), other.identityParts())
}

// Digest encodes every part with an eight-byte length prefix before SHA-256.
func Digest(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		_, _ = h.Write(length[:])
		_, _ = h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func EventID(repo RepositoryIdentity, request string, kind v1.NotificationEvent) string {
	parts := append([]string{"1"}, repo.identityParts()...)
	return "sha256:" + Digest(append(parts, request, string(kind))...)
}

func GraphKey(repo RepositoryIdentity, graph string) string {
	return Digest(append(append([]string{"notification-graph-v1"}, repo.identityParts()...), graph)...)
}

func DeliveryKey(event, destination, generation string) string {
	return Digest("delivery-v1", event, destination, generation)
}

type RequestV1 struct {
	ID                 string                     `json:"id"`
	Type               v1.NotificationRequestType `json:"type"`
	Source             string                     `json:"source"`
	Destination        string                     `json:"destination"`
	LogicalSource      string                     `json:"logicalSource,omitempty"`
	LogicalDestination string                     `json:"logicalDestination,omitempty"`
	URL                string                     `json:"url"`
}

type LifecycleState string

const (
	LifecycleOpen   LifecycleState = "open"
	LifecycleMerged LifecycleState = "merged"
	LifecycleClosed LifecycleState = "closed-unmerged"
)

// LifecycleRequest is constructed only after the provider verifies ownership.
type LifecycleRequest struct {
	Repository     RepositoryIdentity    `json:"repository"`
	Graph          string                `json:"graph"`
	Request        RequestV1             `json:"request"`
	State          LifecycleState        `json:"state"`
	CreatedAt      time.Time             `json:"createdAt"`
	MergedAt       time.Time             `json:"mergedAt,omitempty"`
	SourceOID      string                `json:"sourceOID,omitempty"`
	BaseOID        string                `json:"baseOID,omitempty"`
	MergeResultOID string                `json:"mergeResultOID,omitempty"`
	Origin         *NotificationOriginV1 `json:"origin,omitempty"`
}

type NotificationOriginV1 struct {
	Version       int       `json:"version"`
	OperationID   string    `json:"operationID"`
	Graph         string    `json:"graph"`
	ConfigOID     string    `json:"configOID"`
	ObservedAt    time.Time `json:"observedAt"`
	LogicalSource string    `json:"logicalSource"`
	LogicalTarget string    `json:"logicalTarget"`
	SourceOID     string    `json:"sourceOID"`
	BaseOID       string    `json:"baseOID"`
}

type CommitSummary struct {
	SHA      string `json:"sha"`
	ShortSHA string `json:"shortSha"`
	Subject  string `json:"subject"`
	URL      string `json:"url,omitempty"`
}

type CommitSnapshot struct {
	SourceOID          string          `json:"sourceOID,omitempty"`
	BaseOID            string          `json:"baseOID,omitempty"`
	MergeResultOID     string          `json:"mergeResultOID,omitempty"`
	Commits            []CommitSummary `json:"commits"`
	CommitCount        int             `json:"commitCount"`
	CommitCountKnown   bool            `json:"commitCountKnown"`
	CommitsTruncated   bool            `json:"commitsTruncated"`
	CommitsUnavailable bool            `json:"commitsUnavailable"`
}

type EventV1 struct {
	ID                     string               `json:"id"`
	Kind                   v1.NotificationEvent `json:"kind"`
	Repository             RepositoryIdentity   `json:"repository"`
	Graph                  string               `json:"graph"`
	Request                RequestV1            `json:"request"`
	OccurredAt             time.Time            `json:"occurredAt"`
	ObservedAt             time.Time            `json:"observedAt"`
	SourceEnvironment      string               `json:"sourceEnvironment"`
	DestinationEnvironment string               `json:"destinationEnvironment"`
	Snapshot               CommitSnapshot       `json:"snapshot"`
}

type PolicyRevisionV1 struct {
	ConfigOID    string `json:"configOID"`
	PolicyDigest string `json:"policyDigest"`
}

type RenderedMessageV1 struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}
type DeliveryPayloadV1 struct {
	SchemaVersion int               `json:"schemaVersion"`
	Event         EventV1           `json:"event"`
	Message       RenderedMessageV1 `json:"message"`
}

type Lease struct {
	AttemptID string    `json:"attemptID"`
	Until     time.Time `json:"until"`
}
type Subscription struct {
	Event       v1.NotificationEvent       `json:"event"`
	RequestType v1.NotificationRequestType `json:"requestType"`
	Cutoff      time.Time                  `json:"cutoff"`
}
type DestinationState struct {
	Name          string                  `json:"name"`
	Fingerprint   string                  `json:"fingerprint"`
	Generation    string                  `json:"generation"`
	Active        bool                    `json:"active"`
	ActivatedAt   time.Time               `json:"activatedAt"`
	Subscriptions map[string]Subscription `json:"subscriptions"`
	Lease         Lease                   `json:"lease"`
	NextSendAt    time.Time               `json:"nextSendAt"`
}

type DeliveryStatus string

const (
	StatusPending   DeliveryStatus = "pending"
	StatusClaimed   DeliveryStatus = "claimed"
	StatusRetryable DeliveryStatus = "retryable"
	StatusDelivered DeliveryStatus = "delivered"
	StatusSkipped   DeliveryStatus = "skipped"
)

type OutcomeCode string

const (
	OutcomeAccepted         OutcomeCode = "accepted"
	OutcomeNetwork          OutcomeCode = "network-failure"
	OutcomeRateLimited      OutcomeCode = "rate-limited"
	OutcomeService          OutcomeCode = "service-failure"
	OutcomeConfiguration    OutcomeCode = "configuration-failure"
	OutcomeMissingSecret    OutcomeCode = "missing-secret"
	OutcomeInvalidEndpoint  OutcomeCode = "invalid-endpoint"
	OutcomePayloadTooLarge  OutcomeCode = "payload-too-large"
	OutcomeRedirect         OutcomeCode = "redirect-rejected"
	OutcomeResponseTooLarge OutcomeCode = "response-too-large"
	OutcomeCanceled         OutcomeCode = "canceled"
	OutcomeRetired          OutcomeCode = "subscription-retired"
)

type AttemptResult struct {
	Code       OutcomeCode
	RetryAfter time.Duration
}

type DeliveryRecord struct {
	EventID       string         `json:"eventID"`
	Destination   string         `json:"destination"`
	Generation    string         `json:"generation"`
	Status        DeliveryStatus `json:"status"`
	Attempts      int            `json:"attempts"`
	NextAttemptAt time.Time      `json:"nextAttemptAt"`
	Lease         Lease          `json:"lease"`
	// AttemptIDs retain evidence for late accepted responses. They are bounded
	// by the ledger byte budget and never silently pruned.
	AttemptIDs  []string           `json:"attemptIDs,omitempty"`
	AcceptedAt  time.Time          `json:"acceptedAt"`
	DeliveredAt time.Time          `json:"deliveredAt"`
	Code        OutcomeCode        `json:"code,omitempty"`
	Message     *RenderedMessageV1 `json:"message,omitempty"`
}

type ScanProgress struct {
	Version  int       `json:"version"`
	Cursor   string    `json:"cursor"`
	From     time.Time `json:"from"`
	Through  time.Time `json:"through"`
	Complete bool      `json:"complete"`
}

type LedgerV1 struct {
	Version        int                         `json:"version"`
	Repository     RepositoryIdentity          `json:"repository"`
	Graph          string                      `json:"graph"`
	AnchorOID      string                      `json:"anchorOID"`
	PolicyRevision PolicyRevisionV1            `json:"policyRevision"`
	Destinations   map[string]DestinationState `json:"destinations"`
	Events         map[string]EventV1          `json:"events"`
	KnownRequests  map[string]LifecycleRequest `json:"knownRequests"`
	Scans          map[string]ScanProgress     `json:"scans"`
	Deliveries     map[string]DeliveryRecord   `json:"deliveries"`
}

func NewLedger(repo RepositoryIdentity, graph, anchor string) *LedgerV1 {
	return &LedgerV1{Version: SchemaVersion, Repository: repo, Graph: graph, AnchorOID: anchor,
		Destinations: map[string]DestinationState{}, Events: map[string]EventV1{}, KnownRequests: map[string]LifecycleRequest{}, Scans: map[string]ScanProgress{}, Deliveries: map[string]DeliveryRecord{}}
}

// Clone owns all mutable collections and pointers, preventing failed or racing
// transitions from changing an input snapshot or an already persisted payload.
func (l *LedgerV1) Clone() *LedgerV1 {
	out := *l
	out.Destinations = maps.Clone(l.Destinations)
	for k, d := range out.Destinations {
		d.Subscriptions = maps.Clone(d.Subscriptions)
		out.Destinations[k] = d
	}
	out.Events = maps.Clone(l.Events)
	for k, e := range out.Events {
		e.Snapshot.Commits = slices.Clone(e.Snapshot.Commits)
		out.Events[k] = e
	}
	out.KnownRequests = maps.Clone(l.KnownRequests)
	for k, r := range out.KnownRequests {
		if r.Origin != nil {
			origin := *r.Origin
			r.Origin = &origin
			out.KnownRequests[k] = r
		}
	}
	out.Scans = maps.Clone(l.Scans)
	out.Deliveries = maps.Clone(l.Deliveries)
	for k, d := range out.Deliveries {
		d.AttemptIDs = slices.Clone(d.AttemptIDs)
		if d.Message != nil {
			m := *d.Message
			d.Message = &m
		}
		out.Deliveries[k] = d
	}
	return &out
}

func ValidDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func ValidOID(s string) bool {
	if len(s) == 40 {
		return ValidDigest(s + strings.Repeat("0", 24))
	}
	return ValidDigest(s)
}

func ValidOutcome(code OutcomeCode) bool {
	switch code {
	case OutcomeAccepted, OutcomeNetwork, OutcomeRateLimited, OutcomeService, OutcomeConfiguration, OutcomeMissingSecret, OutcomeInvalidEndpoint, OutcomePayloadTooLarge, OutcomeRedirect, OutcomeResponseTooLarge, OutcomeCanceled, OutcomeRetired:
		return true
	default:
		return false
	}
}
