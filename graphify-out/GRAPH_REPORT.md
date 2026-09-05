# Graph Report - oiax  (2026-09-04)

## Corpus Check
- 161 files · ~222,643 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 1681 nodes · 4612 edges · 102 communities (76 shown, 22 thin omitted)
- Extraction: 93% EXTRACTED · 7% INFERRED · 0% AMBIGUOUS · INFERRED: 334 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `d508244c`
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
- ChangeRequest
- common.sh
- Tasks: [FEATURE NAME]
- Run
- newRepo
- LedgerV1
- PromotionGraph Configuration Contract
- Branch Promotion (capability)
- Core Principles
- git.go
- Feature Specification: [FEATURE NAME]
- Content Equivalence Ladder
- annotationHandler
- Provider
- Pinned Configuration Ref (--config-ref)
- ConflictArtifactID
- Core Principles
- Implementation Plan: [FEATURE]
- 0012 — Squash merges on the promotion path
- Plan Format Version 1
- writeJSON
- model.go
- Provider
- [CHECKLIST TYPE] Checklist: [FEATURE NAME]
- CLI Exit-Code Contract
- RequestID
- Drift Policy (forbidden/expected)
- Design proposal in skaphos-resources under tools/oiax/
- GoReleaser Publication
- CI Workflow
- ConflictArtifact
- Deterministic Backflow Branch Naming
- Token Guidance (GITHUB_TOKEN recursion guard)
- Plan-First Rollout
- config.DefaultPath
- MIT License (c) 2026 Skaphos
- Idempotent Reconciliation
- Pinned Configuration Ref
- Shallow-Clone Equivalence Degradation
- repoSettings
- Tasks: Managed Request Notifications
- time.Duration
- v1/types.go
- 0010 — Exported validation and defaulting on the config API
- Parse
- codecLedger
- sync.Mutex
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
- Parse
- .Validate
- tmpl.go
- artifacts.go
- ParseRemoteURL
- Deploying Oiax from Azure Pipelines
- github.go
- Oiax Task-Oriented Guides
- NotificationPolicy
- .Commit
- Implementation Plan: Managed Request Notifications
- Notification data model
- 0009 — Azure DevOps forge provider
- Merge-Commit Backflow Strategy
- Request-text templates
- Validate
- Research: Managed Request Notifications
- Declarative Branch Promotion Reconciler
- Graph
- RepositoryIdentity
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
- errNoResponse
- Implementation validation evidence
- Analysis remediation record
- notificationtest/README.md

## God Nodes (most connected - your core abstractions)
1. `testGraph()` - 76 edges
2. `gitHarness()` - 76 edges
3. `checkout()` - 62 edges
4. `writeJSON()` - 58 edges
5. `newProvider()` - 53 edges
6. `newRepo()` - 44 edges
7. `Provider` - 41 edges
8. `writeCommit()` - 41 edges
9. `Runner` - 38 edges
10. `Coordinator` - 35 edges

## Surprising Connections (you probably didn't know these)
- `BuildPlan()` --conceptually_related_to--> `Reconciliation Loop`  [INFERRED]
  internal/engine/plan.go → docs/architecture.md
- `Pinned Declarative Configuration` --semantically_similar_to--> `Single Pinned Configuration Ref`  [INFERRED] [semantically similar]
  .github/copilot-instructions.md → docs/adr/0003-pinned-configuration-ref.md
- `CI Workflow` --semantically_similar_to--> `Local Validation Task Suite`  [INFERRED] [semantically similar]
  .github/workflows/ci.yml → Taskfile.yml
- `Backflow` --semantically_similar_to--> `Backflow Execution`  [INFERRED] [semantically similar]
  AGENTS.md → docs/adr/0004-backflow-execution.md
