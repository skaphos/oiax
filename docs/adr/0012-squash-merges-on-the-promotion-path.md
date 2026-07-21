# 0012 — Squash merges on the promotion path

- Status: accepted
- Date: 2026-07-20

## Context

Backflow ([ADR 0004](0004-backflow-execution.md)) returns downstream-only
commits to the source branch by replaying them with `git cherry-pick -x`,
one commit at a time. Its identity ladder decides "is this commit already
on the target?" by content — patch-id, the cherry-pick `-x` provenance
trailer, and the O6 `Oiax-Backflow: skip` escape hatch — precisely so a
SHA rewrite (which every promotion and every replay performs) does not
defeat it (ADR 0002).

A **squash merge** breaks that per-commit model. GitHub's squash button
collapses every commit of a merged PR into one commit whose diff is the
*combined* patch, stamped with a new SHA. On a promotion-path branch this
produces a single downstream-only commit that:

- has a patch-id matching no single upstream commit, so the patch-id rung
  never marks it returned; and
- **mixes** content that originated upstream (already on the backflow
  target) with any genuinely-downstream change fused into the same squash.

Backflow then tries to return the whole combined commit. The already-present
parts collide with the target and the cherry-pick conflicts — every run,
forever, because nothing about the situation changes on its own. This is
not hypothetical: it is exactly how `oiax-sample` wedged its hourly
`reconcile` after a `test -> qa` PR was squash-merged, fusing a real
production hotfix with content that had flowed down from `development`.

Two facts bound what Oiax can do about it:

- **A mixed squash is irreducible by content identity.** You cannot
  cherry-pick "just the hotfix" out of a combined patch; the fix and the
  already-present content are one blob. No new identity rung resolves it —
  a squash of purely-already-returned content should already converge
  (the patch is empty after `--empty=drop`), and a squash of purely-new
  content cherry-picks cleanly. Only the *mixed* case conflicts, and it
  needs a human.
- **A squash cannot be reliably detected before it conflicts.** Since a
  squash is only a problem when it conflicts, pre-classifying a commit as
  "a squash that will fail" at plan time risks false positives that
  silently suppress a legitimate return. The conflict itself is the
  reliable signal.

There is also a symmetric, *benign* squash: a backflow **return** branch
squash-merged into the source. That collapses several returned commits'
`(cherry picked from commit …)` lines into one commit, each surviving at
the tail of its own block. The provenance rung read only the message's
**last** paragraph ([ADR 0004](0004-backflow-execution.md) ladder rung 1),
so it saw only the final block's line and re-proposed every other returned
commit — a spurious divergence on an already-settled edge.

## Options considered

- **Auto-resolve or auto-exclude a mixed squash.**
  - *Add an identity rung that excludes a squash whose combined content is
    "mostly" on the target.* There is no correct threshold: a squash that
    is 90% already-present still carries a 10% hotfix that must return,
    and excluding it silently drops that fix. Rejected — it trades a loud,
    fixable conflict for a silent data loss.
  - *Attempt a three-way or partial cherry-pick.* Automatic conflict
    resolution is explicitly out of scope (ADR 0004); a combined patch has
    no clean sub-parts to apply. Rejected.
- **Surface the squash as human-actionable guidance at conflict time.**
  When a backflow cherry-pick conflicts, detect whether the failing commit
  is a rollup and, if so, enrich the existing exit-3 divergence and durable
  conflict artifact ([ADR 0008](0008-durable-backflow-conflict-artifact.md))
  with squash-specific guidance that names the skip escape hatch. Purely
  additive to the human text; changes no control flow, no exit code, and no
  set of attempted commits. This is the chosen behavior.
