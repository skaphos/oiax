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
2. **Tag**: after the release PR merges, the same workflow pushes the
   corresponding `v*` annotated tag (as `skaphos-release-bot[bot]`) and
   reconciles the `autorelease: pending`/`autorelease: tagged` labels.
3. **Publish** (`.github/workflows/release.yml`): the tag triggers
   GoReleaser, which builds the platform binaries (`.goreleaser.yaml`;
   version metadata injected into `internal/version` via ldflags),
   generates `checksums.txt`, and creates the GitHub release, linking
   to `CHANGELOG.md` for the full notes.

The composite Action (`action.yml`) consumes those release artifacts: it
downloads the binary for the runner platform and verifies it against
`checksums.txt`. Cutting a release is therefore also what makes a new
version available to `skaphos/oiax@...` users.

## Required credentials

Release automation does not run on the default `GITHUB_TOKEN`:

| Input | Where | Purpose |
| --- | --- | --- |
| `vars.RELEASE_BOT_APP_ID` | org/repo variable | GitHub App ID used to mint the release bot token (`skaphos-release-bot`). |
| `secrets.RELEASE_BOT_PRIVATE_KEY` | org/repo secret | Private key for that App. |

Both must be provisioned in GitHub before treating release automation as
operational — the release-please workflow fails without them. The
publish workflow uses the default `GITHUB_TOKEN` (`contents: write`).

## Governance

Release workflows, `release-please-config.json`,
`.release-please-manifest.json`, and this document are code-owner gated
(see `.github/CODEOWNERS`). Exit codes, the JSON
plan format, managed-request metadata, and `pkg/api` are compatibility
surfaces: breaking them requires a major-version discussion, not just a
release.
