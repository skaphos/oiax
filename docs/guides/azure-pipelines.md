# Deploying Oiax from Azure Pipelines

Oiax's execution model is CI-native and host-independent: no daemon, just
a job that reconciles your promotion graph when environment branches move
and on a schedule. This guide runs that job on **Azure Pipelines** for a
repository **hosted on GitHub** — the common shape for teams whose CI
lives in Azure DevOps while the code lives on GitHub.

Scope, stated plainly: the forge Oiax talks to is still **GitHub** — it
creates and manages GitHub pull requests. **Azure Repos is not yet a
supported forge provider** (it is a roadmap item; the forge abstraction
was designed for it). If your repository lives in Azure Repos, Oiax
cannot manage its pull requests yet.

The steps template is the Azure sibling of the [composite GitHub
Action](github-action.md): it downloads a checksum-verified `oiax`
release binary, prepares git refs, and runs it. No promotion logic lives
in YAML. Linux agents only (x64 and ARM64); `ubuntu-latest` satisfies
Oiax's git ≥ 2.45 floor.

> **Release status.** The template reference below resolves only after
> the first v1 release exists; Oiax has not cut it yet. The pipeline
> below is the shape you will use; it becomes runnable when the first
> release ships.

## The complete pipeline

Save this as `azure-pipelines.yml` in the GitHub-hosted repository:

```yaml
# The branch list must mirror the branches in your promotion graph:
# pipeline triggers cannot read .oiax.yaml.
trigger:
  branches:
    include: [development, test, qa, production-stage-1, main]

# Scheduled repair: catches anything the push triggers missed.
# `always: true` runs it even when nothing changed.
schedules:
  - cron: "17 * * * *"
    displayName: Hourly reconcile repair
    branches:
      include: [main]
    always: true

resources:
  repositories:
    - repository: oiax
      type: github
      name: skaphos/oiax
      ref: refs/tags/v1.0.0        # pin template and version together
      endpoint: my-github-connection

pool:
  vmImage: ubuntu-latest

steps:
  - checkout: self
    fetchDepth: 0                  # required: full history for correct equivalence detection
    persistCredentials: true       # required: the template's ref-prepare fetch is authenticated
  - template: templates/azure-pipelines/oiax.yml@oiax
    parameters:
      version: v1.0.0
      mode: reconcile              # validate | plan | reconcile
      githubToken: $(OIAX_GITHUB_TOKEN)
```

