# Deploying Oiax as a GitHub Action

The production model for Oiax is a GitHub Action: no daemon, no control
plane, just a workflow that reconciles your promotion graph when
environment branches move and on a schedule. The Action is a thin
composite wrapper around the release binary — it downloads a
checksum-verified `oiax`, prepares git refs, and runs it. No promotion
logic lives in YAML.

The composite Action supports Linux runners on x64 and ARM64. Use the
standalone release binary when running Oiax on another operating system.

> **Release status.** The `skaphos/oiax@v1` Action reference will resolve
> only after the first v1 release exists; Oiax has not cut it yet.
> The workflow below is the shape you will use; it becomes runnable when
> the first release ships. Until then you can exercise the same behavior
> from source with `oiax reconcile` (see [getting started](getting-started.md)).

## The complete workflow

Save this as `.github/workflows/oiax.yml` on your default branch:

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
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0
      - uses: skaphos/oiax@v1
        with:
          config: .oiax.yaml
          mode: reconcile
```

The rest of this guide explains each part and why it is there.

## Inputs

| Input | Default | Meaning |
| --- | --- | --- |
| `mode` | `reconcile` | What to run: `validate`, `plan`, or `reconcile`. |
| `config` | `.oiax.yaml` | Path to the configuration file. |
| `config-ref` | *(empty)* | Ref to read configuration from. Empty means the repository default branch — the [pinned-ref](promotion-graphs.md#where-configuration-is-read-from) default. |
| `token` | `${{ github.token }}` | Token for forge API calls and pushing `oiax/` branches. **Change this** — see [Tokens](#tokens). |
| `version` | Action ref's release | Optional exact binary override, e.g. `v1.0.0`. By default `@v1` reads the release manifest at that ref, so wrapper and binary advance together within v1. A cross-major override is rejected. |

Release automation advances the floating `v1` tag only after a full
`v1.x.y` release and its checksum-verified artifacts are published. A
consumer on `@v1` therefore receives compatible minor and patch updates;
set `version` only when you need to hold the binary at an exact release.

## `fetch-depth: 0` is not optional

`actions/checkout` defaults to a **shallow** clone (`fetch-depth: 1`). A
shallow clone has no merge base, which silently makes the merge-base,
patch-identity, and baseline rungs of Oiax's [equivalence
ladder](../architecture.md#the-equivalence-ladder) unreliable and produces
**spurious promotion pull requests** for content that is already promoted.

Set `fetch-depth: 0` so the full history is present. Oiax detects a
shallow clone and warns:

```
shallow clone detected: equivalence detection is degraded (merge-base, patch-identity and baseline rungs are unreliable), which can produce spurious promotion requests; set fetch-depth: 0 on actions/checkout for correct results
```

The Action deliberately does **not** un-shallow for you — a shallow
checkout stays shallow and you get the warning, because only a
full-history checkout yields correct results.

Under the hood the Action also fetches every branch head into
`refs/remotes/origin/*` and runs `git remote set-head origin --auto`, so
every graph branch is resolvable and the default-branch config-ref
resolves without you setting `config-ref`. You do not need to do anything
for this; it is why a multi-branch graph reconciles correctly on the
first run.

## Triggers

Oiax is event-driven with scheduled repair. Events are only *hints to
reconcile* — the model stays correct when they are duplicated, reordered,
missed, or concurrent — so pick triggers that cover the ways your graph
moves:

- **`push` to the environment branches** — the primary trigger. **This
  list must mirror the branches in your graph.** Workflow triggers cannot
  read `.oiax.yaml`, so if you add a branch to the graph, add it here too.
- **`pull_request: [closed]`** — reconcile right after a promotion PR
  merges, so the next edge is evaluated immediately.
- **`workflow_dispatch`** — a manual "reconcile now" button.
- **`schedule`** — periodic repair that catches anything the events
  missed. Any cron cadence works; hourly is a reasonable default.

## Permissions

Oiax needs to read and write pull requests and push `oiax/` branches:

```yaml
permissions:
  contents: write        # push oiax/backflow/* branches
  pull-requests: write   # create, update, comment on, close managed requests
```

Grant these at the workflow (or job) level, following least privilege —
do not widen beyond what Oiax uses. Write access lets Oiax open and
refresh requests and push `oiax/` branches; it never lets Oiax merge,
approve, or force-push your long-lived branches. Configure the promotion
targets themselves in [Operating — branch protection and required
checks](operating.md#branch-protection-and-required-checks).

## Concurrency

```yaml
concurrency:
  group: oiax
  cancel-in-progress: false
```

This only reduces wasted duplicate runs. **Correctness never depends on
it** — Oiax resolves concurrency without locks (the forge rejects
duplicate promotion requests, and backflow branch names are
deterministic), so overlapping runs converge rather than collide. Use
`cancel-in-progress: false` so a repair run is not cancelled midway.

## Tokens

This is the one setup step that trips everyone up.

Pull requests created with the default `GITHUB_TOKEN` **do not start
`on: pull_request` checks automatically**. GitHub queues workflow runs
for the `opened`, `synchronize`, and `reopened` events in an
approval-required state. Under branch protection, unattended promotion
stalls until a user with write access approves those runs. See GitHub's
[workflow-trigger documentation](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow#triggering-a-workflow-from-a-workflow).
Oiax emits a conservative warning when it sees this identity:

```
created pull request is authored by github-actions[bot]; on: pull_request workflows will not run for it. Configure a GitHub App installation token so managed requests get CI.
```

The unattended fix is a **GitHub App installation token**. It is worth
doing before you rely on Oiax in production. The full walkthrough —
creating the App, wiring `actions/create-github-app-token`, and the
fine-grained-PAT fallback — is in **[Set up a token that triggers
CI](tokens.md)**.

## Choosing a mode

- **`reconcile`** — the steady-state mode. Plan and apply.
- **`plan`** — observe and print the plan without applying. Useful in a
  read-only "what would Oiax do?" job, or to gate on Oiax's
  [exit codes](../reference/configuration.md#exit-codes) with
  `--detailed-exitcode` (via the CLI).
- **`validate`** — semantic configuration check only. Handy as a fast PR
  check on changes to `.oiax.yaml` itself.

The Action rejects any other `mode` value up front.

## Step summary and annotations

When it runs under Actions, Oiax:

- appends a Markdown table of the plan to the job's **step summary**
  (`$GITHUB_STEP_SUMMARY`), so the run page shows what it did at a glance;
- surfaces warnings and errors as workflow **annotations** (`::warning::`
  / `::error::` on stderr), so they appear inline in the run.

Machine output (`-o json`) always goes to stdout uncorrupted; annotations
and logs go to stderr.

## Next steps

- [Set up a token that triggers CI](tokens.md) — do this next.
- [Operating Oiax day to day](operating.md) — reading plans, reviewing
  managed PRs, handling divergence.
- [Troubleshooting](troubleshooting.md) — if a run does something
  unexpected.
