# Graph Report - .  (2026-07-17)

## Corpus Check
- 85 files · ~115,370 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 1169 nodes · 2894 edges · 65 communities (57 shown, 8 thin omitted)
- Extraction: 91% EXTRACTED · 9% INFERRED · 0% AMBIGUOUS · INFERRED: 247 edges (avg confidence: 0.81)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- Backflow Reconcile Tests
- GitHub Client Errors
- GitHub Test Fixtures
- Engine Domain Model
- CLI Entrypoint Errors
- Git Conflict Types
- GitHub Action Delivery
- Forge Test Double
- Plan Validation
- Configuration Model
- Plan CLI Tests
- Git Test Helpers
- Plan Rendering
- CLI Commands
- Glossary and Drift
- CLI Reference Index
- Content Divergence ADR
- Architectural Rationale
- Logging Annotations
- CLI Tests
- Merge Commit Backflow
- Contribution Safety
- Release Changelog
- Promotion Graph Patterns
- Troubleshooting Scenarios
- Drift Gate Contracts
- Edge Evaluation Tests
- Agent Install Workflow
- Operational Lifecycle
- Reconcile CLI Reference
- Getting Started
- GitHub Action Operations
- Plan JSON Schema
- Backflow Guide
- Release Automation
- CI Governance
- Package Ownership
- Operational Recipes
- System Architecture
- Agent Repository Guidance
- Merge Strategy ADR
- Divergence Minimization
- Bash Completion
- Zsh Completion
- Naming ADR
- Documentation Index
- Release Process
- Configuration Contracts
- Pinned Config ADR
- Config API ADR
- Action Contract Tests
- Documentation Navigation
- Copilot Guidance
- Backflow Request Lifecycle
- API Round Trip Tests
- Promotion Rollback
- Environment Isolation
- Marketplace Release
- Legacy Config API
- MIT License
- Root Go Module
- Tools Go Module

## God Nodes (most connected - your core abstractions)
1. `Plan` - 56 edges
2. `gitHarness()` - 54 edges
3. `testGraph()` - 52 edges
4. `checkout()` - 49 edges
5. `newProvider()` - 46 edges
6. `writeJSON()` - 41 edges
7. `Provider` - 37 edges
8. `Runner` - 36 edges
9. `newRepo()` - 33 edges
10. `writeCommit()` - 30 edges

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

## Communities (65 total, 8 thin omitted)

### Community 0 - "Backflow Reconcile Tests"
Cohesion: 0.13
Nodes (80): Plan, BackflowBranchName(), assertExclusionReason(), checkout(), commitOn(), conflictHarness(), containsSHA(), countBackflowOutcome() (+72 more)

### Community 1 - "GitHub Client Errors"
Cohesion: 0.07
Nodes (42): Client, Duration, apiError, errNoResponse, ghIssue, ghLabel, ghPull, ghRef (+34 more)

### Community 2 - "GitHub Test Fixtures"
Cohesion: 0.11
Nodes (72): issueSpec, prSpec, assertAuth(), assertNoToken(), decode(), T, newProvider(), runGit() (+64 more)

### Community 3 - "Engine Domain Model"
Cohesion: 0.09
Nodes (36): `internal/engine`, Action, ActionType, BackflowExclusion, BackflowExclusionReason, BranchState, ChangeRequest, Commit (+28 more)

### Community 4 - "CLI Entrypoint Errors"
Cohesion: 0.07
Nodes (39): exitCodeError, options, versionInfo, main(), Command, newGenCommand(), writeCommandReference(), Command (+31 more)

### Community 5 - "Git Conflict Types"
Cohesion: 0.12
Nodes (12): capWriter, CherryPickConflict, Commit, MergeConflict, Runner, checkMinVersion(), Buffer, Context (+4 more)

### Community 6 - "GitHub Action Delivery"
Cohesion: 0.05
Nodes (42): Downloaded Artifact Verification, Oiax Composite GitHub Action, Action Pinned Config Ref, Git Ref Preparation, Release Binary Download, Human-in-the-Loop Steering, Adopt the Name Oiax, Tiller Ecosystem Collision (+34 more)

### Community 7 - "Forge Test Double"
Cohesion: 0.12
Nodes (15): fakeForge, BranchPush, ConflictArtifact, ConflictArtifactID, ConflictArtifactSpec, CreateRequest, MergeMethods, Reason (+7 more)

### Community 8 - "Plan Validation"
Cohesion: 0.15
Nodes (35): FromConfig(), BuildPlan(), edge(), Graph, T, mergeBackflowEdge(), mergeGraph(), TestBackflowBranchName() (+27 more)

