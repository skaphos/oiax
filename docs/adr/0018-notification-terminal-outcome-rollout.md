# 0018 — Roll out notification terminal outcomes as a one-way v1 state extension

- Status: accepted (maintainer decision; repository publication remains PR-based)
- Date: 2026-09-10
- Decider: Oiax repository maintainer
- Scope: notification ledger state grammar and rollout; no change to notes-namespace authority

## Context

ADR 0014 established an append-only notification ledger, and ADR 0015 fixed
its authority at `refs/notes/oiax/notifications/v1/<graph-key>`. The ledger
continues to use `SchemaVersion: 1`, but its delivery state now needs to record
two durable terminal outcomes: `skipped/abandoned` after deterministic failures
reach the retry threshold, and `skipped/payload-too-large` when the first
recorded receipt proves that the payload cannot be accepted. These are distinct
facts from subscription retirement and from retryable transport failures.

The codec and reducer already preserve saved messages and attempt IDs,
permit late acceptance for a proven attempt, and accept existing v1 records
where payload-too-large was retryable. The new skipped outcomes can represent
that no HTTP exchange occurred, so `lastStatus` may remain absent (zero). This
is an independent reader incompatibility, not a consequence subsumed by
[PR #92](https://github.com/skaphos/oiax/pull/92). The forcing case and rollout
review are tracked in [issue 94](https://github.com/skaphos/oiax/issues/94) and
[PR 101](https://github.com/skaphos/oiax/pull/101).

Keeping old readers pointed at the same ref would let them treat an unfamiliar
ledger state as invalid or otherwise fail closed. Bumping the schema number in
the same ref would provide a clear version boundary, but still breaks old
readers and requires an explicit migration. Moving the ref risks split ledgers
and replay or deduplication loss. Reusing a retired state would encode false
semantics, while continuing an infinite deterministic retry does not solve
bounded ledger capacity.

## Options considered

- Bump the schema version in the existing notes ref: makes incompatibility
  explicit, but still breaks old readers and requires an explicit migration.
- Move the ledger to a new notes namespace: separates readers, but risks split
  ledgers and replay or deduplication loss.
- Reuse an existing terminal state or keep retrying indefinitely: preserves
  superficial reader compatibility, but loses the distinction between policy
  retirement and delivery outcomes or leaves deterministic failures consuming
  capacity forever.
- Retain v1 and coordinate a one-way rollout of the additive state grammar:
  preserves the established ledger identity and old records while requiring
  all participants for a graph to upgrade together.

## Decision

Choose the coordinated one-way rollout. Retain `SchemaVersion: 1` and the
authoritative `refs/notes/oiax/notifications/v1/<graph-key>` namespace. The
record layout and ledger identity are unchanged, and old records need no
transformation; the new reader validates both existing records and the
additive states. Older readers fail closed either way, so a same-ref schema
bump would not remove the need for coordinated rollout. Expand the
allowed v1 delivery-state grammar additively with `skipped/abandoned` and
`skipped/payload-too-large`; this is a protocol compatibility change even
though the numeric schema remains 1. Older readers are explicitly rejected as
unsupported once these states may appear; they must not reinterpret them as
retryable, retired, or an absent ledger.

Before either state can be written, all writers and readers operating on that
graph must be upgraded together. A supporting binary remains deployed for as
long as the state can exist in the ledger. There is no automatic downgrade,
migration, or receipt pruning. Before a downgrade, notification configuration
may be disabled so the older binary can operate without notifications; disabling
does not restore readability of a ledger containing the newer states and must
not be presented as a ledger migration.

The durable terminality rule is intentionally bounded but not universal:
payload-too-large becomes terminal on its first persisted result, regardless of
earlier faults; other deterministic failures become abandoned when a durably
recorded deterministic result is reached with a total claimed attempt count of
at least 24. If a result is transient or cannot be persisted, attempts may
exceed those figures. The ledger retains unknown recovery evidence and accepts
a late success for a proven attempt; terminal state does not delete history or
permit replay.

## Consequences

- Existing v1 ledgers and legacy retryable payload-too-large records remain
  readable, while new terminal records prevent deterministic retry loops and
  preserve their distinct meaning.
- Rollout coordination is required per graph, and old binaries cannot safely
  remain writers or readers after the first new state is published.
- A downgrade can require disabling notifications and leaves the newer ledger
  unreadable to old software; operators must retain a supporting binary or
  perform an explicitly designed future migration.
- Append-only attempt evidence and late-acceptance handling retain auditability
  and recovery possibilities, but transient persistence failures can exceed the
  nominal attempt thresholds and do not provide a universal delivery bound.
- The decision extends ADR 0014's migration/compatibility contract without
  changing ADR 0015's namespace authority or altering older ADR records.

## Links

- Extends [ADR 0014](0014-notification-delivery-ledger.md) and its ledger
  compatibility decision.
- Retains the namespace authority in [ADR 0015](0015-oiax-owned-notes-namespace.md).
- [PR 101](https://github.com/skaphos/oiax/pull/101) and [issue 94](https://github.com/skaphos/oiax/issues/94).
- [Notification ledger codec](../../internal/notification/store/codec.go) and
  [notification reducer](../../internal/notification/reducer.go).
