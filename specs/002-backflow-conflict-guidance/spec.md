# Feature Specification: Backflow Conflict Guidance on the Failing Run

**Feature Branch**: `spec/backflow-conflict-guidance`

**Created**: 2026-11-07

**Status**: Draft

**Tracking**: [#107](https://github.com/skaphos/oiax/issues/107); related [#69](https://github.com/skaphos/oiax/pull/69) (repositories that cannot host conflict artifacts).

**Input**: User description: "reconcile: surface the backflow conflict artifact and its resolution steps from the failing run" (issue #107 and its 2026-11-07 "Before implementation" questions).

## Clarifications

### Session 2026-11-22

- Q: Where does the job-summary section's guidance body come from (FR-003): (A) the same resolved `spec.templates.backflowConflict` slot the artifact uses, (B) always the built-in playbook, or (C) the built-in steps plus the artifact link? → A: **A**, the same resolved slot, so a customized artifact and the summary cannot drift (#107's acceptance). SC-003 applies as written.

### Consistency review 2026-11-22

- The built-in conflict body itself contains artifact-only sentences ("Oiax manages this issue. It closes automatically once the conflict clears … Do not edit the metadata block below."), so FR-003 (same slot) and FR-004 (no artifact-only claims in the section) can only both hold if the template knows which surface it renders for. That variable is now required (FR-004), not a plan decision.
- One template renders for both surfaces, so Markdown inertness (FR-016) must be applied to the values handed to the summary render, not to its output. The artifact render is unchanged (FR-016, OOS-004).
- The unescaped Azure summary upload path in the notification summary is fixed in this feature, as a bug fix with a regression test (FR-012).
- Left to the plan's research: the Azure Pipelines summary size limit (FR-005) and whether the Azure Boards address uses the project name or ID (FR-006).

### Review on PR #126, 2026-11-22

- The 200-rune cap includes the truncation mark (FR-013).
- Look-alike substitution replaces backslash escaping, so values stay inert in any Markdown position, including Setext underlines (FR-016).
- A second review round showed that containing commit text inside arbitrary custom Go templates cannot be made airtight (`printf "%#v"` bypasses a `String()` method, a sentinel probe misses links that only appear for the real subject, `trunc` can strip a sentinel). The design is therefore a gate, not a containment scheme: a custom conflict template is used for the summary only if its parse tree shows it never reads commit text; otherwise the summary uses the built-in body with a note. The template is never rejected at load, which would break previously accepted configuration (FR-003, FR-016, SC-003).
- The size budget reserves room for every reduced section and the note first, and when nothing fits the summary counts as not written (FR-005).

## Current Behavior *(re-verified against `main` at `d774e90`)*

- On a replay conflict, `reconcile` logs `backflow <source> -> <target> halted on … conflict at <sha> "<subject>" …; no request created` **before** it records the artifact. Under CI that line also becomes a `::warning::` or `##vso[task.logissue type=warning]` command.
- The artifact ([ADR 0008](../../docs/adr/0008-durable-backflow-conflict-artifact.md)) is a GitHub issue or an Azure Boards work item. Its text is rendered from `spec.templates.backflowConflict` ([ADR 0011](../../docs/adr/0011-templatable-request-text.md)). The create result is discarded, the re-found paths use only the identifier, and the artifact view has no web address.
- `.Conflict.Subject` is not capped, although ADR 0011 and the [templates reference](../../docs/reference/templates.md#untrusted-variables) say subjects are capped at 200 runes.
- The plan summary is written before apply, and nothing about the conflict is added to it. On exit 3, stderr shows only `converged with reported divergence`.
- When a GitHub repository has Issues disabled, Oiax detects it, warns, and skips artifact I/O (#69). On Azure DevOps, artifacts are always reported as supported, so a Boards failure appears only when create fails.

## User Scenarios & Testing *(mandatory)*

**Terms**:

- **Warning**: the host annotation (GitHub) or run issue (Azure Pipelines).
- **Summary**: the GitHub job summary, or the Azure Pipelines Extensions-tab summary.
- **Artifact**: the ADR 0008 conflict artifact.

### User Story 1 - Find the resolution path from a failed GitHub Actions run (Priority: P1)

A repository owner opens a failed reconcile run (exit 3). From the warnings and the summary, without the raw log or the issue tracker, they find the conflicting edge, the failing commit, a link to the artifact, and the steps to resolve it.

**Why this priority**: In the field (sdp-cluster-addons run 39437456619), issue #177 held the playbook, but the owner never found it. They merged by hand and re-ran, which opened #178.

**Independent Test**: Use a GitHub fixture with Issues enabled and a hotfix that conflicts on replay. Run `reconcile` under GitHub Actions and inspect the warning, the summary, and the exit code.

**Acceptance Scenarios**:

1. **Given** a cherry-pick conflict and no open artifact, **When** reconcile runs, **Then**:
   - the artifact is created as today;
   - the warning ends with `Resolution steps: https://github.com/<owner>/<repo>/issues/<number>`;
   - the summary has one section for the edge;
   - the final stderr output names the edge and the address;
   - the exit code is 3.
2. **Given** an open artifact that this run adopts, advances, or leaves alone (or several duplicates), **When** reconcile runs, **Then** every surface links that artifact (the lowest-numbered canonical one, if there are duplicates).
3. **Given** the merge strategy, **When** the merge conflicts, **Then** the section shows the `git merge --no-ff` steps and says to land the fix as a merge commit.
4. **Given** the earlier plan table lists the edge's backflow as planned, **When** the section is published, **Then** it states that no request was created.

---

### User Story 2 - Hostile commit subjects stay inert (Priority: P1)

Anyone who can land a commit on an observed branch controls its subject. That text must not be able to add CI commands, forge warnings, or plant links.

**Why this priority**: User Story 1 puts untrusted text on more surfaces, so it cannot ship without this.

**Independent Test**: Run the conflict scenario on both hosts once per subject in an adversarial corpus. Inspect the commands each host processed and the rendered summary.

**Acceptance Scenarios**:

1. **Given** each of these subjects, **When** the conflict is reported under GitHub Actions, **Then** the runner processes only Oiax's own commands, and later Oiax warnings still appear:
   - `::error::x`
   - `%0A::warning::x`
   - `##[error]x`
   - `##[add-mask]development`
   - `##[stop-commands]t`
2. **Given** `##vso[task.setvariable variable=OIAX_INJECTED]1` or `##vso[task.complete result=Succeeded]`, **When** the conflict is reported under Azure Pipelines, **Then** no variable is set and the result is unchanged.
3. **Given** each of these subjects, **When** the warning and summary are shown, **Then** none of it becomes a link, image, HTML, mention, heading, or table structure. The only links are the artifact address and the backflow guide.
   - `[Resolution steps](https://attacker.example)`
   - a bare URL
   - `www.attacker.example`
   - `<img src=x>`
   - `@org/team`
   - `| a | b |`
   - `# h`
   - stray backticks
4. **Given** a 500-rune subject with control and bidi characters, **When** it is shown on any new surface, **Then** at most 200 runes appear and those characters are removed.

---

### User Story 3 - The same guidance on Azure Pipelines (Priority: P2)

A team runs Oiax in Azure Pipelines. The forge is either Azure Repos (artifact: Boards work item) or GitHub (artifact: issue). They get the same guidance as on GitHub Actions.

**Why this priority**: Azure Pipelines is a supported host ([azure-pipelines guide](../../docs/guides/azure-pipelines.md#step-summary-and-annotations)), but the field incident was on GitHub Actions.

**Independent Test**: Run the User Story 1 fixture under Azure Pipelines, once with Azure Repos and once with a GitHub repository.

**Acceptance Scenarios**:

1. **Given** Azure Repos, **When** a replay conflicts, **Then** the run issue ends with `Resolution steps: https://dev.azure.com/<organization>/<project>/_workitems/edit/<id>` and the Extensions-tab summary has the edge's section.
2. **Given** equivalent state on either forge or host, **When** the outputs are compared, **Then** the sections differ only in the artifact type, its address, and forge-specific FR-008 wording; the warnings additionally differ in the host's command form.

---

### User Story 4 - No artifact can be hosted or recorded (Priority: P2)

Sometimes the repository cannot hold the artifact, or Oiax failed to record it this run. The person on the red run still gets the steps, and is told that this run is the only place they are.

**Why this priority**: This is a real field configuration (#69), and it is where guidance is least reachable today.

**Independent Test**: Use a GitHub fixture with Issues disabled, plus fault injection on artifact list and create.

**Acceptance Scenarios**:

1. **Given** Issues are disabled, **When** a replay conflicts, **Then**:
   - the warning says no durable conflict record can be kept because Issues are disabled, and that the steps are in this run's summary;
   - the section says it is the only guidance for this conflict, lasts only as long as the CI host keeps the run, and explains how to enable Issues;
   - the exit-3 output points to the summary;
   - the existing #69 warnings are unchanged.
2. **Given** artifact create or list fails (for example, a missing Boards work-item type or a labeling failure), **When** the conflict is reported, **Then** the surfaces say the artifact could not be recorded or confirmed this run and will be retried next run. No address is shown or guessed.
3. **Given** no summary is published (a local run or a failed write), **When** the conflict is reported, **Then** nothing claims the steps are in a summary, and the warning and exit-3 output point to the backflow guide.

---

### User Story 5 - Several conflicting edges in one run (Priority: P3)

Several backflow sources can conflict into the same target in one run.

**Why this priority**: This case is less common, but without rules the edges' guidance could be merged together or reordered between runs.

**Independent Test**: Use a graph with two conflicting backflow sources plus one other reported divergence. Repeat the run.

**Acceptance Scenarios**:

1. **Given** two conflicting edges, **When** reconcile runs, **Then** there are two warnings and two self-contained sections, and the exit-3 output names both edges, each with its own pointer.
2. **Given** a conflict plus a divergence that is not a replay conflict (for example, a merge-strategy fence), **When** reconcile runs, **Then** the exit-3 output names both, but only the conflicting edge gets a section.

### Edge Cases

- **Advance write fails, or a staler run leaves the artifact alone**: the existing artifact is still linked.
- **Adopted artifact with a lagging body** (ADR 0008 bounded lag after a target-only advance): the section describes this run's replay, so it can name a newer failing commit than the artifact does.
- **Conflict template fails to render**: the artifact is not written (existing posture). The section is still published per FR-005.
- **Apply later aborts with exit 1**: warnings that were already emitted stay, and sections for recorded conflicts are still published. Exit 1 is unchanged, and there is no exit-3 output.
- **Squash-rollup failing commit**: the built-in squash note appears in the section as it does in the artifact.
- **Branch names**: they are trusted, validated configuration, but Git allows `|`, `` ` ``, `*`, `_`, `<`, `>`, `$`, `&`, or `@` in them. They must not break section structure anywhere Oiax prints them in the summary, including the built-in guidance body: prose and inline code use contained text, while the executable commands keep the literal ref, shell-quoted when it contains shell metacharacters. A custom template that prints branch names is the repository owner's own text over the owner's own configuration, so Oiax does not gate it.

## Requirements *(mandatory)*

### Functional Requirements

**Contained text** (used throughout): an untrusted value (today, the failing commit's subject) after it is capped at 200 runes in total, including any truncation mark (FR-013), stripped of control and bidi-formatting characters, and made inert for the surface it appears on: escaped for host logging commands (FR-014), defanged against linkification in plain text (FR-015), and Markdown-inert in the summary (FR-016).

**Warning and summary**

- **FR-001**: For each edge whose cherry-pick or merge replay conflicts, reconcile MUST emit exactly one conflict warning per run. The capability warnings that FR-008 keeps are separate and are not counted. The conflict warning MUST be emitted after that edge's artifact step finishes or is skipped, and MUST keep today's leading text format ([ADR 0004](../../docs/adr/0004-backflow-execution.md), ADR 0008 A1), with the subject as contained text. It MUST end with:
  - `Resolution steps: <address>` when the artifact is known;
  - otherwise, the FR-008 statement plus where the steps are: this run's summary if one is being published, or the backflow guide address if not.
- **FR-002**: Under a detected CI host with a summary destination, reconcile MUST publish one section per conflicting edge after apply, including when apply later ends with exit 1 (for each conflict recorded before the abort). The sections MUST be appended after the plan summary, which stays unchanged. Each section MUST contain:
  1. the edge, and that no request was created;
  2. the failing commit's full SHA, its contained subject, and the applied count (cherry-pick) or whole-set note (merge);
  3. the artifact link, or the FR-008 statement;
  4. the guidance body (FR-003);
  5. an absolute link to the backflow guide's [When a replay conflicts](../../docs/guides/backflow.md#when-a-replay-conflicts) section.
- **FR-003**: The guidance body MUST be rendered from the same resolved `spec.templates.backflowConflict` slot the artifact uses (the built-in template when unconfigured), over this run's context. A customized artifact and the summary therefore cannot drift, and the template's governance framing ([governance templates guide](../../docs/guides/governance-templates.md)) reaches the failing run, which matters most when no artifact exists. A custom template may omit the steps or refer to "this issue"; that is the template author's choice, and the Oiax-owned header (FR-002 items 1–3) and the guide link (item 5) still give the edge, the failing commit, the artifact link or FR-008 statement, and the playbook location. **Exception (FR-016):** a custom template that reads commit text is not used for the summary; the section uses the built-in body with a note, and the artifact still uses the custom template.
- **FR-004**: The built-in guidance shown in a section MUST keep every element of today's built-in artifact body:
  - the strategy-specific sequence: branch from `origin/<target>`, `cherry-pick -x` or `merge --no-ff`, resolve, continue or commit, then push a branch of your own;
  - open a request into the target, either keeping the `(cherry picked from commit …)` line or landing a merge commit;
  - the warning that `oiax/backflow/…` is Oiax-owned and force-pushed;
  - the `Oiax-Backflow: skip` alternative;
  - the squash note.

  It MUST NOT make artifact-only claims ("this issue closes automatically", "metadata block below"), nor imply that an artifact exists when none does. Because the built-in body contains those claims today and FR-003 renders the section from the same slot, the render context MUST carry a trusted variable naming the surface being rendered (working name `.Surface`, values `artifact` and `summary`). It is additive under ADR 0011 and documented in the templates reference. The built-in template MUST confine its artifact-only sentences to the artifact surface, and SHOULD use a forge-neutral noun for the artifact, since the same text serves Azure Boards work items. Custom templates MAY use the variable; a custom template that does not is rendered as written on both surfaces (FR-003).
- **FR-005**: If the guidance body cannot be rendered, the section MUST still carry FR-002 items 1–3 and 5, plus a note that the configured text failed to render. One run's sections MUST fit the host's summary size limit (GitHub: 1 MiB per step; the Azure Pipelines limit is established in the plan's research, and the smaller of the two applies when it is unknown). When they would not, later sections MUST be reduced to items 1–3 and 5, with a truncation note. Space for every reduced section and the note MUST be reserved before any full guidance body is added, so the written sections can never exceed the remaining budget. If even the reduced sections and the note do not fit (including when the host's summary is already at its limit), no conflict section is written, the summary counts as not written for FR-010, and the warnings and exit-3 output use the artifact or guide pointers. Exit 3 and the artifact's best-effort posture are unchanged.

**Artifact address**

- **FR-006**: The address MUST be the artifact's forge web page:
  - GitHub: `https://github.com/<owner>/<repo>/issues/<number>`
  - Azure Boards: `https://dev.azure.com/<organization>/<project>/_workitems/edit/<id>`

  It MUST be built from the repository identity Oiax already resolved and the artifact identifier, with segments percent-escaped. It MUST never be taken from artifact text or other free text. Whether the Azure Boards address uses the project name or project ID is settled by the plan's research, from what the Azure provider's resolved identity holds.
- **FR-007**: The address MUST be identical on every path where the run knows the artifact: created, adopted, advanced, advance failed, left alone, or canonical after consolidation. An unknown artifact MUST never get a shown or guessed address.

**No artifact (#69)**

- **FR-008**: Every surface MUST distinguish two cases:
  - **(a) The repository cannot host artifacts** (today, GitHub with Issues disabled). The section MUST say it is the only guidance, is not kept beyond the run's retention, and explain how to enable Issues ([operating guide](../../docs/guides/operating.md#keep-issues-enabled-github-backflow-only)).
  - **(b) The artifact could not be recorded or confirmed this run** (a list, create, or render failure, including Azure Boards errors). The surfaces MUST say Oiax retries on the next run.

  The existing capability warnings remain.

**Exit-3 output**

- **FR-009**: On exit 3, the final stderr output MUST begin with the existing `converged with reported divergence`. It MUST then name every contributing edge in plan order, one per line:
  - each conflicting edge with its pointer: the address, the FR-008 statement plus "resolution steps are in this run's summary", or the guide address when no summary was published;
  - each other reported divergence, named as such.

  For example:

  ```text
  converged with reported divergence on 2 edges:
    main -> development: backflow conflict; resolution steps: https://github.com/<owner>/<repo>/issues/177
    hotfix -> development: backflow conflict; no durable conflict record (Issues are disabled); resolution steps are in this run's summary
  ```

  User-facing text calls the artifact a **conflict record** on every surface, because the same words serve GitHub issues and Azure Boards work items; "artifact" remains this specification's term for it. Counts use proper plurals ("1 edge", "2 edges").

- **FR-010**: No surface may point to the summary unless the sections were actually written. The exit-3 output MUST NOT contain untrusted text.

**Ordering and hosts**

- **FR-011**: Warnings, sections, and exit-3 lines MUST follow plan action order (Constitution III), with one per edge and no merging. Equivalent state MUST produce identical output.
- **FR-012**: The CI host selects the command form and the summary destination. The forge selects the artifact type and address:
  - **GitHub Actions**: workflow-command warnings, appended to the step-summary file.
  - **Azure Pipelines**: `##vso[task.logissue type=warning]`, plus a Markdown file in the agent temp directory attached by `##vso[task.uploadsummary]`. The path MUST be escaped the way the plan summary escapes it. The notification-summary upload currently does not escape its path; this feature MUST fix that too, sharing one escaping helper across all three uploads, with a regression test that fails without the fix.

  Content MUST be the same for GitHub Actions with GitHub, Azure Pipelines with GitHub, and Azure Pipelines with Azure Repos. Outside CI there are no annotations or summary, but the plain warning and the exit-3 output still carry the pointer.

**Untrusted text** (commit subjects, per the [templates reference](../../docs/reference/templates.md#variable-context))

- **FR-013**: The failing-commit subject MUST be capped at 200 runes in total, including the `…` truncation mark (199 runes of text plus `…` when truncated), in the shared render context, matching ADR 0011, which also aligns the artifact body. Control and bidi-formatting characters MUST be removed on every surface this feature adds or changes.
- **FR-014**: In every line this feature emits or changes (the warning command, its plain log copy, the exit-3 output), untrusted text MUST NOT end the line or appear in command properties. It also MUST NOT introduce a host command anywhere in the line:
  - GitHub: a leading `::` (after whitespace), or `##[` anywhere, because the runner honors its legacy form mid-line;
  - Azure: `##vso[` or `##[` anywhere, because the agent matches `##vso[` anywhere in a line.

  Message data MUST use host escaping. GitHub: `%`→`%25`, CR→`%0D`, LF→`%0A`. Azure: `%`→`%AZP25`, CR→`%0D`, LF→`%0A`.
- **FR-015**: In plain-text warnings, untrusted text MUST NOT carry a sequence a host could linkify, such as a URL scheme or a leading `www.`. The only links MUST be the addresses Oiax derives.
- **FR-016**: In the summary, untrusted values MUST render as visible, inert text wherever the header or the template places them. They MUST NOT produce any of the following: a link, autolink, or image with an absolute target taken from the value; raw HTML; a mention; a heading, list, or blockquote; a table-cell break; emphasis that spills over; a code span or fence break-out; a line break. This MUST hold on both hosts' summary renderers. Two rules make that hold:
  - **Only Oiax-controlled text prints commit text in the summary.** Oiax cannot contain untrusted values inside an arbitrary custom template: formatting verbs such as `printf "%#v"` bypass any wrapper, conditionals can emit a link only for the real value, and string functions can transform a value past any marker. So a custom `backflowConflict` template is used for the summary **only if its parse tree never reads commit text**: no reference to `.Conflict.Subject` or `.Commits[].Subject`, and no use of a whole value that contains them (`.Conflict`, `.Commits`, `.Commits` elements, `.` or `$` printed or passed to a function). Such a template cannot be steered by commit text, and it takes the same branches on both surfaces by construction. Otherwise the section MUST use the built-in guidance body with a note that the configured text was not shown because it includes commit text. Configuration that loaded before this feature MUST still load (Constitution XII), so this is never a load-time rejection, and the artifact still uses the configured template.
  - **Context-independent containment in Oiax text.** Where Oiax-controlled text (the built-in body and the Oiax-owned header) prints commit text in the summary, the value MUST be inert in any Markdown position, including inside a code span and on a line of its own (Setext underline). Look-alike substitution of Markdown-active characters satisfies this; backslash escaping alone does not (a backtick can close a code span).

  The artifact render keeps receiving the cleaned values without Markdown containment (OOS-004).

**Unchanged**

- **FR-017**: The following MUST NOT change: exit codes, the JSON plan and stdout, artifact identity (marker, `oiax` and `oiax/conflict` labels or tags, title rules), and artifact lifecycle (create, adopt, advance, consolidate, close). No forge writes may be added (Constitution XI, XII; ADR 0008).
- **FR-018**: Credential values MUST NOT appear on any new surface (Constitution VI). The same change MUST update the backflow guide, the GitHub Action and Azure Pipelines summary and annotation sections, the operating guide's exit codes, and the templates reference. If the `reconcile` help text changes, the CLI reference MUST be regenerated, not hand-edited.

### Key Entities

- **Conflict guidance (per edge, per run)**: edge, failing commit (SHA and contained subject), applied count or whole-set flag, strategy, artifact status (known with an address, unhostable, or not recorded or confirmed), and the rendered body. It travels from apply to the CLI and never into the plan document.
- **Conflict artifact**: the ADR 0008 artifact, unchanged. Only its identifier is read, to derive the address.

## Out of Scope *(mandatory)*

- **OOS-001**: No exit-code change. A replay conflict stays exit 3.
- **OOS-002**: No change to artifact identity or lifecycle, and no new forge writes: no comments, and no re-render on adopt.
- **OOS-003**: No new compatibility contract. Warning, summary, and log text are not contracts (Constitution XII), and no configuration key is added.
- **OOS-004**: Untrusted text inside the artifact body is not escaped (ADR 0011 leaves that to the template author). Only the FR-013 cap changes artifact text.
- **OOS-005**: Divergences that are not replay conflicts get no section; they are only named in the exit-3 output. `oiax plan` is unchanged because it cannot foresee replay conflicts.
- **OOS-006**: Oiax does not resolve conflicts, push fixes, open fix requests, mark skips, merge, approve, or send chat or webhook notifications about conflicts.
- **OOS-007**: Known limitations:
  - When no artifact exists, the guidance lasts only as long as the CI host retains the run.
  - No addresses are produced for GitHub Enterprise Server or Azure DevOps Server; the providers target only `github.com` and `dev.azure.com`.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The matrix covers both hosts, each supported forge, and every artifact path: created, adopted, advanced, advance failed, left alone, canonical after consolidation (FR-007), unhostable, and failed to record or confirm (FR-008). In 100% of matrix runs, the warnings and summary show the edge, the failing commit, and either a working artifact link or an explicit no-artifact statement, without the raw log.
- **SC-002** *(live acceptance, not a CI gate)*: At least three people new to Oiax backflow conflicts each find the steps from the failed run page in under two minutes, without the raw log or the issue tracker, and correctly say that the fix goes on a branch of their own.
- **SC-003**: In 100% of template fixtures, section guidance and artifact body differ only in contained untrusted spans, the Oiax-owned header and footer, and text the template places behind the surface variable (FR-004). This holds for the built-in template and for customized templates that do not read commit text, whether or not they use the surface variable; such a template that does not use the surface variable renders identically on both surfaces. A customized template that reads commit text renders the built-in body in the summary (FR-016) and is measured against the built-in fixture.
- **SC-004**: Across at least 50 adversarial subjects (including every User Story 2 vector) on both hosts, there are zero injected commands, set variables, forged warnings, links, images, raw HTML, mentions, or broken summary structures.
- **SC-005**: On both forges, 100% of existing exit-code, plan-document, and artifact-lifecycle acceptance cases are unchanged.
- **SC-006**: For N = 1, 2, and 3 conflicting edges, there are exactly N sections, N warnings, and N named conflict edges, in the same order across 20 repeated runs.
- **SC-007**: Zero credential values appear in any warning, summary, or exit-3 output.

## Assumptions

- **Plan-step assumption**: surfacing the address likely changes the provider-neutral artifact view (identifier, edge, recorded head) or the `forge.Forge` interface. Both providers MUST still pass the shared conformance suite (`internal/forge/forgetest`, conflict-artifact lifecycle), extended to assert the address. Per-edge outcomes also need a path from apply to the CLI outside the plan document.
- Building addresses from repository identity follows precedent: both providers already build notification request links that way.
- No ADR is expected (issue #107). The FR-004 surface variable is additive under ADR 0011 ("fields may be added within v1").
- ADR 0008 and [ADR 0009](../../docs/adr/0009-azure-devops-forge-provider.md) are accepted (#124) and describe implemented behavior. This spec contradicts neither.
- Whether hosts make URLs in plain-text warnings clickable is unverified. Live acceptance confirms it, and the warning carries the full address either way.
- Linux is the supported automation platform ([ADR 0016](../../docs/adr/0016-linux-automation-support.md)). GitHub's 1 MiB per-step summary limit bounds FR-005.
