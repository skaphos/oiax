# Graph Report - agent-ab5e3e065886dbadc  (2026-09-09)

## Corpus Check
- 251 files · ~301,334 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 2254 nodes · 6292 edges · 132 communities (105 shown, 21 thin omitted)
- Extraction: 90% EXTRACTED · 10% INFERRED · 0% AMBIGUOUS · INFERRED: 634 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `280dc6b3`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- reconcile_test.go
- Provider
- github_test.go
- Coordinator
- RenderMarkdown
- context.Context
- Backflow Execution
- fakeForge
- common.sh
- Tasks: [FEATURE NAME]
- ValidOID
- newRepo
- AcceptPolicy
- PromotionGraph Configuration Contract
- Branch Promotion (capability)
- Core Principles
- validGraph
- Feature Specification: [FEATURE NAME]
- Content Equivalence Ladder
- Provider
- Provider
- time.Time
- artifacts.go
- Core Principles
- Implementation Plan: [FEATURE]
- 0012 — Squash merges on the promotion path
- Plan Format Version 1
- writeJSON
- AppendNotificationOrigin
- DeliveryPayloadV1
- [CHECKLIST TYPE] Checklist: [FEATURE NAME]
- BuildPlan
- RequestID
- Drift Policy (forbidden/expected)
- Design proposal in skaphos-resources under tools/oiax/
- GoReleaser Publication
- CI Workflow
- CreateRequest
- Deterministic Backflow Branch Naming
- Token Guidance (GITHUB_TOKEN recursion guard)
- Plan-First Rollout
- config.DefaultPath
- MIT License (c) 2026 Skaphos
- Idempotent Reconciliation
- Pinned Configuration Ref
- Shallow-Clone Equivalence Degradation
- testing.T
- Tasks: Managed Request Notifications
- annotationHandler
- gitHarness
- 0010 — Exported validation and defaulting on the config API
- RepositoryIdentity
- Validate
- .Validate
- Deterministic Backflow Return Branch
- implementation-validation.md
- Source-First Promotion Rollback
- Isolated Environment-Specific Configuration
- Immutable Release Marketplace Constraint
- pkg/api/v1alpha1 (public configuration API)
- Feature Specification: Managed Request Notifications
- MIT License
- Digest
- github.com/skaphos/oiax/tools
- speckit-analyze/SKILL.md
- .withNotificationRuntime
- tmpl.go
- time.Duration
- ParseRemoteURL
- Deploying Oiax from Azure Pipelines
- notification_config_test.go
- Oiax Task-Oriented Guides
- templates.go
- ChangeRequest
- Implementation Plan: Managed Request Notifications
- Notification data model
- 0009 — Azure DevOps forge provider
- Merge-Commit Backflow Strategy
- Request and notification text templates
- io.Writer
- Research: Managed Request Notifications
- Execution Steps
- Run
- Plan
- Notification validation guide
- 0011 — Templatable request text
- 0013 — Add an optional notification contract
- 0014 — Record notification delivery state in Git notes
- LifecycleRequest
- Governance change-record templates
- Specification Quality Checklist: Managed Request Notifications
- TestNotificationSnapshotConformance
- Proposed outbound delivery contract
- Proposed notification presentation contract
- mergeRuntime
- Implementation validation evidence
- TestPlanBackflowReturnSurvivesPromotion
- notificationtest/README.md
- 0017 — Carry the major-version suffix in the Go module path
- RunNotificationCreation
- Serialize
- github.com/spf13/cobra.Command
- NotificationProblem
- azure_pipelines_test.go
- github.go
- FixedFacts
- speckit-plan/SKILL.md
- speckit-specify/SKILL.md
- notificationAzurePull
- speckit-tasks/SKILL.md
- Platform support
- speckit-checklist/SKILL.md
- Upgrading to v2
- speckit-clarify/SKILL.md
- speckit-implement/SKILL.md
- speckit-constitution/SKILL.md
- 0016 — Support production automation on Linux, retain local CLI portability
- 0015 — Extend Oiax ownership to the standard Git notes namespace
- Managed-request notifications
- Proposed provider, state, and CLI contracts
- speckit-taskstoissues/SKILL.md
- seedMergeAndEmptyBackflow
- Detect
- Graph
- errNoResponse
- github.com/skaphos/oiax/v2

## God Nodes (most connected - your core abstractions)
1. `gitHarness()` - 81 edges
2. `testGraph()` - 80 edges
3. `checkout()` - 66 edges
4. `writeJSON()` - 58 edges
5. `newProvider()` - 53 edges
6. `newRepo()` - 48 edges
7. `writeCommit()` - 45 edges
8. `Provider` - 43 edges
9. `Runner` - 41 edges
10. `LedgerV1` - 40 edges

## Surprising Connections (you probably didn't know these)
- `BuildPlan()` --conceptually_related_to--> `Reconciliation Loop`  [INFERRED]
  internal/engine/plan.go → docs/architecture.md
