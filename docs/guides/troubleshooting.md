# Troubleshooting

Symptoms, their causes, and the fix. Messages below are quoted verbatim
so you can match them against what you see. Under GitHub Actions,
warnings appear as `::warning::` annotations; the CLI prints them to
stderr.

## Managed pull requests get no CI and never merge

**Symptom.** Oiax opens promotion PRs, but their required checks never
run, so they cannot merge. You see:

```
created pull request is authored by github-actions[bot]; on: pull_request workflows will not run for it. Configure a GitHub App installation token so managed requests get CI.
```

**Cause.** The PRs were created with the default `GITHUB_TOKEN`, which by
design does not trigger `on: pull_request` workflows.

**Fix.** Use a GitHub App installation token (or a fine-grained PAT).
Full walkthrough: **[Setting up a token that triggers CI](tokens.md)**.

## Spurious or duplicate promotion PRs for content that is already promoted

**Symptom.** Oiax proposes promotions for changes that clearly already
merged, or opens PRs that GitHub then rejects as having no commits. You
may see:

```
shallow clone detected: equivalence detection is degraded (merge-base, patch-identity and baseline rungs are unreliable), which can produce spurious promotion requests; set fetch-depth: 0 on actions/checkout for correct results
```

**Cause.** A shallow clone. `actions/checkout` defaults to
`fetch-depth: 1`, which has no merge base, so the content-based rungs of
the [equivalence ladder](../architecture.md#the-equivalence-ladder) cannot
run.

**Fix.** Set `fetch-depth: 0` on `actions/checkout`. See
[github-action.md](github-action.md#fetch-depth-0-is-not-optional). Doing
so is also worthwhile if you use squash or rebase merges, which lean
hardest on those rungs.

## `git 2.45 or newer is required`

**Symptom.** `plan` or `reconcile` fails immediately with:

```
git 2.45 or newer is required (backflow uses cherry-pick --empty=drop); detected "git version 2.30.2"
```

**Cause.** The runner's git is older than Oiax's floor. Backflow uses
`git cherry-pick --empty=drop`, added in git 2.45. (Ubuntu 22.04 ships
2.34, Debian bookworm 2.39.)

**Fix.** Use a runner with git ≥ 2.45 — `ubuntu-latest` on GitHub Actions
satisfies it — or upgrade git locally. The `validate` and `graph`
commands do not need it, only `plan`/`reconcile`.

## `cannot resolve the repository default branch`

**Symptom.** Under Actions, `plan`/`reconcile` fails with:

```
cannot resolve the repository default branch (origin/HEAD is not set); pin --config-ref to the default branch, for example --config-ref origin/main
```

**Cause.** Oiax reads configuration from a pinned ref — by default the
repository default branch (`origin/HEAD`). When that ref is not set (a
remote-less or misconfigured checkout), it cannot resolve. Under Actions
it refuses rather than silently reading the triggering ref, which would
run untrusted PR configuration with write credentials.

**Fix.** The `skaphos/oiax` Action prepares refs for you (it runs `git
remote set-head origin --auto`), so this usually means a checkout step ran
without that preparation. Either use the Action, or pin the ref
explicitly: `--config-ref origin/main` (CLI) / `config-ref: origin/main`
(Action). Locally, a remote-less repo simply falls back to the
working-tree file.

## `apiVersion "…v1alpha1" is deprecated`

**Symptom.**

```
warning: apiVersion "oiax.skaphos.dev/v1alpha1" is deprecated; migrate to "oiax.skaphos.dev/v1"
```

**Cause.** Your `.oiax.yaml` declares the pre-1.0 alias.

**Fix.** Change the one string to `oiax.skaphos.dev/v1`. Nothing else
changes — the two decode identically. See
[Migrating the apiVersion](promotion-graphs.md#migrating-the-apiversion).
(Go importers must also move `pkg/api/v1alpha1` → `pkg/api/v1`; there is
no alias for the import path.)

## Configuration won't load

**Symptom.** One of:

```
unsupported apiVersion "…" (want "oiax.skaphos.dev/v1")
unsupported kind "…" (want "PromotionGraph")
configuration is empty
configuration contains multiple YAML documents; v1 accepts exactly one PromotionGraph
```

or a decode error naming an unknown field.

**Cause.** Oiax uses strict decoding: unknown fields are rejected, the
`apiVersion`/`kind` must match, and exactly one document is allowed. A
common one is a typo in a key (`promotons:`), which reads as an unknown
field.

**Fix.** Match the [configuration reference](../reference/configuration.md)
exactly. Check for a stray second `---` document and for misspelled keys.
Run `oiax validate` locally against your working tree to iterate quickly.

## My config change didn't take effect

**Symptom.** You edited `.oiax.yaml` on a branch, but Oiax still behaves
as before.

**Cause.** `plan`/`reconcile` read configuration from a **pinned ref** —
the repository default branch — not from the branch that triggered the
run. This is deliberate ([ADR 0003](../adr/0003-pinned-configuration-ref.md)).

**Fix.** Land the change on the default branch (promote it, the same as
any other change). To test a change on another branch first, pin
`--config-ref <branch>` locally. Details:
[where configuration is read from](promotion-graphs.md#where-configuration-is-read-from).

## Validation errors

**Symptom.** `validate` (or any command) prints one `invalid:` line per
problem, then:

```
.oiax.yaml: 2 validation errors
```

**Cause.** A semantic rule was broken — a cycle, an edge referencing an
undeclared branch, a role constraint (a `source` used as a destination, a
`terminal` used as a source), or an inconsistent backflow policy (target
not `role: source`, target listed in `sources`, or a source declaring
`drift: expected`).

**Fix.** Oiax lists **every** violation at once — fix them as a batch.
See [roles](promotion-graphs.md#roles),
[drift](promotion-graphs.md#drift-policy), and
[backflow config](backflow.md#configuration).

## Merge-method mismatch warning

**Symptom.**

```
config expects "squash" merges on qa -> production-stage-1, but the repository does not allow "squash" merges; enable it in the repository's merge settings or change mergeMethod
```

**Cause.** An edge declares `expectations.mergeMethod`, but the
repository's merge-button settings do not permit it. This is reporting
only — Oiax never changes repository settings.

**Fix.** Either enable that merge method in the repository settings, or
change/remove the edge's `mergeMethod`. See
[merge-method expectations](promotion-graphs.md#merge-method-expectations).

## `reconcile` exited 3

**Symptom.** `reconcile` converged but returned exit code 3, with a
`reported divergence on …` line.

**Cause.** An edge has downstream content the source lacks and no backflow
or drift policy accounts for it, or a backflow replay hit a conflict. Exit
3 means "needs a human," not "failed."

**Fix.** See [when Oiax reports a
divergence](operating.md#when-oiax-reports-a-divergence) and
[backflow conflicts](backflow.md#when-a-replay-conflicts).

## A backflow halted on a conflict

**Symptom.**

```
backflow production-stage-1 -> development halted on cherry-pick conflict at 4f2a91c "hotfix: …" after 2 applied cleanly; no request created
```

**Cause.** A downstream commit genuinely conflicts with the target when
replayed. Oiax never auto-resolves; it pushes nothing and reports.

**Fix.** Resolve it by hand (cherry-pick/promote the fix into the target),
or mark the commit `Oiax-Backflow: skip` if it should stay downstream.
The next reconcile continues from the new state. See
[the backflow guide](backflow.md#when-a-replay-conflicts).

## `--output json is not supported` on validate/graph

**Symptom.**

```
validate: --output "json" is not supported; validate only prints text
```

**Cause.** `validate` and `graph` have no JSON rendering. Rather than
silently ignore `-o json`, they reject it.

**Fix.** Drop `-o json` for those commands. Use `-o json` with `plan` /
`reconcile`, which emit the [plan JSON format](../reference/plan-format.md).

## Cannot determine owner/repo

**Symptom.**

```
resolve repository from origin remote: …
cannot parse owner/repo from remote "…"
```

**Cause.** Oiax resolves the GitHub repository from `GITHUB_REPOSITORY`
(set automatically under Actions) or, failing that, the `origin` remote
URL. Neither was usable.

**Fix.** Under Actions this is set for you. Locally, ensure `origin`
points at the GitHub repository (`git remote -v`), or export
`GITHUB_REPOSITORY=owner/repo`.

## Still stuck?

- Re-read the plan: `oiax plan` (or `plan -o json`) shows exactly what
  Oiax intends before it acts.
- Turn on JSON logs: `OIAX_LOG_FORMAT=json`.
- Check the [Architecture](../architecture.md) doc for how a case is meant
  to behave, and the [Configuration reference](../reference/configuration.md)
  for exact semantics.
