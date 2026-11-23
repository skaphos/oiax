# Research: Backflow Conflict Guidance on the Failing Run

**Spec**: [spec.md](spec.md) · **Plan**: [plan.md](plan.md) · Verified against the spec branch at `189f29a` (main `da3a52b` merged).

Each entry records a decision, why it was made, and what was rejected. File references are to the current tree.

## R1. Where per-edge conflict outcomes travel

- **Decision**: `reconcile.Result` gains an ordered `Divergences []DivergenceReport` (see [data-model.md](data-model.md)). `recordBackflowConflict` and the `ActionReportDivergence` arm append to it in plan action order. The CLI reads it after `Apply` returns, including when `Apply` returns an error, because `Apply` already returns the partial `res` with its error (`internal/reconcile/reconcile.go`, `Apply`).
- **Rationale**: today nothing per-edge leaves `Apply`: `Result` carries only counts and `Divergence bool`, and the `conflicted` map is local (reconcile.go L90-113, L912). `Result` is an in-process type, never serialized, so adding a field touches no compatibility contract (Constitution XII; spec FR-017).
- **Alternatives rejected**: putting outcomes in the plan document (FR-017 forbids it, and the plan is built before apply); scraping log output (Constitution VI: explanations come from the code that decided).

## R2. When and where the conflict warning is emitted

- **Decision**: the conflict warning moves out of `applyBackflow` (today logged before the artifact step, reconcile.go L1082-1084 and L1107-1109) and is emitted by the CLI after `Apply` returns and after the summary write has succeeded or failed. One warning per `DivergenceReport` of kind conflict, in report order.
- **Rationale**: FR-001 needs the warning after the artifact step, and FR-010 forbids pointing at the summary unless the sections were actually written. Only the CLI knows both. Emitting after apply still satisfies "after the artifact step finishes or is skipped", and the apply-abort case (exit 1) still emits, because the CLI has the partial `res`.
- **Alternatives rejected**: emitting from reconcile with a guess about the summary (violates FR-010 when the later write fails); emitting a second "correction" warning after a failed write (two warnings per edge, against FR-001).
- **Consequence**: warnings appear after the apply log lines instead of interleaved. The leading text format is unchanged (FR-001), and tests that assert the old log position are updated.

## R3. Artifact address

- **Decision**: add `URL string` to `forge.ConflictArtifact`. Each provider fills it on `ListConflictArtifacts` and `CreateConflictArtifact` from its resolved identity:
  - GitHub: `https://github.com/` + `PathEscape(Owner)` + `/` + `PathEscape(Repo)` + `/issues/` + number (precedent: `internal/forge/github/notifications.go` request links).
  - Azure Boards: `https://dev.azure.com/` + `PathEscape(Organization)` + `/` + `PathEscape(Project)` + `/_workitems/edit/` + id (precedent: `internal/forge/azuredevops/notifications.go` PR links).
  - `recordBackflowConflict` keeps the `ConflictArtifact` it already holds on every path (create result, canonical `existing`) instead of discarding it, so the address is identical on all FR-007 paths.
- **Resolves spec C7**: the Azure provider's identity holds the project **name** (`azuredevops.Repo{Organization, Project, Name}`, decoded from env or remote, so it can contain spaces), not its ID. The address uses the escaped name, matching the existing PR links.
- **Rationale**: the address is derived from Oiax-resolved identity and the artifact identifier only (FR-006), never from artifact text. Adding a field leaves the `forge.Forge` method set unchanged.
- **Alternatives rejected**: a new `ConflictArtifactURL(id)` interface method (a wider interface change for the same data); reading `html_url` from the GitHub API response (the address must not depend on forge-returned free text, and Azure has no equivalent).
- **Conformance**: `internal/forge/forgetest` `testConflictLifecycle` asserts every listed and created artifact has the provider's expected address. Each provider's conformance runner supplies the expected-address function.

## R4. Template surface variable (spec C1)

- **Decision**: `tmpl.Context` gains `Surface string` (`"artifact"` or `"summary"`). The artifact render sets `artifact`; the summary render sets `summary`. The built-in conflict body wraps its artifact-only closing sentence in `{{if eq .Surface "artifact"}}…{{end}}` and uses a forge-neutral noun ("Oiax manages this conflict record. It closes automatically … Do not edit the metadata block below."). Request templates (promotion, backflow) and the merge message always render with `artifact`.
- **Rationale**: the built-in body ends with artifact-only claims (`internal/tmpl/tmpl.go` default conflict body, closing sentence), and FR-003 renders the section from the same slot. Additive under ADR 0011 ("fields may be added within v1"); `sampleRender` validates the conflict pair on both surfaces at load, so a custom template that fails on either surface fails validation like any other render error.
- **Alternatives rejected**: stripping the closing sentence by string match (breaks on custom templates and is fragile); a separate summary template slot (adds configuration, against OOS-003, and lets the two drift).

## R5. Contained text pipeline (spec FR-013 to FR-016, C2, C9)

One package, `internal/textsafe`, owns the transformations so every surface uses the same code:

