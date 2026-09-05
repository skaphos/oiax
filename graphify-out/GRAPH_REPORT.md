# Graph Report - oiax  (2026-09-05)

## Corpus Check
- 181 files · ~234,198 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 1797 nodes · 4992 edges · 101 communities (81 shown, 20 thin omitted)
- Extraction: 92% EXTRACTED · 8% INFERRED · 0% AMBIGUOUS · INFERRED: 404 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `a28e2a90`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- testing.T
- Provider
- github_test.go
- Coordinator
- Plan
- context.Context
- Backflow Execution
- fakeForge
- common.sh
- Tasks: [FEATURE NAME]
- Run
- newRepo
- AcceptPolicy
- PromotionGraph Configuration Contract
- Branch Promotion (capability)
- Core Principles
- git.go
- Feature Specification: [FEATURE NAME]
- Content Equivalence Ladder
- annotationHandler
- net/http.Header
- Pinned Configuration Ref (--config-ref)
- artifacts.go
- Core Principles
- Implementation Plan: [FEATURE]
- 0012 — Squash merges on the promotion path
- Plan Format Version 1
- writeJSON
- model.go
- DeliveryPayloadV1
- [CHECKLIST TYPE] Checklist: [FEATURE NAME]
- BuildPlan
- RequestID
- Drift Policy (forbidden/expected)
- Design proposal in skaphos-resources under tools/oiax/
- GoReleaser Publication
- CI Workflow
- ChangeRequest
- Deterministic Backflow Branch Naming
- Token Guidance (GITHUB_TOKEN recursion guard)
- Plan-First Rollout
- config.DefaultPath
- MIT License (c) 2026 Skaphos
- Idempotent Reconciliation
- Pinned Configuration Ref
- Shallow-Clone Equivalence Degradation
- fakeForge
- Tasks: Managed Request Notifications
- Provider
- v1/types.go
- 0010 — Exported validation and defaulting on the config API
- .do
- Validate
- time.Time
- Deterministic Backflow Return Branch
- remediation.md
- Source-First Promotion Rollback
- Isolated Environment-Specific Configuration
- Immutable Release Marketplace Constraint
- pkg/api/v1alpha1 (public configuration API)
- Feature Specification: Managed Request Notifications
- MIT License
- github.com/skaphos/oiax
- github.com/skaphos/oiax/tools
- Serialize
- ValidOID
- tmpl.go
- azuredevops/client.go
- ParseRemoteURL
- Deploying Oiax from Azure Pipelines
- github.go
- Oiax Task-Oriented Guides
- LedgerV1
- RunLifecycle
- Implementation Plan: Managed Request Notifications
- Notification data model
- 0009 — Azure DevOps forge provider
- Merge-Commit Backflow Strategy
- Request-text templates
- forge/notifications.go
- Research: Managed Request Notifications
- Declarative Branch Promotion Reconciler
- Notification validation guide
- 0011 — Templatable request text
- 0013 — Add an optional notification contract
- 0014 — Record notification delivery state in Git notes
- 0015 — Extend Oiax ownership to the standard Git notes namespace
- Governance change-record templates
- Specification Quality Checklist: Managed Request Notifications
- Proposed provider, state, and CLI contracts
- Proposed outbound delivery contract
- Proposed notification presentation contract
- mergeEvent
- Implementation validation evidence
- Analysis remediation record
- notificationtest/README.md

## God Nodes (most connected - your core abstractions)
1. `gitHarness()` - 77 edges
2. `testGraph()` - 76 edges
3. `checkout()` - 62 edges
4. `writeJSON()` - 58 edges
5. `newProvider()` - 53 edges
6. `newRepo()` - 44 edges
7. `Provider` - 42 edges
8. `writeCommit()` - 41 edges
9. `Runner` - 38 edges
10. `Coordinator` - 35 edges

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

## Communities (101 total, 20 thin omitted)

### Community 0 - "testing.T"
Cohesion: 0.06
Nodes (143): actionMetadata, pipelineTemplate, testing.T, TestPublishedActionRunnerContract(), TestPublishedAzurePipelinesTemplateContract(), BackflowBranchName(), TestBackflowBranchName(), TestErrNoResponseWraps() (+135 more)

