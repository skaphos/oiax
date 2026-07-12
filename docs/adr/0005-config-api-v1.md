# 0005 — Config API v1

- Status: accepted
- Date: 2026-07-11

## Context

The configuration contract — `apiVersion`, `kind`, and the
`PromotionGraph` spec in `pkg/api` — shipped in v0.1 under the literal
`apiVersion: oiax.skaphos.dev/v1alpha1`. The `v1alpha1` label was the
right signal while the shape was still moving: it warned adopters that
fields could change without notice. That shape is now settled — roles,
drift policies, merge-method expectations, and the backflow policy are
all validated and documented, and `pkg/api` is the one package external
consumers may import.

The 1.0 readiness review (B3) flagged the label as a release blocker.
Freezing an `alpha`-labeled string *as* the stable 1.0 contract is
self-contradictory: the whole point of a 1.0 is a compatibility promise,
and the point of an `alpha` suffix is the absence of one. Every user's
`.oiax.yaml` embeds the string verbatim, so whatever we call the stable
version is a value people copy into their repositories and pin CI
against. The decision is which string 1.0 blesses, and what happens to
the configurations already written against `v1alpha1`.

Two things are entangled but separable. The **configuration contract**
(the YAML `apiVersion` string a repository declares) is data we can keep
accepting indefinitely. The **Go import path** (`pkg/api/v1alpha1`) is
source other Go programs import; renaming it is a breaking change for
those importers regardless of what the YAML says.

## Options considered

- **Ship 1.0 still labeled `v1alpha1`.** Honest about the package's
  history but dishonest about its stability, and it trains users to
  expect breakage from a version that has promised not to break. The
  label would then have to survive to 2.0 to avoid a rename, cementing
  the contradiction the review raised.
- **Hard break: rename the string to `oiax.skaphos.dev/v1` and reject
  `v1alpha1`.** Clean, but it strands every existing `.oiax.yaml` on the
  first run after upgrade, for a change that alters no behavior. The
  contract is byte-for-byte identical; failing closed on the label alone
  is churn with no safety payoff.
- **Canonical `v1`, `v1alpha1` accepted as a deprecated alias.** The
  stable string is `oiax.skaphos.dev/v1`; the old string still parses to
  the identical document and is warned about once per load. A migration
  bridge rather than a break or a permanent alpha.

## Decision

**Ship `oiax.skaphos.dev/v1` as the canonical, stable configuration
apiVersion, and accept `oiax.skaphos.dev/v1alpha1` as a deprecated
alias.** Both strings decode to the same `PromotionGraph`; the contract
does not fork. `config.Parse` accepts either and rejects anything else,
naming the canonical string in the error (`unsupported apiVersion %q
(want %q)`). Parse stays a pure byte→struct decoder with no I/O: it does
not warn. The alias is surfaced by a predicate,
`config.IsDeprecatedAPIVersion`, and the CLI prints exactly one line to
stderr when a loaded graph uses it — on stderr so `-o json` output on
stdout stays machine-clean:

```
warning: apiVersion "oiax.skaphos.dev/v1alpha1" is deprecated; migrate to "oiax.skaphos.dev/v1"
```

**The Go import path moves `pkg/api/v1alpha1` → `pkg/api/v1`,** and the
package is renamed to `v1`. This is a breaking change for any Go program
importing the types, which is acceptable at a 1.0 boundary — the import
path should match the stable contract, and 1.0 is the one release where
that realignment is expected. The alias is a compatibility measure for
the *configuration* string, not the *import* path: YAML written against
`v1alpha1` keeps working, Go code importing `v1alpha1` does not.

## Consequences

- Existing `.oiax.yaml` files keep working across the 1.0 upgrade with a
  one-line nudge, not a failure. Migration is a single-string edit with
  no behavioral change, and the warning tells the operator exactly what
  to change it to.
- The stable string finally matches the stability promise: `v1` means
  the contract will not break under 1.x, and the `alpha` self-
  contradiction the readiness review raised is gone.
- Go importers of `pkg/api/v1alpha1` must update their import path to
  `pkg/api/v1` when they adopt 1.0. There is no alias for the import
  path; the break is one-time and confined to the release boundary.
- Carrying the alias is a small, permanent parsing branch and a warning
  path. If a future major version drops `v1alpha1`, it does so on its own
  ADR with its own deprecation window; nothing here commits to removing
  it, only to warning about it.
