# 0020 — Require author provenance to recognize a managed request

- Status: proposed (awaiting maintainer acceptance)
- Date: 2026-11-22
- Decider: Shawn Stratton, repository maintainer
- Scope: managed-request recognition on both forges; no change to the marker
  format, the JSON plan, exit codes, or `pkg/api`

## Context

Oiax recognizes its own requests by a body marker plus the branch relationship
the marker declares (head = source, base = destination), and refuses a request
whose head lives in a fork. The fork rule was meant to be the authorization
boundary: only someone with push access can put a branch in the base
repository.

That reasoning holds for backflow requests, whose head is a new `oiax/` branch.
It does not hold for promotion requests. A promotion request's head is an
existing long-lived graph branch, so opening one needs no branch creation and
no push access — only the ability to open a pull request between two branches
that already exist. Whoever opens the request writes the marker and picks the
branches, so such a request is indistinguishable from Oiax's own.

A request recognized this way is treated as managed everywhere:

- it is adopted as the edge's request, so Oiax never opens its own;
- Oiax rewrites its body and closes it, which breaks the rule that unmanaged
  pull requests are never touched;
- once merged, its author-editable `sourceHead` feeds the promotion-baseline
  rung, which can mark unpromoted commits as promoted and cause Oiax to close
  its genuine open request;
- it is admitted to notification lifecycle discovery.

Every other signal available at recognition time — marker, branch names,
labels, title — is chosen by the request's author or by anyone with triage
rights. The request's author is the one field the author cannot choose.

## Alternatives

1. **Require the request's author to be a trusted Oiax identity.** The token's
   own identity is looked up from the forge; the operator may declare more.
   Closes the gap for every recognition path with one rule. Costs one identity
   lookup per run and makes a token-identity change an explicit migration.
2. **Require the author to have push access.** Restores the boundary the fork
   rule assumed. Needs a permission lookup per distinct author, cannot be
   answered for App and Actions identities through the collaborator API, and
   still lets any writer forge a request, where option 1 does not.
3. **Sign the marker** with a secret held by CI. Strongest, and independent of
   forge identity, but adds key management for every operator and a marker
   format change, which is a compatibility contract.
4. **Do nothing; document that graph repositories must restrict who can open
   pull requests.** Not available on public repositories, and silently wrong
   everywhere else.

## Decision

Choose option 1.

- A pull request is managed only if it passes the existing shape checks **and**
  was opened by a trusted identity. This applies uniformly to open discovery,
  merged (baseline) discovery, update, close, duplicate-create adoption, and
  notification lifecycle reads.
- The trusted set is the identity the token acts as, plus the entries of
  `OIAX_TRUSTED_AUTHORS` (comma-separated). On GitHub entries are logins,
  compared case-insensitively; the token's login comes from the GraphQL
  `viewer`, because REST offers no self-lookup for installation tokens or
  `GITHUB_TOKEN`. On Azure DevOps entries are identity ids (GUIDs), never
  display or unique names; the token's id comes from `connectionData`.
- The lookup happens only when a request already has the shape of a managed
  one, and its result is cached for the run. Failures are not cached.
- Fail closed. If the token identity cannot be determined and no author is
  declared, the run errors rather than guess. If authors are declared, they
  suffice on their own and the failed lookup is a warning.
- A request that carries a marker but fails the check is treated exactly like
  any other unmanaged pull request and reported once in a warning naming the
  request and its author, so a hand-written marker and a forgotten migration
  are both visible.
- If a request Oiax has just created is recorded with an author outside the
  trusted set, Oiax warns immediately with the value to declare.
- `OIAX_TRUSTED_AUTHORS` is an environment variable, exposed as the Action's
  `trusted-authors` input and the Azure template's `trustedAuthors` parameter.
  It is deliberately not a `.oiax.yaml` field: it describes the credential a
  deployment runs with, not the promotion graph.

## Compatibility and rollout

No stored format changes. Behavior changes for one population: repositories
whose open or recently merged managed requests were opened by a different
identity than the one Oiax runs as now — typically after moving from
`GITHUB_TOKEN` to an App token.

- Open requests from the previous identity stop being recognized. Oiax then
  tries to open its own, the forge rejects the duplicate head/base pair, and
  the run fails. The warning names the previous author; listing it in
  `OIAX_TRUSTED_AUTHORS` restores recognition.
- Merged requests from the previous identity no longer supply a promotion
  baseline. The other equivalence rungs still apply; the cost is confined to
  squash-merged edges whose source has since advanced, where a promotion
  request may be proposed for already-promoted content until the next
  promotion merges under the current identity.

Ship as a fix with an operator note. The setting can be removed once the
previous identity's requests have merged or closed.

## Consequences

- An unmanaged pull request between two graph branches — including one with a
  copied marker — is never edited or closed by Oiax. It still occupies the
  edge's head/base pair, so the create fails and the run exits non-zero, as it
  already does for any human-opened request on that pair. Reporting that as an
  edge divergence instead of a failed apply is a separate change.
- Someone with write access can still edit the body of a request Oiax opened,
  including its `sourceHead`. That is the trust level the tool already assumes
  for writers; narrowing it would need option 3.
- While the token identity is unresolved and declared authors are standing in,
  a marker-bearing request from an undeclared author is "unknown", not
  "unmanaged". Core discovery leaves it alone either way. Notification
  lifecycle reads report it as unavailable rather than unmanaged, because a
  scan records progress past every request it rules out and would otherwise
  permanently skip one of Oiax's own requests over a transient lookup failure.
  The cost: in a deployment where the lookup never succeeds, a request with a
  copied marker holds that scan incomplete. Closing the request does not
  release it — creation scans read requests in every state, and a closed
  request's provenance is just as unknown. The scan completes once the token
  identity resolves (the request is then plainly unmanaged) or the marker is
  removed from the request's body. Declaring the request's author also
  completes it, but only by making that request managed, so it is a remedy
  for a forgotten previous identity and never for a request someone else
  wrote. A delayed notification is recoverable; a skipped one is not.
- The GitHub provider now makes one GraphQL call per run. No GraphQL variables
  or interpolation are involved; the query is a constant.
- Azure DevOps conflict artifacts (work items) are recognized by marker and
  tags and are outside this decision.
