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
the configuration documentation. It also owns the canonical semantic
validation and defaulting ([ADR 0010](adr/0010-exported-config-validation.md)):
`(*PromotionGraph).Validate` is the single rule set `oiax validate` runs,
so an external Go integrator gets the identical checks without importing
`internal/`, and `(*PromotionGraph).Default` is the documented defaulting
pass. Everything else in the module is `internal` until external
consumers demonstrate a real need.

Key types: `PromotionGraph`, `PromotionGraphSpec`, `Branch`,
`Promotion`, `Backflow`. Key methods: `Validate`, `Default`.

## `internal/config`

Loading and syntactic validation: strict YAML decoding (unknown fields
rejected), apiVersion/kind checking, single-document enforcement.
Resolving the pinned configuration ref is the caller's job; this package
parses bytes. Semantic graph rules live in the engine.

Entrypoints: `config.Load`, `config.Parse`, `config.DefaultPath`.

## `internal/engine`

The provider-neutral core: graph conversion and default resolution
(`FromConfig` — semantic validation lives on the public document type,
`pkg/api/v1`'s `Validate`, and runs before conversion), the
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

## `internal/tmpl`

Renders the human-facing text Oiax authors (request titles/bodies, the
conflict artifact, the merge-strategy merge-commit message) from
`spec.templates` — Go `text/template` over a documented context with a
curated, deterministic funcmap. Compiles and sample-renders everything
at configuration load, enforces the marker-ownership and untrusted-text
rules, and ships built-in defaults byte-identical to the pre-template
strings ([ADR 0011](adr/0011-templatable-request-text.md)).

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

## `internal/forge/marker`

The forge-neutral managed-request marker: the HTML-comment metadata block
(`graph`/`type`/`source`/`destination`/`sourceHead`), its labels, and its
injection defenses (`Validate`/`Sanitize`). Both providers serialize and
parse identity from this one implementation, so the frozen format — a
compatibility contract — and its security posture never drift between
them.

## `internal/forge/azuredevops`

The Azure DevOps forge provider (`forge.Forge` against REST api-version
7.1), plus the Azure DevOps repository identity — the
organization/project/repository triple — resolved from the Azure
Pipelines environment (`TfsGit` builds) or by parsing
`dev.azure.com`/`visualstudio.com` remote URLs. It manages Azure Repos
pull requests (marker-first description plus a durable PR-properties
copy), pushes/deletes `oiax/` branches, records backflow conflicts as
Azure Boards work items (type from `OIAX_ADO_WORKITEM_TYPE`,
category-driven state), and reads per-branch merge-strategy policy.
Authenticates with `AZURE_DEVOPS_TOKEN` (PAT as Basic or
`System.AccessToken` JWT as Bearer); credentials never appear in output,
and errors never echo URL userinfo (where PATs are commonly embedded).

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
