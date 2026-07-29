# Graph Report - defensive-conflict-artifacts  (2026-07-28)

## Corpus Check
- 125 files · ~186,724 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 1439 nodes · 3901 edges · 69 communities (50 shown, 19 thin omitted)
- Extraction: 90% EXTRACTED · 10% INFERRED · 0% AMBIGUOUS · INFERRED: 403 edges (avg confidence: 0.8)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `e6c98385`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- reconcile_test.go
- Provider
- github_test.go
- Coordinator
- loadGraph
- Context
- Backflow Execution
- fakeForge
- BuildPlan
- common.sh
- plan_reconcile_test.go
- newRepo
- render_test.go
- PromotionGraph Configuration Contract
- Branch Promotion (capability)
- Tasks: [FEATURE NAME]
- Core Principles
- git.go
- Content Equivalence Ladder
- annotationHandler
- validate_test.go
- Feature Specification: [FEATURE NAME]
- Core Principles
- MergeMethods
- Implementation Plan: [FEATURE]
- 0012 — Squash merges on the promotion path
- Plan Format Version 1
- writeJSON
- github/units_test.go
- azuredevops/units_test.go
- [CHECKLIST TYPE] Checklist: [FEATURE NAME]
- CLI Exit-Code Contract
- forge_test.go
- Drift Policy (forbidden/expected)
- Design proposal in skaphos-resources under tools/oiax/
- GoReleaser Publication
- CI Workflow
- Forge
- Deterministic Backflow Branch Naming
- Token Guidance (GITHUB_TOKEN recursion guard)
- Plan-First Rollout
- config.DefaultPath
- MIT License (c) 2026 Skaphos
- Idempotent Reconciliation
- Pinned Configuration Ref
- Shallow-Clone Equivalence Degradation
- Provider
- 0010 — Exported validation and defaulting on the config API
- action_test.go
- Deterministic Backflow Return Branch
- TestPromotionGraphJSONRoundTrip
- Source-First Promotion Rollback
- Isolated Environment-Specific Configuration
- Immutable Release Marketplace Constraint
- pkg/api/v1alpha1 (public configuration API)
- MIT License
- github.com/skaphos/oiax
- github.com/skaphos/oiax/tools
- ChangeRequest
- v1/types.go
- tmpl.go
- ConflictArtifactID
- ParseRemoteURL
- Deploying Oiax from Azure Pipelines
- github.go
- azure_pipelines_test.go
- 0011 — Templatable request text

## God Nodes (most connected - your core abstractions)
1. `Context` - 159 edges
2. `testGraph()` - 76 edges
3. `gitHarness()` - 75 edges
4. `Plan` - 74 edges
5. `checkout()` - 61 edges
6. `writeJSON()` - 58 edges
7. `newProvider()` - 53 edges
8. `Provider` - 41 edges
9. `newRepo()` - 41 edges
10. `writeCommit()` - 38 edges

## Surprising Connections (you probably didn't know these)
- `BuildPlan()` --conceptually_related_to--> `Reconciliation Loop`  [INFERRED]
  internal/engine/plan.go → docs/architecture.md
- `ChangeRequest` --conceptually_related_to--> `Managed Change Requests`  [INFERRED]
  internal/engine/types.go → docs/architecture.md
- `EdgeState` --shares_data_with--> `Content Equivalence Ladder`  [INFERRED]
  internal/engine/types.go → docs/architecture.md
- `Pinned Declarative Configuration` --semantically_similar_to--> `Single Pinned Configuration Ref`  [INFERRED] [semantically similar]
  .github/copilot-instructions.md → docs/adr/0003-pinned-configuration-ref.md
- `CI Workflow` --semantically_similar_to--> `Local Validation Task Suite`  [INFERRED] [semantically similar]
  .github/workflows/ci.yml → Taskfile.yml

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **CI Quality Gate Suite** — github_workflows_ci_dco_gate, github_workflows_ci_reuse_gate, github_workflows_ci_cross_platform_tests, github_workflows_ci_static_analysis, github_workflows_ci_generated_artifact_check, github_workflows_ci_snapshot_build [EXTRACTED 1.00]
- **Automated Release Pipeline** — github_workflows_release_please_automation, github_workflows_release_please_annotated_tag, github_workflows_release_goreleaser_publication, github_workflows_release_floating_major_tag [EXTRACTED 1.00]
- **Convergent Backflow Execution** — adr0004_deterministic_return_branch, adr0004_identity_ladder, adr0004_ephemeral_worktree, adr0004_conflict_divergence, adr0004_supersede_stale_request [EXTRACTED 1.00]
- **Backflow Execution Model** — adr0006_merge_commit_backflow, docs_guides_backflow_deterministic_request, docs_guides_backflow_conflict_handling [EXTRACTED 1.00]
- **Reconciliation Layer Model** — docs_architecture_pure_reconciliation_layers, docs_code_map_engine_core, docs_code_map_reconcile_layer, docs_code_map_git_layer [EXTRACTED 1.00]

