# Implementation Plan: Backflow Conflict Guidance on the Failing Run

**Branch**: `spec/backflow-conflict-guidance` (speckit key `002-backflow-conflict-guidance`) | **Date**: 2026-11-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/002-backflow-conflict-guidance/spec.md`, including the 2026-11-22 clarification (FR-003 = A) and consistency review.

## Summary

When a backflow replay conflicts, `reconcile` exits 3 and the only visible pointer today is a warning logged before the conflict artifact is even recorded; the artifact's address never reaches the run (issue #107). This plan carries a per-edge **conflict report** from apply to the CLI, has the providers build the artifact's **web address** from resolved identity, and makes the CLI emit, after apply: one **warning** per conflicting edge ending with the address (or a no-artifact statement), one **summary section** per edge rendered from the same `backflowConflict` template with a new trusted `.Surface` variable, and an **exit-3 message** naming every contributing edge. A new `internal/textsafe` package contains every untrusted value for each surface, and the annotation handler neutralizes host logging commands in all warnings, closing an existing gap in both the annotation and the plain log copy. No exit code, JSON plan, request metadata, or artifact identity changes.

## Technical Context

**Language/Version**: Go, per `go.mod` (1.27.4).

**Primary Dependencies**: stdlib, Cobra (CLI), `log/slog`. No new runtime dependency.

**Storage**: N/A. Nothing new is persisted; conflict reports live for one process.

**Testing**: `go test -race -shuffle=on`; table-driven unit tests; fuzz targets for the new untrusted-text functions; `internal/forge/forgetest` conformance on both providers; a built-binary scenario via the existing notification-binary fixture overlay.

**Target Platform**: Linux CI runners (ADR 0016): GitHub Actions and Azure Pipelines; local runs on any platform.

**Project Type**: CLI (single Go module).

**Performance Goals**: no new forge or git I/O on the success path. The conflict path adds no API calls: addresses come from data already listed or created.

**Constraints**: summary sections fit 1 MiB minus what this run already wrote (research R8); no new forge writes (spec FR-017); credential values never appear (Constitution VI).

**Scale/Scope**: typically one conflicting edge per run; a handful at most (one per backflow source).

## Constitution Check

*GATE: passed before Phase 0; re-checked after Phase 1 (same results).*

| # | Gate | Status |
|---|------|--------|
| I | New operational concepts are declared config or machine-readable metadata, not convention or title-matching | **PASS**: artifact identity stays marker plus labels (ADR 0008); the address is derived from resolved identity and ID, never from titles or text (R3). |
| II | Configuration is read from the pinned ref; nothing this feature reads from config is executed | **PASS**: the only config input is the existing `backflowConflict` template slot, rendered by the existing pure template engine. |
| III | Identical observed state yields equivalent plans; any new generated artifact is drift-gated | **PASS**: no change to planning; reports follow plan action order; output is a function of state (FR-011). If the `reconcile` help text changes, `docs/reference/cli.md` is regenerated and drift-gated as today. |
| IV | Config surface follows declarative API conventions; git/forge behavior is surfaced, not hidden | **PASS**: the feature surfaces the forge artifact on the run instead of hiding it in the tracker. No config surface change; `.Surface` is a template-context field (ADR 0011). |
| V | No new hard dependency on another Skaphos tool; output stays ordinary Git and forge artifacts | **PASS** |
| VI | Every new decision path can state its reason; failures name a next safe action; no credentials in output | **PASS**: each artifact status carries its reason and next action (link, enable Issues, retried next run, guide). Messages are built from the code that decided (R1), never from logs. |
| VII | `validate`/`graph`/`plan` still work without mutation credentials; mutation stays in `reconcile` | **PASS**: only `reconcile`'s post-apply output changes. |
| VIII | Topology facts are modeled and validated, not inferred from names or ordering | **PASS**: edges come from plan actions. |
| IX | Spec states what this does NOT do; no unverified claims; Skaphos glossary terms used correctly | **PASS**: spec OOS-001 to OOS-007; unverified host behavior (URL clickability, renderer details) is live acceptance (quickstart §5). Uses "backflow" and "branch promotion" per the glossary. |
| X | `internal/engine` stays pure | **PASS**: no engine change. `textsafe` and reporting live in `internal/textsafe`, `internal/reconcile`, `internal/cli`. |
| XI | No merge/approve/deploy/settings mutation; unmanaged requests untouched; force-push confined; notes rules; branch names validated | **PASS**: no new forge writes or pushes (FR-017). |
| XII | Changes to exit codes, JSON plan format, request metadata, config `apiVersion`, or `pkg/api` carry an ADR | **PASS, no ADR**: none of these change. `forge.ConflictArtifact.URL` and `reconcile.Result.Divergences` are internal. `.Surface` is an additive template-context field, which ADR 0011 already permits within v1. The exit-3 message's first line keeps today's text. |

**Engineering gates**: every bug fix ships a regression test (the notification-summary path escaping, R7; the log-command gap, R6); tests run `-race -shuffle=on`; the new untrusted-text functions are fuzzed (R5), with a bounded campaign task (`textsafe:fuzz`) like the notification parsers'; `docs/` updates land in the same change (FR-018). **Coverage floor as a hard gate in this change**: `tools/checkcoverage` gains `-prefix` and `-min` flags (defaults keep today's `internal/notification` / 85% behavior), a `textsafe:coverage` Taskfile task enforces **≥ 95%** for `internal/textsafe`, and CI runs it. Touched packages must not drop below their current floor.

## Project Structure

### Documentation (this feature)

```text
specs/002-backflow-conflict-guidance/
├── spec.md
├── plan.md                         # this file
├── research.md                     # Phase 0: R1–R10
├── data-model.md                   # Phase 1
├── quickstart.md                   # Phase 1: validation guide
├── contracts/
│   ├── surfaces.md                 # warning, summary section, exit-3 output
│   ├── contained-text.md           # internal/textsafe functions and properties
│   └── forge-and-templates.md      # ConflictArtifact.URL, tmpl.Context.Surface
├── checklists/requirements.md
└── tasks.md                        # Phase 2 (/speckit-tasks), not created here
```

### Source Code (repository root)

```text
internal/textsafe/                  # NEW: Subject, LogCommand, PlainLink, Markdown (+ tests, fuzz, testdata/adversarial.txt)
internal/tmpl/tmpl.go               # Context.Surface; built-in conflict body closing text behind .Surface; sampleRender both surfaces
internal/forge/forge.go             # ConflictArtifact.URL
internal/forge/github/github.go     # set URL on list/create (issue web address)
internal/forge/azuredevops/artifacts.go  # set URL on list/create (_workitems/edit address)
internal/forge/forgetest/conformance.go  # assert URL per provider
internal/reconcile/reconcile.go     # Result.Divergences; recordBackflowConflict returns status + keeps artifact; remove pre-artifact warning; ActionReportDivergence appends KindOther
internal/reconcile/report.go        # NEW: DivergenceReport, ConflictGuidance, ArtifactStatus; summary-surface render
internal/reconcile/logging.go       # annotationHandler applies textsafe.LogCommand to every message (annotation + plain copy)
internal/reconcile/render.go        # RenderConflictSections(w, reports, budget)
internal/cli/summary.go             # NEW: summaryTarget + shared Azure command-data escaper (plan, notification, conflict)
internal/cli/notification_summary.go# use summaryTarget (fixes unescaped upload path)
internal/cli/wiring.go              # writeStepSummary uses summaryTarget
internal/cli/reconcile.go           # after Apply: write sections → emit conflict warnings → build exit-3 message
tools/checkcoverage/main.go         # -prefix / -min flags (coverage floor gate for internal/textsafe)
Taskfile.yml                        # textsafe:coverage, textsafe:fuzz
.github/workflows/ci.yml            # run textsafe:coverage
docs/guides/backflow.md, github-action.md, azure-pipelines.md, operating.md
docs/reference/templates.md, configuration.md (exit codes), docs/architecture.md
```

**Structure Decision**: single Go module; the feature adds one leaf package (`internal/textsafe`, no internal imports) and otherwise changes existing packages along the apply → CLI path. depguard's engine rule is unaffected.

## Design Overview

1. **Record (reconcile)**: `recordBackflowConflict` keeps the `forge.ConflictArtifact` it creates or finds (including the canonical one after consolidation) and returns an `ArtifactStatus`. `applyBackflow` builds a `ConflictGuidance` (contained subject, strategy, counts, status, address, summary-surface body) and appends a `KindConflict` report. The pre-artifact `Warn` is removed. The `ActionReportDivergence` arm appends a `KindOther` report.
2. **Address (forge)**: providers fill `ConflictArtifact.URL` (R3).
3. **Render (tmpl)**: the artifact render passes `Surface: "artifact"` and `Subject(raw)`. For the summary, a custom template that reads commit text (`Set.ConflictBodyReadsCommitText`, decided from the parse tree at load) is replaced by the built-in body plus a note; the built-in body gets `Surface: "summary"` and `Markdown(Subject(raw))`, which is safe because it only prints the subject. A custom template that never reads commit text renders as written with `Surface: "summary"` (R4, R5, FR-016).
4. **Publish (cli)**, after `Apply` returns (with or without error), in this order:
   1. after the notification diagnostics and `writeNotificationSummary` (so the conflict sections are the last summary write and the R8 budget holds), write conflict sections to the summary target, if any (R7, R8); remember whether they were written;
   2. emit one conflict warning per report through the logger (contract §1), choosing the summary or guide pointer by step 1's outcome (FR-010);
   3. if `applyErr != nil`, return it (exit 1, no exit-3 output); else if `res.Divergence`, return `exitCodeError{3, msg}` with the exit-3 message (contract §3).
5. **Neutralize (logging)**: the annotation handler applies `textsafe.LogCommand` to every warning message before both outputs (R6).

## Risks

| Risk | Mitigation |
| --- | --- |
| Moving the warning later breaks tests or log-scrapers that expect it mid-apply | Text format unchanged; tests updated; documented in the backflow guide. Log text is not a contract. |
| Contained text renders differently on Azure DevOps vs GitHub | `Markdown` uses look-alike substitution (no backslash escapes or entities, which renderers treat differently and which fail inside code spans); only Oiax-controlled text prints commit text, and custom templates that read it fall back to the built-in body; corpus tests plus one live render per host (quickstart §5). |
| Custom templates that ignore `.Surface` show artifact-only wording in the summary | Accepted by the spec (FR-003); the templates reference documents `.Surface` with an example. |
| Binary scenario hits the clock flake (#143) | Land after #153. |

## Complexity Tracking

No constitution violations to justify.
