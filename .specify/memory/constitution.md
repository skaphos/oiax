<!--
SYNC IMPACT REPORT — Oiax Constitution
======================================
Version change: 1.0.0 → 2.0.0
Bump rationale: explicit expansion of Principle XI's permitted mutation boundary
to refs/notes/oiax/, authorized by Shawn Stratton on 2026-09-04 and recorded in
ADR 0015. This redefines a binding permission, so the repository's MAJOR rule
applies. This is not a software release or configuration API version change.

Derived from: skaphos-resources/standards/constitution.md v1.1.0 (ratified
2026-07-23). Upstream-derived Principles I–IX are unchanged. The amendment is
limited to Oiax-specific Principle XI, retaining bounded effects and Git durability.

Modified principles:
  - XI. Bounded Blast Radius (Oiax-specific) — same title; permit
    refs/heads/oiax/ and refs/notes/oiax/, with expected-tip, append-only notes
    updates, no notes deletion/rewind, and unchanged long-lived-branch protection.
Added sections: none.
Removed sections: none.

SCOPED DERIVATION — Principle IV remains scoped to Oiax's Git/forge substrates;
Oiax has no Kubernetes dependency. This amendment does not change that derivation.

Dependent artifacts:
  ✅ .specify/templates/plan-template.md — version and XI gate updated.
  ✅ .specify/templates/spec-template.md — reviewed; no namespace rule to change.
  ✅ .specify/templates/tasks-template.md — reviewed; test/phase rules unchanged.
  ✅ AGENTS.md, CONTRIBUTING.md, docs/architecture.md — owned ref families and
     append-only notes restriction synchronized.
  ✅ README.md — no conflicting namespace rule.
  ✅ Codex command files — none installed in this checkout's .agents/.codex;
     the separate integration PR is not changed by this amendment.
  ✅ docs/adr/0015-oiax-owned-notes-namespace.md — maintainer decision recorded;
     ADR 0004 only gains a partial-supersession status/link, decision body retained.
  ✅ specs/001-promotion-notifications/ — C1/T001 authority evidence recorded;
     proposed ADR 0014 updated without accepting its broader design.

Follow-up TODOs — known compliance gaps in this repository, recorded here rather
than weakening the principles they violate. Each needs its own change:
  - TODO(TEST_FLAGS): Taskfile.yml `test` and `test-cover` do not pass
    `-race -shuffle=on`. Required by Engineering Constraints → Testing.
  - TODO(COVERAGE_GATE): no hard coverage floor is enforced; `test-cover` emits a
    profile that nothing gates on. Required by Engineering Constraints → Testing.
  - TODO(AGGREGATE_TARGET): no single `verify`/`ci` Taskfile target running
    exactly what CI runs. Required by Engineering Constraints → Toolchain.
  - TODO(GOFUMPT): formatting uses goimports; the standard requires a `gofumpt`
    fail-on-diff check.
-->

# Oiax Constitution

Oiax is a declarative Git branch promotion reconciler for branch-per-environment
GitOps repositories. This constitution is the gate every Oiax specification, plan,
and review is checked against.

It is **derived from** the canonical Skaphos constitution
(`skaphos-resources/standards/constitution.md`, v1.1.0). Principles I–IX restate
that document as it applies to this repository; principles X–XII are Oiax-specific
additions. Nothing here weakens the upstream — where this file appears to, this
file is the bug (see Governance).

## Core Principles

### I. Explicit State Over Implicit Behavior

The promotion graph MUST be a declared, durable primitive — `.oiax.yaml`, a
`PromotionGraph` with branches, roles, edges, and backflow policy — never
convention, branch-naming folklore, or knowledge held only by the team that set
the repository up. Managed change requests MUST be identified by machine-readable
body metadata and branch relationship, never by title, label text, or author.
Behavior that depends on an undocumented assumption is a defect, not a
convenience.

*Rationale: intent that is not explicit cannot be enforced, explained, or
recovered. A promotion request Oiax cannot positively identify as its own is a
promotion request it MUST NOT touch.*

### II. Git Is the Durable Desired-State Boundary

