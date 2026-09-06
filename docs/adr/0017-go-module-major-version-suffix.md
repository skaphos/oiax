# 0017 — Carry the major-version suffix in the Go module path

- Status: accepted
- Date: 2026-09-05
- Decider: Shawn Stratton, repository maintainer
- Scope: Go module identity and the `pkg/api` import path; no runtime behavior,
  no configuration API, plan format, exit code or managed-request metadata change

## Context

v2.0.0 was tagged and released while `go.mod` still declared
`module github.com/skaphos/oiax`. Go's import compatibility rule requires a
module released at v2 or later to carry the major version in its path, so the
module proxy ignores every v2+ tag this repository publishes:

```
$ go list -m -versions github.com/skaphos/oiax
github.com/skaphos/oiax v1.0.0 v1.0.1 v1.0.2 v1.0.3 v1.1.0 v1.2.0 v1.3.0

$ go list -m github.com/skaphos/oiax@latest
github.com/skaphos/oiax v1.3.0

$ go list -m github.com/skaphos/oiax@v2.0.0
go: invalid version: module contains a go.mod file, so module path must match
major version ("github.com/skaphos/oiax/v2")
```

The install command documented in the README, the getting-started guide, the
agent-install guide and the recipes — `go install
github.com/skaphos/oiax/cmd/oiax@latest` — therefore installs **1.3.0**. A
reader following the v2 documentation receives a v1 binary and no error. That
is the most expensive shape a defect can take here: nothing reports it, and the
resulting binary is plausible enough to use. Asking for the version explicitly
fails outright instead of silently downgrading.

This was found while upgrading `skaphos/oiax-sample` to v2: the sample could not
obtain a 2.x CLI by the route its own README documented.

Oiax is not only a binary. [ADR 0010](0010-exported-config-validation.md)
exports `Validate` and `Default` on `pkg/api/v1` precisely so that `oiax
validate` and an external integrator run byte-identical checks by construction.
That commitment presumes the package is importable. A module path the proxy
cannot resolve is therefore a defect in a stated contract, not a packaging
detail.

## Alternatives

1. Leave the path unsuffixed and stop publishing oiax as an importable module —
   distribute the CLI through release archives and the two wrappers, and remove
   `go install` from the documentation. This is the smallest change and matches
   how most users actually consume oiax, but it retracts ADR 0010's
   exported-validation contract and strands `pkg/api` on the 1.x line.
2. Leave the path unsuffixed and keep tagging a 1.x line for Go consumers while
   publishing 2.x binaries. This keeps both audiences served, at the cost of two
   version lines over one codebase, and `go install …@latest` still resolves to
   a stale binary with no diagnostic.
3. Retract v2.0.0 and re-tag under a corrected path. Published tags are
   immutable to the proxy and module cache; retraction marks a version
   undesirable but does not make v2.0.0 installable, so this spends a release
   without fixing the reported problem.
4. Add the required `/v2` suffix to the module path and to every internal import
   path.

## Decision

Choose option 4. The suffix is not a preference — it is what the toolchain
requires of any module published at v2 or later — and option 1 is the only
alternative that genuinely competes, at the price of a contract this repository
has already made.

- `go.mod` declares `module github.com/skaphos/oiax/v2`.
- Every `internal/…` and `pkg/…` import carries the suffix.
- The GoReleaser `ldflags` version path is updated. Left stale, `-X` would
  address a package that no longer exists and `oiax version` would report an
  empty version while still building successfully.
- The depguard rules in `.golangci.yml` are updated. Left stale, they would
  match nothing, and the engine- and notification-purity boundaries this
  repository treats as safety rules would stop being enforced without failing.
- Documented `go install` invocations name `github.com/skaphos/oiax/v2/cmd/oiax`.
- Repository URLs, release-download URLs, and the nested
  `github.com/skaphos/oiax/tools` module keep their existing paths. Only package
  import paths take the suffix; `tools` is neither published nor versioned.

## Compatibility and rollout

Changing an import path is breaking for importers of `pkg/api/v1`, and is the
unavoidable cost of any correctly published Go v2 module: a v2 module is a
distinct import path by design, so 1.x consumers keep working against the
unsuffixed path and are not disturbed by this change.

v2.0.0 cannot be repaired retroactively. It remains permanently unresolvable
through the module proxy, so the checksum-verified release archive stays the
only route to exactly that version. The first release cut after this change is
the first one `go install github.com/skaphos/oiax/v2/cmd/oiax@latest` can
obtain, and the documentation must keep pointing at the archive until that
release exists.

No merge, tag push or release is authorized by this ADR.

## Consequences

The documented install route stops silently producing a v1 binary, and the
exported-validation contract of ADR 0010 becomes reachable at 2.x. Version
stamping and the depguard purity boundaries keep working, both of which would
have degraded silently had the path been changed without them.

Against that: every internal import in the repository changes, which is a large
mechanical diff with no behavioral content and a rebase cost for any open branch.
External importers must edit their import paths. And the window during which
v2.0.0 is the newest release but is not installable as a module does not close
until the next release ships — until then the documentation carries an
explanation of a defect rather than only an instruction.
