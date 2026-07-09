# Release process

Releases are automated; humans review and merge, automation versions,
tags, and publishes.

## How a release happens

1. **Release PR** (`.github/workflows/release-pr.yml`): every push to
   `main` maintains a release pull request via
   `skaphos/actions/release-pr`, which aggregates conventional commits
   into `CHANGELOG.md` and computes the next semantic version
   (`.release-please-manifest.json` records the current one).
2. **Tag** (`.github/workflows/release-tag.yml`): merging the release PR
   (label `release-pr`) pushes the corresponding `v*` tag via
   `skaphos/actions/release-tag`.
3. **Publish** (`.github/workflows/release.yml`): the tag triggers
   GoReleaser, which builds the platform binaries (`.goreleaser.yaml`;
   version metadata injected into `internal/version` via ldflags),
   generates `checksums.txt`, and creates the GitHub release.

The composite Action (`action.yml`) consumes those release artifacts: it
downloads the binary for the runner platform and verifies it against
`checksums.txt`. Cutting a release is therefore also what makes a new
version available to `skaphos/oiax@...` users.

## Required credentials

Release automation does not run on the default `GITHUB_TOKEN`:

| Input | Where | Purpose |
| --- | --- | --- |
| `vars.HOMEBREW_APP_ID` | org/repo variable | GitHub App ID used to mint the release bot token. |
| `secrets.HOMEBREW_APP_PRIVATE_KEY` | org/repo secret | Private key for that App. |

Both must be provisioned in GitHub before treating release automation as
operational — the release PR and tag workflows fail without them. The
publish workflow uses the default `GITHUB_TOKEN` (`contents: write`).

## Governance

Release workflows, `.release-please-manifest.json`, and this document
are code-owner gated (see `.github/CODEOWNERS`). Exit codes, the JSON
plan format, managed-request metadata, and `pkg/api` are compatibility
surfaces: breaking them requires a major-version discussion, not just a
release.
