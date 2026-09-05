# Implementation validation evidence

## Current checkpoint — resume on another machine

2026-09-04: work-in-progress implementation checkpoint for draft PR #77,
branch `spec/promotion-notifications`. **Not merge-ready or deployable.**
The evidence below for T002–T015 describes the earlier foundation snapshot,
not a passing verification of all code in this checkpoint.

T001–T015 are checked complete; T016–T080 remain unchecked. US1 is partially
implemented: both provider lifecycle readers and their shared conformance
fixtures, Teams/Slack/webhook adapters, HTTPS transport protections, built-in
rendering, merge selection, scheduling and observation scaffolding are present.
These additions have not completed the US1 acceptance matrix.

Checkpoint verification: `GOCACHE=/tmp/oiax-go-build go test ./... -run '^$'`
fails because `internal/reconcile/notification_merge_test.go` references
`NotificationRuntime.Dispatch`, which has not been implemented. Keep this
tests-first failure visible; do not skip the test or add a no-op implementation
merely to make CI green. Whitespace verification passes. The full suite and
lint must be rerun after completing the interrupted implementation.

Resume in this order:

1. Read the feature's `tasks.md`, spec, plan, contracts and repository guidance.
   Continue US1 from T016–T019; the existing tests cover only part of that matrix.
2. Implement durable dispatch in `internal/reconcile/notification_delivery.go`
   using the existing reducers, saved messages, claims, renewal and monotone
   results. Respect bounded time, concurrency, per-destination spacing and
   fresh policy checks before sending. Do not contact live receivers in tests.
3. Reconcile runtime store calls with the in-memory CAS fixture:
   `notification_observe.go` currently passes an empty expected revision on
   every commit, while `notificationtest.MemoryStore` requires the current
   revision after initialization. Preserve stale-write/conflict guarantees.
4. Complete secret-safe event/message persistence, lifecycle pagination and
   incomplete-scan tests, then wire the runtime into mutating CLI finalization.
   The CLI currently carries the loaded policy but does not run notifications.
   Keep all-disabled invocations free of notification I/O and preserve core
   exit codes and unmanaged-request boundaries.
5. Continue creation provenance, custom templates, immutable commit enrichment,
   preview/diagnostics and release validation in task dependency order.
   Refresh `graphify-out/` afterward: its committed refresh predates the latest
   US1 files. Recheck coverage, race tests, lint and generated artifacts.

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
