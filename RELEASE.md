# Release process

Releases are automated; humans review and merge, automation versions,
tags, and publishes.

## How a release happens

1. **Release PR** (`.github/workflows/release-pr.yml`): every push to
   `main` maintains a release pull request via
   `skaphos/actions/release-pr` — a release-please-style flow
   implemented with [svu](https://github.com/caarlos0/svu): the next
   semantic version is inferred from Conventional Commits, and the PR's
   single change bumps `.release-please-manifest.json` (the filename is
   a Skaphos convention; the tooling is not Google's release-please).
   The PR body lists the commits since the last tag.
2. **Tag** (`.github/workflows/release-tag.yml`): merging the release PR
   (label `release-pr`) pushes the corresponding `v*` tag via
   `skaphos/actions/release-tag`.
3. **Publish** (`.github/workflows/release.yml`): the tag triggers
   GoReleaser, which builds the platform binaries (`.goreleaser.yaml`;
   version metadata injected into `internal/version` via ldflags),
   generates `checksums.txt`, and creates the GitHub release with
   release notes generated from the commit history (there is no
   `CHANGELOG.md` file; the GitHub releases page is the changelog).

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
