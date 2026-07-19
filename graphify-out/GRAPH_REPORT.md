# Graph Report - ska-54-templatable-pr-text  (2026-07-18)

## Corpus Check
- 100 files · ~160,753 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 1593 nodes · 4066 edges · 91 communities (80 shown, 11 thin omitted)
- Extraction: 91% EXTRACTED · 9% INFERRED · 0% AMBIGUOUS · INFERRED: 352 edges (avg confidence: 0.81)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `e8340f95`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- reconcile_test.go
- Provider
- github_test.go
- Coordinator
- newPlanCommand
- Context
- Backflow Execution
- ChangeRequest
- BuildPlan
- Load
- run
- git_test.go
- render_test.go
- oiax validate
- oiax
- CLI reference
- 0007 — Keep the git layer a shell-out to the git binary
- Content Equivalence Ladder
- NewLogger
- validate_test.go
- Event-Driven GitHub Action Reconciliation
- Contributing to oiax
- Changelog
- Modeling your promotion graph
- Troubleshooting
- oiax plan
- writeJSON
- Installing Oiax with an AI agent
- Operating Oiax day to day
- Configuration reference
- Getting started
- Deploying Oiax as a GitHub Action
- `Action`
- Backflow: returning hotfixes
- GoReleaser Publication
- CI Workflow
- Code map
- Recipes
- Architecture
- AGENTS.md
- 0006 — Merge-commit backflow strategy
- Minimizing divergence
- oiax completion bash
- oiax completion zsh
- 0001 — Adopt the name Oiax
- Oiax documentation
- Release process
- Provider
- 0003 — Read configuration from a pinned ref
- 0010 — Exported validation and defaulting on the config API
- action_test.go
- Guides
- Copilot instructions for oiax
- Deterministic Backflow Return Branch
- TestPromotionGraphJSONRoundTrip
- Source-First Promotion Rollback
- Isolated Environment-Specific Configuration
- Immutable Release Marketplace Constraint
- pkg/api/v1alpha1 (public configuration API)
- MIT License
- github.com/skaphos/oiax
- github.com/skaphos/oiax/tools
- Provider
- .Validate
- tmpl.go
- ConflictArtifactID
- ParseRemoteURL
- Deploying Oiax from Azure Pipelines
- artifacts.go
- loadGraph
- 0009 — Azure DevOps forge provider
- git.go
- NewRootCommand
- 0004-backflow-execution.md
- azure_pipelines_test.go
- Request-text templates
- Detect
- 0008 — Durable backflow-conflict artifact
- 0011 — Templatable request text
- Governance change-record templates
- newGraphCommand
- 0002 — Detect divergence by content, not ancestry
- 0004 — Backflow execution
- 0005 — Config API v1
- newGenCommand
- planExitCode

## God Nodes (most connected - your core abstractions)
1. `Context` - 144 edges
2. `Plan` - 66 edges
3. `testGraph()` - 64 edges
4. `gitHarness()` - 64 edges
5. `checkout()` - 59 edges
6. `writeJSON()` - 51 edges
7. `newProvider()` - 46 edges
8. `Runner` - 38 edges
9. `newRepo()` - 38 edges
10. `Provider` - 36 edges

## Surprising Connections (you probably didn't know these)
- `BuildPlan()` --conceptually_related_to--> `Reconciliation Loop`  [INFERRED]
  internal/engine/plan.go → docs/architecture.md
- `ChangeRequest` --conceptually_related_to--> `Managed Change Requests`  [INFERRED]
  internal/engine/types.go → docs/architecture.md
- `EdgeState` --shares_data_with--> `Content Equivalence Ladder`  [INFERRED]
  internal/engine/types.go → docs/architecture.md
- `Forge` --implements--> ``internal/forge/github``  [INFERRED]
  internal/forge/forge.go → docs/code-map.md
