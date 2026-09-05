# Implementation validation evidence

## Current increment — reproducible verification gates

2026-09-05: `notifications:verify` passes locally. It runs the production
notification packages with race/shuffle and atomic coverage, verifies the coverage
checker itself, gates every production notification package independently at 85%,
and runs notification provider/marker/coordinator/compiled-CLI suites. The only
excluded notification package is the explicitly test-only `notificationtest`.
Missing/empty packages, duplicate/malformed coverage and an under-covered new
production subpackage fail the checker. Existing Linux/macOS/Windows `Test` jobs
now invoke this exact task without changing their names or repository permissions.

Measured coverage: model 92.53% (471/509), delivery 87.90% (109/124), store 86.70%
(176/203). The verification task's CLI suite passed in 116.942 seconds.

`notifications:fuzz` passed five ten-second, two-worker campaigns: templates
205,258 executions; ledger 674,188; endpoint parser 300,059; origin 406,071;
combined config/origin/commit payload parser 47,667. The existing ledger codec
fuzzer is reused rather than duplicated. No failing regression corpus or captured
credentials resulted.

The full repository `go test -race -shuffle=on ./...` passed (CLI 176.492 seconds;
coordinator 203.108 seconds) with process-local test Git signing/hook overrides,
not repository configuration changes. `task lint staticcheck vuln build reuse`
passed: zero lint issues, no govulncheck findings, successful stock binary build,
and REUSE compliance. Cross-OS execution is delegated to the existing CI matrix;
this machine's results are not represented as Windows/Linux execution.

## Earlier increment — read-only preview and safe recovery

2026-09-05: enabled policy adds an optional renderer-owned `notifications` member
to the single JSON v1 plan document. Engine fields/actions and detailed exit
calculation are unchanged. Text and CI summaries include equivalent decisions.
Preview reads notes and bounded lifecycle pages, uses no endpoint lookup, does
not write/claim/accept policy, and reports uninitialized/unavailable/incomplete/
complete observations. New creations are conditional and have no fabricated IDs.
Delivered, pending, filtered, inactive and retry-not-due are distinct. Revision
ancestry checks report stale/unordered/mismatched policy without advancing state.

Delivery diagnostics now contain safe destination/reason/action fields, including
persisted success versus uncertain acceptance. Unknown errors are never rendered.
Missing notes warns about the new cutoff and restoring prior receipts; corrupt
or unavailable notes suspend sends. Retrying preserves persisted wording/facts.
Free-form presentation and commit subjects redact non-request HTTP(S) addresses;
the independently validated request link remains available. Runtime rendering
overflow retains the event and does not starve other destinations.

Evidence: `go test -race -shuffle=on ./internal/reconcile ./internal/cli
./internal/notification/... -run 'TestNotification|TestRender|TestTemplate'`
passed (CLI 94.087 seconds). The expanded compiled CLI fault matrix covers both
forges with missing credentials, corrupt/newer-version ledger, unavailable notes,
incomplete discovery and lost notes; plans perform no remote writes or sends.
Prior mixed-receiver/core-exit 0/1/3 and concurrency/uncertain-receipt suites remain
passing. Canary checks cover CLI output, remote ledger and received payloads;
safe display-address and transport-code tests pass. Store tests cover capacity,
CAS conflict, malformed state and monotonic receipts. Lint passed with zero issues.

Coverage measured before CI wiring: notification 92.5%, delivery 87.9%, store
86.7%. These are separate package figures, not an aggregate masking a weak package.
The final fault-only binary rerun also passed (23.491 seconds), adding runtime
template overflow alongside a healthy destination on both forges.

## Earlier increment — immutable commit enrichment

2026-09-05: snapshot enrichment is wired outside ledger CAS callbacks. First
admission fixes bounded commit facts and environment labels; rereads and retries
cannot replace them. Enrichment has a ten-second page budget and finalization a
shared two-minute budget. Enrichment failure preserves the lifecycle event with
explicit unavailable details.

