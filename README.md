# oiax

Declarative Git branch promotion reconciler for branch-per-environment
GitOps repositories.

> **Status: pre-release.** The core reconciler is implemented — edge
> evaluation through the full equivalence ladder, the GitHub forge
> provider, managed promotion requests, and backflow (see the
> [roadmap](docs/architecture.md#roadmap)). No release is cut yet; the
> first is 1.0.0, in progress, so install from source (see
> [Quickstart](#quickstart)) until then. New here? Start with the
> [getting-started guide](docs/guides/getting-started.md).

Branch-based GitOps repositories model environments as long-lived
branches — `development → test → qa → production-stage-1 → main` — and
promote changes with pull requests between adjacent branches. Those
promotion PRs are usually hand-made, go stale, and lose hotfixes applied
downstream. Oiax treats them as reconciled resources instead: given a
promotion graph declared in `.oiax.yaml`, it observes branch and forge
state and ensures the pull requests required to move changes through the
graph exist — exactly one active managed request per diverged edge, no
duplicates. Requests for edges removed from the graph are deliberately
left for a human to close. It is
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

Oiax shells out to `git` for all repository state and backflow replay, and
requires **git 2.45 or newer** (backflow uses `git cherry-pick
--empty=drop`, added in git 2.45). The version is checked once at startup;
on older git, `plan` and `reconcile` fail fast with a clear message naming
the required floor and the detected version.

`plan` evaluates every promotion edge and prints the actions
`reconcile` would apply; `reconcile` then creates, updates, and closes
managed pull requests, including backflow. The one piece not yet
implemented is `validate`'s repository-state check (confirming configured
branches exist as refs).

## GitHub Action

The initial execution model is a GitHub Action — a thin composite
wrapper around the release binary. The Action supports Linux runners on
x64 and ARM64; standalone release binaries remain available for the other
published platforms.

```yaml
- uses: actions/checkout@v7
  with:
    fetch-depth: 0         # required: full history for correct equivalence detection
- uses: skaphos/oiax@v1
  with:
    config: .oiax.yaml
    mode: reconcile        # validate | plan | reconcile
```

Release automation advances `@v1` only after publishing a successful
`v1.x.y` release. The Action reads that release's manifest and downloads
the matching binary, so wrapper and binary update together within v1.

`fetch-depth: 0` is not optional: `actions/checkout`'s default shallow
clone (`fetch-depth: 1`) has no merge base, which silently degrades
equivalence detection and yields spurious promotion requests. Oiax warns
when it detects a shallow clone.

One trap worth knowing before anything else: pull requests created with
the default `GITHUB_TOKEN` do not start `on: pull_request` checks
automatically. GitHub queues `opened`, `synchronize`, and `reopened` runs
for approval by a user with write access, so unattended promotion stalls
when those checks are required. See GitHub's
[workflow-trigger documentation](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow#triggering-a-workflow-from-a-workflow).
Use a GitHub App installation token in production. See
[architecture](docs/architecture.md#execution-model) for the full
workflow example and token guidance.

## Documentation

- [Documentation index](docs/README.md)
- [Guides](docs/guides/README.md) — getting started, GitHub Action setup,
  tokens, backflow, day-two operations, troubleshooting
- [Architecture](docs/architecture.md) — the design: promotion graph,
  equivalence ladder, backflow, execution model, security posture
- [Configuration reference](docs/reference/configuration.md)
- [CLI reference](docs/reference/cli.md) (generated)
- [Code map](docs/code-map.md) — package tour for contributors
- [Architecture Decision Records](docs/adr/)
- [Contributing](CONTRIBUTING.md) · [Releases](RELEASE.md)

## License

[MIT](LICENSE) © Skaphos
