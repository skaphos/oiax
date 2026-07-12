# Setting up a token that triggers CI

Read this before you rely on Oiax in production. It is the single most
common way an Oiax deployment silently half-works.

## The problem

Oiax authenticates to GitHub with the token you pass as the Action's
`token` input (or `GITHUB_TOKEN` in the environment for the CLI). By
default that is `${{ github.token }}` — the workflow's built-in
`GITHUB_TOKEN`.

Pull requests created with `GITHUB_TOKEN` **do not trigger other
workflows.** This is GitHub's deliberate recursion guard: it stops an
Action from creating a PR that re-runs the Action forever. The
consequence for Oiax:

- A managed promotion PR that Oiax opens with `GITHUB_TOKEN` gets **no
  `on: pull_request` runs** — no required checks fire.
- Under branch protection that requires those checks, the PR **can never
  become mergeable.** Promotion stalls silently.

Oiax detects this and warns once:

```
created pull request is authored by github-actions[bot]; on: pull_request workflows will not run for it. Configure a GitHub App installation token so managed requests get CI.
```

`GITHUB_TOKEN` is only acceptable when **no required checks guard your
promotion targets** — for example a trial run, or a repo where promotion
PRs merge without CI. Everywhere else, use a GitHub App installation
token.

## The options, ranked

1. **GitHub App installation token** — recommended for production. PRs it
   creates are authored by your App, so they trigger workflows normally.
   No human owns the credential and it rotates automatically.
2. **Fine-grained personal access token (PAT)** — works, but a person
   owns it and you carry the rotation burden. Acceptable for a team
   without an App.
3. **`GITHUB_TOKEN`** — works out of the box, degraded: created PRs get
   no CI. Only when no required checks gate promotion.

## Recommended: a GitHub App installation token

You create a small GitHub App once, install it on the repository, and let
the workflow mint a short-lived installation token per run with
[`actions/create-github-app-token`](https://github.com/actions/create-github-app-token).

### 1. Create the App

Under **Settings → Developer settings → GitHub Apps → New GitHub App**
(in your org if the repo is org-owned):

- **Name:** anything, e.g. `oiax-promoter`.
- **Homepage URL:** any valid URL.
- **Webhook:** uncheck **Active** — Oiax does not receive webhooks.
- **Repository permissions:**
  - **Contents: Read and write** — push `oiax/` backflow branches.
  - **Pull requests: Read and write** — create, update, comment on, and
    close managed requests.
  - Leave everything else at **No access**.

Create the App, then note its **App ID** and generate a **private key**
(a `.pem` download).

### 2. Install it on the repository

From the App's page, **Install App**, and grant it access to the
repository (or repositories) Oiax reconciles.

### 3. Store the credentials

In the repository (or org) settings, under **Secrets and variables →
Actions**:

- Add a **variable** `OIAX_APP_ID` = the App ID.
- Add a **secret** `OIAX_APP_PRIVATE_KEY` = the full contents of the
  `.pem` private key.

(Names are yours to choose; these match the example below.)

### 4. Wire it into the workflow

Mint a token in a step and pass it to Oiax:

```yaml
jobs:
  reconcile:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/create-github-app-token@v1
        id: app-token
        with:
          app-id: ${{ vars.OIAX_APP_ID }}
          private-key: ${{ secrets.OIAX_APP_PRIVATE_KEY }}

      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
          token: ${{ steps.app-token.outputs.token }}

      - uses: skaphos/oiax@v1
        with:
          config: .oiax.yaml
          mode: reconcile
          version: v1.0.0
          token: ${{ steps.app-token.outputs.token }}
```

Passing the App token to `actions/checkout` too means the branches Oiax
pushes are authored by the App, so any push-triggered checks also fire.

After this, managed promotion PRs are authored by your App, `on:
pull_request` workflows run for them, required checks report, and the PRs
can merge. The degradation warning disappears.

## Alternative: a fine-grained PAT

If you cannot create an App, a fine-grained PAT works. Grant it, scoped to
the repository:

- **Contents: Read and write**
- **Pull requests: Read and write**

Store it as a secret (e.g. `OIAX_TOKEN`) and pass it directly:

```yaml
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
          token: ${{ secrets.OIAX_TOKEN }}
      - uses: skaphos/oiax@v1
        with:
          version: v1.0.0
          token: ${{ secrets.OIAX_TOKEN }}
```

The tradeoff is ownership: a PAT belongs to a person and expires on the
schedule you set, so you must rotate it. Prefer the App for anything
long-lived.

## Local CLI use

Running `oiax plan` or `oiax reconcile` from your machine uses
`GITHUB_TOKEN` from the environment:

```bash
export GITHUB_TOKEN=github_pat_...
oiax plan
```

For local inspection that is fine — you are usually reading, not creating
PRs that need CI. Reserve the App token for the automated runs that
actually open managed requests.

## Why not native App auth in Oiax?

Supplying an App **installation token** (as above) works from the first
release. Oiax minting tokens from an App key itself — so you would hand it
an App ID and private key directly — is a later milestone (see the
[roadmap](../architecture.md#roadmap)). The installation-token approach
above needs nothing further from Oiax.

## Next steps

- [Deploy Oiax as a GitHub Action](github-action.md) — the full workflow.
- [Operating Oiax day to day](operating.md).
