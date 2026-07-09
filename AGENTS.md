# AGENTS.md

Briefing for AI agents and new contributors working in this repository.

## What this is

Oiax is a declarative Git branch promotion reconciler for
branch-per-environment GitOps repositories: it ensures the pull requests
required to move changes through a declared promotion graph exist —
exactly one active managed request per diverged edge — and returns
hotfixes landed on downstream branches to the authoritative source
branch (backflow). It never merges, approves, deploys, or renders
manifests.

Read `docs/architecture.md` before non-trivial changes;
`docs/code-map.md` explains what each package owns. The design proposal
lives in the `skaphos-resources` repository under `tools/oiax/`.

## Safety rules (do not violate)

- `internal/engine` is pure: no provider API calls, no imports of
  `internal/git` or `internal/forge` (depguard-enforced), and identical
  inputs must produce equivalent plans.
- Never force-push refs outside the `oiax/` namespace; never force-push
  long-lived branches under any circumstances.
- Never close, edit, or otherwise touch unmanaged pull requests.
- Branch names are data: validate with `git check-ref-format`, pass with
  `--` separators, never interpolate into shell.
- Configuration is read from a pinned ref and is declarative data —
  never execute anything it defines.
- Credential values must never appear in logs, plans, errors, or docs.

## Building and testing

```bash
go -C tools tool task --list        # all tasks
go -C tools tool task build         # build ./cmd/oiax
go -C tools tool task test          # unit tests
go -C tools tool task lint          # golangci-lint (v2 config)
go -C tools tool task verify-generated
```

CI (`.github/workflows/ci.yml`) gates on: DCO sign-off, REUSE lint,
lint, tests (three OSes), staticcheck + govulncheck, generated-artifact
drift, and a GoReleaser snapshot build.

## Conventions

- Conventional commits, signed, with DCO sign-off — release automation
  depends on them.
- `docs/reference/cli.md` is generated (`task docs:cli-ref`); regenerate
  instead of editing.
- Exit codes, the JSON plan format, managed-request metadata, and
  `pkg/api` are compatibility contracts; changing them needs an ADR
  (`docs/adr/`, immutable, superseded not rewritten).
- Skaphos glossary discipline: this tool does **branch promotion**;
  unqualified "Promotion" is a Keleustes term. The hotfix-return flow is
  **backflow**, never "reconciliation" (that word is reserved for the
  observe/plan/apply loop).

## Release constraints

Releases are cut by automation (release-please, see `RELEASE.md`). Do
not hand-edit `CHANGELOG.md`, `.release-please-manifest.json`, or
`release-please-config.json`, and do not push tags.