## Communities (69 total, 19 thin omitted)

### Community 0 - "reconcile_test.go"
Cohesion: 0.08
Nodes (118): Plan, BackflowBranchName(), Logger, NewLogger(), T, TestAnnotationEscapesWorkflowCommandChars(), TestAzureAnnotationEscapesLoggingCommandChars(), TestNewLoggerAnnotatesWarningsOnlyWhenSinkSet() (+110 more)

### Community 1 - "Provider"
Cohesion: 0.14
Nodes (10): Provider, repoSettings, Client, marker, Mutex, Once, issueNumber(), managedMarker() (+2 more)

### Community 2 - "github_test.go"
Cohesion: 0.07
Nodes (90): ghFake, ghFakeIssue, ghFakePull, issueSpec, prSpec, ghNum(), HandlerFunc, Mutex (+82 more)

### Community 3 - "Coordinator"
Cohesion: 0.07
Nodes (43): Action, ActionType, BackflowExclusion, BackflowExclusionReason, BranchState, Commit, EdgeObservation, EdgeState (+35 more)

### Community 4 - "loadGraph"
Cohesion: 0.06
Nodes (53): Kind, exitCodeError, forgeKind, options, versionInfo, Detect(), T, TestDetect() (+45 more)

### Community 5 - "Context"
Cohesion: 0.19
Nodes (4): Cmd, Runner, gitCommand(), Context

### Community 6 - "Backflow Execution"
Cohesion: 0.05
Nodes (42): Downloaded Artifact Verification, Oiax Composite GitHub Action, Action Pinned Config Ref, Git Ref Preparation, Release Binary Download, Human-in-the-Loop Steering, Adopt the Name Oiax, Tiller Ecosystem Collision (+34 more)

### Community 7 - "fakeForge"
Cohesion: 0.11
Nodes (10): fakeForge, BranchPush, ConflictArtifact, ConflictArtifactSpec, Reason, RequestFilter, RequestID, RequestState (+2 more)

### Community 8 - "BuildPlan"
Cohesion: 0.17
Nodes (35): ADR 0003: Read configuration from a pinned ref, Rationale: config is itself promoted and differs per branch; reading the triggering ref is nondeterministic and lets untrusted PR config run with write credentials, Reconciliation Loop, oiax (root command), Pinned Configuration Ref (--config-ref), FromConfig(), PromotionGraph, PromotionGraph (+27 more)

### Community 9 - "common.sh"
Cohesion: 0.09
Nodes (15): check-prerequisites.sh script, check_dir(), check_file(), get_feature_paths(), get_repo_root(), has_jq(), _persist_feature_json(), resolve_specify_init_dir() (+7 more)

### Community 10 - "plan_reconcile_test.go"
Cohesion: 0.07
Nodes (75): main(), run(), T, TestRunExitCodes(), T, run(), TestDeprecatedAPIVersionWarns(), TestGenDocs() (+67 more)

### Community 11 - "newRepo"
Cohesion: 0.17
Nodes (45): T, newRepo(), oidLike(), requireGit(), runGit(), TestCherryPickCancelledContextIsOperationalError(), TestCherryPickConflict(), TestCherryPickDropsRedundant() (+37 more)

### Community 12 - "render_test.go"
Cohesion: 0.15
Nodes (27): actionVerb(), edgeSummaryText(), exclusionCounts(), Commit, Writer, mdCell(), RenderJSON(), RenderMarkdown() (+19 more)

### Community 13 - "PromotionGraph Configuration Contract"
Cohesion: 0.50
Nodes (4): Backflow Policy Configuration, PromotionGraph Configuration Contract, Environments PromotionGraph Fixture, Strict Configuration Validation

### Community 14 - "Branch Promotion (capability)"
Cohesion: 0.17
Nodes (12): Skaphos Glossary Discipline (branch promotion vs Promotion vs backflow), Conventional Commits, Signed commits + DCO sign-off, Branch Promotion (capability), argoproj-labs/gitops-promoter (prior art), Kargo (prior art), Promotion Graph (DAG model), release-please (prior art / inspiration) (+4 more)

### Community 15 - "Tasks: [FEATURE NAME]"
Cohesion: 0.07
Nodes (26): Dependencies & Execution Order, Format: `[ID] [P?] [Story] Description`, Implementation for User Story 1, Implementation for User Story 2, Implementation for User Story 3, Implementation Strategy, Incremental Delivery, MVP First (User Story 1 Only) (+18 more)