- `Action Pinned Config Ref` --semantically_similar_to--> `Single Pinned Configuration Ref`  [INFERRED] [semantically similar]
  action.yml → docs/adr/0003-pinned-configuration-ref.md
- `CI Workflow` --semantically_similar_to--> `Local Validation Task Suite`  [INFERRED] [semantically similar]
  .github/workflows/ci.yml → Taskfile.yml
- `Pinned Declarative Configuration` --semantically_similar_to--> `Single Pinned Configuration Ref`  [INFERRED] [semantically similar]
  .github/copilot-instructions.md → docs/adr/0003-pinned-configuration-ref.md
- `Backflow` --semantically_similar_to--> `Backflow Execution`  [INFERRED] [semantically similar]
  AGENTS.md → docs/adr/0004-backflow-execution.md

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Automated Release Pipeline** — github_workflows_release_please_automation, github_workflows_release_please_annotated_tag, github_workflows_release_goreleaser_publication, github_workflows_release_floating_major_tag [EXTRACTED 1.00]
- **Backflow Execution Model** — adr0006_merge_commit_backflow, docs_guides_backflow_deterministic_request, docs_guides_backflow_conflict_handling [EXTRACTED 1.00]
- **CI Quality Gate Suite** — github_workflows_ci_dco_gate, github_workflows_ci_reuse_gate, github_workflows_ci_cross_platform_tests, github_workflows_ci_static_analysis, github_workflows_ci_generated_artifact_check, github_workflows_ci_snapshot_build [EXTRACTED 1.00]
- **Convergent Backflow Execution** — adr0004_deterministic_return_branch, adr0004_identity_ladder, adr0004_ephemeral_worktree, adr0004_conflict_divergence, adr0004_supersede_stale_request [EXTRACTED 1.00]
- **Reconciliation Layer Model** — docs_architecture_pure_reconciliation_layers, docs_code_map_engine_core, docs_code_map_reconcile_layer, docs_code_map_git_layer [EXTRACTED 1.00]

## Communities (132 total, 21 thin omitted)

### Community 0 - "reconcile_test.go"
Cohesion: 0.13
Nodes (39): NewLogger(), conflictHarness(), findLogRecord(), TestApplyBackflowConflictAdoptsSameHead(), TestApplyBackflowConflictAdvancesHeadInPlace(), TestApplyBackflowConflictBestEffortForgeErrorPreservesExit3(), TestApplyBackflowConflictConsolidatesDuplicates(), TestApplyBackflowConflictCreatesArtifact() (+31 more)

### Community 1 - "Provider"
Cohesion: 0.17
Nodes (5): repoSettings, Provider, issueNumber(), prNumber(), understoodMarker()

### Community 2 - "github_test.go"
Cohesion: 0.09
Nodes (70): issueSpec, prSpec, assertAuth(), assertNoToken(), decode(), Provider, newProvider(), runGit() (+62 more)

### Community 3 - "Coordinator"
Cohesion: 0.06
Nodes (49): BranchState, Equivalence, capWriter, CherryPickConflict, MergeConflict, bytes.Buffer, backflowToReturn(), EvaluateEdge() (+41 more)

### Community 4 - "RenderMarkdown"
Cohesion: 0.18
Nodes (22): actionVerb(), edgeSummaryText(), exclusionCounts(), mdCell(), RenderMarkdown(), RenderText(), returnedSubjects(), mergePlan() (+14 more)

### Community 5 - "context.Context"
Cohesion: 0.17
Nodes (4): context.Context, os/exec.Cmd, gitCommand(), Runner

### Community 6 - "Backflow Execution"
Cohesion: 0.05
Nodes (42): Downloaded Artifact Verification, Oiax Composite GitHub Action, Action Pinned Config Ref, Git Ref Preparation, Release Binary Download, Human-in-the-Loop Steering, Adopt the Name Oiax, Tiller Ecosystem Collision (+34 more)

### Community 7 - "fakeForge"
Cohesion: 0.09
Nodes (12): fakeForge, artifactID(), htmlBody(), BranchPush, ConflictArtifact, ConflictArtifactID, ConflictArtifactSpec, MergeMethods (+4 more)

### Community 8 - "common.sh"
Cohesion: 0.13
Nodes (27): check-prerequisites.sh script, check_dir(), check_file(), find_specify_root(), format_speckit_command(), get_current_branch(), get_feature_paths(), get_invoke_separator() (+19 more)

### Community 9 - "Tasks: [FEATURE NAME]"
Cohesion: 0.07
Nodes (26): Dependencies & Execution Order, Format: `[ID] [P?] [Story] Description`, Implementation for User Story 1, Implementation for User Story 2, Implementation for User Story 3, Implementation Strategy, Incremental Delivery, MVP First (User Story 1 Only) (+18 more)

