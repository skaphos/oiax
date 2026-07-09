# Configuration reference

Oiax is configured by a repository-local file, default path
`.oiax.yaml`, containing exactly one `PromotionGraph` document. Unknown
fields are rejected. Multi-document files are rejected (multiple graphs
per repository are reserved for a future version).

The file is read from a **pinned ref** — the repository default branch
unless `--config-ref` says otherwise — never from the ref that triggered
the run. Configuration on other refs is ignored; see
[ADR 0003](../adr/0003-pinned-configuration-ref.md).

## PromotionGraph

| Key | Type | Required | Meaning |
| --- | --- | --- | --- |
| `apiVersion` | string | yes | Must be `oiax.skaphos.dev/v1alpha1`. |
| `kind` | string | yes | Must be `PromotionGraph`. |
| `metadata.name` | string | yes | Graph name, used in managed-request metadata, plans, and logs. |
| `spec.branches` | map | yes | Long-lived branches by name. Every branch referenced by an edge or backflow must appear. Oiax never creates long-lived branches. |
| `spec.promotions` | list | yes | Directed promotion edges. Must form a DAG; disconnected components are allowed. |
| `spec.backflow` | object | no | Hotfix-return policy. |

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
| `expectations.mergeMethod` | string | no | `merge`, `squash`, or `rebase`. Reporting metadata only: Oiax warns when repository settings contradict it and never modifies settings. Recommended where possible: disable squash on promotion targets for cheaper, exact detection. |

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
| `--config-ref` | unset (working-tree file) | Ref the configuration is read from, via `git show <ref>:<path>`. In CI, pin this to the repository default branch; resolving it automatically is roadmap scope. |
| `--output`, `-o` | `text` | Output format for plan-producing commands: `text` or `json`. |

## Environment variables

| Variable | Default | Meaning |
| --- | --- | --- |
| `OIAX_LOG_FORMAT` | `text` | Structured log format: `text` or `json`. (Planned; logging lands with edge evaluation.) |

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
