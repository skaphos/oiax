# Notification data model

Design for [spec.md](spec.md). Foundation types, transitions and notes storage
are implemented locally through T015; lifecycle observation and outbound delivery
remain pending. See [implementation evidence](checklists/implementation-validation.md).

## Configuration

`NotificationPolicy` contains `destinations`. `NotificationDestination` contains
`name`, `type`, `endpointEnv`, `enabled`, `events`, `requestTypes`, and
`allowPrivateNetwork`. Public configuration types belong in `pkg/api/v1`;
[the configuration contract](contracts/configuration.md) owns validation/defaults.
Policy also carries an `environmentNames` map and graph-wide/per-destination
presentation templates, described in [presentation](contracts/presentation.md).
No endpoint URL, credential, HTTP client, or environment value enters the pure model.

## Repository and managed request

`RepositoryIdentity` combines the normalized provider host and immutable
repository identifier (GitHub repository ID; Azure organization/project/repository
IDs). Display names and request URLs are presentation, not event keys. Rename of
a repository must not generate different event IDs.

`LifecycleRequest` contains repository identity, graph, immutable request ID,
request type, actual head/base refs, optional logical edge, URL, created/merged
timestamps, state (`open`, `merged`, `closed-unmerged`), and optional origin.
It is separate from `engine.ChangeRequest`. Ownership is verified by the forge
before constructing this value. Deletion of a backflow ref does not invalidate
the preserved request's identity. Missing provenance never authorizes guessing
an edge from a branch-name pattern.

`NotificationOriginV1` is written in the initial create body, separate from the
existing ownership marker. Fields: `version`, `operationID`, `graph`,
`configOID`, `observedAt`, `logicalSource`, `logicalTarget`, and the creation
source/base snapshot OIDs. It is immutable
through baseline updates and adoption. An origin is used only after validating
existing request ownership and consistency with the current graph. It is evidence
of the original creation operation, not another ownership mechanism. No actor
display name or title participates in identity.

## Event

`EventV1` has `id`, `kind`, `repository`, `graph`, `request`, `occurredAt`, and
`observedAt`. Request data includes actual refs, optional logical edge, ID, type,
and URL. `kind` is `request-created` or `request-merged`.

The event ID is `sha256` of a canonical, length-delimited encoding of
`(schema=1, repository identity, request ID, kind)`. Do not hash string
concatenation without delimiters. Destination name, current head SHA, title,
configuration version, and observation time are excluded. A create operation
nonce is provenance, not event identity. The first successful ledger admission
fixes the envelope and `observedAt`; competing runs adopt that envelope.
It also captures source/base OIDs, optional merge-result OID, environment labels,
at most 100 commit summaries, and count-known/truncated/unavailable flags. Source
SHAs describe the reviewed request, not unchanged destination SHAs after squash.

Creation needs valid origin and a forge-confirmed request creation time. Merging
needs authoritative merged state and time, not closed status alone or branch
equivalence. The current pinned graph must still include the logical edge;
orphaned edges are reported as skipped. For a legacy backflow PR with no logical
source evidence, send the actual head/base refs, identify it as backflow, and mark
logical edge unavailable. Never fabricate the real downstream branch.

## Ledger snapshot

`LedgerV1` contains `version`, immutable repository/graph identity, `anchorOID`,
`policyRevision`, destination states, events, known requests, scan progress, and
delivery records. `PolicyRevisionV1` contains the last accepted `configOID` and
the digest of the normalized non-secret notification policy, including template
sources loaded at that OID. It is distinct from the fixed initialization anchor.
The anchor is the config commit captured at first initialization; a standard
Git note on this object contains the JSON. Each snapshot is one commit with the
previous tip as its only parent. Key ordering is canonical. Snapshot reads cap
bytes before JSON parsing, reject duplicate keys/unknown versions, and validate
identity and all cross-references.

Persist no raw config blobs, destination URLs, environment values, arbitrary
remote error bodies, or complete PR descriptions. Whitelisted diagnostic codes
and safe destination names are sufficient for recovery. Event content is
sanitized and checked against resolved secret values before persistence/output.
Metadata written by an authorized repository writer is within the repository's
existing trust boundary; a notes record alone cannot make an unmanaged PR eligible.

## Destination state and routing lifetime

### Configuration revision ordering

Every invocation keeps its resolved pinned config OID. Before changing policy,
the impure coordinator supplies verified commit-ancestry evidence for that OID
and the ledger's accepted config OID; the pure reducer never runs Git itself.
Use the following rules inside the same expected-tip transition as the policy
update, and recompute evidence after each CAS conflict:

| Incoming revision versus accepted revision | Allowed behavior |
|---|---|
| No ledger | Initialize policy and cutoff atomically, only with enabled destinations |
| Same OID and policy digest | Keep current generations and cutoffs |
| Same OID, different digest | Defer with `policy-revision-mismatch`; never replace state |
| Strict descendant with verified ancestry | Advance `policyRevision` and apply subscription changes atomically |
| Strict ancestor | Defer with `stale-config-revision`; never recreate a retired generation |
| Divergent history or ancestry cannot be proven | Defer with `config-revision-unordered`; never reset state or choose by timestamp |

