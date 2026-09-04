# Research: Managed Request Notifications

Date: 2026-09-04. Design for [#75](https://github.com/skaphos/oiax/issues/75), based on the four accepted [clarifications](spec.md#clarifications). These are proposed implementation decisions, not claims about shipped behavior. Email remains [#76](https://github.com/skaphos/oiax/issues/76).

## 1. Integration boundaries

**Decision:** Add pure notification selection and rendering under `internal/notification`, a coordinator under `internal/reconcile`, and explicit lifecycle/state capabilities at the forge boundary. Keep notification policy alongside, rather than embedded in, the engine's branch topology.

**Evidence:** `internal/cli/root.go:loadGraph` currently returns only the engine graph and template set. `Coordinator.Plan` lists open/merged promotion requests; `Apply` discards request creation results at both creation sites. `engine.ChangeRequest` lacks URLs, lifecycle times, and logical backflow source. `Forge.CreateRequest` currently treats adoption as success without identifying whether it created a request. These must change at the coordination/provider boundary to observe real lifecycle events.

**Rationale:** Pure selectors can take a captured observation time and return deterministic decisions without seeing credentials, HTTP, or Git. Existing branch-plan actions and exit-code calculation remain independent.

**Alternatives:** Putting send actions in `engine.BuildPlan` entangles delivery availability with branch-promotion exit semantics. Scraping logs cannot recover lost events. Running a daemon changes the product's invocation model.

## 2. Durable delivery state and concurrency

**Decision:** Store an ordinary Git notes tree at `refs/notes/oiax/notifications/v1/<graph-key>`. Each snapshot is a commit whose sole parent is the observed current tip. The graph key is SHA-256 of canonical forge repository identity plus graph name. Persist state using an explicit expected-old-object-ID comparison; a conflict requires rereading and reapplying the intended transition. Never overwrite a stale snapshot or rewind the history.

GitHub documents notes in its [reference API](https://docs.github.com/en/enterprise-cloud%40latest/rest/git/refs). Azure documents note permissions and stale-object rejection in [Update Refs](https://learn.microsoft.com/en-us/rest/api/azure/devops/git/refs/update-refs?view=azure-devops-rest-7.1). For portable writes, use the authenticated Git transport with an explicit lease, whose expected-value behavior is defined by [git push](https://git-scm.com/docs/git-push). GitHub's REST `sha`/`force` update alone is not an expected-old-value operation.

**Rationale:** The notes tree preserves reconstructible state in Git, permits atomic competition across CI runs, and creates neither a long-lived branch nor a release tag. Use a standard notes attachment containing the bounded ledger JSON; `git notes --ref=<ref> show <anchor>` can inspect it. The anchor is the pinned configuration commit at initialization. No local checkout changes are needed; temporary object construction uses an isolated index/object workspace.

**Alternatives:** Local files and CI caches do not survive independently scheduled workers reliably. A private database conflicts with the constitution. PR comments have no general compare-and-swap or uniqueness guarantee; updating them can also create notification noise. A state branch conflicts with the long-lived-branch prohibition. Tags interfere with release and tag-triggered workflows.

**Limits:** Neither a lease nor a Git receipt can make an external HTTP send atomic. A crashed or suspended worker, expired lease, or failed receipt write can leave delivery uncertain. Such retries retain the event ID and may duplicate a message. Persisted successful delivery is terminal. General concurrency does not rely on CI serialization, but documented single-run concurrency reduces ambiguous recoveries. See [ADR 0014](../../docs/adr/0014-notification-delivery-ledger.md).

**Authority correction (C1):** This is a proposed mechanism, not a finding that
an explicit-lease write to the notes namespace satisfies Constitution XI. T001
must establish and record that conformity before the writer is implemented.
The plan's XI gate is PENDING. An ADR cannot grant an exception; if the operation
conflicts, redesign it or seek a separate explicit governance change.

## 3. Activation, event discovery, and creation provenance

**Decision:** A destination's first successful ledger initialization records its activation time before any notification is sent. Events whose forge occurrence time precedes that instant are ineligible; an already-open request can still generate a later merge notification. This is the precise no-backfill boundary. Failed initialization sends nothing, warns, and leaves branch reconciliation usable.

**Revision ordering (A1):** Persist the last accepted config OID and normalized
policy digest. Equal revisions agree or report a mismatch; only proven strict
descendants advance policy and subscription epochs atomically. Ancestor workers
cannot restore old policy; divergent/unknown ancestry defers notifications until
ordered history is supplied. Recompute ancestry after CAS conflict. New sends
require the accepted revision, while late receipts remain monotone. A reviewed
descendant commit reverting content is supported; pinning an older OID is not a
notification-policy rollback. See [the revision rules](data-model.md#configuration-revision-ordering).

For creation recovery, put an immutable notification-origin block into the initial PR create body, separate from the existing ownership marker. It records an operation ID, creation observation time, graph, pinned config OID, and logical source/target branches. Azure's subsequent property copy is supplemental; the initial body is required because the process can die after POST succeeds. An adopted request may recover the original event from this block but never generates a new creation event just because it was adopted. Requests without origin can generate merge events using existing ownership checks; they cannot manufacture creation provenance.

**Evidence:** Both forge implementations can fail on follow-up work after the initial PR creation succeeds. Azure stores the existing marker in properties as well as the body. Marker replacement preserves surrounding text. Backflow request heads are synthetic `oiax/` refs, so parsing their names would violate the explicit-topology rule; use the captured logical edge instead. For legacy backflow requests without origin, retain the actual source ref, set logical source unavailable, and report that limitation rather than guessing.

**Decision:** Add dedicated paginated lifecycle discovery with occurrence/updated timestamps and stable request IDs. Do not reuse the baseline lookup as an event stream: both providers use a 180-day lookback, Azure bounds lists at 100 pages of 100, and GitHub's current implementation has no equivalent fixed page-count limit. Persist a known-request inventory; poll known open requests by ID, discover new requests, and scan completed history for requests created and merged during downtime. An incomplete scan never advances a completeness watermark. Provider scan contracts and retry rules are in [interfaces](contracts/interfaces.md).

**Alternatives:** A timestamp derived only from the current config commit can misclassify history after unrelated configuration edits. Inferring creations from first sighting misreports adoption. Looking only at open PRs misses short-lived requests. Treating the newest baseline PR as the only merge loses intermediate events.

## 4. Retry policy and bounded work

**Decision:** Keep undelivered events pending without automatic age expiry. Attempt a delivery at most once per run. Exponential retry spacing starts at one minute and caps at one hour; `Retry-After` sets a later eligible time, capped at 24 hours with a diagnostic for larger values. Missing secrets and configuration-dependent remote failures remain pending with a one-hour delay. A disabled/removed subscription receives no current attempt and is durably skipped only when enabled notification processing records its retirement; changing a destination name or transport identity starts a new activation, never reroutes old pending messages.

**Global disable (I1):** All-disabled invocations make no notification I/O, even
to record retirement or a config revision. Re-enabling the same identity resumes
the last durable subscriptions/backlog; eligible disabled-interval events can be
discovered against that existing cutoff. Re-enable starts a new epoch only after
a disable was durably recorded while another destination kept processing active.
Use a new destination name for an intentional fresh cutoff. Unrecorded disable
does not fence already running workers; there is no global immediate-stop claim.

Initial hard bounds: 20 configured destinations; 10 attempts per destination and 100 total per run; one in-flight send per destination; at least one second between sends; 10-second HTTP timeout; 120-second aggregate notification stage; 120-second claims renewed immediately before a send; 100 lifecycle pages of 100 requests per run; 8 MiB ledger and 50,000 delivery records, whichever is reached first. Scan continuations and pending events survive limits. Capacity exhaustion warns and suspends new notification work; it never silently drops receipts or changes the core exit result. Increasing limits requires a future deliberate configuration/implementation change rather than an automatic burst.

**Rationale:** This preserves the accepted later-retry requirement without an unapproved expiry rule. Admission control bounds new outward work. No receipt garbage collection in v1: deleting evidence would allow replay by old observations. Ledger growth and denied notes writes must be visible operational limitations.

**Alternatives:** Infinite in-process retries hang CI; expiring events after an arbitrary week silently weakens recovery; unbounded scans and catch-up sends violate the outward-action bound.

## 5. Transport choice and prior art

**Decision:** Implement three small standard-library HTTP adapters. All destination URLs come from the specifically named runtime environment variable. They are secret values; retain the whole callback URL and never print it. Support HTTPS only, normal certificate verification, no redirects, a bounded response body, and fixed diagnostic codes instead of arbitrary response/error text. Do not send forge authentication headers to destinations.

**Teams:** Target Workflows with the webhook trigger in `Anyone` mode. Send an Adaptive Card message envelope, use non-Markdown TextRuns for untrusted values, and a fixed-label OpenUrl action for a validated PR URL. Tenant-authenticated OAuth triggers are beyond this initial secret-URL transport. Microsoft reports the old Office 365 connector retirement completed in May 2026; legacy connector URLs are not the design target. Workflows are user-owned, so deployment guidance includes a co-owner. Sources: [retirement announcement](https://devblogs.microsoft.com/microsoft365dev/retirement-of-office-365-connectors-within-microsoft-teams/), [Teams trigger](https://learn.microsoft.com/en-us/connectors/teams/#microsoft-teams-webhook), [TextRun](https://learn.microsoft.com/en-us/adaptive-cards/schema-explorer/text-run), [ownership](https://learn.microsoft.com/en-us/microsoftteams/platform/webhooks-and-connectors/what-are-webhooks-and-connectors).

**Slack:** Use an app incoming webhook bound to its installed channel; success requires HTTP 200 and body `ok`. Use `plain_text` blocks and static fallback text; control links separately. Keep section text within 3,000 characters and field text within 2,000. Honor 429 and `Retry-After`; do not assume Slack deduplicates on our event ID. Sources: [incoming webhooks](https://docs.slack.dev/messaging/sending-messages-using-incoming-webhooks/), [rate limits](https://docs.slack.dev/apis/web-api/rate-limits/), [section blocks](https://docs.slack.dev/reference/block-kit/blocks/section-block/).

**Generic webhook:** JSON event schema v1, `X-Oiax-Event-ID` header, success on any 2xx, one-way HTTPS. No custom headers, arbitrary payload templates, executable hooks, or OAuth negotiation in this release. Human title/body templates are supported within the fixed envelope. A receiver may deduplicate using the event ID.

**Acknowledgment:** Teams/ordinary webhooks can accept asynchronously. A 2xx receipt records endpoint acceptance, not proof a human saw the message. Preserve SC-001 as an end-to-end acceptance measurement with real destination fixtures; do not claim the HTTP receipt alone proves visibility.

**Prior art:** `tools/ECOSYSTEM.md` is absent in this checkout. [Shoutrrr Teams code](https://raw.githubusercontent.com/containrrr/shoutrrr/main/pkg/services/teams/teams.go) and [its Workflows issue](https://github.com/containrrr/shoutrrr/issues/446) do not satisfy this design's current Workflows, cancellation, secret handling, and rendering contracts without changes. [Notify](https://github.com/nikoksr/notify) provides broad transports but its overview does not establish these guarantees. Broad SDK coverage offers little benefit for three small adapters with email deferred. This is a decision for this scope, not a claim that those libraries are generally unsuitable.

## 6. Presentation templates and environment language

**Decision:** Following the user's planning refinement, add graph-wide title/body
templates with per-destination overrides and an explicit map from configured
branches to notification environment names. This supports “These commits were
promoted to the test environment.” Labels are presentation metadata, not new
topology or evidence of deployment completion. Expose immutable event-time commit
summaries, never a fresh log of the moving source branch.

Reuse the repository's existing stdlib template discipline from
[ADR 0011](../../docs/adr/0011-templatable-request-text.md): a closed context,
curated deterministic functions, pinned template files, parse/sample validation,
and no environment/network/command access. The notification context is separate
from the PR-body context. Render title/body as inert text; adapters own escaping
and append required identity/link/truncation information independently. Generic
webhook facts remain schema-controlled with a customizable `message` member.

Capture commits from the forge's event-specific PR revision and bound the included
list at 100 commits and subjects at 200 runes. GitHub provides a PR commits endpoint
and detail count; Azure's paged commit count must not be mistaken for a total.
Use explicit completeness metadata; unavailable history is not an empty change.
Delivery retries retain captured facts. Persist each destination's rendered message
before first dispatch so a template edit cannot rewrite an ambiguous retry.
Pass `DeliveryPayloadV1 {schemaVersion, event, message}` directly to the adapter,
joining those immutable facts with its persisted title/body; adapters never
read state or re-render. Every transport's fixed identity section includes all
FR-008 fields, explicitly the request identifier and original observed-at time,
even if custom text omits every context field.
Sources: [GitHub PR commits](https://docs.github.com/en/rest/pulls/pulls#list-commits-on-a-pull-request),
[Azure PR commits](https://learn.microsoft.com/en-us/rest/api/azure/devops/git/pull-request-commits/get-pull-request-commits?view=azure-devops-rest-7.1).

Source commit SHAs are the request's review history, not necessarily the SHAs
landed on the destination after squash/rebase. The merge-result OID is separate.
Verify historical snapshot behavior on both forges before release; if an endpoint
returns a mutable view, use captured source/base OIDs or mark details unavailable.

**Alternatives:** Fixed wording cannot satisfy the user's refinement. Arbitrary
payload templates let presentation corrupt schema/identity. Current branch history
can attribute later commits to an earlier merge. Building a new template language
duplicates an existing repository facility without benefit.

## 7. Public compatibility and safety

**Decision:** Add optional `spec.notifications` with exported pure validation/defaulting. Add an optional informational `notifications` member to plan JSON v1 at the rendering layer; no notification-only actions enter the branch engine's action list or change detailed exit codes. Carry origin in a separate marker; preserve the existing v1 ownership marker and older configs. New fields are additive; no existing surface is deprecated. See [ADR 0013](../../docs/adr/0013-notification-configuration-contract.md).

Restrict endpoint access to HTTPS global-unicast addresses by default; private network receivers require explicit `allowPrivateNetwork: true` per destination. Always reject loopback, link-local, multicast, unspecified, and metadata-service addresses, including redirects and DNS rebinding. Validate resolved addresses at connection time and retain TLS hostname verification. This permits deliberate internal webhooks without allowing PR text to select a destination. Tests may inject a private HTTP client/server internally; there is no insecure production flag.

**Research disposition:** Product alternatives have concrete proposals, including
revision ordering, all-disabled resumption and payload handoff. Notes-write
constitutional authority remains unresolved under C1/T001; provider capability
and real-channel tests are additional implementation release gates. SC-001 now
uses the same bounded normal workload and predeclared sampling protocol in the
specification, plan and quickstart; backlog recovery is tested separately. No
notification has been sent and no remote ref was created during this planning work.
