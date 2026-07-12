# Oiax documentation

Oiax is a declarative Git branch promotion reconciler for
branch-per-environment GitOps repositories. Start with the repository
[README](../README.md) for installation and a quickstart.

## Guides

Task-oriented walkthroughs — see the [guides index](guides/README.md):

- [Getting started](guides/getting-started.md) — install, write your
  first graph, inspect it locally.
- [Modeling your promotion graph](guides/promotion-graphs.md) — branches,
  roles, drift, merge methods, and common topologies.
- [Deploy Oiax as a GitHub Action](guides/github-action.md) — the
  workflow, triggers, permissions, `fetch-depth: 0`.
- [Setting up a token that triggers CI](guides/tokens.md) — the GitHub
  App token setup that keeps managed PRs from stalling.
- [Backflow: returning hotfixes](guides/backflow.md).
- [Operating Oiax day to day](guides/operating.md) — reading plans,
  reviewing managed PRs, handling divergence.
- [Troubleshooting](guides/troubleshooting.md) — symptom → cause → fix.

## Design and internals

- [Architecture](architecture.md) — what Oiax is and is not; the
  promotion graph model; the equivalence ladder; managed change requests;
  backflow; execution model; failure handling; security posture; roadmap.
- [Code map](code-map.md) — a package-by-package tour for new
  contributors.

## Reference

- [CLI reference](reference/cli.md) — generated from the command tree;
  regenerate with `task docs:cli-ref`.
- [Configuration reference](reference/configuration.md) — every
  `.oiax.yaml` key, flag, and exit code.
- [Plan JSON format](reference/plan-format.md) — the frozen
  `planFormatVersion: 1` field-level contract for `oiax plan -o json`.

## Decisions

Architecture Decision Records live in [adr/](adr/):

- [0001 — Adopt the name Oiax](adr/0001-adopt-the-name-oiax.md)
- [0002 — Detect divergence by content, not ancestry](adr/0002-content-based-divergence-detection.md)
- [0003 — Read configuration from a pinned ref](adr/0003-pinned-configuration-ref.md)
- [0004 — Backflow execution](adr/0004-backflow-execution.md)
- [0005 — Config API v1](adr/0005-config-api-v1.md)

## Process

- [Contributing](../CONTRIBUTING.md)
- [Release process](../RELEASE.md)
- [Changelog](../CHANGELOG.md) (release-please managed)