- `Action Pinned Config Ref` --semantically_similar_to--> `Single Pinned Configuration Ref`  [INFERRED] [semantically similar]
  action.yml → docs/adr/0003-pinned-configuration-ref.md

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **CI Quality Gate Suite** — github_workflows_ci_dco_gate, github_workflows_ci_reuse_gate, github_workflows_ci_cross_platform_tests, github_workflows_ci_static_analysis, github_workflows_ci_generated_artifact_check, github_workflows_ci_snapshot_build [EXTRACTED 1.00]
- **Automated Release Pipeline** — github_workflows_release_please_automation, github_workflows_release_please_annotated_tag, github_workflows_release_goreleaser_publication, github_workflows_release_floating_major_tag [EXTRACTED 1.00]
- **Convergent Backflow Execution** — adr0004_deterministic_return_branch, adr0004_identity_ladder, adr0004_ephemeral_worktree, adr0004_conflict_divergence, adr0004_supersede_stale_request [EXTRACTED 1.00]
- **Backflow Execution Model** — adr0006_merge_commit_backflow, docs_guides_backflow_deterministic_request, docs_guides_backflow_conflict_handling [EXTRACTED 1.00]
- **Reconciliation Layer Model** — docs_architecture_pure_reconciliation_layers, docs_code_map_engine_core, docs_code_map_reconcile_layer, docs_code_map_git_layer [EXTRACTED 1.00]

## Communities (102 total, 22 thin omitted)

### Community 0 - "testing.T"
Cohesion: 0.06
Nodes (139): actionMetadata, pipelineTemplate, testing.T, TestPublishedActionRunnerContract(), TestPublishedAzurePipelinesTemplateContract(), BackflowBranchName(), TestBackflowBranchName(), TestErrNoResponseWraps() (+131 more)

### Community 1 - "Provider"
Cohesion: 0.19
Nodes (4): Provider, issueNumber(), prNumber(), understoodMarker()

### Community 2 - "github_test.go"
Cohesion: 0.09
Nodes (70): issueSpec, prSpec, assertAuth(), assertNoToken(), decode(), Provider, newProvider(), runGit() (+62 more)

### Community 3 - "Coordinator"
Cohesion: 0.06
Nodes (64): BranchState, Equivalence, log/slog.Logger, backflowToReturn(), EvaluateEdge(), Commit, EdgeObservation, commits() (+56 more)

### Community 4 - "Plan"
Cohesion: 0.06
Nodes (62): Kind, exitCodeError, forgeKind, loadedConfig, options, versionInfo, github.com/spf13/cobra.Command, io.Writer (+54 more)

### Community 6 - "Backflow Execution"
Cohesion: 0.05
Nodes (42): Downloaded Artifact Verification, Oiax Composite GitHub Action, Action Pinned Config Ref, Git Ref Preparation, Release Binary Download, Human-in-the-Loop Steering, Adopt the Name Oiax, Tiller Ecosystem Collision (+34 more)

### Community 7 - "ChangeRequest"
Cohesion: 0.08
Nodes (13): fakeForge, RequestState, ChangeRequest, RequestType, changeRequest(), BranchPush, CreateRequest, MergeMethods (+5 more)

### Community 8 - "common.sh"
Cohesion: 0.13
Nodes (25): check-prerequisites.sh script, check_dir(), check_file(), find_specify_root(), format_speckit_command(), get_current_branch(), get_feature_paths(), get_invoke_separator() (+17 more)

### Community 9 - "Tasks: [FEATURE NAME]"
Cohesion: 0.07
Nodes (26): Dependencies & Execution Order, Format: `[ID] [P?] [Story] Description`, Implementation for User Story 1, Implementation for User Story 2, Implementation for User Story 3, Implementation Strategy, Incremental Delivery, MVP First (User Story 1 Only) (+18 more)

### Community 10 - "Run"
Cohesion: 0.07
Nodes (66): main(), run(), TestRunExitCodes(), testing.TB, run(), TestDeprecatedAPIVersionWarns(), TestGenDocs(), TestGraphCommand() (+58 more)

### Community 11 - "newRepo"
Cohesion: 0.09
Nodes (54): NotesOptions, Provider, Provider, newRepo(), oidLike(), requireGit(), runGit(), TestCherryPickCancelledContextIsOperationalError() (+46 more)

