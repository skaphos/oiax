# Release process

Releases are automated; humans review and merge, automation versions,
tags, and publishes.

## How a release happens

1. **Release PR** (`.github/workflows/release-please.yml`): every push
   to `main` runs [release-please](https://github.com/googleapis/release-please)
   (`googleapis/release-please-action@v5`, configured by
   `release-please-config.json`), which maintains a release pull
   request that aggregates conventional commits into `CHANGELOG.md` and
   bumps `.release-please-manifest.json`. GitHub-release creation is
   skipped (`skip-github-release: true`) — GoReleaser owns that step.
2. **Immutable tag**: after the release PR merges, the same workflow
   pushes the corresponding full `vMAJOR.MINOR.PATCH` annotated tag (as
   `skaphos-release-bot[bot]`) and reconciles the
   `autorelease: pending`/`autorelease: tagged` labels.
3. **Publish** (`.github/workflows/release.yml`): the tag triggers
   GoReleaser, which builds the platform binaries (`.goreleaser.yaml`;
   version metadata injected into `internal/version` via ldflags),
   generates `checksums.txt`, and creates the GitHub release, linking
   to `CHANGELOG.md` for the full notes. Only after publication succeeds,
   the workflow force-advances the floating `vMAJOR` Action tag to the
   release commit. A monotonicity guard prevents a rerun of an older
   release from moving the major tag backward. This follows GitHub's
   [recommended Action release-management
   model](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/release-and-maintain-actions),
   where immutable semantic-version releases coexist with a current
   floating major tag.

The composite Action (`action.yml`) consumes those release artifacts: it
reads the version from `.release-please-manifest.json` at the Action ref,
downloads that binary for the runner platform, and verifies it against
`checksums.txt`. Consumers using `skaphos/oiax@v1` therefore receive the
newest published `v1.x.y` wrapper and binary together; consumers can set
the Action's `version` input to a full tag when they need an exact binary
within the same major. The Action rejects a cross-major override.

## Publishing to the GitHub Marketplace

Listing the Action on the [GitHub Marketplace](https://github.com/marketplace)
is separate from release automation and is a **manual, one-time** step.
GitHub exposes no API for the *Publish this Action to the GitHub Marketplace*
toggle, so neither release-please nor GoReleaser can set it. The listing only
adds discoverability: `skaphos/oiax@v1` resolves from tags and release assets
whether or not a listing exists.

The listing anchors to a **release** — you tick the checkbox on the release
form and choose the categories (primary **Deployment**, secondary
**Utilities**); the icon and colour come from `action.yml`'s `branding`.
Establishing the listing once is enough: it persists across later releases
and only needs re-publishing when you want to feature a newer version.

**Immutability interaction (load-bearing).** An immutable release is locked
at publish time and cannot be edited, so the Marketplace checkbox cannot be
added to it afterward — and disabling the repository's immutable-releases
setting does **not** unlock releases that were already published. Anchor the
listing to a release that is editable when you tick the box: either one
published while the immutable-releases setting is off, or one still in
**draft** (the checkbox is available on a draft, and immutability applies
only once the draft is published). If you draft the target release, publish
it promptly — `action.yml` downloads binaries from the release assets, which
are not publicly readable while the release is a draft.

## Required credentials

Release automation does not run on the default `GITHUB_TOKEN`:

| Input | Where | Purpose |
| --- | --- | --- |
| `vars.RELEASE_BOT_CLIENT_ID` | org/repo variable | GitHub App Client ID used to mint the release bot token (`skaphos-release-bot`). |
| `secrets.RELEASE_BOT_PRIVATE_KEY` | org/repo secret | Private key for that App. |

Both must be provisioned in GitHub before treating release automation as
operational — both release workflows fail without them. The release bot
must be allowed to create immutable `vMAJOR.MINOR.PATCH` tags and
force-update only the floating `vMAJOR` tags (for example `v1`).

## Governance

Release workflows, `release-please-config.json`,
`.release-please-manifest.json`, and this document are code-owner gated
(see `.github/CODEOWNERS`). Exit codes, the JSON
plan format, managed-request metadata, and `pkg/api` are compatibility
surfaces: breaking them requires a major-version discussion, not just a
release.
