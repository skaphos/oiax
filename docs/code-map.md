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
(`EvaluateEdge`) implements the equivalence ladder over observations
assembled outside the engine. `BuildPlan` turns evaluated edge states
into promotion, backflow, close, or report actions.

## `internal/git`

The git layer: shells out to the system `git` executable (`Runner`).
Owns ref validation (`CheckRefFormat` — branch names are data, never
shell), ref existence and resolution, merge bases, reachability, stable
patch-ids, tree comparison, git-version enforcement, shallow-clone
detection, and isolated cherry-pick replay in ephemeral worktrees.

## `internal/reconcile`

The impure coordination layer between the pure engine and external
systems. `Coordinator.Plan` observes Git and forge state and feeds the
engine; `Coordinator.Apply` executes the resulting actions. It owns
backflow replay and lifecycle, merge-method warnings, plan rendering,
CI annotations (GitHub Actions and Azure Pipelines dialects), and
step-summary output.

## `internal/forge`

The provider-neutral forge abstraction: the `Forge` interface
(list/create/update/close managed change requests, push `oiax/`
branches) and its request/response types. Providers own authentication;
the engine never sees credentials.

## `internal/forge/github`

The implemented GitHub provider. It discovers managed requests by body
metadata plus branch relationship, creates/updates/comments/closes pull
requests, manages labels and `oiax/` refs, adopts safe HTTP 422 duplicate
creates, paginates and bounds merged-request discovery, and applies
bounded retries where replay is safe.

## `internal/cli`

The Cobra command tree: `validate`, `plan`, `reconcile`, `graph`,
`version`, and the hidden `gen docs` generator that produces
`docs/reference/cli.md` (drift-gated in CI). Exit codes are a
compatibility contract; see the [configuration
reference](reference/configuration.md).

## `internal/cienv`

CI-host detection (`Detect`): GitHub Actions (`GITHUB_ACTIONS`) or Azure
Pipelines (`TF_BUILD`). Drives which annotation dialect and run-summary
mechanism the CLI uses and whether the pinned-config-ref working-tree
fallback is refused. Detection only; no credentials, no promotion logic.

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
- `templates/azure-pipelines/oiax.yml` — the Azure Pipelines steps
  template: the Action's sibling wrapper (same download-verify-prepare-run
  contract, pinned by `internal/actioncontract` tests) for GitHub-hosted
  repositories built on Azure DevOps. Still no promotion logic.
- `Taskfile.yml` — the task runner (`go -C tools tool task --list`).
- `tools/` — pinned tool dependencies (task).