### Community 12 - "LedgerV1"
Cohesion: 0.28
Nodes (23): DeliveryKey(), Digest(), LedgerV1, modelEvent(), modelLedger(), modelPolicy(), modelRepo(), modelRevision() (+15 more)

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

### Community 20 - "Provider"
Cohesion: 0.13
Nodes (14): adoPull, adoPullList, forkRef, gitRef, propertiesCollection, refList, refUpdateResult, refUpdateResults (+6 more)

### Community 21 - "Pinned Configuration Ref (--config-ref)"
Cohesion: 0.25
Nodes (9): Agent Safety Rules (do not violate), ADR 0003: Read configuration from a pinned ref, Rationale: config is itself promoted and differs per branch; reading the triggering ref is nondeterministic and lets untrusted PR config run with write credentials, Engine Purity Rules, Reconciliation Loop, Layering Rule: entrypoint to engine to git/forge, depguard-enforced, oiax (root command), Pinned Configuration Ref (--config-ref) (+1 more)

### Community 22 - "ConflictArtifactID"
Cohesion: 0.18
Nodes (9): workItem, encoding/json.RawMessage, artifactID(), fieldString(), Provider, htmlBody(), understood(), ConflictArtifactID (+1 more)

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

### Community 28 - "model.go"
Cohesion: 0.19
Nodes (20): time.Time, AttemptResult, CommitSnapshot, DeliveryPayloadV1, EventV1, NotificationOriginV1, RequestV1, ScanProgress (+12 more)

### Community 29 - "Provider"
Cohesion: 0.14
Nodes (7): net/http.Client, net/http.Response, Provider, looksLikeJWT(), parseAPIError(), TestLooksLikeJWT(), parseAPIError()

### Community 30 - "[CHECKLIST TYPE] Checklist: [FEATURE NAME]"
Cohesion: 0.40
Nodes (4): [Category 1], [Category 2], [CHECKLIST TYPE] Checklist: [FEATURE NAME], Notes

### Community 31 - "CLI Exit-Code Contract"
Cohesion: 0.67
Nodes (4): CLI Exit-Code Contract, Read-Only Drift Gate, Scheduled Drift Gate, Human-Attention Exit 3

### Community 32 - "RequestID"
Cohesion: 0.15
Nodes (14): notificationCapabilityTrap, fakeForge, CreateDisposition, CreateOutcome, LifecycleReader, NotificationNotesProvider, SnapshotReader, RequestID (+6 more)

### Community 35 - "GoReleaser Publication"
Cohesion: 0.24
Nodes (11): Floating Major Action Tag, GoReleaser Publication, Release Tag Monotonicity Guard, Annotated Immutable SemVer Tag, Release Please Automation, Release Bot GitHub App Token, Release PR Label Reconciliation, Release Checksums (+3 more)

### Community 36 - "CI Workflow"
Cohesion: 0.24
Nodes (10): Skaphos Contribution Governance, Cross-Platform Test Matrix, DCO Sign-Off Gate, Generated Artifact Drift Check, REUSE License Gate, GoReleaser Snapshot Build, Staticcheck and Govulncheck, CI Workflow (+2 more)

### Community 37 - "ConflictArtifact"
Cohesion: 0.32
Nodes (15): ConflictArtifact, Forge, assertAscending(), containsID(), findArtifact(), mustCreate(), mustCreateArtifact(), mustList() (+7 more)

### Community 47 - "Tasks: Managed Request Notifications"
Cohesion: 0.10
Nodes (20): Dependencies and execution order, Implementation, Implementation, Implementation, Implementation, Implementation strategy and completion accounting, Parallel execution examples, Phase 1: Setup (+12 more)

### Community 48 - "time.Duration"
Cohesion: 0.13
Nodes (15): apiError, capWriter, errNoResponse, net/http.Header, time.Duration, isDuplicateActiveRequest(), retryableStatus(), retryDelay() (+7 more)

### Community 49 - "v1/types.go"
Cohesion: 0.19
Nodes (17): BackflowPolicy, Branch, Branch, Expectations, Promotion, BackflowStrategy, DriftPolicy, MergeMethod (+9 more)

