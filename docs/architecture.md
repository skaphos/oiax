# Architecture

Oiax is a declarative Git branch promotion reconciler. Given a promotion
graph declared over long-lived branches, it observes branch and forge
state and ensures the pull requests required to move changes through that
graph exist — exactly one active managed request per diverged edge, no
duplicates, no stale leftovers.

Oiax deliberately does **not**:

- deploy applications,
- render manifests,
- decide whether a change is safe to promote.

Oiax manages the Git workflow used to *express* promotion. Approval,
validation, and policy gates remain wherever the repository already puts
them (branch protection, required checks, CODEOWNERS, human review).

Terminology: Skaphos documentation calls this capability **branch
promotion**, qualified — unqualified *Promotion* is a settled Keleustes
term meaning release-into-targets. Oiax serves teams below the
control-plane threshold: branch-per-environment repositories on plain
Argo CD or Flux that need the promotion PR workflow reconciled without
adopting a delivery control plane.

## The model

Branch promotion is a directed acyclic graph: nodes are long-lived
branches, edges are allowed promotion paths. The desired state of an
edge:

> If the source branch contains changes not yet represented in the
> destination branch, exactly one active Oiax-managed promotion request
> SHOULD represent that difference.

Two distinct flows exist:

- **Promotion** — forward movement along declared edges
  (`development → test → qa → …`).
- **Backflow** — exceptional changes introduced directly on a downstream
  branch (hotfixes) are returned to the single configured authoritative
  branch, then promoted forward normally. Oiax never propagates
  downstream changes backward through every edge.

The reconciliation loop: load configuration (pinned ref) → validate
graph → fetch repository state → inspect existing managed requests →
evaluate promotion edges → evaluate backflow sources → build plan →
apply plan → emit result. "Reconciliation" refers exclusively to this
loop; the hotfix-return flow is always called backflow.

## The equivalence ladder

The load-bearing design decision (see
[ADR 0002](adr/0002-content-based-divergence-detection.md)): divergence
is detected **by content, not only by commit ancestry**. Squash and
rebase merges rewrite SHAs; under naive `rev-list to..from` detection
they leave every edge looking permanently diverged, and "create PR"
then fails at the forge (GitHub rejects PRs with no commits, HTTP 422).
That failure mode is v1 scope, not an optimization.

Whether source content is represented in the destination is decided by
an ordered ladder; the first conclusive rung wins:

1. **Reachability** — `rev-list to..from` empty → in sync. Exact when
   promotion edges merge with merge commits.
2. **Patch identity** — candidates whose stable patch-id
   (`git patch-id --stable`) already appears in
   `merge-base(from,to)..to` are promoted. Handles rebase merges.
3. **Head-tree equality** — `tree(from) == tree(to)` → in sync by
   content. Handles squash merges at the moment of merge.
4. **Promotion baseline** — every managed request records the source
   head SHA it proposes (`sourceHead`); once merged, commits reachable
   from that baseline are promoted regardless of SHA rewriting. Handles
   squash merges after the source has advanced.

Rung 4 is why managed-request metadata matters: under history-rewriting
merges, the merged promotion request is the durable record of what was
promoted. Git remains the source of truth for content; the forge holds
the promotion baseline. Both are reconstructible without any
Oiax-private database.

Edge states and their handling:

| State | Meaning | Action |
| --- | --- | --- |
| In sync | no unpromoted source content | none (an open managed request is obsolete → close with comment) |
| Promotion required | unpromoted source commits exist | ensure exactly one managed promotion request |
| Destination ahead | destination has content the source lacks | backflow source → return it; `drift: expected` → nothing; else report |
| Diverged | both sides unique | ensure the promotion request; apply backflow/drift policy; identify the divergence plainly |

Oiax never infers a safe merge for a diverged edge, never force-pushes
long-lived branches, and never attempts automatic conflict resolution.

## Managed change requests

Managed requests are identified by a machine-readable marker in the
request body plus branch relationship — never by title:

```html
<!--
oiax:
  version: v1
  graph: environments
  type: promotion          # promotion | backflow
  source: development
  destination: test
  sourceHead: 4f2a91c…
-->
```

Default labels: `oiax` plus `oiax/promotion` or `oiax/backflow`.
Unmanaged requests between the same branches are never closed or edited.

Lifecycle: a promotion request's head *is* the live source branch, so
new source commits join the open request naturally; Oiax updates the
recorded `sourceHead` and never creates a duplicate. Consequence stated
plainly: an approved promotion request can gain commits after approval —
enable the branch-protection setting that dismisses stale approvals. A
snapshot strategy (frozen `oiax/promote/...` candidates) is deliberately
deferred.

Obsolete requests (edge removed from configuration, edge synchronized
out-of-band, metadata no longer matching actual base/head) are closed
with an explanatory comment, never silently, never deleted.

## Backflow

Given `backflow: {sources: [main], target: development}` and a hotfix
`X` on `main`, Oiax:

