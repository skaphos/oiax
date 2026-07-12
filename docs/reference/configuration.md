# Configuration reference

Oiax is configured by a repository-local file, default path
`.oiax.yaml`, containing exactly one `PromotionGraph` document. Unknown
fields are rejected. Multi-document files are rejected (multiple graphs
per repository are reserved for a future version).

`plan` and `reconcile` read the file from a **pinned ref** — the
repository default branch unless `--config-ref` says otherwise — never
from the ref that triggered the run. Configuration on other refs is
ignored; see [ADR 0003](../adr/0003-pinned-configuration-ref.md). The
inspection commands `validate` and `graph` read the working-tree file by
default; pass `--config-ref` to inspect a pinned ref instead.

Under GitHub Actions the composite Action fetches every branch head and runs
`git remote set-head origin --auto`, so the default branch (`origin/HEAD`)
resolves and the pinned-ref default works without setting `--config-ref`. Use
`actions/checkout` with `fetch-depth: 0` so divergence detection sees full
history.

## PromotionGraph

| Key | Type | Required | Meaning |
| --- | --- | --- | --- |
| `apiVersion` | string | yes | Must be `oiax.skaphos.dev/v1`. The pre-1.0 `oiax.skaphos.dev/v1alpha1` is accepted as a deprecated alias — see [Migration](#migration--deprecated-alias). |
| `kind` | string | yes | Must be `PromotionGraph`. |
| `metadata.name` | string | yes | Graph name, used in managed-request metadata, plans, and logs. |
| `spec.branches` | map | yes | Long-lived branches by name. Every branch referenced by an edge or backflow must appear. Oiax never creates long-lived branches. |
| `spec.promotions` | list | yes | Directed promotion edges. Must form a DAG; disconnected components are allowed. |
| `spec.backflow` | object | no | Hotfix-return policy. |

### Migration / deprecated alias

The canonical apiVersion is `oiax.skaphos.dev/v1`. Configurations
declaring the pre-1.0 `oiax.skaphos.dev/v1alpha1` still parse unchanged
(the contract is identical), but every load emits a one-line deprecation
warning to stderr. Migrate by changing the `apiVersion` string to
`oiax.skaphos.dev/v1`; nothing else in the document changes. See
[ADR 0005](../adr/0005-config-api-v1.md).

## `spec.branches.<name>`

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `role` | string | unset | `source` — authoritative entry branch, must not be any edge's destination; the backflow target must have this role. `terminal` — exit branch, must not be any edge's source. Unset — intermediate. |
| `drift` | string | `forbidden` | `forbidden`: downstream-only content is reported (and returned via backflow if the branch is a backflow source). `expected`: downstream-only content is steady state, acknowledged silently; promotion detection is unaffected. A backflow source must not declare `expected`. |

## `spec.promotions[]`

| Key | Type | Required | Meaning |
| --- | --- | --- | --- |
| `from` | string | yes | Source branch (must be declared in `spec.branches`). |
| `to` | string | yes | Destination branch (must be declared; distinct from `from`; each `from`/`to` pair at most once). |
| `expectations.mergeMethod` | string | no | `merge`, `squash`, or `rebase`. Reporting metadata: Oiax warns (on stderr) when the repository's merge-button settings do not permit the configured method, and never modifies settings. Recommended where possible: disable squash on promotion targets for cheaper, exact detection. |

## `spec.backflow`

| Key | Type | Required | Meaning |
| --- | --- | --- | --- |
| `sources` | list | yes | Downstream branches whose downstream-only commits are returned to `target`. |
| `target` | string | yes | The authoritative branch commits are returned to. Must be declared, have role `source`, and not appear in `sources`. Exactly one target per graph in v1. |
| `strategy` | string | `cherry-pick` | Return mechanism. v1 supports only `cherry-pick` (with `-x` provenance trailers). |

## Flags

Persistent flags on every command (precedence: flag → default; see the
generated [CLI reference](cli.md) for per-command flags):

| Flag | Default | Meaning |
| --- | --- | --- |
| `--config` | `.oiax.yaml` | Path to the configuration file. |
| `--config-ref` | see note | Ref the configuration is read from, via `git show <ref>:<path>`. Default: the repository default branch (`origin/HEAD`) for `plan`/`reconcile`, the working-tree file for `validate`/`graph`. When the default branch cannot be resolved (no `origin/HEAD`), `plan`/`reconcile` fall back to the working-tree file locally but refuse under GitHub Actions — pin this flag to recover. |
| `--output`, `-o` | `text` | Output format for plan-producing commands: `text` or `json`. |

## Environment variables

| Variable | Default | Meaning |
| --- | --- | --- |
| `GITHUB_TOKEN` | none | **Required.** Token the GitHub provider authenticates with — creating, updating, closing, and listing managed requests, and pushing backflow branches. See [Architecture — Tokens](../architecture.md#tokens) for the token-type tradeoffs (`GITHUB_TOKEN` works out of the box but is degraded: created pull requests do not trigger other workflows). |
| `OIAX_LOG_FORMAT` | `text` | Structured log format: `text` or `json`. |

## Exit codes

Exit codes are part of the CLI's compatibility contract, following the
`terraform plan -detailed-exitcode` convention so CI systems can gate on
Oiax without parsing output.

| Command | Code | Meaning |
| --- | --- | --- |
| `plan` | 0 | Fully in sync (without `--detailed-exitcode`: any successful plan). |
| `plan` | 1 | Error. |
| `plan` | 2 | Valid plan with pending actions (only with `--detailed-exitcode`). |
| `reconcile` | 0 | Converged, including "applied actions successfully". |
| `reconcile` | 1 | Error. |
| `reconcile` | 3 | Converged with reported divergence requiring human attention (unresolvable diverged edges, backflow conflicts). |
| all others | 0/1 | Success/error. |
