# 0020 — Enforce independent review of promotion requests with a required check

- Status: proposed
- Date: 2026-11-06
- Decider: Oiax repository maintainer
- Scope: approval evidence on managed promotion requests; issue #113

## Context

Organizations use Oiax under SOX change management, where the person who
authored a change must not be the person who approves its release.
[ADR 0011](0011-templatable-request-text.md) made the promotion request the
auditable change record and left approval in the forge.

That split has a gap. Oiax opens the request, so the forge sees a bot as its
author. The request's head is the live source branch, so it carries commits from
many people, and any of them can approve it. A forge's protection against
self-approval looks at the request's creator or its last pusher:

- GitHub blocks the request author, and "require approval of the most recent
  reviewable push" blocks the last pusher.
- Azure DevOps can discount the creator's vote and the most recent pusher's vote.

Neither forge can state the rule the control needs: nobody who authored a change
in this request may approve it. Oiax is the component that knows which commits a
request moves, so it can compute that set.

## Options considered

- Add `Co-authored-by` trailers: Oiax creates no commits on a promotion request's
  head, so there is nowhere to write them. Trailers would also be metadata only;
  no evidence was found that either forge blocks a co-author from approving.
- Rely on last-pusher settings and CODEOWNERS: needs no code and remains good
  practice, but covers one person, or trusts a group whose members may have
  authored the change.
- Assign the request to a contributor so the forge blocks them: a request has one
  author, and Oiax would need that person's credential.
- Publish a required status check computed by Oiax: expresses the rule on both
  forges and leaves enforcement with the forge, at the cost of new capability and
  a new position for Oiax in the merge path.

## Decision

Publish an opt-in, per-edge status check on promotion requests, working name
`oiax/independent-review`. The forge enforces it as a required check. Oiax still
never approves or merges; it reports whether the evidence meets the rule.

**Originators** are the people who authored the changes in the request's
unpromoted commits: the forge-recorded author of the request that landed each
commit, the commit's author, and any `Co-authored-by` identity. Forge records are
the anchor, because git author fields and trailers are self-declared. Self-declared
data can therefore add to the set and can never remove anyone from it. A person who
only merged a change is not an originator on that basis.

**Eligible approvers** are people with an approval at the current head who are not
originators and who either hold write access or belong to the edge's named approver
group. Bot approvals never count. The check passes when the configured number of
eligible approvals exists.

**The approver group widens who may review. It does not waive independence.** It
exists so a change can still be approved when every engineer with write access
contributed, by letting people such as release managers count without repository
write access. A group member who is an originator is not eligible. If no eligible
person exists, the request stays blocked, because that is the control working.

**The check fails closed.** A commit that cannot be attributed to a forge identity,
an unreadable group, or a forge error yields a failing result, never a pass.

**Results bind to a head.** An approval counts only for the head it was given on,
and a head change invalidates the previous result. Where a forge does not bind a
vote to a commit, the guide must require its reset-votes-on-push policy.

**Each result change leaves a durable record** on the request: head, originators,
approvals counted and the path each qualified by, and the result. A result that used
the approver group says so.

The feature covers promotion requests only. Backflow requests return changes to
lower branches and stay out of scope.

## Consequences

- Positive: the rule is enforced on both forges rather than asserted, and each
  promotion carries reviewable evidence of who was excluded and why.
- Positive: the approval boundary holds. Oiax's posture of never approving or
  merging is unchanged, and an edge that does not opt in behaves as it does today.
- Negative: this is the first time a misbehaving or unavailable Oiax can block a
  merge. Failing closed means an outage stops promotions on opted-in edges.
- Negative: small teams may deadlock until they staff an approver group, and a
  long-lived promotion with many contributors shrinks the eligible set further.
- Negative: identity mapping becomes load-bearing. Commits with unlinked emails
  will fail the check until someone resolves them.
- Negative: new forge capability on both providers, wider token scopes (reading
  reviews and group membership, writing statuses), and a new evaluation trigger.
- Neutral: `engine.Commit` carries only a SHA and subject today and must gain
  author data, and the config contract gains an additive field.

### Limits of the control

This check is one part of a control and does not make a repository SOX compliant.
It cannot stop a repository administrator from bypassing branch protection or
removing the required check. Anyone able to post the status with Oiax's credential
can forge a result, so on GitHub the required check should be pinned to its expected
source. A person can also ask someone else to approve a change they wrote under
another identity. Bypass monitoring and access reviews stay with the organization,
and the control design needs review by whoever owns SOX evidence before anyone
relies on it.

## Open questions

These must be resolved before this ADR is accepted:

- Evidence retention: is a record on the request durable enough for the audit
  period, or does it also need an append-only store such as an Oiax notes ref?
  Extending that namespace needs its own decision (ADR 0015).
- Azure DevOps trigger: a vote may not be able to start a pipeline directly, so
  evaluation may need a service hook or a schedule. Needs testing.
- Attribution exceptions: how to handle commits that can never map to a person,
  such as old history or bot commits, without creating a way to bypass the check.
- Reviewer count: whether one eligible approval is the default, or the count should
  be mandatory configuration.
- Last-pusher behavior: who GitHub treats as the pusher after a web-UI merge into
  the source branch. Needs testing before the guides recommend the setting
  alongside this check.
