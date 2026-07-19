# Governance change-record templates

> How to make Oiax's promotion and backflow PRs carry your organization's
> change-management scaffold — control references, risk classification,
> ticket links, and an approver-justification section — so the PR itself
> is the auditable change record. Key-by-key details live in the
> [templates reference](../reference/templates.md).

Oiax runs unattended, and an unattended tool must not *invent* a
justification. The model that works under SOX/ITGC/CRISC-style control
frameworks splits the record in two:

- **Oiax fills in the known facts** — what is changing: the edge, the
  environments, the commit list, the source head. These are mechanical
  and always correct at creation time.
- **The template carries your scaffold** — change classification,
  control-id references, the ticket link, and an explicit
  **approver-justification section as a placeholder** that the human
  reviewer completes before approving.

Approval, validation, and policy gates stay where the repository already
puts them (branch protection, required reviews, CODEOWNERS): Oiax's
posture is unchanged. The PR body just becomes the audit narrative your
change process expects.

## A minimal change-record setup

Put the scaffold in a template file so it is reviewable like any other
governed file, and reference it from `.oiax.yaml`:

```yaml
apiVersion: oiax.skaphos.dev/v1
kind: PromotionGraph
metadata:
  name: environments
spec:
  branches:
    development: { role: source }
    staging: {}
    production: { role: terminal }
  promotions:
    - from: development
      to: staging
    - from: staging
      to: production
      # Production promotions carry the heavier record.
      templates:
        title: "CHG: promote {{.From}} to {{.To}} ({{.Count}} commits)"
        bodyFile: .oiax/templates/production-change-record.md.tmpl
  templates:
    # Every other promotion gets the lighter default scaffold.
    promotion:
      bodyFile: .oiax/templates/promotion-change-record.md.tmpl
  backflow:
    sources: [production]
    target: development
```

`.oiax/templates/production-change-record.md.tmpl`:

```markdown
Oiax opened this request to promote {{.Count}} commit(s) from
`{{.From}}` into `{{.To}}` (source head `{{.SourceHeadShort}}`).

## Change record

- **Change type:** normal <!-- standard | normal | emergency -->
- **Risk classification:** <!-- low | medium | high -->
- **Control references:** ITGC-CM-02, SOX-A12
- **Change ticket:** <!-- link the ServiceNow / Jira / ADO item -->

## What is changing

{{range .Commits}}- `{{.ShortSHA}}` {{.Subject}}
{{end}}{{if gt .Count (len .Commits)}}…and more — see the request diff for the full list.
{{end}}

## Approver justification

<!-- Reviewer: replace this comment with the business justification and
     rollback plan before approving. Approval of this PR is the recorded
     approval of the change. -->

---
This request is managed by Oiax. Do not edit the metadata block below.
```

Everything above the marker block is yours; Oiax appends its
machine-readable marker after the rendered body and will refuse a
template that tries to imitate or swallow it. Note the balanced HTML
comments used as reviewer prompts — those are fine; only marker-shaped
comments are rejected.

The template files are read from the **pinned config ref** — the same
trust rule as `.oiax.yaml` itself ([ADR 0003](../adr/0003-pinned-configuration-ref.md)).
A pull request that edits the scaffold changes nothing until it merges
to the default branch, and the edit history of the scaffold is itself
part of your audit trail.

Ready-to-copy variants of these files live in
[`docs/examples/templates/`](../examples/templates/).

## What renders when

Bodies render **once, when the request is created**, and are never
re-rendered — Oiax's baseline updates rewrite only the metadata marker,
so the reviewer's filled-in justification is never overwritten. That
also means the commit list describes the request as created; when the
source advances, the PR's diff is authoritative (the body says so in the
truncation line above).

If a template cannot render at apply time, Oiax fails the run rather
than open a request without its scaffold — for a governance adopter the
scaffold *is* the change record. In practice broken templates never get
that far: every configured template is compiled and sample-rendered at
configuration load, so `oiax validate` (wire it into PR CI per the
[recipes](recipes.md)) rejects them in the same round trip as any other
config error.

## Backflow and conflict records

The same pattern applies to the hotfix-return flow:

```yaml
  templates:
    backflow:
      title: "CHG-BF: return {{.Count}} hotfix commit(s) from {{.From}}"
      bodyFile: .oiax/templates/backflow-change-record.md.tmpl
    backflowConflict:
      bodyFile: .oiax/templates/backflow-conflict-record.md.tmpl
```

The conflict artifact's context additionally carries
`{{.Conflict.SHA}}`, `{{.Conflict.Subject}}`, `{{.Conflict.Applied}}`,
and `{{.Conflict.Whole}}` — see the
[variable table](../reference/templates.md#variable-context). Keep the
built-in playbook link (`docs/guides/backflow.md#when-a-replay-conflicts`)
in a custom conflict body; operators need it.

With `strategy: merge`, the `--no-ff` merge-commit message is also
templatable — useful when your git history conventions (or tooling that
parses merge messages) expect a structured form:

```yaml
  backflow:
    sources: [production]
    target: development
    strategy: merge
  templates:
    backflowMergeMessage:
      text: |
        backflow: return {{.From}} to {{.To}} ({{.Count}} commit(s))

        Source-Head: {{.SourceHead}}
        Graph: {{.Graph}}
```

## Untrusted input, one more time

Commit subjects in `{{.Commits}}` are written by whoever lands commits
on your branches. Oiax caps them and keeps them out of anything
identity-bearing, but your template decides where they appear — keep
them in list items or code spans, and do not route them into titles if
downstream tooling keys on title text. The
[reference](../reference/templates.md#untrusted-variables) has the full
guidance.

## Next steps

- [Templates reference](../reference/templates.md) — every key, variable,
  and constraint.
- [Operating Oiax day to day](operating.md) — reviewing and merging
  managed PRs, branch protection, approvals going stale.
- [ADR 0011](../adr/0011-templatable-request-text.md) — why the design
  looks like this.
