# Installing Oiax with an AI agent

This is a playbook for an AI coding agent (Claude Code, Cursor, Copilot,
or similar) asked to "install Oiax" or "set up Oiax" on a Git repository.
It is written to be executed step by step. A human can follow it too, but
its structure — inspect, infer, **confirm**, then write — is aimed at an
agent working on a repository it did not author.

The governing rule: **Oiax's configuration encodes intent that only the
repository's owners know** — which branches are environments, what order
they promote in, which one is authoritative. You can *infer* all of it
from the repository, but a wrong guess creates promotion pull requests
against the wrong branches. So every inference in this guide has an
explicit confirmation gate. Do not write files or run `reconcile` until
the user has confirmed the graph.

For the underlying concepts behind each step, defer to the human guides:
[getting started](getting-started.md), [modeling your promotion
graph](promotion-graphs.md), [GitHub Action](github-action.md),
[tokens](tokens.md). This playbook links into them rather than repeating
them.

## What "installed" means

A complete installation adds two files and one credential:

1. **`.oiax.yaml`** at the repository root, on the **default branch** —
   the promotion graph. `plan`/`reconcile` read config from a pinned ref
   (the default branch), not the ref that triggered the run, so the file
   only takes effect once it lands there. See [where configuration is
   read from](promotion-graphs.md#where-configuration-is-read-from).
2. **`.github/workflows/oiax.yml`** — the GitHub Action that runs
   `reconcile` on branch pushes and a schedule.
3. **A token** that lets managed pull requests start CI — a GitHub App
   installation token in production. This is a human step you cannot do
   for them; surface it clearly (see [step 7](#7-set-up-the-token-human-step)).

Your job is to produce 1 and 2 correctly for *this* repository and hand
off 3.

## Before you start: scope and safety

- **Forge support.** Oiax's only implemented forge provider is
  **GitHub**. If the repository's remote is not GitHub (GitLab,
  Bitbucket, Gitea, …), stop and tell the user Oiax cannot manage it
  yet — do not attempt a workaround.
- **Oiax never creates long-lived branches.** Every branch you name in
  the graph must already exist as a ref. Do not propose a graph that
  invents branches.
- **Do not run `oiax reconcile` yourself without explicit consent.**
  `validate` and `graph` are read-only and safe to run anytime. `plan`
  reaches the forge read-only (needs a token). `reconcile` opens, edits,
  and closes real pull requests — treat it as a mutation the user must
  approve.
- **Work on a branch.** Make your changes on a feature branch and open a
  pull request; never commit `.oiax.yaml` or the workflow directly to the
  default branch. (The config only activates once merged to default —
  which is the user's decision, via that PR.)

## 1. Check preconditions

Confirm the environment can run Oiax before designing anything.

```bash
git rev-parse --is-inside-work-tree     # must be a git repo
git remote -v                            # remote must be github.com
git --version                            # plan/reconcile need git >= 2.45
gh auth status                           # gh CLI authenticated (you'll use it to inspect the repo)
```

- **git ≥ 2.45** is required for `plan`/`reconcile` (backflow uses `git
  cherry-pick --empty=drop`). `ubuntu-latest` runners satisfy this; a
  local machine may not.
- If you will run the CLI locally to validate (recommended), you need the
  binary. Until Oiax's first release, install from source with **Go 1.26+**:
  ```bash
  go install github.com/skaphos/oiax/cmd/oiax@latest
  oiax version
  ```
  If you cannot install it, you can still write the config and rely on
  the Action's `mode: validate` in CI — but say so; you're giving up the
  local check in step 5.

## 2. Discover the repository shape

Gather the facts you will turn into a graph. Prefer read-only commands.

```bash
# Default branch — this is where config is read from (the pinned ref).
gh repo view --json defaultBranchRef -q .defaultBranchRef.name

# All long-lived branches (remote heads), so you don't miss any.
git ls-remote --heads origin | sed 's#.*refs/heads/##'
# or, if fetched:  git for-each-ref --format='%(refname:short)' refs/remotes/origin

# Merge-button settings — informs expectations.mergeMethod per target.
gh repo view --json mergeCommitAllowed,squashMergeAllowed,rebaseMergeAllowed

# Existing open PRs between branches — matters for adoption (step 6).
gh pr list --state open --json number,headRefName,baseRefName,title

# Any existing config? Don't clobber it.
test -f .oiax.yaml && cat .oiax.yaml || echo "no .oiax.yaml yet"
```

Also skim `README`/`CONTRIBUTING`/existing CI workflows for stated branch
conventions ("we promote dev → staging → prod"). Those are stronger
signals than branch names alone.

## 3. Infer the promotion graph

From the facts above, form a hypothesis. This is inference — you are
guessing, and you will confirm before acting.

- **Which branches are environments?** Long-lived, environment-named
  branches (`development`/`dev`, `test`, `qa`, `staging`,
  `production*`, `main`/`master`, `prod`, `release/*`) are candidates.
  **Exclude** feature branches, `dependabot/*`, `renovate/*`, `gh-pages`,
  release-automation branches, and anything short-lived. When unsure,
  list it and ask rather than assuming.
- **What order do they promote in?** Branch names do **not** encode
  order — this is the inference most likely to be wrong. Use any stated
  convention, then a conventional lifecycle ordering
  (`development → test → qa → staging → production → main`) as a fallback
  hypothesis. Promotions must form a **DAG** (usually a straight line).
- **Which branch is the source?** The authoritative entry branch where
  work first lands — often `development`/`main`/`master`. It gets
  `role: source` and must never be a promotion *destination*. The
  backflow target must be this branch.
- **Which branch is terminal?** The final branch (often `main`/`prod`)
  gets `role: terminal` and must never be a promotion *source*. This is
  optional — set it only when a branch truly ends the chain.
- **Merge method per edge** (`expectations.mergeMethod`) — reporting-only
  metadata. If the repo disables squash on protected branches, prefer
  `merge`; Oiax detects equivalence more cheaply and exactly without
  squash. Omit if unsure.
- **Backflow.** If hotfixes land directly on downstream branches
  (`production`, `main`) and should return to the source, propose
  `backflow` with those `sources` and the source branch as `target`. If
  the team doesn't hotfix downstream, omit backflow entirely — it's
  optional.

See [modeling your promotion graph](promotion-graphs.md) for roles,
drift, and topologies, and the [configuration
reference](../reference/configuration.md) for every key.

## 4. Confirm the inference with the user (gate — do not skip)

Present your hypothesis back and get explicit sign-off before writing
anything. Show:

1. The **default branch** you detected (where config will live).
2. The **ordered promotion chain** you inferred, and which branches you
   **excluded** as non-environment (so they can correct omissions).
3. The **source** and **terminal** roles.
4. Whether you're proposing **backflow**, and from which sources.
5. The exact `.oiax.yaml` you intend to write.

Ask directly, e.g.: *"I detected these environment branches and inferred
this promotion order: development → test → qa → main, with development as
the source. Is that the correct promotion order, and did I miss or
wrongly include any branch?"* Use pointed questions for the ambiguous
parts (ordering, source/terminal, backflow) rather than a yes/no on the
whole thing.

Only proceed once the user confirms or corrects. Apply corrections and
re-confirm if the shape changed materially.

## 5. Write and locally verify `.oiax.yaml`

Write the confirmed graph to `.oiax.yaml` at the repo root. Skeleton (fill
in from the confirmed shape — see [Quickstart](../../README.md#quickstart)
for a complete example):

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
    main:
      role: terminal
  promotions:
    - from: development
      to: test
    - from: test
      to: qa
    - from: qa
      to: main
  # backflow: (only if confirmed in step 4)
  #   sources: [main]
  #   target: development
  #   strategy: cherry-pick
```

Then verify locally — these commands touch no forge and need no token:

```bash
oiax validate    # semantic check; reports every violation at once
oiax graph       # prints the parsed topology — eyeball it against intent
```

`validate` must exit 0 and `graph` must show the chain you confirmed. If
`validate` reports an undeclared branch or a role violation, fix the YAML
and re-run — do not proceed with a graph that fails validation. (If you
couldn't install the binary in step 1, skip this and rely on the Action's
`mode: validate`; note that you did.)

## 6. Handle adoption (existing promotion PRs)

Before the first `reconcile`, check the open PRs you listed in step 2 for
**hand-made promotion PRs on edges Oiax will manage** (e.g. an existing
open PR from `development` into `test`). Oiax cannot adopt a PR it didn't
create; it will try to open its own on that edge, GitHub rejects the
duplicate (HTTP 422), and `reconcile` fails on that edge.

Do **not** close those PRs yourself. Surface them to the user and
recommend they merge or close each one before Oiax takes over the edge.
Unmanaged PRs on *other* edges are safe and left untouched. See
[getting started — adopting Oiax on an existing
repository](getting-started.md#adopting-oiax-on-an-existing-repository).

## 7. Write the workflow

Save `.github/workflows/oiax.yml`. **Critically, the `push.branches` list
must mirror the branches in the graph** — the workflow trigger cannot read
`.oiax.yaml`, so keep them in sync.

```yaml
name: Oiax
on:
  push:
    branches: [development, test, qa, main]   # MUST match spec.branches
  pull_request:
    types: [closed]
  workflow_dispatch:
  schedule:
    - cron: "17 * * * *"
permissions:
  contents: write        # push oiax/ branches
  pull-requests: write   # manage promotion requests
concurrency:
  group: oiax
  cancel-in-progress: false
jobs:
  reconcile:
    runs-on: ubuntu-latest             # Linux x64/ARM64 only
    steps:
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0               # REQUIRED — full history; shallow degrades detection
      - uses: skaphos/oiax@v1
        with:
          config: .oiax.yaml
          mode: reconcile
          # token: ${{ steps.app-token.outputs.token }}   # see step 8
```

Two things are not optional and worth stating to the user:

- **`fetch-depth: 0`** — a shallow clone has no merge base and produces
  spurious promotion PRs. See [`fetch-depth: 0` is not
  optional](github-action.md#fetch-depth-0-is-not-optional).
- The **`push.branches` list mirrors the graph.** If a branch is added to
  the graph later, it must be added here too.

**Recommend a plan-first rollout.** For a repository adopting Oiax, set
`mode: plan` initially so the first runs only report what they *would* do;
switch to `mode: reconcile` once the user has reviewed a plan. See [roll
out plan-first](recipes.md#roll-out-plan-first).

## 8. Set up the token (human step)

This is the step that trips everyone up, and you cannot do it for the
user. Pull requests created with the default `GITHUB_TOKEN` **do not start
`on: pull_request` checks** — GitHub holds those runs for write-user
approval, so unattended promotion stalls under branch protection.

Tell the user plainly: for production they should configure a **GitHub App
installation token** and wire it into the workflow's `token:` input. Point
them at **[Set up a token that triggers CI](tokens.md)** for the full
walkthrough (creating the App, `actions/create-github-app-token`, the
fine-grained-PAT fallback). The default token works for a first
`mode: plan` trial, but flag the limitation.

## 9. Commit, open a PR, and summarize

- Put `.oiax.yaml` and `.github/workflows/oiax.yml` on a feature branch
  and open a pull request against the default branch. **Do not merge it**
  — that is the user's call, and the config only activates on merge to
  the default branch.
- In the PR description and your final summary, state clearly:
  - the promotion graph you configured (the confirmed chain);
  - that the token step ([step 8](#8-set-up-the-token-human-step)) is
    **required before production** and is theirs to do;
  - whether you set `mode: plan` (recommended first) or `mode: reconcile`;
  - any hand-made promotion PRs from [step 6](#6-handle-adoption-existing-promotion-prs)
    they need to resolve;
  - that they should run/observe one `plan` before enabling `reconcile`.

## Checklist

- [ ] Remote is GitHub; git ≥ 2.45 available where `plan`/`reconcile` run.
- [ ] Default branch identified (config lives there).
- [ ] Environment branches and promotion **order** inferred **and
      confirmed by the user**.
- [ ] `source` (and optionally `terminal`) roles set; backflow decided.
- [ ] `.oiax.yaml` written; `oiax validate` exits 0 and `oiax graph`
      matches intent.
- [ ] Hand-made promotion PRs on managed edges surfaced to the user.
- [ ] `.github/workflows/oiax.yml` written; `push.branches` mirrors the
      graph; `fetch-depth: 0` set.
- [ ] Token step handed off to the user; plan-first rollout recommended.
- [ ] Changes on a branch and opened as a PR — not committed to default,
      not merged, `reconcile` not run without consent.

## Next steps for the user

- [Set up a token that triggers CI](tokens.md) — do this before relying
  on Oiax in production.
- [Operating Oiax day to day](operating.md) — reading plans, reviewing
  managed PRs, branch protection.
- [Troubleshooting](troubleshooting.md) — if a run does something
  unexpected.