### Community 10 - "ValidOID"
Cohesion: 0.22
Nodes (10): notificationCommit, Provider, commitSummaries(), Provider, EventRevision, BoundSnapshot(), TestBoundSnapshot(), CommitSnapshot (+2 more)

### Community 11 - "newRepo"
Cohesion: 0.07
Nodes (62): gitInvocationLog, NotesOptions, Provider, Provider, SetNotesCommandTimeout(), newRepo(), oidLike(), requireGit() (+54 more)

### Community 12 - "AcceptPolicy"
Cohesion: 0.25
Nodes (26): TestNotificationResultStatusAndOutcomeError(), DeliveryKey(), modelEvent(), modelLedger(), modelPolicy(), modelRepo(), modelRevision(), modelTime() (+18 more)

### Community 13 - "PromotionGraph Configuration Contract"
Cohesion: 0.50
Nodes (4): Backflow Policy Configuration, PromotionGraph Configuration Contract, Environments PromotionGraph Fixture, Strict Configuration Validation

### Community 14 - "Branch Promotion (capability)"
Cohesion: 0.17
Nodes (12): Skaphos Glossary Discipline (branch promotion vs Promotion vs backflow), Conventional Commits, Signed commits + DCO sign-off, Branch Promotion (capability), argoproj-labs/gitops-promoter (prior art), Kargo (prior art), Promotion Graph (DAG model), release-please (prior art / inspiration) (+4 more)

### Community 15 - "Core Principles"
Cohesion: 0.11
Nodes (17): Core Principles, Development Workflow and Quality Gates, Engineering Constraints, Governance, I. Explicit State Over Implicit Behavior, II. Git Is the Durable Desired-State Boundary, III. Deterministic, Reconstructible Operation, IV. Control-Plane Conventions, Never Obscured (+9 more)

### Community 16 - "validGraph"
Cohesion: 0.18
Nodes (19): notificationPolicy(), TestNotificationDefaultsAndRoundTrip(), TestNotificationPolicyEnabled(), TestNotificationTemplateSourceDiagnosticPaths(), TestNotificationValidation(), TestDefault(), TestDefaultIsIdempotent(), TestDefaultMergeStrategyExpectedMergeMethod() (+11 more)

### Community 17 - "Feature Specification: [FEATURE NAME]"
Cohesion: 0.14
Nodes (13): Assumptions, Edge Cases, Feature Specification: [FEATURE NAME], Functional Requirements, Key Entities *(include if feature involves data)*, Measurable Outcomes, Out of Scope *(mandatory)*, Requirements *(mandatory)* (+5 more)

### Community 18 - "Content Equivalence Ladder"
Cohesion: 0.09
Nodes (27): Git 2.45 Runtime Contract, Git Runner Shell-Out, Agent Safety Rules (do not violate), ADR 0001: Adopt the name Oiax, Rationale: Tiller collided with Helm v2's Tiller in the target ecosystem; Oiax is the literal Greek for tiller and keeps the hand-on-the-helm intent, ADR 0002: Detect divergence by content, not ancestry, Rationale: squash/rebase merges rewrite SHAs; ancestry-only detection leaves edges permanently diverged and PR creation fails with HTTP 422; a private state database would violate the no-control-plane posture, ADR 0003: Read configuration from a pinned ref (+19 more)

### Community 19 - "Provider"
Cohesion: 0.13
Nodes (7): net/http.Client, net/http.Response, Provider, looksLikeJWT(), parseAPIError(), TestLooksLikeJWT(), parseAPIError()

### Community 20 - "Provider"
Cohesion: 0.14
Nodes (14): adoCommitRef, adoPull, adoPullList, forkRef, gitRef, propertiesCollection, refList, refUpdateResult (+6 more)

### Community 21 - "time.Time"
Cohesion: 0.14
Nodes (24): notificationCursor, notificationInterval, time.Time, DeliveryRecord, LedgerV1, OutcomeCode, ScanProgress, ValidDigest() (+16 more)

### Community 22 - "artifacts.go"
Cohesion: 0.13
Nodes (16): policyConfiguration, policyList, policyScope, policySettings, wiqlResult, wiState, wiStates, workItem (+8 more)

### Community 23 - "Core Principles"
Cohesion: 0.18
Nodes (10): Core Principles, Governance, [PRINCIPLE_1_NAME], [PRINCIPLE_2_NAME], [PRINCIPLE_3_NAME], [PRINCIPLE_4_NAME], [PRINCIPLE_5_NAME], [PROJECT_NAME] Constitution (+2 more)

### Community 24 - "Implementation Plan: [FEATURE]"
Cohesion: 0.22
Nodes (8): Complexity Tracking, Constitution Check, Documentation (this feature), Implementation Plan: [FEATURE], Project Structure, Source Code (repository root), Summary, Technical Context

