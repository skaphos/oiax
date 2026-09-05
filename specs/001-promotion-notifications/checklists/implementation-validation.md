# Implementation validation evidence

## Current checkpoint — basic merge-alert integration

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
