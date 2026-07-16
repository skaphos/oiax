# 0006 — Merge-commit backflow strategy

- Status: proposed
- Date: 2026-07-16

## Context

[ADR 0004](0004-backflow-execution.md) fixed cherry-pick as the only
backflow strategy, and the config API reserved the field for it
(`backflow.strategy`, default and sole value `cherry-pick`). Operator
feedback asks for a merge-commit return: teams that already require
merge commits on promotion edges want the same shape on the return path
— one merge commit that says "this was a deliberate return of hotfix X",
one SHA to revert, and a history consistent with how everything else
lands.

The request is not a toggle on the existing mechanics. ADR 0004's
concurrency and identity story is built out of cherry-pick specifics:

- **Determinism** comes from replaying commits with committer identity
  and date pinned to the originals, so racing runs produce
  byte-identical pushes and converge on one branch head.
- **Identity** ("is this commit already returned?") rests on two
  cherry-pick artifacts — the `-x` provenance trailer and stable
  patch-ids — plus the `Oiax-Backflow: skip` trailer as the human
  escape hatch.
- **Selectivity** is per-commit: a conflicting or deliberately
  downstream-only commit can be excluded while the rest return.

A merge-based return replaces all three at once. Any design must also
survive the constraint that carried ADR 0004: no private state
database, and overlapping event-driven runs that cannot rely on locks
or ordering.

## Options considered

- **Keep cherry-pick as the only strategy.** No new surface. Teams
  wanting merge-shaped history keep getting per-commit replays; the
  audit trail for "hotfix X returned" is N cherry-picked commits and a
  managed-request body, not one merge.
- **`strategy: merge` — a real merge of the source head.** The return
  branch is taken from the current target head and the backflow source
  head is merged into it (`git merge --no-ff`), committer identity and
  date pinned for the same byte-identical-push determinism as ADR 0004.
  The managed request returns everything downstream-only, wholesale.
  Identity becomes ancestry: once the return merges (as a merge
  commit), the source head is reachable from the target and the edge is
  conclusively settled by the cheapest rung there is.
- **`strategy: merge` via the forge's merge API instead of a local
  merge.** Delegates conflict detection to the forge, but the merge
  then happens outside the deterministic-branch model: no local branch
  to converge on, no plan-time conflict preview, and forge-specific
  semantics leak into the engine. Rejected as a variant.

## Decision

*Proposed, not accepted — this ADR records the design so it can be
weighed; implementation is deliberately deferred until the consequences
below are accepted or the demand disappears.*

Extend `backflow.strategy` with `merge`. Per (source, target) edge:

- **Branch and naming unchanged.** The return branch keeps the ADR 0004
  deterministic name `oiax/backflow/<source>-to-<target>/<short-sha>`,
  keyed to the source head; supersede-and-close works identically.
- **Replay becomes one merge.** In the ephemeral worktree, merge the
  source head onto the current target head with `--no-ff` and pinned
  committer identity/date. A conflict aborts, pushes nothing, and
  surfaces as a reported divergence (exit 3), exactly as ADR 0004
  handles a conflicting cherry-pick.
- **Identity becomes ancestry plus baseline.** After the return merges,
  the backflow source's head is an ancestor of the backflow target, so
  the downstream-only range (`target..source`) is empty by ancestry —
  exact and cheap, and the patch-id and provenance filters never have to
  run. (This is the ancestry computation that *produces* the
  downstream-only set, not rung 1 of the forward promotion ladder, which
  tests the opposite range.)
- **Validation must fence the merge method.** The strategy only works
  if the managed request itself merges as a **merge commit** — squash
  or rebase at that moment destroys the ancestry the identity model
  depends on, and the same content would be re-proposed forever. This
  is **new enforcement, not a stricter version of an existing check**:
  `mergeMethod` today lives on `Expectations`, which hangs off a
  `Promotion` only, and `warnMergeMethodMismatch` walks `Promotions`
  alone — neither reaches a backflow edge. Adopting `merge` therefore
  requires a new config surface for the backflow edge's expected merge
  method, plus a reconcile-time check that fences it. Cost to be
  budgeted with SKA-562, not absorbed by it.

## Consequences

- **Positive:** one merge commit per return — a single revert point,
  durable "hotfix X was returned" traceability, and identity that is
  exact rather than best-effort. History matches merge-commit promotion
  edges.
- **Negative:** the return is all-or-nothing. There is no per-commit
  selectivity: `Oiax-Backflow: skip` is inapplicable under `merge` (you
  cannot exclude one commit from a merge without rewriting it), so a
  deliberately downstream-only commit forces the edge back to
  cherry-pick or manual handling. Everything downstream-only comes
  back, including commits a reviewer might have wanted to hold.
- **Negative:** correctness now depends on forge-side merge behavior.
  A repository that later enables squash-only merging silently breaks
  the identity model; the validation fence catches the config, not an
  org-policy change made behind it.
- **Negative:** a second execution path through the most
  correctness-sensitive code (worktree replay, conflict handling,
  supersede), roughly doubling the backflow test matrix.
- **Neutral:** `cherry-pick` remains the default and the general-case
  recommendation (selective, works under any merge policy). This ADR
  does not supersede ADR 0004 — it extends its execution model with a
  second strategy under the same naming, isolation, and supersede
  rules.

## Links

- Extends [ADR 0004 — Backflow execution](0004-backflow-execution.md)
- Identity posture: [ADR 0002 — Detect divergence by content, not
  ancestry](0002-content-based-divergence-detection.md) (a merge-commit
  return restores plain ancestry as the identity signal, which is what
  the content rungs exist to substitute for when it is absent)