### Community 9 - "Configuration Model"
Cohesion: 0.09
Nodes (34): `internal/config`, BackflowPolicy, Branch, Expectations, Promotion, config.DefaultPath, IsDeprecatedAPIVersion(), Load() (+26 more)

### Community 10 - "Plan CLI Tests"
Cohesion: 0.16
Nodes (33): T, runCode(), setupRepo(), TestPlanAssertsGitFloorBeforeConfigRead(), TestPlanExitCode(), TestPlanForgeErrorExitsOne(), TestPlanInSyncExitsZero(), TestPlanJSONShape() (+25 more)

### Community 11 - "Git Test Helpers"
Cohesion: 0.23
Nodes (34): T, newRepo(), oidLike(), requireGit(), runGit(), TestCherryPickConflict(), TestCherryPickDropsRedundant(), TestCherryPickHappyPath() (+26 more)

### Community 12 - "Plan Rendering"
Cohesion: 0.15
Nodes (27): actionVerb(), edgeSummaryText(), exclusionCounts(), Commit, Writer, mdCell(), RenderJSON(), RenderMarkdown() (+19 more)

### Community 13 - "CLI Commands"
Cohesion: 0.11
Nodes (22): `internal/cli`, oiax graph, oiax plan, oiax (root command), oiax validate, oiax version, Options, Options (+14 more)

### Community 14 - "Glossary and Drift"
Cohesion: 0.13
Nodes (20): Skaphos Glossary Discipline (branch promotion vs Promotion vs backflow), Design proposal in skaphos-resources under tools/oiax/, Backflow, Branch Promotion (capability), Deterministic Backflow Branch Naming, Drift Policy (forbidden/expected), Execution model, argoproj-labs/gitops-promoter (prior art) (+12 more)

### Community 16 - "CLI Reference Index"
Cohesion: 0.10
Nodes (20): CLI reference, oiax, oiax completion, oiax completion fish, oiax completion powershell, Options, Options, Options (+12 more)

### Community 17 - "Content Divergence ADR"
Cohesion: 0.11
Nodes (16): 0002 — Detect divergence by content, not ancestry, Consequences, Context, Decision, Options considered, 0004 — Backflow execution, Consequences, Context (+8 more)

### Community 18 - "Architectural Rationale"
Cohesion: 0.14
Nodes (18): Git 2.45 Runtime Contract, Git Runner Shell-Out, ADR 0001: Adopt the name Oiax, Rationale: Tiller collided with Helm v2's Tiller in the target ecosystem; Oiax is the literal Greek for tiller and keeps the hand-on-the-helm intent, ADR 0002: Detect divergence by content, not ancestry, Rationale: squash/rebase merges rewrite SHAs; ancestry-only detection leaves edges permanently diverged and PR creation fails with HTTP 422; a private state database would violate the no-control-plane posture, Declarative Branch Promotion Reconciler, Content Equivalence Ladder (+10 more)

### Community 19 - "Logging Annotations"
Cohesion: 0.16
Nodes (13): Attr, Handler, escapeAnnotation(), Context, Logger, Writer, NewLogger(), T (+5 more)

### Community 20 - "CLI Tests"
Cohesion: 0.35
Nodes (17): T, run(), TestDeprecatedAPIVersionWarns(), TestGenDocs(), TestGraphCommand(), TestInvalidOutputFlagRejected(), TestPlanAndReconcileAreHonestAboutScope(), TestRootRejectsJSONOutputWithoutSubcommand() (+9 more)

### Community 21 - "Merge Commit Backflow"
Cohesion: 0.14
Nodes (17): Live Merge-Method Fence, Merge-Commit Backflow Strategy, Skip-in-Range Fence, Conflict Issue Marker-and-Label Identity, Durable Backflow Conflict Artifact, Lock-Free Conflict Issue Convergence, Oiax Installation Artifacts, Agent Installation Confirmation Gate (+9 more)

### Community 22 - "Contribution Safety"
Cohesion: 0.14
Nodes (17): Agent Safety Rules (do not violate), Commits, Contributing to oiax, Design invariants, Documentation, Generated artifacts, Local validation, Workflow (+9 more)