### Community 1 - "Provider"
Cohesion: 0.17
Nodes (5): repoSettings, escapeRefPath(), Provider, isForbidden(), isNotFound()

### Community 2 - "github_test.go"
Cohesion: 0.09
Nodes (70): issueSpec, prSpec, assertAuth(), assertNoToken(), decode(), Provider, newProvider(), runGit() (+62 more)

### Community 3 - "Coordinator"
Cohesion: 0.08
Nodes (39): BranchState, Equivalence, log/slog.Logger, backflowToReturn(), EvaluateEdge(), Commit, EdgeObservation, commits() (+31 more)

### Community 4 - "Plan"
Cohesion: 0.06
Nodes (62): Kind, exitCodeError, forgeKind, loadedConfig, options, versionInfo, github.com/spf13/cobra.Command, io.Writer (+54 more)

### Community 6 - "Backflow Execution"
Cohesion: 0.05
Nodes (42): Downloaded Artifact Verification, Oiax Composite GitHub Action, Action Pinned Config Ref, Git Ref Preparation, Release Binary Download, Human-in-the-Loop Steering, Adopt the Name Oiax, Tiller Ecosystem Collision (+34 more)

### Community 7 - "fakeForge"
Cohesion: 0.10
Nodes (6): fakeForge, os/exec.Cmd, gitCommand(), BranchPush, MergeMethods, UpdateRequest

### Community 8 - "common.sh"
Cohesion: 0.09
Nodes (15): check-prerequisites.sh script, check_dir(), check_file(), get_feature_paths(), get_repo_root(), has_jq(), _persist_feature_json(), resolve_specify_init_dir() (+7 more)

### Community 9 - "Tasks: [FEATURE NAME]"
Cohesion: 0.07
Nodes (26): Dependencies & Execution Order, Format: `[ID] [P?] [Story] Description`, Implementation for User Story 1, Implementation for User Story 2, Implementation for User Story 3, Implementation Strategy, Incremental Delivery, MVP First (User Story 1 Only) (+18 more)

### Community 10 - "Run"
Cohesion: 0.06
Nodes (71): main(), run(), TestRunExitCodes(), testing.TB, run(), TestDeprecatedAPIVersionWarns(), TestGenDocs(), TestGraphCommand() (+63 more)

### Community 11 - "newRepo"
Cohesion: 0.09
Nodes (55): NotesOptions, Provider, Provider, newRepo(), oidLike(), requireGit(), runGit(), TestCherryPickCancelledContextIsOperationalError() (+47 more)

### Community 12 - "AcceptPolicy"
Cohesion: 0.26
Nodes (23): DeliveryKey(), modelEvent(), modelLedger(), modelPolicy(), modelRepo(), modelRevision(), modelTime(), TestNotificationAdmissionClaimAndMonotoneReceipt() (+15 more)

### Community 13 - "PromotionGraph Configuration Contract"
Cohesion: 0.50
Nodes (4): Backflow Policy Configuration, PromotionGraph Configuration Contract, Environments PromotionGraph Fixture, Strict Configuration Validation

### Community 14 - "Branch Promotion (capability)"
Cohesion: 0.17
Nodes (12): Skaphos Glossary Discipline (branch promotion vs Promotion vs backflow), Conventional Commits, Signed commits + DCO sign-off, Branch Promotion (capability), argoproj-labs/gitops-promoter (prior art), Kargo (prior art), Promotion Graph (DAG model), release-please (prior art / inspiration) (+4 more)

### Community 15 - "Core Principles"
Cohesion: 0.11
Nodes (17): Core Principles, Development Workflow and Quality Gates, Engineering Constraints, Governance, I. Explicit State Over Implicit Behavior, II. Git Is the Durable Desired-State Boundary, III. Deterministic, Reconstructible Operation, IV. Control-Plane Conventions, Never Obscured (+9 more)

### Community 16 - "git.go"
Cohesion: 0.16
Nodes (9): capWriter, CherryPickConflict, MergeConflict, bytes.Buffer, checkMinVersion(), Commit, parseGitVersion(), TestCheckMinVersion() (+1 more)

