# 0014 — Record notification delivery state in Git notes

- Status: proposed
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

### Recovery when the accepted revision no longer exists

The ordering rule above assumes the accepted commit stays resolvable. It does
not: a force-push, a branch rewrite or a GC of the configuration branch removes
it, `merge-base --is-ancestor` then errors, and every run defers permanently
([#95](https://github.com/skaphos/oiax/issues/95)). The rule's own remedy —
commit a reviewed descendant — cannot be performed, because there is nothing
left to descend from, and this ADR forbids the other exit: deleting the notes ref
would destroy the deduplication evidence.

Two recoveries were considered.

**Rejected — self-healing acceptance.** Treat an accepted OID that does not
resolve locally as `RevisionUnknown` and accept the incoming revision anyway,
provided it is on the pinned configuration ref. It requires no operator and no
CLI surface, but it fails on evidence. The runtime observes only its own object
database: nothing here can distinguish "the remote no longer has this commit"
from "this checkout never fetched it", and the second is routine — Oiax's primary
host is a CI runner whose checkout is frequently shallow, filtered or
single-branch. The guard offered (the incoming OID is on the pinned ref) is
vacuous: by ADR 0003 the incoming OID is *always* resolved from the pinned ref,
so the condition reduces to local absence alone. Worse, the events that strand
the accepted commit — rewriting the configuration branch — are precisely the ones
the ordering rule defends against, so an automatic override would relax the check
exactly when a rewritten-backwards configuration could restore retired
subscriptions unnoticed. An unattended, silent weakening of the guarantee is not
an acceptable price for convenience.

**Decision — explicit, audited recovery.** Add `oiax notifications reset
--accept-revision <oid>`, which writes a new accepted revision on operator
authority. Local absence still decides nothing on its own; a human does, and the
command bounds the false positive rather than ignoring it. It refuses while the
accepted commit resolves (ordinary ordering governs an ordinary ledger); it
refuses from any object database known to be scoped to part of origin, since a
commit missing from such a checkout is not evidence of a commit missing from
origin; and `--accept-revision` must equal the commit this invocation resolved,
so the authorization cannot be pinned once into a workflow and keep applying as
configuration moves. The advance carries a distinct evidence value that no
automatic verifier produces, so a scheduled run can never reach it.

The scoping test is behavioural, not nominal. A shallow clone truncates history;
a single-branch checkout configures a non-wildcard fetch refspec and therefore
lacks whole branches while reporting itself *not* shallow — the common CI
default, and the case a shallowness test alone would wave through. A partial
(promisor) clone is deliberately allowed: `--filter=blob:none` and
`--filter=tree:0` retain complete commit reachability and lazily fetch filtered
objects, so the false positive cannot arise there and refusing it would only
break the partial-clone setup recommended for large repositories.

The recovery is non-destructive: events, deliveries and receipts are carried
across unchanged, so nothing is re-sent. It appends an immutable
`revisionOverrides` record naming the abandoned and accepted commits, keeping the
break in the ordering chain permanently visible. Deferred runs also gain a
distinct `config-revision-unreachable` diagnostic; it narrows, and still wraps,
the unordered error, so the fail-closed behaviour is unchanged and only the
advice differs.

Remote HTTP delivery and Git receipt persistence cannot form a transaction.
Successful persisted receipts suppress repeat sends. Interrupted sends, failed
receipt writes, and lease recovery remain ambiguous and can duplicate external
messages. The same event ID is preserved; no exactly-once claim is made. A
worker that loses its claim cannot knowingly begin another send.

## Consequences

- Fresh clones recover state from Git, without a daemon or private database.
- The design adds explicit notes fetch/write permissions and an audit history
  that grows with delivery activity. Capacity limits suspend notification work
  with a diagnostic; v1 has no automatic receipt deletion.
- Notes writes can trigger broad repository push automation; deployment guidance
  must scope workflows appropriately. Permission failures do not block core work.
- State destruction cannot be silently repaired without losing deduplication
  evidence; recovery must explain the possible gap rather than backfill blindly.
- A rewritten configuration history needs a human. The stuck ledger keeps
  deferring — safely, and with an actionable diagnostic — until an operator runs
  the reset, which is a real cost in attention that the rejected self-healing
  option would have avoided at the price of the guarantee.
- The reset cannot prove the abandoned revision was older than the one accepted
  in its place; that evidence died with the commit. The ledger therefore records
  that ordering was repaired by hand rather than claiming it was preserved.
- `revisionOverrides` extends the versioned ledger format, on the same terms
  already set by `lastStatus`: omitted until the event that records it occurs,
  so ordinary ledgers stay readable by earlier binaries. Here rejection is also
  the *correct* outcome — an earlier binary refuses the note as invalid state
  rather than reading a repaired ordering history as an unbroken one. Downgrades
  past this release must disable notifications first.
- The durable origin and ledger formats become versioned compatibility surfaces;
  incompatible future changes need a migration decision, not reinterpretation.

## Links

- Extends [0002](0002-content-based-divergence-detection.md) and
  [0013](0013-notification-configuration-contract.md); supersedes neither.
- [Git explicit leases](https://git-scm.com/docs/git-push),
  [GitHub refs including notes](https://docs.github.com/en/enterprise-cloud%40latest/rest/git/refs),
  [Azure note permissions and expected-old updates](https://learn.microsoft.com/en-us/rest/api/azure/devops/git/refs/update-refs?view=azure-devops-rest-7.1).
- [State and provider contract](../../specs/001-promotion-notifications/contracts/interfaces.md).
