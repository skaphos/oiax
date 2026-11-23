# Data Model: Backflow Conflict Guidance on the Failing Run

**Spec**: [spec.md](spec.md) · **Research**: [research.md](research.md)

None of these types is serialized or exposed outside the binary except as noted. The JSON plan, managed-request metadata, and exit codes are unchanged (spec FR-017).

## DivergenceReport (`internal/reconcile`, new)

One entry per edge that contributed to exit 3, appended to `Result.Divergences` in plan action order (spec FR-011).

| Field | Type | Notes |
| --- | --- | --- |
| `From`, `To` | string | The edge (validated branch names; trusted). |
| `Kind` | `DivergenceKind` | `KindConflict` (replay conflict) or `KindOther` (any other reported divergence, for example a merge-strategy fence). |
| `Conflict` | `*ConflictGuidance` | Set only for `KindConflict`. |

**Validation**: at most one report per edge per run. A conflict on an edge that also has an `ActionReportDivergence` is a single `KindConflict` report.

## ConflictGuidance (`internal/reconcile`, new)

| Field | Type | Notes |
| --- | --- | --- |
| `Strategy` | string | `cherry-pick` or `merge`. |
| `SHA` | string | Failing commit, full object id. |
| `Subject` | string | `textsafe.Subject` of the raw subject: capped, control and `Cf` stripped. Still untrusted: each surface applies its own function on output. |
| `Applied` | int | Commits applied cleanly before the conflict (cherry-pick). |
| `Whole` | bool | True for merge (the whole set conflicted). |
| `Artifact` | `ArtifactStatus` | See below. |
| `ArtifactURL` | string | Set only when `Artifact == ArtifactKnown`. Built by the provider (R3). |
| `ArtifactNoun` | string | `issue` or `work item`, from the forge kind (used in summary and warning text). |
| `SummaryBody` | string | The guidance body rendered for the summary surface (R4, R5), or empty. |
| `RenderFailed` | bool | True when the summary-surface render failed (spec FR-005). |

### ArtifactStatus

| Value | Meaning | Set when (`recordBackflowConflict`) |
| --- | --- | --- |
| `ArtifactKnown` | The artifact exists and its address is known. | Created; adopted (same head); advanced; advance failed (`UpdateConflictArtifact` error, `existing` still known); left alone (staler run, shallow clone, ancestry or existence check error); canonical after consolidation. Also a render failure after list when `existing` is known. |
| `ArtifactUnhostable` | The repository cannot host artifacts (spec FR-008a). | `SupportsConflictArtifacts` returned `false, nil` (GitHub, Issues disabled). |
| `ArtifactNotRecorded` | Not recorded or confirmed this run; retried next run (spec FR-008b). | List failed; create failed; render failed with no `existing`. |

**State transitions**: computed once per conflicting edge per run; not persisted. The next run recomputes from the forge.

## Result (`internal/reconcile`, changed)

Adds `Divergences []DivergenceReport`. Existing fields (`Divergence bool`, `Conflicts int`, …) are unchanged, and `Divergence` remains the exit-3 signal. Invariant: `Divergence == (len(Divergences) > 0)`.

## forge.ConflictArtifact (`internal/forge`, changed)

Adds `URL string`: the artifact's forge web page, built by the provider from its resolved identity and the artifact identifier (R3). Never parsed from forge-returned text. Empty only in test fakes that opt out; real providers always set it on `List` and `Create`.

| Provider | Format |
| --- | --- |
| GitHub | `https://github.com/{PathEscape(owner)}/{PathEscape(repo)}/issues/{number}` |
| Azure Boards | `https://dev.azure.com/{PathEscape(organization)}/{PathEscape(project name)}/_workitems/edit/{id}` |

## tmpl.Context (`internal/tmpl`, changed)

Adds `Surface string`: `"artifact"` or `"summary"` (R4). Additive under ADR 0011. `tmpl.Conflict.Subject` now receives `textsafe.Subject(...)` (spec FR-013); on the summary surface the coordinator passes `textsafe.Markdown(textsafe.Subject(...))`, so the template never sees raw Markdown-active text there.

## Contained text (`internal/textsafe`, new)

See [contracts/contained-text.md](contracts/contained-text.md). Pure functions, no state.