Define `OIAX_GITHUB_TOKEN` as a **secret pipeline variable** (or in a
variable group) holding a GitHub credential — see [Tokens](#tokens).

## Parameters

| Parameter | Default | Meaning |
| --- | --- | --- |
| `version` | *(required)* | Exact Oiax release to download, e.g. `v1.0.0`. Unlike the Action — whose `@v1` ref carries a release manifest that picks the binary — a template ref cannot see what release it came from, so you pin the binary explicitly. Keep it in step with the `ref` you pin the `oiax` repository resource to. |
| `mode` | `reconcile` | What to run: `validate`, `plan`, or `reconcile`. |
| `config` | `.oiax.yaml` | Path to the configuration file. |
| `configRef` | *(empty)* | Ref to read configuration from. Empty means the repository default branch — the [pinned-ref](promotion-graphs.md#where-configuration-is-read-from) default. |
| `githubToken` | *(required)* | GitHub token for forge API calls and pushing `oiax/` branches. |

## `fetchDepth: 0` is not optional

Azure Pipelines defaults new YAML pipelines to a **shallow** fetch
(`fetchDepth: 1`). A shallow clone has no merge base, which silently makes
the merge-base, patch-identity, and baseline rungs of Oiax's [equivalence
ladder](../architecture.md#the-equivalence-ladder) unreliable and produces
**spurious promotion pull requests** for content that is already promoted.

Set `fetchDepth: 0` on the `checkout: self` step so the full history is
present. Oiax detects a shallow clone and warns; the template deliberately
does **not** un-shallow for you, because only a full-history checkout
yields correct results. The partial-clone advice in the [GitHub Action
guide](github-action.md#large-repositories-partial-clone) applies here
unchanged if full clones dominate the job's runtime.

`persistCredentials: true` is also required: the template fetches every
branch head into `refs/remotes/origin/*` and runs
`git remote set-head origin --auto` (so every graph branch is resolvable
and the default-branch config-ref resolves), and that fetch needs the
checkout credentials to still be present.

## Triggers

Events are only *hints to reconcile* — the model stays correct when they
are duplicated, reordered, missed, or concurrent:

- **`trigger` on the environment branches** — the primary trigger. **This
  list must mirror the branches in your graph**; pipeline triggers cannot
  read `.oiax.yaml`, so a branch added to the graph must be added here too.
- **`schedules`** — periodic repair. Azure has no trigger for "a pull
  request merged", but a merged promotion PR lands as a push to its target
  branch, which the `trigger` list already covers; the schedule catches
  everything else. Use `always: true` so repair runs even without new
  commits.
- **Manual runs** — the Run pipeline button covers the
  `workflow_dispatch` role.

Azure Pipelines has no equivalent of the workflow-level `concurrency`
group. That is acceptable: correctness never depends on it — Oiax
resolves concurrency without locks (the forge rejects duplicate promotion
requests, and backflow branch names are deterministic), so overlapping
runs converge rather than collide.

## Tokens

There is no ambient GitHub token on an Azure agent — `githubToken` must
always be a credential you provision, stored as a secret variable:

1. **GitHub App installation token** — production guidance, exactly as in
   [Set up a token that triggers CI](tokens.md). Azure has no
   `actions/create-github-app-token` step, so mint the installation token
   in a preceding script step (or supply a short-lived token from your
   secret store) and pass it to the template.
2. **Fine-grained PAT** — acceptable; rotation burden on the user. Scope
   it to the repository with `contents: write` and `pull-requests: write`.

The [GitHub Actions token trap](github-action.md#tokens) — PRs created
with the workflow's own `GITHUB_TOKEN` not starting `on: pull_request`
checks — applies to the *token*, not the CI host: if the repository also
runs GitHub Actions checks, a PAT or App token (either of the above)
avoids it. Build-validation builds triggered from Azure Pipelines' own
GitHub integration are not affected.

The service connection (`endpoint`) used for the `oiax` repository
resource and checkout is separate from `githubToken` and only needs read
access.

## Step summary and annotations

When it runs under Azure Pipelines (`TF_BUILD` is set), Oiax:

- publishes a Markdown table of the plan to the run's summary page via
  `##vso[task.uploadsummary]`, so the run shows what it did at a glance;
- surfaces warnings and errors as `##vso[task.logissue type=warning|error]`
  logging commands, so they appear as issues on the run.

Machine output (`-o json`) always goes to stdout uncorrupted; logging
commands and logs go to stderr.

One CI-safety behavior also follows the host: when the repository default
branch cannot be resolved (no `origin/HEAD` — typically a checkout shape
the template's ref-prepare step did not run against), Oiax under CI
refuses to fall back to the working-tree configuration and asks you to
pin `--config-ref`, exactly as it does under GitHub Actions
([pinned configuration ref](promotion-graphs.md#where-configuration-is-read-from)).

## Choosing a mode

Identical to the Action: `reconcile` is the steady state, `plan` is the
read-only dry run, `validate` is a fast configuration check for PRs that
touch `.oiax.yaml`. The template rejects any other value at compile time
(parameter `values`) and again at run time.

## Azure Repos

Not yet. The forge abstraction is provider-neutral by design and Azure
DevOps is the anticipated second provider (managed pull requests map to
Azure Repos PRs; conflict artifacts to work items), but no `azuredevops`
provider ships today. Until it does, Oiax manages GitHub pull requests
only, whatever CI host it runs on.

Oiax does already *detect* Azure Repos: a pipeline run whose checkout is
an Azure Repos repository (or an `origin` remote on `dev.azure.com` /
`visualstudio.com`) fails fast with a clear not-yet-supported error
instead of running the GitHub provider against the wrong forge. The
`OIAX_FORGE` environment variable overrides detection (`github` or
`azuredevops`) — set `OIAX_FORGE=github` if the detection misfires for a
repository whose promotion pull requests genuinely live on GitHub. See
the [configuration reference](../reference/configuration.md#environment-variables).

## Next steps

- [Set up a token that triggers CI](tokens.md) — the GitHub App setup.
- [Operating Oiax day to day](operating.md) — reading plans, reviewing
  managed PRs, handling divergence.
- [Troubleshooting](troubleshooting.md) — if a run does something
  unexpected.