### Community 17 - "Feature Specification: [FEATURE NAME]"
Cohesion: 0.14
Nodes (13): Assumptions, Edge Cases, Feature Specification: [FEATURE NAME], Functional Requirements, Key Entities *(include if feature involves data)*, Measurable Outcomes, Out of Scope *(mandatory)*, Requirements *(mandatory)* (+5 more)

### Community 18 - "Content Equivalence Ladder"
Cohesion: 0.24
Nodes (11): ADR 0001: Adopt the name Oiax, Rationale: Tiller collided with Helm v2's Tiller in the target ecosystem; Oiax is the literal Greek for tiller and keeps the hand-on-the-helm intent, ADR 0002: Detect divergence by content, not ancestry, Rationale: squash/rebase merges rewrite SHAs; ancestry-only detection leaves edges permanently diverged and PR creation fails with HTTP 422; a private state database would violate the no-control-plane posture, Content Equivalence Ladder, Rung 3: Head-Tree Equality, Managed Change Requests, Rung 2: Stable Patch Identity (+3 more)

### Community 19 - "annotationHandler"
Cohesion: 0.24
Nodes (9): log/slog.Attr, log/slog.Handler, log/slog.Level, log/slog.Record, escapeAnnotation(), escapeAzureAnnotation(), formatAnnotation(), annotationHandler (+1 more)

### Community 20 - "net/http.Header"
Cohesion: 0.25
Nodes (8): net/http.Header, net/http.Response, parseAPIError(), retryableStatus(), retryDelay(), serverBackoff(), sleepCtx(), TestDoOnceConstructionErrorNotRetried()

### Community 21 - "Pinned Configuration Ref (--config-ref)"
Cohesion: 0.25
Nodes (9): Agent Safety Rules (do not violate), ADR 0003: Read configuration from a pinned ref, Rationale: config is itself promoted and differs per branch; reading the triggering ref is nondeterministic and lets untrusted PR config run with write credentials, Engine Purity Rules, Reconciliation Loop, Layering Rule: entrypoint to engine to git/forge, depguard-enforced, oiax (root command), Pinned Configuration Ref (--config-ref) (+1 more)

### Community 22 - "artifacts.go"
Cohesion: 0.12
Nodes (18): policyConfiguration, policyList, policyScope, policySettings, wiqlResult, wiState, wiStates, workItem (+10 more)

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
Nodes (46): adoFake, adoFakePull, adoFakeWI, pullSpec, ghFake, ghFakeIssue, ghFakePull, net/http.HandlerFunc (+38 more)

### Community 28 - "model.go"
Cohesion: 0.11
Nodes (29): LifecycleSeed, EventRevision, Digest(), EventID(), CommitSnapshot, EventV1, LifecycleRequest, LifecycleState (+21 more)

### Community 29 - "DeliveryPayloadV1"
Cohesion: 0.10
Nodes (21): Client, failTransport, net/netip.Addr, net/url.URL, adapterPayload(), allStrings(), TestNotificationAdapterPersistedMessagesAndFacts(), allowedAddress() (+13 more)

### Community 30 - "[CHECKLIST TYPE] Checklist: [FEATURE NAME]"
Cohesion: 0.40
Nodes (4): [Category 1], [Category 2], [CHECKLIST TYPE] Checklist: [FEATURE NAME], Notes

### Community 31 - "BuildPlan"
Cohesion: 0.24
Nodes (25): FromConfig(), TestFromConfigAgreesWithDefault(), TestFromConfigAppliesDriftDefault(), TestFromConfigPreservesCherryPickMergeMethods(), TestFromConfigResolvesBackflowDefaults(), validGraph(), BuildPlan(), edge() (+17 more)

### Community 32 - "RequestID"
Cohesion: 0.14
Nodes (9): notificationCapabilityTrap, fakeForge, Provider, RequestID, Provider, LifecyclePage, LifecycleQuery, fakeForge (+1 more)

### Community 35 - "GoReleaser Publication"
Cohesion: 0.24
Nodes (11): Floating Major Action Tag, GoReleaser Publication, Release Tag Monotonicity Guard, Annotated Immutable SemVer Tag, Release Please Automation, Release Bot GitHub App Token, Release PR Label Reconciliation, Release Checksums (+3 more)

