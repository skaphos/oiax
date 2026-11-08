# 0021 — Notification clock source and mixed-version ledger tolerance

- Status: proposed
- Date: 2026-11-07
- Decider: Oiax repository maintainer
- Scope: activation-cutoff clock, lease skew assumption and delivery-record
  decoding; issue #97

## Context

[Issue 97](https://github.com/skaphos/oiax/issues/97) names two load-bearing
assumptions that are neither documented nor defended. This ADR adds a decision
on each. It extends ADR 0014 and ADR 0018 and rewrites neither; those records
are immutable. The guide half of the issue describes current behavior and does
not wait on this ADR.

**Clocks today.** Every ledger time is the runner's `time.Now().UTC()`
(`internal/reconcile/notification_runtime.go`), passed to the pure reducers as
an explicit input. Two kinds of comparison use it.

Runner against runner: a claim or renewal writes lease `Until` as now plus
`ClaimDuration` (120 s), and another runner treats the lease as free once its
own clock reaches `Until` (`internal/notification/reducer.go`, `schedule.go`).
A batch renews only when fewer than 60 s remain before a POST, each POST has a
10 s deadline, and receipts for the whole batch are written once after the last
POST, within 90 s (`internal/reconcile/notification_delivery.go`). By its own
clock the holder therefore has roughly 50 s left at the end of any POST, and
records it already POSTed stay `claimed` until that receipt write.

Runner against forge: `AcceptPolicy` sets a subscription's `cutoff` to runner
now whenever it creates one. That covers first activation, a new destination
or changed fingerprint, a newly subscribed event or request type, and a revision
reset. Activation runs before core apply (`internal/cli/reconcile.go`).
`AdmitEvent` compares the forge's `created_at`/`merged_at` against that cutoff.
`EventAdmissionTime` (`internal/notification/creation.go`) uses the runner's
pre-POST origin time instead only when the forge reports a whole second and
the origin falls inside that same second. Scan bounds are also runner time
compared with forge timestamps (`internal/reconcile/notification_observe.go`,
`listedOutsideInterval` in `internal/forge/github/notifications.go`).

Failure modes, where Δ is how far a claimant's clock leads the lease holder's
and δ is how far a runner leads the forge:

- Δ ≥ 120 s: a freshly written lease already looks expired, so two runners can
  send at once. This is the case the issue describes.
- Δ above roughly 50 s: a claimant can take a destination mid-batch and resend
  records the holder POSTed but has not yet recorded, up to the batch limit of
  10 per destination. The holder stops at its next pre-send check
  (`CheckBatchSend`). Even at Δ = 0, a receipt write slower than the remaining
  margin lets the lease lapse first. ADR 0014 already accepts duplicates on
  lease recovery; skew moves that point Δ earlier. Δ < 0 only delays a runner.
- δ > 0 at a cutoff: a `request-created` event is dropped when the forge's
  timestamp precedes the runner-clock cutoff and the same-second rule does not
  apply. With GitHub's whole-second `created_at`, a lead of one second or more
  defeats that rule for a fast POST. Any of the run's own creates that reaches
  the forge within about δ of activation is then lost. A lead of under a second
  loses a create only when the runner's clock has rolled into the next second
  and the forge's has not. A forge timestamp with a fractional second never
  qualifies for the rule. The same cutoff governs `request-merged`. With δ > 0,
  merges within δ after a new cutoff are dropped. With δ < 0, merges up to |δ|
  before it are admitted. No run diagnostic is emitted for a dropped event. A
  later plan preview can report it as `before-subscription-cutoff`
  (`internal/reconcile/notification_render.go`).
- Runner behind a recorded scan bound: a runner can trail the `through` of the
  previous completed scan, or a cutoff set by a leading runner, by more than the
  time since. It then writes an interval that starts after it ends. `Validate`
  rejects that interval (`internal/notification/store/codec.go`), so
  observation fails as invalid state until its clock catches up. This comes
  from reading the code; no test reproduces it.

The specification requires "reasonably synchronized CI clocks"
(`specs/001-promotion-notifications/data-model.md`) without a bound, and the
notification guide says nothing about clocks.

The forge's clock is reachable. The identity call is GitHub
`GET /repos/{owner}/{repo}` and, for Azure DevOps, the Git repository plus
`_apis/connectionData`. It is memoized per process and first runs during plan
preview, before activation. Both providers' `do` helpers already return
response headers (`internal/forge/github/github.go`,
`internal/forge/azuredevops/client.go`), but the identity code discards them.
The only HTTP date header the code parses is `Retry-After`. RFC 9110
requires a `Date` header from an origin server that has a clock. A probe on
2026-11-07 found `date` on both `api.github.com` and `dev.azure.com`. The header
has one-second resolution, RFC 9110 does not specify how the fraction is
dropped, and an intermediary can rewrite it.

**Decoding today.** `Decode` calls `DisallowUnknownFields`
(`internal/notification/store/codec.go`), and `encoding/json` applies that to
every nested struct. An unknown key anywhere in the ledger, whether at the top
level or inside a delivery record, its lease or its message, rejects the whole
ledger as invalid state. `Store.Read` and every `Commit` attempt decode first
(`internal/notification/store/store.go`), so an older binary can neither read
nor write. Every commit re-encodes the full snapshot, and `validateAppend`
compares decoded values, never bytes. Released additions:

- `lastStatus` inside a delivery record: PR #92, release 2.0.2. Releases 2.0.0
  and 2.0.1 reject it. It is written and validated but no decision or
  diagnostic reads it.
- `skipped/abandoned` and `skipped/payload-too-large`: PR #101, release 2.0.2
  (ADR 0018).
- Top-level `revisionOverrides`: PR #103, release 2.1.0 (ADR 0019).

Batching shipped in 2.0.2 together with `lastStatus`. Releases 2.0.0–2.0.1
send one record at a time: claim, renew, POST, record. If a newer binary writes
a ledger containing `lastStatus` between their renewal and their receipt, the
receipt commit fails, and that record is resent after its lease expires. That
is one duplicate per in-flight record per destination. The issue's mid-batch
case, where several POSTed records are claimed again, is what 2.0.2–2.1.1
batch runners would hit on the next additive record field. The guide already
says "do not run two binary versions against one graph" (added with PR #101).
It does not say why, or how to arrange it.

## Options considered

**Cutoff clock source.**

- Keep the runner clock and document a bound. This needs no code. The loss
  above scales with how far a runner leads the forge, so it is negligible on
  NTP-synchronized hosts and silent on hosts that drift.
- Use the forge clock from the identity response's `Date`. The offset is
  `Date` minus local receipt time, and forge time is runner now plus that
  offset. This puts the cutoff in the same clock domain as `created_at` and
  `merged_at`. The estimate is early by up to one second plus one-way latency,
  or up to half a second late if the server rounds. One second subtracted makes
  it no later than true forge time, so the run's own creates are admitted
  whatever the runner's skew. The cost is that events up to about two seconds
  plus latency before activation can be admitted. This option needs a fallback
  when `Date` is absent. Scan bounds must move with the cutoff. Otherwise a
  runner behind the forge starts its first scan after its end.
- Hybrid: use the forge clock when a usable `Date` was observed, and otherwise
  use the runner clock less a fixed tolerance, with a diagnostic. A larger
  tolerance loses fewer events and replays more history. Using only the fixed
  backdate needs no header, but it protects only up to its size.

**Lease expiry.**

- Compare leases in forge-referenced time. This bounds runner-to-runner skew by
  the estimate error, but it puts a header Oiax does not control inside the
  concurrency fence. Every participant would also have to switch together.
- Keep runner clocks and document a bound, using the measured forge offset as a
  skew check. If every runner is within X of the forge, any two runners are
  within 2X of each other.

**Unknown-field tolerance.**

- Keep strict decoding and require single-version operation. This needs no
  code. Every additive field becomes a stop-the-world upgrade, overlap still
  duplicates in-flight records, and Oiax cannot detect a second version.
- Accept unknown delivery-record members and drop them when re-writing. An older
  writer's first commit of any kind then erases them from every record, because
  each commit re-encodes the whole snapshot. `validateAppend` cannot notice,
  because it compares decoded values.
- Accept unknown delivery-record members and preserve them verbatim, while every
  other object stays strict.
- Accept unknown fields everywhere. An older binary would then read a ledger
  carrying `revisionOverrides` as an unbroken history (ADR 0019) and would
  ignore lease, message or subscription changes it cannot interpret.

## Decision

Propose the hybrid clock source, unchanged lease comparisons with a documented
bound, and preserving tolerance confined to delivery records. Each part can be
accepted or rejected on its own.

**1. Cutoff clock.** Capture `Date` from the identity response in the forge
layer and derive the forge offset. Pass forge-referenced time through the
existing `now` inputs for values compared with forge timestamps:

- subscription cutoffs, set at activation or reset;
- scan bounds;
- the plan preview's projection, so it reports what reconcile does.

Each cutoff is that time less one second. Leases, `nextAttemptAt`, `nextSendAt`,
receipt times and the origin's `observedAt` keep runner time. The same-second
rule stays as written and becomes redundant for the run's own creates. Without a
parseable `Date`, use runner now less one second and report the fallback.
Nothing new is persisted. Cutoffs stay RFC 3339 times in existing fields, and
`internal/notification` remains a pure consumer of the time it is given.

**2. Leases.** Do not change lease comparisons. The guide states that runners
sharing a graph must be NTP-synchronized and within a stated bound of the forge.
This ADR proposes 5 s, which keeps any two runners within 10 s, well inside the
roughly 50 s margin. Oiax warns when a measured offset exceeds that bound.
Optionally, Oiax also defers notification work for a run whose offset exceeds a
hard limit, proposed as 30 s. Above that limit neither the cutoff nor the fence
can be trusted, whichever clock is wrong.

**3. Delivery-record tolerance.** `Decode` accepts unknown members of a
delivery-record object only. Top-level ledger members, leases, messages,
events, known requests, destinations, subscriptions, scans and revision
overrides stay strict. Unknown members are rules for the reader, not new data:

- They are kept as raw JSON, carried through `Clone` and the reducers, and
  re-emitted on `Encode` in sorted key order.
- `validateAppend` treats them as part of the record.
- They are never logged, rendered, sent or used in a decision.
- They count toward the 8 MiB cap, and the duplicate-key, depth and UTF-8 checks
  still apply.

A future record field may be introduced as ignorable only if two conditions
hold:

- No older binary would make a different claim, retry, terminal or
  deduplication decision by knowing it.
- Its value stays true, or visibly stale, when an older writer changes the
  record's other fields. For example, a per-attempt value can name the attempt
  ID it describes.

`lastStatus` as shipped fails the second condition, because an older writer
recording a later attempt would leave the previous status in place. Any other
change is must-understand. It must be paired with something every reader
rejects: a new status or code value, as ADR 0018 did, or a top-level field, as
ADR 0019 did. Each change that adds a record field states its class.

**Rollout and downgrade.** These rules are restated from ADR 0018. They add to
that ADR and do not replace it:

- Strict binaries are every release before the first tolerant one, which today
  means 2.0.0 through 2.1.1. ADR 0018's coordinated one-way rollout governs
  unchanged any overlap that involves a strict binary, and any must-understand
  change.
- Tolerant binaries whose only difference is ignorable record fields may
  overlap during a rollout. The newer one may be downgraded to the older one,
  which preserves those fields.
- No downgrade goes below the release that introduced a recorded must-understand
  value, and disabling notifications does not restore readability (ADR 0018 and
  ADR 0019).
- Existing state gains nothing. `lastStatus` stays unreadable to 2.0.0–2.0.1,
  the ADR 0018 states to releases before 2.0.2, and `revisionOverrides` to
  releases before 2.1.0.
- Serialize runs for a graph across any upgrade, for example with a CI
  concurrency group that queues rather than cancels, so an in-flight older run
  finishes first.

**Compatibility surface.** The ledger remains the versioned surface ADR 0014
declared, with `SchemaVersion: 1` and the ADR 0015 ref unchanged. A ledger
without unknown members encodes byte-for-byte as it does today. This ADR adds
one reader rule: delivery records accept ignorable additions, and everything
else stays closed. The origin block keeps its strict parser
(`internal/forge/marker/notification_origin.go`). Exit codes, the plan format,
the configuration API and `pkg/api` do not change.

**Maintainer decisions:**

- the hybrid or the runner clock;
- the one-second margin and the documented bound;
- whether to add the hard limit;
- whether to adopt tolerance now or keep strict single-version operation.

## Consequences

- With the hybrid, the run's own creates no longer depend on runner-to-forge
  skew. Activation can admit up to about two seconds of earlier events, and the
  guide must say so. The cutoff depends on a header an intermediary could alter.
  The hard limit, if adopted, bounds that damage by failing closed.
- The clock change is ledger-compatible but mixes clock domains across binaries.
  An old runner that leads the forge can record a scan bound that makes a newer
  runner stall as described above, so single-version operation still applies to
  that rollout.
- Leases still rely on operator clock discipline. The new diagnostic makes skew
  visible, but it does not make concurrent runners safe beyond the stated bound.
- With tolerance, the next ignorable record field no longer forces a
  stop-the-world upgrade among tolerant binaries. Every field addition carries a
  classification burden, and the codec gains custom decode and encode paths
  that need fuzz and append-guard coverage.
- Without tolerance, every additive field repeats today's coordination, and
  overlap at an upgrade remains a duplicate risk bounded by the in-flight batch.
- Independently of this decision, clamping a new scan interval's start to its
  end would turn the skew stall into an empty interval. That is a separate fix.

## Links

- Extends [ADR 0014](0014-notification-delivery-ledger.md) and
  [ADR 0018](0018-notification-terminal-outcome-rollout.md) and supersedes
  neither. Retains [ADR 0015](0015-oiax-owned-notes-namespace.md) and the
  fail-closed `revisionOverrides` rule of
  [ADR 0019](0019-audited-notification-revision-recovery.md).
- [Issue 97](https://github.com/skaphos/oiax/issues/97); PRs
  [92](https://github.com/skaphos/oiax/pull/92),
  [101](https://github.com/skaphos/oiax/pull/101) and
  [103](https://github.com/skaphos/oiax/pull/103).
- [Ledger codec](../../internal/notification/store/codec.go),
  [store](../../internal/notification/store/store.go),
  [reducer](../../internal/notification/reducer.go),
  [admission](../../internal/notification/creation.go) and
  [batch delivery](../../internal/reconcile/notification_delivery.go).
- [RFC 9110 §6.6.1, Date](https://www.rfc-editor.org/rfc/rfc9110#section-6.6.1).
- [Notification guide](../guides/notifications.md#changing-destination-identity-and-rollback).
