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

**Authority gate remains pending:** This proposed ADR does not establish that
`refs/notes/oiax/notifications/v1/<graph-key>` with an explicit-lease push is
permitted by Constitution XI. Before implementing the writer, T001 must record
the exact operation and maintainer-reviewed basis for conformity with the current
restriction. If it conflicts, redesign the mechanism or address governance in a
separate explicit change; this ADR cannot grant a namespace exception. Neither
repository write permissions nor append-only ancestry resolves that authority
question. No accepted ADR or constitution text is changed by this proposal.

Propose a reserved `refs/notes/oiax/notifications/v1/<graph-key>` ref containing
bounded ledger snapshots. Ref writes require the exact observed old object ID;
each new commit has that tip as its parent. Conflicts reread/reduce instead of
overwriting. Subject to that gate, the proposed mechanism uses explicit-lease Git push and must enforce
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
