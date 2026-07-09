# Oiax documentation

Oiax is a declarative Git branch promotion reconciler for
branch-per-environment GitOps repositories. Start with the repository
[README](../README.md) for installation and a quickstart.

## Contents

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

## Decisions

Architecture Decision Records live in [adr/](adr/):

- [0001 — Adopt the name Oiax](adr/0001-adopt-the-name-oiax.md)
- [0002 — Detect divergence by content, not ancestry](adr/0002-content-based-divergence-detection.md)
- [0003 — Read configuration from a pinned ref](adr/0003-pinned-configuration-ref.md)

## Process

- [Contributing](../CONTRIBUTING.md)
- [Release process](../RELEASE.md)
- [Releases / changelog](https://github.com/skaphos/oiax/releases)
