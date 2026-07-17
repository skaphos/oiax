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
| `strategy` | The return mechanism: `cherry-pick` (the default if you omit it) or `merge`. See [Choosing a strategy](#choosing-a-strategy). |
| `expectedMergeMethod` | Only meaningful with `strategy: merge`, where it must be `merge` (the default). It records that the return request is intended to land as a merge commit; a `squash` or `rebase` value is rejected because it would erase the ancestry link the merge strategy depends on. |

> A backflow source must not also declare `drift: expected`. That would
> say "return this content" and "this content is fine to keep" at the same
> time — validation rejects it. Use `drift: expected` for branches whose
> local commits should *stay* local; use `sources` for branches whose
> local commits should come *back*.

## Choosing a strategy

Backflow returns downstream-only commits by one of two mechanisms, chosen
per edge with `strategy`. They differ in what they preserve and what they
demand of the target branch:

| | `cherry-pick` (default) | `merge` |
| --- | --- | --- |
| **Optimizes for** | Precision — a clean, replayed patch series on the source. | Traceability — the returned commits keep their original identity and ancestry. |
| **Granularity** | Selective: per-commit. `Oiax-Backflow: skip` withholds individual commits; already-returned commits are filtered out by content. | All-or-nothing: the entire downstream-only range returns together in one `--no-ff` merge. Per-commit exclusion is not possible. |
| **Identity check** | Content-based: patch-id, cherry-pick `-x` provenance, and the skip trailer (survives squash and rebase rewrites). | Ancestry-based: once the merge lands, the source head is an ancestor of the target, so the edge settles by reachability and never re-proposes. |
| **Target merge policy** | Any: the return PR can be squashed, rebased, or merged. | Requires merge commits: the target branch must allow merge commits and must not require linear history. |

Reach for `merge` when downstream and upstream history must stay
connected — audits, `git log --graph` readability, or tools that follow
merge ancestry. Stay on `cherry-pick` (the default) when you want a tidy
linear source history, need to withhold individual commits, or the target
branch enforces squash-only or linear history.

### All-or-nothing (the merge strategy)

A merge returns the **whole** downstream-only range in a single `--no-ff`
merge of the source head onto the target head. It cannot honor per-commit
exclusion the way cherry-pick does, so every downstream-only commit — even
unmarked, incidental ones — comes back together. The plan makes this scope
visible: a merge edge's `edges` summary lists the exact `returned` set, and
the human summary prints `strategy: merge — returning N wholesale: …`.

Because the return is a real merge, the committer and author identity and
dates are pinned to the source head's committer, so the merge commit is
byte-identical on every run (ADR-0004 determinism), and the deterministic
`oiax/backflow/<source>-to-<target>/<shortSHA>` branch name is keyed to the
source head exactly as for cherry-pick.

### Two fences that stop a merge backflow

A merge backflow is refused — surfaced as a **reported divergence**
(`reconcile` exits 3), pushing nothing and opening no request — in two
cases a cherry-pick would tolerate:

1. **The target forbids merge commits (live check).** Every plan reads the
   target branch's *live* merge policy from the forge — repo-level
   `allow_merge_commit` **and** the branch's own ruleset or classic
   protection (a required-linear-history rule a repo-level read cannot
   see). If merge commits are disallowed, the wholesale merge cannot land,
   so Oiax reports divergence rather than opening a request that could
   never merge. This is a live read on every run, not static config: a
   protection rule added after you configured backflow is caught the next
   plan. (A genuine failure to *read* the policy is a loud operational
   error — exit 1 — never silently treated as "not allowed".)
2. **An `Oiax-Backflow: skip` trailer inside the returned range.** Under
   cherry-pick the skip trailer silently withholds that one commit. A merge
   cannot withhold one commit from a wholesale range, so a skip trailer in
   range is a **hard error**, not a silent exclusion — Oiax reports
   divergence and names the offending commits. Either unmark those commits,
   or set `strategy: cherry-pick` on the edge if you need per-commit
   control.

See [ADR 0006](../adr/0006-merge-commit-backflow-strategy.md) for the full
rationale behind the merge strategy and both fences.

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
