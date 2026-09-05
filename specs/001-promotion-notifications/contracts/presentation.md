# Proposed notification presentation contract

Users can replace human-facing titles and bodies, including environment-oriented
wording and commit lists. Event facts, routing, ownership, stable IDs, and delivery
receipts remain system-controlled. This addresses FR-023–026.

## Configuration and precedence

`spec.notifications.environmentNames` maps existing branch names to display
labels. An omitted label falls back to the branch name. Labels do not assert
observed deployment state: a merge establishes branch promotion only.

`spec.notifications.templates` supplies optional `title`, `body`, or `bodyFile`.
Each destination can override these slots with its own `templates`. Inline body
and bodyFile are mutually exclusive; a destination body override replaces the
whole graph-wide body slot. Otherwise inherit by slot, then use built-in defaults.
Files use the same pinned config commit and bounded repository-relative loading
as existing PR templates, never the triggering branch or local absolute paths.

Built-in wording distinguishes creation/merge and promotion/backflow. A custom
merge body can read:

```gotemplate
{{if eq .RequestType "backflow"}}These commits were returned to {{.DestinationEnvironment}} by backflow:
{{else}}These commits were promoted to the {{.DestinationEnvironment}} environment:
{{end}}{{range .Commits}}- {{.ShortSHA}} {{.Subject}}
{{end}}
```

Use an `.Event` conditional for creation subscriptions: describe them as ready
for review, not already promoted. Templates render plain text, not entire payloads.

## Closed context

| Field | Meaning |
|---|---|
| `.Event` | `request-created` or `request-merged` |
| `.RequestType` | `promotion` or `backflow` |
| `.Repository`, `.Graph` | Safe repository display name and graph name |
| `.RequestID`, `.RequestURL`, `.EventID` | Event/request identity |
| `.SourceBranch`, `.DestinationBranch` | Actual PR head/base refs |
| `.LogicalSourceBranch`, `.LogicalDestinationBranch` | Logical edge if known, otherwise empty |
| `.SourceEnvironment`, `.DestinationEnvironment` | Logical edge label when known; otherwise actual branch fallback |
| `.OccurredAt`, `.ObservedAt` | Captured event times |
| `.Commits` | At most 100 event-specific `{SHA, ShortSHA, Subject, URL}` summaries |
| `.CommitCount`, `.CommitCountKnown` | Total if authoritative, otherwise zero/false |
| `.CommitsTruncated`, `.CommitsUnavailable` | Explicit completeness flags |

Unknown fields fail validation. Reuse the existing curated deterministic template
functions where applicable. No environment, current time, randomness, file access,
network, process execution, or dynamic inclusion is exposed. Parse/sample-render
all four event/request combinations at config validation without secrets/network.
Runtime rendering errors affect notification delivery only.

Template files have the existing 1 MiB file cap. Titles are reduced to a single
inert line capped at 256 runes; bodies are capped at 12 KiB, with overflow reported
as a delivery problem. The final adapter envelope remains bounded at 24 KiB.
Subjects are capped at 200 runes with an explicit truncation indicator.

## Immutable facts and retries

At merge use the PR's merged revision; at creation use captured head/base/iteration.
OID values observed before POST are hints until verified against the forge's
creation revision: a branch can advance during creation. Prefer the immutable
first PR iteration where available; if exact creation membership cannot be
established, mark commits unavailable instead of asserting the pre-POST list.
Never substitute current branch history. If an endpoint exposes a mutable view,
reconstruct from captured Git objects or mark details unavailable. Deleted refs,
squash, rebase, and source advancement are mandatory fixtures. PR source SHAs
are not necessarily destination SHAs after squash/rebase; the merge result is a
separate fact. The PR link remains the authoritative review record.

Commit facts and environment labels are fixed in the first admitted event.
Persist each destination's rendered title/body before first dispatch and reuse
it for retries. Template changes affect not-yet-rendered deliveries; they do not
replay success or rewrite an ambiguous attempted payload. Endpoint credential
rotation does not alter event/presentation facts.

## Delivery encoding

Teams uses inert TextRuns; Slack uses `plain_text`. Generic JSON adds
`message: {title, body}` and structured bounded commits/completeness fields.
Templates cannot replace that envelope or change required facts.

A standard footer or structured identity section always includes repository,
graph, event type, request type, actual source and destination branches, event ID,
the explicit forge request identifier, PR link, and the original observed-at
timestamp (RFC3339 UTC). This is the complete FR-008 field set on **every**
transport, including Teams and Slack; a link is not a substitute for the explicit
request identifier. Partial or unavailable commit information is identified
independently of the custom body. Even constant or empty custom text cannot
remove these fields. Tests must assert all fields with templates omitting every
context field, both on first delivery and after retry.
Sanitize untrusted fields and redact any resolved credentials before output/storage.

Rendering returns `RenderedMessageV1 {title, body}`. After persisting it, the
coordinator passes `DeliveryPayloadV1 {schemaVersion, event, message}` to the
adapter; see [delivery](delivery.md). Adapters do not execute templates or obtain
current policy. They encode the supplied message and append fixed facts from the
supplied event without modifying either value.