### Community 23 - "Release Changelog"
Cohesion: 0.12
Nodes (16): 1.0.0 (2026-07-12), [1.0.1](https://github.com/skaphos/oiax/compare/v1.0.0...v1.0.1) (2026-07-13), [1.0.2](https://github.com/skaphos/oiax/compare/v1.0.1...v1.0.2) (2026-07-13), ⚠ BREAKING CHANGES, Bug Fixes, Bug Fixes, Changelog, Changelog (+8 more)

### Community 24 - "Promotion Graph Patterns"
Cohesion: 0.12
Nodes (17): A minimal graph, Branches, Drift policy, Fan-out, Linear pipeline, Merge-method expectations, Migrating the apiVersion, Modeling your promotion graph (+9 more)

### Community 25 - "Troubleshooting Scenarios"
Cohesion: 0.12
Nodes (17): A backflow halted on a conflict, A configured branch does not exist, `apiVersion "…v1alpha1" is deprecated`, Cannot determine owner/repo, `cannot resolve the repository default branch`, Configuration won't load, `git 2.45 or newer is required`, Managed pull request checks wait for approval (+9 more)

### Community 26 - "Drift Gate Contracts"
Cohesion: 0.18
Nodes (16): CLI Exit-Code Contract, Read-Only Drift Gate, Plan-First Rollout, oiax plan, oiax reconcile, Prompt Hotfix Backflow, Scheduled Drift Gate, Idempotent Reconciliation (+8 more)

### Community 27 - "Edge Evaluation Tests"
Cohesion: 0.41
Nodes (14): EvaluateEdge(), commits(), Commit, T, idSet(), patchIDs(), TestEvaluateEdge(), TestEvaluateEdgeExclusionReasons() (+6 more)

### Community 28 - "Agent Install Workflow"
Cohesion: 0.14
Nodes (14): 1. Check preconditions, 2. Discover the repository shape, 3. Infer the promotion graph, 4. Confirm the inference with the user (gate — do not skip), 5. Write and locally verify `.oiax.yaml`, 6. Handle adoption (existing promotion PRs), 7. Write the workflow, 8. Set up the token (human step) (+6 more)

### Community 29 - "Operational Lifecycle"
Cohesion: 0.14
Nodes (14): Approvals can go stale, Branch protection and required checks, Exit codes and CI gating, Next steps, Observability, Obsolete requests are closed, not deleted, Operating Oiax day to day, Reading a plan (+6 more)

### Community 30 - "Reconcile CLI Reference"
Cohesion: 0.14
Nodes (14): oiax reconcile, Options, Options inherited from parent commands, SEE ALSO, Synopsis, Configuration reference, Environment variables, Exit codes (+6 more)

### Community 31 - "Getting Started"
Cohesion: 0.17
Nodes (12): 1. Install, 2. Write your first graph, 3. Inspect it locally, 4. See what Oiax would do, 5. Apply, 6. Deploy it, Adopting Oiax on an existing repository, From source (works today) (+4 more)

### Community 32 - "GitHub Action Operations"
Cohesion: 0.17
Nodes (12): Choosing a mode, Concurrency, Deploying Oiax as a GitHub Action, `fetch-depth: 0` is not optional, Inputs, Large repositories: partial clone, Next steps, Permissions (+4 more)

### Community 33 - "Plan JSON Schema"
Cohesion: 0.17
Nodes (12): `Action`, `branch`, Compatibility, `Edge`, `equivalence`, `Exclusion`, `Plan`, Plan JSON format (`planFormatVersion: 1`) (+4 more)

### Community 34 - "Backflow Guide"
Cohesion: 0.18
Nodes (11): "Already returned" — the identity check, Backflow: returning hotfixes, Configuration, Next steps, One active request, superseded on a new hotfix, Requirements, The backflow branch name, The `Oiax-Backflow: skip` escape hatch (+3 more)

### Community 35 - "Release Automation"
Cohesion: 0.24
Nodes (11): Floating Major Action Tag, GoReleaser Publication, Release Tag Monotonicity Guard, Annotated Immutable SemVer Tag, Release Please Automation, Release Bot GitHub App Token, Release PR Label Reconciliation, Release Checksums (+3 more)

### Community 36 - "CI Governance"
Cohesion: 0.24
Nodes (10): Skaphos Contribution Governance, Cross-Platform Test Matrix, DCO Sign-Off Gate, Generated Artifact Drift Check, REUSE License Gate, GoReleaser Snapshot Build, Staticcheck and Govulncheck, CI Workflow (+2 more)

### Community 37 - "Package Ownership"
Cohesion: 0.25
Nodes (9): `cmd/oiax`, Code map, `internal/forge`, `internal/forge/github`, `internal/reconcile`, `internal/version`, Not Go, `pkg/api/v1` (+1 more)

### Community 38 - "Operational Recipes"
Cohesion: 0.22
Nodes (9): Consume the plan as JSON, Gate CI on drift (read-only policy check), Next steps, Preview a graph change before it lands, Recipes, Roll back a promotion that landed, Roll out plan-first, Run multiple pipelines in one repository (monorepo) (+1 more)

### Community 39 - "System Architecture"
Cohesion: 0.25
Nodes (8): Architecture, Failure handling and observability, Layers, Managed change requests, Prior art, Roadmap, The equivalence ladder, The model

### Community 40 - "Agent Repository Guidance"
Cohesion: 0.29
Nodes (5): Building and testing, Conventions, Release constraints, Safety rules (do not violate), What this is

### Community 41 - "Merge Strategy ADR"
Cohesion: 0.29
Nodes (6): 0006 — Merge-commit backflow strategy, Consequences, Context, Decision, Links, Options considered

### Community 42 - "Divergence Minimization"
Cohesion: 0.29
Nodes (7): Gate on drift so divergence is loud, Isolate environment-specific configuration, Keep hotfixes short-lived, Minimizing divergence, Next steps, Prefer merge commits on promotion targets, When divergence still happens

### Community 43 - "Bash Completion"
Cohesion: 0.29
Nodes (7): Linux:, macOS:, oiax completion bash, Options, Options inherited from parent commands, SEE ALSO, Synopsis

### Community 44 - "Zsh Completion"
Cohesion: 0.29
Nodes (7): Linux:, macOS:, oiax completion zsh, Options, Options inherited from parent commands, SEE ALSO, Synopsis

### Community 45 - "Naming ADR"
Cohesion: 0.33
Nodes (5): 0001 — Adopt the name Oiax, Consequences, Context, Decision, Options considered

### Community 46 - "Documentation Index"
Cohesion: 0.33
Nodes (6): Decisions, Design and internals, Guides, Oiax documentation, Process, Reference

### Community 47 - "Release Process"
Cohesion: 0.33
Nodes (5): Governance, How a release happens, Publishing to the GitHub Marketplace, Release process, Required credentials

### Community 48 - "Configuration Contracts"
Cohesion: 0.50
Nodes (5): Backflow Policy Configuration, PromotionGraph Configuration Contract, oiax validate, Environments PromotionGraph Fixture, Strict Configuration Validation

### Community 49 - "Pinned Config ADR"
Cohesion: 0.40
Nodes (5): 0003 — Read configuration from a pinned ref, Consequences, Context, Decision, Options considered

### Community 50 - "Config API ADR"
Cohesion: 0.40
Nodes (5): 0005 — Config API v1, Consequences, Context, Decision, Options considered

### Community 51 - "Action Contract Tests"
Cohesion: 0.50
Nodes (3): actionMetadata, T, TestPublishedActionRunnerContract()

### Community 52 - "Documentation Navigation"
Cohesion: 0.50
Nodes (4): Guides, Reference, Set up, Use

### Community 53 - "Copilot Guidance"
Cohesion: 0.50
Nodes (3): Copilot instructions for oiax, Invariants that must not be violated, Workflow expectations

### Community 54 - "Backflow Request Lifecycle"
Cohesion: 0.67
Nodes (3): Deterministic Backflow Return Branch, Event-Driven Concurrency Without Locks, Supersede Stale Backflow Request

## Ambiguous Edges - Review These
- `oiax` → `Design proposal in skaphos-resources under tools/oiax/`  [AMBIGUOUS]
  AGENTS.md · relation: rationale_for

## Knowledge Gaps
- **265 isolated node(s):** `github.com/skaphos/oiax`, `actionMetadata`, `versionInfo`, `Graph`, `github.com/skaphos/oiax/tools` (+260 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **8 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **What is the exact relationship between `oiax` and `Design proposal in skaphos-resources under tools/oiax/`?**
  _Edge tagged AMBIGUOUS (relation: rationale_for) - confidence is low._
- **Why does `Code map` connect `Package Ownership` to `Engine Domain Model`, `Configuration Model`, `CLI Commands`, `Documentation References`, `Contribution Safety`?**
  _High betweenness centrality (0.232) - this node is a cross-community bridge._
- **Why does ``internal/engine`` connect `Engine Domain Model` to `Backflow Reconcile Tests`, `Package Ownership`, `Plan Validation`, `CLI Commands`, `Contribution Safety`?**
  _High betweenness centrality (0.178) - this node is a cross-community bridge._
- **Why does `ChangeRequest` connect `Engine Domain Model` to `GitHub Client Errors`, `Architectural Rationale`, `Forge Test Double`?**
  _High betweenness centrality (0.137) - this node is a cross-community bridge._
- **Are the 2 inferred relationships involving `gitHarness()` (e.g. with `Run()` and `InitRepo()`) actually correct?**
  _`gitHarness()` has 2 INFERRED edges - model-reasoned connections that need verification._
- **What connects `github.com/skaphos/oiax`, `actionMetadata`, `versionInfo` to the rest of the system?**
  _265 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Backflow Reconcile Tests` be split into smaller, more focused modules?**
  _Cohesion score 0.1279735019572418 - nodes in this community are weakly interconnected._