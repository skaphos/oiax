# Code map

A package-by-package tour for new contributors. The layering rule to
keep in mind everywhere: entrypoint → engine → git layer / forge
provider, and the engine never reaches down (depguard enforces it).

## `pkg/api/v1`

The public configuration API — the only package external consumers may
import. Defines `PromotionGraph` and its spec types for
`apiVersion: oiax.skaphos.dev/v1` (the pre-1.0
`oiax.skaphos.dev/v1alpha1` is accepted as a deprecated alias), plus the
enums (roles, drift policies, merge methods, backflow strategies). Field
doc comments are
the configuration documentation. Everything else in the module is
`internal` until external consumers demonstrate a real need.

Key types: `PromotionGraph`, `PromotionGraphSpec`, `Branch`,
`Promotion`, `Backflow`.

## `internal/config`

Loading and syntactic validation: strict YAML decoding (unknown fields
rejected), apiVersion/kind checking, single-document enforcement.
Resolving the pinned configuration ref is the caller's job; this package
parses bytes. Semantic graph rules live in the engine.

Entrypoints: `config.Load`, `config.Parse`, `config.DefaultPath`.

## `internal/engine`

The provider-neutral core: graph conversion and defaulting
(`FromConfig`), semantic validation (`Graph.Validate` — acyclicity,
edge/branch references, role constraints, backflow consistency), the
observed-state types (`EdgeState`, `BranchState`, `ChangeRequest`), and
the pure planner (`BuildPlan` → `Plan` of `Action`s).

Purity rules: no provider API calls, no imports of `internal/git` or
`internal/forge`, equivalent plans for identical inputs. Edge evaluation
(the equivalence ladder) will live here, consuming observations produced
by the git layer and forge provider.

## `internal/git`

The git layer: shells out to the system `git` executable (`Runner`).
Owns ref validation (`CheckRefFormat` — branch names are data, never
shell), ref existence and resolution, and eventually merge bases,
reachability, stable patch-ids, tree comparison, and backflow branch
construction.

## `internal/forge`

The provider-neutral forge abstraction: the `Forge` interface
(list/create/update/close managed change requests, push `oiax/`
branches) and its request/response types. Providers own authentication;
the engine never sees credentials.

## `internal/forge/github`

The GitHub provider (first supported forge; currently a compile-checked
stub). Will identify managed requests by body metadata + branch
relationship, adopt HTTP 422 duplicate-create rejections as success, and
warn on degraded `GITHUB_TOKEN` configurations.

## `internal/cli`

The Cobra command tree: `validate`, `plan`, `reconcile`, `graph`,
`version`, and the hidden `gen docs` generator that produces
`docs/reference/cli.md` (drift-gated in CI). Exit codes are a
compatibility contract; see the [configuration
reference](reference/configuration.md).

## `internal/version`

Build metadata injected by GoReleaser ldflags.

## `cmd/oiax`

`main`: argv in, exit code out. Nothing else.

## Not Go

- `action.yml` — the composite GitHub Action: downloads a
  checksum-verified release binary and runs it. It also prepares Git refs
  first — fetching every branch head into `refs/remotes/origin/*` and running
  `git remote set-head origin --auto` — so a multi-branch graph is resolvable
  and the default config-ref (`origin/HEAD`) is known under
  `actions/checkout`. Still no promotion logic.
- `Taskfile.yml` — the task runner (`go -C tools tool task --list`).
- `tools/` — pinned tool dependencies (task).
