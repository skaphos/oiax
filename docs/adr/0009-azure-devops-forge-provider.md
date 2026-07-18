# 0009 — Azure DevOps forge provider

- Status: Proposed
- Date: 2026-07-18

## Context

Oiax reconciles branch-promotion requests through the provider-neutral
`forge.Forge` interface ([internal/forge/forge.go](../../internal/forge/forge.go)).
The first and, until now, only implementation targets GitHub. Phases 1–3
of the Azure DevOps work (SKA-599..602) made the CLI-facing behavior
host-neutral (CI detection, Azure Pipelines annotations and summary),
packaged the binary as an Azure Pipelines steps template, and added
forge **selection** — `resolveForgeKind` picks `github` or `azuredevops`
from `OIAX_FORGE`, the CI environment, or the origin remote — while the
`azuredevops` arm still refused with `forge.ErrNotImplemented`.

This ADR covers the decision that fills that arm: an Azure DevOps
implementation of `forge.Forge`, and the two places where mapping the
interface onto Azure DevOps forces a **contract** decision rather than a
mechanical translation — the managed-request marker and the durable
conflict artifact. Azure DevOps is a genuinely different substrate:

- Repository identity is an organization/project/repository **triple**,
  not GitHub's owner/repo pair (already resolved by
  [`azuredevops.ResolveRepo`](../../internal/forge/azuredevops/azuredevops.go)).
- A pull request's `description` is **truncated to 400 characters** in
  the list response ([Git > Pull Requests > Get Pull Requests], REST 7.1);
  the full text requires a GET by id.
- There is **no forge-issue type**. The nearest durable, labelable,
  first-class object is an **Azure Boards work item**, whose work-item
  **type** and **state model vary by process template** (Basic has
  "Issue"; Agile/Scrum/CMMI do not, and their closed-state name differs).
- There are **no repository-level merge-button settings**. Which merge
  strategies a branch permits is governed by the per-branch **"Require a
  merge strategy"** branch policy.
- Duplicate-active-PR rejection is **HTTP 409 `TF401179`**, not GitHub's
  422 head/base rejection.

The invariants from [ADR 0002](0002-content-based-divergence-detection.md)
(no private state database — Git and the forge are the only sources of
truth) and the agent safety rules (credentials never in output; the
engine never imports `forge`/`git`; mutation confined to the `oiax/`
namespace) bind this provider exactly as they bind the GitHub one.

## Options considered

### Marker storage on a managed request

The managed-request marker — the HTML-comment block carrying
`graph`/`type`/`source`/`destination`/`sourceHead`, defended by
`marker.Validate`/`Sanitize` — is how Oiax recognizes its own requests
across runs without a private database. On GitHub it lives at the **end**
of the PR body and is parsed straight from the list response. Azure
DevOps's 400-character list truncation breaks that: a marker after a
long human description would be cut off in the list, so
`ListManagedRequests` could not recognize the request cheaply.

- *Marker at the end of the description, GitHub-identical.* Fails the
  list path: truncation can drop it, and re-reading the full description
  per candidate PR is an N+1 fetch over **every** active PR in the repo
  (human PRs included), not just Oiax's few.
- *Marker in PR properties only.* Azure DevOps PRs carry an arbitrary
  key/value **Properties** collection (durable metadata, invisible in the
  UI). Storing identity there is durable against a human editing the
  description — but properties are **not** returned in the PR list
  either, so the list path still pays N+1, and the marker loses the
  cross-provider "same encoding in the body" property that makes a
  managed request self-describing to a human reading it on any forge.
- *Dual-write: marker **first** in the description AND in PR properties.*
  Writing the marker at the **start** of the description keeps it within
  the first 400 characters for every realistic graph/branch name, so the
  list path recognizes managed requests from the (truncated) list
  response with **no extra call** — the shared `marker.Parse`/`Replace`
  scan for the block anywhere in the text, so leading position needs no
  new code and the encoding is byte-identical to GitHub's. In parallel,
  the same marker fields are written to PR properties as the **durable**
  copy: the single-PR mutation paths (update, close) GET the full,
  untruncated PR and prefer the properties marker, falling back to the
  description parse — so a human who edits or deletes the marker out of
  the description does not make Oiax lose track of the request. **Chosen.**

### The durable conflict artifact

[ADR 0008](0008-durable-backflow-conflict-artifact.md) made the
backflow-conflict artifact a **labeled forge issue** whose identity is
the body marker (`type: conflict`) plus the load-bearing `oiax` +
`oiax/conflict` labels, consolidated to one-per-edge on the read path.
Azure DevOps has no issue type.

