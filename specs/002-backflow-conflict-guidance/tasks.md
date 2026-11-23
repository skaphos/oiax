# Tasks: Backflow Conflict Guidance on the Failing Run

**Input**: Design documents from `specs/002-backflow-conflict-guidance/` ([plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md))

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Test tasks are MANDATORY in this repository. The constitution's Engineering Constraints make regression tests non-negotiable for every bug fix, require `-race -shuffle=on`, and gate on a coverage floor landed in the same change. Untrusted-input handling gets fuzz targets. Within each story, write the tests first and confirm they fail before implementing.

**Organization**: Tasks are grouped by user story. US1 and US2 are both P1; the spec states US1 cannot ship without US2, so the MVP is US1 + US2.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: US1–US5 from spec.md
- Paths are repository-relative. Run Go with `GOTOOLCHAIN=local`; tasks with `go -C tools tool task <name>`.

## Path Conventions

Single Go module. New package `internal/textsafe/`; otherwise changes in `internal/tmpl`, `internal/forge/...`, `internal/reconcile`, `internal/cli`, and `docs/`.

---

## Phase 1: Setup

**Purpose**: confirm the base and create the new package.

- [ ] T001 Merge current `main` into `spec/backflow-conflict-guidance` (or branch `feat/backflow-conflict-guidance` from it) and confirm `go test -race -shuffle=on ./...` and `go -C tools tool task lint` pass before any change; if #153 (notification fixture clock fix, #143) is merged, include it
- [ ] T002 Create package `internal/textsafe/` with `doc.go` describing its purpose (containing untrusted text per surface; no internal imports) and an empty `textsafe.go`, matching the layout in plan.md

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: shared types and helpers every story uses. See [contracts/contained-text.md](contracts/contained-text.md), [contracts/forge-and-templates.md](contracts/forge-and-templates.md), [data-model.md](data-model.md).

**⚠️ CRITICAL**: no user story work begins until this phase is complete.

### Tests for Foundational (MANDATORY) ⚠️

- [ ] T003 [P] Write table tests for `Subject` in `internal/textsafe/textsafe_test.go`: valid UTF-8 output, first line only, control and `Cf` (bidi, U+2060) removed, 200-rune cap with `…`, idempotence (`Subject(Subject(s)) == Subject(s)`)
- [ ] T004 [P] Write table tests for `LogCommand`, `PlainLink`, `Markdown` in `internal/textsafe/textsafe_test.go` asserting the properties in contracts/contained-text.md (no `##[`, `##vso[` any case, or `::`; no `://` or word-initial `www.`; Markdown punctuation backslash-escaped, `<` and `&` entity-escaped, U+2060 after `@` and inside `://`/`www.`)
- [ ] T005 [P] Write a test in `internal/tmpl/tmpl_test.go` that renders the built-in conflict pair with `Surface: "artifact"` and `Surface: "summary"`: the artifact body ends with the forge-neutral closing sentence ("Oiax manages this conflict record. It closes automatically … Do not edit the metadata block below."), the summary body does not contain it; update `TestDefaultConflictBody` golden text for the new closing sentence
- [ ] T006 [P] Write a test in `internal/tmpl/tmpl_test.go` that `Resolve` rejects a custom `backflowConflict` template whose body fails to render only when `.Surface` is `"summary"` (sample render covers both surfaces)
- [ ] T007 [P] Extend `testConflictLifecycle` in `internal/forge/forgetest/conformance.go` to assert every created and listed `ConflictArtifact.URL` equals an expected-address function supplied by the runner; update `internal/forge/github/conformance_test.go` (`https://github.com/acme/widgets/issues/<n>`) and `internal/forge/azuredevops/conformance_test.go` (`https://dev.azure.com/<org>/<project>/_workitems/edit/<id>`, using a project name with a space to prove escaping)
- [ ] T008 [P] Write tests in `internal/cli/summary_test.go` for `summaryTarget`: GitHub appends to `$GITHUB_STEP_SUMMARY`; Azure writes a temp file in `$AGENT_TEMPDIRECTORY` and `finish` prints `##vso[task.uploadsummary]` with the path escaped (`%`→`%AZP25`, CR→`%0D`, LF→`%0A`); neither host returns `ok == false`

