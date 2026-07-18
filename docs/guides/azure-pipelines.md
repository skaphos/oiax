# Deploying Oiax from Azure Pipelines

Oiax's execution model is CI-native and host-independent: no daemon, just
a job that reconciles your promotion graph when environment branches move
and on a schedule. This guide runs that job on **Azure Pipelines** for a
repository **hosted on GitHub** — the common shape for teams whose CI
lives in Azure DevOps while the code lives on GitHub.

Scope, stated plainly: this walkthrough targets the common shape — a
**GitHub-hosted** repository whose CI runs on Azure Pipelines, so the
forge Oiax manages is **GitHub**. Oiax also supports **Azure Repos** as a
forge provider (managed pull requests, backflow branches, and Azure
Boards conflict artifacts); if your code lives in Azure Repos, the
pipeline shape is the same and the [Azure Repos](#azure-repos) section
below covers the two differences (the token and the work-item type).

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
| `githubToken` | *(empty)* | GitHub token for forge API calls and pushing `oiax/` branches. Required when the repository is GitHub-hosted. |
| `azureDevOpsToken` | *(empty)* | Azure DevOps token — a PAT or `$(System.AccessToken)`. Required when the repository lives in [Azure Repos](#azure-repos). |
| `workItemType` | *(empty)* | Azure Boards work-item type for [conflict artifacts](#azure-repos). Empty means the provider default (`Issue`). |
| `forge` | *(empty)* | Forge override (`github` or `azuredevops`). Empty means automatic detection. |

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

There is no ambient GitHub token on an Azure agent — for a GitHub-hosted
repository, `githubToken` must be a credential you provision, stored as
a secret variable (an Azure Repos repository uses `azureDevOpsToken`
instead — see [Azure Repos](#azure-repos)):

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

When the repository itself lives in **Azure Repos** (not just the CI),
Oiax manages Azure Repos pull requests directly. The pipeline is the same
steps template shown above; two things change.

**Forge selection is automatic.** A pipeline whose checkout is an Azure
Repos repository (`Build.Repository.Provider = TfsGit`), or an `origin`
remote on `dev.azure.com` / `visualstudio.com`, selects the `azuredevops`
provider. The template's `forge` parameter (the `OIAX_FORGE` environment
variable, for CLI runs) overrides detection (`github` or `azuredevops`)
— pass `forge: github` if a GitHub-hosted repository's `origin` misleads
the detection.

**The token.** Pass `azureDevOpsToken` instead of `githubToken`. It
accepts either a personal access token or the pipeline's built-in
`$(System.AccessToken)`:

```yaml
- template: templates/azure-pipelines/oiax.yml@oiax
  parameters:
    mode: reconcile
    version: v1.0.0
    azureDevOpsToken: $(System.AccessToken)
```

Using `System.AccessToken` needs no secret: grant the build service
identity (`<Project> Build Service`) **Contribute** and **Contribute to
pull requests** on the repository (and, for conflict artifacts, work-item
**Edit** on the project). A personal access token needs the Code
(read/write), Pull Request Threads, and Work Items (read/write) scopes.

**Conflict artifacts and the work-item type.** A backflow conflict is
recorded as an Azure Boards work item tagged `oiax` + `oiax/conflict`. The
default type is `Issue`, which exists only in the **Basic** process. On an
**Agile/Scrum/CMMI** project set the `workItemType` parameter (the
`OIAX_ADO_WORKITEM_TYPE` environment variable, for CLI runs) to a type
your process defines (e.g. `Bug` or `Task`):

```yaml
  parameters:
    mode: reconcile
    version: v1.0.0
    azureDevOpsToken: $(System.AccessToken)
    workItemType: Bug
```

Everything else — the promotion graph, the equivalence ladder, backflow,
the plan and exit codes — is identical to the GitHub forge. See the
[configuration reference](../reference/configuration.md#environment-variables)
and [ADR 0009](../adr/0009-azure-devops-forge-provider.md).

## Next steps

- [Set up a token that triggers CI](tokens.md) — the GitHub App setup.
- [Operating Oiax day to day](operating.md) — reading plans, reviewing
  managed PRs, handling divergence.
- [Troubleshooting](troubleshooting.md) — if a run does something
  unexpected.