- `oiax` --semantically_similar_to--> `argoproj-labs/gitops-promoter (prior art)`  [INFERRED] [semantically similar]
  README.md → docs/architecture.md

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **CI Quality Gate Suite** — github_workflows_ci_dco_gate, github_workflows_ci_reuse_gate, github_workflows_ci_cross_platform_tests, github_workflows_ci_static_analysis, github_workflows_ci_generated_artifact_check, github_workflows_ci_snapshot_build [EXTRACTED 1.00]
- **Automated Release Pipeline** — github_workflows_release_please_automation, github_workflows_release_please_annotated_tag, github_workflows_release_goreleaser_publication, github_workflows_release_floating_major_tag [EXTRACTED 1.00]
- **Convergent Backflow Execution** — adr0004_deterministic_return_branch, adr0004_identity_ladder, adr0004_ephemeral_worktree, adr0004_conflict_divergence, adr0004_supersede_stale_request [EXTRACTED 1.00]
- **Backflow Execution Model** — adr0006_merge_commit_backflow, docs_guides_backflow_deterministic_request, docs_guides_backflow_conflict_handling [EXTRACTED 1.00]
- **Reconciliation Layer Model** — docs_architecture_pure_reconciliation_layers, docs_code_map_engine_core, docs_code_map_reconcile_layer, docs_code_map_git_layer [EXTRACTED 1.00]

## Communities (91 total, 11 thin omitted)

### Community 0 - "reconcile_test.go"
Cohesion: 0.10
Nodes (98): Plan, BackflowBranchName(), assertExclusionReason(), checkout(), commitOn(), conflictHarness(), containsSHA(), countBackflowOutcome() (+90 more)

### Community 1 - "Provider"
Cohesion: 0.08
Nodes (34): apiError, apiError, errNoResponse, ghIssue, ghLabel, ghPull, ghRef, ghRepo (+26 more)

### Community 2 - "github_test.go"
Cohesion: 0.08
Nodes (83): ghFake, ghFakeIssue, ghFakePull, issueSpec, prSpec, ghNum(), HandlerFunc, Mutex (+75 more)

### Community 3 - "Coordinator"
Cohesion: 0.11
Nodes (26): EdgeObservation, backflowToReturn(), EvaluateEdge(), Commit, commits(), Commit, T, idSet() (+18 more)

### Community 4 - "newPlanCommand"
Cohesion: 0.18
Nodes (16): forgeKind, Command, newPlanCommand(), Command, newReconcileCommand(), buildCoordinator(), buildLogger(), Command (+8 more)

### Community 6 - "Backflow Execution"
Cohesion: 0.05
Nodes (42): Downloaded Artifact Verification, Oiax Composite GitHub Action, Action Pinned Config Ref, Git Ref Preparation, Release Binary Download, Human-in-the-Loop Steering, Adopt the Name Oiax, Tiller Ecosystem Collision (+34 more)

### Community 7 - "ChangeRequest"
Cohesion: 0.11
Nodes (13): fakeForge, ChangeRequest, RequestType, BranchPush, CreateRequest, MergeMethods, Reason, RequestFilter (+5 more)

### Community 8 - "BuildPlan"
Cohesion: 0.09
Nodes (58): `internal/engine`, Action, ActionType, BackflowExclusion, BackflowExclusionReason, BackflowPolicy, BranchState, Commit (+50 more)

### Community 9 - "Load"
Cohesion: 0.26
Nodes (14): `internal/config`, config.DefaultPath, PromotionGraph, IsDeprecatedAPIVersion(), Load(), Parse(), T, TestLoadAcceptsFileAtLimit() (+6 more)

### Community 10 - "run"
Cohesion: 0.10
Nodes (55): T, run(), TestDeprecatedAPIVersionWarns(), TestGenDocs(), TestGraphCommand(), TestInvalidOutputFlagRejected(), TestPlanAndReconcileAreHonestAboutScope(), TestRootRejectsJSONOutputWithoutSubcommand() (+47 more)

### Community 11 - "git_test.go"
Cohesion: 0.20
Nodes (40): T, newRepo(), oidLike(), requireGit(), runGit(), TestCherryPickCancelledContextIsOperationalError(), TestCherryPickConflict(), TestCherryPickDropsRedundant() (+32 more)

### Community 12 - "render_test.go"
Cohesion: 0.15
Nodes (27): actionVerb(), edgeSummaryText(), exclusionCounts(), Commit, Writer, mdCell(), RenderJSON(), RenderMarkdown() (+19 more)

### Community 13 - "oiax validate"
Cohesion: 0.12
Nodes (20): Backflow Policy Configuration, PromotionGraph Configuration Contract, `internal/cli`, oiax graph, oiax (root command), oiax validate, oiax version, Options (+12 more)

### Community 14 - "oiax"
Cohesion: 0.10
Nodes (26): Skaphos Glossary Discipline (branch promotion vs Promotion vs backflow), Design proposal in skaphos-resources under tools/oiax/, Conventional Commits, Signed commits + DCO sign-off, Backflow, Branch Promotion (capability), Deterministic Backflow Branch Naming, Drift Policy (forbidden/expected) (+18 more)

