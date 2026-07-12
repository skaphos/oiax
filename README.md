# oiax

Declarative Git branch promotion reconciler for branch-per-environment
GitOps repositories.

> **Status: pre-release scaffold.** The configuration API, graph
> validation, and CLI skeleton exist; edge evaluation, the GitHub forge
> provider, and backflow are in progress (see the
> [roadmap](docs/architecture.md#roadmap)). Nothing is released yet.

Branch-based GitOps repositories model environments as long-lived
branches — `development → test → qa → production-stage-1 → main` — and
promote changes with pull requests between adjacent branches. Those
promotion PRs are usually hand-made, go stale, and lose hotfixes applied
downstream. Oiax treats them as reconciled resources instead: given a
promotion graph declared in `.oiax.yaml`, it observes branch and forge
state and ensures the pull requests required to move changes through the
graph exist — exactly one active managed request per diverged edge, no
duplicates, no stale leftovers. It is
[release-please](https://github.com/googleapis/release-please)'s posture
applied to environment promotion.

Oiax deliberately does **not** deploy applications, render manifests, or
decide whether a change is safe to promote. Approval, validation, and
policy gates stay where the repository already puts them: branch
protection, required checks, CODEOWNERS, human review. (The name keeps
the point — the Greek οἴαξ is the tiller: a hand stays on it.)

## Quickstart

Declare the promotion graph at `.oiax.yaml` on the default branch:

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

Then:

```bash
go install github.com/skaphos/oiax/cmd/oiax@latest

oiax validate    # semantic validation of the graph
oiax graph       # display the promotion topology
oiax plan        # compute pending actions (the dry run)
oiax reconcile   # plan, then apply
```

`plan` and `reconcile` currently validate configuration and stop; edge
evaluation is the next roadmap milestone.

## GitHub Action

The initial execution model is a GitHub Action — a thin composite
wrapper around the release binary:

```yaml
- uses: skaphos/oiax@v1
  with:
    config: .oiax.yaml
    mode: reconcile        # validate | plan | reconcile
    version: v0.1.0
```

One trap worth knowing before anything else: pull requests created with
the default `GITHUB_TOKEN` do not trigger `on: pull_request` workflows,
so managed requests get no CI and can never merge under branch
protection. Use a GitHub App installation token in production. See
[architecture](docs/architecture.md#execution-model) for the full
workflow example and token guidance.

## Documentation

- [Documentation index](docs/README.md)
- [Architecture](docs/architecture.md) — the design: promotion graph,
  equivalence ladder, backflow, execution model, security posture
- [Configuration reference](docs/reference/configuration.md)
- [CLI reference](docs/reference/cli.md) (generated)
- [Code map](docs/code-map.md) — package tour for contributors
- [Architecture Decision Records](docs/adr/)
- [Contributing](CONTRIBUTING.md) · [Releases](RELEASE.md)

## License

[MIT](LICENSE) © Skaphos
