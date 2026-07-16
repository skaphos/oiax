# Modeling your promotion graph

This guide is about writing `.oiax.yaml`: how to describe your
environment branches as a promotion graph, which knobs exist, and how to
model the common shapes. For the exhaustive key-by-key table see the
[configuration reference](../reference/configuration.md); for the design
rationale see [Architecture](../architecture.md).

## The mental model

A promotion graph is a directed acyclic graph (DAG):

- **Nodes are long-lived branches** — one per environment
  (`development`, `test`, `qa`, `production-stage-1`, `main`, …). These
  already exist in your repository. **Oiax never creates long-lived
  branches**; every branch you name must already exist as a ref.
- **Edges are allowed promotion paths** — "changes flow from
  `development` to `test`." For each edge where the source has content the
  destination lacks, Oiax keeps exactly one open pull request that
  represents the difference.

That is the whole job: Oiax makes sure the promotion PRs your workflow
needs exist and stay honest. It never merges them, never approves them,
never deploys anything. Approval and safety stay with your existing
branch protection, required checks, CODEOWNERS, and reviewers.

## A minimal graph

The smallest useful configuration is two branches and one edge:

```yaml
apiVersion: oiax.skaphos.dev/v1
kind: PromotionGraph

metadata:
  name: environments

spec:
  branches:
    development:
      role: source
    main:
      role: terminal

  promotions:
    - from: development
      to: main
```