### Community 16 - "CLI reference"
Cohesion: 0.10
Nodes (20): CLI reference, oiax, oiax completion, oiax completion fish, oiax completion powershell, Options, Options, Options (+12 more)

### Community 17 - "0007 — Keep the git layer a shell-out to the git binary"
Cohesion: 0.33
Nodes (6): 0007 — Keep the git layer a shell-out to the git binary, Consequences, Context, Decision, Links, Options considered

### Community 18 - "Content Equivalence Ladder"
Cohesion: 0.14
Nodes (18): Git 2.45 Runtime Contract, Git Runner Shell-Out, ADR 0001: Adopt the name Oiax, Rationale: Tiller collided with Helm v2's Tiller in the target ecosystem; Oiax is the literal Greek for tiller and keeps the hand-on-the-helm intent, ADR 0002: Detect divergence by content, not ancestry, Rationale: squash/rebase merges rewrite SHAs; ancestry-only detection leaves edges permanently diverged and PR creation fails with HTTP 422; a private state database would violate the no-control-plane posture, Declarative Branch Promotion Reconciler, Content Equivalence Ladder (+10 more)

### Community 19 - "NewLogger"
Cohesion: 0.15
Nodes (18): Attr, Handler, escapeAnnotation(), escapeAzureAnnotation(), formatAnnotation(), Logger, Writer, NewLogger() (+10 more)

### Community 20 - "validate_test.go"
Cohesion: 0.31
Nodes (15): PromotionGraph, T, TestDefault(), TestDefaultIsIdempotent(), TestDefaultMergeStrategyExpectedMergeMethod(), TestValidateAcceptsAtSignBranchName(), TestValidateAcceptsCanonicalGraph(), TestValidateAcceptsCherryPickMergeMethods() (+7 more)

### Community 21 - "Event-Driven GitHub Action Reconciliation"
Cohesion: 0.14
Nodes (17): Live Merge-Method Fence, Merge-Commit Backflow Strategy, Skip-in-Range Fence, Conflict Issue Marker-and-Label Identity, Durable Backflow Conflict Artifact, Lock-Free Conflict Issue Convergence, Oiax Installation Artifacts, Agent Installation Confirmation Gate (+9 more)

### Community 22 - "Contributing to oiax"
Cohesion: 0.13
Nodes (18): Agent Safety Rules (do not violate), Commits, Contributing to oiax, Design invariants, Documentation, Generated artifacts, Local validation, Workflow (+10 more)