### Community 36 - "CI Workflow"
Cohesion: 0.24
Nodes (10): Skaphos Contribution Governance, Cross-Platform Test Matrix, DCO Sign-Off Gate, Generated Artifact Drift Check, REUSE License Gate, GoReleaser Snapshot Build, Staticcheck and Govulncheck, CI Workflow (+2 more)

### Community 37 - "ChangeRequest"
Cohesion: 0.21
Nodes (18): RequestState, ChangeRequest, RequestType, CreateRequest, Forge, RequestFilter, assertAscending(), containsID() (+10 more)

### Community 46 - "fakeForge"
Cohesion: 0.19
Nodes (6): ConflictArtifact, ConflictArtifactID, ConflictArtifactSpec, Reason, findArtifact(), fakeForge

### Community 47 - "Tasks: Managed Request Notifications"
Cohesion: 0.10
Nodes (20): Dependencies and execution order, Implementation, Implementation, Implementation, Implementation, Implementation strategy and completion accounting, Parallel execution examples, Phase 1: Setup (+12 more)

### Community 48 - "Provider"
Cohesion: 0.14
Nodes (6): net/http.Client, Provider, looksLikeJWT(), parseAPIError(), sleepCtx(), TestLooksLikeJWT()

### Community 49 - "v1/types.go"
Cohesion: 0.06
Nodes (46): BackflowPolicy, Branch, Expectations, Promotion, IsDeprecatedAPIVersion(), Load(), Parse(), safeParseError() (+38 more)

### Community 50 - "0010 — Exported validation and defaulting on the config API"
Cohesion: 0.29
Nodes (6): 0010 — Exported validation and defaulting on the config API, Consequences, Context, Decision, Links, Options considered

### Community 51 - ".do"
Cohesion: 0.28
Nodes (3): issueNumber(), prNumber(), understoodMarker()

### Community 52 - "Validate"
Cohesion: 0.18
Nodes (21): io.Reader, Decode(), Encode(), eventKind(), requestKind(), safeText(), codecLedger(), FuzzNotificationLedgerCodec() (+13 more)

### Community 53 - "time.Time"
Cohesion: 0.09
Nodes (19): notificationCursor, notificationInterval, sync.Mutex, time.Time, LedgerStore, Sender, Snapshot, Transition (+11 more)

### Community 54 - "Deterministic Backflow Return Branch"
Cohesion: 0.67
Nodes (3): Deterministic Backflow Return Branch, Event-Driven Concurrency Without Locks, Supersede Stale Backflow Request

### Community 61 - "Feature Specification: Managed Request Notifications"
Cohesion: 0.12
Nodes (16): Assumptions, Clarifications, Edge Cases, Feature Specification: Managed Request Notifications, Functional Requirements, Key Entities, Measurable Outcomes, Out of Scope *(mandatory)* (+8 more)

### Community 65 - "Serialize"
Cohesion: 0.08
Nodes (44): adoPull, adoPullList, forkRef, gitRef, propertiesCollection, refList, refUpdateResult, refUpdateResults (+36 more)

### Community 66 - "ValidOID"
Cohesion: 0.24
Nodes (10): ValidDigest(), ValidOID(), CheckRevision(), mapError(), New(), validateAppend(), Coordinator, PolicyRevisionV1 (+2 more)

### Community 67 - "tmpl.go"
Cohesion: 0.13
Nodes (36): text/template.FuncMap, text/template.Template, checkBodySafety(), compileTemplate(), Default(), execute(), funcMap(), Commit (+28 more)

### Community 68 - "azuredevops/client.go"
Cohesion: 0.18
Nodes (7): apiError, capWriter, errNoResponse, isDuplicateActiveRequest(), retryableStatus(), retryDelay(), serverBackoff()

### Community 69 - "ParseRemoteURL"
Cohesion: 0.26
Nodes (12): Repo, orgFromCollectionURI(), ParseRemoteURL(), pathSegments(), repoFromEnv(), ResolveRepo(), splitRemote(), TestParseRemoteURL() (+4 more)

