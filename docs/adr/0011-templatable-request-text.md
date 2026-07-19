# 0011 — Templatable request text

- Status: accepted
- Date: 2026-07-18

## Context

The human-facing text Oiax authors — promotion and backflow request
titles and bodies, the durable backflow-conflict artifact, and (for the
merge backflow strategy) the `--no-ff` merge-commit message — was
hardcoded. Organizations adopting Oiax under change-management regimes
(SOX/ITGC, CRISC-style control frameworks) need the promotion PR itself
to be the auditable change record: change-type and risk classification,
control-id references, a link to the change ticket, and an
approver-justification section. Oiax's posture keeps *approval* in the
forge; the missing piece was letting the *request text* carry the org's
scaffold (issue #54).

Precision about "commit messages" matters. Oiax authors the request
title, the request body, and the merge-strategy backflow merge-commit
message — nothing else. The promotion merge commit belongs to the forge's
merge button; cherry-pick backflow commit messages belong to git, and the
`cherry-pick -x` provenance trailer in them is load-bearing identity
([ADR 0004](0004-backflow-execution.md)) that must never be rewritten.

Three hard constraints bound any design:

1. The machine-readable marker block is the identity substrate
   (`internal/forge/marker`). It is append-only and Oiax-owned.
2. Commit subjects and similar free text are attacker-influenceable and
   must stay out of identity surfaces (marker fields, labels).
3. Oiax must stay idempotent: template output must not cause an
   update-thrash on managed requests every run.

## Options considered

- **Go `text/template` with a curated function set** (chosen). Stdlib,
  no new dependency, consistent with the shell-out/no-control-plane
  minimalism. Rendering data, not executing config: no arbitrary code
  surface, no volatile functions (`now`, env access) in the funcmap.
- **A placeholder micro-syntax (`${from}`/`${to}`).** No conditionals or
  loops, so a commit list or per-strategy wording needs ad-hoc growth
  until it is a worse template engine.
- **External templating (org tooling rewrites PR bodies after Oiax).**
  Races Oiax's own body authorship and pushes the audit scaffold outside
  the pinned-config trust boundary.
- **Full body ownership by config with a marker the user must include.**
  Rejected outright: the marker must never be forgeable or omittable.

## Decision

**Request text is templatable from `spec.templates` (with per-edge
`spec.promotions[].templates` overrides for promotion requests), rendered
by Go `text/template` over a documented, closed variable context with a
curated funcmap.** Resolution and compilation happen at configuration
load (`internal/tmpl`), template files are read from the **same pinned
source as the configuration document** ([ADR 0003](0003-pinned-configuration-ref.md)),
and every configured template is sample-rendered at load so `oiax
validate` rejects a broken template in the same round trip as any other
config error.

The three constraints resolve as follows:

1. **Marker ownership.** Templates render only the human text; the
   provider appends the marker after it, unchanged. Rendered bodies are
   rejected — at load via sample render, and again at every real render —
   when they contain a recognizable marker block (recognition takes the
   *first* oiax-keyed HTML comment, so an earlier forged block would
   hijack identity) or an unclosed `<!--` (which would swallow the
   appended marker). Balanced non-oiax comments stay legal: governance
   scaffolds use them as fill-in prompts.
2. **Untrusted text containment.** Commit subjects are exposed only to
   the rendered human body, capped in count (100 commits) and length
   (200 runes each); titles are reduced to a single sanitized line capped
   at 256 runes, silently — an attacker-sized subject must not be able to
   fail a reconcile. Marker fields never take template output.
3. **Idempotency by construction.** Text renders **once, at creation**.
   Oiax never re-renders the human body of an existing managed request:
   updates rewrite only the marker (`UpdateRequest`), and the
   adopt-on-422 path leaves the body untouched. The conflict artifact
   re-renders only on the already-head-gated advance path. Volatile
   template output therefore cannot thrash a managed request — which is
   also why the variable context deliberately excludes wall-clock time.

A render failure on the create paths **fails the apply loudly** (exit 1):
for a governance adopter the scaffold *is* the change record, so a
request without it must not be opened. The conflict-artifact path instead
follows its established best-effort posture ([ADR 0008](0008-durable-backflow-conflict-artifact.md)):
a render failure warns and leaves the artifact for the next run, never
downgrading the exit-3 divergence.

The merge-commit message template (`spec.templates.backflowMergeMessage`,
merge strategy only) feeds `git merge --no-ff -m`. Its output is a
deterministic function of the merge inputs, preserving ADR 0004's
byte-identical replay: re-runs at the same heads push identical SHAs, and
an edited template changes the replayed SHA exactly once (bounded,
self-healing churn, same class as a target advance).

Assembling the full promotion context (the post-ladder commit list) costs
extra observation, so it is paid only when the edge's promotion text is
actually customized; the built-in defaults render from the action alone
and are byte-identical to the previous hardcoded strings.

## Consequences

- `pkg/api/v1` grows `spec.templates` (and `spec.promotions[].templates`)
  — additive to the compatibility contract; existing documents are
  unaffected and default output is byte-identical.
- The PR body becomes a legitimate audit surface: orgs ship a
  change-record scaffold whose known facts (edge, commit list, heads)
  Oiax fills, and whose justification section a human completes in
  review — approval stays in the forge.
- Configuration remains declarative data: the funcmap is curated, has no
  volatile or effectful functions, and template files come only from the
  pinned config source, so a pull request cannot smuggle template
  content into a privileged run.
- A new load-time failure mode exists (broken template ⇒ validate/plan/
  reconcile error). This is deliberate fail-closed behavior; the sample
  render keeps it out of apply time in all static cases.
- The variable context is a documented, closed contract
  ([reference](../reference/templates.md)); fields may be added within
  v1, never removed or re-typed.

## Links

- Issue [#54](https://github.com/skaphos/oiax/issues/54) (SKA-54).
- Extends [ADR 0003](0003-pinned-configuration-ref.md) (pinned template
  source), [ADR 0004](0004-backflow-execution.md) (deterministic replay),
  [ADR 0008](0008-durable-backflow-conflict-artifact.md) (best-effort
  conflict artifact).
- Variable context and constraints: [templates reference](../reference/templates.md).