### Implementation for Foundational

- [ ] T009 [P] Implement `Subject`, `LogCommand`, `PlainLink`, `Markdown` in `internal/textsafe/textsafe.go` per contracts/contained-text.md, using U+2060 WORD JOINER to break sequences (research R5) and `notification.CleanText` semantics as the precedent for control/`Cf` removal (do not import `internal/notification`; reimplement)
- [ ] T010 [P] Add `Surface string` to `tmpl.Context` in `internal/tmpl/tmpl.go`; wrap the built-in conflict body's closing sentence in `{{if eq .Surface "artifact"}}…{{end}}` with forge-neutral wording; make `sampleRender` render the conflict pair with both surfaces; set `Surface: "artifact"` wherever existing renders build a `Context` (promotion, backflow, merge message, conflict artifact) in `internal/reconcile/reconcile.go`
- [ ] T011 [P] Add `URL string` to `forge.ConflictArtifact` in `internal/forge/forge.go` with a doc comment (forge web page built from resolved identity and ID, never from forge text)
- [ ] T012 [P] Set `ConflictArtifact.URL` on list and create in `internal/forge/github/github.go` using `"https://" + gitRemoteHost + "/" + url.PathEscape(p.Owner) + "/" + url.PathEscape(p.Repo) + "/issues/" + number` (precedent: `internal/forge/github/notifications.go` request link)
- [ ] T013 [P] Set `ConflictArtifact.URL` on list and create in `internal/forge/azuredevops/artifacts.go` using `"https://dev.azure.com/" + url.PathEscape(p.Repo.Organization) + "/" + url.PathEscape(p.Repo.Project) + "/_workitems/edit/" + id` (precedent: `internal/forge/azuredevops/notifications.go` PR link; project name per research R3)
- [ ] T014 Create `internal/reconcile/report.go` with `DivergenceReport`, `DivergenceKind` (`KindConflict`, `KindOther`), `ConflictGuidance`, `ArtifactStatus` (`ArtifactKnown`, `ArtifactUnhostable`, `ArtifactNotRecorded`) per data-model.md; add `Divergences []DivergenceReport` to `Result` in `internal/reconcile/reconcile.go` with the invariant `Divergence == (len(Divergences) > 0)` documented
- [ ] T015 Create `internal/cli/summary.go` with `summaryTarget(cmd) (w io.WriteCloser, finish func() error, ok bool)` and one Azure command-data escaper (research R7); refactor `writeGitHubSummary`/`writeAzureSummary` in `internal/cli/wiring.go` to use it with no behavior change for the plan summary
- [ ] T016 Run `go test -race -shuffle=on ./internal/textsafe/ ./internal/tmpl/ ./internal/forge/... ./internal/cli/` and confirm T003–T008 now pass

**Checkpoint**: foundation ready. Stories can begin.

---

## Phase 3: User Story 1 - Find the resolution path from a failed GitHub Actions run (Priority: P1) 🎯 MVP (with US2)

**Goal**: on GitHub Actions with Issues enabled, a conflicting backflow edge produces one warning ending with the issue address, one summary section, and an exit-3 message naming the edge and address (spec FR-001 to FR-007, FR-009, FR-011).

**Independent Test**: GitHub fixture with Issues enabled and a conflicting hotfix; run `reconcile` with `GITHUB_ACTIONS=true` and a `GITHUB_STEP_SUMMARY` file; inspect warning, summary, exit code (quickstart §2).

### Tests for User Story 1 (MANDATORY) ⚠️

