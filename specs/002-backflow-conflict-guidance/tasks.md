# Tasks: Backflow Conflict Guidance on the Failing Run

**Input**: Design documents from `specs/002-backflow-conflict-guidance/` ([plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md))

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Test tasks are MANDATORY in this repository. The constitution's Engineering Constraints make regression tests non-negotiable for every bug fix, require `-race -shuffle=on`, and gate on a coverage floor landed in the same change. Untrusted-input handling gets fuzz targets. Within each story, write the tests first and confirm they fail before implementing.

**Organization**: Tasks are grouped by user story. US1 and US2 are both P1; the spec states US1 cannot ship without US2, so the MVP is US1 + US2.

**Revision**: updated 2026-11-22 after `/speckit-analyze` (findings D1, C1–C4, I1–I3, U1–U3, G1, A1).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: US1–US5 from spec.md
- Paths are repository-relative. Run Go with `GOTOOLCHAIN=local`; tasks with `go -C tools tool task <name>`.

## Path Conventions

Single Go module. New package `internal/textsafe/`; otherwise changes in `internal/tmpl`, `internal/forge/...`, `internal/reconcile`, `internal/cli`, `tools/checkcoverage`, `Taskfile.yml`, `.github/workflows/ci.yml`, and `docs/`.

---

## Phase 1: Setup

**Purpose**: confirm the base and create the new package.

- [X] T001 Merge the docs-only spec PR #126 to `main` first, then create the implementation branch `feat/backflow-conflict-guidance` from `main` (which must include #153, the notification fixture clock fix for #143); confirm `go test -race -shuffle=on ./...` and `go -C tools tool task lint` pass before any change
- [X] T002 Create package `internal/textsafe/` with `doc.go` describing its purpose (containing untrusted text per surface; no internal imports) and an empty `textsafe.go`, matching the layout in plan.md

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: shared types, helpers, and gates every story uses. See [contracts/contained-text.md](contracts/contained-text.md), [contracts/forge-and-templates.md](contracts/forge-and-templates.md), [data-model.md](data-model.md).

**⚠️ CRITICAL**: no user story work begins until this phase is complete.

### Tests for Foundational (MANDATORY) ⚠️

