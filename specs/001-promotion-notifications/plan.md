# Implementation Plan: Managed Request Notifications

**Branch**: `spec/promotion-notifications` | **Date**: 2026-09-04 | **Spec**: [spec.md](spec.md)

**Input**: `specs/001-promotion-notifications/spec.md`

**Tracking**: [#75](https://github.com/skaphos/oiax/issues/75); email follow-up [#76](https://github.com/skaphos/oiax/issues/76).

**Status**: Design and task remediation drafted; implementation has not started. Constitution XI notes-write compliance remains PENDING (T001), so this is not an unconditional implementation-ready plan. ADRs are proposals for repository review. Setup reports the feature directory key `001-promotion-notifications`; the actual Git branch is shown above.

## Summary

Notify Teams Workflows, Slack incoming webhooks, and generic HTTPS receivers when a managed request merges, and optionally at creation. Both promotion and backflow are enabled by default, with per-destination opt-outs. Runtime endpoint environment variables are explicit. Delivery failures are reported and retried without changing core exit statuses.

Provide environment-oriented title/body templates, graph-wide defaults and per-destination overrides, and event-time commit summaries. For example, “These commits were promoted to the test environment.” This describes branch promotion, not observed deployment completion. Required identity and completeness remain available independently of the custom text.

Use a pure notification model/selector/renderer with separate impure coordination. Observe lifecycle facts through both forge providers, persist event/delivery records in a bounded Git notes ledger using expected-tip updates, and preserve creation origin in the initial PR body. Small stdlib HTTP adapters own delivery. An external send and Git receipt cannot be atomic; uncertain retries may duplicate messages.

## Technical Context

**Language/Version**: Go 1.27.1 from this checkout's `go.mod`.

**Primary Dependencies**: Existing Cobra, YAML v3, Go template facilities and system Git ≥2.45; stdlib HTTP, JSON, crypto, context, time. No new runtime SDK category.

**Storage**: Ordinary Git notes at `refs/notes/oiax/notifications/v1/<graph-key>` with append-only commit ancestry; forge PRs and a separate immutable notification-origin block. No private database.

**Testing**: Table-driven Go tests, `httptest`, bare Git repositories, fake clocks, provider conformance, parser fuzzing, and built-binary scenarios. Opt-in disposable GitHub/Azure repositories and Teams/Slack receivers verify platform behavior before release; this planning work sends no messages.

**Target Platform**: Existing Linux/macOS/Windows amd64/arm64 targets; CLI, GitHub Actions, Azure Pipelines. Invocation-driven, with no daemon or inbound listener.

**Project Type**: Existing CLI and public configuration library.

**Performance Goals**: SC-001 remains an end-to-end visibility target: 95% within 60 seconds and 99% within five minutes after the observing run completes. Normal-load acceptance uses ≤20 destinations, ≤10 due deliveries/destination and ≤100/run, no existing retry/backfill backlog, complete discovery within scan budget, and healthy endpoints. Predeclare at least 100 deliveries per transport across bounded runs; missing messages count as failures. HTTP acceptance alone does not prove visibility. Fixtures invoke reconciliation every minute; backlog/outage/incomplete-discovery recovery is verified separately for bounded progress, not these latency percentiles.

**Constraints**: 20 destinations; 100 total attempts and 10/destination/run; one-second pacing; 10-second HTTP deadline; 120-second aggregate notification stage and claims; 100 lifecycle pages/run; 8 MiB/50,000-delivery ledger cap. Pending events do not expire automatically. Commit summaries cap at 100 with explicit completeness. Capacity suspends work visibly while preserving receipts and core results.

**Scale/Scope**: Two events × two request types × three transports across both current forges; configurable presentation and environment labels. Email, historical backfill, inbound commands, merging, approvals, and deployment observation remain excluded.

## Constitution Check

Pre-research review found no inherent conflict with optional notifications; state, compatibility and bounded effects required explicit designs. Post-design checks below assess those proposals, not unimplemented code.

| # | Gate and evidence | Post-design |
|---|---|---|
| I | Named pinned destinations, versioned origin/ledger, existing ownership checks; no title matching | PASS |
| II | Policy and template files loaded at resolved config OID; no config-defined commands | PASS |
| III | Clock/observations injected, stable event IDs, immutable snapshots; CLI generation remains drift-gated | PASS |
| IV | Optional fields on existing v1 API, public validation/defaulting, provider limitations surfaced | PASS |
| V | Stdlib adapters, ordinary Git notes/PRs, no other Skaphos dependency | PASS |
| VI | Safe reason codes, identity/footer, actionable errors, secret redaction | PASS |
| VII | Read-only commands do not resolve endpoint secrets, send, or mutate remote state | PASS |
| VIII | Environment labels reference declared branches; logical edge captured, not guessed from backflow names | PASS |
| IX | Proposed status, explicit scope and endpoint-acceptance limits; no deployment claim | PASS |
| X | Engine independent; add depguard denial of effects/HTTP for pure notification code | PASS |
| XI | Reserved notes ref and explicit-lease operation require T001 maintainer disposition against the existing force-push restriction; ancestry guards alone do not settle authorization | PENDING — blocks T012 and dependent writes/delivery |
| XII | Proposed [0013](../../docs/adr/0013-notification-configuration-contract.md) and [0014](../../docs/adr/0014-notification-delivery-ledger.md) cover additive contracts; no existing removal/deprecation | PASS |

Implementation gates: race/shuffle tests, fuzz untrusted origin/ledger/URL/template inputs, at least 85% statement coverage on new notification packages, generated-reference drift checks, and same-change operator docs. Existing Taskfile targets do not enforce race/shuffle or a coverage floor; add reproducible feature verification targets and wire them into CI when implementing. This plan does not claim to resolve unrelated constitution TODOs.

T001 must record the exact permitted ref/operation and its basis under the current
constitution before the notes writer is implemented. A maintainer's ADR approval
is not an exception to a MUST rule: if the proposed operation conflicts, redesign
it or obtain a separately reviewed constitution change. This remediation changes
neither governance nor permissions and performs no ref write.

## Project Structure

### Documentation (this feature)

~~~text
specs/001-promotion-notifications/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── configuration.md
│   ├── interfaces.md
│   ├── delivery.md
│   └── presentation.md
└── checklists/requirements.md
docs/adr/
├── 0013-notification-configuration-contract.md
└── 0014-notification-delivery-ledger.md
~~~

The implementation backlog is now in [tasks.md](tasks.md); remediation evidence
and the unresolved authority gate are in [checklists/remediation.md](checklists/remediation.md).

### Source Code (planned changes)

~~~text
pkg/api/v1/                     optional config, validation/defaulting
internal/config/                strict parse and compatibility fixtures
internal/notification/          NEW pure event/selection/reduction/rendering
internal/notification/delivery/ NEW HTTP effects and three adapters
internal/notification/store/    NEW bounded ledger codec and notes operations
internal/tmpl/                  reusable safe template helpers, if needed
internal/forge/                 lifecycle, snapshot, creation-outcome capabilities
internal/forge/github/          lifecycle/origin, commit snapshots, authenticated Git
internal/forge/azuredevops/      equivalent behavior and full-detail hydration
internal/forge/forgetest/        shared lifecycle/provenance fixtures
internal/git/                   narrow notes helpers with isolated object/index work
internal/reconcile/             optional notification observation/finalization
internal/cli/                   loaded policy, preview, result and summary wiring
docs/guides/notifications.md     NEW setup, templates, troubleshooting and recovery
docs/reference/                 config/preview/webhook/context and generated CLI
Taskfile.yml                    feature verification/coverage targets
.github/workflows/ci.yml         invoke the feature verification gates
~~~

**Structure Decision**: A loaded-config value carries topology, templates, notification policy, and resolved config OID separately. Internal lifecycle capabilities provide richer facts without widening the branch engine's request view. A renderer wrapper composes an optional preview with existing plan JSON; notification-only work does not change branch action counts or exit codes. Reuse existing template discipline while keeping event and PR-body contexts separate.

**Revision and presentation handoff**: Store the last accepted config OID/digest;
only a verified descendant can advance policy atomically with its subscription
changes. Stale or unorderable revisions defer new notification work; CAS receipt
reduction still permits late results for already dispatched attempts. All-disabled
invocations leave the ledger untouched, so re-enable resumes the last durable
epoch unless identity changes or a retirement was recorded while another
destination stayed enabled. This is not an immediate cross-worker stop mechanism.
Pass `DeliveryPayloadV1 {schemaVersion, event, message}` directly to adapters,
joining immutable event facts with persisted per-destination title/body. The
fixed footer includes all FR-008 fields, explicitly request ID and observed time.

## Phase 0 — Research conclusions

[research.md](research.md) records choices and alternatives for notes storage/CAS, provenance, pagination, activation, bounded retries, adapters, presentation, and compatibility. The repo has no `tools/ECOSYSTEM.md`; upstream notification libraries were evaluated directly. Research agents separately investigated transport contracts and durable state.

## Phase 1 — Design and implementation sequence

1. **Contracts and policy.** Resolve T001 before affected writes. Add optional configuration, environment names, templates, public validation/defaulting, and loaded-policy wiring. Pin omitted/false/empty semantics and all-disabled resumption. Setup scaffolds fixture documentation only; typed fixtures follow model/interface definitions in T010. Review the proposed ADRs in the implementation PR without assuming acceptance.
2. **Pure model and presentation.** Implement stable IDs, revision-ordered epochs, monotone transitions, typed template context, complete FR-008 footer, persisted delivery payload, and deterministic preview. Validate all event/request combinations. Test stale workers, all-disabled intervals, per-destination overrides, unknown fields, secret exclusion and overflow.
3. **Durable ledger.** Build notes read/create/expected-tip updates, parsing caps, claims and receipts. Race workers against a bare Git remote; prove namespace guards, no rewind and success monotonicity. Persist immutable commit facts and rendered retry payloads.
4. **Forge lifecycle and snapshots.** Add complete/incomplete pages, known-request polling, explicit creation/adoption disposition and initial origin. Fetch event-specific commits; test source advancement, squash/rebase, deleted refs, partial POST success and short-lived requests.
5. **Delivery adapters.** Implement constrained HTTP, payload envelopes, safe responses and persisted retry scheduling. Exercise redirects, TLS/DNS policy, throttling, timeouts, malformed responses, and independent destinations.
6. **Coordinate and preview.** Initialize activation, observe without effects, render previews, capture creation outcomes incrementally and finalize notifications within budget after core work. Preserve partial core failures and exit 0/1/3; cancelled contexts start no sends.
7. **Operator acceptance and release gates.** Execute [quickstart.md](quickstart.md), including opt-in live provider CAS and real channel rendering/visibility. Add docs, regenerate CLI help, enforce coverage and required CI checks.

Transports and forge observation can proceed independently once contracts/model exist. No production implementation is performed during this planning phase.

## Verification and traceability

| Requirements | Evidence required |
|---|---|
| FR-001–003, 007, 018–019 | Config fixtures, default/empty behavior, filters, no-policy call counters |
| FR-004–006, 021 | Both forge suites: ownership, adoption, closure, partial creation and recovery |
| FR-008–012, 022 | Stable IDs, activation, CAS races, lost receipts, pending recovery, no private DB |
| FR-013–015 | Multi-destination fault tests and unchanged core exit 0/1/3 |
| FR-016–017 | Redaction, hostile text, TLS/DNS/redirect tests and real channel checks |
| FR-020 | Read-only no-secret/no-send/no-remote-write tests and preview compatibility |
| FR-023–026 | Template context/overrides, environment labels, immutable commit snapshots, fixed identity footer |
| SC-001–008 | Real endpoint visibility, 1,000 repeat evaluations, failure matrix and timed operator setup |

Run race/shuffle suites on all supported OSes; enforce the new-package 85% floor rather than hiding it in unrelated coverage. Fuzz duplicate JSON keys, malformed/newer origins and ledgers, oversized data, hostile URLs and templates. Golden fixtures preserve existing no-policy stdout and exit semantics.

## Operational risks and rollback

- Notes-write constitutional authority requires T001 before implementation; notes permission and live expected-tip semantics separately require opt-in conformance on both forges before release. Neither platform permissions nor passing tests grant constitutional authority. Denied writes suspend sends and leave core work usable.
- Stale/divergent config revisions defer notifications, not core work; restore ordered history with a reviewed descendant config commit. Fully disabled runs cannot record retirement or stop older workers through the untouched ledger; use a new destination name for a fresh cutoff after re-enable.
- A send and its receipt are not atomic. Recovered leases or lost receipts can duplicate externally accepted messages; stable IDs remain available.
- Dense history, long outages and capacity limits can delay notifications. Incomplete scans retain cursors and known pending deliveries; never silently advance past unseen data.
- Teams Workflows ownership/tenant policy may prevent channel delivery after HTTP acceptance. Verify actual visibility, document co-ownership and the initial Anyone mode.
- PR commit APIs must prove historical snapshot fidelity. Use captured immutable OIDs where necessary, otherwise disclose unavailable details. Source SHAs are not destination SHAs after squash/rebase.
- Rollback disables/removes optional policy on the pinned config ref before downgrading the binary. Keep notes and origin metadata for continuity; do not delete state or replay history automatically.

## Complexity Tracking

No constitution exception is granted or requested by these artifacts. The notes
proposal has an unresolved authority gate, C1/T001, rather than a PASS inferred
from append-only ancestry. The exact operation must be demonstrated to conform
before affected implementation, or the design must change through review. Pure
models and documentation can be reviewed without ref-write authority. Proposed
ADRs remain proposed; platform verification gates remain additional requirements.