Oiax's own desired state MUST be derivable from a commit: configuration is read
from a pinned ref — the default branch unless explicitly overridden — and never
from the triggering ref (ADR 0003). Configuration is declarative data; Oiax MUST
NOT execute anything it defines. Oiax MUST NOT become a second source of truth
for the repositories it operates on: it creates and closes change requests that a
human or an existing policy gate resolves, and it records why in the artifact
itself.

*Rationale: reading config from the triggering ref would let any branch rewrite
the rules that govern it. Determinism and the security boundary are the same
rule.*

### III. Deterministic, Reconstructible Operation

Identical observed state MUST produce equivalent plans. Divergence detection MUST
be content-based and ordered — reachability, then patch identity, then head-tree
equality, then the recorded promotion baseline (ADR 0002) — so that detection
survives squash and rebase merges that rewrite SHAs. Generated artifacts (the CLI
reference, and anything else derived from source) MUST be regenerated, never
hand-edited, and MUST be drift-gated in CI. Backflow branch names MUST be
deterministic functions of their inputs; naming is the concurrency strategy, not a
convenience.

*Rationale: determinism is what makes `plan` trustworthy. A planner whose output
depends on when it ran cannot be used as a dry run, and a dry run nobody trusts is
not a safety mechanism.*

### IV. Control-Plane Conventions, Never Obscured

Oiax runs no controller and has no Kubernetes dependency; cluster integration is
explicitly out of scope for this repository. Two upstream requirements still bind:

- **Declarative API conventions.** The configuration surface MUST follow
  Kubernetes API conventions — `apiVersion`, `kind`, `spec`, a group-qualified
  version string, validation and defaulting exported for external consumers
  (ADR 0005, ADR 0010).
- **Never obscure the substrate.** Oiax's substrates are Git and the forge. Oiax
  MUST NOT hide their behavior; it clarifies and enforces correct operation.
  Operational traps — shallow clones degrading equivalence detection,
  `GITHUB_TOKEN`-created pull requests not starting `on: pull_request` checks —
  MUST be surfaced in output and documented as first-class, not papered over.

*Rationale: hiding the machinery discards the most useful part of the system. An
operator debugging a stalled promotion needs to see git and the forge, not an
Oiax-shaped abstraction over them.*

### V. Compose, Don't Trap

Oiax MUST do one operational job — reconciling branch promotion requests — and
MUST remain independently adoptable with concrete standalone value. It MUST NOT
take a hard dependency on any other Skaphos tool. Its output MUST be ordinary Git
and ordinary forge pull requests that any GitOps reconciler consumes without
knowing Oiax exists. Approval, validation, and deployment policy stay where the
repository already puts them: branch protection, required checks, CODEOWNERS,
human review.

*Rationale: a repository must be able to stop using Oiax by deleting a workflow
file and keep every branch, request, and gate it had.*

### VI. Explainable Reconciliation, Evidence-Grade Audit

For every edge evaluated, Oiax MUST be able to show: the observed source and
target state, which rung of the equivalence ladder decided the outcome, the action
taken or withheld, and the resulting artifact. A reported failure MUST name a
reason and a next safe action; "failed" alone is a defect. Machine-readable output
(JSON) and detailed exit codes are part of this contract, not decoration.
Explanations MUST be emitted by the code that made the decision, never
reconstructed by scraping logs. Credential values MUST NEVER appear in output,
plans, errors, or documentation.

*Rationale: a reconciler that cannot explain why it opened — or did not open — a
promotion request will be turned off the first time it surprises someone.*

### VII. Read-Only Degradation Over Blindness

`validate`, `graph`, and `plan` MUST remain fully usable when mutation paths are
unavailable — no forge credentials, no push access, a read-only token, a degraded
API. Designs MUST fail toward read-only, never toward blindness: an operator who
cannot create a promotion request MUST still be able to see the graph, the
divergence, and what Oiax would do. Mutation MUST be confined to `reconcile`.

*Rationale: the moment an operator most needs to see promotion state is the moment
something is broken. Read-only degradation is a feature; blindness during failure
is an architectural bug.*

### VIII. Topology Is Deployment State

