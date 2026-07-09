# 0002 — Detect divergence by content, not ancestry

- Status: accepted
- Date: 2026-07-08

## Context

The naive divergence model — `git rev-list to..from` non-empty means
promotion required — is correct only when promotion PRs merge as merge
commits, which preserve reachability. Real repositories frequently
require squash or rebase merges for linear history, and both rewrite
SHAs on merge. Under ancestry-only detection, either case leaves the
edge looking permanently diverged, and the resulting "create PR" action
fails at the forge (GitHub rejects pull requests with no commits between
the branches, HTTP 422). This is the classic failure mode of
branch-promotion automation.

## Options considered

- **Ancestry only.** Simple and cheap, but broken for squash/rebase
  repositories — the common case, not the edge case.
- **Require merge commits on promotion edges.** Pushes the problem onto
  users; many teams cannot change org-level merge policy.
- **A private state database recording what was promoted.** Reliable but
  violates the no-control-plane posture and creates a second source of
  truth to corrupt or lose.
- **An ordered equivalence ladder over content.** Reachability → stable
  patch identity (`git patch-id --stable`) → head-tree equality → the
  promotion baseline (`sourceHead`) recorded in merged managed-request
  metadata.

## Decision

Implement the equivalence ladder, in v0.1 — it is scope, not an
optimization. The first conclusive rung wins. Rung 4 makes the merged
promotion request the durable record of what was promoted: Git remains
the source of truth for content, the forge holds the promotion baseline,
and desired state is reconstructible without any Oiax-private database.

## Consequences

- All three merge methods are supported. Merge commits keep detection on
  rung 1 (exact, cheap); rebase works via rung 2; squash relies on rungs
  3–4, where the promotion baseline carries the most weight.
- Managed-request metadata (`sourceHead`) becomes load-bearing and must
  be updated whenever the source branch advances while a request is
  open.
- Recommended (not required) repository configuration: disable squash
  merging on promotion targets, buying cheaper and exact detection.
- The backflow identity check reuses the same posture in the opposite
  direction (cherry-pick trailers → patch identity).