- [ ] T017 [P] [US1] In `internal/reconcile/reconcile_test.go`, write one test per artifact path asserting the resulting `DivergenceReport` (kind conflict, status `ArtifactKnown`, `ArtifactURL` equal to the fake's address for the right ID): created (`TestApplyBackflowConflictCreatesArtifact` path), adopted (same head), advanced, advance failed (`updateConflictErr`), left alone (staler run; shallow clone; ancestry error), canonical after consolidation (lowest ID); extend `fakeForge` in the same file so `CreateConflictArtifact`/`ListConflictArtifacts` return artifacts with `URL` set
- [ ] T018 [P] [US1] In `internal/reconcile/reconcile_test.go`, assert the conflict path no longer logs the `halted on … conflict` warning during apply (it moves to the CLI) and that `ConflictGuidance` carries `Strategy`, `SHA`, contained `Subject`, `Applied`/`Whole`, and a non-empty `SummaryBody` rendered with `Surface: "summary"`, for both cherry-pick and merge strategies
- [ ] T019 [P] [US1] In `internal/reconcile/render_test.go`, write golden tests for `RenderConflictSections` per contracts/surfaces.md §2: cherry-pick section (applied count), merge section (whole-set note and `git merge --no-ff` guidance), artifact link rendered as `[issue <n>](<address>)`, guide link, branch names with `|`, `` ` ``, `*`, `_`, `<`, `@` escaped
- [ ] T020 [P] [US1] In `internal/cli/plan_reconcile_test.go`, extend the CLI `fakeForge` conflict stubs to return artifacts with IDs and URLs, then write `TestReconcileConflictGitHubSurfaces` (`GITHUB_ACTIONS=true`, temp `GITHUB_STEP_SUMMARY`): stderr has exactly one `::warning::backflow … halted on cherry-pick conflict … Resolution steps: <address>`; the summary file has the plan summary followed by one `### Backflow conflict:` section; stderr ends with `converged with reported divergence on 1 edge(s):` and the edge line with the address; exit code 3
- [ ] T021 [P] [US1] In `internal/cli/execute_test.go`, update `TestExecuteDivergenceMessage` for the multi-line exit-3 message: first line still begins `converged with reported divergence`

### Implementation for User Story 1

- [ ] T022 [US1] In `internal/reconcile/reconcile.go`, change `recordBackflowConflict` to keep the `forge.ConflictArtifact` from `CreateConflictArtifact` (no longer discarded) and the canonical `existing` on every re-found path, and to return an `ArtifactStatus` plus the artifact; on a render failure after list, report `ArtifactKnown` when `existing` is known (spec FR-007)
- [ ] T023 [US1] In `internal/reconcile/reconcile.go` (`applyBackflow`, merge and cherry-pick conflict branches), remove the pre-artifact `c.log().Warn(... halted on ...)` calls; after `recordBackflowConflict`, build a `ConflictGuidance` (subject via `textsafe.Subject`) and append a `KindConflict` `DivergenceReport` to `res.Divergences`
- [ ] T024 [US1] In `internal/reconcile/reconcile.go` (`recordBackflowConflict` render) and `internal/reconcile/report.go`, render the artifact with `Surface: "artifact"` and `tmpl.Conflict.Subject = textsafe.Subject(raw)` (spec FR-013), and render the summary body once with `Surface: "summary"` and `textsafe.Markdown(textsafe.Subject(raw))` for `.Conflict.Subject` and every `.Commits[].Subject` (spec FR-016); set `RenderFailed` on a summary render error
- [ ] T025 [US1] Implement `RenderConflictSections(w io.Writer, reports []DivergenceReport, budget int) (written bool, err error)` in `internal/reconcile/render.go` per contracts/surfaces.md §2, using `textsafe.Markdown` for branch names in the header
- [ ] T026 [US1] In `internal/cli/reconcile.go`, after `coord.Apply` returns: (1) write conflict sections through `summaryTarget` with a 1 MiB budget minus bytes already written (research R8) and record whether they were written; (2) emit one conflict warning per `KindConflict` report through the logger, leading text per contracts/surfaces.md §1 with the subject passed through `textsafe.PlainLink`, ending with `. Resolution steps: <address>` for known artifacts; (3) return `applyErr` if set; (4) otherwise build the exit-3 message (contracts/surfaces.md §3) and return `exitCodeError{code: 3, msg: ...}`
- [ ] T027 [US1] Add the exit-3 message builder in `internal/cli/reconcile.go` (or `internal/cli/divergence_message.go`): first line `converged with reported divergence on N edge(s):`, one line per report in order, trusted text only (spec FR-010)
- [ ] T028 [US1] Run `go test -race -shuffle=on ./internal/reconcile/ ./internal/cli/` and update existing conflict tests that asserted the old in-apply warning position (`TestApplyBackflowConflictUnsupportedForgeSkipsArtifact` log assertions, `TestReconcileJSONAnnotationNotOnStdout`) to the new CLI emission

**Checkpoint**: US1 works on GitHub Actions. It must not ship before US2.

---

## Phase 4: User Story 2 - Hostile commit subjects stay inert (Priority: P1) 🎯 MVP (with US1)

**Goal**: no commit subject can add CI commands, forge warnings, links, HTML, mentions, or Markdown structure on any surface (spec FR-013 to FR-016, SC-004), and the annotation handler neutralizes host commands in every warning (research R6).

**Independent Test**: run the conflict scenario with each subject in the adversarial corpus on both hosts; inspect processed commands and rendered summary (quickstart §4).

### Tests for User Story 2 (MANDATORY) ⚠️

- [ ] T029 [P] [US2] Create `internal/textsafe/testdata/adversarial.txt` with at least 50 subjects, including every User Story 2 vector: `::error::x`, `%0A::warning::x`, `##[error]x`, `##[add-mask]development`, `##[stop-commands]t`, `##vso[task.setvariable variable=OIAX_INJECTED]1`, `##vso[task.complete result=Succeeded]`, `[Resolution steps](https://attacker.example)`, a bare URL, `www.attacker.example`, `<img src=x>`, `@org/team`, `| a | b |`, `# h`, stray backticks, a 500-rune subject with control and bidi characters; plus case variants (`##VSO[`), attacker-supplied U+2060, and mixed vectors
- [ ] T030 [P] [US2] Write a corpus test in `internal/textsafe/corpus_test.go` asserting, for every corpus line, each function's contract property (contracts/contained-text.md); for `Markdown`, parse the output with a minimal CommonMark inline check (no `[`/`](`/`<`/`*`/`` ` `` unescaped, no line break) and assert only literal text remains
- [ ] T031 [P] [US2] Add fuzz targets `FuzzSubject`, `FuzzLogCommand`, `FuzzMarkdown`, `FuzzPlainLink` in `internal/textsafe/fuzz_test.go` seeded from the corpus, asserting the same properties; commit any fuzzer-found input to `internal/textsafe/testdata/fuzz/`
- [ ] T032 [P] [US2] In `internal/reconcile/logging_test.go`, add regression tests that a warning message containing mid-line `##vso[task.setvariable ...]`, `##[error]`, and `::warning::` is neutralized in both the GitHub annotation, the Azure annotation, and the plain log copy (the existing `TestAzureAnnotationEscapesLoggingCommandChars` input leaves `##vso[` intact; these tests fail before T034)
- [ ] T033 [P] [US2] In `internal/cli/plan_reconcile_test.go`, write `TestReconcileConflictAdversarialSubjects`: for each corpus subject, run the US1 GitHub scenario and the Azure scenario (`TF_BUILD=True`) and assert stderr contains exactly the expected Oiax commands (no extra `::`/`##vso[`/`##[` command lines), and the summary file contains no Markdown link, image, HTML tag, or mention derived from the subject

### Implementation for User Story 2

- [ ] T034 [US2] In `internal/reconcile/logging.go`, apply `textsafe.LogCommand` to the record message in `annotationHandler.Handle` before both `formatAnnotation` and the base handler, so annotations and the plain stderr copy are neutralized for every warning (research R6)
- [ ] T035 [US2] Verify T024 and T026 apply `textsafe.Subject` before every surface function and `textsafe.PlainLink` to the warning subject; fix any path the corpus test (T033) shows leaking
- [ ] T036 [US2] Run `go test -race -shuffle=on ./internal/textsafe/ ./internal/reconcile/ ./internal/cli/` and each fuzz target for 60s (`go test -run XXX -fuzz <Target> -fuzztime 60s ./internal/textsafe/`); confirm `internal/textsafe` statement coverage ≥ 95%

**Checkpoint**: MVP (US1 + US2) is shippable.

---

## Phase 5: User Story 3 - The same guidance on Azure Pipelines (Priority: P2)

**Goal**: identical guidance under Azure Pipelines, with Azure Repos (Boards work item) or GitHub (issue) as the forge (spec FR-012, US3).

**Independent Test**: run the US1 fixture under Azure Pipelines, once with Azure Repos and once with GitHub (quickstart §3).

### Tests for User Story 3 (MANDATORY) ⚠️

- [ ] T037 [P] [US3] In `internal/cli/plan_reconcile_test.go`, write `TestReconcileConflictAzureSurfaces` (`TF_BUILD=True`, temp `AGENT_TEMPDIRECTORY`): one `##vso[task.logissue type=warning]` conflict warning ending with the address, one `##vso[task.uploadsummary]` for a file containing the conflict section, exit code 3; run once with a GitHub-forge fake (issue address) and once with an Azure-forge fake (work item address and noun), and assert the sections differ only in artifact noun and address (spec US3 AS2)
- [ ] T038 [P] [US3] In `internal/cli/notification_summary_test.go`, add the regression test for the existing bug: with `AGENT_TEMPDIRECTORY` containing `%` and a newline, the notification summary's `##vso[task.uploadsummary]` path is escaped (`%AZP25`, `%0A`); it fails before T040

### Implementation for User Story 3

- [ ] T039 [US3] In `internal/reconcile/report.go`, set `ConflictGuidance.ArtifactNoun` from the forge kind (`issue` for GitHub, `work item` for Azure DevOps), passed in from `internal/cli/wiring.go` `buildCoordinator`; use it in the summary section and warnings
- [ ] T040 [US3] In `internal/cli/notification_summary.go`, replace the hand-rolled temp-file and unescaped `##vso[task.uploadsummary]` with `summaryTarget` (spec FR-012, research R7)
- [ ] T041 [US3] Run `go test -race -shuffle=on ./internal/cli/` and confirm T037 and T038 pass

**Checkpoint**: US1–US3 work on both hosts and both forges.

---

## Phase 6: User Story 4 - No artifact can be hosted or recorded (Priority: P2)

**Goal**: when the repository cannot host the artifact (Issues disabled) or Oiax could not record it this run, every surface says so and still gives the steps; nothing points at a summary that was not written (spec FR-008, FR-010, US4).

**Independent Test**: GitHub fixture with Issues disabled, plus fault injection on artifact list and create; also with no summary destination (quickstart §2 last paragraph).

### Tests for User Story 4 (MANDATORY) ⚠️

- [ ] T042 [P] [US4] In `internal/reconcile/reconcile_test.go`, assert `ArtifactUnhostable` when `conflictArtifactsUnsupported` is set (no list or create attempted, existing #69 warning unchanged), and `ArtifactNotRecorded` for `listConflictsErr`, `createConflictErr`, and a template render failure with no existing artifact; `ArtifactURL` empty in all three
- [ ] T043 [P] [US4] In `internal/reconcile/render_test.go`, golden-test the unhostable section (only-guidance statement, retention statement, link to `docs/guides/operating.md#keep-issues-enabled-github-backflow-only`) and the not-recorded section (retried next run), and the render-failure section (FR-005 note instead of body)
- [ ] T044 [P] [US4] In `internal/cli/plan_reconcile_test.go`, write `TestReconcileConflictNoArtifactPointers`: for unhostable and not-recorded, with a summary written the warning and exit-3 line say "resolution steps are in this run's summary"; with `GITHUB_STEP_SUMMARY` unset (local run) and with an unwritable summary path, neither mentions a summary and both point to the backflow guide address (spec FR-010, US4 AS3)

### Implementation for User Story 4

- [ ] T045 [US4] In `internal/reconcile/reconcile.go`, return `ArtifactUnhostable` from the capability gate and `ArtifactNotRecorded` from the list, create, and render-without-existing failure paths of `recordBackflowConflict`
- [ ] T046 [US4] In `internal/reconcile/render.go` and `internal/cli/reconcile.go`, implement the FR-008 statements and the summary-written/not-written pointer selection per contracts/surfaces.md §1–§3

**Checkpoint**: US1–US4 complete.

---

## Phase 7: User Story 5 - Several conflicting edges in one run (Priority: P3)

**Goal**: N conflicting edges give N warnings, N sections, and N exit-3 lines in plan order; other divergences are named in exit-3 output only (spec FR-009, FR-011, US5, SC-006).

**Independent Test**: graph with two conflicting backflow sources plus one other reported divergence; repeat the run.

### Tests for User Story 5 (MANDATORY) ⚠️

- [ ] T047 [P] [US5] In `internal/reconcile/reconcile_test.go`, assert two conflicting backflow sources yield two `KindConflict` reports in plan action order, and an `ActionReportDivergence` on another edge yields one `KindOther` report; repeat 20 times and assert identical order (SC-006)
- [ ] T048 [P] [US5] In `internal/cli/plan_reconcile_test.go`, write `TestReconcileMultipleConflictsOrdered`: two warnings, two self-contained sections, and an exit-3 message naming both conflict edges with their own pointers plus `<edge>: reported divergence` for the other, in plan order
- [ ] T049 [P] [US5] In `internal/reconcile/render_test.go`, test the size budget: with a budget smaller than the full sections, later sections are reduced to header items and the truncation note appears once (spec FR-005, research R8)

### Implementation for User Story 5

- [ ] T050 [US5] In `internal/reconcile/reconcile.go`, append a `KindOther` `DivergenceReport` in the `ActionReportDivergence` arm, and ensure a single report per edge (a conflict and a report-divergence on the same edge yield one `KindConflict`)
- [ ] T051 [US5] In `internal/reconcile/render.go`, implement the budget reduction and single truncation note in `RenderConflictSections`

**Checkpoint**: all user stories complete.

---

## Phase 8: Polish & Cross-Cutting Concerns

- [ ] T052 [P] Extend the notification-binary fixture (`internal/cli/notification_merge_test.go`, `internal/cli/testdata/notification-binary/forge.go`) with a conflicting-hotfix scenario run under `GITHUB_ACTIONS=true` with a summary file, asserting the warning, section, and exit 3 from the built binary (research R10); requires #153 merged
- [ ] T053 [P] Update `docs/guides/backflow.md` "When a replay conflicts": new warning text and pointer, the summary section, the exit-3 lines, the warning now emitted after apply, and the Issues-disabled case
- [ ] T054 [P] Update `docs/guides/github-action.md` and `docs/guides/azure-pipelines.md` "Step summary and annotations" for the conflict sections and warnings
- [ ] T055 [P] Update `docs/guides/operating.md` ("Exit codes and CI gating", "When Oiax reports a divergence", "Keep Issues enabled") and `docs/reference/configuration.md` "Exit codes" for the multi-line exit-3 message (exit code unchanged)
- [ ] T056 [P] Update `docs/reference/templates.md`: add `.Surface` with an example of guarding artifact-only text, document `.Conflict.SquashCommits`, and state that `.Conflict.Subject` is capped at 200 runes with control and bidi characters removed
- [ ] T057 [P] Update `docs/architecture.md` annotation and summary paragraphs for the post-apply conflict surfaces and the log-command neutralization
- [ ] T058 If the `reconcile` help text changed, regenerate `docs/reference/cli.md` with `go -C tools tool task docs:cli-ref` and confirm `go -C tools tool task verify-generated` is clean
- [ ] T059 Run the full gate: `go test -race -shuffle=on ./...`, `go -C tools tool task lint`, `go -C tools tool task staticcheck`, `go -C tools tool task vuln`, `go -C tools tool task verify-generated`; confirm the coverage floor (textsafe ≥ 95%, no touched package below its current floor)
- [ ] T060 Walk through quickstart.md §1–§4 and record results in the PR description; list quickstart §5 (live renders on both hosts, URL clickability, SC-002) as post-merge live acceptance

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)** → **Foundational (Phase 2)** → user stories.
- **US1 (Phase 3)** depends on Phase 2.
- **US2 (Phase 4)** depends on Phase 2; its corpus tests (T033) exercise US1's surfaces, so run T033 after T026. US1 must not ship without US2.
- **US3 (Phase 5)** depends on Phase 2 and on US1 (it reuses the CLI flow); T038/T040 (notification summary fix) depend only on T015.
- **US4 (Phase 6)** depends on US1 (status plumbing and pointers).
- **US5 (Phase 7)** depends on US1.
- **Polish (Phase 8)** after the stories it documents; T052 also requires #153.

### Story completion order

```text
Setup → Foundational → US1 ─┬─> US2 ─> (MVP ship point)
                            ├─> US3
                            ├─> US4
                            └─> US5
                                      └─> Polish
```

### Within each story

Tests first (they must fail), then implementation, then the story's test run.

## Parallel Opportunities

- **Phase 2**: T003–T008 (tests) in parallel; T009–T013 in parallel (different files); T014 and T015 after.
- **US1**: T017–T021 in parallel; T022 → T023 → T024 sequential (same file); T025 in parallel with T022–T024; T026–T027 after T025.
- **US2**: T029–T032 in parallel; T034 independent of US1 implementation.
- **US3 / US4 / US5**: after US1, the three stories can proceed in parallel; within each, test tasks are parallel.
- **Polish**: T053–T057 in parallel.

### Example: Foundational in parallel

```text
Task: "T003 Subject tests in internal/textsafe/textsafe_test.go"
Task: "T005 Surface tests in internal/tmpl/tmpl_test.go"
Task: "T007 URL conformance in internal/forge/forgetest/conformance.go"
Task: "T008 summaryTarget tests in internal/cli/summary_test.go"
```

### Example: US2 in parallel

```text
Task: "T029 adversarial corpus in internal/textsafe/testdata/adversarial.txt"
Task: "T031 fuzz targets in internal/textsafe/fuzz_test.go"
Task: "T032 log-command regression tests in internal/reconcile/logging_test.go"
```

## Implementation Strategy

### MVP first (US1 + US2)

1. Phase 1 → Phase 2.
2. US1 (GitHub end to end), then US2 (containment and the logging fix).
3. **Stop and validate**: quickstart §2 and §4 on GitHub. This is the shippable increment: it closes the field incident in #107 safely.

### Incremental delivery

- US3 adds Azure parity and fixes the notification-summary upload path.
- US4 covers Issues-disabled and failure cases.
- US5 hardens ordering and size limits.
- Each story ends at a checkpoint where the full test suite passes; Polish lands docs, the binary test, and the full gate.

## Notes

- Commit after each task or logical group; DCO sign-off as `Shawn Stratton <shawn@skaphos.io>`; Conventional Commits (`feat(reconcile): …`, `fix(cli): …` for the notification path, `test(...)`, `docs(...)`).
- No exit code, JSON plan, request metadata, artifact identity, or forge-write change (spec FR-017): any task that seems to need one is out of scope; stop and raise it.
