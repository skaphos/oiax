# 0008 — Durable backflow-conflict artifact

- Status: Proposed
- Date: 2026-07-17

## Context

[ADR 0004](0004-backflow-execution.md) fixed how a backflow replay
conflict is handled: on the first cherry-pick whose content genuinely
conflicts (git exit code 1) Oiax aborts, pushes nothing, creates no
request, and surfaces a **reported divergence (reconcile exit 3)**
carrying the failing commit's SHA, its subject, and the count of commits
that applied cleanly before it. [ADR 0006](0006-merge-commit-backflow-strategy.md)
extended the same exit-3 path to a `--no-ff` merge conflict, and its
live merge-method fence explicitly reserved "the SKA-601 conflict
artifact once it lands."

Today that exit-3 signal has exactly one durable surface: a single
`slog.Warn` line (`internal/reconcile/reconcile.go:761` cherry-pick,
`:740` merge). On a busy repo, where reconcile runs on every push and
the log scrolls, that line is invisible. The operator who must act —
resolve the conflict by hand, or mark the commit `Oiax-Backflow: skip` —
never sees it, and nothing on the forge records that the return is stuck.

We want a durable, forge-side record of the conflict. It must obey the
two constraints that shaped ADR 0004: **no private state database** (Git
and the forge are the only sources of truth — ADRs 0002/0003), and
**event-driven, concurrent reconciliation** that cannot rely on locks or
run ordering. Overlapping or repeated runs must converge on **one**
record, not spam the repo with a new one every run, and the record must
resolve itself when the conflict clears.

Two facts make this harder than reusing the managed-request machinery
verbatim:

- A conflict is precisely the case where **no managed request is
  created** — there is nothing to hang a comment on. The record must be a
  first-class forge object of its own.
- The managed-request identity guard (`managedMarker`,
  `internal/forge/github/github.go:1114`) rests on the branch
  relationship (`head == source`, `base == destination`) and same-repo
  provenance (`head.repo == base.repo`). A forge **issue** has no
  head/base branches and no fork-vs-same-repo signal, so that guard
  cannot be reused as-is; the authorization boundary it provides has to
  be re-established another way.

A third fact is specific to a *singleton* record and shapes the
convergence design below: a managed request supersedes by
**create-new-branch, close-old** — each source head gets its own
immutable object, so a plain ancestor guard is sufficient and no object
is ever mutated in place. A single durable issue per edge is instead
**updated in place**, so its recorded head is a mutable field with no
compare-and-swap available on the forge. The convergence rules therefore
cannot be a verbatim copy of `supersedeBackflow`; they must be re-derived
for an in-place singleton.

## Options considered

- **Where the durable record lives.**
  - *Keep the `slog.Warn` line only.* No new surface; the exact status
    quo that is invisible on a busy repo. This is the problem, not an
    option.
  - *Comment on the managed backflow request.* There is no managed
    request on a conflict — the conflict is the reason one was never
    opened. A comment has nothing to attach to.
  - *A labeled forge issue keyed to the deterministic backflow identity.*
    A first-class object that survives across runs, is visible in the
    forge UI, and can carry the operator playbook. The only option that
    is durable and does not depend on a request that does not exist.
- **How the artifact is recognized without private state.**
  - *Title match.* Fragile and human-editable — rejected for the same
    reason managed requests never key on title (`managedMarker` never
    consults it).
  - *A body marker (the existing managed-request encoding) plus the
    `oiax` + `oiax/conflict` labels.* Reuses `serializeMarker` /
    `parseMarker` / `validateMarker` verbatim. Because an issue has no
    head/base+same-repo provenance, the required labels become
    **load-bearing for identity** (a deliberate departure from the
    decorative role labels play on managed PRs), restoring the
    collaborator-only authorization boundary: only a user with triage
    write can label an in-repo issue, so an outsider cannot forge a
    recognized artifact by hand-crafting a marker in an issue body.
- **Convergence under overlapping, lock-free runs.**
  - *One issue per (source, target, head); close-and-reopen when the
    source head advances.* Mirrors the return-branch model exactly, but
    on an **issue** each supersede is a close + a new open — a burst of
    notifications on every hotfix on a busy edge, i.e. the spam the
    ticket exists to prevent.
  - *One issue per (source, target), updated in place, closed on resolve.*
    A single durable issue per logical edge. Repeated runs adopt it; a
    later source head refreshes it in place under a guard that keeps a
    staler run from regressing it; convergence or a later successful
    replay closes it with a comment. This is the chosen model — but,
    unlike the request supersede it resembles, it mutates one object's
    head field, so its guards are re-derived rather than copied (see
    Decision A3).
- **How duplicates are prevented and collapsed.**
  - *Rely on the forge as the concurrency arbiter (the 422 head/base
    dedup that powers managed-request adopt).* Not available: GitHub has
    no native dedup for issues, so two runs racing the very first
    conflict will both POST an issue.
  - *Collapse only at create time (re-list after POST, close extras).*
    Best-effort and windowed: if the loser's re-list fails, the leaked
    duplicate is never revisited, so two issues persist for the whole
    episode — the spam the ticket targets.
  - *Consolidate on every run, on the read path.* Every run, in both the
    record and the resolve paths, groups the open artifacts by
    (graph, source, target), keeps the lowest-numbered as canonical, and
    closes the rest with a "consolidating duplicate" comment. A leaked
    duplicate therefore self-heals on the very next reconcile, and the
    guarantee does not depend on any single best-effort call succeeding.