The promotion topology — which branches exist, their roles (source, terminal),
which edges connect them, direction, and backflow sources and target — MUST be
encoded in the data model and validated as a graph. It MUST NOT be reconstructed
from branch-name patterns, label conventions, or ordering assumptions. Structural
properties the tool depends on (acyclicity, reachability, exactly one active
managed request per diverged edge) MUST be enforced by validation, not assumed.

*Rationale: Oiax's entire job is to answer "what moves where, and next". A tool
that infers its topology cannot safely answer that question.*

### IX. Technical Precision, Honest Scope

Documentation and specifications MUST describe verified behavior, not intent.
They MUST state plainly what Oiax does not do — it does not deploy, render
manifests, merge, approve, or judge whether a change is safe to promote — and MUST
name known limitations and unimplemented surfaces explicitly. Marketing language
and exaggerated claims are forbidden in all repository content. Skaphos glossary
discipline binds: this tool does **branch promotion**; unqualified *Promotion* is
a Keleustes term; the hotfix-return flow is **backflow**, and *reconciliation*
refers only to the observe/plan/apply loop.

*Rationale: operational credibility is the product. A tool that overclaims is
worse than a tool that does less.*

### X. The Engine Is Pure (Oiax-specific)

`internal/engine` MUST remain pure: no forge API calls, no imports of
`internal/git` or `internal/forge`, enforced by depguard rather than convention.
Planning MUST be a total function of observed state. Effects belong at the edges —
the git layer, the forge provider, the CLI — and MUST be injected, never reached
for from inside the engine.

*Rationale: this is what makes plans testable without a network and identical
between `plan` and `reconcile`. Once the engine can call out, the dry run and the
apply can disagree, and Principle III is unenforceable.*

### XI. Bounded Blast Radius (Oiax-specific)

Oiax MUST NOT merge, approve, deploy, create long-lived branches, or mutate
repository settings. It MUST NOT close, edit, or otherwise touch unmanaged pull
requests. Force-push authority MUST be confined to Oiax-owned branches under
`refs/heads/oiax/` and Git notes under `refs/notes/oiax/`; long-lived branches
MUST NEVER be force-pushed under any circumstances. Notes updates MUST compare
an explicit expected old object ID and preserve append-only commit ancestry:
each replacement tip MUST have the expected tip as its sole parent. Initial
creation MUST require an absent ref. Notes history MUST NOT be rewound or deleted,
and notes belonging to other tools MUST NOT be mutated. The notification ledger
is further restricted to `refs/notes/oiax/notifications/v1/<graph-key>`; namespace
authority does not grant an arbitrary-ref writer or relax per-feature contracts.
This extension is explicitly authorized by the maintainer in ADR 0015; it does
not authorize branch or tag mutations through the notes capability. Branch names are
untrusted data: they MUST be validated with `git check-ref-format`, passed after
`--` separators, and never interpolated into a shell. Any new bulk or outward
action MUST bound its blast radius and refuse to exceed it without explicit
confirmation.

*Rationale: Oiax is trusted with write access to repositories where the
production branch lives. The list of things it will never do is the reason that
trust is grantable.*

### XII. Compatibility Contracts Are Explicit (Oiax-specific)

Exit codes, the JSON plan format, managed-request metadata, the configuration
`apiVersion`, and `pkg/api` are compatibility contracts. Changing one requires an
ADR under `docs/adr/`. Contracts evolve additively: a deprecation window first,
removal only on a major version. A previously accepted configuration MUST keep
parsing unless a documented major version removes it.

*Rationale: users pin CI against exit codes and copy `apiVersion` strings
verbatim into repositories we do not control. Breaking those silently is breaking
someone's production promotion path.*

## Engineering Constraints

These bind the *how*. The referenced Skaphos standards are normative; this section
is their index and Oiax's specialization of them, not their replacement.

- **Stack**: Go, targeting the version declared in `go.mod`. Cobra for the CLI.
  Runtime dependencies stay minimal — the stdlib first, and a new runtime
  dependency category needs written justification in the introducing PR. The git
  layer shells out to the system `git` binary rather than embedding an
  implementation (ADR 0007), and the required git floor is checked once at
  startup with a message naming both the floor and the detected version.
