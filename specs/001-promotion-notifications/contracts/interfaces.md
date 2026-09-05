# Proposed provider, state, and CLI contracts

These are interfaces to implement and verify, not existing exported APIs.

## Lifecycle observation

Add an internal `LifecycleReader` capability with `RepositoryIdentity`,
`ListLifecyclePage(query) -> page`, and `GetLifecycleRequest(id)` operations.
Every operation takes `context.Context` first. A page returns validated managed
requests, an opaque cursor, scanned interval, and completeness; partial results
cannot be silently treated as complete. Transport errors are separate from
"not managed" or "not found". All matching uses existing ownership rules; inspect
full Azure descriptions/properties where the list truncates them.

Queries cover all managed request types and states. The baseline API remains
unchanged for the branch engine. Notification observation has its own bounds:

- GitHub new-request discovery uses `state=all`, `sort=created`, `direction=asc`,
  overlapping the last processed page with stable-ID deduplication. Already
  known open requests are polled by ID. Bootstrap inventories existing open
  requests; a complete all-request pass covers requests created and merged
  during downtime, including ones absent from the open list.
- Azure discovery uses frozen creation-time intervals and `status=all`; merge
  catch-up also enumerates completed requests in frozen closed-time intervals.
  Use inclusive interval overlap, stable-ID deduplication, and full detail reads
  before admission. No undocumented order is assumed from `$skip` pagination.
  Split dense intervals until a complete partition fits the page budget.
- Do not use creation time to filter merge eligibility: an old request may have
  merged after activation. Do not reuse either provider's 180-day baseline
  cutoff or GitHub's updated-order/last-merged-date heuristic.
- A scan hit by a page/time limit persists progress but leaves its interval
  incomplete; it resumes without advancing the verified watermark. A dense
  same-timestamp Azure interval that cannot be proven complete suspends that
  scan with `discovery-incomplete`, retains known records, and reports the
  limiting bound. Repeated stable enumeration and ID-set verification are
  required for Azure partitions; unexpected movement never establishes completeness.
- Pending delivery processing is independent of scan completeness. Registered
  requests cannot be forgotten just because a new list omits them. Deletion or
  denied detail reads warn and retain unknown status, never fabricate a merge.

