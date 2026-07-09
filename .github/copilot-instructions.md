# Copilot instructions for oiax

Oiax is a declarative Git branch promotion reconciler for
branch-per-environment GitOps repositories, written in Go. Read
`docs/architecture.md` before making non-trivial changes.

## Invariants that must not be violated

- The engine (`internal/engine`) is provider-neutral and pure: no
  provider API calls, no imports of `internal/git` or `internal/forge`
  (enforced by depguard), and identical inputs must produce equivalent
  plans.
- Oiax never merges, approves, or closes unmanaged requests; it never
  creates long-lived branches, and force-push is confined to the `oiax/`
  ref namespace.
- Configuration is read from a pinned ref, never from the triggering
  ref. Configuration is declarative data — never execute commands from it.
- Branch names are passed to git as data (validated with
  `git check-ref-format`, `--` separators), never interpolated into shell.
- Credential values must never appear in logs, plans, or errors.
- Managed requests are identified by body metadata and branch
  relationship, never by title.

## Workflow expectations

- Changes land through pull requests with signed, DCO signed-off commits
  and conventional commit messages (release tooling depends on them).
- Local validation mirrors CI: `go -C tools tool task lint test
  staticcheck vuln verify-generated`.
- `docs/reference/cli.md` is generated (`task docs:cli-ref`); never edit
  it by hand.
- Exit codes and the JSON plan format are compatibility contracts;
  changing them requires a documented decision (ADR) and a major/minor
  version discussion.
