# Setting up a token that triggers CI

Read this before you rely on Oiax in unattended production. Token choice
determines whether checks on Oiax-managed pull requests start
automatically or wait for manual approval.

## The problem

Oiax authenticates to GitHub with the token you pass as the Action's
`token` input (or `GITHUB_TOKEN` in the environment for the CLI). By
default that is `${{ github.token }}` — the workflow's built-in
`GITHUB_TOKEN`.

GitHub applies a recursion guard to events created with `GITHUB_TOKEN`.
For pull requests, `opened`, `synchronize`, and `reopened` events now
create workflow runs in an **approval-required** state; a user with write
access must approve them. Other pull-request activity types still do not
create runs. GitHub documents the current behavior under
[triggering a workflow from a workflow](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow#triggering-a-workflow-from-a-workflow).
The consequence for Oiax:

- A managed promotion PR that Oiax opens with `GITHUB_TOKEN`
  does not start its `on: pull_request` checks automatically.
- Under branch protection that requires those checks, unattended
  promotion stalls until a write user approves the queued runs.

Oiax detects this identity and emits the following conservative warning
once (its wording predates GitHub's approval-required behavior):

```
created pull request is authored by github-actions[bot]; on: pull_request workflows will not run for it. Configure a GitHub App installation token so managed requests get CI.
```

`GITHUB_TOKEN` is acceptable for an attended trial, or when no required
checks guard your promotion targets. For unattended operation, use a
GitHub App installation token.

## The options, ranked

1. **GitHub App installation token** — recommended for production. PRs it
   creates are authored by your App, so they trigger workflows normally.
   No human owns the credential and it rotates automatically.
2. **Fine-grained personal access token (PAT)** — works, but a person
   owns it and you carry the rotation burden. Acceptable for a team
   without an App.
3. **`GITHUB_TOKEN`** — works out of the box, degraded: Oiax-created PRs
   need a write user to approve their initial queued workflow runs. Use
   for attended operation or when no required checks gate promotion.

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

Create the App, then note its **Client ID** and generate a **private key**
(a `.pem` download).

### 2. Install it on the repository

From the App's page, **Install App**, and grant it access to the
repository (or repositories) Oiax reconciles.

### 3. Store the credentials

In the repository (or org) settings, under **Secrets and variables →
Actions**:

- Add a **variable** `OIAX_APP_CLIENT_ID` = the Client ID.
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
      - uses: actions/create-github-app-token@v3.2.0
        id: app-token
        with:
          client-id: ${{ vars.OIAX_APP_CLIENT_ID }}
          private-key: ${{ secrets.OIAX_APP_PRIVATE_KEY }}

      - uses: actions/checkout@v7
        with:
          fetch-depth: 0
          token: ${{ steps.app-token.outputs.token }}

      - uses: skaphos/oiax@v1
        with:
          config: .oiax.yaml
          mode: reconcile
          token: ${{ steps.app-token.outputs.token }}
```

The App token must be on **both** steps — this is not optional, and the
reason is subtler than it looks.

Oiax authenticates its own `git push` of an `oiax/` branch (the
backflow/promotion branches) with the token from its `token` input: it
supplies an `AUTHORIZATION` header for the push rather than putting a
credential in the remote URL. But `actions/checkout`, with its default
`persist-credentials: true`, also writes a credential for `github.com`
into the repository's git config — and git sends **both**. The push is
attributed to checkout's persisted credential, so that is the one that
decides whether the push is allowed, whatever you pass to Oiax's `token`
input.

The requirement, then, is not that checkout supply Oiax's push auth —
Oiax has its own — but that checkout must not persist a *different*
credential than the one Oiax pushes with. Giving both steps the same App
token satisfies that, and attributes the ref-preparation fetch to the App
as well. Oiax's `token` input still covers its REST API calls — opening
and managing the pull requests. Push and API are then both attributed to
the App and can trigger workflows automatically.

If you leave `actions/checkout` on the default `GITHUB_TOKEN`, the push is
attributed to its persisted `github-actions[bot]` credential. Once you
also apply the recommended hardening — reducing the workflow's own
`GITHUB_TOKEN` to `contents: read` because the App now carries the write
scopes — that `github-actions[bot]` credential has no push permission, and
the first backflow or promotion fails:

```text
remote: Permission to <owner>/<repo>.git denied to github-actions[bot].
fatal: unable to access 'https://github.com/<owner>/<repo>.git/': The requested URL returned error: 403
```

This surfaces only on a run that actually pushes: `validate` and `plan`
never push, so the wiring can look correct until the first `reconcile`
that opens a backflow or promotion branch.

The fix is to pass the App token to `actions/checkout`, as shown above.
Setting `persist-credentials: false` is an alternative rather than an
equivalent: it stops checkout installing a competing credential, so Oiax's
own push auth stands alone — but checkout's fetch then still runs as
`github-actions[bot]`, which means the workflow's `GITHUB_TOKEN` must keep
`contents: read`.

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
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0
          token: ${{ secrets.OIAX_TOKEN }}
      - uses: skaphos/oiax@v1
        with:
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
release. Oiax minting tokens from App credentials itself — so you would
hand it a Client ID and private key directly — is a possible later
capability (see the
[roadmap](../architecture.md#roadmap)). The installation-token approach
above needs nothing further from Oiax.

## Next steps

- [Deploy Oiax as a GitHub Action](github-action.md) — the full workflow.
- [Operating Oiax day to day](operating.md).
