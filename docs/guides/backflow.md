# Backflow: returning hotfixes

Promotion moves changes **forward** through the graph
(`development → test → … → main`). But sometimes an urgent fix lands
**directly** on a downstream branch — a hotfix committed straight to
`main`. If nothing brings it back, the next promotion from upstream will
either clobber it or look permanently diverged.

**Backflow** is how Oiax returns those downstream-only commits to your
authoritative source branch, where they re-enter normal forward
promotion. Oiax never propagates a downstream change backward through
every edge — it returns it once, to the one configured target.

## When you need it

Enable backflow if any of these is true:

- Hotfixes are sometimes committed directly to a production or staging
  branch instead of promoted up from the source.
- You want those fixes to automatically come back to `development` (or
  whatever your source branch is) so they are not lost on the next
  promotion.

If your process *never* commits directly to downstream branches, you do
not need backflow — leave `spec.backflow` out.

## Configuration

```yaml
spec:
  backflow:
    sources:
      - production-stage-1
      - main
    target: development
    strategy: cherry-pick
```

| Key | Meaning |
| --- | --- |
| `sources` | The downstream branches to watch for downstream-only commits. |
| `target` | The authoritative branch commits are returned to. Must be a declared branch with `role: source`, and must not appear in `sources`. v1 allows exactly one target. |
| `strategy` | The return mechanism. v1 supports only `cherry-pick`; it is also the default if you omit it. |

> A backflow source must not also declare `drift: expected`. That would
> say "return this content" and "this content is fine to keep" at the same
> time — validation rejects it. Use `drift: expected` for branches whose
> local commits should *stay* local; use `sources` for branches whose
> local commits should come *back*.

## What Oiax does

Given a hotfix commit `X` on a backflow source, Oiax:

1. Creates a fresh branch from the **current target head**.
2. Replays the downstream-only commits onto it with `git cherry-pick -x`,
   one at a time. The `-x` trailer — `(cherry picked from commit <sha>)`
   — is durable provenance and the cheapest rung of the identity check.
3. Opens a managed pull request from that branch **into the target**.

You then review and merge that PR like any other. Once it merges, the fix
is on the source branch and promotes forward through the graph normally.

The replay happens in a throwaway detached worktree, never your checkout,
and that worktree is cleaned up on every exit path. Oiax force-pushes and
deletes only branches under the `oiax/` namespace — no ref of yours is
ever force-pushed.

### The backflow branch name

The return branch has a deterministic name:

```
oiax/backflow/<source>-to-<target>/<shortSHA>
```

`<shortSHA>` is a fixed-length short SHA of the **backflow source head**
(what is being returned) — not of the replayed commits. Determinism is
the concurrency strategy: the same source head always yields the same
name, so repeated or racing runs converge on one branch and one request
instead of piling up duplicates.

### One active request, superseded on a new hotfix

There is at most one active managed backflow request per
`(source, target)` pair. When a **new** hotfix advances the source head,
Oiax mints a new branch name, opens the new request, and closes the old
one with an explanatory comment (only when the old request's encoded head
is an ancestor of the new one, so overlapping runs stay monotonic). The
managed request carries the labels `oiax` and `oiax/backflow`.

## "Already returned" — the identity check

Oiax treats a downstream commit as already-returned if **any** of these
hold (they are exclusion checks — a match by any one is enough):

- **Cherry-pick trailer** — a `(cherry picked from commit <sha>)` line on
  a target commit names this commit.
- **Stable patch-id** — the commit's `git patch-id --stable` already
  appears on the target.
- **`Oiax-Backflow: skip` trailer** — a human marked the commit as
  intentionally not-returned (see below).

A commit matched by none of these is unreturned, and becomes a replay
candidate. Merge commits and empty commits are excluded (they carry no
patch to replay). A commit whose change is already on the target reduces
to an empty diff on replay and is simply skipped — that is convergence,
not a conflict.

## The `Oiax-Backflow: skip` escape hatch

Sometimes you want a downstream commit to **stay** downstream — a fix
that only applies to production, say. Or a cherry-pick that once resolved
conflicts by hand left no recoverable patch identity, and Oiax keeps
re-proposing it. Add a trailer to that commit:

```
Oiax-Backflow: skip
```

The identity ladder then treats the commit as already-returned and stops
proposing it. Like all Oiax state, this lives in the commit itself, not
in any database.

> It is also a foot-gun: a stray `Oiax-Backflow: skip` permanently
> suppresses a legitimate return, with no record beyond the commit. Use it
> deliberately.

See [ADR 0004](../adr/0004-backflow-execution.md) for the full rationale.

## When a replay conflicts

If replaying a downstream commit genuinely conflicts with the target,
Oiax **stops**: it aborts the cherry-pick, pushes nothing, and creates no
request. It never attempts automatic conflict resolution. This surfaces
as a **reported divergence** (`reconcile` exits 3) with a diagnostic that
names the failing commit and how many applied cleanly before it:

```
backflow production-stage-1 -> development halted on cherry-pick conflict at 4f2a91c "hotfix: clamp retry budget" after 2 applied cleanly; no request created
```

Resolve it the way you resolve any conflicting change: promote or
cherry-pick the fix into the target by hand, or, if the commit should not
be returned at all, mark it with `Oiax-Backflow: skip`. The next
reconcile picks up from the new state — there is no partial or broken
branch left behind.

This is the one outcome `plan` cannot foresee: a backflow whose commits
only conflict at cherry-pick time shows in `plan` as an ordinary
applyable change (exit 2), but `reconcile` hits the conflict and exits 3.

## Requirements

Backflow uses `git cherry-pick --empty=drop`, so `plan` and `reconcile`
require **git 2.45 or newer**. `ubuntu-latest` satisfies this; the floor
is checked at startup with a clear error on older git.

## Next steps

- [Modeling your promotion graph](promotion-graphs.md) — roles and drift.
- [Operating Oiax day to day](operating.md) — reviewing managed requests.
- [Troubleshooting](troubleshooting.md).