### Community 25 - "0012 — Squash merges on the promotion path"
Cohesion: 0.29
Nodes (6): 0012 — Squash merges on the promotion path, Consequences, Context, Decision, Links, Options considered

### Community 26 - "Plan Format Version 1"
Cohesion: 0.40
Nodes (6): Prompt Hotfix Backflow, Managed Request Lifecycle, Plan Diagnostics, Plan Action Schema, Edge Diagnostic Schema, Plan Format Version 1

### Community 27 - "writeJSON"
Cohesion: 0.08
Nodes (48): adoFake, adoFakePull, adoFakeWI, pullSpec, ghFake, ghFakeIssue, ghFakePull, net/http.HandlerFunc (+40 more)

### Community 28 - "AppendNotificationOrigin"
Cohesion: 0.35
Nodes (12): AppendNotificationOrigin(), NotificationOriginMatches(), originBlock(), ParseNotificationOrigin(), FuzzNotificationOrigin(), testNotificationOrigin(), TestNotificationOriginHeadVerifiedIsOptional(), TestNotificationOriginRejectsAmbiguousAndMalformedBlocks() (+4 more)

### Community 29 - "DeliveryPayloadV1"
Cohesion: 0.08
Nodes (35): Client, dialContextFunc, failTransport, lookupNetIPFunc, net/netip.Addr, net/url.URL, NewFixtureClient(), adapterPayload() (+27 more)

### Community 30 - "[CHECKLIST TYPE] Checklist: [FEATURE NAME]"
Cohesion: 0.40
Nodes (4): [Category 1], [Category 2], [CHECKLIST TYPE] Checklist: [FEATURE NAME], Notes

### Community 31 - "BuildPlan"
Cohesion: 0.07
Nodes (62): BackflowPolicy, Branch, Expectations, Promotion, IsDeprecatedAPIVersion(), Load(), Parse(), safeParseError() (+54 more)

### Community 32 - "RequestID"
Cohesion: 0.11
Nodes (11): notificationCapabilityTrap, fakeForge, Provider, RequestID, Provider, LifecyclePage, LifecycleQuery, fakeForge (+3 more)

### Community 35 - "GoReleaser Publication"
Cohesion: 0.24
Nodes (11): Floating Major Action Tag, GoReleaser Publication, Release Tag Monotonicity Guard, Annotated Immutable SemVer Tag, Release Please Automation, Release Bot GitHub App Token, Release PR Label Reconciliation, Release Checksums (+3 more)

### Community 36 - "CI Workflow"
Cohesion: 0.24
Nodes (10): Skaphos Contribution Governance, Cross-Platform Test Matrix, DCO Sign-Off Gate, Generated Artifact Drift Check, REUSE License Gate, GoReleaser Snapshot Build, Staticcheck and Govulncheck, CI Workflow (+2 more)

### Community 37 - "CreateRequest"
Cohesion: 0.17
Nodes (10): CreateDisposition, NotificationNotesProvider, SnapshotCase, CreateRequest, RunNotificationSnapshots(), CreateOutcome, LifecycleReader, SnapshotReader (+2 more)

### Community 46 - "testing.T"
Cohesion: 0.09
Nodes (40): actionMetadata, TestRunExitCodes(), testing.T, TestPublishedActionRunnerContract(), readRepoYAML(), TestCIPlatformSupportTiers(), TestLinuxTasksHaveExplicitPackageDeadlines(), TestNotificationVerificationKeepsLinuxBudgetsAndCoverage() (+32 more)

### Community 47 - "Tasks: Managed Request Notifications"
Cohesion: 0.10
Nodes (20): Dependencies and execution order, Implementation, Implementation, Implementation, Implementation, Implementation strategy and completion accounting, Parallel execution examples, Phase 1: Setup (+12 more)

### Community 48 - "annotationHandler"
Cohesion: 0.24
Nodes (9): log/slog.Attr, log/slog.Handler, log/slog.Level, log/slog.Record, escapeAnnotation(), escapeAzureAnnotation(), formatAnnotation(), annotationHandler (+1 more)

### Community 49 - "gitHarness"
Cohesion: 0.12
Nodes (57): BackflowBranchName(), TestBackflowBranchName(), TestNotificationBackflowRetainsPartialCreation(), checkout(), commitOn(), countBackflowOutcome(), countReports(), gitExec() (+49 more)

### Community 50 - "0010 — Exported validation and defaulting on the config API"
Cohesion: 0.29
Nodes (6): 0010 — Exported validation and defaulting on the config API, Consequences, Context, Decision, Links, Options considered

### Community 51 - "RepositoryIdentity"
Cohesion: 0.14
Nodes (13): log/slog.Logger, sync.Once, EventID(), RepositoryIdentity, GraphKey(), NewLedger(), TestNotificationIdentity(), seededNotificationLedger() (+5 more)

