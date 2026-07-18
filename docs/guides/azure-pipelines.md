# Deploying Oiax from Azure Pipelines

Oiax's execution model is CI-native and host-independent: no daemon, just
a job that reconciles your promotion graph when environment branches move
and on a schedule. This guide runs that job on **Azure Pipelines**.

**The primary shape this guide targets:** your code lives on **GitHub**,
your CI runs on **Azure Pipelines**, and Oiax runs as a pipeline step that
manages the GitHub pull requests — connected through a GitHub
[service connection](#connecting-azure-devops-to-github). Most of this
guide is about that path.

Oiax *also* supports **Azure Repos** as a forge provider (managed pull
requests, backflow branches, and Azure Boards conflict artifacts), for
teams whose code lives in Azure Repos too. The pipeline shape is identical;
the secondary [Azure Repos](#azure-repos) section covers the two
differences (the token and the work-item type). If you are here for the
common "GitHub repo, Azure CI" case, you can skip it.

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

## Connecting Azure DevOps to GitHub

When your code lives on GitHub and your CI runs on Azure Pipelines, Azure
DevOps reaches GitHub through a **service connection** — a stored
credential it uses to clone the repository, receive its webhooks (the
triggers), and post build status back. That connection is **separate from**
the `githubToken` Oiax itself uses to manage pull requests. Two
credentials, two different jobs:

| Credential | Who uses it | What it does | Access it needs |
| --- | --- | --- | --- |
| **GitHub service connection** | Azure DevOps (checkout, triggers, status) | Clone the repo (`checkout: self`), deliver push/PR triggers, report the build back to GitHub | Repository **read** + commit-status write |
| **`githubToken`** (secret variable) | the `oiax` binary | Create / update / close managed PRs, push `oiax/` branches | `contents: write`, `pull-requests: write` |

The service connection's token is **not** exposed to your script as a
usable `GITHUB_TOKEN`, and reusing it would be wrong even if it were: that
identity is scoped for checkout, not for authoring the pull requests Oiax
manages — and a PR opened as the connection identity may not start the
downstream `on: pull_request` checks (the [token trap](#tokens)). Always
provision a dedicated `githubToken`.

### Create the service connection

You get one the first time you point a pipeline at a GitHub repo, but you
can also create it explicitly:

1. **Project Settings → Service connections → New service connection →
   GitHub.**
2. Choose the authentication:
   - **Azure Pipelines GitHub App** *(recommended)* — install the
     [Azure Pipelines app](https://github.com/apps/azure-pipelines) on the
     org or repo. Fine-grained, revocable per repository, posts checks
     natively, and there is no PAT to rotate.
   - **OAuth** — fastest to set up; tied to the authorizing user's access.
   - **Personal access token** — a **classic** GitHub PAT with `repo`
     scope (the connection type Azure DevOps expects here, not a
     fine-grained PAT); rotation is on you.
3. Grant it the repository (or the whole org).
4. Name it — that name is what you reference as `endpoint:`. This guide
   uses `my-github-connection`.

### Wire it into the pipeline

Two things reference a GitHub connection, and they can share one:

- **Your repository** — the one Oiax reconciles. Creating the pipeline
  (*Pipelines → New pipeline → GitHub → select your repo*) records the
  connection for it; `checkout: self` clones it and the same connection
  delivers triggers. You do not name this one in YAML — it is bound to the
  pipeline.
- **The `oiax` template repository** (`skaphos/oiax`, public). The
  `resources.repositories` entry needs an explicit `endpoint`. Because it
  is a public repo, any GitHub service connection that can read public
  repositories works — reuse your repository's connection, or make a
  minimal public-access one.

```yaml
resources:
  repositories:
    - repository: oiax
      type: github
      name: skaphos/oiax
      ref: refs/tags/v1.0.0
      endpoint: my-github-connection    # ← the service connection name

steps:
  - checkout: self                      # your GitHub repo, via its pipeline connection
    fetchDepth: 0
    persistCredentials: true
  - template: templates/azure-pipelines/oiax.yml@oiax
    parameters:
      version: v1.0.0
      mode: reconcile
      githubToken: $(OIAX_GITHUB_TOKEN) # ← Oiax's own forge token, a secret variable
```

### Triggers over a service connection

Push and PR triggers work exactly as in the top example — Azure DevOps
subscribes to GitHub webhooks through the connection. Two notes:

- A **GitHub App** connection delivers triggers and status checks with no
  extra setup. With **OAuth/PAT**, confirm the pipeline's **Triggers →
  Enable continuous integration** is on and the identity is allowed to
  create webhooks on the repo.
- If the target branch enforces required status checks, the connection
  identity reports Oiax's own build status; Oiax's *managed* PRs still need
  `githubToken` to be created and to start their own checks.

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

The GitHub **service connection** used for checkout, triggers, and the
`oiax` template resource is a different, lower-privilege credential from
`githubToken`: it needs repository **read** plus commit-status write to
report build results (a **GitHub App** connection grants that
automatically), and never the `contents`/`pull-requests` write that
`githubToken` carries. The read-only `oiax` template resource needs even
less — just public read. See
[Connecting Azure DevOps to GitHub](#connecting-azure-devops-to-github)
for the distinction and setup.

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
`$(System.AccessToken)`, and `checkout: self` uses the Azure Repos
credentials directly — no service connection is involved:

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
