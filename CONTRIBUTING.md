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

## Generated artifacts

`docs/reference/cli.md` is generated from the cobra command tree
(`task docs:cli-ref`). Never edit it by hand — CI fails on drift. If you
change a command or flag, regenerate and commit the result.

## Design invariants

Before touching the engine, read `docs/architecture.md`. In particular:

- `internal/engine` is pure — no provider calls, no `internal/git` or
  `internal/forge` imports (depguard enforces this), deterministic plans.
- Oiax never merges, never approves, never touches unmanaged requests,
  never force-pushes outside the `oiax/` ref namespace.
- Exit codes and the JSON plan format are compatibility contracts;
  changes need an ADR.

Significant, hard-to-reverse decisions get an ADR under `docs/adr/`
(immutable; supersede with a new one).

## Documentation

User-visible behavior changes update user-facing docs in the same PR;
architectural changes update `docs/architecture.md` or add an ADR.