### Community 52 - "Validate"
Cohesion: 0.14
Nodes (26): Decode(), Encode(), eventKind(), requestKind(), safeText(), codecLedger(), FuzzNotificationLedgerCodec(), populatedLedger() (+18 more)

### Community 53 - ".Validate"
Cohesion: 0.25
Nodes (8): findCycle(), Branch, Promotion, PromotionGraph, sortedBranchNames(), validateRefName(), validateRequestTemplate(), validateTemplatePath()

### Community 54 - "Deterministic Backflow Return Branch"
Cohesion: 0.67
Nodes (3): Deterministic Backflow Return Branch, Event-Driven Concurrency Without Locks, Supersede Stale Backflow Request

### Community 61 - "Feature Specification: Managed Request Notifications"
Cohesion: 0.12
Nodes (16): Assumptions, Clarifications, Edge Cases, Feature Specification: Managed Request Notifications, Functional Requirements, Key Entities, Measurable Outcomes, Out of Scope *(mandatory)* (+8 more)

### Community 63 - "Digest"
Cohesion: 0.19
Nodes (17): batchAttempts(), batchLedger(), TestNotificationBatchClaimAndRecord(), TestNotificationBatchCompetingRunsAndLateAcceptance(), TestNotificationBatchSkipsRecordsThatAreNotDue(), TestNotificationCheckBatchSend(), Digest(), BatchID() (+9 more)

### Community 65 - "speckit-analyze/SKILL.md"
Cohesion: 0.08
Nodes (25): 1. Initialize Analysis Context, 2. Load Artifacts (Progressive Disclosure), 3. Build Semantic Models, 4. Detection Passes (Token-Efficient Analysis), 5. Severity Assignment, 6. Produce Compact Analysis Report, 7. Provide Next Actions, 8. Offer Remediation (+17 more)

### Community 66 - ".withNotificationRuntime"
Cohesion: 0.28
Nodes (3): NotificationRuntime, Coordinator, newNotificationOperationID()

### Community 67 - "tmpl.go"
Cohesion: 0.12
Nodes (37): text/template.FuncMap, text/template.Template, tmplCommits(), checkBodySafety(), compileTemplate(), Default(), execute(), funcMap() (+29 more)

### Community 68 - "time.Duration"
Cohesion: 0.13
Nodes (16): apiError, capWriter, errNoResponse, net/http.Header, time.Duration, isDuplicateActiveRequest(), retryableStatus(), retryDelay() (+8 more)

### Community 69 - "ParseRemoteURL"
Cohesion: 0.26
Nodes (12): Repo, orgFromCollectionURI(), ParseRemoteURL(), pathSegments(), repoFromEnv(), ResolveRepo(), splitRemote(), TestParseRemoteURL() (+4 more)

### Community 70 - "Deploying Oiax from Azure Pipelines"
Cohesion: 0.14
Nodes (14): Azure Repos, Choosing a mode, Connecting Azure DevOps to GitHub, Create the service connection, Deploying Oiax from Azure Pipelines, `fetchDepth: 0` is not optional, Next steps, Parameters (+6 more)

### Community 71 - "notification_config_test.go"
Cohesion: 0.30
Nodes (3): exitCodeError, TestNotificationLoadedConfigRejectsOversizedSource(), TestNotificationLoadedConfigValidatesClosedTemplates()

### Community 72 - "Oiax Task-Oriented Guides"
Cohesion: 0.17
Nodes (15): CLI Exit-Code Contract, Example request-text templates, Oiax Installation Artifacts, Agent Installation Confirmation Gate, Backflow Hotfix Return, Plan-First Repository Adoption, Promotion Graph Quickstart, CI-Triggering Installation Token (+7 more)

### Community 73 - "templates.go"
Cohesion: 0.17
Nodes (17): reflect.Type, strings.Builder, text/template/parse.Node, TemplateSet, overlayTemplate(), renderTemplatePair(), ResolveTemplates(), FuzzNotificationTemplate() (+9 more)

### Community 74 - "ChangeRequest"
Cohesion: 0.26
Nodes (17): RequestState, ChangeRequest, RequestType, Forge, RequestFilter, assertAscending(), containsID(), mustCreate() (+9 more)

### Community 75 - "Implementation Plan: Managed Request Notifications"
Cohesion: 0.17
Nodes (12): Complexity Tracking, Constitution Check, Documentation (this feature), Implementation Plan: Managed Request Notifications, Operational risks and rollback, Phase 0 — Research conclusions, Phase 1 — Design and implementation sequence, Project Structure (+4 more)

### Community 76 - "Notification data model"
Cohesion: 0.18
Nodes (11): Configuration, Configuration revision ordering, Delivery and claims, Destination state and routing lifetime, Event, Ledger snapshot, Notification data model, Repository and managed request (+3 more)