- *A pull request with no changes, standing in for an issue.* Abuses the
  PR model (a PR needs a source/target and would show as mergeable), and
  pollutes managed-request discovery. Rejected.
- *An Azure Boards work item.* A first-class, durable, labelable object,
  visible in the org, carrying an HTML `System.Description` for the
  marker + operator playbook and `System.Tags` for the `oiax` /
  `oiax/conflict` labels. It is the structural analogue of the GitHub
  issue. **Chosen** — but two Azure-specific facts shape it:
  - The **work-item type varies by process** (Basic: "Issue";
    Agile/Scrum: no "Issue"). The type must be configurable.
    - *A declarative `.oiax.yaml` field.* Discoverable, but the config is
      a versioned compatibility API ([ADR 0005](0005-config-api-v1.md));
      adding a field is an API change requiring its own ADR and migration
      story, for what is a per-runner environment detail. Rejected as
      premature.
    - *An environment variable `OIAX_ADO_WORKITEM_TYPE` (default
      `Issue`).* Low-ceremony, no compatibility contract, sits beside the
      other environment knobs (`OIAX_FORGE`, `AZURE_DEVOPS_TOKEN`).
      **Chosen.**
  - The **closed-state name varies by process** ("Closed" vs "Done").
    Hard-coding either breaks the other with a 400 invalid-transition.
    The provider instead reads the work-item type's states
    (`/wit/workitemtypes/{type}/states`) and selects the state whose
    **category** is `Completed` — correct by construction across
    processes. "Open" for `ListConflictArtifacts` is symmetrically any
    state **not** in the `Completed`/`Removed` categories, and the query
    orders by `[System.Id] ASC` so the lowest-id canonical choice is
    deterministic (the ordering `forge.Forge` requires).

### Authentication and the token

- *A single fixed scheme.* Azure DevOps accepts a **PAT** as HTTP Basic
  (`base64(":" + PAT)`) and a **pipeline OAuth token**
  (`$(System.AccessToken)`, a JWT) as **Bearer**. Fixing one scheme
  would reject the other — and `System.AccessToken` is the zero-config,
  no-secret-management choice in a pipeline (the Azure analogue of
  `GITHUB_TOKEN`).
