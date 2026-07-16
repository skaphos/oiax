# Guides

Task-oriented guides for using Oiax. Start at the top and work down, or
jump to the one that matches what you are doing. For exhaustive tables see
the [reference](../reference/), and for the design see
[Architecture](../architecture.md).

## Set up

1. **[Getting started](getting-started.md)** — install, write your first
   promotion graph, and inspect it locally.
   - **[Installing Oiax with an AI agent](agent-install.md)** — hand this
     playbook to a coding agent: it infers your repository's promotion
     graph, confirms the shape with you, then writes the config and
     workflow.
2. **[Modeling your promotion graph](promotion-graphs.md)** — branches,
   roles, drift policy, merge methods, and common topologies.
3. **[Deploy Oiax as a GitHub Action](github-action.md)** — the workflow
   file, triggers, permissions, and `fetch-depth: 0`.
4. **[Setting up a token that triggers CI](tokens.md)** — the GitHub App
   token setup that keeps managed PRs from stalling. Do this before you
   rely on Oiax in production.

## Use

- **[Backflow: returning hotfixes](backflow.md)** — bring downstream-only
  commits back to your source branch.
- **[Minimizing divergence](minimizing-divergence.md)** — practices
  upstream of Oiax that keep managed requests merging cleanly: config
  isolation, hotfix discipline, drift gates, and rollback.
- **[Operating Oiax day to day](operating.md)** — read plans, review and
  merge managed PRs, branch protection, offboarding, scale, and reported
  divergence.
- **[Recipes](recipes.md)** — drift/policy gates, PR-time config
  validation, previewing a graph change, plan-first rollout, JSON output,
  and monorepos.
- **[Troubleshooting](troubleshooting.md)** — symptom → cause → fix for
  every warning and error Oiax emits.

## Reference

- [CLI reference](../reference/cli.md)
- [Configuration reference](../reference/configuration.md)
- [Plan JSON format](../reference/plan-format.md)