- [X] T003 [P] Write table tests for `Subject` in `internal/textsafe/textsafe_test.go`: valid UTF-8 output, first line only, control and `Cf` (bidi, U+2060) removed, cap of 200 runes in total including the `…` (a 500-rune input yields exactly 199 runes plus `…`), idempotence (`Subject(Subject(s)) == Subject(s)`)
- [X] T004 Write table tests for `LogCommand`, `PlainLink`, `Markdown` in `internal/textsafe/textsafe_test.go` asserting the properties in contracts/contained-text.md: no `##[`, `##vso[` any case, or `::`; no `://` or word-initial `www.`; `Markdown` output contains none of `` ` * _ [ ] ( ) < > # | ! ~ \ @ & = `` (each replaced by its look-alike per the contract table), no line-leading `-`, `+`, or digit-plus-`.`, and `===`/`---` inputs cannot form a Setext underline
- [X] T005 [P] Write a test in `internal/tmpl/tmpl_test.go` that renders the built-in conflict pair with `Surface: "artifact"` and `Surface: "summary"`: the artifact body ends with the forge-neutral closing sentence ("Oiax manages this conflict record. It closes automatically … Do not edit the metadata block below."), the summary body does not contain it; update `TestDefaultConflictBody` golden text for the new closing sentence; and render the complete built-in summary section with `From` set to ``release/`<b>`x`` and `To` set to `dev$(id)`: outside the `sh` fence the output contains no `<b>` and no ASCII backtick from the branch, and inside the fence the commands carry `'release/`<b>`x'` and `'dev$(id)'` literally; with `development`/`main` the artifact and summary text is unchanged from today apart from the surface sentence
- [X] T006 Write a test in `internal/tmpl/tmpl_test.go` that `Resolve` rejects a custom `backflowConflict` template whose body fails to render only when `.Surface` is `"summary"` (sample render covers both surfaces)
- [X] T007 Write template-gate tests in `internal/tmpl/tmpl_test.go` for `Set.ConflictBodyReadsCommitText` (spec FR-016, SC-003). **True** (summary falls back) for: `{{.Conflict.Subject}}`; `{{printf "%#v" .Conflict.Subject}}`; `{{if eq (printf "%s" .Conflict.Subject) "*"}}x{{end}}`; `{{if eq .Conflict.Subject "attacker.example"}}[help](https://{{.Conflict.Subject}}){{end}}`; `{{trunc 10 .Conflict.Subject}}`; `{{range .Commits}}{{.Subject}}{{end}}`; `{{printf "%v" .Conflict}}`, `{{.}}`, `{{$}}`, `{{template "x" .}}`, and a `define`/`block`. **False** (used as written) for: the built-in body, a body reading only `.Conflict.SHA`, `.Conflict.Applied`, `.Conflict.Whole`, `{{range .Commits}}{{.ShortSHA}}{{end}}`, `{{len .Commits}}`, and `{{if eq .Surface "artifact"}}`. Also: (a) a gate-false custom body guarding a sentence with `{{if eq .Surface "artifact"}}` renders identically on both surfaces except for that sentence; (b) a gate-false custom body that never references `.Surface` renders identically on both surfaces
- [X] T008 [P] Extend `testConflictLifecycle` in `internal/forge/forgetest/conformance.go` to assert every created and listed `ConflictArtifact.URL` equals an expected-address function supplied by the runner; update `internal/forge/github/conformance_test.go` (`https://github.com/acme/widgets/issues/<n>`) and `internal/forge/azuredevops/conformance_test.go` (`https://dev.azure.com/<org>/<project>/_workitems/edit/<id>`, using a project name with a space to prove escaping)
- [X] T009 [P] Write tests in `internal/cli/summary_test.go` for `summaryTarget`: GitHub appends to `$GITHUB_STEP_SUMMARY`; Azure writes a temp file in `$AGENT_TEMPDIRECTORY` and `finish` prints `##vso[task.uploadsummary]` with the path escaped (`%`→`%AZP25`, CR→`%0D`, LF→`%0A`); neither host returns `ok == false`
- [X] T010 [P] Write tests in `tools/checkcoverage/main_test.go` for new `-prefix` and `-min` flags: defaults keep today's behavior (`github.com/skaphos/oiax/v2/internal/notification`, 85%); with `-prefix github.com/skaphos/oiax/v2/internal/textsafe -min 95` a profile at 94.9% fails and at 95.0% passes

### Implementation for Foundational

- [X] T011 [P] Implement `Subject` (200 runes total including `…`), `LogCommand`, `PlainLink`, `Markdown` (look-alike substitution per the contract table, including `=` and line-leading markers) in `internal/textsafe/textsafe.go` per contracts/contained-text.md, using U+2060 WORD JOINER to break log and plain-text sequences (research R5) and `notification.CleanText` semantics as the precedent for control/`Cf` removal (do not import `internal/notification`; reimplement)
- [X] T012 [P] In `internal/tmpl/tmpl.go`: add `Surface string` to `tmpl.Context`; add `Set.ConflictBodyReadsCommitText() bool`, computed once in `Resolve` by walking the conflict body's `text/template/parse` tree per contracts/forge-and-templates.md "Summary template gate" (conservative: any `Subject` field chain, whole-value use of `.Conflict`/`.Commits`/a commit element/`.`/`$`, `define`/`template`/`block`, or an unrecognized node counts as reading commit text); add the `text` and `shellArg` template funcs and use them for branch names in the built-in conflict body per contracts/forge-and-templates.md; wrap the built-in conflict body's closing sentence in `{{if eq .Surface "artifact"}}…{{end}}` with forge-neutral wording; make `sampleRender` render the conflict pair with both surfaces; set `Surface: "artifact"` wherever existing renders build a `Context` (promotion, backflow, merge message, conflict artifact) in `internal/reconcile/reconcile.go`. `Conflict.Subject` and `Commit.Subject` stay `string`
- [X] T013 [P] Add `URL string` to `forge.ConflictArtifact` in `internal/forge/forge.go` with a doc comment (forge web page built from resolved identity and ID, never from forge text)
- [X] T014 [P] Set `ConflictArtifact.URL` on list and create in `internal/forge/github/github.go` using `"https://" + gitRemoteHost + "/" + url.PathEscape(p.Owner) + "/" + url.PathEscape(p.Repo) + "/issues/" + number` (precedent: `internal/forge/github/notifications.go` request link)
- [X] T015 [P] Set `ConflictArtifact.URL` on list and create in `internal/forge/azuredevops/artifacts.go` using `"https://dev.azure.com/" + url.PathEscape(p.Repo.Organization) + "/" + url.PathEscape(p.Repo.Project) + "/_workitems/edit/" + id` (precedent: `internal/forge/azuredevops/notifications.go` PR link; project name per research R3)
- [X] T016 Create `internal/reconcile/report.go` with `DivergenceReport`, `DivergenceKind` (`KindConflict`, `KindOther`), `ConflictGuidance` (including `ArtifactNoun`), `ArtifactStatus` (`ArtifactKnown`, `ArtifactUnhostable`, `ArtifactNotRecorded`) per data-model.md; add `Divergences []DivergenceReport` to `Result` in `internal/reconcile/reconcile.go` with the invariant `Divergence == (len(Divergences) > 0)` documented
- [X] T017 Add an `ArtifactNoun string` field to `reconcile.Coordinator` (`"issue"` for the GitHub forge, `"work item"` for Azure DevOps) and set it in `internal/cli/wiring.go` `buildCoordinator` from the resolved forge kind; `ConflictGuidance.ArtifactNoun` copies it; default `"issue"` when empty (test fakes)
- [X] T018 Create `internal/cli/summary.go` with `summaryTarget(cmd) (w io.WriteCloser, finish func() error, ok bool)` and one Azure command-data escaper (research R7); refactor `writeGitHubSummary`/`writeAzureSummary` in `internal/cli/wiring.go` to use it with no behavior change for the plan summary
- [X] T019 Add `-prefix` (default `github.com/skaphos/oiax/v2/internal/notification`) and `-min` (default 85) flags to `tools/checkcoverage/main.go`; add a `textsafe:coverage` task to `Taskfile.yml` (`go test -race -shuffle=on -covermode=atomic -coverprofile=coverage-textsafe.out ./internal/textsafe/...` then `go -C tools run ./checkcoverage -profile ../coverage-textsafe.out -prefix github.com/skaphos/oiax/v2/internal/textsafe -min 95`); run it from the Static Analysis job in `.github/workflows/ci.yml` so the floor is a hard CI gate in this change (constitution: coverage floors land in the same change)
- [X] T020 Run `go test -race -shuffle=on ./internal/textsafe/ ./internal/tmpl/ ./internal/forge/... ./internal/cli/` and `go -C tools test ./checkcoverage`; confirm T003–T010 now pass

**Checkpoint**: foundation ready. Stories can begin.

---

## Phase 3: User Story 1 - Find the resolution path from a failed GitHub Actions run (Priority: P1) 🎯 MVP (with US2)

**Goal**: on GitHub Actions with Issues enabled, a conflicting backflow edge produces one warning ending with the issue address, one summary section, and an exit-3 message naming the edge and address (spec FR-001 to FR-007, FR-009, FR-011).

**Independent Test**: GitHub fixture with Issues enabled and a conflicting hotfix; run `reconcile` with `GITHUB_ACTIONS=true` and a `GITHUB_STEP_SUMMARY` file; inspect warning, summary, exit code (quickstart §2).

### Tests for User Story 1 (MANDATORY) ⚠️

- [X] T021 [P] [US1] In `internal/reconcile/reconcile_test.go`, write one test per artifact path asserting the resulting `DivergenceReport` (kind conflict, status `ArtifactKnown`, `ArtifactURL` equal to the fake's address for the right ID): created (`TestApplyBackflowConflictCreatesArtifact` path), adopted (same head), advanced, advance failed (`updateConflictErr`), left alone (staler run; shallow clone; ancestry error), canonical after consolidation (lowest ID); extend `fakeForge` in the same file so `CreateConflictArtifact`/`ListConflictArtifacts` return artifacts with `URL` set
- [X] T022 [US1] In `internal/reconcile/reconcile_test.go`, assert the conflict path no longer logs the `halted on … conflict` warning during apply (it moves to the CLI) and that `ConflictGuidance` carries `Strategy`, `SHA`, contained `Subject`, `Applied`/`Whole`, `ArtifactNoun`, and a non-empty `SummaryBody` rendered with `Surface: "summary"`, for both cherry-pick and merge strategies; and that a custom artifact body reading `{{range .Commits}}{{.Subject}}{{end}}` receives commit subjects with bidi and control characters removed and capped at 200 runes (regression for the artifact surface, spec FR-013)
- [X] T023 [P] [US1] In `internal/reconcile/render_test.go`, write golden tests for `RenderConflictSections` per contracts/surfaces.md §2: cherry-pick section (applied count), merge section (whole-set note and `git merge --no-ff` guidance), the artifact link rendered as `[issue <n>](<address>)`, guide link, branch names with `|`, `` ` ``, `*`, `_`, `<`, `@` escaped
- [X] T024 [P] [US1] In `internal/cli/plan_reconcile_test.go`, extend the CLI `fakeForge` conflict stubs to return artifacts with IDs and URLs, then write `TestReconcileConflictGitHubSurfaces` (`GITHUB_ACTIONS=true`, temp `GITHUB_STEP_SUMMARY`): stderr has exactly one `::warning::backflow … halted on cherry-pick conflict … Resolution steps: <address>`; the summary file has the plan summary followed by one `### Backflow conflict:` section; stderr ends with `converged with reported divergence on 1 edge:` and the edge line with the address; exit code 3
- [X] T025 [P] [US1] In `internal/cli/execute_test.go`, update `TestExecuteDivergenceMessage` for the multi-line exit-3 message: first line still begins `converged with reported divergence`
- [X] T026 [US1] In `internal/cli/plan_reconcile_test.go`, write `TestReconcileConflictThenApplyAbort`: a fake forge that records a conflict on one edge and then fails a later apply step; assert exit code 1, the conflict section is in the summary, the conflict warning is on stderr, and no exit-3 message is printed (spec FR-002, edge case "Apply later aborts with exit 1")

### Implementation for User Story 1

- [ ] T027 [US1] In `internal/reconcile/reconcile.go`, change `recordBackflowConflict` to keep the `forge.ConflictArtifact` from `CreateConflictArtifact` (no longer discarded) and the canonical `existing` on every re-found path, and to return an `ArtifactStatus` plus the artifact; on a render failure after list, report `ArtifactKnown` when `existing` is known (spec FR-007). After the end-of-Apply sweep (`resolveBackflowConflicts` → `canonicalConflictArtifacts`), re-point every `KindConflict` report whose artifact was closed as a duplicate at the edge's canonical artifact (lowest ID) and its URL, so no surface links a closed duplicate (post-merge review on #126); T021 adds the case where this run creates a higher-numbered duplicate that the sweep closes
- [ ] T028 [US1] In `internal/reconcile/reconcile.go` (`applyBackflow`, merge and cherry-pick conflict branches), remove the pre-artifact `c.log().Warn(... halted on ...)` calls; after `recordBackflowConflict`, build a `ConflictGuidance` (subject via `textsafe.Subject`, noun from `c.ArtifactNoun`) and append a `KindConflict` `DivergenceReport` to `res.Divergences`
- [ ] T029 [US1] In `internal/reconcile/reconcile.go` (`recordBackflowConflict` render) and `internal/reconcile/report.go`, before either render, set `tmpl.Conflict.Subject` and every `.Commits[].Subject` to `textsafe.Subject(raw)` (the conflict context today builds commits with `tmpl.NewCommit`, which keeps control and bidi characters and has no 200-rune cap; data-model.md requires the cleaned value on both surfaces); render the artifact with the configured template and `Surface: "artifact"` (spec FR-013). For the summary: if `ConflictBodyReadsCommitText()` is true, set `BuiltinFallback` and render the **built-in** conflict body; otherwise render the configured body. Either way pass `Surface: "summary"`; the built-in body gets `textsafe.Markdown(textsafe.Subject(raw))` in `.Conflict.Subject` and every `.Commits[].Subject` (it only prints them), and a gate-false custom body never reads them (spec FR-016). Set `RenderFailed` on a summary render error
- [X] T030 [US1] Implement `RenderConflictSections(w io.Writer, reports []DivergenceReport, budget int) (written bool, err error)` in `internal/reconcile/render.go` per contracts/surfaces.md §2, using `textsafe.Markdown` for branch names and the subject in the header, writing the FR-005 note (`The configured conflict text could not be rendered; see the run log.`) in place of the body when `RenderFailed` is set, and the FR-016 note (`The configured conflict text includes commit text, so Oiax shows its built-in steps here.`) before the built-in body when `BuiltinFallback` is set; add `BuiltinFallback bool` to `ConflictGuidance` in `internal/reconcile/report.go`
- [X] T031 [US1] In `internal/cli/reconcile.go`, after `coord.Apply` returns and **after** the notification diagnostics are logged and `writeNotificationSummary` runs (conflict sections must be the last summary write, research R8): (1) compute the budget per research R8 (GitHub: 1 MiB minus `os.Stat` size of `$GITHUB_STEP_SUMMARY`; Azure: 1 MiB for the new file), write conflict sections through `summaryTarget`, and record whether they were written; (2) emit one conflict warning per `KindConflict` report through the logger, leading text per contracts/surfaces.md §1 with the subject passed through `textsafe.PlainLink`, ending with `. Resolution steps: <address>` for known artifacts; (3) return `applyErr` if set; (4) otherwise build the exit-3 message (contracts/surfaces.md §3) and return `exitCodeError{code: 3, msg: ...}`
- [X] T032 [US1] Add the exit-3 message builder in `internal/cli/divergence_message.go`: first line `converged with reported divergence on N edge:` / `on N edges:` (proper plural), one line per report in order, trusted text only, using "conflict record" wording (spec FR-009, FR-010)
- [ ] T033 [US1] Run `go test -race -shuffle=on ./internal/reconcile/ ./internal/cli/` and update existing conflict tests that asserted the old in-apply warning position (`TestApplyBackflowConflictUnsupportedForgeSkipsArtifact` log assertions, `TestReconcileJSONAnnotationNotOnStdout`) to the new CLI emission

**Checkpoint**: US1 works on GitHub Actions. It must not ship before US2.

---

## Phase 4: User Story 2 - Hostile commit subjects stay inert (Priority: P1) 🎯 MVP (with US1)

**Goal**: no commit subject can add CI commands, forge warnings, links, HTML, mentions, or Markdown structure on any surface (spec FR-013 to FR-016, SC-004), the annotation handler neutralizes host commands in every warning (research R6), and no credential reaches a new surface (SC-007).

**Independent Test**: run the conflict scenario with each subject in the adversarial corpus on both hosts; inspect processed commands and rendered summary (quickstart §4).

### Tests for User Story 2 (MANDATORY) ⚠️

- [X] T034 [P] [US2] Create `internal/textsafe/testdata/adversarial.txt` with at least 50 subjects, including every User Story 2 vector: `::error::x`, `%0A::warning::x`, `##[error]x`, `##[add-mask]development`, `##[stop-commands]t`, `##vso[task.setvariable variable=OIAX_INJECTED]1`, `##vso[task.complete result=Succeeded]`, `[Resolution steps](https://attacker.example)`, a bare URL, `www.attacker.example`, `<img src=x>`, `@org/team`, `| a | b |`, `# h`, stray backticks, a 500-rune subject with control and bidi characters; plus case variants (`##VSO[`), attacker-supplied U+2060, and mixed vectors
- [X] T035 [P] [US2] Write a corpus test in `internal/textsafe/corpus_test.go` asserting, for every corpus line, each function's contract property (contracts/contained-text.md); for `Markdown`, the oracle is: the output contains none of the ASCII characters `` ` * _ [ ] ( ) < > # | ! ~ \ @ & = `` at all, no `://`, no word-initial `www.`, no line-leading `-`, `+`, or digit-plus-`.`, and no line break. Each corpus line is also rendered through the built-in conflict body on the summary surface, and the result must keep exactly the body's own backticks and must not contain a line made only of `=` or `-` characters
- [X] T036 [P] [US2] Add fuzz targets `FuzzSubject`, `FuzzLogCommand`, `FuzzMarkdown`, `FuzzPlainLink` in `internal/textsafe/fuzz_test.go` seeded from the corpus, asserting the same properties; commit any fuzzer-found input to `internal/textsafe/testdata/fuzz/`
- [X] T037 [P] [US2] In `internal/reconcile/logging_test.go`, add regression tests that a warning message containing mid-line `##vso[task.setvariable ...]`, `##[error]`, and `::warning::` is neutralized in the GitHub annotation, the Azure annotation, and the plain log copy (the existing `TestAzureAnnotationEscapesLoggingCommandChars` input leaves `##vso[` intact; these tests fail before T041)
- [X] T038 [P] [US2] In `internal/cli/plan_reconcile_test.go`, write `TestReconcileConflictAdversarialSubjects`: for each corpus subject, run the US1 GitHub scenario and the Azure scenario (`TF_BUILD=True`) and assert stderr contains exactly the expected Oiax commands (no extra `::`/`##vso[`/`##[` command lines), and the summary file contains no Markdown link, image, HTML tag, or mention derived from the subject
- [X] T039 [US2] In `internal/cli/plan_reconcile_test.go`, write `TestReconcileConflictSurfacesNoCredentials` (spec SC-007, FR-018): set sentinel values for `GITHUB_TOKEN`, `GH_TOKEN`, and `AZURE_DEVOPS_EXT_PAT`/the provider's token variable, run the conflict scenario under GitHub Actions and Azure Pipelines for each artifact status, and assert no sentinel appears in stderr (warnings, plain log, exit-3) or in any summary file
- [X] T040 [P] [US2] Add a `textsafe:fuzz` task to `Taskfile.yml` that runs each `internal/textsafe` fuzz target for a bounded time (for example `-fuzztime 30s` each), alongside the existing `notifications:fuzz` campaign

### Implementation for User Story 2

- [X] T041 [US2] In `internal/reconcile/logging.go`, apply `textsafe.LogCommand` to the record message **and every string-valued attribute** (including `err`, whose text can carry forge API response messages, and attributes inside groups) in `annotationHandler.Handle` before both `formatAnnotation` and the base handler, so annotations and the plain stderr copy are neutralized for every warning (research R6; post-merge review on #126). T037 adds a case with `"err", errors.New("##vso[task.complete result=Succeeded]")`
- [X] T042 [US2] Verify T029 and T031 apply `textsafe.Subject` before every surface function and `textsafe.PlainLink` to the warning subject; fix any path the corpus test (T038) or credential test (T039) shows leaking
- [ ] T043 [US2] Run `go test -race -shuffle=on ./internal/textsafe/ ./internal/reconcile/ ./internal/cli/`, `go -C tools tool task textsafe:fuzz`, and `go -C tools tool task textsafe:coverage` (must pass the ≥ 95% gate)

**Checkpoint**: MVP (US1 + US2) is **validated, not yet shippable**. Shipping also requires the built-binary check (T060), the same-change documentation (T061–T066), and the full gate (T067) from Phase 8 (constitution: binary-boundary verification; spec FR-018).

---

## Phase 5: User Story 3 - The same guidance on Azure Pipelines (Priority: P2)

**Goal**: identical guidance under Azure Pipelines, with Azure Repos (Boards work item) or GitHub (issue) as the forge (spec FR-012, US3).

**Independent Test**: run the US1 fixture under Azure Pipelines, once with Azure Repos and once with GitHub (quickstart §3).

### Tests for User Story 3 (MANDATORY) ⚠️

- [X] T044 [P] [US3] In `internal/cli/plan_reconcile_test.go`, write `TestReconcileConflictAzureSurfaces` (`TF_BUILD=True`, temp `AGENT_TEMPDIRECTORY`): one `##vso[task.logissue type=warning]` conflict warning ending with the address, one `##vso[task.uploadsummary]` for a file containing the conflict section, exit code 3; run once with a GitHub-forge fake (issue address) and once with an Azure-forge fake (work item address and noun), and assert the sections differ only in artifact noun and address (spec US3 AS2)
- [X] T045 [P] [US3] In `internal/cli/notification_summary_test.go`, add the regression test for the existing bug: with `AGENT_TEMPDIRECTORY` containing `%` and a newline, the notification summary's `##vso[task.uploadsummary]` path is escaped (`%AZP25`, `%0A`); it fails before T047

### Implementation for User Story 3

- [X] T046 [US3] Confirm `buildCoordinator` (T017) sets `ArtifactNoun` to `"work item"` for the Azure DevOps forge under Azure Pipelines, and `"issue"` for a GitHub forge under Azure Pipelines (`BUILD_REPOSITORY_PROVIDER=github`); fix the wiring in `internal/cli/wiring.go` if T044 shows otherwise
- [X] T047 [US3] In `internal/cli/notification_summary.go`, replace the hand-rolled temp-file and unescaped `##vso[task.uploadsummary]` with `summaryTarget` (spec FR-012, research R7)
- [ ] T048 [US3] Run `go test -race -shuffle=on ./internal/cli/` and confirm T044 and T045 pass

**Checkpoint**: US1–US3 work on both hosts and both forges.

---

## Phase 6: User Story 4 - No artifact can be hosted or recorded (Priority: P2)

**Goal**: when the repository cannot host the artifact (Issues disabled) or Oiax could not record it this run, every surface says so and still gives the steps; nothing points at a summary that was not written (spec FR-008, FR-010, US4).

**Independent Test**: GitHub fixture with Issues disabled, plus fault injection on artifact list and create; also with no summary destination (quickstart §2 last paragraph).

### Tests for User Story 4 (MANDATORY) ⚠️

- [ ] T049 [P] [US4] In `internal/reconcile/reconcile_test.go`, assert `ArtifactUnhostable` when `conflictArtifactsUnsupported` is set (no list or create attempted, existing #69 warning unchanged), and `ArtifactNotRecorded` for `listConflictsErr`, `createConflictErr`, and a template render failure with no existing artifact; `ArtifactURL` empty in all three
- [ ] T050 [P] [US4] In `internal/reconcile/render_test.go`, golden-test the unhostable section (only-guidance statement, retention statement, link to `docs/guides/operating.md#keep-issues-enabled-github-backflow-only`), the not-recorded section (retried next run), and the render-failure section (FR-005 note instead of body), using "conflict record" wording
- [ ] T051 [P] [US4] In `internal/cli/plan_reconcile_test.go`, write `TestReconcileConflictNoArtifactPointers`: for unhostable and not-recorded, with a summary written the warning and exit-3 line say "resolution steps are in this run's summary"; with `GITHUB_STEP_SUMMARY` unset (local run) and with an unwritable summary path, neither mentions a summary and both point to the backflow guide address (spec FR-010, US4 AS3)

### Implementation for User Story 4

- [ ] T052 [US4] In `internal/reconcile/reconcile.go`, return `ArtifactUnhostable` from the capability gate **and from a create that fails because the repository cannot host artifacts** (GitHub's create-time 410, already recognized in `internal/forge/github/github.go`; expose it as a typed `forge` error the coordinator can test with `errors.Is`), and `ArtifactNotRecorded` from the other list, create, and render-without-existing failure paths of `recordBackflowConflict` (post-merge review on #126); T049 adds the create-410 case
- [ ] T053 [US4] In `internal/reconcile/render.go` and `internal/cli/reconcile.go`, implement the FR-008 statements ("No durable conflict record can be kept (Issues are disabled)", "could not be recorded or confirmed this run and will be retried next run") and the summary-written/not-written pointer selection per contracts/surfaces.md §1–§3

**Checkpoint**: US1–US4 complete.

---

## Phase 7: User Story 5 - Several conflicting edges in one run (Priority: P3)

**Goal**: N conflicting edges give N warnings, N sections, and N exit-3 lines in plan order; other divergences are named in exit-3 output only (spec FR-009, FR-011, US5, SC-006).

**Independent Test**: graph with two conflicting backflow sources plus one other reported divergence; repeat the run.

### Tests for User Story 5 (MANDATORY) ⚠️

- [ ] T054 [P] [US5] In `internal/reconcile/reconcile_test.go`, assert two conflicting backflow sources yield two `KindConflict` reports in plan action order, and an `ActionReportDivergence` on another edge yields one `KindOther` report; repeat 20 times and assert identical order (SC-006)
- [ ] T055 [P] [US5] In `internal/cli/plan_reconcile_test.go`, write `TestReconcileMultipleConflictsOrdered`: two warnings, two self-contained sections, and an exit-3 message `converged with reported divergence on 3 edges:` naming both conflict edges with their own pointers plus `<edge>: reported divergence` for the other, in plan order
- [ ] T056 [US5] In `internal/reconcile/render_test.go`, test the size budget (spec FR-005, research R8): (a) every section fits in full: no note; (a2) a budget exactly equal to the full total: every section full, no note (the note's reserve must not shorten guidance that fits); (b) a budget between the reserve and the full total: earlier sections full, later ones reduced, one note at the end, total written ≤ budget; (c) a budget below the reserve (reduced sections plus note do not fit): nothing written and `written == false`; (d) a budget of zero: nothing written and `written == false`. In `internal/cli/plan_reconcile_test.go`, add a case where `$GITHUB_STEP_SUMMARY` is already at 1 MiB: no section is appended, and the warning and exit-3 line use the artifact address (known) or the guide (no artifact), never "this run's summary"; and a combined-writer case where the plan summary, notification diagnostics, and conflict sections all write to one `$GITHUB_STEP_SUMMARY` that starts near 1 MiB: the conflict sections never take the file past 1 MiB (when the earlier writers already did, no section is appended), and every pointer matches what was actually written. Oiax does not budget the plan summary or notification diagnostics; that is unchanged behavior (post-merge review on #126)

### Implementation for User Story 5

- [ ] T057 [US5] In `internal/reconcile/reconcile.go`, append a `KindOther` `DivergenceReport` in the `ActionReportDivergence` arm, and ensure a single report per edge (a conflict and a report-divergence on the same edge yield one `KindConflict`)
- [ ] T058 [US5] In `internal/reconcile/render.go`, implement the budget allocation in `RenderConflictSections` per research R8: if the full sections total at most the budget, write them all with no note; otherwise reserve every reduced section plus the note, write nothing and return `written == false` when the reserve exceeds the budget, otherwise upgrade sections to full form in plan order while they fit and write the note once when any section stayed reduced

**Checkpoint**: all user stories complete.

---

## Phase 8: Polish & Cross-Cutting Concerns

- [ ] T059 Write `TestReconcileConflictMatrix` in `internal/cli/plan_reconcile_test.go` (spec SC-001), table-driven over host (GitHub Actions, Azure Pipelines) × forge (GitHub, Azure DevOps; skipping GitHub Actions + Azure DevOps if unsupported) × artifact status (known, unhostable, not recorded), asserting the edge, failing commit, and either the artifact link or the no-artifact statement on the warning and the summary; the reconcile per-path tests (T021, T049) remain the proof for the eight FR-007/FR-008 paths
- [ ] T060 [P] Extend the notification-binary fixture (`internal/cli/notification_merge_test.go`, `internal/cli/testdata/notification-binary/forge.go`) with a conflicting-hotfix scenario run under `GITHUB_ACTIONS=true` with a summary file, asserting the warning, section, and exit 3 from the built binary (research R10)
- [ ] T061 [P] Update `docs/guides/backflow.md` "When a replay conflicts": new warning text and pointer, the summary section, the exit-3 lines, the warning now emitted after apply, the Issues-disabled case, and "conflict record" wording
- [ ] T062 [P] Update `docs/guides/github-action.md` and `docs/guides/azure-pipelines.md` "Step summary and annotations" for the conflict sections and warnings
- [ ] T063 [P] Update `docs/guides/operating.md` ("Exit codes and CI gating", "When Oiax reports a divergence", "Keep Issues enabled") and `docs/reference/configuration.md` "Exit codes" for the multi-line exit-3 message (exit code unchanged)
- [ ] T064 [P] Update `docs/reference/templates.md`: add `.Surface` with an example of guarding artifact-only text, document `.Conflict.SquashCommits`, state that `.Conflict.Subject` is capped at 200 runes in total (including `…`) with control and bidi characters removed, and explain that a conflict template that reads commit text (`.Conflict.Subject`, `.Commits[].Subject`, or a whole `.Conflict`/`.Commits`/`.` value) is still used for the artifact but the run summary shows the built-in steps instead, with the list of fields a summary-safe template may read
- [ ] T065 [P] Update `docs/architecture.md` annotation and summary paragraphs for the post-apply conflict surfaces and the log-command neutralization
- [ ] T066 If the `reconcile` help text changed, regenerate `docs/reference/cli.md` with `go -C tools tool task docs:cli-ref` and confirm `go -C tools tool task verify-generated` is clean
- [ ] T067 Run the full gate: `go test -race -shuffle=on ./...`, `go -C tools tool task lint`, `go -C tools tool task staticcheck`, `go -C tools tool task vuln`, `go -C tools tool task verify-generated`, `go -C tools tool task textsafe:coverage`; confirm no touched package dropped below its current coverage floor
- [ ] T068 Walk through quickstart.md §1–§4 and record results in the PR description; list quickstart §5 (live renders on both hosts, URL clickability, SC-002) as post-merge live acceptance

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)** → **Foundational (Phase 2)** → user stories.
- **US1 (Phase 3)** depends on Phase 2.
- **US2 (Phase 4)** depends on Phase 2; its CLI tests (T038, T039) exercise US1's surfaces, so run them after T031. US1 must not ship without US2.
- **US3 (Phase 5)** depends on Phase 2 and on US1 (it reuses the CLI flow); T045/T047 (notification summary fix) depend only on T018.
- **US4 (Phase 6)** depends on US1 (status plumbing and pointers).
- **US5 (Phase 7)** depends on US1.
- **Polish (Phase 8)**: T059 after US3 and US4; T060 after US1 (and #153, merged in T001); docs after the stories they describe.

### Story completion order

```text
Setup → Foundational → US1 → US2 (MVP validated) → US3 → US4 → US5 → Polish → ship
```

US2–US5 run in sequence after US1 because they share test and implementation files (`internal/cli/plan_reconcile_test.go`, `internal/reconcile/reconcile_test.go`, `internal/reconcile/render_test.go`, `internal/reconcile/reconcile.go`, `internal/reconcile/render.go`). Shipping requires Polish: the binary check (T060), the documentation (T061–T066), and the full gate (T067). The whole feature lands as one pull request.

### Within each story

Tests first (they must fail), then implementation, then the story's test run.

## Parallel Opportunities

`[P]` means file-disjoint from every other `[P]` task in the same phase and with no dependency on an incomplete task. Tasks that share a file run in ID order.

- **Phase 2 tests**: T003, T005, T008, T009, T010 in parallel; then T004 after T003 (`textsafe_test.go`); T006 → T007 after T005 (`tmpl_test.go`).
- **Phase 2 implementation**: T011–T015 in parallel (different files); then T016 → T017 → T018 → T019 (T016 and T012 share `reconcile.go`; T017 and T018 share `wiring.go`).
- **US1 tests**: T021, T023, T024, T025 in parallel; then T022 after T021 (`reconcile_test.go`) and T026 after T024 (`plan_reconcile_test.go`).
- **US1 implementation**: T027 → T028 → T029 → T030 (shared `reconcile.go`, then shared `report.go`); T031 → T032 after T030.
- **US2**: T034–T038 and T040 in parallel; T039 after T038 (`plan_reconcile_test.go`); T038–T039 also need T031; T041 independent of US1 implementation.
- **US3, US4, US5**: run one story at a time, in that order. Within a story, its `[P]` tests are file-disjoint (US5: T054 and T055 in parallel, then T056).
- **Polish**: T060–T065 in parallel.

### Example: Foundational in parallel

```text
Task: "T003 Subject tests in internal/textsafe/textsafe_test.go"
Task: "T005 Surface tests in internal/tmpl/tmpl_test.go"
Task: "T008 URL conformance in internal/forge/forgetest/conformance.go"
Task: "T009 summaryTarget tests in internal/cli/summary_test.go"
Task: "T010 checkcoverage flag tests in tools/checkcoverage/main_test.go"
```

### Example: US2 in parallel

```text
Task: "T034 adversarial corpus in internal/textsafe/testdata/adversarial.txt"
Task: "T036 fuzz targets in internal/textsafe/fuzz_test.go"
Task: "T037 log-command regression tests in internal/reconcile/logging_test.go"
Task: "T040 textsafe:fuzz task in Taskfile.yml"
```

## Implementation Strategy

### MVP first (US1 + US2)

1. Phase 1 → Phase 2 (including the coverage gate, T019).
2. US1 (GitHub end to end), then US2 (containment, the logging fix, and the credential check).
3. **Stop and validate**: quickstart §2 and §4 on GitHub. This is a **validation checkpoint, not a release**: it proves the fix for the field incident in #107 works safely, but it ships only together with the Phase 8 binary check (T060), documentation (T061–T066), and full gate (T067).

### Incremental delivery

- US3 adds Azure parity and fixes the notification-summary upload path.
- US4 covers Issues-disabled and failure cases.
- US5 hardens ordering and size limits.
- Each story ends at a checkpoint where the full test suite passes; Polish lands the SC-001 matrix, docs, the binary test, and the full gate.

## Notes

- Commit after each task or logical group; DCO sign-off as `Shawn Stratton <shawn@skaphos.io>`; Conventional Commits (`feat(reconcile): …`, `fix(cli): …` for the notification path, `test(...)`, `build(...)`/`ci(...)` for the coverage gate, `docs(...)`).
- No exit code, JSON plan, request metadata, artifact identity, or forge-write change (spec FR-017): any task that seems to need one is out of scope; stop and raise it.