### Community 50 - "0010 — Exported validation and defaulting on the config API"
Cohesion: 0.29
Nodes (6): 0010 — Exported validation and defaulting on the config API, Consequences, Context, Decision, Links, Options considered

### Community 51 - "Parse"
Cohesion: 0.18
Nodes (15): IsDeprecatedAPIVersion(), Load(), Parse(), safeParseError(), TestLoadAcceptsFileAtLimit(), TestLoadExample(), TestLoadRejectsOversizedFile(), TestParseAcceptsTemplates() (+7 more)

### Community 52 - "codecLedger"
Cohesion: 0.21
Nodes (13): io.Reader, Decode(), Encode(), codecLedger(), FuzzNotificationLedgerCodec(), TestNotificationLedgerCodec(), TestNotificationLedgerFullStateValidation(), TestNotificationLedgerJSONBoundaries() (+5 more)

### Community 53 - "sync.Mutex"
Cohesion: 0.15
Nodes (10): sync.Mutex, Snapshot, Transition, NewLedger(), NewClock(), TestFixturesOwnSnapshots(), LedgerStore, Sender (+2 more)

### Community 54 - "Deterministic Backflow Return Branch"
Cohesion: 0.67
Nodes (3): Deterministic Backflow Return Branch, Event-Driven Concurrency Without Locks, Supersede Stale Backflow Request

### Community 61 - "Feature Specification: Managed Request Notifications"
Cohesion: 0.12
Nodes (16): Assumptions, Clarifications, Edge Cases, Feature Specification: Managed Request Notifications, Functional Requirements, Key Entities, Measurable Outcomes, Out of Scope *(mandatory)* (+8 more)

### Community 65 - "Parse"
Cohesion: 0.16
Nodes (24): testing.F, blockBounds(), hasOiaxKey(), Parse(), parseInner(), Replace(), Sanitize(), Serialize() (+16 more)

### Community 66 - ".Validate"
Cohesion: 0.25
Nodes (8): findCycle(), Branch, Promotion, PromotionGraph, sortedBranchNames(), validateRefName(), validateRequestTemplate(), validateTemplatePath()

### Community 67 - "tmpl.go"
Cohesion: 0.13
Nodes (36): text/template.FuncMap, text/template.Template, checkBodySafety(), compileTemplate(), Default(), execute(), funcMap(), Commit (+28 more)

### Community 68 - "artifacts.go"
Cohesion: 0.21
Nodes (12): policyConfiguration, policyList, policyScope, policySettings, wiqlResult, wiState, wiStates, workItemBatch (+4 more)

### Community 69 - "ParseRemoteURL"
Cohesion: 0.26
Nodes (12): Repo, orgFromCollectionURI(), ParseRemoteURL(), pathSegments(), repoFromEnv(), ResolveRepo(), splitRemote(), TestParseRemoteURL() (+4 more)

### Community 70 - "Deploying Oiax from Azure Pipelines"
Cohesion: 0.14
Nodes (14): Azure Repos, Choosing a mode, Connecting Azure DevOps to GitHub, Create the service connection, Deploying Oiax from Azure Pipelines, `fetchDepth: 0` is not optional, Next steps, Parameters (+6 more)

### Community 71 - "github.go"
Cohesion: 0.14
Nodes (22): apiError, apiError, ghIssue, ghLabel, ghPull, ghRef, ghRepo, ghUser (+14 more)

### Community 72 - "Oiax Task-Oriented Guides"
Cohesion: 0.27
Nodes (10): Example request-text templates, Oiax Installation Artifacts, Agent Installation Confirmation Gate, Backflow Hotfix Return, Plan-First Repository Adoption, Promotion Graph Quickstart, CI-Triggering Installation Token, Event-Driven GitHub Action Reconciliation (+2 more)

### Community 73 - "NotificationPolicy"
Cohesion: 0.19
Nodes (10): notificationPolicy(), TestNotificationDefaultsAndRoundTrip(), TestNotificationPolicyEnabled(), TestNotificationValidation(), NotificationEvent, NotificationPolicy, NotificationRequestType, NotificationDestination (+2 more)