## Decision

Add a durable conflict artifact — a labeled forge **issue** — emitted in
addition to (never in place of) the existing exit-3 reported divergence.
Implementation tracked as SKA-601.

**A1 — A labeled conflict issue.** On a backflow cherry-pick or merge
conflict, Oiax ensures a forge issue labeled `oiax` + `oiax/conflict`
exists for the edge, in addition to setting `res.Divergence = true` and
writing the existing `Warn` line. The issue body carries the failing
commit's SHA and subject, the count that applied cleanly (cherry-pick;
the merge strategy notes the whole source set was attempted, since a
merge has no per-commit clean count), the `source -> target` edge and the
source-head short SHA, and the operator playbook (resolve by hand, or
mark the commit `Oiax-Backflow: skip`) linking the backflow guide.

**A2 — Identity is the body marker plus the labels, never the title.**
The artifact reuses the managed-request marker encoding
(`internal/forge/github/marker.go`) with a new `type: conflict` token
(a package-local constant, not an `engine.RequestType`, so it can never
leak into `RequestFilter` PR discovery) and a new `oiax/conflict` label.
Its logical key is the triple (`graph`, `source = a.From`,
`destination = a.To`) — the same `(source, target)` the deterministic
return branch `oiax/backflow/<source>-to-<target>/<short-sha>` is keyed
to — and the marker's `sourceHead` carries the **full** backflow source
head SHA for the ancestry guard. The marker encodes the *real* source
branch and *real* source head, not the managed-request convention where
`source` is the pushed `oiax/` branch and `sourceHead` is the replayed
head. The failing commit's subject is attacker-influenceable free text
and lives only in the human body (markdown), never in a marker identity
field; `validateMarker` still runs on the marker as defense in depth.

**A3 — One artifact per edge: create-if-absent, consolidate-duplicates,
advance-live-tip, close-on-resolve.** Every run lists the open artifacts
and, in both the record and the resolve paths, first consolidates: group
by (graph, source, target), keep the lowest-numbered issue as canonical,
close the rest with a "consolidating duplicate" comment. Then, on a fresh
conflict:

- *Absent* → create the issue.
- *Present, recorded head == live source head* → adopt: no write (the
  conflict content is deterministic at a fixed head), so no notification
  churn.
- *Present, this run's live head is a strict ancestor of the recorded
  head* → **leave alone**: this run observed a staler head; never regress
  the record.
- *Present, otherwise* (live head is a descendant of the recorded head,
  **or the two have diverged**, or the recorded head no longer resolves
  on a non-shallow clone) → **update in place** to the live head and
  refresh the body. Updating on genuine divergence is deliberate: with a
  create-new-close-old request a diverged head simply spawns a new
  object, but an in-place singleton must follow the live tip or it would
  strand an operator on a commit that no longer exists on the branch. A
  recorded head that does not resolve is only treated as authoritative-to-
  overwrite on a non-shallow clone; on a shallow clone "absent" is
  indistinguishable from "never fetched," so the record is left alone
  (the `supersedeBackflow` shallow caveat).

When the conflict clears — the replay later succeeds, the edge converges
(nothing left to return), or the returnable commits become
returned/skipped — a single end-of-Apply sweep closes the artifact with
an explanatory comment (comment-then-close, never delete, exactly as
`CloseRequest`). The sweep closes only artifacts for a **currently
configured** backflow edge that **did not re-record a conflict this run**
and whose recorded head is an ancestor of (or equal to) the live source
head; an artifact for an edge no longer in the backflow config, or one
recording a head this run cannot prove it has advanced past, is left for
manual dismissal.

Three points were weighed as correctness matters rather than product
tradeoffs, and are recorded here so a reviewer sees them at the decision,
not buried in Consequences:

- **Labels are load-bearing for issue identity (unlike managed PRs).** On
  a managed PR the label is decorative and the head/base+same-repo
  relationship is the authorization boundary. An issue has neither, so
  the `oiax/conflict` label is promoted to part of identity to restore
  that boundary. The accepted cost: a collaborator who strips the label
  makes Oiax lose track of that artifact (the next conflict opens a fresh
  one) — treated as a deliberate dismissal, consistent with keeping all
  state in Git and the forge rather than an Oiax database.

- **The still-conflicting signal is recorded before any forge I/O.** The
  end-of-Apply sweep must never close an artifact for an edge that
  genuinely still conflicts this run. Because the record path is
  best-effort (a forge outage must not downgrade exit 3), the
  edge is marked "conflicted this run" the instant the cherry-pick/merge
  conflict is detected, before the first forge call — so a flaky forge
  can fail to *touch* the artifact but can never make the sweep
  *false-close* it.

