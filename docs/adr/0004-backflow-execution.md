# 0004 — Backflow execution

- Status: accepted
- Date: 2026-07-11

## Context

Backflow returns downstream-only commits — hotfixes landed directly on a
downstream branch such as `main` — to the single authoritative source
branch (`development`), where they re-enter normal forward promotion.
Config already declares it (`backflow: {sources: [main], target:
development}`, cherry-pick the only strategy); validation is settled.
What v0.1 left open is *execution*: how the return branch is named,
how "already returned" is decided, how conflicts surface, and how a
second hotfix arriving while a return is open is handled.

Two constraints shape every answer. Oiax keeps **no private state
database** — Git and the forge are the only sources of truth
([ADR 0002](adr/0002-content-based-divergence-detection.md),
[ADR 0003](adr/0003-pinned-configuration-ref.md)). And reconciliation is
**event-driven and concurrent**: the same edge may be evaluated by
overlapping runs, in any order, with duplicated or missed events, so
correctness cannot depend on locks or run ordering.

A backflow return also mutates Git (it replays commits), which the
promotion flow never does. That mutation must not touch the caller's
checked-out branch or index, and any branch it force-pushes must stay
inside the `oiax/` namespace.

## Options considered

- **Return-branch naming.**
  - *Random or timestamped suffix.* Every run coins a new branch, so
    concurrent runs cannot converge and a re-run cannot recognize its own
    prior work — it can only accumulate duplicates.
  - *Fixed name per (source,target).* Converges, but a second hotfix
    would force-push over an open, possibly-approved return, silently
    changing what a reviewer already saw.
  - *Deterministic name keyed to the source-branch head short SHA.* The
    name is a pure function of what is being returned, so identical runs
    converge and a new hotfix (new head) mints a distinct name.
- **Identity ("is downstream commit X already on the target?").**
  - *Ancestry only.* Broken for the same SHA-rewriting reasons as
    forward promotion (ADR 0002), and here there is no merge commit to
    lean on — every returned commit is replayed under a new SHA.
  - *A state DB of returned commits.* Reliable, but a second source of
    truth to lose or corrupt; rejected for the same reason as in ADR 0002.
  - *A content ladder plus a human escape hatch.* Cherry-pick `-x`
    provenance, then stable patch-id, then a commit trailer a human can
    add to mark a commit intentionally not-returned.
- **A second hotfix while a return is open.**
  - *Leave both requests open.* Two managed backflow requests per
    (source,target) — ambiguous, and both propose overlapping content.
  - *Edit the existing request in place.* Rewrites history a reviewer
    already approved.
  - *Supersede: close the stale request with a comment, open the new one.*

## Decision