### Community 16 - "Core Principles"
Cohesion: 0.11
Nodes (17): Core Principles, Development Workflow and Quality Gates, Engineering Constraints, Governance, I. Explicit State Over Implicit Behavior, II. Git Is the Durable Desired-State Boundary, III. Deterministic, Reconstructible Operation, IV. Control-Plane Conventions, Never Obscured (+9 more)

### Community 17 - "git.go"
Cohesion: 0.16
Nodes (10): capWriter, CherryPickConflict, Commit, MergeConflict, checkMinVersion(), Buffer, parseGitVersion(), T (+2 more)

### Community 18 - "Content Equivalence Ladder"
Cohesion: 0.07
Nodes (34): Live Merge-Method Fence, Merge-Commit Backflow Strategy, Skip-in-Range Fence, Git 2.45 Runtime Contract, Git Runner Shell-Out, Conflict Issue Marker-and-Label Identity, Durable Backflow Conflict Artifact, Lock-Free Conflict Issue Convergence (+26 more)

### Community 19 - "annotationHandler"
Cohesion: 0.22
Nodes (10): Attr, Handler, escapeAnnotation(), escapeAzureAnnotation(), formatAnnotation(), Writer, Level, annotationHandler (+2 more)

### Community 20 - "validate_test.go"
Cohesion: 0.31
Nodes (15): PromotionGraph, T, TestDefault(), TestDefaultIsIdempotent(), TestDefaultMergeStrategyExpectedMergeMethod(), TestValidateAcceptsAtSignBranchName(), TestValidateAcceptsCanonicalGraph(), TestValidateAcceptsCherryPickMergeMethods() (+7 more)

### Community 21 - "Feature Specification: [FEATURE NAME]"
Cohesion: 0.14
Nodes (13): Assumptions, Edge Cases, Feature Specification: [FEATURE NAME], Functional Requirements, Key Entities *(include if feature involves data)*, Measurable Outcomes, Out of Scope *(mandatory)*, Requirements *(mandatory)* (+5 more)

### Community 22 - "Core Principles"
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
Cohesion: 0.10
Nodes (48): adoFake, adoFakePull, adoFakeWI, pullSpec, T, issueStates(), TestCloseConflictArtifactUsesCompletedCategoryState(), TestCreateConflictArtifactEscapesBodyAndTags() (+40 more)

### Community 28 - "github/units_test.go"
Cohesion: 0.53
Nodes (5): T, TestAPIErrorMessage(), TestBaseURL(), TestErrNoResponseWraps(), TestScrubToken()

### Community 29 - "azuredevops/units_test.go"
Cohesion: 0.60
Nodes (4): T, TestErrNoResponseWraps(), TestRepoString(), TestScrubToken()

### Community 30 - "[CHECKLIST TYPE] Checklist: [FEATURE NAME]"
Cohesion: 0.40
Nodes (4): [Category 1], [Category 2], [CHECKLIST TYPE] Checklist: [FEATURE NAME], Notes

### Community 31 - "CLI Exit-Code Contract"
Cohesion: 0.67
Nodes (4): CLI Exit-Code Contract, Read-Only Drift Gate, Scheduled Drift Gate, Human-Attention Exit 3

### Community 32 - "forge_test.go"
Cohesion: 0.67
Nodes (3): T, TestMergeCommitAllowed(), TestMergeMethodsAllows()

### Community 35 - "GoReleaser Publication"
Cohesion: 0.24
Nodes (11): Floating Major Action Tag, GoReleaser Publication, Release Tag Monotonicity Guard, Annotated Immutable SemVer Tag, Release Please Automation, Release Bot GitHub App Token, Release PR Label Reconciliation, Release Checksums (+3 more)

### Community 36 - "CI Workflow"
Cohesion: 0.24
Nodes (10): Skaphos Contribution Governance, Cross-Platform Test Matrix, DCO Sign-Off Gate, Generated Artifact Drift Check, REUSE License Gate, GoReleaser Snapshot Build, Staticcheck and Govulncheck, CI Workflow (+2 more)

### Community 37 - "Forge"
Cohesion: 0.26
Nodes (20): Agent Safety Rules (do not violate), Engine Purity Rules, Managed Change Requests, Layering Rule: entrypoint to engine to git/forge, depguard-enforced, Forge, assertAscending(), containsID(), findArtifact() (+12 more)

### Community 48 - "Provider"
Cohesion: 0.09
Nodes (15): apiError, capWriter, errNoResponse, Provider, Client, Duration, Header, Response (+7 more)