### Community 23 - "Changelog"
Cohesion: 0.13
Nodes (14): 1.0.0 (2026-07-12), [1.0.1](https://github.com/skaphos/oiax/compare/v1.0.0...v1.0.1) (2026-07-13), [1.0.2](https://github.com/skaphos/oiax/compare/v1.0.1...v1.0.2) (2026-07-13), [1.0.3](https://github.com/skaphos/oiax/compare/v1.0.2...v1.0.3) (2026-07-13), [1.1.0](https://github.com/skaphos/oiax/compare/v1.0.3...v1.1.0) (2026-07-18), ⚠ BREAKING CHANGES, Bug Fixes, Bug Fixes (+6 more)

### Community 24 - "Modeling your promotion graph"
Cohesion: 0.11
Nodes (18): A minimal graph, Branches, Drift policy, Fan-out, Linear pipeline, Merge-method expectations, Migrating the apiVersion, Modeling your promotion graph (+10 more)

### Community 25 - "Troubleshooting"
Cohesion: 0.11
Nodes (19): A backflow halted on a conflict, A configured branch does not exist, `apiVersion "…v1alpha1" is deprecated`, Azure DevOps: `work-item type "Issue" does not exist` (or a create/close failure), Cannot determine owner/repo, `cannot resolve the repository default branch`, Configuration won't load, `git 2.45 or newer is required` (+11 more)

### Community 26 - "oiax plan"
Cohesion: 0.11
Nodes (24): CLI Exit-Code Contract, Read-Only Drift Gate, Plan-First Rollout, oiax plan, oiax reconcile, Options, Options, Options inherited from parent commands (+16 more)

### Community 27 - "writeJSON"
Cohesion: 0.10
Nodes (48): adoFake, adoFakePull, adoFakeWI, pullSpec, T, issueStates(), TestCloseConflictArtifactUsesCompletedCategoryState(), TestCreateConflictArtifactEscapesBodyAndTags() (+40 more)

### Community 28 - "Installing Oiax with an AI agent"
Cohesion: 0.14
Nodes (14): 1. Check preconditions, 2. Discover the repository shape, 3. Infer the promotion graph, 4. Confirm the inference with the user (gate — do not skip), 5. Write and locally verify `.oiax.yaml`, 6. Handle adoption (existing promotion PRs), 7. Write the workflow, 8. Set up the token (human step) (+6 more)

### Community 29 - "Operating Oiax day to day"
Cohesion: 0.14
Nodes (14): Approvals can go stale, Branch protection and required checks, Exit codes and CI gating, Next steps, Observability, Obsolete requests are closed, not deleted, Operating Oiax day to day, Reading a plan (+6 more)

### Community 30 - "Configuration reference"
Cohesion: 0.22
Nodes (9): Configuration reference, Environment variables, Flags, Migration / deprecated alias, PromotionGraph, `spec.backflow`, `spec.branches.<name>`, `spec.promotions[]` (+1 more)

### Community 31 - "Getting started"
Cohesion: 0.17
Nodes (12): 1. Install, 2. Write your first graph, 3. Inspect it locally, 4. See what Oiax would do, 5. Apply, 6. Deploy it, Adopting Oiax on an existing repository, From source (+4 more)

### Community 32 - "Deploying Oiax as a GitHub Action"
Cohesion: 0.17
Nodes (12): Choosing a mode, Concurrency, Deploying Oiax as a GitHub Action, `fetch-depth: 0` is not optional, Inputs, Large repositories: partial clone, Next steps, Permissions (+4 more)

### Community 33 - "`Action`"
Cohesion: 0.14
Nodes (14): `Action`, `branch`, `Commit`, Compatibility, `Edge`, `equivalence`, `Exclusion`, `Plan` (+6 more)

### Community 34 - "Backflow: returning hotfixes"
Cohesion: 0.13
Nodes (15): All-or-nothing (the merge strategy), "Already returned" — the identity check, Backflow: returning hotfixes, Choosing a strategy, Configuration, Next steps, One active request, superseded on a new hotfix, Re-pushes when the target advances (accepted, bounded) (+7 more)

### Community 35 - "GoReleaser Publication"
Cohesion: 0.24
Nodes (11): Floating Major Action Tag, GoReleaser Publication, Release Tag Monotonicity Guard, Annotated Immutable SemVer Tag, Release Please Automation, Release Bot GitHub App Token, Release PR Label Reconciliation, Release Checksums (+3 more)

### Community 36 - "CI Workflow"
Cohesion: 0.24
Nodes (10): Skaphos Contribution Governance, Cross-Platform Test Matrix, DCO Sign-Off Gate, Generated Artifact Drift Check, REUSE License Gate, GoReleaser Snapshot Build, Staticcheck and Govulncheck, CI Workflow (+2 more)

### Community 37 - "Code map"
Cohesion: 0.17
Nodes (27): `cmd/oiax`, Code map, `internal/cienv`, `internal/forge`, `internal/forge/azuredevops`, `internal/forge/github`, `internal/forge/marker`, `internal/reconcile` (+19 more)

### Community 38 - "Recipes"
Cohesion: 0.22
Nodes (9): Consume the plan as JSON, Gate CI on drift (read-only policy check), Next steps, Preview a graph change before it lands, Recipes, Roll back a promotion that landed, Roll out plan-first, Run multiple pipelines in one repository (monorepo) (+1 more)

### Community 39 - "Architecture"
Cohesion: 0.25
Nodes (8): Architecture, Failure handling and observability, Layers, Managed change requests, Prior art, Roadmap, The equivalence ladder, The model

### Community 40 - "AGENTS.md"
Cohesion: 0.29
Nodes (5): Building and testing, Conventions, Release constraints, Safety rules (do not violate), What this is

### Community 41 - "0006 — Merge-commit backflow strategy"
Cohesion: 0.33
Nodes (6): 0006 — Merge-commit backflow strategy, Consequences, Context, Decision, Links, Options considered

### Community 42 - "Minimizing divergence"
Cohesion: 0.29
Nodes (7): Gate on drift so divergence is loud, Isolate environment-specific configuration, Keep hotfixes short-lived, Minimizing divergence, Next steps, Prefer merge commits on promotion targets, When divergence still happens

### Community 43 - "oiax completion bash"
Cohesion: 0.29
Nodes (7): Linux:, macOS:, oiax completion bash, Options, Options inherited from parent commands, SEE ALSO, Synopsis

### Community 44 - "oiax completion zsh"
Cohesion: 0.29
Nodes (7): Linux:, macOS:, oiax completion zsh, Options, Options inherited from parent commands, SEE ALSO, Synopsis

### Community 45 - "0001 — Adopt the name Oiax"
Cohesion: 0.33
Nodes (5): 0001 — Adopt the name Oiax, Consequences, Context, Decision, Options considered

### Community 46 - "Oiax documentation"
Cohesion: 0.33
Nodes (6): Decisions, Design and internals, Guides, Oiax documentation, Process, Reference

### Community 47 - "Release process"
Cohesion: 0.40
Nodes (5): Governance, How a release happens, Publishing to the GitHub Marketplace, Release process, Required credentials

### Community 48 - "Provider"
Cohesion: 0.09
Nodes (15): apiError, capWriter, errNoResponse, Provider, Client, Duration, Header, Response (+7 more)

### Community 49 - "0003 — Read configuration from a pinned ref"
Cohesion: 0.40
Nodes (5): 0003 — Read configuration from a pinned ref, Consequences, Context, Decision, Options considered

### Community 50 - "0010 — Exported validation and defaulting on the config API"
Cohesion: 0.33
Nodes (6): 0010 — Exported validation and defaulting on the config API, Consequences, Context, Decision, Links, Options considered

### Community 51 - "action_test.go"
Cohesion: 0.50
Nodes (3): actionMetadata, T, TestPublishedActionRunnerContract()

### Community 52 - "Guides"
Cohesion: 0.50
Nodes (4): Guides, Reference, Set up, Use

### Community 53 - "Copilot instructions for oiax"
Cohesion: 0.50
Nodes (3): Copilot instructions for oiax, Invariants that must not be violated, Workflow expectations

### Community 54 - "Deterministic Backflow Return Branch"
Cohesion: 0.67
Nodes (3): Deterministic Backflow Return Branch, Event-Driven Concurrency Without Locks, Supersede Stale Backflow Request

### Community 65 - "Provider"
Cohesion: 0.10
Nodes (29): adoPull, adoPullList, forkRef, gitRef, propertiesCollection, refList, refUpdateResult, refUpdateResults (+21 more)

### Community 66 - ".Validate"
Cohesion: 0.26
Nodes (8): findCycle(), Branch, Promotion, PromotionGraph, sortedBranchNames(), validateRefName(), validateRequestTemplate(), validateTemplatePath()

### Community 67 - "tmpl.go"
Cohesion: 0.08
Nodes (51): Branch, FuncMap, checkBodySafety(), compileTemplate(), Default(), execute(), funcMap(), PromotionGraph (+43 more)

### Community 68 - "ConflictArtifactID"
Cohesion: 0.18
Nodes (9): workItem, ConflictArtifact, ConflictArtifactID, ConflictArtifactSpec, artifactID(), fieldString(), Provider, htmlBody() (+1 more)

### Community 69 - "ParseRemoteURL"
Cohesion: 0.26
Nodes (14): Repo, orgFromCollectionURI(), ParseRemoteURL(), pathSegments(), repoFromEnv(), ResolveRepo(), splitRemote(), T (+6 more)

### Community 70 - "Deploying Oiax from Azure Pipelines"
Cohesion: 0.14
Nodes (14): Azure Repos, Choosing a mode, Connecting Azure DevOps to GitHub, Create the service connection, Deploying Oiax from Azure Pipelines, `fetchDepth: 0` is not optional, Next steps, Parameters (+6 more)

### Community 71 - "artifacts.go"
Cohesion: 0.21
Nodes (12): policyConfiguration, policyList, policyScope, policySettings, wiqlResult, wiState, wiStates, workItemBatch (+4 more)

### Community 72 - "loadGraph"
Cohesion: 0.22
Nodes (11): exitCodeError, options, effectiveConfigRef(), Command, Graph, loadGraph(), readWorkingTreeFile(), requireTextOutput() (+3 more)

### Community 73 - "0009 — Azure DevOps forge provider"
Cohesion: 0.22
Nodes (9): 0009 — Azure DevOps forge provider, Authentication and the token, Consequences, Context, Decision, Links, Marker storage on a managed request, Options considered (+1 more)

### Community 74 - "git.go"
Cohesion: 0.16
Nodes (10): capWriter, CherryPickConflict, Commit, MergeConflict, checkMinVersion(), Buffer, parseGitVersion(), T (+2 more)

### Community 76 - "NewRootCommand"
Cohesion: 0.27
Nodes (7): versionInfo, main(), Execute(), NewRootCommand(), Command, newVersionCommand(), printVersion()

### Community 78 - "azure_pipelines_test.go"
Cohesion: 0.50
Nodes (3): pipelineTemplate, T, TestPublishedAzurePipelinesTemplateContract()

### Community 79 - "Request-text templates"
Cohesion: 0.22
Nodes (9): Configuration keys, Functions, Rendering rules and constraints, Request-text templates, `spec.templates.backflowMergeMessage`, `spec.templates.promotion`, `.backflow`, `.backflowConflict`, Untrusted variables, Variable context (+1 more)

### Community 80 - "Detect"
Cohesion: 0.40
Nodes (4): Kind, Detect(), T, TestDetect()

### Community 82 - "0008 — Durable backflow-conflict artifact"
Cohesion: 0.33
Nodes (6): 0008 — Durable backflow-conflict artifact, Consequences, Context, Decision, Links, Options considered

### Community 83 - "0011 — Templatable request text"
Cohesion: 0.33
Nodes (6): 0011 — Templatable request text, Consequences, Context, Decision, Links, Options considered

### Community 84 - "Governance change-record templates"
Cohesion: 0.33
Nodes (6): A minimal change-record setup, Backflow and conflict records, Governance change-record templates, Next steps, Untrusted input, one more time, What renders when

### Community 85 - "newGraphCommand"
Cohesion: 0.53
Nodes (4): Command, Graph, newGraphCommand(), printGraph()

### Community 86 - "0002 — Detect divergence by content, not ancestry"
Cohesion: 0.40
Nodes (5): 0002 — Detect divergence by content, not ancestry, Consequences, Context, Decision, Options considered

### Community 87 - "0004 — Backflow execution"
Cohesion: 0.40
Nodes (5): 0004 — Backflow execution, Consequences, Context, Decision, Options considered

### Community 88 - "0005 — Config API v1"
Cohesion: 0.40
Nodes (5): 0005 — Config API v1, Consequences, Context, Decision, Options considered

### Community 89 - "newGenCommand"
Cohesion: 0.83
Nodes (3): Command, newGenCommand(), writeCommandReference()

## Ambiguous Edges - Review These
- `oiax` → `Design proposal in skaphos-resources under tools/oiax/`  [AMBIGUOUS]
  AGENTS.md · relation: rationale_for

## Knowledge Gaps
- **329 isolated node(s):** `github.com/skaphos/oiax`, `actionMetadata`, `pipelineTemplate`, `versionInfo`, `Graph` (+324 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **11 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **What is the exact relationship between `oiax` and `Design proposal in skaphos-resources under tools/oiax/`?**
  _Edge tagged AMBIGUOUS (relation: rationale_for) - confidence is low._
- **Why does `Code map` connect `Code map` to `BuildPlan`, `Load`, `README.md`, `oiax validate`, `Contributing to oiax`?**
  _High betweenness centrality (0.259) - this node is a cross-community bridge._
- **Why does `Context` connect `Context` to `Provider`, `Provider`, `tmpl.go`, `ConflictArtifactID`, `newPlanCommand`, `ParseRemoteURL`, `artifacts.go`, `ChangeRequest`, `Coordinator`, `run`, `Provider`, `NewLogger`?**
  _High betweenness centrality (0.179) - this node is a cross-community bridge._
- **Why does ``internal/engine`` connect `BuildPlan` to `reconcile_test.go`, `Code map`, `Contributing to oiax`, `ChangeRequest`?**
  _High betweenness centrality (0.138) - this node is a cross-community bridge._
- **Are the 5 inferred relationships involving `testGraph()` (e.g. with `TestScenarioBackflowMixedDropAndApplyConverges()` and `TestScenarioBackflowPushIsByteIdenticalAcrossIndependentRepos()`) actually correct?**
  _`testGraph()` has 5 INFERRED edges - model-reasoned connections that need verification._
- **What connects `github.com/skaphos/oiax`, `actionMetadata`, `pipelineTemplate` to the rest of the system?**
  _329 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `reconcile_test.go` be split into smaller, more focused modules?**
  _Cohesion score 0.10396039603960396 - nodes in this community are weakly interconnected._