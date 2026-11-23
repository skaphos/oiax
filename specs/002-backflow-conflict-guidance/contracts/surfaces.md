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

- `<from>`, `<to>` are passed through `textsafe.Markdown` (trusted, but may contain `|`, `` ` ``, `*`, `_`, `<`, `@`).
- `<subject>` is `textsafe.Markdown(textsafe.Subject(raw))`: the header is Oiax-controlled text (FR-016).
- Unhostable (FR-008a): the statement says this section is the only guidance for the conflict, that it lasts only as long as the CI host retains the run, and links the operating guide's [Keep Issues enabled](../../../docs/guides/operating.md#keep-issues-enabled-github-backflow-only) section.
- Render failure (FR-005): the guidance body is replaced by `The configured conflict text could not be rendered; see the run log.`
- Template gate (FR-016): when the configured template reads commit text (`tmpl.Set.ConflictBodyReadsCommitText`), the guidance body is the built-in one, preceded by `The configured conflict text includes commit text, so Oiax shows its built-in steps here.`
- Size budget (FR-005, R8): space for every reduced section (no guidance body) and the note is reserved first; full sections are upgraded in plan order while they fit. When any section is reduced, the summary ends with one `Some conflict guidance was shortened to fit the summary size limit.` When even the reduced sections and the note do not fit (including a zero budget), no section is written, and the warnings and exit-3 output treat the summary as not written (§1, §3).

## 3. Exit-3 output (stderr, printed by `Execute`)

```text
converged with reported divergence on <N> <edge|edges>:
  <edge>: backflow conflict; resolution steps: <address>
  <edge>: backflow conflict; no durable conflict record (Issues are disabled); resolution steps are in this run's summary
  <edge>: backflow conflict; conflict <noun> not recorded this run, will be retried next run; resolution steps are in this run's summary
  <edge>: backflow conflict; conflict <noun> not recorded this run, will be retried next run; resolution steps: <guide>
  <edge>: reported divergence
```

- `<edge|edges>` is `edge` when N is 1 and `edges` otherwise ("on 1 edge:", "on 2 edges:").
- Lines follow plan action order; one per edge (FR-009, FR-011).
- "are in this run's summary" appears only if the summary sections were written (FR-010); otherwise the guide address is used. A known artifact always uses its address. The not-recorded lines always carry the next-run retry statement (FR-008b).
- No untrusted text appears (FR-010): no subjects, no plan reasons.
- When there are no reports (should not happen with exit 3), the single line `converged with reported divergence` is printed, as today.

## 4. Hosts and forges (FR-012)

| Host | Warning command | Summary destination |
| --- | --- | --- |
| GitHub Actions | `::warning::` | append to `$GITHUB_STEP_SUMMARY` |
| Azure Pipelines | `##vso[task.logissue type=warning]` | temp Markdown file in `$AGENT_TEMPDIRECTORY`, attached with `##vso[task.uploadsummary]<escaped path>` |
| none (local) | plain stderr line only | none: a known artifact keeps its address as the pointer; only no-artifact cases (unhostable, not recorded) use `<guide>` (FR-001, §1) |

Content is identical across GitHub Actions + GitHub, Azure Pipelines + GitHub, and Azure Pipelines + Azure Repos, apart from the artifact noun, the address, and GitHub-only FR-008a wording.