### Community 70 - "Deploying Oiax from Azure Pipelines"
Cohesion: 0.14
Nodes (14): Azure Repos, Choosing a mode, Connecting Azure DevOps to GitHub, Create the service connection, Deploying Oiax from Azure Pipelines, `fetchDepth: 0` is not optional, Next steps, Parameters (+6 more)

### Community 71 - "github.go"
Cohesion: 0.13
Nodes (20): apiError, apiError, errNoResponse, ghIssue, ghLabel, ghPull, ghRef, ghRepo (+12 more)

### Community 72 - "Oiax Task-Oriented Guides"
Cohesion: 0.20
Nodes (14): CLI Exit-Code Contract, Example request-text templates, Oiax Installation Artifacts, Agent Installation Confirmation Gate, Backflow Hotfix Return, Plan-First Repository Adoption, Promotion Graph Quickstart, CI-Triggering Installation Token (+6 more)

### Community 73 - "LedgerV1"
Cohesion: 0.19
Nodes (12): time.Duration, AttemptResult, LedgerV1, RecordResult(), RenewClaim(), RetryDelay(), claimDelay(), deliveryPayload() (+4 more)

### Community 74 - "RunLifecycle"
Cohesion: 0.33
Nodes (7): RunLifecycle(), notificationPull(), serveNotificationIdentity(), TestNotificationLifecycleConformance(), TestNotificationLifecycleDetailFailureRetainsPartialProgress(), TestNotificationLifecyclePaginationOverlapsAndDeduplicatesMovement(), TestNotificationLifecycleRejectsCrossOriginContinuation()

### Community 75 - "Implementation Plan: Managed Request Notifications"
Cohesion: 0.17
Nodes (12): Complexity Tracking, Constitution Check, Documentation (this feature), Implementation Plan: Managed Request Notifications, Operational risks and rollback, Phase 0 — Research conclusions, Phase 1 — Design and implementation sequence, Project Structure (+4 more)

### Community 76 - "Notification data model"
Cohesion: 0.18
Nodes (11): Configuration, Configuration revision ordering, Delivery and claims, Destination state and routing lifetime, Event, Ledger snapshot, Notification data model, Repository and managed request (+3 more)

### Community 77 - "0009 — Azure DevOps forge provider"
Cohesion: 0.18
Nodes (9): 0009 — Azure DevOps forge provider, Authentication and the token, Consequences, Context, Decision, Links, Marker storage on a managed request, Options considered (+1 more)

### Community 78 - "Merge-Commit Backflow Strategy"
Cohesion: 0.25
Nodes (8): Live Merge-Method Fence, Merge-Commit Backflow Strategy, Skip-in-Range Fence, Conflict Issue Marker-and-Label Identity, Durable Backflow Conflict Artifact, Lock-Free Conflict Issue Convergence, Backflow Conflict Handling, Deterministic Backflow Request Lifecycle

### Community 79 - "Request-text templates"
Cohesion: 0.22
Nodes (9): Configuration keys, Functions, Rendering rules and constraints, Request-text templates, `spec.templates.backflowMergeMessage`, `spec.templates.promotion`, `.backflow`, `.backflowConflict`, Untrusted variables, Variable context (+1 more)

### Community 80 - "forge/notifications.go"
Cohesion: 0.33
Nodes (6): CreateDisposition, CreateOutcome, NotificationNotesProvider, SnapshotReader, LifecycleReader, NotificationOriginV1

### Community 82 - "Research: Managed Request Notifications"
Cohesion: 0.25
Nodes (8): 1. Integration boundaries, 2. Durable delivery state and concurrency, 3. Activation, event discovery, and creation provenance, 4. Retry policy and bounded work, 5. Transport choice and prior art, 6. Presentation templates and environment language, 7. Public compatibility and safety, Research: Managed Request Notifications

### Community 83 - "Declarative Branch Promotion Reconciler"
Cohesion: 0.33
Nodes (7): Git 2.45 Runtime Contract, Git Runner Shell-Out, Declarative Branch Promotion Reconciler, Pure Reconciliation Layering, Provider-Neutral Engine Core, System Git Layer, Reconcile Coordination Layer

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

