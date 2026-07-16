# 0007 — Keep the git layer a shell-out to the git binary

- Status: accepted
- Date: 2026-07-16

## Context

`internal/git` executes the system `git` binary through a single
`Runner`, the primary git seam. A few callers outside it shell out
directly, under the same posture: commit-trailer reads in
`internal/reconcile`, push and ref validation in `internal/forge/github`,
and origin resolution in `internal/cli`. The engine never shells out at
all. The layer enforces a version floor — git 2.45, required for
`cherry-pick --empty=drop` — with a startup check and a clear error. External review of the 1.0 codebase
recommended migrating the core operations (refs, reachability,
patch-ids, cherry-pick, tree comparison) to a pure-Go library such as
go-git, or to libgit2 bindings, citing portability, easier mocking, and
removal of the version floor.

The operations Oiax actually depends on are the deciding constraint.
The equivalence ladder needs `patch-id --stable`; backflow execution
needs `cherry-pick -x --empty=drop`, ephemeral worktrees
(`worktree add --detach` / `remove --force`), and exact distinction
between a content conflict (exit 1) and an operational failure. The
documented execution model is a GitHub Action on hosted runners, where
a current git binary is unconditionally present.

## Options considered

- **Keep the shell-out (status quo).** One narrow, tested seam; behavior
  is byte-identical to the git users run by hand, including trailer
  formats (`(cherry picked from commit …)`) that double as durable
  state under the no-database posture.
- **go-git (pure Go).** Removes the binary dependency, but the
  capability math fails: go-git implements no cherry-pick, no
  `patch-id --stable`, no `--empty=drop` semantics, and its worktree
  support is immature. Adopting it means reimplementing precisely the
  plumbing the backflow path and ladder rung 2 depend on — a large
  rewrite of the most correctness-sensitive code in the project, with
  the burden of proving the reimplementation matches git's output
  commit-for-commit (patch-ids and trailer bytes are load-bearing).
- **libgit2 via cgo bindings.** Closer to feature parity, but still no
  `patch-id --stable` equivalent, and cgo breaks the trivially
  cross-compiled release matrix (GoReleaser currently builds static
  binaries per platform) while replacing a universally-present runtime
  dependency with a vendored C library to patch and re-release for.

## Decision

Keep `internal/git` a shell-out to the system git binary, behind the
`Runner` seam, and keep the direct shell-outs elsewhere to the same
posture rather than growing them. The version floor stays and stays
checked at startup. Mockability is already served where it matters —
the engine is pure and never sees a subprocess — and the reconcile tests
exercise real git against throwaway repositories, which is a feature:
the tests verify the behavior users' own git will exhibit.

Revisit only if the execution model changes to environments where a
git binary cannot be assumed (which today it can, on every supported
runner). Portability pressure short of that is answered by documenting
the floor per environment, not by rewriting the layer.

## Consequences

- The hard runtime dependency on git ≥ 2.45 remains, and so does the
  startup check and its documentation burden per environment.
- Error handling continues to parse exit codes and stderr; git's CLI
  contract (exit 1 for a content conflict) is part of Oiax's
  correctness surface and is pinned by tests.
- Subprocess overhead persists, but is noise against network and clone
  cost in the Action execution model.
- Oiax inherits git's behavior — including fixes and performance work —
  for free, and never diverges from what an operator reproducing a step
  by hand will see.

## Links

- [ADR 0002](0002-content-based-divergence-detection.md) — the ladder
  rungs the library options cannot supply.
- [ADR 0004](0004-backflow-execution.md) — the cherry-pick execution
  mechanics that make the capability gap disqualifying.
