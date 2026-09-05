# Feature Specification: Managed Request Notifications

**Feature Branch**: `spec/promotion-notifications`

**Created**: 2026-09-04

**Status**: Draft

**Tracking**: [Notifications #75](https://github.com/skaphos/oiax/issues/75); [future email integration #76](https://github.com/skaphos/oiax/issues/76).

**Input**: User description: "A new feature that will notify a Teams Channel, Slack Channel, or other interface (email, webhook) upon promotion (merge) of a oiax PR, optionally also upon creation of a PR"

## Clarifications

### Session 2026-09-04

- Q: Should notifications cover both Oiax-managed promotion requests and backflow requests? → A: Both are enabled by default, and each destination can disable either request type.
- Q: When any notification delivery fails, should Oiax preserve its existing reconciliation exit status? → A: Preserve the existing exit status, report the failure, and retry on a later reconciliation.
- Q: How should each configured destination identify the runtime credential or secret-bearing address it needs? → A: Each destination names an environment variable whose value is supplied only at runtime.
- Q: What should “email” support mean in the first release? → A: Defer email; initially support Teams, Slack, and generic webhooks.
- User refinement: Notifications need customizable presentation, environment names, and the commits included in the request; for example, “These commits were promoted to the test environment.” Branch-promotion completion does not assert deployment completion.
- Analysis remediation: A fully disabled invocation performs no notification I/O and therefore cannot record a disabled generation. Re-enabling the same destination resumes its last durable subscriptions/backlog unless an intervening transition was recorded while another destination remained enabled. A new destination name establishes a fresh activation cutoff.
- Analysis remediation: Configuration revisions are ordered by verified Git ancestry, not worker start time or CAS arrival order. An older or unorderable revision cannot replace newer durable notification policy. Latency acceptance is measured at the bounded normal load defined under SC-001, separately from backlog recovery.
- Maintainer decision: Explicitly extend Oiax's owned namespace to `refs/notes/oiax/`, using Git's standard notes prefix. [ADR 0015](../../docs/adr/0015-oiax-owned-notes-namespace.md) and Constitution XI v2.0.0 record the namespace-only authorization; notification updates remain expected-tip and append-only, with no notes deletion/rewind or force-push of long-lived branches.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Know when a managed request merges (Priority: P1)

As an operator responsible for a branch-promotion graph, I receive a notification at each configured destination when an Oiax-managed request is merged. The notification tells me which repository, graph, request type, and branch edge changed and gives me a direct link to the merged request.

**Why this priority**: A merged managed request is the moment branch-promotion state changes. Timely awareness of that event is the feature's primary value.

**Independent Test**: Configure one destination for merged-request events, merge one managed request, run reconciliation, and verify that one complete notification is delivered and that unchanged subsequent runs deliver no duplicate.

**Acceptance Scenarios**:

1. **Given** a destination subscribed to merged-request events and an open managed promotion request, **When** that request is merged and Oiax next observes it, **Then** the destination receives one notification identifying the repository, graph, source, destination, request, event, and request link.
2. **Given** a destination subscribed to merged-request events and an open managed backflow request, **When** that request is merged and Oiax next observes it, **Then** the destination receives one notification clearly identifying the request as backflow rather than forward branch promotion.
3. **Given** a merged-request notification was successfully delivered, **When** reconciliation runs again against unchanged observed state, **Then** Oiax does not deliver that event again to the same destination.
4. **Given** a managed request is closed without being merged, **When** Oiax observes the closed request, **Then** no merged-request notification is produced.

---

### User Story 2 - Opt in to request-created notifications (Priority: P2)

As an operator, I can opt a destination into notifications when Oiax creates a managed request, so reviewers can act without waiting for someone to discover the request in the forge.

**Why this priority**: Creation notifications shorten review time, but they are optional because some teams already rely on forge-native request notifications.

**Independent Test**: Enable created-request events for one destination, cause Oiax to create one managed request, and verify one notification; repeat with the event disabled and verify no notification.

**Acceptance Scenarios**:

1. **Given** a destination subscribed to created-request events, **When** Oiax successfully creates a managed promotion request, **Then** the destination receives one notification with the request type, edge, and link.
2. **Given** a destination subscribed to created-request events, **When** Oiax successfully creates a managed backflow request, **Then** the destination receives one notification clearly identifying the request as backflow.
3. **Given** a destination is not subscribed to created-request events, **When** Oiax creates a managed request, **Then** no creation notification is attempted for that destination.
4. **Given** Oiax adopts or updates an already-existing managed request, **When** reconciliation completes, **Then** it does not misreport that request as newly created.

---

### User Story 3 - Route events to the right audiences (Priority: P3)

As an operator, I can configure one or more destinations using supported Teams, Slack, or generic webhook delivery types and choose both the event subscriptions and managed request types for each destination.

**Why this priority**: Teams commonly need different audiences for operational awareness and review requests. Independent subscriptions prevent unnecessary message volume.

**Independent Test**: Configure destinations with different subscriptions, exercise both supported event types, and verify that each destination receives only its selected events.

**Acceptance Scenarios**:

1. **Given** multiple enabled destinations with different event subscriptions, **When** a managed-request event occurs, **Then** only destinations subscribed to that event receive it.
2. **Given** a destination with backflow notifications disabled, **When** a managed backflow request event occurs, **Then** that destination does not receive it while remaining eligible for enabled promotion request events.
3. **Given** a disabled destination, **When** any supported event occurs, **Then** Oiax makes no delivery attempt to that destination.
4. **Given** an unsupported destination type or incomplete destination definition, **When** an operator validates the configuration, **Then** validation fails with the destination name, the invalid field, and a corrective action before any repository or destination mutation occurs.
5. **Given** a destination names a structurally valid credential environment variable that is not present, **When** an operator runs a read-only validation, **Then** configuration validation succeeds without requiring the credential value.
6. **Given** the target branch `test` has the notification environment label `test` and a custom notification template, **When** a managed promotion request merges, **Then** recipients see the configured wording and the commits included in that request, such as “These commits were promoted to the test environment,” with a link to the request.
7. **Given** the source branch advances after the request merges, **When** its notification is rendered or retried, **Then** the listed commits still describe the merged request rather than newer source-branch changes.

---

### User Story 4 - Diagnose delivery problems safely (Priority: P4)

As an operator, I can distinguish successful, failed, skipped, and retryable notification outcomes without exposing credentials or losing the result of the underlying branch-promotion reconciliation.

**Why this priority**: Notifications are useful only when failures are visible, but this supplementary feature must not make Oiax stop maintaining managed requests.

**Independent Test**: Make one of several destinations unavailable and verify that other destinations are attempted, core reconciliation completes, the failed destination is identified safely, and a later run can retry the undelivered event.

**Acceptance Scenarios**:

1. **Given** one unavailable destination and one available destination subscribed to the same event, **When** delivery is attempted, **Then** the available destination receives the event and the unavailable destination is reported separately as retryable.
2. **Given** a delivery failure, **When** Oiax reports the outcome, **Then** the report contains the destination's non-secret name and a useful reason but contains no credential value or secret-bearing destination address.
3. **Given** a delivery failure after the managed-request action succeeds, **When** reconciliation completes, **Then** the request action remains successful and is not rolled back or misreported as failed.
4. **Given** one or more notification deliveries fail, **When** reconciliation completes, **Then** Oiax returns the same exit status that the underlying reconciliation result would have produced without notifications and reports each failed delivery for a later retry.

### Edge Cases

- Notifications are enabled in a repository with historical merged managed requests: historical requests are not backfilled by default; only events newly observed after the configuration becomes effective are eligible.
- Two reconciliation runs observe the same event concurrently: they converge on the same stable event identity and do not intentionally create two logical notifications.
- A destination accepts a delivery but the response is lost: a retry may result in a duplicate, and the stable event identity allows receivers that support deduplication to suppress it.
- A request is created and merged between closely spaced runs: each enabled event remains distinct, and the merged notification is not suppressed merely because the request was short-lived.
- A managed request's recorded source head changes while it remains open: this is an update, not another request-created event.
- A destination is renamed, disabled, or removed while an event is awaiting retry: the current invocation makes no attempt to it. When some notification destination remains enabled, the accepted configuration transition durably retires affected pending deliveries as skipped; they are never rerouted.
- All destinations are disabled for one or more invocations: no notification observation, ledger access, or endpoint access occurs, including no durable retirement or cutoff update. Re-enabling unchanged identities can resume pre-disable backlog and discover events from the disabled interval after the existing cutoff. Use a new destination name for a fresh cutoff. Previously running workers cannot learn a fully disabled configuration through the bypassed ledger; this is not a cross-worker emergency-stop mechanism.
- An older worker resumes after a newer enabled configuration was committed to notification state: it cannot restore old subscriptions or start another send; an already in-flight send cannot be recalled. Unprovable/divergent config ancestry safely defers notifications rather than choosing a winner by wall clock.
- An enabled destination's named credential environment variable is absent during reconciliation: Oiax skips the current attempt with a safe, actionable diagnostic, keeps the delivery retryable rather than terminally skipped, and preserves the underlying reconciliation result and exit status.
- One destination is slow or unavailable: it does not prevent attempts to other destinations or indefinitely block core reconciliation.
- Branch names, request titles, actor names, and other forge-provided text contain markup or control characters: the notification presents them as inert text appropriate to the destination.
- The forge omits optional details such as the merging actor: the message is still delivered with the required request identity and clearly marks the missing optional value.
- A request contains more commits than can be included safely: the message explicitly identifies the list as partial and links to the full request; unavailable commit details are disclosed rather than replaced with the live branch history.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Operators MUST be able to declare named notification destinations as part of Oiax's pinned, declarative configuration.
- **FR-002**: The first release MUST support Microsoft Teams channel, Slack channel, and generic webhook destinations. Email delivery is deferred and is not a supported destination type in this release.
- **FR-003**: Each destination MUST independently select event and managed request types. A newly configured destination defaults to `request-merged` events for both promotion and backflow requests; operators MAY opt into `request-created` events and MAY disable either promotion or backflow events for that destination.
- **FR-004**: Notifications MUST apply only to requests positively identified as Oiax-managed by existing managed-request identity rules. Oiax MUST NOT notify about, edit, close, or otherwise touch unmanaged requests.
- **FR-005**: A `request-created` event MUST be produced only after Oiax successfully creates a new managed request, not when it adopts, updates, or merely observes an existing request.
- **FR-006**: A `request-merged` event MUST be produced only after the forge reports an Oiax-managed request as merged, not when a request is merely closed, approved, mergeable, or synchronized out of band.
- **FR-007**: Both promotion and backflow managed requests MUST be supported, and every notification MUST label the request type unambiguously.
- **FR-008**: Every notification MUST include a stable event identity, repository identity, graph name, request type, source branch, destination branch, forge request identifier, event type, request link, and the time the event was observed. Optional actor details MAY be included when the forge supplies them.
- **FR-009**: The same logical event MUST have the same stable identity across retries and concurrent or repeated reconciliation runs.
- **FR-010**: After a delivery is confirmed successful, unchanged subsequent runs MUST NOT initiate another delivery of that event to the same destination.
- **FR-011**: If delivery success is ambiguous because the destination accepted or may have accepted a request without returning a conclusive response, Oiax MUST retain the stable event identity on retry and MUST document that an externally visible duplicate is possible.
- **FR-012**: Enabling notifications MUST NOT backfill historical managed-request events by default. Any future explicit backfill capability requires a separately bounded operator action.
- **FR-013**: A failure or timeout at one destination MUST NOT prevent eligible delivery attempts to other destinations.
- **FR-014**: Notification delivery failure MUST NOT roll back, block, misstate, or change the existing exit status of the underlying managed-request reconciliation result. The failure MUST remain visible and MUST be retried on a later reconciliation.
- **FR-015**: Delivery outcomes MUST distinguish at least successful, failed, retryable, and intentionally skipped events and MUST identify the affected destination by its non-secret configured name.
- **FR-016**: Each destination MUST name the environment variable containing its credential or secret-bearing address. Only the variable name is durable configuration; its value MUST be supplied at runtime and MUST NEVER appear in notifications, plans, logs, errors, or documentation.
- **FR-017**: All destination-provided and forge-provided text MUST be treated as untrusted and rendered so it cannot inject destination markup, commands, mentions, or additional fields.
- **FR-018**: Configuration validation MUST reject unsupported destination types, duplicate destination names, invalid event subscriptions, missing or invalid credential environment-variable names, and structurally invalid destination settings before mutation begins. Read-only validation MUST NOT require the referenced environment variable to contain a value.
- **FR-019**: When no destinations are enabled, Oiax MUST preserve existing behavior and MUST make no notification-related network calls. Such an invocation MUST NOT mutate notification state or claim to have durably retired subscriptions; unchanged identities resume from the last recorded state on re-enable, as described in Edge Cases.
- **FR-020**: Read-only operations MUST remain read-only. Planning MUST explain which notifications would be eligible without contacting a destination, while delivery MUST be confined to reconciliation.
- **FR-021**: Notification behavior and outcomes MUST be consistent across supported forge providers for equivalent observed managed-request state.
- **FR-022**: Notification event and delivery state MUST remain reconstructible from Git and forge state and MUST NOT require an Oiax-private state database.
- **FR-023**: Operators MUST be able to customize notification titles and bodies with graph-wide defaults and per-destination overrides, using documented event, request, branch, environment, and commit fields. Customization affects presentation only; it MUST NOT change event identity, request ownership, routing, or delivery state.
- **FR-024**: Operators MUST be able to assign notification environment names to configured branches. Unlabeled branches MUST fall back to their branch names; labels MUST NOT change the promotion topology or imply verified deployment completion.
- **FR-025**: Notification templates MUST expose the commits included in the actual created or merged request at that event, with commit identifiers and subjects. Retries MUST preserve that snapshot even if the source branch advances. Bounded or unavailable commit details MUST be identified explicitly and accompanied by the request link.
- **FR-026**: Templates MUST be declarative, validated before use, and unable to access runtime secrets or execute commands. Required event/request identification MUST remain available in a standard message footer or structured fields even when a custom template omits it. Rendering failures at delivery time MUST preserve core reconciliation results and remain safely diagnosable.

### Key Entities

- **Notification Destination**: A uniquely named, enabled or disabled delivery target with a supported destination type, selected event and managed request types, non-secret settings, and the name of an environment variable holding its runtime credential or secret-bearing address.
- **Notification Event**: A stable description of one lifecycle change for one managed request, including event identity, repository, graph, request type, edge, forge request identity, link, and observation time.
- **Delivery Record**: The observable result for one notification event and one destination, including its stable identity, outcome, attempt information, and safe diagnostic details. It contains no credential material.
- **Notification Presentation**: Graph-wide and per-destination title/body templates, environment labels for existing branches, and an immutable event-time commit summary. Presentation does not redefine the underlying event facts.
- **Managed Request**: An Oiax-owned promotion or backflow request identified by the existing machine-readable marker and branch relationship. Unmanaged requests are never notification sources.

## Out of Scope *(mandatory)*

- **OOS-001**: This feature does NOT merge, approve, close, deploy, render, or decide whether a managed request is safe to promote; those responsibilities remain with the forge, repository policy, delivery system, and humans.
- **OOS-002**: This feature does NOT notify about unmanaged pull requests, general pushes, check results, deployments, releases, or backflow conflicts.
- **OOS-003**: This feature does NOT accept commands or approvals from Teams, Slack, or webhook recipients; delivery is one-way.
- **OOS-004**: This feature does NOT provide historical event replay or bulk backfill in its initial scope.
- **OOS-005**: This feature does NOT guarantee real-time delivery independent of Oiax's invocation cadence; merge events are reported after Oiax next observes them.
- **OOS-006**: This feature does NOT promise exactly-once external delivery when a destination's response is ambiguous; it provides stable event identity and suppresses repeat delivery after confirmed success.
- **OOS-007**: This feature does NOT store credentials in `.oiax.yaml`, managed-request metadata, notification state, or any other durable repository artifact.
- **OOS-008**: The initial scope is limited to Teams, Slack, and generic webhooks. Email delivery is deferred, and a general third-party adapter marketplace is not included.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Under normal destination availability and normal load (at most 20 configured destinations, 10 due deliveries per destination and 100 total per observing run, no pre-existing retry/backfill backlog, and discovery completing within the configured scan budget), at least 95% of eligible notifications are available to recipients within 60 seconds after the reconciliation run that first observes the event completes, and at least 99% within five minutes. Acceptance fixtures invoke reconciliation every minute. Measure at least 100 predeclared deliveries per transport across bounded runs, counting missing messages as failures rather than excluding them; HTTP acceptance is not evidence of recipient visibility. Backlog recovery, capacity suspension, degraded history discovery and destination outages are tested separately for bounded progress/retry and are not claimed to satisfy these latency percentiles.
- **SC-002**: In acceptance testing, 100% of supported destination and event-type combinations include every required identity field and correctly distinguish promotion from backflow.
- **SC-003**: Across 1,000 repeated evaluations of unchanged state after confirmed delivery, no additional logical notification is sent to the same destination for the same event.
- **SC-004**: In fault-injection testing, failure of any one destination leaves 100% of eligible core managed-request actions, attempts to other destinations, and existing reconciliation exit statuses unaffected.
- **SC-005**: In security-focused acceptance tests, zero credential values, secret-bearing destination addresses, unintended mentions, or unintended active markup appear in plans, logs, errors, or delivered messages.
- **SC-006**: A first-time operator can configure one supported destination, validate it, and understand which events it receives in under ten minutes using repository documentation alone.
- **SC-007**: All supported forge providers pass the same lifecycle scenarios for created, merged, closed-unmerged, updated, retried, and repeated observations.
- **SC-008**: Template acceptance tests demonstrate custom environment-oriented wording, per-destination overrides, immutable event-time commit lists, explicit truncation/unavailability, and unchanged event identity across presentation changes.

## Assumptions

- Merge notifications for both managed promotion and backflow requests are enabled by default for an enabled destination; creation notifications require explicit opt-in, and either request type can be disabled per destination.
- Oiax remains invocation-driven rather than becoming a continuously running event receiver. Delivery latency therefore begins when reconciliation observes an event, not when the forge records it.
- The active configuration comes from the pinned ref defined by [ADR 0003](../../docs/adr/0003-pinned-configuration-ref.md); a notification destination proposed only on another branch has no effect.
- Managed-request identity and merged-request history remain durable forge evidence as described in [Architecture — Managed change requests](../../docs/architecture.md#managed-change-requests) and [ADR 0002](../../docs/adr/0002-content-based-divergence-detection.md).
- Teams, Slack, and generic webhook destinations are configured through a common product model. Each destination declares only the name of an environment variable, while the destination-specific credential or secret-bearing address is supplied through that variable at runtime. Email delivery is deferred beyond the first release.
- Any addition to the stable configuration contract or machine-readable plan requires an additive compatibility design and an ADR before implementation, per Constitution Principle XII and [ADR 0005](../../docs/adr/0005-config-api-v1.md).
- Provider implementations may expose different native fields, but the recipient-visible event meaning and required content remain provider-neutral; [ADR 0009](../../docs/adr/0009-azure-devops-forge-provider.md) establishes the existing cross-provider posture.
- Notification delivery is supplementary and best-effort in the same sense as other forge-side observability artifacts: failures are reported and retried later without changing the truth or exit status of the underlying reconciliation result.
