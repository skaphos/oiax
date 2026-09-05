# 0013 — Add an optional notification contract

- Status: proposed
- Date: 2026-09-04
- Deciders: Oiax maintainers (proposal awaiting repository review)

## Context

[#75](https://github.com/skaphos/oiax/issues/75) requires notifications for
managed request merges and optionally creation. Accepted clarifications require
per-destination event/request filters, explicit environment-variable references,
and delivery failures that preserve reconciliation exit statuses. Email is deferred.

`pkg/api/v1/types.go` has no notification policy. `loadGraph` currently discards
configuration outside topology/templates. The JSON plan and exit-code rules are
compatibility contracts, and notification work must not change their existing
branch-promotion meaning.

## Options considered

- Environment-only subscriptions: avoid a config extension but lose pinned,
  reviewable desired notification behavior.
- Notification actions in the branch engine: reuse its action list, but couple
  delivery failures and pending messages to branch-promotion exit semantics.
- Optional pinned policy and informational plan preview: add an explicit surface
  with independent delivery outcomes.

## Decision

Propose optional `spec.notifications`, validated/defaulted in the public v1 API,
with named destinations, transport type, endpoint environment-variable reference,
enabled flag, event subscriptions, and request-type subscriptions. The initial
transports are Teams Workflows, Slack incoming webhooks, and generic webhooks.

The user also requires environment-oriented presentation and commit summaries.
Add environment labels for existing branches and graph-wide/per-destination
title/body templates. Reuse the pinned-file and pure template conventions in
ADR 0011 with a separate event context. Customization controls human text only;
fixed event facts and the complete FR-008 identity footer, including explicit
request ID and observed-at time, remain system-owned. Event-time commit snapshots
prevent later source advances from changing a notification's meaning. Each
destination's persisted rendered message is joined to those facts in a delivery
payload; adapters cannot re-render against newer configuration.

Fully disabled invocations perform no notification I/O and do not record a
retirement. Re-enable of unchanged identities resumes the last durable cutoffs
and backlog; a new destination name requests a fresh cutoff. Retirement/re-enable
epochs apply only to disable transitions recorded while another destination keeps
notification processing active. This preserves the zero-network contract without
claiming that an unrecorded global disable cancels other running workers.

Expose an optional `notifications` preview in plan JSON v1 at the renderer, not
as branch-engine actions. Absent policy preserves old serialized output. Existing
exit codes retain their meaning, including notification-only pending work. Runtime
delivery errors produce safe diagnostics and later retries. Structural config
errors still fail normal validation.

No existing field or version is removed or deprecated. New consumers must tolerate
additive preview fields; older binaries still reject unknown configuration fields,
so binary upgrades precede deploying notification config. Rollback removes the
optional config first. A future removal requires the normal deprecation window
and major-version policy.

## Consequences

- Notification intent stays reviewable and pinned; secrets remain runtime values.
- API consumers receive one canonical validation/defaulting implementation.
- A second preview surface needs explicit tests and documentation so users do
  not mistake pending messages for branch changes or confirmed deliveries.
- Disabled policy is cheap; enabled policy adds observation work and operational
  limits even when the branch graph is converged.

## Links

- Extends [0003](0003-pinned-configuration-ref.md),
  [0005](0005-config-api-v1.md), [0010](0010-exported-config-validation.md),
  and [0011](0011-templatable-request-text.md).
- [Proposed contracts](../../specs/001-promotion-notifications/contracts/configuration.md).
- [Delivery state decision](0014-notification-delivery-ledger.md).
