# Recipes

Task-focused patterns beyond the steady-state reconcile loop. Each is
grounded in flags and behavior Oiax actually has today.

## Roll out plan-first

**Scenario.** You are adding Oiax to a repository and want to see it
behave for a cycle before it opens or closes anything.

**How.** Deploy the workflow with `mode: plan` first. In plan mode Oiax
observes and prints the plan (to the logs and the job step summary) but
applies nothing. Watch a few real events go by, confirm the actions match
your intent, then flip the input to `mode: reconcile`.

```yaml
- uses: skaphos/oiax@v1
  with:
    mode: plan          # observe only; change to `reconcile` when satisfied
    version: v1.0.0
```

Pair this with [adopting on an existing repo](getting-started.md#adopting-oiax-on-an-existing-repository)
so any hand-made promotion PRs are resolved before you switch to
`reconcile`.

## Gate CI on drift (read-only policy check)

**Scenario.** You want a job that **fails** (or alerts) when the promotion
graph is not reconciled — e.g. a scheduled "is anything un-promoted?"
check, or a required check that blocks something until promotions are
clean.

**How.** Use `oiax plan --detailed-exitcode`. Its exit code is a contract:

| Exit | Meaning |
| --- | --- |
| 0 | fully in sync |
| 2 | applyable changes pending (a promotion/backflow would be created/updated/closed) |
| 3 | a report-only divergence needs a human |

> **Important:** the Action's `mode: plan` does **not** pass
> `--detailed-exitcode` — it always exits 0 on a successful plan. To gate
> on the exit code you must run the **CLI** directly, not the Action.

```yaml
name: Oiax drift check
on:
  schedule: [{ cron: "*/30 * * * *" }]
  workflow_dispatch:
jobs:
  drift:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - name: Prepare git refs          # mirror what the Action does for you
        run: |
          git fetch --no-tags --prune origin "+refs/heads/*:refs/remotes/origin/*"
          git remote set-head origin --auto
      - run: go install github.com/skaphos/oiax/cmd/oiax@latest
      - env:
          GITHUB_TOKEN: ${{ secrets.OIAX_TOKEN }}
        run: oiax plan --detailed-exitcode   # non-zero → job fails → you get alerted
```

Because this runs the CLI directly (not the Action), it also does the ref
preparation the Action would normally handle — fetching every branch head
and setting `origin/HEAD` so the graph branches and the pinned config-ref
resolve. Treat exit 2 as "drift to reconcile" and exit 3 as "needs a human." See
[Operating — exit codes and CI gating](operating.md#exit-codes-and-ci-gating).

## Validate `.oiax.yaml` on every pull request

**Scenario.** Catch a broken graph *before* it merges to the default
branch (where it would take effect), instead of finding out at the next
reconcile.

**How.** Run `mode: validate` on pull requests that touch `.oiax.yaml`.
`validate` reads the **working-tree** file — so with the PR head checked
out it validates the *proposed* config. It touches no forge, needs no
token, and does not require git 2.45, which is exactly why reading the
non-pinned (PR) ref is safe here.

```yaml
name: Validate promotion graph
on:
  pull_request:
    paths: [".oiax.yaml"]
jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4          # PR head; default depth is fine
      - uses: skaphos/oiax@v1
        with:
          mode: validate
          version: v1.0.0
```

`validate` checks graph *semantics* (acyclicity, references, roles,
backflow consistency). It does **not** check that the branches exist as
refs — see [the missing-branch note](promotion-graphs.md#branches).

## Preview a graph change before it lands

**Scenario.** You have edited `.oiax.yaml` on a branch and want to see
what Oiax would do under the new graph — but a config change only takes
effect once it reaches the default branch (the [pinned
ref](promotion-graphs.md#where-configuration-is-read-from)).

**How.** Point the inspection at your branch with `--config-ref`, which
reads the config from that ref (`git show <ref>:.oiax.yaml`) while still
observing current repository state:

```bash
oiax graph --config-ref my-graph-change      # topology under the proposed config
oiax plan  --config-ref my-graph-change      # actions reconcile would take under it
```

`graph` needs no token; `plan` observes the forge, so give it a
`GITHUB_TOKEN` as usual.

## Consume the plan as JSON

**Scenario.** You want to feed Oiax's intended actions into a dashboard,
notifier, or another automation step.

**How.** `oiax plan -o json` writes a `Plan` document to **stdout**;
logs and workflow annotations go to stderr, so stdout is clean JSON you
can pipe straight into a parser.

```bash
oiax plan -o json > plan.json
jq -r '.actions[] | "\(.type) \(.from) -> \(.to): \(.reason)"' plan.json
```

The shape is the frozen `planFormatVersion: 1` contract — every field,
type, and presence rule is documented in the [plan JSON
format](../reference/plan-format.md). Ignore unrecognized fields rather
than rejecting the document, and do not pattern-match on `reason` text
(its wording is not part of the contract).

## Run multiple pipelines in one repository (monorepo)

**Scenario.** A monorepo with several independent promotion pipelines
(one per app or component).

**How.** Declare them as **disconnected components in a single
`.oiax.yaml`** — one `PromotionGraph` with several unconnected sets of
branches and edges. Oiax reconciles each independently. Note the
constraint: a repository has exactly one graph document. Multiple
`.oiax.yaml` files or multiple YAML documents in one file are **not**
supported (the loader rejects multi-document files). See
[Modeling — multiple independent paths](promotion-graphs.md#multiple-independent-paths).

## Next steps

- [Operating Oiax day to day](operating.md)
- [Troubleshooting](troubleshooting.md)
- [Configuration reference](../reference/configuration.md)
