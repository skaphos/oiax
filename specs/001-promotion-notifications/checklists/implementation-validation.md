# Implementation validation evidence

## Current checkpoint — bounded delivery resumed

2026-09-04: work-in-progress implementation checkpoint for draft PR #77,
branch `spec/promotion-notifications`. **Not merge-ready or deployable.**
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
| focused notification/reconcile race tests, repeated 10 times after concurrent dispatch | PASS |
| `go -C tools tool task lint` | PASS, 0 issues |
| `go -C tools tool task build` | PASS |
| `go -C tools tool task verify-generated` | PASS |
| `graphify update .` | PASS: 1,787 nodes, 4,962 edges, 107 communities |
| `graphify diagnose multigraph --max-examples 1` | PASS: no missing/dangling endpoints, self-loops or collapsed edges; raw producer loss not measured |
| `git diff --check` | PASS |

US1 remains incomplete: provider pagination/incomplete-scan adversarial coverage,
the rest of merge integration and default-presentation matrices, built-binary
notification scenarios and independent acceptance evidence are still unchecked.
Creation provenance, custom templates, immutable commit enrichment, preview and
diagnostics remain later phases.

Resume in this order:

1. Complete T016–T019 and T020–T028 provider/client/presentation conformance,
   especially pagination movement, dense/incomplete scans, unmanaged requests,
   both request types and exact adapter goldens.
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