GitHub reads completed PR review membership with authoritative detail totals,
bounded retrieval and revision rechecks. A failed membership read can fall back
to an immutable compare only when a two-parent merge proves its pre-merge base
and reviewed source head. No branch-name history fallback is used. GitHub's API
does not establish the first creation iteration, so GitHub creation summaries
remain explicitly unavailable, including when pre-POST hints match today's head.
Azure uses server-owned first-iteration source/target OIDs for creation (overriding
raced pre-POST hints), and completed last-merge source/target OIDs for merges.
Its history query pins both versions as commits and bounds lookahead to 101;
page counts are never presented as authoritative totals.

Both paths cap admitted summaries at 100, subjects at 200 runes, strip controls
and untrusted commit URLs, and preserve merge-result identity separately from
reviewed source SHA. The source APIs are the [GitHub PR commit endpoint](https://docs.github.com/en/rest/pulls/pulls#list-commits-on-a-pull-request),
[Azure first iteration](https://learn.microsoft.com/en-us/rest/api/azure/devops/git/pull-request-iterations/get?view=azure-devops-rest-7.1),
and [Azure commit-version query](https://learn.microsoft.com/en-us/rest/api/azure/devops/git/commits/get-commits?view=azure-devops-rest-7.1).

Automated evidence: both-provider snapshot fixtures cover source advancement,
deleted refs, squash/rebase identity, unavailable reads and over-100 bounds;
immutable merge fallback has a dedicated proof fixture. Runtime tests prove
event/label immutability and preservation after enrichment failure. A compiled
CLI test on each provider delivers configured environment wording with the full
fixed facts and no repeat send. The notification race/shuffle suite passed
(CLI 75.832 seconds); these are local fixtures, not live adoption evidence.

## Earlier increment — routing and custom presentation

2026-09-05: the maintainer explicitly deferred live acceptance testing to adoption
after release. T076/T077 are out of the current implementation scope, not passed
tests. No live messages, provider merges, repository settings or release actions
are authorized by this decision. Local automated tests remain required.

Custom notification title/body slots now compile at configuration load from the
pinned source, validate against all four lifecycle/request combinations, and
render through a closed fact-only context. Explicit empty strings override;
missing slots inherit. Field validation also visits inactive branches. Times are
captured RFC3339 strings, not objects exposing methods. The deterministic helpers
match the existing request-template `trunc`/`shortSHA` semantics without importing
the request-template package (which depends on notification ownership markers).
Ranges are bounded to `.Commits`; template inclusion, method calls, assignment,
`with` and unbounded/integer ranges are not exposed. Source is capped at 1 MiB,
execution output at 12 KiB, and inert titles at 256 runes. Error messages identify
the configuration slot without echoing template source or execution values.

Routing tests cover all 12 transport/event/type combinations, destination
identity changes, empty/disabled/removed selections, new subscriptions, template
edits and secret-variable identity preservation. Recorded retirement differs from
all-disabled zero-I/O processing; resumed old epochs admit disabled-interval
events, while new names and recorded re-enables use fresh cutoffs.

A two-destination runtime regression verifies different saved wording for the
same event and byte-equivalent retries after a descendant configuration edit,
without replaying the successful destination. Existing adapter tests independently
retain required facts with constant and empty text.

Verification: focused race/shuffle routing, rendering, dispatch and pinned-file
tests passed; lint passed with zero issues. A 10-second, two-worker
`FuzzNotificationTemplate` campaign passed 239,686 executions. Full notification
compiled-CLI regressions also passed: `go test -race -shuffle=on
./internal/notification/... ./internal/reconcile ./internal/cli -run
'TestTemplate|TestRouting|TestNotification|TestRender|TestFixedFacts'`
(CLI 75.092 seconds). This includes the prior merge and creation binary matrices.

## Earlier checkpoint — optional creation alerts and recovery

2026-09-05: T039–T048 complete the local US2 creation-alert milestone. Optional
PR-created notifications are now wired through both forges and the CLI.
The merge-alert integration checkpoint was committed as `e404360`
before this increment. The full feature remains **not merge-ready or deployable**:
custom templates, immutable commit enrichment, previews/diagnostics, operator
documentation and live release acceptance are still later phases.

Creation provenance is a separate, non-templatable, HTML-escaped JSON comment in
the initial POST body. It does not modify the v1 ownership marker or establish
ownership itself. The parser enforces the 4 KiB encoded block bound, exact field
names, unique keys/blocks, valid OIDs/times and bounded text. A valid origin must
match existing provider-verified ownership; backflow retains its logical source
separately from its actual candidate branch. Baseline updates preserve origin.

The internal forge create contract now returns explicit created/adopted outcomes.
An actual successful POST survives a later label/property failure in the returned
outcome. An adopted PR gets only its own recovered origin, never the attempted
operation's origin; a legacy survivor stays without provenance. Both providers
recover origin from the full initial body without requiring supplemental Azure
properties. Pre-POST source/base OIDs remain hints, not verified commit membership.

The CLI durably activates notifications before core apply, then retains outcomes
incrementally at promotion and backflow create sites. Finalization confirms those
IDs directly before bounded discovery, so an incomplete scan does not suppress
an actual create. A later invocation can recover from the original body if all
notification reads failed after POST. Core errors remain authoritative; metadata
failure or a later failed create still returns exit 1 while eligible notification
work proceeds. Creation/merge have distinct IDs and independent opt-in cutoffs.

A deterministic regression exposed whole-second timestamp precision at first
activation. Original pre-POST evidence now disambiguates eligibility only inside
that same second; the forge occurrence timestamp remains unchanged, and an
operation before the cutoff still produces no historical backfill. The rule is
documented in the data model and covered with a fixed clock plus coarse-timestamp
compiled CLI fixtures.

Evidence for this increment:

| Check | Result |
|---|---|
| Origin parser/format/ownership/preservation tests | PASS; first failed on the missing codec before implementation |
| Shared creation conformance on both providers | PASS, 20 cases: both request types × success, metadata failure, POST failure, adoption with origin, legacy adoption |
| Creation/recovery/default opt-in runtime tests | PASS for promotion and backflow; independent creation/merge IDs and no duplicate sends |
| Partial backflow apply outcome | PASS, original logical source/config and pushed source OID retained alongside the original follow-up error |
| Whole-second activation cutoff regression | PASS after reproducing the missed first-run creation; original timestamps unchanged |
| Compiled CLI creation matrix | PASS, eight both-forge cases including initial activation, partial core progress, failed metadata, failed POST, unavailable reads then fresh-process recovery, source advancement and later merge |
| `go test -race -shuffle=on ./internal/cli -run '^TestNotification(Creation\|Merge)Binary$' -count=1` | PASS, all 20 merge/creation subprocess scenarios |
| `go test ./internal/forge/marker -run '^$' -fuzz '^FuzzNotificationOrigin$' -fuzztime=10s -parallel=2` | PASS, 577,422 executions, no failing corpus |
| `go test -race -shuffle=on ./...` with process-local Git settings | PASS |
| `go -C tools tool task lint` | PASS, 0 issues |
| `go -C tools tool task build` | PASS |
| `go -C tools tool task verify-generated` | PASS, reference unchanged |
| `reuse lint` | PASS, all 302 files compliant |
| `git diff --check` | PASS |

Tests used the workspace-safe cache and local-only fixture permissions described
below. Live provider writes, channel visibility, cross-OS execution and latency
acceptance were not run. No push, live notification send or repository-setting
change was performed.

Next: routing-generation coverage, custom notification templates, pinned-file
validation and immutable event-specific commit snapshots (US3). Full read-only
notification preview and operational diagnostics (US4) remain afterward.

## Earlier checkpoint — basic merge-alert integration

2026-09-05: T019, T036 and T038 complete the local US1 merge-alert milestone.
The remaining US1 integration work now has executable local evidence.
The full notification feature is still **not merge-ready or deployable**:
creation provenance, custom presentation, event-time commit enrichment,
preview/diagnostics, operator documentation and live release acceptance follow.

`internal/cli/notification_merge_test.go` builds `cmd/oiax` with a narrowly scoped
Go build overlay. It substitutes fixture-only forge construction and notification
connection setup; no production source on disk or stock-binary configuration
surface is changed. Real GitHub/Azure provider parsers, pinned configuration,
planning/apply, observation, rendering, adapters, scheduling, notes CAS and exit
handling execute in fresh subprocesses against local HTTPS and bare Git. TLS
hostname verification remains enabled using a fixture CA. See the fixture
[boundary README](../../../internal/cli/testdata/notification-binary/README.md).

The independent US1 matrix is:

| Scenario | GitHub | Azure DevOps | Evidence |
|---|---|---|---|
| Promotion and backflow merges through Teams, Slack and generic webhook | PASS | PASS | Six subprocess cases, two accepted messages each; stable webhook ID/header and durable saved messages/receipts |
| First activation excludes historical merges | PASS | PASS | No deliveries before a post-activation merge |
| Short-lived requests discovered between invocations; deleted backflow source ref | PASS | PASS | Provider detail reads and default request-type routing in subprocess cases |
| Closed-unmerged and unmanaged requests excluded | PASS | PASS | Exactly the two eligible merge deliveries; unexpected forge mutations fail the fixture |
| Repeat evaluation after accepted delivery | PASS | PASS | Three fresh CLI processes per transport retain byte-equivalent terminal receipts and never resend |
| 1,000 unchanged repeat evaluations per request type | PASS | PASS | Four runtime cases rebuild the coordinator runtime against the same store; one accepted send each, also excluding historical backfill |
| Core exits 0/1/3 survive failed delivery | PASS | PASS | Six subprocess fault cases, one retryable receiver and one independently accepted receiver |
| Partial core progress survives a later create failure | PASS | PASS | First PR POST succeeds; second fails; original apply error/exit 1 retained while notification finalization proceeds |
| Plan detailed exits 0/2/3 and JSON compatibility | PASS | PASS | Exact core plan JSON retained; read-only invocation sends nothing and does not advance notes, including pending merges |
| Retry not yet due after process restart | PASS | PASS | Persisted retry timestamp/attempt and terminal success remain unchanged |
| Checkout and remote branch/tag preservation | PASS | PASS | Clean checkout and identical remote branch/tag refs before/after delivery |
| Credential isolation | PASS | PASS | No forge Authorization header at receiver; endpoint/forge canaries absent from stdout, stderr and decoded ledger, including failed receiver response text |

The 1,000-repeat cases use the runtime's memory-store fixture; the subprocess
cases separately prove real bare-remote persistence. This does not claim 1,000
HTTP or live-provider invocations. Existing lifecycle conformance additionally
covers old open requests merging after activation, and existing concurrency and
failure tests cover cancellation, competing workers and uncertain receipts.

Verification for this increment:

| Check | Result |
|---|---|
| `go test ./internal/cli -run '^TestNotificationMergeBinary$' -count=1 -v` | PASS, all 12 subprocess scenarios |
| `go test -race -shuffle=on ./internal/cli ./internal/reconcile ./internal/notification/... ./internal/forge/github ./internal/forge/azuredevops ./internal/forge/forgetest` | PASS |
| `go test -race -shuffle=on ./...` with the process-local Git settings below | PASS |
| `go test -race -shuffle=on ./internal/reconcile -run '^TestNotificationMergeDeliveryAndRepeat$' -count=3 -v` | PASS, all four 1,000-repeat scenarios on each of three runs |
| `go -C tools tool task lint` | PASS, 0 issues |
| `go -C tools tool task build` | PASS, stock binary (without fixture overlay) |
| `go -C tools tool task verify-generated` | PASS, CLI reference unchanged |
| `reuse lint` | PASS, all 292 files compliant |
| `git diff --check` | PASS |

Commands used `GOCACHE=/private/tmp/oiax-notification-go-cache` and approved local
fixture networking. Full-suite Git signing/hooks overrides are the same
process-local settings documented below. Initial sandbox attempts failed because
the default cache was inaccessible and local listeners were denied; those are
not passing test results. No host trust store or Git configuration was changed.

Live Teams/Slack visibility, real-forge notes permissions/CAS, Linux/Windows
execution of the new subprocess harness, delivery latency sampling and timed
operator setup were **not run**. HTTP acceptance is not recipient visibility.
No external receiver, remote PR, external notes ref or repository setting was
mutated in this increment. Validation was performed locally before commit.

Next: implement optional PR-created notifications, starting with immutable
creation-origin parsing and explicit created/adopted outcomes, then provider
recovery and incremental apply capture. Custom templates/commit snapshots and
read-only notification diagnostics follow; the original full feature's live
release gates remain separate.

## Earlier checkpoint — lifecycle completeness and bounded delivery

2026-09-05: work-in-progress implementation checkpoint for PR #77,
branch `spec/promotion-notifications`. The PR is marked ready for review, but the
feature checklist remains incomplete and it is **not merge-ready or deployable.**
T016 and T020–T022 now establish equivalent lifecycle discovery contracts for
both forges. GitHub creation and merge scans use their respective authoritative
timestamps, so an old request merged after activation and a request created and
merged between runs are both retained. Full detail hydration, overlapping page
movement with stable-ID deduplication, Azure frozen-interval partitioning and
repeated ID-set verification all fail closed: deleted/denied details, unstable
enumeration, cross-origin continuation, and dense same-timestamp intervals keep
partial progress explicitly incomplete.

T017–T018 and T025–T028 complete the constrained HTTP and v1 adapter slice.
Connection-time resolution rejects every mixed DNS answer when any address is
unsafe, dials only validated numeric addresses, and retains the original URL
hostname for TLS verification. Private unicast remains a generic-webhook-only
opt-in; redirects, oversized responses, oversized outbound payloads, missing
secrets, unsafe request links, and raw transport details reduce to bounded safe
outcomes. Deterministic checked-in JSON goldens cover Teams Adaptive Card 1.2,
Slack plain-text blocks, and the flat generic schema, including every FR-008
fact, explicit request ID/observed time, commit unavailability, inert hostile
text, fixed safe links, generic event-ID header, and transport-specific acks.

T023–T024 and T035 complete merge normalization and built-in presentation.
Provider-authoritative merged state/time and current topology route promotion,
explicit backflow, and legacy backflow facts without inferring ownership, merge,
or a deleted logical source. Default subscriptions admit both managed request
types only at/after their durable activation cutoff. The four event/type wording
combinations distinguish ready-for-review from completed branch promotion and
never claim deployment. Required facts reject control text, oversized identity,
cross-repository/query/fragment links, preserve explicit unavailable/truncated/
unknown commit state, and include Azure organization/project/repository IDs.

T030 completes independent lifecycle observation. Activation is durable before
eligibility; known-open direct reads and creation/merge catch-up scans remain
independent. Partial records/events and opaque cursors commit without advancing
an incomplete watermark, failed direct refresh retains known-open state, and the
combined provider work remains below the 100-page bound. Discovery failure does
not block already pending dispatch. A stale runtime stops before lifecycle,
secret resolution, sender creation, or delivery once a descendant config is
accepted; revision evidence remains inside each freshly reduced CAS transition.

T034 completes the adversarial concurrency matrix. Simultaneous initialization
converges, competing claims send once, and a forced conflict re-runs revision
comparison against the newly committed config instead of reusing stale evidence.
Equal-OID digest mismatch and divergent/unknown ancestry leave durable state
unchanged. A descendant policy content revert creates a fresh generation without
reviving retired work or changing event identity. After a suspended sender's
claim expires, a replacement attempt can fail durably and the proven late
acceptance still wins monotonically as the terminal two-attempt receipt.

T037 confirms the compatibility bypass: no-policy, all-disabled, explicitly
empty event selection and explicitly empty request-type selection produce the
same text/JSON plan and reconcile output while notification capability traps
remain untouched. The passing depguard lint retains `internal/engine`'s ban on
notification/effect dependencies.

T029 and T031–T033 now complete the bounded dispatcher slice: runtime mutations
use caller-observed CAS revisions, messages persist before claims, claims renew
against the current accepted config/generation, destination workers send in
parallel while remaining sequential and one-second-spaced within a destination,
and accepted sends with failed receipt persistence report explicit uncertainty.
Mutating CLI finalization initializes only the exact notes ref, continues pending
dispatch after incomplete observation, and logs notification deferral without
changing the core apply error or exit 0/1/3. All-disabled and canceled invocations
bypass capabilities before endpoint lookup or outbound work.

New regression coverage includes competing workers, a real local bare-remote
notes initialization with no `oiax/` branch/tag creation, 1,000 repeated
evaluations after durable success, due/lease/pacing fences, 20-destination fair
allocation under the 100-run/10-destination bounds, independent destination
failure, a blocked slow sender alongside a successful sender, and accepted-send
receipt uncertainty. No live receiver or external repository was contacted.

Verification on this checkpoint:

| Check | Result |
|---|---|
| `go test -race -shuffle=on ./...` with the documented process-local Git settings | PASS |
| `go test -race -shuffle=on ./internal/forge/github ./internal/forge/azuredevops ./internal/forge/forgetest` | PASS |
| `go test -race -shuffle=on -count=10 -timeout=60s ./internal/notification/delivery` | PASS |
| `go test -race -shuffle=on -count=10 ./internal/notification ./internal/notification/delivery` | PASS |
| `go test -race -shuffle=on -run 'TestNotification(Observe|Stale)' ./internal/reconcile` | PASS |
| `go test -race -shuffle=on -count=25 -run 'TestNotification(Concurrent|Conflict|Activation|Descendant|Expired|DispatchCompeting)' ./internal/reconcile` | PASS |
| `go test -race -shuffle=on -run TestNotificationDisabledPreservesLegacyOutput ./internal/cli` | PASS |
| focused notification/reconcile race tests, repeated 10 times after concurrent dispatch | PASS |
| `go -C tools tool task lint` | PASS, 0 issues |
| `go -C tools tool task build` | PASS |
| `go -C tools tool task verify-generated` | PASS |
| `reuse lint` | PASS, all 288 files compliant |
| `graphify update .` | PASS: 1,840 nodes, 5,167 edges, 112 communities |
| `graphify diagnose multigraph --max-examples 1` | PASS: no missing/dangling endpoints, self-loops or collapsed edges; raw producer loss not measured |
| `git diff --check` | PASS |

US1 remains incomplete: the rest of merge integration, built-binary
notification scenarios and independent acceptance evidence are still unchecked.
Creation provenance, custom templates, immutable commit enrichment, preview and
diagnostics remain later phases.

Resume in this order:

1. Complete T019 merge integration, especially unmanaged/closed exclusion,
   partial core failures and unchanged exit 0/1/3 isolation.
2. Finish T030 and T034–T038: adversarial observation/config-order races,
   default presentation, built-binary merge scenarios, exact core exit evidence
   and the independent US1 matrix. Do not contact live receivers in unit tests.
3. Continue creation provenance, custom templates, immutable commit enrichment,
   preview/diagnostics and release validation in task dependency order. Refresh
   `graphify-out/` after later code changes, then recheck coverage, race tests,
   lint and generated artifacts.

Namespace-only ADR 0015 is accepted following explicit maintainer approval of
`notes/oiax`; broader ADRs 0013/0014 remain proposed. No live receiver sends,
remote notes writes, provider merges, releases or repository-setting changes
are authorized by this checkpoint. Live acceptance needs separately approved
resources. Email remains deferred to #76; notifications are tracked by #75.

To resume from a checkout with the origin configured:

```sh
git fetch origin
git switch --track origin/spec/promotion-notifications
```

If that branch already exists locally, switch to it and fast-forward with
`git pull --ff-only`. Then run `$speckit-implement` with this checkpoint as
context. Task boxes are the completion record, not the mere presence of files.

## T001 — Namespace authority and contract review

Status: complete, 2026-09-04. This completes the governance prerequisite, not
runtime implementation or blanket acceptance of ADRs 0013/0014.

Maintainer decision: Shawn Stratton explicitly stated in this session:
“I'm fine with extending the namespace boundary to include notes/oiax”.
This followed an explanation of Git's standardized `refs/notes/` prefix and the
proposed explicit-lease, append-only notes operation.

The separate decision is recorded in [ADR 0015](../../../docs/adr/0015-oiax-owned-notes-namespace.md)
and propagated to Constitution XI, AGENTS.md and the plan template. The exact
notification target is `refs/notes/oiax/notifications/v1/<64-hex-graph-key>`.
The proposed write uses an explicit expected old OID (absence for creation),
and each replacement snapshot commit must have that old tip as its sole parent.
Conflicts reread/reduce. No rewind, notes deletion, arbitrary refs, branch/tag
mutation through the notes writer, or force-push of long-lived branches is allowed.

Contract review disposition: proposed ADR 0013 preserves optional configuration,
read-only preview and existing core exit semantics; proposed ADR 0014 supplies
durable receipts and states external-send ambiguity explicitly. Namespace
authority is now established for its constrained mechanism. The broader ADRs
remain proposed; implementation must validate their contracts and all remaining
tasks. No approval of those broader decisions is inferred from this permission.

## T002–T015 — Setup and foundation

Status: complete, 2026-09-04, at the earlier foundation validation snapshot.

- T002: fixture responsibility README precedes typed fixtures.
- T003: pure-package depguard rule retains engine guards. A temporary production
  probe importing `net/http` and `os/exec` produced two explicit depguard failures;
  the probe was removed and clean lint passed afterward.
- T004/T007: public optional policy, exported validation/defaulting, nil/empty
  and omitted/false YAML/JSON round trips, template-slot structure, label/path/
  destination bounds, unsupported email, strict fields and secret-safe parse
  errors. Tests failed on absent types/parsing before implementation, then passed.
- T005/T008/T009: stable length-delimited event IDs, immutable copied snapshots,
  revision-bound ancestry evidence, policy generations/cutoffs, global-off no-op,
  claims/renewal, persisted presentation, retry limits and monotone late receipts.
  Capacity reserves result space before admission/claim; no receipt GC.
- T006/T011/T012: canonical bounded ledger JSON, duplicate/unknown keys, malformed
  references and statuses, exact notes target, absence versus unavailable state,
  concurrent creation, stale updates, fixed anchor, sole-parent ancestry,
  malformed merge ancestry, oversized reads/writes and ordinary `git notes`
  readability. Local bare-remote tests confirm branch/worktree preservation.
  The writer owns an isolated bare object database, accepts only the providers'
  narrow HTTP-auth environment, and exposes no arbitrary-ref/commit writer.
- T010: lifecycle/snapshot/create-outcome interfaces and race-safe injected clock,
  memory store and payload recorder fixtures. Production pure code does not
  depend on effects or fixtures. Existing create-call migration remains T043.
- T013: transition-based store retries at most five conflicts, refreshes revision
  verification against each new snapshot, rejects stale/unordered replacements,
  preserves immutable events/messages/receipts, and maps remote failures to safe
  codes. Both providers expose the notes opener using existing Git auth helpers.
- T014/T015: configuration resolves once to a commit OID; referenced files load
  from that OID with the 1 MiB template bound. Loaded policy/sources stay outside
  `engine.Graph`. No-policy/all-disabled/empty-selection text and JSON stdout
  are byte-identical in `plan` and `reconcile`; lifecycle capability traps stay
  untouched. These are in-process CLI command tests; built-binary notification
  scenarios remain T036/T048/T060/T063.

## Earlier foundation commands and results

Go used the workspace-safe `GOCACHE=/tmp/oiax-go-build` because the default cache
is read-only in the sandbox.

| Check | Result |
|---|---|
| `go test -race -shuffle=on ./...` | PASS with loopback fixture access and test-only Git settings below; both existing forge suites passed |
| `go test -race -shuffle=on -cover ./internal/notification/...` | PASS: pure package 94.9%, store 86.7%; fixture package excluded from the production floor |
| `go test -race -shuffle=on ./internal/git -run TestNotification -v` | PASS, four nonempty local bare-remote suites |
| `go test -race -shuffle=on ./internal/cli -run TestNotification -v` | PASS: pinned loading, source bound, disabled compatibility |
| `go test ./internal/notification/store -run '^$' -fuzz FuzzNotificationLedgerCodec -fuzztime=10s -parallel=2` | PASS, 12,568 executions; no crash corpus produced |
| `go -C tools tool task build` | PASS |
| `go vet ./...` | PASS |
| `go -C tools tool task lint` / installed `golangci-lint run ./...` | PASS, version 2.13.2; installed binary uses the same repository config |
| `go -C tools tool task verify-generated` | PASS, CLI reference unchanged |
| `reuse lint` / `git diff --check` | PASS |
| `graphify update .` | Code-only refresh: 1,681 nodes, 4,612 edges; no semantic/API extraction |
| `graphify diagnose multigraph --max-examples 1` | No dangling/missing endpoints, self-loops or post-build collapsed edges; raw producer loss not measured |

Initial sandbox runs exposed environment limitations, not passing verification:
HTTP test listeners were denied, and an existing shallow-clone backflow test
attempted host signing through an unavailable WSL socket. The full suite passed
with approved loopback access and these process-local test settings (no repository
configuration was edited):

```sh
GOCACHE=/tmp/oiax-go-build \
GIT_CONFIG_COUNT=2 \
GIT_CONFIG_KEY_0=commit.gpgSign GIT_CONFIG_VALUE_0=false \
GIT_CONFIG_KEY_1=core.hooksPath GIT_CONFIG_VALUE_1=/dev/null \
go test -race -shuffle=on ./...
```

## Earlier foundation handoff (historical)

At this earlier validation point, T016–T080 (65 tasks) remained unchecked.
This was a foundation-only checkpoint,
not a completed `speckit-implement` run or a functional notification release.
The binary can load the new policy but does not yet discover notification events,
send messages, render notification templates or show notification previews. Do
not deploy notification configuration on the strength of these tests.

Next is the US1 tests-first batch T016–T019, then both forge lifecycle readers,
rendering, safe HTTP adapters and durable dispatch. Creation provenance, custom
templates, commit snapshot enrichment, preview/diagnostics and CI coverage wiring
follow in dependency order. Cross-OS notification tests, live notes permissions/
snapshot conformance, recipient-visible timing and timed operator setup remain
unverified. No live receiver sends, provider merges, remote notes writes, commits,
pushes, repository-setting changes, tags or releases were performed. ADRs
0013/0014 remain proposed; namespace-only ADR 0015 remains accepted.
