# Contributing to oiax

Thanks for contributing. This repository follows the Skaphos repository
governance standard; the short version is below.

## Workflow

- Changes land through pull requests to `main`; no direct commits.
- Keep pull requests focused on one logical change. Explain what
  changed, why, what was tested, and any documentation updates.
- Branch names: `feat/...`, `fix/...`, `docs/...`, `chore/...`.

## Commits

- Use [Conventional Commits](https://www.conventionalcommits.org/)
  (`feat:`, `fix:`, `docs:`, `chore:`, ...) — release automation
  classifies changes from commit messages.
- Two extra types surface operator-facing prose in the changelog and the
  GitHub release body:
  - `migrate:` — **Migration Notes**: anything a consumer must *do* when
    upgrading (an Action input renamed, a config key with a new default,
    a changed exit code). Write the summary line as the instruction, e.g.
    `migrate: set backflow.strategy explicitly; the default changes to merge`.
  - `note:` — **Operator Notes**: user-facing guidance that is neither a
    feature nor a fix (a new warning operators will start seeing, a
    behavior clarification prompted by user feedback).

  Only the commit **summary line** lands in the changelog, so keep it a
  self-contained sentence; longer migration prose belongs in a
  `BREAKING CHANGE:` footer (which gets its own changelog section). These
  types never bump the version by themselves — pair them with the `feat:`
  / `fix:` that motivated them. Because PRs are squash-merged, a PR whose
  title is the `feat:`/`fix:` adds a `migrate:`/`note:` entry via a
  commit-override block in the PR description:

  ```text
  BEGIN_COMMIT_OVERRIDE
  feat: the actual change
  migrate: what the operator must do about it
  END_COMMIT_OVERRIDE
  ```
- Sign commits cryptographically and include a DCO sign-off
  (`git commit -S --signoff`). CI rejects commits without a
  `Signed-off-by:` trailer.

## Local validation

Local checks mirror the CI gates:

```bash
go -C tools tool task fmt               # goimports + go fmt
go -C tools tool task lint              # golangci-lint (incl. depguard purity rules)
go -C tools tool task test              # go test ./...
go -C tools tool task staticcheck vuln  # static analysis + govulncheck
go -C tools tool task verify-generated  # generated CLI reference is current
```

Run `go -C tools tool task --list` for everything else. Go version and
tool pins live in `.tool-versions` and `go.mod`.

Linux is the supported automation platform. CI runs `test-cover` (including
Git-backed CLI/reconcile scenarios) and `test-race` there. macOS and Windows
run `test-portability`: a native build of every package and an explicit unit
suite covering the entrypoint, wrapper contracts, configuration, engine,
provider-neutral types/markers, and templates. Git-backed integration and
provider subprocess fixtures are excluded from that task, not deleted.

For a quick local check, run `go -C tools tool task test-portability`.
The full suite remains available on any OS through `task test`, but is not
a non-Linux support guarantee. Review new unit packages for inclusion in the
portability task; do not add expensive subprocess scenarios to it.
Linux has a 25-minute CI job budget with ten-minute package deadlines.
Portability jobs have ten minutes, with three-minute unit-package deadlines.
All existing `Test (<runner>)` check names are preserved, avoiding a
branch-protection migration. See [platform support](docs/reference/platform-support.md)
and [ADR 0016](docs/adr/0016-linux-automation-support.md).

## Generated artifacts

`docs/reference/cli.md` is generated from the cobra command tree
(`task docs:cli-ref`). Never edit it by hand — CI fails on drift. If you
change a command or flag, regenerate and commit the result.

## Design invariants

Before touching the engine, read `docs/architecture.md`. In particular:

- `internal/engine` is pure — no provider calls, no `internal/git` or
  `internal/forge` imports (depguard enforces this), deterministic plans.
- Oiax never merges, never approves, never touches unmanaged requests,
  and never force-pushes long-lived branches. Its owned ref families are
  `refs/heads/oiax/` and `refs/notes/oiax/`; notes writes require explicit
  expected-tip, append-only updates with no deletion or rewind (ADR 0015).
- Exit codes and the JSON plan format are compatibility contracts;
  changes need an ADR.

Significant, hard-to-reverse decisions get an ADR under `docs/adr/`
(immutable; supersede with a new one).

## Documentation

User-visible behavior changes update user-facing docs in the same PR;
architectural changes update `docs/architecture.md` or add an ADR.