### Community 50 - "0010 — Exported validation and defaulting on the config API"
Cohesion: 0.29
Nodes (6): 0010 — Exported validation and defaulting on the config API, Consequences, Context, Decision, Links, Options considered

### Community 51 - "action_test.go"
Cohesion: 0.50
Nodes (3): actionMetadata, T, TestPublishedActionRunnerContract()

### Community 54 - "Deterministic Backflow Return Branch"
Cohesion: 0.67
Nodes (3): Deterministic Backflow Return Branch, Event-Driven Concurrency Without Locks, Supersede Stale Backflow Request

### Community 65 - "ChangeRequest"
Cohesion: 0.08
Nodes (45): adoPull, adoPullList, forkRef, gitRef, propertiesCollection, refList, refUpdateResult, refUpdateResults (+37 more)

### Community 66 - "v1/types.go"
Cohesion: 0.08
Nodes (33): BackflowPolicy, Branch, Expectations, Promotion, Branch, Graph, Expectations, Promotion (+25 more)

### Community 67 - "tmpl.go"
Cohesion: 0.12
Nodes (37): FuncMap, checkBodySafety(), compileTemplate(), Default(), execute(), funcMap(), PromotionGraph, NewCommit() (+29 more)

### Community 68 - "ConflictArtifactID"
Cohesion: 0.12
Nodes (19): policyConfiguration, policyList, policyScope, policySettings, wiqlResult, wiState, wiStates, workItem (+11 more)

### Community 69 - "ParseRemoteURL"
Cohesion: 0.26
Nodes (14): Repo, orgFromCollectionURI(), ParseRemoteURL(), pathSegments(), repoFromEnv(), ResolveRepo(), splitRemote(), T (+6 more)

### Community 70 - "Deploying Oiax from Azure Pipelines"
Cohesion: 0.08
Nodes (23): 0009 — Azure DevOps forge provider, Authentication and the token, Consequences, Context, Decision, Links, Marker storage on a managed request, Options considered (+15 more)

### Community 71 - "github.go"
Cohesion: 0.11
Nodes (25): apiError, apiError, errNoResponse, ghIssue, ghLabel, ghPull, ghRef, ghRepo (+17 more)

### Community 78 - "azure_pipelines_test.go"
Cohesion: 0.50
Nodes (3): pipelineTemplate, T, TestPublishedAzurePipelinesTemplateContract()

### Community 79 - "0011 — Templatable request text"
Cohesion: 0.08
Nodes (22): 0011 — Templatable request text, Consequences, Context, Decision, Links, Options considered, Example request-text templates, A minimal change-record setup (+14 more)

## Knowledge Gaps
- **156 isolated node(s):** `common.sh script`, `github.com/skaphos/oiax`, `actionMetadata`, `pipelineTemplate`, `versionInfo` (+151 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **19 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Context` connect `Context` to `ChangeRequest`, `Provider`, `tmpl.go`, `ConflictArtifactID`, `loadGraph`, `ParseRemoteURL`, `fakeForge`, `github.go`, `Coordinator`, `plan_reconcile_test.go`, `Provider`, `annotationHandler`, `MergeMethods`?**
  _High betweenness centrality (0.239) - this node is a cross-community bridge._
- **Why does `InitRepo()` connect `plan_reconcile_test.go` to `reconcile_test.go`, `github_test.go`, `ParseRemoteURL`, `newRepo`, `writeJSON`?**
  _High betweenness centrality (0.070) - this node is a cross-community bridge._
- **Why does `Runner` connect `Context` to `reconcile_test.go`, `Coordinator`, `loadGraph`, `newRepo`, `git.go`?**
  _High betweenness centrality (0.059) - this node is a cross-community bridge._
- **Are the 5 inferred relationships involving `testGraph()` (e.g. with `TestScenarioBackflowMixedDropAndApplyConverges()` and `TestScenarioBackflowPushIsByteIdenticalAcrossIndependentRepos()`) actually correct?**
  _`testGraph()` has 5 INFERRED edges - model-reasoned connections that need verification._
- **Are the 5 inferred relationships involving `gitHarness()` (e.g. with `InitRepo()` and `TestScenarioBackflowMixedDropAndApplyConverges()`) actually correct?**
  _`gitHarness()` has 5 INFERRED edges - model-reasoned connections that need verification._
- **What connects `common.sh script`, `github.com/skaphos/oiax`, `actionMetadata` to the rest of the system?**
  _156 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `reconcile_test.go` be split into smaller, more focused modules?**
  _Cohesion score 0.08345752608047691 - nodes in this community are weakly interconnected._