### Community 74 - ".Commit"
Cohesion: 0.29
Nodes (10): ValidDigest(), ValidOID(), CheckRevision(), RevisionRelation, mapError(), New(), validateAppend(), RevisionEvidence (+2 more)

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

### Community 79 - "Request-text templates"
Cohesion: 0.22
Nodes (9): Configuration keys, Functions, Rendering rules and constraints, Request-text templates, `spec.templates.backflowMergeMessage`, `spec.templates.promotion`, `.backflow`, `.backflowConflict`, Untrusted variables, Variable context (+1 more)

### Community 80 - "Validate"
Cohesion: 0.71
Nodes (7): eventKind(), requestKind(), safeText(), Validate(), validEvent(), validRepository(), validRequest()

### Community 82 - "Research: Managed Request Notifications"
Cohesion: 0.25
Nodes (8): 1. Integration boundaries, 2. Durable delivery state and concurrency, 3. Activation, event discovery, and creation provenance, 4. Retry policy and bounded work, 5. Transport choice and prior art, 6. Presentation templates and environment language, 7. Public compatibility and safety, Research: Managed Request Notifications

### Community 83 - "Declarative Branch Promotion Reconciler"
Cohesion: 0.33
Nodes (7): Git 2.45 Runtime Contract, Git Runner Shell-Out, Declarative Branch Promotion Reconciler, Pure Reconciliation Layering, Provider-Neutral Engine Core, System Git Layer, Reconcile Coordination Layer

### Community 84 - "Graph"
Cohesion: 0.29
Nodes (6): Expectations, Promotion, Branch, Graph, Expectations, Promotion

### Community 85 - "RepositoryIdentity"
Cohesion: 0.48
Nodes (4): EventID(), RepositoryIdentity, GraphKey(), TestNotificationIdentity()

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

### Community 97 - "Implementation validation evidence"
Cohesion: 0.67
Nodes (3): Implementation validation evidence, T001 — Namespace authority and contract review, Verification scope

### Community 98 - "Analysis remediation record"
Cohesion: 0.67
Nodes (3): Analysis remediation record, Document verification, Resolved gate: C1 / T001

## Knowledge Gaps
- **258 isolated node(s):** `common.sh script`, `github.com/skaphos/oiax`, `actionMetadata`, `pipelineTemplate`, `versionInfo` (+253 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 361 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **22 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `BuildPlan()` connect `Coordinator` to `testing.T`, `Plan`, `Pinned Configuration Ref (--config-ref)`?**
  _High betweenness centrality (0.265) - this node is a cross-community bridge._
- **Why does `Reconciliation Loop` connect `Pinned Configuration Ref (--config-ref)` to `Coordinator`?**
  _High betweenness centrality (0.260) - this node is a cross-community bridge._
- **Are the 5 inferred relationships involving `testGraph()` (e.g. with `TestScenarioBackflowMixedDropAndApplyConverges()` and `TestScenarioBackflowPushIsByteIdenticalAcrossIndependentRepos()`) actually correct?**
  _`testGraph()` has 5 INFERRED edges - model-reasoned connections that need verification._
- **Are the 4 inferred relationships involving `gitHarness()` (e.g. with `TestScenarioBackflowMixedDropAndApplyConverges()` and `TestScenarioBackflowPushIsByteIdenticalAcrossIndependentRepos()`) actually correct?**
  _`gitHarness()` has 4 INFERRED edges - model-reasoned connections that need verification._
- **Are the 4 inferred relationships involving `checkout()` (e.g. with `TestScenarioBackflowMixedDropAndApplyConverges()` and `TestScenarioBackflowPushIsByteIdenticalAcrossIndependentRepos()`) actually correct?**
  _`checkout()` has 4 INFERRED edges - model-reasoned connections that need verification._
- **What connects `common.sh script`, `github.com/skaphos/oiax`, `actionMetadata` to the rest of the system?**
  _258 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `testing.T` be split into smaller, more focused modules?**
  _Cohesion score 0.055391498881431765 - nodes in this community are weakly interconnected._