# 0014 — Record notification delivery state in Git notes

- Status: accepted (maintainer decision; repository publication remains PR-based); amended 2026-11-07 — see [Amendment](#amendment-2026-11-07)
- Date: 2026-09-04
- Deciders: Oiax maintainers (proposal awaiting repository review)

## Context

Notification workers run in disposable, potentially concurrent CI invocations.
[#75](https://github.com/skaphos/oiax/issues/75) requires retries and suppression
after confirmed success, while Constitution II/III prohibit a private state
database and XI prohibits creating long-lived branches. The current forge
request interface has neither durable delivery receipts nor atomic delivery claims.

## Options considered

- Local state or CI caches: low cost but not authoritative across runners.
- PR comments/properties: colocate receipts, but lack a shared portable atomic
  claim and can add visible comment noise.
- A dedicated branch or mutable tag: supports Git concurrency, but branches
  violate the stated boundary and tags interfere with release workflows.
- A notes ref with append-only commit ancestry and expected-tip updates: retains
  ordinary Git inspectability and a compare-and-swap boundary.

## Decision

**Namespace authority resolved:** The maintainer explicitly approved extending
ownership to `refs/notes/oiax/`, recorded separately in
[ADR 0015](0015-oiax-owned-notes-namespace.md) and Constitution XI v2.0.0.
[T001 evidence](../../specs/001-promotion-notifications/checklists/implementation-validation.md)
records the exact expected-tip, append-only operation. This resolves C1 without
inferring permission from an ADR proposal. This ADR remains proposed for its
broader state/delivery contract; implementation and platform verification remain.

Propose a reserved `refs/notes/oiax/notifications/v1/<graph-key>` ref containing
bounded ledger snapshots. Ref writes require the exact observed old object ID;
each new commit has that tip as its parent. Conflicts reread/reduce instead of
overwriting. The proposed mechanism uses explicit-lease Git push and must enforce
append-only ancestry and this exact namespace. It never pushes release tags or
creates a branch. Repository administrators grant notes-write permissions; Oiax
does not change permissions or settings itself.

Capture activation, stable event identity, pending delivery, claim, attempt result,
and terminal success in the ledger. Record immutable creation provenance in the
initial managed PR body using a separate versioned notification-origin block;
the existing v1 ownership marker remains unchanged. This makes a successful PR
POST recoverable even when the process dies before recording its result.

Persist the accepted configuration OID/digest separately from the fixed anchor.
Only verified descendant revisions advance policy; older or unorderable revisions
cannot restore retired subscriptions. CAS arrival order is not configuration
order. Fully disabled runs perform no ledger operation, so they cannot establish
new epochs or globally stop workers; unchanged identities resume the last durable
state on re-enable. These constraints trade automatic history-reset recovery for
safe, explicit configuration ordering without another state store.

Remote HTTP delivery and Git receipt persistence cannot form a transaction.
Successful persisted receipts suppress repeat sends. Interrupted sends, failed
receipt writes, and lease recovery remain ambiguous and can duplicate external
messages. The same event ID is preserved; no exactly-once claim is made. A
worker that loses its claim cannot knowingly begin another send.

## Amendment (2026-11-07)

Accepted after notifications shipped in 2.0.0 and passed adoption testing
([#86](https://github.com/skaphos/oiax/issues/86),
[#87](https://github.com/skaphos/oiax/issues/87),
[#88](https://github.com/skaphos/oiax/issues/88)). This acceptance replaces the
Decision's statement that the ADR remains proposed; the decision text above is
otherwise unchanged.

**The origin block's `headVerified` field is no longer evidence.**
[#89](https://github.com/skaphos/oiax/pull/89) added an optional `headVerified`
field to the notification-origin block as a trust hint for request-created
commit membership. [#104](https://github.com/skaphos/oiax/pull/104), fixing
[#96](https://github.com/skaphos/oiax/issues/96), reversed that. The parser
still accepts the field, so bodies written by the interim, unreleased build
decode, but it discards the value. Request-created commit membership now comes
from the forge. The origin block only confirms that the request is still the
one Oiax opened; the commits, their OIDs and any total come from the forge's own
view of the request. A request body that anyone with write access can edit is
never evidence. See
[`internal/forge/marker/notification_origin.go`](../../internal/forge/marker/notification_origin.go).

Later delivery decisions are recorded elsewhere:

- Finalization has a ten-minute budget, independent of the claim lease. See the
  [notifications guide](../guides/notifications.md#delivery-and-recovery) and
  [research §4](../../specs/001-promotion-notifications/research.md#4-retry-policy-and-bounded-work).
- A claim covers one destination's batch per run. Before each send, the worker
  rereads the ledger and confirms the batch still owns the destination and
  record at the run's configuration revision. See the
  [notifications guide](../guides/notifications.md#delivery-and-recovery).
- The ledger records the receiver's HTTP status as `lastStatus`. Once a newer
  binary records it, older binaries reject the ledger, and disabling
  notifications does not make it readable to them. See
  [ADR 0018](0018-notification-terminal-outcome-rollout.md) and the
  [notifications guide](../guides/notifications.md#changing-destination-identity-and-rollback).
- [ADR 0018](0018-notification-terminal-outcome-rollout.md) also adds the
  terminal `skipped/abandoned` and `skipped/payload-too-large` states.
  [ADR 0019](0019-audited-notification-revision-recovery.md) adds an audited
  override for an unreachable accepted configuration revision.

## Consequences

- Fresh clones recover state from Git, without a daemon or private database.
- The design adds explicit notes fetch/write permissions and an audit history
  that grows with delivery activity. Capacity limits suspend notification work
  with a diagnostic; v1 has no automatic receipt deletion.
- Notes writes can trigger broad repository push automation; deployment guidance
  must scope workflows appropriately. Permission failures do not block core work.
- State destruction cannot be silently repaired without losing deduplication
  evidence; recovery must explain the possible gap rather than backfill blindly.
- The durable origin and ledger formats become versioned compatibility surfaces;
  incompatible future changes need a migration decision, not reinterpretation.

## Links

- Extends [0002](0002-content-based-divergence-detection.md) and
  [0013](0013-notification-configuration-contract.md); supersedes neither.
- [Git explicit leases](https://git-scm.com/docs/git-push),
  [GitHub refs including notes](https://docs.github.com/en/enterprise-cloud%40latest/rest/git/refs),
  [Azure note permissions and expected-old updates](https://learn.microsoft.com/en-us/rest/api/azure/devops/git/refs/update-refs?view=azure-devops-rest-7.1).
- [State and provider contract](../../specs/001-promotion-notifications/contracts/interfaces.md).