- **How to detect a rollup.**
  - *Parse the GitHub squash body shape (`* bullet` lines, `---------`).*
    Fragile, forge-specific, locale- and config-dependent. Rejected.
  - *Count the cherry-pick `-x` provenance lines the commit carries.* A
    plain `git cherry-pick -x` writes exactly one; a commit bearing **two
    or more** is a rollup of that many upstream commits — a reliable,
    forge-neutral signal that reuses machinery Oiax already has. Chosen. It
    only fires when the rolled-up commits themselves carried provenance
    (Oiax's own promotions do); a hand-squash of provenance-free commits is
    not flagged, which is an accepted blind spot — the generic conflict
    guidance still applies and still names the skip hatch.
- **The benign squashed-return miss.**
  - *Keep last-paragraph-only provenance matching.* Leaves a squashed
    return re-proposing all-but-one returned commit forever. Rejected.
  - *Match a standalone provenance line at the tail of ANY paragraph.*
    Recovers every rolled-up provenance line while preserving the guard the
    last-paragraph rule provided — a provenance phrase merely quoted inside
    a prose paragraph, not at its tail, is still ignored. Chosen.

## Decision

**D1 — Oiax does not auto-resolve a mixed squash.** A backflow replay that
conflicts stays a reported divergence (exit 3) with a durable conflict
artifact; Oiax never guesses at splitting a combined patch. The exit-code
contract (0 / 1 / 3) and the artifact lifecycle are unchanged.

**D2 — The conflict surface is squash-aware.** When a backflow cherry-pick
or merge conflicts, `recordBackflowConflict` counts the distinct cherry-pick
`-x` provenance ids in the **failing commit's** own body
(`squashCommitCount`). When that count is `>= 2`, the conflict template
renders an extra paragraph: it states the commit combines *N* cherry-picked
commits and looks like a squash merge, explains that a squashed commit
cannot be returned commit-by-commit, and directs the operator to
cherry-pick/promote the missing change by hand **or** mark the commit
`Oiax-Backflow: skip`. Below the threshold the rendered text is
byte-identical to before (guarded by the legacy-text test). The read is
best-effort — a git failure or unparseable body yields a count of 0 (no
squash paragraph) and never fails the exit-3 record path or changes the
exit code.

**D3 — Provenance matching scans every paragraph tail.** The identity
ladder's cherry-pick-provenance rung
(`backflowAlreadyReturned`/`provenanceSHAs`) now recognizes a standalone
`(cherry picked from commit <sha>)` line at the end of **any**
blank-line-delimited paragraph, not only the body's last. A backflow return
that is squash-merged into the source is therefore fully recognized as
already-returned, and none of its commits is re-proposed. The anti-prose
guard is preserved: a provenance phrase that is not a paragraph's final
line is still not read as provenance.

**D4 — Documentation names the durable fix.** The backflow guide states
plainly that squash merges on a promotion-path branch defeat backflow and
that rebase or merge-commit merges should be used there; the squash-aware
conflict text repeats the same steer. Oiax does not *enforce* a
merge-method on promotion-path branches (unlike the merge **return** fence
of [ADR 0006](0006-merge-commit-backflow-strategy.md), which is load-bearing
for that strategy's ancestry settle) — a squash there is a misconfiguration
Oiax surfaces, not a state it forbids.

This ADR changes no package boundaries: detection lives in
`internal/reconcile` (git read + the shared `provenanceSHAs` helper), the
guidance in `internal/tmpl`, and the engine stays pure.

## Consequences

- **Positive:** the `oiax-sample`-class failure — a squash on the
  promotion path wedging `reconcile` on an opaque per-hour cherry-pick
  conflict — now produces a conflict artifact that explains exactly what
  happened and what to do, with the skip hatch named. The situation is
  still a divergence a human must clear (correctly — content was lost into
  a squash), but it is no longer opaque.
- **Positive:** a squash-merged backflow *return* no longer re-proposes
  its already-returned commits; the edge settles as it should.
- **Accepted blind spot:** the rollup signal is the count of cherry-pick
  `-x` provenance lines, so a squash of commits that never carried
  provenance is not flagged as a squash. It still conflicts and still gets
  the generic conflict artifact naming the skip hatch; it just does not get
  the squash-specific sentence. Detecting a provenance-free squash reliably
  from Git alone is not possible, and a false "this is a squash" is worse
  than a missing one.
- **Neutral:** no change to exit codes, the plan JSON contract, the
  artifact lifecycle, or which commits a replay attempts. D2 is additive
  human text; D3 widens an existing match while keeping its guard.

## Links

- Extends [ADR 0004 — Backflow execution](0004-backflow-execution.md)
  (the cherry-pick identity ladder and conflict = exit 3)
- Extends [ADR 0008 — Durable backflow-conflict artifact](0008-durable-backflow-conflict-artifact.md)
  (the artifact the squash guidance is rendered into)
- Identity posture: [ADR 0002 — Detect divergence by content, not
  ancestry](0002-content-based-divergence-detection.md)
