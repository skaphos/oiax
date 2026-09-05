# Tasks: Managed Request Notifications

**Input**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts](contracts/), and [quickstart.md](quickstart.md).

**Branch**: `spec/promotion-notifications` · **Tracking**: [#75](https://github.com/skaphos/oiax/issues/75). Email remains deferred to [#76](https://github.com/skaphos/oiax/issues/76).

**Status**: T001–T015 and the bounded delivery slice T029/T031–T033 are complete. Remaining US1 conformance, lifecycle, observation, presentation, built-binary and acceptance tasks are pending; this is not a release-ready notification feature. Evidence is recorded in [implementation-validation.md](checklists/implementation-validation.md). Paths below are repository-relative.

**Tests**: Mandatory under `.specify/memory/constitution.md`. Write tests first and demonstrate the relevant failure before implementation, then pass them with `-race -shuffle=on`. Include parser fuzzing, built-binary checks, and an enforced 85% statement-coverage floor for each new notification package. Never count an empty test-name match as verification.

**Format**: `- [ ] Tnnn [P?] [USn?] Description with exact file paths`. `[P]` means independent files within the explicitly named batch after its prerequisites are complete, not unrestricted parallel execution. Default execution is sequential. Test writing can be parallel; implementation does not overlap a test task editing its files.

## Phase 1: Setup

**Purpose**: Confirm contract authority and establish reusable test boundaries without changing product behavior.

- [X] T001 Review the namespace prerequisite in `docs/adr/0013-notification-configuration-contract.md` and `docs/adr/0014-notification-delivery-ledger.md` against `docs/architecture.md` and `.specify/memory/constitution.md`; record the maintainer's explicit extension to `refs/notes/oiax/` in `docs/adr/0015-oiax-owned-notes-namespace.md` and `specs/001-promotion-notifications/checklists/implementation-validation.md`. The exact notification target, expected-tip/append-only operation and no-rewind/no-deletion safeguards are authorized; broader ADRs remain proposed and runtime checks are not claimed complete.
- [X] T002 Scaffold fixture responsibilities and planned seams only in `internal/notification/notificationtest/README.md`; document injected clocks, lifecycle/store doubles and attempt recorders without introducing typed Go fixtures or importing interfaces that do not yet exist. Typed fixtures are implemented in T010 after T008 defines the model.
- [X] T003 Add the pure-notification import boundary to `.golangci.yml`: deny Git, forge, HTTP, environment/process access and effect subpackages from `internal/notification/*.go`, while retaining existing engine guards and allowing effects only in their designated packages.

**Checkpoint**: Namespace review has a recorded explicit maintainer disposition (T001); remaining setup tasks still require completion. Fixture responsibilities are documented before typed implementation. Namespace authority comes from ADR 0015/the explicit constitution amendment, not inferred approval of proposed ADRs 0013/0014.

## Phase 2: Foundational prerequisites

**Purpose**: Shared policy, deterministic event state, persistence safety, and effect interfaces. This phase blocks all story implementation.

- [X] T004 [P] Add table-driven policy/defaulting and YAML/JSON round-trip tests in `pkg/api/v1/notifications_test.go` and `internal/config/notifications_test.go`: omitted versus false/empty, duplicate names/subscriptions, 20-destination bound, environment variable names, email rejection, template-slot structure, graph-label validation, unknown fields, and old-config compatibility; verify errors do not echo rejected secret values.
- [X] T005 [P] Add deterministic event-ID, policy-revision, epoch, claim and monotone-reducer tests in `internal/notification/model_test.go`: repository rename, event-kind separation, same-OID/digest agreement, ancestor rejection, descendant acceptance, divergent/unknown ancestry, legitimate descendant content reverts, injected clocks/skew, terminal success, stale failure, lost lease and no historical replay; keep tests self-contained until T010 fixtures exist.
- [X] T006 [P] Add ledger codec and bare-remote expected-tip tests in `internal/notification/store/codec_test.go` and `internal/git/notes_test.go`: duplicate keys, unknown schemas, bounded reads, invalid references/OIDs, absent versus denied state, concurrent creation/update, exact namespace, sole-parent ancestry, and refusal to rewind or touch branches/tags/worktrees.
- [X] T007 Implement additive notification types and exported pure validation/defaulting in `pkg/api/v1/types.go`, `pkg/api/v1/validate.go`, and `internal/config/config.go` per `contracts/configuration.md`; preserve explicit empty slices, pointer booleans, strict parsing, existing v1/v1alpha1 behavior, and structural presentation validation without secret resolution.
- [X] T008 Implement immutable repository/request/event/snapshot types, `PolicyRevisionV1`, `RenderedMessageV1`, `DeliveryPayloadV1` and destination generations in `internal/notification/model.go`, with canonical length-delimited SHA-256 identities, explicit captured times and safe outcome enums; keep `internal/engine` unchanged and endpoint values outside the pure model.
- [X] T009 Implement pure transitions in `internal/notification/reducer.go`: injected verified config-ancestry evidence, same-OID/digest agreement, strict-descendant policy advancement, stale/unordered revision deferral, atomic policy/subscription updates, activation before admission, durable-only disable/re-enable epochs, 120-second destination/event claims, terminal delivered records, late receipts without policy rollback and reserved claim/result capacity without automatic receipt garbage collection.
- [X] T010 Define optional lifecycle, event-revision snapshot and creation-outcome capabilities in `internal/forge/notifications.go`, and store/delivery contracts accepting `DeliveryPayloadV1` in `internal/notification/interfaces.go`. After those definitions and T008, implement injected clock, lifecycle/store doubles and attempt-recorder fixtures in `internal/notification/notificationtest/fixtures.go`; assert interface conformance with no secrets or effect imports in production pure code.
- [X] T011 Implement canonical ledger encoding/validation in `internal/notification/store/codec.go`: fixed initialization anchor, identity/cross-reference checks, duplicate-key rejection, version checks, 8 MiB/50,000-delivery caps including reserved result space, and no raw remote errors, PR bodies, configuration blobs, or secrets.
- [X] T012 Implement narrow notes object/read/write helpers in `internal/git/notes.go`: isolated index/object workspace, explicit fetch, standard note on the anchor, validated exact `refs/notes/oiax/notifications/v1/<64-hex-graph-key>`, full expected OID lease including absent creation, sole-parent child commits, no hooks, and no checkout or arbitrary-ref writer; depend on the T001 disposition.
- [X] T013 Implement `Read` and transition-based `Commit` in `internal/notification/store/store.go` using existing provider Git authentication without credentials in URLs/arguments; distinguish absence/conflict/denial/corruption, reread and reduce conflicts at most five times, require refreshed revision-order evidence rather than replaying stale policy replacements, and suspend sends safely on invalid state or exhausted capacity.
- [X] T014 Carry graph, PR templates, notification policy, notification template sources, and resolved config OID separately through `internal/cli/root.go` and `internal/cli/wiring.go`; load body files from that exact commit with repository-relative validation and the existing 1 MiB cap, never the triggering ref or arbitrary local files.
- [X] T015 Add loaded-config/no-policy regression tests in `internal/cli/notification_config_test.go`: absent/all-disabled policies invoke no notification capabilities or environment lookups and write no retirement/revision; read-only commands make no remote writes, pinned files win over local/triggering changes, and legacy stdout/exit fixtures remain unchanged.

**Checkpoint**: T004–T006 and T015 pass; pure transitions and durable storage are usable without any destination HTTP call. Notification state failure cannot authorize an untracked send.

## Phase 3: US1 — Know when a managed request merges (P1, MVP milestone)

**Goal**: Notify on actual managed promotion/backflow merges, with required identity, durable receipts, no historical backfill, and no change to core results.

**Independent test**: Activate one destination, merge a managed request through a human/test fixture, reconcile, verify one complete notification, and evaluate unchanged state 1,000 times without another send. Repeat for backflow and both forges; closed-unmerged and unmanaged requests produce none.

### Tests first

- [X] T016 [P] [US1] Add shared lifecycle conformance cases in `internal/forge/forgetest/notifications.go`: ownership, merged versus merely closed, old open requests merging after activation, short-lived requests, backflow with deleted refs, legacy logical-edge unavailability, and complete/incomplete scan semantics for both providers.
- [ ] T017 [P] [US1] Add safe-client tests in `internal/notification/delivery/client_test.go`: HTTPS-only endpoints, userinfo/fragments, TLS verification, no redirects, DNS rebinding, IPv4/IPv6 forbidden ranges, generic-only private-unicast opt-in, bounded bodies/deadlines, absent secrets, and no forge-auth forwarding or raw error leakage.
- [ ] T018 [P] [US1] Add payload/acknowledgment goldens in `internal/notification/delivery/adapters_test.go` for Teams Workflows Adaptive Card 1.2, Slack plain-text blocks and strict `200/ok`, and fixed webhook schema/header/2xx; assert every FR-008 field, explicit request ID/observed-at time, inert text, safe links and 24 KiB limits. Feed distinct persisted messages for the same immutable event and prove adapters use the supplied payload without store access or re-rendering.
- [ ] T019 [P] [US1] Add merge/repeat/failure integration tests in `internal/reconcile/notification_merge_test.go`: first activation, competing runs, receipt persistence, 1,000 repeat evaluations, no unmanaged calls, no-policy bypass, cancellation, partial core errors, and unchanged core exit 0/1/3.

### Implementation

- [X] T020 [US1] Implement managed lifecycle discovery/detail hydration in `internal/forge/github/notifications.go`: immutable repository ID, all states/types, `state=all&sort=created&direction=asc`, overlap with ID deduplication, known-open direct reads, authoritative merged timestamps, and explicit incomplete continuations independent of baseline history.
- [X] T021 [US1] Implement equivalent lifecycle discovery in `internal/forge/azuredevops/notifications.go`: full body/property hydration, frozen inclusive creation and completion intervals, bounded partitioning, repeated ID-set completeness verification without assumed ordering, and retained incomplete cursors for dense same-timestamp intervals.
- [X] T022 [US1] Register both provider suites and add provider-specific pagination/detail fixtures in `internal/forge/github/notification_test.go` and `internal/forge/azuredevops/notification_test.go`; demonstrate long outages beyond baseline lookback, list movement, deleted/denied details, and unprovable intervals without silently losing events.
- [ ] T023 [US1] Implement merge-event normalization and baseline routing in `internal/notification/select.go`: existing ownership evidence only, forge occurrence after persisted cutoff, both request types by default, actual refs and explicit unknown logical edge, stable identities, and no merge inferred from branch equality.
- [ ] T024 [US1] Implement built-in title/body and fixed identity/completeness sections in `internal/notification/render.go`, distinguishing creation/merge and promotion/backflow; sanitize controls and use bounded explicit unavailable-commit placeholders until snapshot enrichment is wired, never assert deployment completion.
- [ ] T025 [US1] Implement constrained endpoint resolution and HTTP execution in `internal/notification/delivery/client.go` after T017: runtime named variables only for eligible enabled deliveries, connection-time address checks, original-host TLS verification, 10-second timeout, no redirect, 16 KiB response cap, and whitelisted diagnostics with secrets excluded from errors/output/storage.
- [ ] T026 [P] [US1] Implement the fixed generic JSON envelope and `X-Oiax-Event-ID` header in `internal/notification/delivery/webhook.go` after T024–T025; preserve structured identity/commit completeness outside customizable message text and interpret only 2xx as endpoint acceptance.
- [ ] T027 [P] [US1] Implement Teams Workflows `Anyone`-mode payload/response handling in `internal/notification/delivery/teams.go` after T024–T025: inert RichTextBlock/TextRun content, no mention entities, fixed-label validated OpenUrl, and endpoint acceptance distinct from channel visibility.
- [ ] T028 [P] [US1] Implement Slack incoming-webhook payload/response handling in `internal/notification/delivery/slack.go` after T024–T025: static fallback, plain-text sections/fields within 3,000/2,000-character limits, safe link button, and exact success-body semantics without assuming remote deduplication.
- [X] T029 [US1] Implement persisted retry scheduling and fair per-destination dispatch selection in `internal/notification/schedule.go`: at most once per event/destination/run, exponential one-minute to one-hour delay, bounded Retry-After up to 24 hours, one-hour configuration-failure delay, 10 attempts/destination and 100/run, and one-second spacing.
- [ ] T030 [US1] Implement independent lifecycle observation in `internal/reconcile/notification_observe.go`: verify config ancestry outside pure code and recompute after CAS conflict, atomically admit policy only at equal/descendant revisions, initialize activation before eligibility, persist known requests/events with scan progress, bound scans to 100 pages of 100, retain incomplete watermarks and process pending work despite discovery failures. Stale/unorderable config defers new admissions/claims without changing core results.
- [X] T031 [US1] Implement durable dispatch orchestration in `internal/reconcile/notification_delivery.go`: persist sanitized `RenderedMessageV1` before claiming a send, join it to the admitted event as `DeliveryPayloadV1`, acquire/renew claims, recheck accepted config OID and generation immediately before dispatch, reserve receipt capacity, respect the 120-second stage budget and report accepted-without-receipt as uncertain. Adapters must not re-render; old workers may record late results but not restore policy or send again.
- [X] T032 [US1] Wire optional observation/finalization through `internal/reconcile/reconcile.go`, `internal/cli/reconcile.go`, and `internal/cli/wiring.go`; finalize eligible work on partial core failure only while context remains usable, preserve original core errors/results and exit 0/1/3, and bypass all notification effects when disabled.
- [X] T033 [US1] Add scheduler/claim boundary tests in `internal/notification/schedule_test.go`: due times, skew, 429 and capped Retry-After, global/per-destination budgets, fairness, and at-most-one attempt per pair/run; prove a failing or slow destination cannot consume all other destinations' opportunities.
- [ ] T034 [US1] Add adversarial store/dispatch tests in `internal/reconcile/notification_concurrency_test.go`: initialize/claim races, newer config committed before an older worker resumes, conflict followed by stale re-reduction, equal OID/digest mismatch, divergent/unknown ancestry, a legitimate descendant content revert, expired suspended sender, failed receipt and late success racing failure; assert no revived subscriptions, stable event IDs and preserved terminal receipts.
- [ ] T035 [US1] Add default-presentation tests in `internal/notification/render_test.go`: both events/types, all FR-008 fixed fields including explicit request ID and observed-at time, actual/logical refs, unsafe/control text, unfit required identity and explicit unavailable completeness without fabricated empty history.
- [ ] T036 [US1] Add local built-binary merge scenarios in `internal/cli/notification_merge_test.go` using fake forge/HTTPS boundaries and bare Git: managed merge delivery, no duplicate after receipt, closed/unmanaged exclusion, failed endpoint, read-only invocation, and existing detailed-exit behavior.
- [ ] T037 [US1] Verify no-policy plans remain byte-identical and no notification dependencies enter `internal/engine` using `internal/cli/plan_reconcile_test.go` and `.golangci.yml`; test all-disabled and explicitly empty event/request-type selections as equivalent effect bypasses.
- [ ] T038 [US1] Execute and record the independent US1 fixture matrix in `specs/001-promotion-notifications/checklists/implementation-validation.md`, including 1,000 repeat evaluations and both-forge results; identify unrun live checks explicitly, with no claim of recipient visibility from HTTP acceptance.

**Checkpoint**: Default merge notifications form an internal MVP milestone with durable recovery and all three transports. This is not full feature release readiness: custom presentation, creation provenance, complete preview, and live acceptance gates follow.

## Phase 4: US2 — Opt in to request-created notifications (P2)

**Goal**: Emit creation only for real Oiax creates and recover the same event after partial failure, without relabeling adoption/update as creation.

**Independent test**: Enable creation, create one managed promotion/backflow request and observe one message. Disable creation and observe none. Crash after POST and recover the original event; adoption without origin and ordinary updates never invent one.

### Tests first

- [ ] T039 [P] [US2] Add origin parse/format/preservation tests and a seeded fuzz target in `internal/forge/marker/notification_origin_test.go`: 4 KiB cap, duplicate blocks/keys, malformed version/OIDs, escaped comment terminators, invalid ownership, and unchanged existing marker replacement.
- [ ] T040 [P] [US2] Add shared creation disposition and partial-success fixtures in `internal/forge/forgetest/notification_creation.go`: initial POST origin, post-create label/property failure, adoption with/without original provenance, logical backflow source, and source advancement between observation and POST.
- [ ] T041 [P] [US2] Add recovery/opt-in tests in `internal/reconcile/notification_creation_test.go`: both request types, distinct created/merged events for short-lived requests, partial Apply progress, no event on failed POST, and no new event on adoption/update.

### Implementation

- [ ] T042 [US2] Implement the separate immutable origin codec in `internal/forge/marker/notification_origin.go` with operation/config/logical-edge/snapshot evidence, HTML-safe JSON and limits; origin must never establish ownership by itself or change the existing v1 marker wire format.
- [ ] T043 [US2] Migrate creation contracts and callers in `internal/forge/forge.go`, `internal/forge/forge_test.go`, and `internal/reconcile/reconcile.go` to explicit created/adopted outcomes with optional origin, preserving partial successful creates and existing engine semantics; migrate fake implementations in `internal/reconcile/reconcile_test.go` and `internal/forge/forgetest/conformance.go` together.
- [ ] T044 [US2] Implement GitHub creation disposition and initial-body origin in `internal/forge/github/github.go`, `internal/forge/github/marker.go`, and `internal/forge/github/notification_creation_test.go`; preserve origin through updates, return recoverable actual-create information on follow-up failure, and never add origin to an adopted legacy request.
- [ ] T045 [US2] Implement equivalent Azure creation behavior in `internal/forge/azuredevops/azuredevops.go`, `internal/forge/azuredevops/artifacts.go`, and `internal/forge/azuredevops/notification_creation_test.go`; body origin precedes POST, optional property mirror is supplemental, and full-detail recovery survives property failure.
- [ ] T046 [US2] Implement origin-backed created-event admission and recovery in `internal/notification/creation.go` and `internal/reconcile/notification_observe.go`, requiring forge-confirmed creation time/current ownership/graph consistency and the original event ID; process recovered creates and merges independently after their subscription cutoffs.
- [ ] T047 [US2] Capture outcomes incrementally at both promotion and backflow creation sites in `internal/reconcile/reconcile.go` and feed `internal/reconcile/notification_delivery.go` even when a later action fails; never replace the underlying core error with a notification outcome.
- [ ] T048 [US2] Register the shared creation conformance cases in `internal/forge/github/conformance_test.go` and `internal/forge/azuredevops/conformance_test.go`, and add built-binary opt-in/recovery checks in `internal/cli/notification_creation_test.go`; record US2 evidence in `specs/001-promotion-notifications/checklists/implementation-validation.md`.

**Checkpoint**: Created messages are optional, truthfully worded, and recoverable from initial immutable origin without manufacturing history.

## Phase 5: US3 — Route events to the right audiences (P3)

**Goal**: Complete independent subscriptions, environment-oriented templates, per-destination overrides, and immutable event-time commit details.

**Independent test**: Configure Teams/Slack/webhook destinations with different subscriptions, request types and template overrides; verify only eligible recipients receive the intended wording and fixed facts. Advance source branches and edit templates before retry: attempted payloads and event snapshots remain unchanged.

### Tests first

- [ ] T049 [P] [US3] Add routing-generation matrix tests in `internal/notification/routing_test.go`: event/type/transport combinations, omitted/empty selections, disabled/removed/renamed destinations, transport/endpoint-variable changes, new subscriptions and credential rotation. Test a recorded per-destination disable while another stays enabled versus an all-disabled run that leaves no record: re-enable of the latter resumes its old epoch/backlog, while a new name starts a fresh cutoff.
- [ ] T050 [P] [US3] Add template contract tests and seeded fuzzing in `internal/notification/templates_test.go`: closed context, unknown fields/functions, four sample combinations, slot inheritance/bodyFile conflicts, environment fallback, inert subjects, no secret/clock/process access and size limits; constant/empty custom text must retain every FR-008 field, especially explicit request ID and original observed-at time on first send and retry.
- [ ] T051 [P] [US3] Add shared event-revision commit fixtures in `internal/forge/forgetest/notification_commits.go`: creation-time branch races, merge then source advancement, squash/rebase, deleted refs, over 100 commits, unavailable details, and unknown totals; distinguish reviewed source SHAs from merge-result OID.

### Implementation

- [ ] T052 [US3] Complete generation handling in `internal/notification/select.go` and `internal/notification/reducer.go`: per-subscription cutoffs, immutable labels, durable retirement only during enabled processing, same-variable rotation and revision-ordered policy changes. All-disabled invocations persist nothing; unchanged re-enable resumes last durable epochs/backlog, including eligible disabled-interval discoveries. A new name starts fresh; stale workers cannot advance policy, admit events or claim new sends.
- [ ] T053 [US3] Implement the closed notification context, deterministic curated helpers, title/body slot inheritance, and four-combination parse/sample validation in `internal/notification/templates.go`; reuse allowed facilities from `internal/tmpl/tmpl.go` without exposing the PR-body context, runtime secrets or executable configuration.
- [ ] T054 [US3] Wire template validation and pinned-file sources into `internal/cli/root.go` and `internal/cli/notification_config_test.go`; enforce 1 MiB source, single-line 256-rune title, 12 KiB body, graph-bound labels up to 100 runes, and redacted actionable validation before mutation.
- [ ] T055 [US3] Implement GitHub event-specific snapshot reads in `internal/forge/github/notification_commits.go`, with request detail totals, bounded PR commit retrieval, verified creation/merge revisions, immutable-OID fallback and explicit unavailable/truncated flags; never substitute moving branch history.
- [ ] T056 [US3] Implement equivalent Azure snapshot reads in `internal/forge/azuredevops/notification_commits.go`, using verified first iteration/last-merge source and target evidence, bounded continuation reads, and unknown total unless authoritative; pre-POST OIDs remain hints until verified.
- [ ] T057 [US3] Admit at most 100 immutable commit summaries with 200-rune subjects and completeness indicators in `internal/reconcile/notification_observe.go` and `internal/notification/model.go`; retain events when enrichment fails, persist labels/facts once, and never replace admitted facts on retry.
- [ ] T058 [US3] Integrate custom presentation in `internal/notification/render.go` and `internal/reconcile/notification_delivery.go`; append the complete FR-008 identity/completeness section independently, persist sanitized per-destination `RenderedMessageV1` before first attempt and pass `DeliveryPayloadV1` directly to adapters. Verify two destinations receive different saved wording for one unchanged event and retries reuse it after edits or uncertain acceptance.
- [ ] T059 [US3] Register snapshot conformance in `internal/forge/github/notification_test.go` and `internal/forge/azuredevops/notification_test.go`, extend `internal/notification/delivery/adapters_test.go` for custom text plus fixed facts, and make T049–T051 pass across all supported combinations.
- [ ] T060 [US3] Add end-to-end environment/template acceptance in `internal/cli/notification_templates_test.go`: “These commits were promoted to the test environment,” creation-ready/backflow wording, per-destination overrides, source advancement, attempted-payload immutability, truncation/unavailability, and unchanged event IDs; record SC-008 evidence in `specs/001-promotion-notifications/checklists/implementation-validation.md`.

**Checkpoint**: Presentation changes human text only, never event truth, ownership, topology, recipient selection or receipts. A branch promotion message is not proof of deployment.

## Phase 6: US4 — Diagnose delivery problems safely (P4)

**Goal**: Complete safe previews and operational outcomes; harden recovery and show notification failures separately from core reconciliation.

**Independent test**: Fail one destination while another succeeds, confirm all eligible core actions and other attempts proceed with identical exit 0/1/3, inspect safe diagnostics, then recover the pending event on a later invocation. Read-only preview requires no endpoint secrets and performs no sends/writes.

### Tests first

- [ ] T061 [P] [US4] Add preview/output goldens in `internal/reconcile/notification_render_test.go`: optional single-document JSON v1 member, deterministic ordering, observation states, conditional-on-create without fabricated IDs, distinct delivered/filtered/not-due/inactive decisions and safe stale/unordered/mismatched config-revision diagnostics with no state advancement.
- [ ] T062 [P] [US4] Add redaction and outcome tests in `internal/reconcile/notification_diagnostics_test.go`: endpoint URLs embedded in errors or forged subjects, bounded response data, safe name/reason/action only, success versus uncertain receipt, intentionally skipped versus retryable, and no secrets in ledger/messages/logs/summary.
- [ ] T063 [P] [US4] Add built-binary fault and read-only tests in `internal/cli/notification_failures_test.go`: mixed endpoints, missing credentials, denied notes, unknown/corrupt/lost ledger, incomplete scan, stage cancellation, payload/template failure, capacity exhaustion, and preserved core exits/no-policy stdout.

### Implementation

- [ ] T064 [US4] Implement deterministic notification preview composition in `internal/reconcile/notification_render.go` and `internal/reconcile/render.go`, leaving engine actions/edges and `planFormatVersion: 1` intact; omit the member without enabled policy and distinguish uninitialized, unavailable, incomplete and complete observations.
- [ ] T065 [US4] Wire read-only preview through `internal/cli/plan.go` and `internal/cli/wiring.go`; never resolve endpoint environment variables, claim state, write remote refs or send, and ensure notification-only backlog cannot produce branch detailed-exit code 2.
- [ ] T066 [US4] Implement safe reason/action formatting in `internal/reconcile/notification_diagnostics.go` and integrate CLI text/CI summaries in `internal/cli/reconcile.go`; distinguish persisted delivery, failure/retry, deliberate skip and accepted-receipt uncertainty while retaining the original core result.
- [ ] T067 [US4] Complete recovery diagnostics in `internal/notification/store/store.go` and `internal/reconcile/notification_observe.go`: denied/malformed/newer-version state suspends sends, missing state warns and establishes a current cutoff, incomplete scans retain progress, capacity preserves receipts and stale/divergent/unknown config revisions defer notifications. Recommend restoring a reviewed descendant config commit, not deleting notes or selecting a winner by timestamp.
- [ ] T068 [US4] Complete bounded fault handling in `internal/reconcile/notification_delivery.go` and `internal/notification/delivery/client.go`: missing credentials, TLS/DNS/redirect rejection, response/body/render overflow, backoff, renewal conflict and cancellation; sanitize before persistence/output and permit unaffected destinations to proceed within the shared budget.
- [ ] T069 [US4] Exercise T061–T063 with compound faults and stale concurrent workers in `internal/cli/notification_failures_test.go` and `internal/reconcile/notification_concurrency_test.go`; prove no false durable-success report, no success-to-failure regression, and no notification outcome masking core failures.
- [ ] T070 [US4] Record the complete safe-diagnostics and read-only matrix in `specs/001-promotion-notifications/checklists/implementation-validation.md`, including both-forge parity, skipped/retryable distinctions, credential canary results, and exact core exit evidence.

**Checkpoint**: Delivery trouble is visible and recoverable without changing core truth or weakening read-only operation.

## Phase 7: Polish and cross-cutting release gates

**Purpose**: Document verified behavior, enforce reproducible quality gates, and validate platform assumptions. No release, repository settings change, live message send, commit or push is authorized merely by generating this backlog.

- [ ] T071 [P] Write `docs/guides/notifications.md` covering transport setup, runtime secrets, notes permissions, branch-scoped triggers, cadence, capacity/retry recovery, revision-order deferral and descendant content reverts, destination identity changes and duplicate ambiguity. Explain global-disable zero-I/O/backlog resumption and lack of cross-worker immediate cancellation, contrast durably recorded per-destination disable, and document new-name fresh cutoffs and disable-policy-before-downgrade rollback preserving notes/origin.
- [ ] T072 [P] Update `docs/reference/configuration.md`, `docs/reference/templates.md`, and `docs/reference/plan-format.md`, and add `docs/reference/notification-webhook.md`; document defaults/empty choices, closed context and precedence, commit/completeness semantics, fixed schema/footer, preview limitations, and email deferral without promising deployment observation.
- [ ] T073 [P] Add bounded fuzz targets/corpora in `internal/notification/store/codec_fuzz_test.go`, `internal/notification/delivery/client_fuzz_test.go`, and `internal/forge/forgetest/notification_payload_fuzz_test.go`; cover ledger/origin/config/forge payload/URL/template parsers alongside T039/T050, committing discovered regressions without captured credentials.
- [ ] T074 Add reproducible notification verification targets to `Taskfile.yml` and a coverage checker in `tools/checkcoverage/main.go` with tests in `tools/checkcoverage/main_test.go`; require `-race -shuffle=on`, nonempty suites and at least 85% statement coverage for each new production notification package, excluding only test-support fixtures explicitly, and keep checks independent of unrelated package coverage.
- [ ] T075 Wire T074 into existing Linux/macOS/Windows test jobs in `.github/workflows/ci.yml`, preserving required check names and existing DCO/REUSE/lint/staticcheck/govulncheck/generated/snapshot gates; verify local/CI command parity without changing repository settings or broadening permissions unnecessarily.
- [ ] T076 Add opt-in disposable-resource conformance tests in `internal/forge/forgetest/notification_live_test.go` and `internal/notification/delivery/visibility_live_test.go` after the T001 authority gate; verify both providers' notes expected-tip rejection and historical snapshots, Teams/Slack rendering with full FR-008 facts, and SC-001 visibility under the workload/sampling protocol in `specs/001-promotion-notifications/quickstart.md`, using operator resources and human/policy merges only.
- [ ] T077 Execute authorized live conformance and timed first-time setup from `specs/001-promotion-notifications/quickstart.md`; record at least 100 predeclared deliveries per transport across bounded normal-load runs, original observation-run completion/recipient-visible times, missing messages as failures, 95%/60-second and 99%/five-minute results, separate backlog-recovery evidence and under-ten-minute setup in `specs/001-promotion-notifications/checklists/implementation-validation.md`. Missing resources/authority keep the release gate unchecked.
- [ ] T078 Update ownership/boundary documentation in `docs/architecture.md` and `docs/code-map.md`, link the new guide from `README.md`, and regenerate `docs/reference/cli.md` using `go -C tools tool task docs:cli-ref`; review generated diffs, never hand-edit generated help.
- [ ] T079 Run build, race/shuffle tests, the T074 coverage gate, bounded fuzz campaigns, lint, staticcheck, govulncheck, REUSE and generated-drift checks through `Taskfile.yml`; record actual commands/results and verify all FR/SC mappings below in `specs/001-promotion-notifications/checklists/implementation-validation.md`, keeping unrun platform checks explicit.
- [ ] T080 Prepare the implementation handoff in `specs/001-promotion-notifications/checklists/implementation-validation.md`: summarize completed tasks, ADR disposition, no-policy compatibility, live release gates and rollback; when separately authorized to commit/open a PR, use cryptographically signed Conventional Commits with DCO sign-off and reference #75, leaving #76, release-managed files and tags untouched.

## Dependencies and execution order

```text
Setup T001–T003
  └─ Foundation T004–T015
       └─ US1 T016–T038 (internal default-merge MVP)
            ├─ US2 T039–T048 (creation provenance/recovery)
            └─ US3 T049–T060 (routing/templates/snapshots)
                 ↑ final creation-snapshot checks require US2
       US1 + US2 + US3
            └─ US4 T061–T070 (full previews/diagnostics)
                 └─ Polish T071–T080 (release evidence)
```

Recommended completion order is US1 → US2 → US3 → US4. Each story has a standalone behavioral test once its shared prerequisites exist; this does not claim storage and provider integration can be rebuilt independently per story. Essential retry, isolation and safe errors land in US1, not deferred to US4. Full configuration/template support and release readiness require every phase.

Within foundation, T004–T006 are self-contained test-writing tasks after setup and do not import the future T010 fixtures. T007 follows T004; T008 follows T005; T009 depends on T008; T010 defines interfaces against shared models and only then implements typed fixtures. T011–T013 follow T006 and the model/interfaces; T012 additionally requires the resolved T001 authority gate. T014 follows policy/schema; T015 verifies loaded wiring before story work begins. T002 contains documentation only, so there is no setup-to-foundation typed-fixture dependency cycle.

Within each story, finish its tests-first batch before implementation. T020/T021 implement the T016 contract; T022 verifies both. T026–T028 may run together only after T024/T025 and their contract tests are ready. T030 requires providers, selector and store; T031 requires renderer, transports, scheduler and store; T032 integrates both. Complete all US1 checks before treating the MVP milestone as reached.

US2's origin codec precedes contract/provider migration, which precedes recovery and Apply integration. US3 template and provider snapshot work can be developed against foundation/US1 fixtures, but creation fidelity and final integration depend on US2. Changes to shared `internal/reconcile/reconcile.go`, `internal/reconcile/notification_observe.go`, CLI wiring, renderer, model or reducer must be serialized. US4 integrates all event/presentation cases and precedes final evidence collection.

Polish T071–T073 can proceed together after US4. T074 precedes T075; T076 precedes the authorized live run T077. T079 follows docs/generation and verification wiring; T080 follows recorded results. Unavailable live resources are a release gate, not permission to fabricate a pass or send to production.

## Parallel execution examples

These describe safe work batches, not an instruction to spawn agents automatically.

| Story | Independent batch after prerequisites | Join point |
|---|---|---|
| US1 | T016 lifecycle fixtures, T017 HTTP tests, T018 payload goldens, T019 coordinator tests | Finish before corresponding implementations |
| US1 | T026 webhook, T027 Teams, T028 Slack, after T024–T025 | Join before T031 dispatch integration |
| US2 | T039 origin tests, T040 provider fixtures, T041 recovery tests | Join before T042–T047 |
| US3 | T049 routing tests, T050 template tests, T051 snapshot fixtures | Join before T052–T059 |
| US4 | T061 preview goldens, T062 diagnostics tests, T063 binary fault tests | Join before T064–T069 |

Setup/foundation production interfaces are shared; do not mark their implementation as independent merely because multiple packages are involved. The default sequence avoids file conflicts and keeps each story checkpoint verifiable.

## Requirements coverage

| Requirement | Implementation and verification tasks |
|---|---|
| FR-001 pinned named destinations | T004, T007, T014–T015, T054 |
| FR-002 Teams/Slack/webhook; no email | T004, T007, T017–T018, T025–T028, T076 |
| FR-003 defaults and independent subscriptions | T004, T007, T023, T049, T052 |
| FR-004 managed ownership only | T016, T020–T023, T036, T039–T046 |
| FR-005 actual creation only | T039–T048 |
| FR-006 authoritative merged state | T016, T019–T023, T036 |
| FR-007 promotion/backflow parity | T016, T018, T023–T024, T035, T040–T048 |
| FR-008 required event facts | T008, T018, T023–T028, T035, T058–T060 |
| FR-009 stable identity | T005, T008–T009, T019, T034, T046, T060 |
| FR-010 no resend after durable success | T005–T006, T009, T013, T019, T031, T034, T038 |
| FR-011 uncertain acceptance retains ID | T009, T031, T034, T062, T066, T071 |
| FR-012 no historical backfill | T005, T009, T023, T030, T049, T052, T067 |
| FR-013 destination isolation | T019, T029, T031–T034, T063, T068–T070 |
| FR-014 nonfatal failures and later retry | T019, T029, T031–T034, T047, T063, T068–T070 |
| FR-015 safe distinguishable outcomes | T008, T061–T070 |
| FR-016 runtime-only secret values | T004, T007, T011, T015, T017, T025, T062–T063, T065–T068 |
| FR-017 inert untrusted text | T017–T018, T024–T028, T035, T050, T058–T059, T062, T073, T076 |
| FR-018 pure strict validation | T004, T007, T014–T015, T050, T053–T054 |
| FR-019 no-policy effect bypass | T015, T019, T032, T037, T063–T065 |
| FR-020 read-only preview | T015, T036–T037, T061, T063–T065, T069 |
| FR-021 both-forge equivalence | T016, T020–T022, T038, T040, T044–T048, T051, T055–T056, T059, T076–T077 |
| FR-022 Git/forge reconstructible state | T001, T006, T011–T013, T030–T034, T039–T048, T067, T076 |
| FR-023 title/body overrides without fact mutation | T050, T053–T054, T058–T060, T072 |
| FR-024 environment labels/fallback | T004, T007, T049–T050, T052–T054, T057, T060 |
| FR-025 immutable event commit summaries | T051, T055–T060, T076–T077 |
| FR-026 safe validated templates/fixed facts | T014–T015, T018, T035, T050, T053–T054, T058–T060, T063, T068 |
| SC-001 actual visibility latency | T029, T033, T076–T077 |
| SC-002 identity across combinations | T018, T035, T048–T051, T059–T060, T076–T077 |
| SC-003 1,000 unchanged evaluations | T019, T038 |
| SC-004 fault isolation and core exit preservation | T019, T033–T034, T047–T048, T063, T069–T070 |
| SC-005 zero secrets/active injection | T017–T018, T035, T050, T062–T063, T069–T070, T073, T076–T077 |
| SC-006 under-ten-minute setup | T071–T072, T077 |
| SC-007 lifecycle parity on both forges | T016, T022, T038, T040, T048, T051, T059, T076–T077 |
| SC-008 environment/template/snapshot acceptance | T049–T060, T076–T077 |

## Implementation strategy and completion accounting

1. Complete setup/foundation and validate state safety before attempting destination delivery.
2. Complete US1 as the internal MVP: default managed-merge notifications with one configured destination in the independent test, both request types/forges, all three adapters, receipt deduplication and nonfatal retries. Stop and verify before adding creation. Do not ship unimplemented template configuration as functional.
3. Add US2 creation provenance/recovery, then US3 routing/custom presentation/verified snapshots, and US4 full preview/diagnostics. Re-run prior story checks at every join.
4. Complete documentation, coverage/CI gates, built-binary/fuzz checks and authorized live acceptance before declaring the entire feature ready. Leave tasks unchecked when evidence is missing.
5. Keep changes reviewable; commits/PRs require separate authorization and must follow the repository's signed Conventional Commit/DCO policy. Rollback removes optional pinned configuration before binary downgrade; retain durable receipts and origin.

**Count**: 80 tasks — Setup 3, Foundation 12, US1 23, US2 10, US3 12, US4 10, Polish 10. There are 22 `[P]` tasks across bounded batches. All 26 functional requirements and eight success criteria are mapped above.

The seven analysis findings are tracked in [remediation.md](checklists/remediation.md).
That document records specification changes, not completed implementation tests;
C1/T001 is resolved by the explicit maintainer decision in ADR 0015; setup and foundation T002–T015 are complete, and the remaining 65 tasks are pending.