1. creates a branch from the target,
2. replays the downstream-only commits onto it with
   `git cherry-pick -x` (the trailer is durable provenance and the
   identity check's cheapest rung),
3. opens a managed request from that branch to the target.

The branch name is deterministic —
`oiax/backflow/<source>-to-<target>/<source-head-short-sha>` — and
determinism is the concurrency strategy: racing runs converge on the
same ref, identical pushes are no-ops, conflicting creates fail cleanly
at the ref level. Branches under `oiax/` are Oiax's to force-push and
delete; no ref outside that namespace is ever force-pushed.

Backflow identity ("is downstream commit X already represented in the
target?"): cherry-pick trailer → stable patch-id → otherwise unreturned.
Known limitation, stated rather than hidden: patch identity breaks when
a cherry-pick needed conflict resolution and no trailer exists; a manual
override marker is planned with backflow (v0.2).

Cherry-pick conflicts stop the operation: nothing partial is pushed, and
the output identifies the failing commit and how many applied cleanly.

## Layers

```text
┌──────────────────────────────────────┐
│              Entrypoint              │
│ CLI / GitHub Action / Azure Pipeline │
├──────────────────────────────────────┤
│                Engine                │
│ graph / divergence / planning        │
├──────────────────────────────────────┤
│              Git layer               │
│ refs / reachability / patch identity │
├──────────────────────────────────────┤
│            Forge provider            │
│ GitHub / Azure DevOps / GitLab       │
└──────────────────────────────────────┘
```

Two purity rules, enforced structurally (depguard forbids the engine
from importing the git layer or forge providers):

- The planner makes no provider API calls. Observation happens before
  planning; application happens after.
- Identical graph and observed state produce an equivalent plan.

The git layer shells out to the system `git` executable rather than
reimplementing Git object semantics. The forge abstraction speaks in
provider-neutral **change requests**; GitHub is an implementation detail
of the first provider. Forge-reported mergeability is advisory (GitHub
computes it asynchronously); unknown means "proceed and let the request
surface the conflict," never an error.

See the [code map](code-map.md) for where each layer lives.

## Execution model

Oiax requires no daemon: event-driven reconciliation plus scheduled
repair, initially as a GitHub Action.

```yaml
name: Oiax

on:
  push:
    branches: [development, test, qa, production-stage-1, main]
  pull_request:
    types: [closed]
  workflow_dispatch:
  schedule:
    - cron: "17 * * * *"

permissions:
  contents: write
  pull-requests: write

concurrency:
  group: oiax
  cancel-in-progress: false

jobs:
  reconcile:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: skaphos/oiax@v1
        with:
          config: .oiax.yaml
          mode: reconcile
```

The `on.push.branches` list must mirror the configured graph; workflow
triggers cannot read `.oiax.yaml`.

Events are hints to reconcile, not authoritative state. The model stays
correct when events are duplicated, reordered, missed, or concurrent.
Concurrency resolves without locks: the forge rejects duplicate
promotion requests (adopted as success — re-list and continue), and
backflow branch naming is deterministic. The workflow `concurrency`
group only reduces wasted runs; correctness never depends on it.

### Tokens

Pull requests created with the default `GITHUB_TOKEN` do not trigger
`on: pull_request` workflows (GitHub's recursion guard) — under branch
protection such requests can never merge. In order of recommendation:

1. **GitHub App installation token** (e.g. via
   `actions/create-github-app-token`) — production guidance. Native App
   operation is v0.3 scope; supplying an App token works from v0.1.
2. **Fine-grained PAT** — acceptable; rotation burden on the user.
3. **`GITHUB_TOKEN`** — works out of the box, degraded: created
   requests get no CI. Acceptable only when no required checks guard
   promotion targets; Oiax warns when it detects this.

## Failure handling and observability

- Merge conflict on an edge: create/preserve the request, label
  `oiax/conflict`, report — never auto-resolve, never close.
- Backflow conflict: stop, push nothing, diagnose.
- Provider failure: non-zero exit; the plan is emitted before
  application, so partial runs stay explainable; the next reconcile
  converges (idempotency is the recovery mechanism — there is no
  rollback).

Structured logging (`OIAX_LOG_FORMAT=text|json`); in GitHub Actions,
workflow annotations for warnings/errors and a plan summary written to
`$GITHUB_STEP_SUMMARY`. Credential values never appear in output.

## Security posture

- Configuration is read from a pinned ref
  ([ADR 0003](adr/0003-pinned-configuration-ref.md)); untrusted PR
  configuration never runs with privileged credentials, and
  `pull_request_target` is not a default trigger.
- Configuration is declarative data — Oiax never invokes commands it
  defines, and request templates evaluate nothing.
- Branch names are passed to git as data (`git check-ref-format`
  validation, `--` separators), never interpolated into shell.
- Backflow operates in a clean working tree.
- Force-push is confined to the `oiax/` ref namespace.

## Roadmap

- **v0.1** — edge evaluation with the full equivalence ladder; managed
  promotion request discovery/creation/update with `sourceHead`
  metadata; obsolescence handling; `validate`/`plan`/`reconcile`/`graph`
  with text and JSON output; exit-code contract; GitHub forge provider;
  GitHub Action wrapper.
- **v0.2** — backflow: drift policy enforcement at runtime,
  deterministic backflow branches, cherry-pick `-x`, identity ladder,
  conflict diagnostics.
- **v0.3** — native GitHub App mode; org-level defaults; request
  templates; labels/assignees/reviewers.
- **v0.4** — Azure DevOps provider; provider capability discovery.
- **v1.0** — stable configuration API; idempotence under all three
  merge methods; reliable backflow identity; concurrent execution
  tested; managed-request compatibility across minor releases.

## Prior art

release-please (the direct inspiration — a PR as a reconciled resource,
CI-native, no daemon), argoproj-labs/gitops-promoter (Kubernetes
controller, hydrated-manifest branch model), Kargo (promotion
orchestration control plane), Telefonistka (directory-per-environment
sibling), and the incumbent: ad-hoc CI scripts.