### Community 77 - "0009 — Azure DevOps forge provider"
Cohesion: 0.20
Nodes (9): 0009 — Azure DevOps forge provider, Authentication and the token, Consequences, Context, Decision, Links, Marker storage on a managed request, Options considered (+1 more)

### Community 78 - "Merge-Commit Backflow Strategy"
Cohesion: 0.25
Nodes (8): Live Merge-Method Fence, Merge-Commit Backflow Strategy, Skip-in-Range Fence, Conflict Issue Marker-and-Label Identity, Durable Backflow Conflict Artifact, Lock-Free Conflict Issue Convergence, Backflow Conflict Handling, Deterministic Backflow Request Lifecycle

### Community 79 - "Request and notification text templates"
Cohesion: 0.20
Nodes (10): Configuration keys, Functions, Notification templates, Rendering rules and constraints, Request and notification text templates, `spec.templates.backflowMergeMessage`, `spec.templates.promotion`, `.backflow`, `.backflowConflict`, Untrusted variables (+2 more)

### Community 80 - "io.Writer"
Cohesion: 0.18
Nodes (11): totals, io.Reader, io.Writer, renderNotificationPreview(), TestNotificationPreviewDocument(), RenderJSON(), errWriter, check() (+3 more)

### Community 82 - "Research: Managed Request Notifications"
Cohesion: 0.25
Nodes (8): 1. Integration boundaries, 2. Durable delivery state and concurrency, 3. Activation, event discovery, and creation provenance, 4. Retry policy and bounded work, 5. Transport choice and prior art, 6. Presentation templates and environment language, 7. Public compatibility and safety, Research: Managed Request Notifications

### Community 83 - "Execution Steps"
Cohesion: 0.12
Nodes (15): 1. Initialize Convergence Context, 2. Load Artifacts (Progressive Disclosure), 3. Build the Intent Inventory, 4. Assess the Codebase and Classify Findings, 5. Assign Severity, 6. Present the In-Session Findings Summary, 7. Append Convergence Tasks (or report converged), 8. Provide Next Actions (Handoff) (+7 more)

### Community 84 - "Run"
Cohesion: 0.06
Nodes (68): notificationBinaryFixture, notificationReceived, main(), run(), testing.TB, captureProcessStreams(), TestExecuteDivergenceMessage(), TestExecuteExitCodes() (+60 more)

### Community 85 - "Plan"
Cohesion: 0.31
Nodes (9): forgeKind, planExitCode(), planReportsDivergence(), TestPlanExitCode(), resolveForgeKind(), writeAzureSummary(), writeGitHubSummary(), writeStepSummary() (+1 more)

### Community 86 - "Notification validation guide"
Cohesion: 0.29
Nodes (7): Build and fixture validation, Configure and preview, Failure and concurrency matrix, Lifecycle and presentation acceptance, Notification validation guide, Prerequisites, Recipient-visible validation and rollback

### Community 87 - "0011 — Templatable request text"
Cohesion: 0.33
Nodes (6): 0011 — Templatable request text, Consequences, Context, Decision, Links, Options considered

### Community 88 - "0013 — Add an optional notification contract"
Cohesion: 0.33
Nodes (6): 0013 — Add an optional notification contract, Consequences, Context, Decision, Links, Options considered

### Community 89 - "0014 — Record notification delivery state in Git notes"
Cohesion: 0.33
Nodes (6): 0014 — Record notification delivery state in Git notes, Consequences, Context, Decision, Links, Options considered

### Community 90 - "LifecycleRequest"
Cohesion: 0.12
Nodes (25): CreationEvent(), EventAdmissionTime(), TestCreationEventRequiresOriginalProvenance(), EventV1, LifecycleRequest, RequestV1, eventForRequest(), MergeEvent() (+17 more)

### Community 91 - "Governance change-record templates"
Cohesion: 0.33
Nodes (6): A minimal change-record setup, Backflow and conflict records, Governance change-record templates, Next steps, Untrusted input, one more time, What renders when

### Community 92 - "Specification Quality Checklist: Managed Request Notifications"
Cohesion: 0.33
Nodes (5): Content Quality, Feature Readiness, Notes, Requirement Completeness, Specification Quality Checklist: Managed Request Notifications

### Community 93 - "TestNotificationSnapshotConformance"
Cohesion: 0.30
Nodes (12): fixtureCommits(), TestCreateRequestVerifiesOriginHead(), TestNotificationCreationSnapshot(), TestNotificationImmutableMergeFallback(), testNotificationOrigin(), TestNotificationSnapshotConformance(), notificationPull(), serveNotificationIdentity() (+4 more)

### Community 94 - "Proposed outbound delivery contract"
Cohesion: 0.40
Nodes (5): Generic webhook schema v1, Outcomes and retries, Proposed outbound delivery contract, Slack incoming webhook, Teams Workflows