Put this at `.oiax.yaml` on your repository's **default branch**. Oiax
reads its configuration from a pinned ref — the default branch unless you
say otherwise — never from the branch that triggered a run (see
[pinned configuration](#where-configuration-is-read-from) below).

## The document

Every configuration file is exactly one `PromotionGraph` document. Extra
top-level documents are rejected, and so are unknown fields — a typo like
`promotons:` fails loudly rather than silently changing behavior.

| Key | Required | What it is |
| --- | --- | --- |
| `apiVersion` | yes | `oiax.skaphos.dev/v1`. The pre-1.0 `oiax.skaphos.dev/v1alpha1` still parses but warns; see [Migrating the apiVersion](#migrating-the-apiversion). |
| `kind` | yes | `PromotionGraph`. |
| `metadata.name` | yes | Names the graph. It appears in managed-request metadata, plan output, and logs — pick something stable; changing it later orphans the metadata on already-open requests. |
| `spec.branches` | yes | The branches, keyed by name. |
| `spec.promotions` | yes | The promotion edges. |
| `spec.backflow` | no | The hotfix-return policy — see the [backflow guide](backflow.md). |

## Branches

`spec.branches` is a map from branch name to per-branch settings. A
branch with no settings is written `{}`:

```yaml
spec:
  branches:
    development:
      role: source
    test: {}
    qa: {}
    production-stage-1: {}
    main:
      role: terminal
```

Every branch here must already exist as a ref — Oiax never creates
long-lived branches. Note that `oiax validate` checks graph *semantics*,
not repository state, so it will **not** catch a branch that doesn't exist
yet; that surfaces only at `plan`/`reconcile` time as `branch "<name>" not
found as a local head or origin-tracking ref` (see
[Troubleshooting](troubleshooting.md#a-configured-branch-does-not-exist)).

### Roles

`role` is optional metadata that constrains the shape of the graph. It
does not change how edges are evaluated — it catches modeling mistakes at
validation time.

| `role` | Meaning | Constraint |
| --- | --- | --- |
| `source` | The authoritative entry branch where changes originate. | Must not be the destination of any promotion edge. A backflow target must have this role. |
| `terminal` | An exit branch — usually production. | Must not be the source of any promotion edge. |
| *(unset)* | An intermediate branch. | None. |

You do not have to assign roles, but doing so turns "I accidentally drew
an edge *into* my source branch" into a validation error instead of a
subtle bug.

### Drift policy

`drift` states what Oiax should do about **downstream-only content** —
commits on a branch that its upstream does not have. That happens when
someone commits a hotfix directly to `main` instead of promoting it up
the graph.

| `drift` | Meaning |
| --- | --- |
| `forbidden` *(default)* | Downstream-only content is a problem. Oiax **reports** it, and if the branch is a backflow source, **returns** it to the authoritative branch via backflow. |
| `expected` | Downstream-only content is normal steady state and is acknowledged silently. Promotion detection is unaffected — downstream-only commits never enter the question it asks. |

Use `expected` for a branch that is *supposed* to carry local commits the
upstream will never see (for example, an environment branch with
environment-specific overlays committed directly to it).

> A branch cannot be both a backflow source **and** `drift: expected` —
> the two are contradictory (one says "return this content," the other
> says "this content is fine to keep"). Validation rejects the
> combination.

#### Transit branches and merge residue

There is a second, subtler reason to reach for `drift: expected`: the
merge method your forge uses for promotion pull requests.

Oiax decides whether a destination has diverged **by reachability**
(`git rev-list <source>..<destination>`), not by content. When a
promotion PR is merged with **squash** or a **merge commit** — the two
default buttons on most forges — the destination gains a commit that
exists on no upstream branch: the squash commit, or the merge node. Its
*content* is fully represented upstream, so the promotion direction stays
in sync: the [equivalence
ladder](../architecture.md#the-equivalence-ladder) only asks whether the
commits unique to the *source* are represented in the destination, and
downstream-only commits never enter that question. But as a *commit* the
residue is unique to the destination, so Oiax reports it as
downstream-only — one line of the plan output:

```text
  report   development -> test (1): test has 1 commits not represented in development
```

Only a **fast-forward** merge leaves the destination a strict subset with
nothing unique:

| Promotion PR merged as | commits unique to the destination |
| --- | --- |
| Merge commit | 1 (the merge node) |
| Squash | 1 (the squash commit) |
| Rebase | 1 per replayed commit (new SHAs) |
| Fast-forward | 0 |

So a **transit branch** — one that only ever receives content through
promotion (a mid-graph `test` or `qa`, never a hotfix target) — will
accumulate this benign residue on every squash/rebase/merge-commit
promotion. You have two clean options:

- **Enforce fast-forward-only promotions** (via your forge's API, the
  CLI, or a merge queue) and keep every branch `forbidden`. The graph
  stays a clean chain of subsets, and genuine accidental drift is still
  caught.
- **Mark the transit branches `drift: expected`.** This is the pragmatic
  choice when promotions merge as squash/rebase/merge commits (the
  standard forge buttons do not fast-forward). The trade-off is that
  real accidental drift on those branches is no longer flagged — which is
  usually acceptable, because in a promotion graph the branches where
  downstream content *matters* are the backflow sources, and those stay
  `forbidden` and are handled by backflow.

Do not use `expected` on a backflow source: its downstream-only content
is a hotfix to be **returned**, not ignored (and validation rejects the
combination, as above).

## Promotion edges

`spec.promotions` is a list of directed edges. Each edge names a `from`
and a `to`, both of which must be declared in `spec.branches`:

```yaml
spec:
  promotions:
    - from: development
      to: test
    - from: test
      to: qa
    - from: qa
      to: production-stage-1
    - from: production-stage-1
      to: main
```

Rules Oiax enforces:

- The edges must form a **DAG** — no cycles. `A → B → A` is rejected.
- `from` and `to` must differ.
- Each `from`/`to` pair may appear **at most once**.
- **Disconnected components are allowed** — one repository can hold
  several independent promotion paths (see
  [Multiple independent paths](#multiple-independent-paths)).

### Merge-method expectations

Optionally, an edge can declare the merge method its promotion PRs are
expected to use:

```yaml
  promotions:
    - from: qa
      to: production-stage-1
      expectations:
        mergeMethod: merge      # merge | squash | rebase
```

This is **reporting metadata only**. Oiax never changes your repository's
merge-button settings; it only warns (on stderr) when the repository does
not permit the method you declared. It is a way to make a mismatch
between your intent and your repo settings visible in CI.

**Recommendation:** where you can, disable squash merging on promotion
targets and let promotions merge as merge commits. Oiax detects "already
promoted" through an [equivalence ladder](../architecture.md#the-equivalence-ladder)
that works under all three merge methods, but merge commits keep
detection on its cheapest, exact rung. Squash and rebase rewrite commit
SHAs and lean on the content-based rungs. Everything still works under
squash — this is an optimization, not a requirement. See
[ADR 0002](../adr/0002-content-based-divergence-detection.md).

## Worked shapes

### Linear pipeline

The most common shape — a single line from source to production:

```yaml
apiVersion: oiax.skaphos.dev/v1
kind: PromotionGraph
metadata:
  name: environments
spec:
  branches:
    development: { role: source }
    test: {}
    qa: {}
    production-stage-1: {}
    main: { role: terminal }
  promotions:
    - { from: development, to: test }
    - { from: test, to: qa }
    - { from: qa, to: production-stage-1 }
    - { from: production-stage-1, to: main }
```

### Fan-out

One branch promoting into several — for example a shared `development`
feeding two independent production regions:

```yaml
spec:
  branches:
    development: { role: source }
    prod-us: { role: terminal }
    prod-eu: { role: terminal }
  promotions:
    - { from: development, to: prod-us }
    - { from: development, to: prod-eu }
```

### Multiple independent paths

Two unrelated pipelines in the same repository. They share no branches
and never interact; Oiax reconciles each:

```yaml
spec:
  branches:
    app-dev: { role: source }
    app-prod: { role: terminal }
    infra-dev: { role: source }
    infra-prod: { role: terminal }
  promotions:
    - { from: app-dev, to: app-prod }
    - { from: infra-dev, to: infra-prod }
```

## Where configuration is read from

`plan` and `reconcile` read `.oiax.yaml` from a **pinned ref** — your
repository's default branch unless you pass `--config-ref` — never from
the ref that triggered the run. This is deliberate
([ADR 0003](../adr/0003-pinned-configuration-ref.md)):

- **Determinism** — behavior does not depend on which branch happened to
  move last.
- **Security** — `.oiax.yaml` is itself promoted through the graph, so it
  differs per branch. Reading it from an untrusted pull request would let
  that PR's configuration run with your write credentials. Oiax never
  does this, and `pull_request_target` is not a default trigger.

A practical consequence: **a change to `.oiax.yaml` takes effect when it
lands on the default branch**, not when you open the PR that proposes it.
Edit the graph the same way you promote anything else.

The inspection commands `validate` and `graph` are the exception — they
read your working-tree file by default so you can iterate locally. Pass
`--config-ref` to inspect a pinned ref instead.

## Validating before you commit

Check a graph without touching any forge:

```bash
oiax validate    # semantic checks: acyclicity, references, roles, backflow
oiax graph       # print the topology Oiax parsed
```

`validate` reports **every** violation at once, not just the first, so
you can fix a batch in one pass. See the
[getting-started guide](getting-started.md) for a full local walkthrough.

## Migrating the apiVersion

If your file still declares the pre-1.0 alias:

```yaml
apiVersion: oiax.skaphos.dev/v1alpha1   # deprecated
```

every load prints one line to stderr:

```
warning: apiVersion "oiax.skaphos.dev/v1alpha1" is deprecated; migrate to "oiax.skaphos.dev/v1"
```

Migrate by changing the single string to `oiax.skaphos.dev/v1`. Nothing
else in the document changes — the two versions decode to an identical
graph. See [ADR 0005](../adr/0005-config-api-v1.md).

> The alias covers the **YAML** string only. Go programs that imported
> `pkg/api/v1alpha1` must update the import path to `pkg/api/v1`; there
> is no alias for the import path.

## Next steps

- [Deploy Oiax as a GitHub Action](github-action.md)
- [Set up a token that triggers CI](tokens.md)
- [Enable backflow for hotfixes](backflow.md)
- [Operate Oiax day to day](operating.md)
