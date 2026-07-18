# Getting started

This guide takes you from nothing to a working promotion graph you can
inspect locally, then points you at deploying Oiax as a GitHub Action.
It assumes you have a branch-per-environment repository — long-lived
branches like `development`, `test`, `qa`, `main` — and promote changes
between them with pull requests.

> **Release status.** Oiax is released: the `skaphos/oiax@v1` Action and
> per-tag prebuilt binaries (each with a `checksums.txt`) are published,
> and `@v1` tracks the latest `v1.x.y`. The Action is the production path;
> build from source (below) when you want to track `main` or work on Oiax
> itself.

## 1. Install

### From source

Requires **Go 1.26 or newer**:

```bash
go install github.com/skaphos/oiax/cmd/oiax@latest
```

This builds the CLI from `main` and drops the `oiax` binary in
`$(go env GOPATH)/bin`. Make sure that directory is on your `PATH`.

Confirm it runs:

```bash
oiax version
```

```
oiax dev
commit: none
built: unknown
```

(A source build reports `dev`; released binaries report their version,
commit, and build date.)

> **Optional — shell completion.** Oiax ships completions for common
> shells: `source <(oiax completion zsh)` (or `bash`, `fish`,
> `powershell`). Run `oiax completion --help` for permanent-install
> instructions.

### Prebuilt binaries and the Action

Each release tag publishes platform binaries with a `checksums.txt`, and
the [GitHub Action](github-action.md) downloads and verifies them for you.
That is the intended production path — you will rarely install the binary
by hand on a CI runner.

### git version

`plan` and `reconcile` shell out to `git` and require **git 2.45 or
newer** (backflow uses `git cherry-pick --empty=drop`, added in 2.45).
The floor is checked once at startup; on older git you get a clear error
naming the required and detected versions. The `validate` and `graph`
inspection commands do not need it. `ubuntu-latest` on GitHub Actions
satisfies the floor.

## 2. Write your first graph

Create `.oiax.yaml` at the root of your repository, describing your
environment branches and the promotion paths between them. Here is a
five-branch pipeline with hotfix backflow:

```yaml
apiVersion: oiax.skaphos.dev/v1
kind: PromotionGraph

metadata:
  name: environments

spec:
  branches:
    development:
      role: source
    test: {}
    qa: {}
    production-stage-1: {}
    main:
      role: terminal

  promotions:
    - from: development
      to: test
    - from: test
      to: qa
    - from: qa
      to: production-stage-1
    - from: production-stage-1
      to: main

  backflow:
    sources:
      - production-stage-1
      - main
    target: development
    strategy: cherry-pick
```

Every branch you name must **already exist** in the repository — Oiax
never creates long-lived branches. For what each key means and how to
model other topologies, see [Modeling your promotion
graph](promotion-graphs.md).

## 3. Inspect it locally

Two commands read your working-tree file and touch no forge, so they are
safe to run anywhere while you iterate.

**Validate** the graph's semantics:

```bash
oiax validate
```

```
Configuration valid: graph "environments", 5 branches, 4 promotion edges, backflow enabled.
```

If something is wrong, `validate` reports **every** violation at once
(not just the first), each on its own line, then exits non-zero:

```
invalid: promotion edge test -> qa: branch "qa" is not declared in spec.branches
invalid: backflow: target "development" must have role "source"
.oiax.yaml: 2 validation errors
```

**Print the topology** Oiax parsed, to confirm it matches your intent:

```bash
oiax graph
```

```
Promotion graph: environments

Branches:
  development  (source)
  main  (terminal)
  production-stage-1
  qa
  test

Promotions:
  development -> test
  test -> qa
  qa -> production-stage-1
  production-stage-1 -> main

Backflow (cherry-pick):
  production-stage-1 -> development
  main -> development
```

## 4. See what Oiax would do

`plan` observes real branch and forge state, evaluates every edge, and
prints the actions `reconcile` would take — without changing anything.
It is the dry run; there is no separate `--dry-run` flag.

`plan` and `reconcile` talk to GitHub, so they need a token and a way to
resolve the repository. Locally:

```bash
export GITHUB_TOKEN=ghp_...          # a token with repo access
oiax plan
```

Typical output when work is pending:

```
Promotion graph: environments
  create   development -> test (3): 3 unpromoted commits and no managed promotion request
```

When everything is already promoted:

```
Promotion graph: environments
In sync, no actions.
```

The leading verb tells you the action kind — `create`, `update`,
`close`, `backflow`, or `report`. For machine consumption, add
`-o json` and parse the [plan JSON format](../reference/plan-format.md).

> Running `plan`/`reconcile` locally reads configuration from your
> repository's **default branch** (a pinned ref), not your working tree —
> see [where configuration is read from](promotion-graphs.md#where-configuration-is-read-from).
> If Oiax cannot resolve the default branch locally it falls back to the
> working-tree file; pin `--config-ref` to be explicit.

## 5. Apply

`reconcile` computes the same plan and then applies it — creating,
updating, and closing managed pull requests, and opening backflow
requests. It never merges, approves, or force-pushes your long-lived
branches.

```bash
oiax reconcile
```

You will almost always run `reconcile` from CI rather than your laptop,
so that it runs with the right identity and on the right events.

## Adopting Oiax on an existing repository

Two things are worth knowing before the **first** reconcile on a
repository that already has promotion PRs:

- **Unmanaged pull requests are safe.** Oiax only ever acts on requests it
  created — recognized by a marker in the body plus the branch
  relationship, never by title or label. A promotion PR you opened by hand
  is never edited or closed.
- **But a hand-made PR on an edge Oiax also manages will collide.** If you
  already have an open PR from, say, `development` into `test`, and Oiax
  decides that edge needs a promotion request, it tries to open its own
  with the same head and base. GitHub rejects the duplicate (HTTP 422),
  and Oiax only adopts a duplicate that is *its own* managed request — so
  it cannot adopt your hand-made one, and `reconcile` fails on that edge
  with a "create request" error.

  Let Oiax own the edge from the start: merge or close your hand-made
  promotion PR first, then run `reconcile`. See
  [Troubleshooting](troubleshooting.md#reconcile-fails-with-a-create-request-error).

A safe adoption sequence: run `oiax plan` to see exactly which edges Oiax
will open requests for, resolve any hand-made PRs on those edges, then
switch to `reconcile`. Deploying with `mode: plan` before `mode: reconcile`
(see [Recipes — roll out plan-first](recipes.md#roll-out-plan-first)) makes
this a low-risk rollout.

## 6. Deploy it

The production model is a GitHub Action that reconciles on pushes to your
environment branches plus a scheduled repair run. Continue with:

- **[Deploy Oiax as a GitHub Action](github-action.md)** — the workflow
  file, triggers, and permissions.
- **[Set up a token that triggers CI](tokens.md)** — the one setup step
  that trips everyone up; do this before you rely on Oiax in production.

## Where to go next

| You want to… | Read |
| --- | --- |
| Understand every config key | [Configuration reference](../reference/configuration.md) |
| Model roles, drift, multiple pipelines | [Modeling your promotion graph](promotion-graphs.md) |
| Return hotfixes to the source branch | [Backflow](backflow.md) |
| Read plans and review managed PRs | [Operating Oiax](operating.md) |
| Diagnose a problem | [Troubleshooting](troubleshooting.md) |
| Understand the design | [Architecture](../architecture.md) |