- **Layout**: `cmd/oiax/main.go` stays thin. Logic lives in `internal/`; `pkg/api`
  is the only package external consumers may import, and it is governed by
  Principle XII. Import direction is explicit and depguard-enforced.
- **Go engineering**: per `go-engineering-standard.md` — KISS/YAGNI, errors
  wrapped with operation context and branched with `errors.Is`/`errors.As`,
  `ctx` first and never stored in a struct, typed and validated configuration,
  structured logging with no secrets, tools pinned via the `go.mod` `tool`
  directive and run as `go tool`.
- **Testing**: per `go-engineering-standard.md`, non-negotiable — every bug fix
  ships a regression test that would fail without the change; tests run with
  `-race -shuffle=on` in CI; coverage floors are hard gates landed in the same
  change; runtime-behavior changes are verified at the most realistic boundary
  reachable, which for a CLI means the built binary. Table-driven tests are the
  default shape. Parsers and anything handling untrusted input — configuration,
  git output, forge payloads, request metadata — are fuzzed, with fuzzer-found
  inputs committed as permanent regression cases.
- **Documentation**: per `documentation-standard.md`. Portable Markdown under
  `docs/`; the CLI reference is generated and drift-gated, never hand-written;
  hard-to-reverse decisions get immutable ADRs under `docs/adr/`, superseded
  rather than rewritten.
- **Repository governance**: per `repository-governance.md`. Pull-request-based
  change management, signed and DCO-signed-off commits, Conventional Commits, CI
  as a required gate, `CODEOWNERS` over release and workflow paths.
- **Attribution**: released binaries ship third-party notice artifacts, and the
  repository stays REUSE-compliant.

## Development Workflow and Quality Gates

- Changes land by pull request against `main`. Direct commits, force-pushes to
  long-lived branches, and hand-pushed tags are forbidden.
- Commits are Conventional, cryptographically signed, and DCO signed off as
  `Shawn Stratton <shawn@skaphos.io>` for this organization. Release automation
  depends on the commit format.
- CI is the gate, and the local `Taskfile.yml` MUST be able to run exactly what CI
  runs. Current required checks: DCO, REUSE lint, lint, tests across the
  supported operating systems, staticcheck, govulncheck, generated-artifact
  drift, and a GoReleaser snapshot build.
- Feature work follows the spec-driven flow — `/speckit-specify` → `/speckit-plan`
  → `/speckit-tasks` → `/speckit-implement` — checked against this constitution at
  plan time. Specifications MUST cite the relevant ADR or `FACTS.md` finding
  rather than re-deriving a settled question, and MUST NOT contradict an accepted
  ADR without proposing its supersession.
- **Adopt before build**: where `tools/ECOSYSTEM.md` records mature prior art, a
  plan that builds instead of adopting MUST document why the verdict does not
  apply.
- Releases are cut by automation (release-please, then GoReleaser).
  `CHANGELOG.md`, `.release-please-manifest.json`, and
  `release-please-config.json` are never hand-edited.
- User-visible behavior changes update user-facing documentation in the same
  change.

## Governance

This constitution is subordinate to the Skaphos constitution at
`skaphos-resources/standards/constitution.md`. It MAY add Oiax-specific principles
and constraints; it MUST NOT weaken or contradict anything upstream. When upstream
changes, this file is re-synced — propose upstream first, mirror second. If this
file drifts from upstream or from the standards it indexes, this file is the bug
and gets fixed first.

**Amendment**: amendments land by pull request against this file, with the
rationale in the PR description and the Sync Impact Report at the top of this file
updated in the same change. Version semantics: MAJOR for removing or redefining a
principle, MINOR for adding a principle or section, PATCH for clarifications that
change no requirement.

**Compliance**: specifications and plans are gated against this constitution at
plan time. A deviation is either (a) justified in writing in the plan's Complexity
Tracking table, naming the principle, the need, and the rejected simpler
alternative, or (b) a proposed amendment. Silent divergence is not an option.
Known unmet requirements are recorded as follow-up TODOs in the Sync Impact
Report above rather than by softening the requirement.

**Version**: 2.0.0 | **Ratified**: 2026-07-26 | **Last Amended**: 2026-09-04