- **The artifact is strictly best-effort and never changes the exit
  code.** A create/update/close/list failure logs a warning and
  continues; it must never downgrade the exit-3 content conflict to an
  exit-1 error, and never swallow `res.Divergence`. The exit-code
  contract (0 / 1 / 3) is unchanged; the artifact is purely additive and
  self-heals on the next run.

The engine stays pure — no new `engine` import of `forge` or `git`. The
new capability is a `forge.Forge` extension (issue list/create/update/
close); the create/consolidate/advance/close state machine and every
ancestry decision live in `internal/reconcile`, using `c.Git.IsAncestor`
/ `CommitExists` / `IsShallowRepository` exactly as `supersedeBackflow`
does. The plan JSON (`planFormatVersion: 1`) is untouched: the artifact
is created only during Apply, so its identity does not exist at pre-apply
plan-render time, and the plan document stays a pre-mutation record.

## Consequences

- **Positive:** the exit-3 backflow conflict becomes a durable, visible,
  self-resolving forge object with the operator playbook attached, not a
  log line that scrolls away. Overlapping and repeated runs converge on
  one issue per edge; a cleared conflict closes itself.
- **Positive:** no new infrastructure. The marker encoding, label
  convention, `IsAncestor`/`CommitExists`/`IsShallowRepository` ancestry
  machinery, and comment-then-close discipline are all reused; the only
  genuinely new code is the issue REST surface and the state machine that
  drives it.
- **Accepted tradeoff:** the `oiax/conflict` label is part of identity,
  so removing it is a foot-gun (a duplicate artifact on the next
  conflict). This is the substitute for the head/base+same-repo
  provenance an issue lacks, and is accepted as the no-private-state cost.
- **Accepted tradeoff:** because there is no native issue dedup, two runs
  racing the very first conflict can both POST an issue. Consolidation
  runs on the read path of every run, so the duplicate is collapsed
  deterministically on the **next** reconcile (keep lowest-numbered,
  close the rest) — a brief duplicate can exist for at most one cycle,
  and the guarantee does not depend on any best-effort call succeeding.
- **Accepted tradeoff (in-place head is eventually consistent, not
  strictly monotonic):** the advance decision is monotonic — a run whose
  observed head is an ancestor of the recorded head never writes — but
  the *write* has no compare-and-swap. Two runs concurrently advancing
  from the same recorded head can interleave so the recorded head
  transiently regresses to the staler of the two; the next run at the tip
  re-advances it. The recorded head/body may therefore briefly show a
  staler-than-tip conflict. This is the price of a singleton (a
  create-new-close-old request has immutable objects and no such window)
  and it is self-healing; we do not claim strict monotonicity for the
  head field, only for the decision.
- **Neutral / bounded (cherry-pick body can lag a target advance):** the
  record path adopts without rewriting when the recorded head equals the
  live source head, to avoid an issue edit every reconcile. On the
  cherry-pick strategy the failing commit is a function of both the source
  *and* the target head, so a pure target advance while the source head is
  fixed can shift which commit conflicts, leaving the body's failing-commit
  line stale until the source head next moves (which retakes the refresh
  branch). The merge strategy is unaffected — its failing SHA is the source
  head itself. This is the no-churn tradeoff; it self-heals on the next
  source-head advance. Switching to always-refresh-on-adopt (an issue-body
  PATCH sends no notification) would close the gap at the cost of one extra
  forge call per run during an unresolved conflict.
- **Neutral / bounded (close↔recreate flap):** lock-free reconciliation
  admits a close/re-create flap — one run's sweep can close an artifact
  an earlier, still-in-flight run then re-creates from a pre-fix snapshot
  (one spurious notification), cleared on the next run. This mirrors how a
  superseded request can be transiently re-created, and is bounded and
  self-healing. The sweep is gated to only close artifacts for edges in
  the current backflow config, so a run that no longer examines an edge
  cannot close its artifact on stale information.
- **Neutral / bounded (diverged or config-orphaned artifact):** an
  artifact whose recorded head is neither an ancestor of the live head
  nor definitively absent (a force-push to a diverged line whose old head
  still exists), or whose edge left the backflow config, is left open for
  manual dismissal rather than closed — the same conservative posture as
  an orphaned managed request. Time-based auto-close of orphaned artifacts
  is deliberately out of scope, matching the supersede model.
- **Neutral:** exit codes, the plan JSON contract, and the existing
  `Warn` diagnostic are unchanged; the artifact is emitted alongside
  them. This ADR extends ADRs 0004 and 0006 — it does not supersede them.

## Links

- Extends [ADR 0004 — Backflow execution](0004-backflow-execution.md)
  (conflict = exit 3; supersede-by-ancestry)
- Extends [ADR 0006 — Merge-commit backflow strategy](0006-merge-commit-backflow-strategy.md)
  (the merge conflict path and the reserved "SKA-601 conflict artifact")
- Identity posture: [ADR 0002 — Detect divergence by content, not
  ancestry](0002-content-based-divergence-detection.md) (no private
  state; identity from Git + forge alone)
