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

A managed promotion request whose edge is still in the graph but has
synchronized out of band — so it now proposes nothing — is closed with an
explanatory comment, never silently and never deleted. Removing an edge
from the configuration is different: its request is no longer evaluated,
so it is left open (orphaned) rather than closed. Backflow requests, by
contrast, are superseded and closed (and their head branch deleted) on a
new hotfix — see [Backflow](#backflow).

## Backflow

Given `backflow: {sources: [main], target: development}` and a hotfix
`X` on `main`, Oiax:

1. creates a branch from the target,
2. replays the downstream-only commits onto it with
   `git cherry-pick -x` (the trailer is durable provenance and the
   identity check's cheapest rung),
3. opens a managed request from that branch to the target.

Execution is specified in
[ADR 0004](adr/0004-backflow-execution.md). The branch name is
deterministic — `oiax/backflow/<source>-to-<target>/<short-sha>`, where
`<short-sha>` is a fixed-length short SHA of the **backflow source branch
head** (the downstream head), not the replayed commits. Determinism is
the concurrency strategy: the same source head yields the same name, so
racing or repeated runs converge on one ref. The force-push is a no-op
on a repeated run *when the source and target heads are both unchanged*:
the cherry-pick pins each commit's committer identity and date to the
original commit's, so a fixed replay base and inputs produce an identical
HEAD SHA rather than a fresh one stamped with the wall-clock time. The
replay lands on the *current* target head, so an advancing target
re-pushes the branch onto the new base — the intended behaviour for a
return request that must merge into the live target — and concurrent runs
that observe the same source and target heads still produce identical
pushes, so they converge rather than clobber. A new hotfix
advances the source head, mints a new name, and supersedes the prior
request (the stale request is closed only when its encoded head is an
ancestor of the new one, so supersede stays monotonic under concurrent
runs that observe different heads). At most one active managed backflow
request exists per (source,target): on a new hotfix the stale request is
closed with an explanatory comment and the new one opened. Branches
under `oiax/` are Oiax's to force-push and delete; no ref outside that
namespace is ever force-pushed. The replay runs in an ephemeral detached
worktree, never the caller's checkout, and that worktree is removed on
every exit path.

Backflow identity ("is downstream commit X already represented in the
target?"): cherry-pick trailer → stable patch-id → the `Oiax-Backflow:
skip` commit trailer → otherwise unreturned. The trailer is a human
escape hatch (ADR 0004): it marks a downstream commit as intentionally
not-returned — for a hotfix deliberately kept downstream-only, or a
commit whose patch identity cannot be recovered because an earlier
cherry-pick resolved conflicts and left no trailer. Like all Oiax state
it lives in Git, not a private database.

Cherry-pick conflicts stop the operation: nothing partial is pushed, and
the diagnostic identifies the failing commit's SHA and subject and how
many commits applied cleanly. This is a reported divergence
(reconcile exit 3), not a created request.

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
          version: v1.0.0
```

The `on.push.branches` list must mirror the configured graph; workflow
triggers cannot read `.oiax.yaml`.

Under GitHub Actions the composite Action prepares Git refs before it runs
Oiax. `actions/checkout` materializes only the triggering branch as a local
head and does not set `origin/HEAD`, so the Action fetches every branch head
into `refs/remotes/origin/*` and runs `git remote set-head origin --auto`.
This makes every graph branch resolvable — Oiax resolves a branch name to its
local head or, failing that, its origin-tracking ref — and the default
config-ref (`origin/HEAD`) known, so a multi-branch graph reconciles on its
first run and configuration resolves from the default branch without pinning
`--config-ref`. Use `fetch-depth: 0` on `actions/checkout` (as above) so the
full history the reachability and patch rungs rely on is present; a shallow
checkout leaves divergence detection inaccurate.

Oiax also requires **git 2.45 or newer** on the runner: backflow replays
commits with `git cherry-pick --empty=drop`, an option older git lacks
(Ubuntu 22.04 ships 2.34, Debian bookworm 2.39, and some GHES images are
older still). The floor is asserted once at startup, so an unsupported
runner fails fast with a clear error naming the required version and the
detected one rather than failing deep inside a backflow. `ubuntu-latest`
satisfies this.

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

- Merge conflict on an edge: create/preserve the request and report it —
  never auto-resolve, never close. (Managed requests carry the `oiax`
  label plus `oiax/promotion` or `oiax/backflow`; a dedicated conflict
  label is not applied today.)
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
- Backflow runs in an ephemeral detached worktree, never the caller's
  checkout, and that worktree is removed on every exit path.
- Force-push is confined to the `oiax/` ref namespace.

## Roadmap

- **v0.1** — edge evaluation with the full equivalence ladder; managed
  promotion request discovery/creation/update with `sourceHead`
  metadata; obsolescence handling; `validate`/`plan`/`reconcile`/`graph`
  with text and JSON output; exit-code contract; GitHub forge provider;
  GitHub Action wrapper.
- **v0.2** *(implemented)* — backflow: drift policy enforcement at
  runtime, deterministic backflow branches keyed to the source head,
  cherry-pick `-x` replay in an ephemeral worktree, identity ladder with
  the `Oiax-Backflow: skip` override, supersede-and-close on a new
  hotfix, conflict diagnostics as reported divergence
  ([ADR 0004](adr/0004-backflow-execution.md)).
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