Evidence: [GitHub list controls](https://docs.github.com/en/rest/pulls/pulls#list-pull-requests),
[Azure time windows](https://learn.microsoft.com/en-us/rest/api/azure/devops/git/pull-requests/get-pull-requests?view=azure-devops-rest-7.1).

Change internal request creation to return a `CreateOutcome` with request and
`created|adopted` disposition, and accept optional `NotificationOriginV1`.
Include origin in the initial body before POST. Additional labels/properties may
fail after creation: later discovery must recover the request and its event.
Never edit an adopted request to invent a missing origin. Existing callers and
forge conformance fakes must be migrated together; `engine.ChangeRequest` remains
the branch engine's small view.

Add `GetCommitSnapshot(request, eventRevision)` to the lifecycle capability.
Use bounded PR-specific detail/commit reads. Capture source/base OIDs at creation
in origin, and use completed PR revision evidence at merge. Azure exposes
last-merge source/target OIDs; GitHub's merge-result OID is distinct from source
commits. Return up to 100 entries with count-known/truncated/unavailable flags.
Never call a page count the total. Provider fixtures must prove snapshot behavior
after source advancement/ref deletion, or use immutable-OID reconstruction.
Retries reuse persisted facts. Commit enrichment failure must not lose the event.

## Notification-origin wire format

The provider writes a second, non-templatable block outside the v1 ownership marker:

```html
<!-- oiax-notification-origin:{"version":1,"operationID":"opaque-operation-id","graph":"environments","configOID":"full-object-id","observedAt":"2026-09-04T18:00:00Z","logicalSource":"development","logicalTarget":"test"} -->
```

The origin also contains `sourceOID` and `baseOID` for the creation snapshot
(omitted in this abbreviated example). JSON-escape `<`, `>`, and `&` to prevent comment termination; bound block bytes
at 4 KiB. Reject duplicate origin blocks and malformed fields. Never use origin
alone to grant ownership. Azure may mirror it in `oiax.notificationOrigin.v1`
properties after creation, but the original full body remains the crash-recovery
source. Existing v1 ownership-marker parse/replace behavior must stay byte-compatible.

## Ledger store

**Namespace authority (C1/T001 resolved):** The maintainer explicitly extended
ownership to `refs/notes/oiax/`; [ADR 0015](../../../docs/adr/0015-oiax-owned-notes-namespace.md)
and Constitution XI v2.0.0 record the change. Notification writes remain limited
to the exact ref grammar below, expected-tip comparisons and append-only commits,
with no deletion or rewind. [T001 evidence](../checklists/implementation-validation.md)
records the decision, not completed implementation, live-write permission or
blanket acceptance of the broader proposed ledger contract.

`LedgerStore.Read(ctx)` returns a validated snapshot and revision OID, or a
distinct absent state. `Commit(ctx, expectedOID, transition)` creates a child
snapshot and atomically updates the exact reserved notes ref. Return conflict
distinctly from permission/transport/validation failure. Consumers reread on
conflict and apply transitions to current state. After five conflicts per
transition, defer and warn rather than spin.

The reserved ref is `refs/notes/oiax/notifications/v1/<64-hex-graph-key>`.
The Git implementation validates that exact grammar, full object IDs, commit
parentage, and ref format. Use an explicit expected OID (empty for absent-ref
creation); never an implicit lease based on a remote-tracking branch. Credentials
are supplied using the existing providers' environment-based Git authentication,
never URL arguments or logs. No generic arbitrary-ref-write capability is exposed.

Observation may fetch objects into an isolated cache but performs no remote write.
Never checkout the notes ref in the user's worktree. Reads must not execute Git
hooks or config-defined content. Writes occur only within reconciliation. An
unreadable/malformed/newer-version ledger blocks notification sends, not core
reconciliation. A missing previously-known ledger is an explicit recovery warning;
if no local evidence distinguishes deletion from first enablement, initialize a
new current cutoff and report that historical notification continuity is unknown.
Never silently replay old events after state loss.

Per-destination leases and per-event claims are 120 seconds, with a 10-second
send deadline. CAS conflicts, expired ownership, and stage cancellation prevent
new sends. Recheck ownership/config revision/generation immediately before dispatch; persist
results through monotone reduction. An accepted response with a failed receipt
write is uncertain, not confirmed delivery. Recovery semantics are in
[data-model.md](../data-model.md#delivery-and-claims).

Policy transitions persist the accepted config OID/digest and subscription changes
atomically. The coordinator verifies ancestry outside the pure reducer and
recomputes that evidence after conflict; the store must never replay a stale
policy replacement unconditionally. Same-OID agreement is idempotent, strict
descendants advance, ancestors defer, and divergent/unknown ancestry fails closed
for notification work. See [revision ordering](../data-model.md#configuration-revision-ordering).
Claims/admissions must match the accepted config OID; late attempt receipts can
still be recorded monotonically without restoring the sender's older policy.

With no eligible enabled destinations, bypass this capability entirely, including
revision updates and retirement writes. Tests must cover an all-disabled run
between two enabled runs with the same destination identity: the durable epoch
and pending backlog remain unchanged. Also test a per-destination disable recorded
while another destination remains enabled: re-enable starts a new epoch.

## Delivery payload handoff

The renderer returns `RenderedMessageV1 {title, body}`. The coordinator sanitizes
and persists that value before claiming a send, then constructs immutable
`DeliveryPayloadV1 {schemaVersion: 1, event: EventV1, message: RenderedMessageV1}`
from durable values. The delivery capability accepts that payload plus a separate
resolved endpoint and context, and returns `AttemptResult`. It cannot call the
renderer or store. Contract tests supply two different persisted messages for
one event and assert each adapter uses the right one without mutating the shared
event, then repeat after a template edit to verify stored-message reuse.

## CLI and plan preview

Keep existing branch `actions`, `edges`, `planFormatVersion: 1`, and detailed exit
calculation intact. A renderer-owned document may add an optional object:

```json
{
  "notifications": {
    "schemaVersion": 1,
    "observation": "complete",
    "items": [
      {"eventId":"opaque-stable-id","destination":"operations-teams","event":"request-merged","requestType":"promotion","requestId":"123","decision":"pending","reason":"not-yet-delivered"}
    ]
  }
}
```

This is a fragment within the existing plan document, not a second stdout JSON
document. Observation is `complete|incomplete|unavailable|uninitialized`.
Items for planned-but-not-created requests omit event/request IDs and report
`conditional-on-create`. Durable-delivered, filtered, retry-not-due, and
subscription-not-active decisions are distinct safe reason codes. Ordering is
stable. No endpoint values, environment values, volatile attempt IDs, or raw
HTTP errors appear. Capture time is an explicit observation input, not a hidden
clock inside the selector. CLI text and CI summaries carry equivalent meaning.
Preview also distinguishes `stale-config-revision`, `config-revision-unordered`,
and `policy-revision-mismatch`; it does not advance the accepted revision.

`plan --detailed-exitcode` still depends only on core branch actions/divergence;
pending notifications alone do not produce exit 2. `reconcile` preserves the
core result/error even if the notification stage fails. Collect create outcomes
incrementally, and run bounded notification finalization on partial core failure
when the context remains usable. A cancelled context does not start new sends.
Do not mask a core exit 1 or 3 with delivery success or failure.

Read-only preview cannot prove secret availability or destination readiness. It
reports eligibility based on observed forge/ledger state without reading endpoint
environment variables. With no enabled destinations, omit the object and bypass
notification capabilities entirely.