| Function | Applies | Used by |
| --- | --- | --- |
| `Subject(s) string` | `strings.ToValidUTF8`, control and Unicode `Cf` (bidi) characters removed, first line only, capped at 200 runes with `…` | the shared render context (both surfaces), the warning |
| `LogCommand(s) string` | breaks host command openers anywhere: `##[`, `##vso[`, and `::` | the annotation handler (all warnings) and the plain log copy |
| `PlainLink(s) string` | breaks linkifiable sequences: `://` and a word-initial `www.` | conflict warning text |
| `Markdown(s) string` | backslash-escapes CommonMark punctuation (`` \ ` * _ { } [ ] ( ) # + - . ! \| ~ > ``), entity-escapes `<` and `&`, breaks `@` mentions and autolinks (`://`, `www.`) | values handed to the summary render, and the Oiax-owned section header |

- **Breaking sequences**: insert U+2060 WORD JOINER inside the sequence (`#⁠#[`, `:⁠//`, `@⁠`). It is invisible, so the text still reads correctly, and the host's byte-level match fails. `Subject` strips `Cf` characters before the later functions insert their own joiners, so attacker-supplied joiners cannot be used to rebuild a sequence.
- **Order**: `Subject` first (shared context), then the surface function. The artifact render receives `Subject(s)` only, so artifact text changes only by the cap and character removal (OOS-004, FR-013).
- **Rationale**: there is no Markdown or log-command neutralizer today. `escapeAnnotation` and `escapeAzureAnnotation` (`internal/reconcile/logging.go`) only escape `%`, CR and LF; `mdCell` only escapes `|` and newlines; `notification.CleanText` strips control and `Cf` characters and is the precedent for `Subject`.
- **Alternatives rejected**: escaping the rendered summary Markdown (cannot tell template Markdown from values, spec FR-016); fenced code blocks for subjects (a backtick in the subject can close the fence); HTML entities only (GitHub and Azure renderers differ on which entities they decode in all positions).
- **Verification**: a corpus of at least 50 adversarial subjects (SC-004) in `internal/textsafe/testdata/`, checked by unit tests for each function, plus fuzz targets `FuzzSubject`, `FuzzMarkdown` and `FuzzLogCommand` (Constitution: untrusted input is fuzzed). Live acceptance renders the corpus on both hosts' summary renderers once.

## R6. Log-command neutralization scope

- **Decision**: apply `textsafe.LogCommand` to the record message in `annotationHandler.Handle` before both the annotation and the base handler see it, so the annotation and the plain stderr copy are both safe, for every warning, not only conflict warnings.
- **Rationale**: the plain log copy is written to stderr through the base slog handler with the message quoted, and a mid-line `##vso[` survives quoting. The Azure agent scans every output line, so the plain copy alone can inject a command. Other warnings already carry untrusted text, for example `reported divergence on %s -> %s: %s` with the plan reason. Fixing it in the handler covers all of them (FR-014 requires only this feature's lines, but a handler-level fix is simpler and strictly safer).
- **Alternatives rejected**: neutralizing only the conflict warning (leaves the same hole in other warnings); neutralizing in `formatAnnotation` only (misses the plain copy).

## R7. Summary destination and the Azure upload path (spec FR-012, C4)

- **Decision**: extract one helper, `summaryTarget(cmd) (w io.WriteCloser, finish func() error, ok bool)`, used by the plan summary, the notification summary, and the new conflict sections. On GitHub it appends to `$GITHUB_STEP_SUMMARY`; on Azure it creates a temp Markdown file in `$AGENT_TEMPDIRECTORY` and, on `finish`, prints `##vso[task.uploadsummary]` with the path escaped by one shared `azureCommandData` escaper (`%`→`%AZP25`, CR→`%0D`, LF→`%0A`).
- **Fixes the existing bug**: `writeNotificationSummary` prints the path unescaped (`internal/cli/notification_summary.go`). A regression test with `%` and a newline in `AGENT_TEMPDIRECTORY` fails before the change.
- **Rationale**: three writers, one escaping rule. The plan summary's escaping (`internal/cli/wiring.go` `writeAzureSummary`) becomes the shared escaper.

## R8. Summary size limit (spec C3)

- **Decision**: budget the conflict sections at **1 MiB minus the bytes already written to the summary this run**, on both hosts. When the next full section would exceed the budget, write the reduced section (FR-002 items 1–3 and 5) with a truncation note, and keep reducing.
- **Rationale**: GitHub documents a 1 MiB per-step job-summary limit. Azure Pipelines does not document a size limit for `task.uploadsummary` attachments, so the spec's rule applies: use the smaller known limit when one is unknown. Conflict sections are small (a few KiB each), so truncation is only reachable with many conflicting edges or very large custom templates.
- **Alternatives rejected**: no budget (a large custom template times several edges could exceed GitHub's limit, and GitHub then drops the whole step summary).

## R9. Exit-3 output

- **Decision**: build the exit-3 message in the CLI from `res.Divergences`: the first line is `converged with reported divergence` (plus ` on N edges:` when N ≥ 1), then one line per report in plan order with its pointer (FR-009). It contains only trusted text: branch names (validated configuration), Oiax-derived addresses, and fixed phrases (FR-010).
- **Rationale**: `exitCodeError.msg` is already printed verbatim to stderr by `Execute` (`internal/cli/root.go`), so a multi-line message needs no new plumbing. The first line keeps today's exact text, so anything matching on it keeps working; the text is not a compatibility contract (the exit code is, and it is unchanged).

## R10. Testing at the most realistic boundary

- **Decision**: unit tests in `textsafe`, `tmpl`, `reconcile` (every FR-007 artifact path, plus unhostable and failed), and `cli` (GitHub and Azure environments, summary written, summary unavailable, apply abort). Plus one built-binary scenario using the existing notification-binary fixture overlay (`internal/cli/notification_merge_test.go`), extended so its fake forge can produce a cherry-pick conflict, under `GITHUB_ACTIONS=true` with a summary file.
- **Rationale**: the constitution requires runtime-behavior changes to be verified at the most realistic reachable boundary, which for a CLI is the built binary. The fixture already builds the real binary against real providers with an HTTPS fake.
- **Note**: that fixture is affected by the clock flake fixed in #153 (#143); this feature's binary test should land after #153.