### Community 90 - "0015 — Extend Oiax ownership to the standard Git notes namespace"
Cohesion: 0.33
Nodes (6): 0015 — Extend Oiax ownership to the standard Git notes namespace, Consequences, Context, Decision, Links, Options considered

### Community 91 - "Governance change-record templates"
Cohesion: 0.33
Nodes (6): A minimal change-record setup, Backflow and conflict records, Governance change-record templates, Next steps, Untrusted input, one more time, What renders when

### Community 92 - "Specification Quality Checklist: Managed Request Notifications"
Cohesion: 0.33
Nodes (5): Content Quality, Feature Readiness, Notes, Requirement Completeness, Specification Quality Checklist: Managed Request Notifications

### Community 93 - "Proposed provider, state, and CLI contracts"
Cohesion: 0.33
Nodes (6): CLI and plan preview, Delivery payload handoff, Ledger store, Lifecycle observation, Notification-origin wire format, Proposed provider, state, and CLI contracts

### Community 94 - "Proposed outbound delivery contract"
Cohesion: 0.40
Nodes (5): Generic webhook schema v1, Outcomes and retries, Proposed outbound delivery contract, Slack incoming webhook, Teams Workflows

### Community 95 - "Proposed notification presentation contract"
Cohesion: 0.40
Nodes (5): Closed context, Configuration and precedence, Delivery encoding, Immutable facts and retries, Proposed notification presentation contract

### Community 96 - "mergeEvent"
Cohesion: 0.44
Nodes (10): mergeEvent(), mergeRuntime(), TestNotificationAcceptedWithoutReceiptIsUncertain(), TestNotificationDispatchCancellationAndDisabledBypass(), TestNotificationDispatchCompetingRunsSendOnce(), TestNotificationDispatchSpacingAndIndependentFailure(), TestNotificationMergeDeliveryAndRepeat(), TestNotificationSlowDestinationDoesNotStarveHealthyDestination() (+2 more)

### Community 97 - "Implementation validation evidence"
Cohesion: 0.33
Nodes (6): Current checkpoint — lifecycle completeness and bounded delivery, Earlier foundation commands and results, Earlier foundation handoff (historical), Implementation validation evidence, T001 — Namespace authority and contract review, T002–T015 — Setup and foundation

### Community 98 - "Analysis remediation record"
Cohesion: 0.67
Nodes (3): Analysis remediation record, Document verification, Resolved gate: C1 / T001

## Knowledge Gaps
- **258 isolated node(s):** `common.sh script`, `github.com/skaphos/oiax`, `actionMetadata`, `pipelineTemplate`, `versionInfo` (+253 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **20 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `BuildPlan()` connect `BuildPlan` to `testing.T`, `Coordinator`, `Plan`, `Pinned Configuration Ref (--config-ref)`?**
  _High betweenness centrality (0.218) - this node is a cross-community bridge._
- **Why does `Reconciliation Loop` connect `Pinned Configuration Ref (--config-ref)` to `BuildPlan`?**
  _High betweenness centrality (0.213) - this node is a cross-community bridge._
- **Are the 5 inferred relationships involving `gitHarness()` (e.g. with `.commit()` and `TestScenarioBackflowMixedDropAndApplyConverges()`) actually correct?**
  _`gitHarness()` has 5 INFERRED edges - model-reasoned connections that need verification._
- **Are the 5 inferred relationships involving `testGraph()` (e.g. with `TestScenarioBackflowMixedDropAndApplyConverges()` and `TestScenarioBackflowPushIsByteIdenticalAcrossIndependentRepos()`) actually correct?**
  _`testGraph()` has 5 INFERRED edges - model-reasoned connections that need verification._
- **Are the 4 inferred relationships involving `checkout()` (e.g. with `TestScenarioBackflowMixedDropAndApplyConverges()` and `TestScenarioBackflowPushIsByteIdenticalAcrossIndependentRepos()`) actually correct?**
  _`checkout()` has 4 INFERRED edges - model-reasoned connections that need verification._
- **What connects `common.sh script`, `github.com/skaphos/oiax`, `actionMetadata` to the rest of the system?**
  _258 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `testing.T` be split into smaller, more focused modules?**
  _Cohesion score 0.055583126550868486 - nodes in this community are weakly interconnected._