# Upgrading to v2

[Oiax v2.0.0](https://github.com/skaphos/oiax/releases/tag/v2.0.0) adds
optional managed-request notifications for Teams Workflows, Slack, and generic
HTTPS webhooks. It also makes Linux the supported platform for production
automation. That support-policy reduction is the breaking change recorded in
[ADR 0016](../adr/0016-linux-automation-support.md).

## 1. Check the runner and keep the existing graph

Use Linux amd64/arm64 for automation, including standalone CLI jobs. Existing
Linux Action and Azure Pipelines users need no runner migration. Move native
macOS/Windows automation to Linux before upgrading; those binaries remain
available for best-effort local use. Keep Git 2.45 or newer and a full-history
checkout (`fetch-depth: 0` on GitHub, `fetchDepth: 0` on Azure).

Keep `apiVersion: oiax.skaphos.dev/v1` in `.oiax.yaml`. The release major is
not the configuration API version. Existing graph configuration, CLI arguments,
core exit codes, `planFormatVersion: 1`, and managed-request ownership markers
are unchanged. Notifications are optional: without `spec.notifications`, no
notification I/O occurs.

## 2. Upgrade the wrapper and binary together

### GitHub Action

Change `uses: skaphos/oiax@v1` to `uses: skaphos/oiax@v2`:

```yaml
- uses: actions/checkout@v7
  with:
    fetch-depth: 0
- uses: skaphos/oiax@v2
  with:
    config: .oiax.yaml
    mode: plan
```

Retain your existing token setup, configuration ref, workflow triggers, and
concurrency settings. Use `mode: plan` for the initial inspection, then restore
`mode: reconcile` to apply the reviewed plan.

The `v2` tag follows published 2.x releases. Its manifest selects the matching
binary automatically; remove an old `version: v1.x.y` override or replace it
with an exact 2.x version. `@v1` stays on the 1.x line and cannot run a 2.x
binary via the `version` input. For an exact wrapper-and-binary pin, use
`skaphos/oiax@v2.0.0` and omit `version`. See the
[Action guide](github-action.md) for the complete workflow.

### Azure Pipelines

Update both the `oiax` repository resource's `ref` and the template's `version`
parameter to the same release:

```yaml
resources:
  repositories:
    - repository: oiax
      type: github
      name: skaphos/oiax
      ref: refs/tags/v2.0.0
      endpoint: <github-service-connection>

pool:
  vmImage: ubuntu-24.04

steps:
  - checkout: self
    fetchDepth: 0
    persistCredentials: true
  - template: templates/azure-pipelines/oiax.yml@oiax
    parameters:
      version: v2.0.0
      mode: plan
      githubToken: $(OIAX_GITHUB_TOKEN)
```

This fragment assumes a GitHub-hosted repository. For Azure Repos, retain your
existing `azureDevOpsToken` and work-item configuration instead of `githubToken`.
The template cannot infer its release from its ref, so changing only one pin
is insufficient. See the [Azure guide](azure-pipelines.md) for full pipelines.

### Standalone CLI

Install the matching archive from the v2.0.0 release and verify it against that
release's `checksums.txt`. Confirm `oiax version` reports `2.0.0` before adding
notification policy. Production jobs must use the Linux archive for their
architecture.

## 3. Enable notifications separately

After upgrading every job that reads the graph, follow the
[notification setup guide](notifications.md). Older binaries do not understand
`spec.notifications`, so do not add it while v1 jobs still consume that pinned
configuration ref.

- Bind endpoint variables through the runner's secret store; never commit
  webhook URLs or other credential values.
- Verify the existing forge credential can read and update the exact
  `refs/notes/oiax/notifications/v1/<graph-key>` ref. Do not broaden unrelated
  branch permissions.
- Run `validate` and `plan` against the intended pinned policy before enabling
  delivery with `reconcile`. A preview does not test receiver credentials.
- The first enabled reconcile establishes a cutoff; historical merges are not
  replayed. Merge alerts are the default; creation alerts are opt-in. Email
  remains a separate follow-up, not part of v2.0.0.

During adoption, confirm a new managed-request event reaches the intended
receiver and inspect subsequent runs for a saved delivery receipt. Live endpoint
acceptance was deferred to post-release adoption; passing unit/CI tests is not
evidence of recipient visibility. Ambiguous HTTP acceptance can still produce
duplicates; there is no exactly-once delivery guarantee.

## If you need to return to v1

If you have not added notification policy, restore your prior Action ref or
both prior Azure version pins. If you have added it, remove
`spec.notifications` from the pinned configuration consumed by v1 jobs before
downgrading. Do not delete or rewind the notification notes ledger. Leaving
that ledger intact is not a promise of duplicate-free replay after a downgrade;
review [delivery recovery](notifications.md#delivery-and-recovery) before
re-enabling notifications. Pinning an older release does not create a new
maintenance commitment for that release line.