- *Detect the scheme from the token shape.* A `System.AccessToken` is a
  JWT (`eyJ…` with two dots); a PAT never is. The provider sends Bearer
  for a JWT and Basic for anything else, so **either token just works**
  through one environment variable. **Chosen.** The token env is
  `AZURE_DEVOPS_TOKEN` (mirroring `GITHUB_TOKEN`'s role); the scheme
  choice is applied identically to REST calls and to the git-push
  `http.extraHeader`.

## Decision

Implement `forge.Forge` in `internal/forge/azuredevops` against the
Azure DevOps REST API (api-version 7.1), wired into
`internal/cli/wiring.go` `newForge` in place of the
`forge.ErrNotImplemented` refusal. The engine and reconcile layers are
untouched (depguard keeps the engine from importing `forge`/`git`); the
new provider is the only new production code path. Implementation tracked
as SKA-602.

**D1 — REST surface (api-version 7.1).** The 12 `forge.Forge` methods map
as:

| Method | Azure DevOps REST |
|---|---|
| `ListManagedRequests` | `GET .../git/repositories/{repo}/pullrequests?searchCriteria.status=active` (open) or `completed` (merged), paged by `$top`/`$skip`; marker read from the (truncated) description, marker-first |
| `CreateRequest` | `POST .../pullrequests` (plain JSON). **409 `TF401179`** (duplicate active PR) is adopted as success, exactly as the GitHub 422 path; then PR properties are written and the `oiax`/type labels attached |
| `UpdateRequest` | `PATCH .../pullrequests/{id}` description (≤4000 chars) and PR properties |
| `CloseRequest` | `POST .../pullrequests/{id}/threads` (reason comment), then `PATCH` `status: abandoned` |
| `PushBranch` | `git push` shell-out, identical posture to GitHub (namespace refusal, `check-ref-format`, `oidPattern`, `--end-of-options`, credential via `http.extraHeader` in the environment, token scrubbed from errors) |
| `DeleteBranch` | resolve the ref's current objectId, then `POST .../refs` `[{name, oldObjectId, newObjectId: 0*40}]`; a missing ref or `succeededNonExistentRef` is idempotent success; namespace-confined |
| `ListConflictArtifacts` | WIQL `POST .../wit/wiql` filtered by `System.Tags CONTAINS 'oiax'`/`'oiax/conflict'`, `ORDER BY [System.Id] ASC`, hydrated and filtered to non-`Completed`/`Removed` states |
| `CreateConflictArtifact` | `POST .../wit/workitems/${type}` (JSON Patch) with `System.Title`, HTML `System.Description` (marker + playbook), `System.Tags` |
| `UpdateConflictArtifact` | `PATCH .../wit/workitems/{id}` (JSON Patch) rewriting the description |
| `CloseConflictArtifact` | add a comment, then `PATCH` `System.State` to the type's `Completed`-category state |
| `RepoMergeMethods` | no repo-level setting exists → reports all methods allowed |
| `TargetMergeMethods` | `GET .../policy/configurations?policyType=fa4e907d-c16b-4a4c-9dfa-4916e5d171ab`, filtered client-side to a scope matching `refs/heads/{branch}`; the allowed strategies map to `MergeMethods`, and a policy forbidding no-fast-forward sets `RequiresLinearHistory` |

**D2 — Marker: dual-write, marker-first description + PR properties.** As
argued above. The list path recognizes from the description (marker-first,
truncation-safe); the single-PR paths prefer the durable properties copy
and fall back to the description. The marker **encoding** is the shared,
frozen [`internal/forge/marker`](../../internal/forge/marker) package —
byte-identical across providers, with its injection defenses
(`Validate`/`Sanitize`) applied on every write.

**D3 — Conflict artifact: a tagged work item, configurable type,
category-driven state.** Type from `OIAX_ADO_WORKITEM_TYPE` (default
`Issue`); open/closed decided by state **category**, never a hard-coded
name; identity is the `System.Description` marker (`type: conflict`) plus
the load-bearing `oiax` + `oiax/conflict` **tags** — the tag playing the
authorization role the label plays on a GitHub issue (only a
contributor can tag an in-project work item). One-per-edge consolidation
and the ascending-id canonical rule are unchanged from ADR 0008 and live
in `internal/reconcile`, not the provider.

**D4 — Auth via `AZURE_DEVOPS_TOKEN`, scheme by token shape.** JWT →
Bearer, otherwise Basic `base64(":" + token)`; applied identically to
REST and the git-push `http.extraHeader`. The token never appears in any
error, log, plan, or the process table (delivered out of band via
`GIT_CONFIG_*`), matching the GitHub provider's guarantees.

The marker encoding is a compatibility contract; this ADR records that
Azure DevOps stores the **same** encoding, additionally mirrored to PR
properties, and does **not** change the marker schema. The plan JSON
(`planFormatVersion: 1`), exit codes, and `pkg/api` are untouched.

## Consequences

- **Positive:** Oiax runs end to end on Azure DevOps — managed promotion
  and backflow requests, durable conflict artifacts, and the
  merge-method fence — reusing the engine, reconcile state machine, and
  marker encoding unchanged. Provider parity is enforced by a shared
  conformance suite (SKA-602 phase 5).
- **Positive:** one token, either kind. `AZURE_DEVOPS_TOKEN` accepts a
  PAT or `$(System.AccessToken)` transparently; the pipeline zero-config
  path works without minting a secret.
- **Accepted tradeoff (marker-first list recognition is truncation-bounded):**
  a pathologically long graph+branch name whose marker exceeds 400
  characters would not be recognized from the list response. This is far
  outside any real name length, the durable properties copy still
  identifies the request on the single-PR paths, and the failure mode is
  "not recognized" (a duplicate is never created against a request Oiax
  can't see because the forge's own `TF401179` still refuses it).
- **Accepted tradeoff (work-item state coupling):** closing and
  open-filtering depend on the process template's state **categories**,
  read live per type. A process with a bespoke state model that leaves
  the `Completed` category empty would have no close target; this is a
  misconfiguration surfaced as a clear error, not a silent failure.
- **Accepted tradeoff (`OIAX_ADO_WORKITEM_TYPE` is per-runner, not
  declarative):** the work-item type is an environment variable, so it is
  not captured in the promoted `.oiax.yaml`. If it later needs to be
  declarative it becomes a config-API change with its own ADR; the env
  var is forward-compatible with that (a config field would take
  precedence).
- **Neutral:** the marker schema, plan JSON contract, exit codes, and
  `pkg/api` are unchanged. This ADR adds a provider; it supersedes
  nothing.

## Links

- Implements the provider seam declared by
  [ADR 0002](0002-content-based-divergence-detection.md) (identity from
  Git + forge alone, no private state)
- Extends the durable conflict artifact of
  [ADR 0008](0008-durable-backflow-conflict-artifact.md) onto Azure
  Boards work items
- Marker encoding: [`internal/forge/marker`](../../internal/forge/marker)
  (shared, frozen format)
- Configuration API stability: [ADR 0005](0005-config-api-v1.md) (why the
  work-item type is an env var, not a config field)
