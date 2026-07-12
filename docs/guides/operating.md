# Operating Oiax day to day

Once Oiax is running, most of your interaction is reading its plans and
reviewing the pull requests it opens. This guide covers what that looks
like and how to handle the cases that need a human.

## Reading a plan

`plan` and `reconcile` both print the plan first. The text format is one
header line plus one line per action:

```
Promotion graph: environments
  create   development -> test (3): 3 unpromoted commits and no managed promotion request
  update   test -> qa (1): source branch advanced; record the new head as the promotion baseline
  close    qa -> production-stage-1 (0): edge synchronized out-of-band; the open managed request proposes nothing
```

Each action line is `<verb> <from> -> <to> (<count>): <reason>`. The
count is the number of commits the action moves (or, for backflow,
returns). The reason is a human-readable explanation — read it, but do
not script against its exact wording.

The verbs:

| Verb | Action | Meaning |
| --- | --- | --- |
| `create` | createPromotionRequest | Unpromoted source commits exist and no managed promotion request is open — Oiax opens one. |
| `update` | updateManagedRequest | An open promotion request's recorded baseline is stale because the source advanced — Oiax refreshes it. |
| `close` | closeObsoleteRequest | A managed request for an edge **still in the graph** now proposes nothing — the edge synchronized out of band — so Oiax closes it with a comment. |
| `backflow` | createBackflowRequest | Downstream-only commits need returning — Oiax opens a [backflow](backflow.md) request. |
| `report` | reportDivergence | The destination has content the source lacks and no backflow or drift policy accounts for it — **Oiax reports, and does nothing else.** See [Divergence](#when-oiax-reports-a-divergence). |

When there is nothing to do:

```
Promotion graph: environments
In sync, no actions.
```

For automation, add `-o json` and consume the frozen
[plan JSON format](../reference/plan-format.md) instead of parsing text.

## What Oiax creates

Every managed request carries a machine-readable marker in its body — not
in its title — plus the labels `oiax` and either `oiax/promotion` or
`oiax/backflow`:

```
<!--
oiax:
  version: v1
  graph: environments
  type: promotion
  source: development
  destination: test
  sourceHead: 0123456789abcdef0123456789abcdef01234567
-->
```

Oiax identifies its own requests by that marker **and** the branch
relationship, never by title or by the label (the label is decorative —
useful for you to filter on, but not what Oiax keys off). The practical
guarantees:

- Oiax **never touches an unmanaged request** — a pull request between the
  same two branches that it did not create is never edited or closed.
- Do not hand-edit the marker comment. It is how Oiax recognizes and
  updates the request.

## Reviewing and merging managed requests

A managed promotion request is an ordinary pull request. **You review and
merge it** — Oiax never approves, never merges, never force-pushes your
long-lived branches. Your branch protection, required checks, CODEOWNERS,
and reviewers apply exactly as they do to any PR.

### Approvals can go stale

A promotion request's head **is the live source branch**. When new
commits land on the source while the request is open, they join the same
open request — Oiax refreshes the recorded baseline (`sourceHead`) rather
than opening a duplicate. That means **an approved promotion request can
gain commits after approval.**

Turn on the branch-protection setting **"Dismiss stale pull request
approvals when new commits are pushed"** on your promotion targets, so a
review always reflects what is actually about to merge.

### Obsolete requests are closed, not deleted

When an edge that is **still in the graph** synchronizes out of band —
someone merged its content another way, so the managed request now
proposes nothing — Oiax closes that request **with an explanatory
comment**, never silently and never by deleting it. The comment tells you
why.

One case Oiax does **not** clean up automatically: if you *remove* an
edge from the graph, its managed request is no longer evaluated, so it is
left open (orphaned), not closed. Close it yourself — see [Removing or
pausing Oiax](#removing-or-pausing-oiax). (Backflow is the exception — a
superseded or orphaned backflow request *is* closed and its branch
deleted.)

## Branch protection and required checks

Oiax opens and refreshes the promotion pull requests; **your branch
protection decides whether they can merge and who merges them.** A
sensible setup on each promotion target (`test`, `qa`, … through your
terminal branch):

- **Require status checks to pass** — the checks that actually gate a
  promotion. They only run on a managed PR if it was created with a token
  that triggers workflows; see [Tokens](tokens.md).
- **Dismiss stale approvals** — enable "Dismiss stale pull request
  approvals when new commits are pushed," because a managed request's head
  is the live source branch and can gain commits after approval (see
  [above](#approvals-can-go-stale)).
- **Require review / CODEOWNERS** as your process dictates.

Oiax needs `contents: write` (to push `oiax/` backflow branches) and
`pull-requests: write` (to manage requests), but it **never approves,
merges, or force-pushes your long-lived branches** — those decisions stay
with your protected-branch rules and reviewers. Granting Oiax write does
not let it bypass protection.

## When Oiax reports a divergence

A `report` action — and `reconcile` exiting **3** — means the destination
has content the source does not, and nothing in your configuration
accounts for it. Oiax will not guess a safe merge, force-push, or
auto-resolve. It reports and stops on that edge:

```
reported divergence on test -> qa: qa has 2 commits not represented in test
```

This needs a human. Your options:

- **It should come back** — enable [backflow](backflow.md) for that
  source, and Oiax will return the commits instead of only reporting.
- **It is fine to keep** — mark the branch `drift: expected` in the graph
  so the downstream-only content is acknowledged silently.
- **It is a real divergence** — reconcile the branches yourself (promote
  or cherry-pick the missing content the way your process dictates).

For a backflow conflict specifically, see
[Backflow — when a replay conflicts](backflow.md#when-a-replay-conflicts).

## Exit codes and CI gating

Exit codes are a stable contract, following `terraform plan
-detailed-exitcode`:

| Code | `plan --detailed-exitcode` | `reconcile` |
| --- | --- | --- |
| 0 | fully in sync | converged (incl. "applied successfully") |
| 1 | error | error |
| 2 | applyable changes pending | *(never returned)* |
| 3 | report-only divergence present | converged with reported divergence |

`plan --detailed-exitcode` predicts what `reconcile` will do for the same
state — 2 means "safe to reconcile, it will converge to 0," 3 means
"needs a human." A CI gate can rely on that. The one case `plan` cannot
foresee is a backflow that only conflicts at cherry-pick time: it shows as
2 but `reconcile` exits 3. Full details in the
[configuration reference](../reference/configuration.md#exit-codes).

## Recovery: idempotency, not rollback

Oiax has no rollback. Its recovery mechanism is **idempotency**: the plan
is printed before anything is applied, so a run that fails partway is
still explainable from its output, and **the next reconcile converges**
from wherever things ended up. If a provider call fails, fix the cause
(permissions, token, a rate limit) and let the next scheduled or
event-driven run catch up. Re-running is always safe.

## Removing or pausing Oiax

- **Pause** — switch the Action to `mode: plan` (it observes and reports
  but changes nothing), or disable the workflow. Open managed requests are
  left as they are.
- **Remove one edge** — deleting an edge from the graph stops Oiax
  evaluating it, but **does not close the request it already opened** (see
  [Obsolete requests](#obsolete-requests-are-closed-not-deleted)). Close
  that PR yourself, or leave it for a human to merge.
- **Remove Oiax entirely** — delete the workflow (and, if you like,
  `.oiax.yaml`). Open managed requests are ordinary pull requests
  afterward: merge or close them by hand. Oiax keeps no state outside Git
  and the forge, so there is nothing else to clean up beyond any `oiax/`
  backflow branches you no longer want.

## Scale and rate limits

- **Discovery is two list calls per run**, whatever the graph's size —
  Oiax lists open and recently-merged managed requests once and matches
  them to edges in memory, so more edges do not multiply API calls.
- **Rate limits are absorbed** — the GitHub provider retries with backoff
  and honors `Retry-After` / rate-limit-reset headers, so a transient
  throttle is waited out rather than failed. A run that still cannot
  progress exits non-zero, and the next reconcile converges.
- **Merged-request lookback is 180 days.** To recover a promotion
  baseline (rung 4 of the [equivalence
  ladder](../architecture.md#the-equivalence-ladder)), Oiax scans merged
  promotion requests from the last 180 days. An edge whose last promotion
  merged longer ago than that falls back to the other rungs — detection
  stays correct, it just skips the baseline shortcut. An actively-promoted
  edge merges far more often than the window.

## Observability

- **Logs** — structured, controlled by `OIAX_LOG_FORMAT` (`text` or
  `json`). Credential values never appear in output.
- **Step summary** — under GitHub Actions, a Markdown table of the plan is
  appended to the run's summary page.
- **Annotations** — warnings and errors appear inline on the run as
  `::warning::` / `::error::` (on stderr, so `-o json` on stdout stays
  clean).

Warnings worth recognizing — a shallow clone, a degraded token, a
merge-method mismatch — are catalogued in
[Troubleshooting](troubleshooting.md).

## Next steps

- [Troubleshooting](troubleshooting.md) — symptom → cause → fix.
- [Configuration reference](../reference/configuration.md) — every flag,
  env var, and exit code.
- [Architecture](../architecture.md) — how reconciliation works.
