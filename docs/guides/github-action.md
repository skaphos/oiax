# Deploying Oiax as a GitHub Action

The production model for Oiax is a GitHub Action: no daemon, no control
plane, just a workflow that reconciles your promotion graph when
environment branches move and on a schedule. The Action is a thin
composite wrapper around the release binary — it downloads a
checksum-verified `oiax`, prepares git refs, and runs it. No promotion
logic lives in YAML.

> **Release status.** The `skaphos/oiax@v1` Action resolves to a
> published release, and Oiax has not cut one yet (1.0.0 is in progress).
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
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: skaphos/oiax@v1
        with:
          config: .oiax.yaml
          mode: reconcile
          version: v1.0.0
```

The rest of this guide explains each part and why it is there.

## Inputs

| Input | Default | Meaning |
| --- | --- | --- |
| `mode` | `reconcile` | What to run: `validate`, `plan`, or `reconcile`. |
| `config` | `.oiax.yaml` | Path to the configuration file. |
| `config-ref` | *(empty)* | Ref to read configuration from. Empty means the repository default branch — the [pinned-ref](promotion-graphs.md#where-configuration-is-read-from) default. |
| `token` | `${{ github.token }}` | Token for forge API calls and pushing `oiax/` branches. **Change this** — see [Tokens](#tokens). |
| `version` | *(required)* | The Oiax release to download, e.g. `v1.0.0`. Required until a release pins a default. |

## `fetch-depth: 0` is not optional

`actions/checkout` defaults to a **shallow** clone (`fetch-depth: 1`). A
shallow clone has no merge base, which silently disables the
patch-identity and baseline rungs of Oiax's [equivalence
ladder](../architecture.md#the-equivalence-ladder) and produces **spurious
promotion pull requests** for content that is already promoted.

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
do not widen beyond what Oiax uses.

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

Pull requests created with the default `GITHUB_TOKEN` **do not trigger
`on: pull_request` workflows** (GitHub's recursion guard). Under branch
protection with required checks, such a request gets no CI and **can
never merge**. Oiax warns when it sees this:

```
created pull request is authored by github-actions[bot]; on: pull_request workflows will not run for it. Configure a GitHub App installation token so managed requests get CI.
```

The fix is a **GitHub App installation token**. It is worth doing before
you rely on Oiax in production. The full walkthrough — creating the App,
wiring `actions/create-github-app-token`, and the fine-grained-PAT
fallback — is in **[Set up a token that triggers CI](tokens.md)**.

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