### Community 95 - "Proposed notification presentation contract"
Cohesion: 0.21
Nodes (8): Analysis remediation record, Document verification, Resolved gate: C1 / T001, Closed context, Configuration and precedence, Delivery encoding, Immutable facts and retries, Proposed notification presentation contract

### Community 96 - "mergeRuntime"
Cohesion: 0.07
Nodes (55): sync.Mutex, LedgerStore, Sender, Snapshot, Transition, Clock, MemoryStore, NewClock() (+47 more)

### Community 97 - "Implementation validation evidence"
Cohesion: 0.14
Nodes (14): Earlier checkpoint — basic merge-alert integration, Earlier checkpoint — lifecycle completeness and bounded delivery, Earlier checkpoint — optional creation alerts and recovery, Earlier foundation commands and results, Earlier foundation handoff (historical), Earlier increment — immutable commit enrichment, Earlier increment — read-only preview and safe recovery, Earlier increment — reproducible verification gates (+6 more)

### Community 98 - "TestPlanBackflowReturnSurvivesPromotion"
Cohesion: 0.29
Nodes (6): TestPlanBackflowReturnSurvivesPromotion(), assertExclusionReason(), TestPlanBackflowAlreadyReturnedByContent(), TestPlanBackflowProvenanceMatchesSquashedReturn(), TestPlanBackflowProvenanceMatchesTrailerNotProse(), TestPlanBackflowSkipTrailerSuppresses()

### Community 102 - "0017 — Carry the major-version suffix in the Go module path"
Cohesion: 0.33
Nodes (6): 0017 — Carry the major-version suffix in the Go module path, Alternatives, Compatibility and rollout, Consequences, Context, Decision

### Community 103 - "RunNotificationCreation"
Cohesion: 0.28
Nodes (4): CreationScenario, TestNotificationCreationConformance(), RunNotificationCreation(), TestNotificationCreationConformance()

### Community 104 - "Serialize"
Cohesion: 0.18
Nodes (24): testing.F, blockBounds(), hasOiaxKey(), Parse(), parseInner(), Replace(), Sanitize(), Serialize() (+16 more)

### Community 105 - "github.com/spf13/cobra.Command"
Cohesion: 0.18
Nodes (23): loadedConfig, options, versionInfo, github.com/spf13/cobra.Command, newGenCommand(), writeCommandReference(), newGraphCommand(), printGraph() (+15 more)

### Community 107 - "NotificationProblem"
Cohesion: 0.19
Nodes (11): TestNotificationSummaryLabelsGlobalAndDestinationProblems(), writeNotificationSummary(), NotificationDiagnostic, NotificationProblem(), TestNotificationConfigurationActionDistinguishesExchangedRequests(), TestNotificationDiagnosticsAreSafe(), TestNotificationDiagnosticsCarryReceiverStatus(), TestNotificationDiagnosticScope() (+3 more)

### Community 109 - "github.go"
Cohesion: 0.14
Nodes (21): apiError, apiError, ghIssue, ghLabel, ghPull, ghRef, ghRepo, ghUser (+13 more)

### Community 110 - "FixedFacts"
Cohesion: 0.38
Nodes (10): CleanText(), FixedFacts(), RenderBuiltin(), SafeDisplayText(), SafeRequestURL(), renderFixture(), TestFixedFactsIncludesRequiredIdentityAndCompleteness(), TestFixedFactsPreservesCompleteAzureRepositoryIdentity() (+2 more)

### Community 111 - "speckit-plan/SKILL.md"
Cohesion: 0.18
Nodes (10): Completion Report, Done When, Key rules, Mandatory Post-Execution Hooks, Outline, Phase 0: Outline & Research, Phase 1: Design & Contracts, Phases (+2 more)

### Community 112 - "speckit-specify/SKILL.md"
Cohesion: 0.18
Nodes (10): Completion Report, Done When, For AI Generation, Mandatory Post-Execution Hooks, Outline, Pre-Execution Checks, Quick Guidelines, Section Requirements (+2 more)

### Community 113 - "notificationAzurePull"
Cohesion: 0.43
Nodes (6): TestNotificationSnapshotConformance(), notificationAzurePull(), serveNotificationAzureIdentity(), TestNotificationLifecycleMovementAndDetailFailuresStayIncomplete(), TestNotificationLifecyclePartitionsFrozenIntervals(), TestNotificationLifecycleRejectsUnprovableIntervals()

### Community 114 - "speckit-tasks/SKILL.md"
Cohesion: 0.18
Nodes (10): Checklist Format (REQUIRED), Completion Report, Done When, Mandatory Post-Execution Hooks, Outline, Phase Structure, Pre-Execution Checks, Task Generation Rules (+2 more)

### Community 115 - "Platform support"
Cohesion: 0.67
Nodes (3): Contributor checks, Migration, Platform support