**O4 — Deterministic return branch.** Name the return branch
`oiax/backflow/<source>-to-<target>/<short-sha>`, where `<short-sha>` is
a **fixed-length** abbreviation of the **backflow source branch head**
(the downstream head), not the replayed commits. A fixed length is
required: `git rev-parse --short` alone picks the minimum-unique length
for the local object database, so the same head could otherwise yield
different names in a shallow vs. a full clone. Determinism *is* the
concurrency strategy: the same source head always yields the same branch
name. The force-push from a racing or repeated run is idempotent in
content as well as name *for a fixed (source head, target head) pair* —
the cherry-pick pins each replayed commit's committer identity and date
to the original commit's, so replaying the same commits onto the same
target head reproduces the identical HEAD SHA instead of a new one
stamped with the current time (which would move the branch and the open
request's head, re-running CI and dismissing approvals every run). The
replay lands on the *current* target head, so when the target advances
the branch is re-pushed onto the new base — the intended behaviour for a
return request that must merge into the live target — and concurrent runs
that observe the same source and target heads still produce byte-identical
pushes, so they converge rather than clobber; a run that observed a staler
target self-heals on the next reconcile. A new hotfix advances the source
head, which yields a new branch name, which triggers supersede-and-close
of the prior request. Force-push and delete
are confined to the `oiax/` namespace; no ref outside it is ever
force-pushed.

**O6 — Manual identity override.** A commit trailer `Oiax-Backflow:
skip` on a downstream commit marks it intentionally not-returned; the
identity ladder treats such a commit as already-returned. This is the
human escape hatch for the known gap — a hotfix deliberately kept
downstream-only, or a commit whose patch identity cannot be recovered
because an earlier cherry-pick resolved conflicts and left no trailer.
It lives in commit metadata, consistent with ADRs 0002 and 0003 keeping
all durable state in Git and the forge rather than an Oiax database.

Execution mechanics:

- **Replay.** Cherry-pick the downstream-only commits with `git
  cherry-pick -x` onto a branch taken from the current target head, one
  commit at a time so the count of cleanly-applied commits is known
  before any failure. `-x` records `(cherry picked from commit <sha>)`,
  which is both durable provenance and the identity ladder's cheapest
  rung. Only commits that carry a diff are candidates: merge commits
  (which the ordinary merge-PR flow lands on the source) and empty
  commits have no patch-id and cannot be cherry-picked as content, so
  they are excluded at observation rather than allowed to fail the
  replay. A commit that reduces to an empty diff because its change is
  already present on the target (`--empty=drop`) is a convergence, not a
  conflict, and is skipped.
- **Isolation.** The replay happens in an ephemeral detached worktree
  (`git worktree add --detach <tmp> <target-head>`), never the caller's
  checkout or index, and the worktree is removed (`git worktree remove
  --force`) on every exit path — success, conflict, or error.
- **Identity ladder** (first conclusive rung wins), deciding whether a
  downstream commit is already represented on the target:
  1. **Cherry-pick trailer** — a `(cherry picked from commit <sha>)`
     line on a target commit naming this commit.
  2. **Stable patch-id** — `git patch-id --stable` of the commit already
     appears on the target.
  3. **O6 trailer** — the commit carries `Oiax-Backflow: skip`.

  A commit matched by none of these is unreturned and is a replay
  candidate.
- **Conflict handling.** On the first cherry-pick whose content
  genuinely conflicts (git exit code 1): `git cherry-pick --abort`, push
  nothing, remove the worktree, and surface a diagnostic carrying the
  failing commit's SHA, its subject, and the count of commits that
  applied cleanly before it. This is a **reported divergence (reconcile
  exit 3)**, not a created request. Oiax never attempts automatic
  conflict resolution. Only a real content conflict is treated this way:
  an operational failure (a cancelled context, a killed subprocess, any
  other non-1 exit) propagates as an ordinary error, so a transient
  problem is not misreported as a human-actionable conflict.
- **At most one active request per (source,target).** When a new hotfix
  advances the source head (new branch name), close the stale managed
  backflow request with an explanatory comment and open the new one. The
  stale request is closed only when the source head it encodes is an
  ancestor of the new one, so under overlapping runs that observe
  different heads a run never closes a request built on a head newer than
  its own. Managed requests are matched by the body marker and branch
  relationship, never by title (labels `oiax` + `oiax/backflow`). An
  HTTP 422 duplicate-create is adopted as success, consistent with
  promotion requests.

## Consequences

- Concurrency resolves without locks: overlapping runs targeting the
  same source head converge on one ref and one request; a new hotfix
  cleanly supersedes rather than colliding. The workflow `concurrency`
  group only saves wasted work.
- The `<short-sha>` collision window is the short-SHA space; on the rare
  ambiguity a run reconciles on the next event. Keying on the source
  head (not the replayed SHAs, which the cherry-pick rewrites) keeps the
  name stable across re-runs.
- O6 is a genuine escape hatch but also a foot-gun: a stray `Oiax-Backflow:
  skip` trailer permanently suppresses a legitimate return with no
  Oiax-side record beyond the commit itself. That is the accepted cost of
  keeping state in Git rather than a database.
- Identity remains best-effort at rung 2: a cherry-pick that required
  conflict resolution and carries no trailer can still be re-proposed,
  which is precisely what the O6 trailer exists to let a human silence.
- Git mutation is now part of reconcile, but confined — ephemeral
  worktree, guaranteed cleanup, force-push fenced to `oiax/` — so a
  failed or crashed run leaves the caller's checkout and every
  non-`oiax/` ref untouched, and the next reconcile converges.
