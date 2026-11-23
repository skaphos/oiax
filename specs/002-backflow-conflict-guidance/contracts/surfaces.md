# Contract: Output Surfaces

**Spec**: [../spec.md](../spec.md) FR-001, FR-002, FR-005, FR-008 to FR-012.

These formats are documented behavior, not compatibility contracts (Constitution XII; spec OOS-003). Tests assert them; wording may change in later releases. `<edge>` is `<from> -> <to>`. `<subject>` is always contained text for the surface ([contained-text.md](contained-text.md)). `<guide>` is `https://github.com/skaphos/oiax/blob/main/docs/guides/backflow.md#when-a-replay-conflicts`.

## 1. Conflict warning (one per conflicting edge, emitted by the CLI after apply)

Leading text (format unchanged from today):

- cherry-pick: `backflow <edge> halted on cherry-pick conflict at <sha> "<subject>" after <n> applied cleanly; no request created`
- merge: `backflow <edge> halted on merge conflict at <sha> "<subject>"; no request created`

Followed by exactly one pointer, chosen by artifact status and whether the summary was written:

| Artifact | Summary written | Pointer appended |
| --- | --- | --- |
| known | either | `. Resolution steps: <address>` |
| unhostable | yes | `. No durable conflict record can be kept (Issues are disabled); resolution steps are in this run's summary` |
| unhostable | no | `. No durable conflict record can be kept (Issues are disabled); resolution steps: <guide>` |
| not recorded | yes | `. The conflict <noun> could not be recorded or confirmed this run and will be retried next run; resolution steps are in this run's summary` |
| not recorded | no | `. The conflict <noun> could not be recorded or confirmed this run and will be retried next run; resolution steps: <guide>` |

The line ends with Oiax-derived text, never with `<subject>` (FR-014). Under GitHub Actions it is emitted as a `::warning::` workflow command; under Azure Pipelines as `##vso[task.logissue type=warning]`; the plain log copy goes to stderr on every host (FR-012).

## 2. Summary section (one per conflicting edge, appended after the plan summary)

Written only under a detected CI host with a summary destination (FR-002), after apply, including when apply ends with exit 1.

```markdown
### Backflow conflict: <from> → <to>

No request was created for this edge.

- **Failing commit:** `<sha>` — <subject>
- **Applied cleanly before the conflict:** <n>          ← cherry-pick
- **The whole set conflicted** (merge strategy)          ← merge
- **Conflict record:** [<noun> <id>](<address>)         ← known
  | <FR-008 statement>                                   ← unhostable / not recorded

<guidance body: the backflowConflict template rendered with .Surface = "summary">

See [When a replay conflicts](<guide>) in the backflow guide.
```

- `<from>`, `<to>` are escaped for Markdown (trusted, but may contain `|`, `` ` ``, `*`, `_`, `<`, `@`).
- Unhostable (FR-008a): the statement says this section is the only guidance for the conflict, that it lasts only as long as the CI host retains the run, and links the operating guide's [Keep Issues enabled](../../../docs/guides/operating.md#keep-issues-enabled-github-backflow-only) section.
- Render failure (FR-005): the guidance body is replaced by `The configured conflict text could not be rendered; see the run log.`
- Size budget (FR-005, R8): when a full section would exceed the remaining budget, the section is written without the guidance body and ends with `Some conflict guidance was shortened to fit the summary size limit.`

## 3. Exit-3 output (stderr, printed by `Execute`)

```text
converged with reported divergence on <N> <edge|edges>:
  <edge>: backflow conflict; resolution steps: <address>
  <edge>: backflow conflict; no durable conflict record (Issues are disabled); resolution steps are in this run's summary
  <edge>: backflow conflict; conflict <noun> not recorded this run; resolution steps: <guide>
  <edge>: reported divergence
```

- `<edge|edges>` is `edge` when N is 1 and `edges` otherwise ("on 1 edge:", "on 2 edges:").
- Lines follow plan action order; one per edge (FR-009, FR-011).
- "are in this run's summary" appears only if the summary sections were written (FR-010); otherwise the guide address is used.
- No untrusted text appears (FR-010): no subjects, no plan reasons.
- When there are no reports (should not happen with exit 3), the single line `converged with reported divergence` is printed, as today.

## 4. Hosts and forges (FR-012)

| Host | Warning command | Summary destination |
| --- | --- | --- |
| GitHub Actions | `::warning::` | append to `$GITHUB_STEP_SUMMARY` |
| Azure Pipelines | `##vso[task.logissue type=warning]` | temp Markdown file in `$AGENT_TEMPDIRECTORY`, attached with `##vso[task.uploadsummary]<escaped path>` |
| none (local) | plain stderr line only | none (pointers use `<guide>`) |

Content is identical across GitHub Actions + GitHub, Azure Pipelines + GitHub, and Azure Pipelines + Azure Repos, apart from the artifact noun, the address, and GitHub-only FR-008a wording.
