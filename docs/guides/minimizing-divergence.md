# Minimizing divergence

Oiax reconciles divergence; it cannot prevent it. Every conflict a
promotion or backflow hits was created upstream of Oiax — by how
environment differences are stored, how hotfixes land, and how merges
are performed. Repositories that follow the practices below see managed
requests that merge cleanly; repositories that don't see divergence
reports that need a human — a reconcile that exits 3. This guide is
about staying in the first group.

## Isolate environment-specific configuration

The single biggest source of promotion conflicts is content that is
*supposed* to differ per branch living in files that promote.

Keep anything environment-specific — replica counts, hostnames, feature
gates, resource limits — in per-environment files or overlays that only
that branch's tooling reads (Kustomize overlays, per-env Helm value
files, `config/<env>/`). What promotes through the graph should be the
shared substance: charts, bases, application code, defaults.

The test: **a promotion diff should never need editing to be correct on
the target.** If promoting `test → staging` routinely requires fixing up
values, that content belongs in an overlay, not on the promotion path.

If a branch legitimately carries local-only commits (a long-lived
environment fork), declare it instead of fighting it:
`drift: expected` tells Oiax those local commits are fine to keep — see
[modeling your graph](promotion-graphs.md). Do not use it as a pressure
valve for content that should have been an overlay.

## Keep hotfixes short-lived

A hotfix committed straight to `main` is sometimes the right call. What
turns it into lasting divergence is letting it sit there unreturned
while upstream moves on.

The discipline that keeps the window small:

1. Fix on a short-lived branch, PR **into the affected downstream
   branch** (`main`), merge. Direct pushes hide the change from review
   and CI; the PR costs minutes.
2. Let [backflow](backflow.md) return it. If the edge is configured,
   the managed return PR appears on the next reconcile — review and
   merge it **promptly**. An unreturned hotfix is a conflict growing
   quietly: every upstream commit that touches the same area raises the
   chance the eventual return stops on a cherry-pick conflict.
3. If the fix must *stay* downstream-only, say so explicitly with an
   [`Oiax-Backflow: skip` trailer](backflow.md#the-oiax-backflow-skip-escape-hatch)
   rather than leaving the return PR to rot — an open return request
   nobody intends to merge is divergence with a label on it.

The same reasoning applies in reverse: merge managed **promotion**
requests promptly too. The longer an approved promotion waits, the more
the source advances and the larger (and riskier) the eventual diff.

## Prefer merge commits on promotion targets

Where org policy allows it, disable squash merging on promotion target
branches. Merge commits keep divergence detection on the cheapest,
exact rung of the [equivalence
ladder](../architecture.md#the-equivalence-ladder); squash pushes it to
the content rungs, which are correct but best-effort
([ADR 0002](../adr/0002-content-based-divergence-detection.md)). Exact
detection means fewer surprising re-proposals — which is itself a form
of divergence noise.

## Gate on drift so divergence is loud

Divergence you notice in a day is a small merge; divergence you notice
in a month is an incident. Run the [drift-gate
recipe](recipes.md#gate-ci-on-drift-read-only-policy-check) on a
schedule: `oiax plan --detailed-exitcode` exits 2 while anything is
unpromoted or unreturned and 3 when a human is needed, so a failing
scheduled job is your divergence alarm. Policy layers above Oiax (for
example a gate that blocks deploys while exit ≠ 0) build on the same
contract.

## When divergence still happens

It will — that is what the reconcile loop is for. The playbook:

- **A managed promotion PR conflicts** → fix the content on the source
  or target branch. A promotion request's head *is* your source branch,
  so this is the normal workflow; Oiax never force-pushes your
  long-lived branches.
- **A managed backflow PR conflicts** → do not hand-edit its `oiax/`
  branch. That branch is Oiax's own artifact and it force-pushes it by
  design, so local fixes there are overwritten. Fix the underlying
  content and let the next reconcile re-propose.
- **A backflow stops on a cherry-pick conflict (exit 3)** → follow
  [the backflow conflict guide](backflow.md#when-a-replay-conflicts).
- **A promotion landed and was wrong** → revert it; see the
  [rollback recipe](recipes.md#roll-back-a-promotion-that-landed).

## Next steps

- [Backflow: returning hotfixes](backflow.md)
- [Recipes](recipes.md) — the drift gate and rollback patterns.
- [Operating Oiax day to day](operating.md)