### Community 116 - "speckit-checklist/SKILL.md"
Cohesion: 0.25
Nodes (7): Anti-Examples: What NOT To Do, Checklist Purpose: "Unit Tests for English", Example Checklist Types & Sample Items, Execution Steps, Post-Execution Checks, Pre-Execution Checks, User Input

### Community 117 - "Upgrading to v2"
Cohesion: 0.25
Nodes (8): 1. Check the runner and keep the existing graph, 2. Upgrade the wrapper and binary together, 3. Enable notifications separately, Azure Pipelines, GitHub Action, If you need to return to v1, Standalone CLI, Upgrading to v2

### Community 118 - "speckit-clarify/SKILL.md"
Cohesion: 0.29
Nodes (6): Completion Report, Done When, Mandatory Post-Execution Hooks, Outline, Pre-Execution Checks, User Input

### Community 119 - "speckit-implement/SKILL.md"
Cohesion: 0.29
Nodes (6): Completion Report, Done When, Mandatory Post-Execution Hooks, Outline, Pre-Execution Checks, User Input

### Community 120 - "speckit-constitution/SKILL.md"
Cohesion: 0.33
Nodes (5): Outline, Post-Execution Checks, Pre-Execution Checks, Scope Guard, User Input

### Community 121 - "0016 — Support production automation on Linux, retain local CLI portability"
Cohesion: 0.33
Nodes (6): 0016 — Support production automation on Linux, retain local CLI portability, Alternatives, Compatibility and rollout, Consequences, Context, Decision

### Community 122 - "0015 — Extend Oiax ownership to the standard Git notes namespace"
Cohesion: 0.33
Nodes (6): 0015 — Extend Oiax ownership to the standard Git notes namespace, Consequences, Context, Decision, Links, Options considered

### Community 123 - "Managed-request notifications"
Cohesion: 0.33
Nodes (6): Changing destination identity and rollback, Customize wording, Delivery and recovery, Managed-request notifications, Set up a destination, Validate, preview, activate

### Community 124 - "Proposed provider, state, and CLI contracts"
Cohesion: 0.33
Nodes (6): CLI and plan preview, Delivery payload handoff, Ledger store, Lifecycle observation, Notification-origin wire format, Proposed provider, state, and CLI contracts

### Community 125 - "speckit-taskstoissues/SKILL.md"
Cohesion: 0.40
Nodes (4): Outline, Post-Execution Checks, Pre-Execution Checks, User Input

### Community 126 - "seedMergeAndEmptyBackflow"
Cohesion: 0.67
Nodes (4): containsSHA(), seedMergeAndEmptyBackflow(), TestPlanCherryPickBackflowFiltersMergeAndEmptyCommits(), TestPlanMergeBackflowReturnsMergeAndEmptyCommitsWholesale()

### Community 127 - "Detect"
Cohesion: 0.50
Nodes (3): Kind, Detect(), TestDetect()

### Community 128 - "Graph"
Cohesion: 0.40
Nodes (6): Branch, Graph, Promotion, diamondConfig(), diamondGraph(), TestBackflowMultipleIncomingEdgesReturnsCompleteSet()

## Knowledge Gaps
- **384 isolated node(s):** `common.sh script`, `github.com/skaphos/oiax/v2`, `actionMetadata`, `pipelineTemplate`, `versionInfo` (+379 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 525 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **21 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `BuildPlan()` connect `BuildPlan` to `reconcile_test.go`, `Content Equivalence Ladder`, `Coordinator`, `Plan`?**
  _High betweenness centrality (0.171) - this node is a cross-community bridge._
- **Why does `Reconciliation Loop` connect `Content Equivalence Ladder` to `BuildPlan`?**
  _High betweenness centrality (0.167) - this node is a cross-community bridge._
- **Are the 7 inferred relationships involving `gitHarness()` (e.g. with `TestPlanBackflowReturnSurvivesPromotion()` and `TestNotificationBackflowRetainsPartialCreation()`) actually correct?**
  _`gitHarness()` has 7 INFERRED edges - model-reasoned connections that need verification._
- **Are the 7 inferred relationships involving `testGraph()` (e.g. with `TestPlanBackflowReturnSurvivesPromotion()` and `TestNotificationBackflowRetainsPartialCreation()`) actually correct?**
  _`testGraph()` has 7 INFERRED edges - model-reasoned connections that need verification._
- **Are the 6 inferred relationships involving `checkout()` (e.g. with `TestPlanBackflowReturnSurvivesPromotion()` and `TestNotificationBackflowRetainsPartialCreation()`) actually correct?**
  _`checkout()` has 6 INFERRED edges - model-reasoned connections that need verification._
- **What connects `common.sh script`, `github.com/skaphos/oiax/v2`, `actionMetadata` to the rest of the system?**
  _384 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `reconcile_test.go` be split into smaller, more focused modules?**
  _Cohesion score 0.12926829268292683 - nodes in this community are weakly interconnected._