Missing ancestry may be fetched within notification budgets; if still unknown,
defer. A config content revert in a new descendant commit is a valid change; an
old OID supplied with `--config-ref` is not a notification-policy rollback.
Recover divergent history by using a reviewed configuration commit that descends
from the recorded revision, not by deleting notes or automatically resetting
epochs. Identical policy at a newer commit still advances the recorded OID but
does not reset subscription cutoffs. Newly computed event/subscription admission
and every claim require the invocation OID to match the accepted revision.
An older worker may record a result for its already dispatched attempt through
monotone reduction, but cannot change policy, admit events, or start another send.
CAS ordering alone is not configuration ordering. These checks apply across
different `--config-ref` spellings through their resolved OIDs.

### Subscription generations and all-disabled behavior

The identity is `(graph-key, destination name)`. Fields: transport identity
fingerprint, active generation, activation timestamp, normalized subscriptions,
delivery lease, and next allowed send time. The fingerprint includes `type` and
`endpointEnv`, not the secret value. Rotating the same environment variable is
credential maintenance and retains receipts. Operators changing the actual
recipient should use a new destination name, because Oiax cannot infer recipient
identity from a changed secret.

On first initialization, capture UTC and atomically persist an activation cutoff;
no eligible events are older than that cutoff. Unrelated config edits retain it.
Renaming, changing transport identity, or re-enabling a **durably recorded**
disabled destination starts a new generation with a new cutoff. Removed/disabled
subscriptions stop attempts in the current invocation. When another eligible
destination remains enabled, record those retirements and mark affected pending
deliveries skipped in the accepted policy transition. Adding an event/request-type
subscription has its own activation time, so enabling creation later does not
replay old creations.

If all destinations are disabled (including empty selections), bypass all
notification I/O: do not read/write the ledger, advance `policyRevision`, record
retirements, or resolve endpoint variables. Therefore a later enabled invocation
compares against the last **durable** policy, not an imagined disabled snapshot.
Unchanged identities/subscriptions retain their generations and can resume pending
deliveries and discover events from the disabled interval after the existing
cutoff. A new destination name starts fresh. This applies even if the disabled
configuration was run, not only when toggled entirely between runs. It also means
an already running old worker cannot learn a global disable from the ledger;
operators needing an immediate stop must stop those workers/revoke destination
access outside Oiax. There is no claim of cross-worker immediate cancellation.

Before dispatch, evaluate the invocation's pinned config and current ledger
revision/generation again. A worker superseded by a newer recorded revision stops
new sends even if its destination fingerprint has not changed.
In-flight requests cannot be recalled. Pending records for retired generations
become skipped with a reason; they are not rerouted to a new identity.

## Delivery and claims

`DeliveryPayloadV1` is an immutable value containing `schemaVersion: 1`, the
admitted `EventV1`, and the destination's persisted `RenderedMessageV1`
(`title`, `body`). It contains no endpoint, secret, template source, or mutable
policy. The renderer produces the message; the coordinator persists it on the
delivery record and joins it to the admitted event before calling an adapter.
Adapters receive this value directly, never retrieve the ledger or re-render.
Event facts remain shared and unchanged across destinations; presentation does
not move into or overwrite the event. Fixed identity/completeness sections are
derived independently from the payload's event. Retries reuse the same persisted
message, including after an ambiguous send or subsequent template edit.

`DeliveryRecord` key is `(event ID, destination identity, activation generation)`.
Fields: status, attempt count, next attempt time, owner/attempt ID, lease expiry,
accepted time, durable success time, safe diagnostic code, and the rendered
title/body fixed before first dispatch. A destination
lease additionally prevents two workers from concurrently sending different
events to the same destination. It is renewed before dispatch; a per-event
claim records exactly which send may have reached the network.

| Current state | Observation/action | Next state |
|---|---|---|
| absent | eligible event admitted by expected-tip write | pending |
| pending/retryable | due; destination lease and event claim acquired | claimed |
| claimed | endpoint accepts and success receipt persists | delivered |
| claimed | failed attempt and result persists | retryable |
| claimed | process lost or lease expires | uncertain, then retryable |
| any nonterminal | subscription retired | skipped |
| delivered | repeat observation or stale failure | delivered, no send |
| skipped | old generation observed again | skipped, no send |

Never report `delivered` until the success receipt is durable. A response accepted
without a stored receipt is `accepted-receipt-uncertain`. A stale failure cannot
overwrite success. A late acceptance may record terminal success for the same
event/generation even after claim expiry, but cannot undo a retry already sent.
No lease can fence Teams or Slack; these are the documented ambiguity cases.

Run limits and retry spacing are in [research](research.md#4-retry-policy-and-bounded-work).
Pending events do not expire automatically. Reserve ledger space for claim/result
transitions at admission; reaching capacity must not knowingly prevent recording
an already admitted send. Terminal receipts are never silently removed.

## Scan progress

Store provider-specific continuation data behind a versioned opaque cursor,
covered interval boundaries, known request IDs, and `complete` status. Creation
and merge scans are distinct. Known open requests are refreshed by ID; failures
retain them as unknown rather than falsely closed. Pending deliveries no longer
depend on re-listing their PR. Do not advance an interval watermark until all
pages and required detail reads are complete and committed with the admitted
events. See [interfaces](contracts/interfaces.md#lifecycle-observation).

## Time and scale

Use an injected UTC clock captured once per observation/transition; no clock
reads or random IDs inside pure selection. Require reasonably synchronized CI
clocks and test skew at activation/lease boundaries. Negative elapsed time delays
retry; it never shortens a lease or makes a pre-activation event eligible.
Provider timestamps are authoritative for lifecycle occurrence. HTTP deadlines
and stage budgets are independent of event age. Planning sorts by occurrence
time, event ID, and destination name for repeatable output.
