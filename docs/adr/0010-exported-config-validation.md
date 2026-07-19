# 0010 — Exported validation and defaulting on the config API

- Status: accepted
- Date: 2026-07-18

## Context

`pkg/api/v1` is the one package external Go consumers may import
([ADR 0005](0005-config-api-v1.md)). Since 1.1.0 its types carry `json`
tags, so an integrator can (un)marshal a `PromotionGraph` — but it could
not *validate* one: the semantic rules (acyclicity, edge/branch
references, role constraints, backflow consistency, branch-name shape)
lived on `internal/engine.Graph.Validate`, unreachable from outside the
module. An integrator that builds or transforms configuration had to
either shell out to `oiax validate` or re-implement the rules and watch
them drift.

The obvious fix — an exported `Validate` on `pkg/api/v1` that wraps the
engine's — is an import cycle: `internal/engine` already imports
`pkg/api/v1` for the enum types, and `internal/` is not importable
externally anyway. Duplicating the rules in both packages was rejected
during the 1.0 readiness review as two sources of truth that would
drift (finding L13, issue #48).

Defaulting is entangled with validation: the rules must agree on whether
an unset field (drift, backflow strategy, expectedMergeMethod) is an
error or a to-be-defaulted value, so the defaulting semantics have to be
decided — and exported — at the same time.

## Options considered

- **Move the canonical rules down into `pkg/api/v1` and have the engine
  consume them.** The validation rules mention only `pkg/api/v1` types
  and pure graph structure (including the pure-Go branch-name check,
  which deliberately calls no git binary), so the package stays
  dependency-free. One exported validator; the dependency arrow keeps
  pointing inward.
- **A separate `pkg/api/v1/validate` package.** Same effect, but the
  natural call site is a method on the document type; a second public
  package is surface without leverage.
- **Duplicate the rules in `v1` and test the copies against each
  other.** The drift the readiness review rejected, with a test as a
  bandage.
- **Leave validation internal.** Integrators keep shelling out or
  re-implementing; the `json` tags shipped in 1.1.0 stay half useful.

## Decision

**The canonical semantic rule set moves to
`(*v1.PromotionGraph).Validate`, and `internal/engine` no longer has a
validator of its own.** The CLI validates the parsed document and only
then converts it with `engine.FromConfig`, so `oiax validate` and an
external integrator run byte-identical checks by construction. The rules,
their messages, and the everything-at-once error-list contract are
unchanged — they moved, they did not fork. Ref *existence* stays out of
scope: it needs repository state, which the pure API package never
fetches.

**Defaulting is exported as `(*v1.PromotionGraph).Default`,** mutating in
place (the Kubernetes convention): an empty `apiVersion`/`kind` becomes
the canonical pair, an unset drift becomes `forbidden`, an unset backflow
strategy becomes `cherry-pick`, and the merge strategy's unset
`expectedMergeMethod` becomes `merge`. `Default` never overwrites a set
field and is idempotent. `Validate` accepts a document with or without
`Default` applied — every field with a documented default may be unset —
but requires `apiVersion` and `kind`, which `Default` fills for
hand-constructed documents. `engine.FromConfig` resolves the same
defaults while building the engine model; a test pins the two resolutions
against each other.

`Validate` additionally checks `apiVersion` and `kind` (accepting the
[ADR 0005](0005-config-api-v1.md) deprecated alias), which
`config.Parse` had enforced alone; for integrators constructing documents
in Go there is no Parse step, so the document validator must carry the
check itself.

## Consequences

- External integrators validate and default a `PromotionGraph` with two
  method calls and zero `internal/` imports, and cannot drift from the
  CLI: there is exactly one rule set.
- `pkg/api/v1` grows behavior, not just shape. `Validate` and `Default`
  join the compatibility contract: rules may be *added* in a minor
  release (a previously-invalid document staying invalid, or a new rule
  catching what was always broken), but a valid document must not become
  invalid within v1.
- The engine shrinks: `internal/engine` keeps conversion
  (`FromConfig`) and planning, and its depguard-enforced purity is
  unaffected (it already imported `pkg/api/v1`).
- The branch-name check now lives in the public package. It remains the
  documented subset of `git check-ref-format --branch`, agreeing with
  the git layer's authoritative enforcement by construction.

## Links

- Supersedes nothing; extends [ADR 0005 — Config API v1](0005-config-api-v1.md).
- Issue #48 (release-readiness finding L13); grouped cleanup issue #49.
