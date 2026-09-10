# 0019 — Recover an unreachable notification revision by audited override

- Status: accepted (maintainer decision; repository publication remains PR-based)
- Date: 2026-09-10
- Decider: Oiax repository maintainer
- Scope: notification configuration-order recovery and its ledger compatibility

## Context

ADR 0014 requires notification policy to advance only to a verified descendant
of the ledger's accepted configuration commit. A force-push, history rewrite or
garbage collection can make that commit unreachable. Git can then prove neither
ancestry direction, every ordinary run defers, and the normal remedy of committing
a reviewed descendant is impossible. Deleting or replacing the notes ref would
also delete the event and delivery evidence that suppresses duplicate sends.

Local object absence is not proof that a remote deleted an object. Shallow or
narrow checkouts routinely omit reachable commits, and even a structurally
complete fetch configuration may simply be stale. Recovery therefore needs an
explicit authority boundary, an auditable result, and an honest rollout contract.

## Options considered

- Automatically accept the current pinned revision when the old OID is absent:
  repairs unattended runs, but mistakes incomplete or stale local state for
  evidence of remote deletion exactly when the ordering guard matters most.
- Delete or replace the ledger: restores progress, but destroys deduplication and
  delivery evidence and can replay externally visible notifications.
- Require an explicit audited override: retains the ledger and makes the loss of
  ancestry proof a human decision, at the cost of operator work and a one-way
  state-format extension.

## Decision

Choose an explicit audited override. `oiax notifications reset
--accept-revision <oid>` may replace an unreachable accepted revision only when
the named OID equals the revision resolved from the pinned configuration for that
invocation. The exact already-accepted OID and digest is an idempotent no-op.
Otherwise it refuses when the accepted OID resolves, when no ledger exists,
when notifications are disabled, or when the same OID has a different policy
digest. Scheduled reconciliation cannot manufacture override evidence.

The checkout guard establishes only that the local repository is not *known* to
be systematically incomplete. It rejects shallow repositories. For a configured
`origin`, it also requires at least one positive fetch refspec whose source is
exactly `refs/heads/*`, rejects any negative refspec, and rejects a missing fetch
mapping; narrow head patterns and tag-only mappings are insufficient. A repository
without a configured `origin` is rejected. The accepted partial-clone filters are
limited to the commit-complete `blob:none`, `blob:limit=<n>` and `tree:<depth>`
forms; unknown, combined, malformed or commit-omitting filters fail closed.
Passing this structural guard is not proof that the local refs are fresh or that
a remote deleted the commit. The operator must refresh the relevant refs and
confirm absence before authorizing the override.

Recovery retains schema version 1 and the existing
`refs/notes/oiax/notifications/v1/<graph-key>` ref. It adds an optional,
append-only `revisionOverrides` field naming the prior and accepted OIDs and the
recording time. The reset preserves immutable event facts and existing attempt
and receipt evidence, and performs no HTTP delivery. It also applies the newly
accepted policy through the ordinary policy transition: destination generations,
subscriptions and cutoffs may change, and nonterminal deliveries made ineligible
by that policy may become `subscription-retired`. Recovery therefore preserves
audit and deduplication evidence; it does not claim policy state is untouched.

This is a coordinated one-way v1 extension on the same terms as ADR 0018. Every
reader and writer for a graph must be upgraded before the first reset. Once an
override record exists, older binaries must fail closed rather than interpret the
ledger as an unbroken ancestry chain. There is no automatic downgrade, field
deletion, notes rewrite or migration to another ref.

## Consequences

- A stranded ledger can progress without deleting event, attempt or receipt
  evidence, and the broken ancestry chain remains permanently visible.
- Recovery requires a human to review the accepted policy and refresh refs; a
  structurally acceptable checkout can still be stale.
- The override cannot prove whether the abandoned revision was older than the
  accepted one. The operator accepts that uncertainty rather than claiming
  ordering was preserved.
- Applying current policy can establish fresh cutoffs or retire pending work;
  reset is not a byte-for-byte preservation of subscription or delivery state.
- All participants for a graph need a coordinated upgrade, and a supporting
  binary must remain available after an override is recorded.
- The append-only audit is bounded to 32 overrides and still shares the ledger's
  8 MiB limit. If either limit prevents the audit record, reset refuses without
  changing state; v1 has no destructive capacity-recovery path.

## Links

- Extends [ADR 0014](0014-notification-delivery-ledger.md) without modifying its
  history-ordering design.
- Extends [ADR 0018](0018-notification-terminal-outcome-rollout.md) with another
  additive, one-way schema-v1 ledger field.
- Retains the namespace authority and append-only rules in
  [ADR 0015](0015-oiax-owned-notes-namespace.md).
- [Issue 95](https://github.com/skaphos/oiax/issues/95) and
  [PR 103](https://github.com/skaphos/oiax/pull/103).
- [Notification recovery guide](../guides/notifications.md#recovering-an-unreachable-configuration-revision).
