# Graph Report - ska-600-ladder-diagnostics  (2026-07-16)

## Corpus Check
- 75 files · ~90,207 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 932 nodes · 2199 edges · 55 communities (51 shown, 4 thin omitted)
- Extraction: 91% EXTRACTED · 9% INFERRED · 0% AMBIGUOUS · INFERRED: 187 edges (avg confidence: 0.8)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `c7bace21`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- oiax
- plan_reconcile_test.go
- ChangeRequest
- Equivalence Ladder
- 0004-backflow-execution.md
- github_test.go
- Code map
- Agent Safety Rules (do not violate)
- fakeForge
- run
- types.go
- Provider
- Runner
- Plan
- Coordinator
- git_test.go
- oiax plan
- CLI reference
- NewLogger
- Modeling your promotion graph
- Troubleshooting
- Architecture
- Installing Oiax with an AI agent
- Operating Oiax day to day
- oiax completion bash
- oiax reconcile
- Getting started
- Deploying Oiax as a GitHub Action
- Changelog
- Backflow: returning hotfixes
- `Action`
- Recipes
- AGENTS.md
- 0006 — Merge-commit backflow strategy
- Contributing to oiax
- action_test.go
- Copilot instructions for oiax
- TestPromotionGraphJSONRoundTrip
- pkg/api/v1alpha1 (public configuration API)
- github.com/skaphos/oiax
- github.com/skaphos/oiax/tools
- RenderText
- Minimizing divergence
- oiax completion zsh
- 0001 — Adopt the name Oiax
- 0005 — Config API v1
- Oiax documentation
- 0003 — Read configuration from a pinned ref
- 0004 — Backflow execution
- Release process
- Guides

## God Nodes (most connected - your core abstractions)
1. `Plan` - 45 edges
2. `newProvider()` - 34 edges
3. `gitHarness()` - 34 edges
4. `Runner` - 32 edges
5. `testGraph()` - 31 edges
6. `newRepo()` - 30 edges
7. `Provider` - 29 edges
8. `writeJSON()` - 29 edges
9. `checkout()` - 29 edges
10. `writeCommit()` - 27 edges

## Surprising Connections (you probably didn't know these)
- `BuildPlan()` --conceptually_related_to--> `Reconciliation Loop`  [INFERRED]
  internal/engine/plan.go → docs/architecture.md
- `ChangeRequest` --conceptually_related_to--> `Managed Change Requests`  [INFERRED]
  internal/engine/types.go → docs/architecture.md
- `EdgeState` --shares_data_with--> `Equivalence Ladder`  [INFERRED]
  internal/engine/types.go → docs/architecture.md
- `Forge` --implements--> ``internal/forge/github``  [INFERRED]
  internal/forge/forge.go → docs/code-map.md
- `oiax` --semantically_similar_to--> `argoproj-labs/gitops-promoter (prior art)`  [INFERRED] [semantically similar]
  README.md → docs/architecture.md

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Ordered equivalence ladder: reachability, patch identity, head-tree equality, promotion baseline (first conclusive rung wins)** — docs_architecture_equivalence_ladder, docs_architecture_reachability, docs_architecture_patch_identity, docs_architecture_head_tree_equality, docs_architecture_promotion_baseline [EXTRACTED 1.00]
- **Entrypoint to Engine to Git layer / Forge provider layering (depguard-enforced, engine never reaches down)** — docs_code_map_internal_cli, docs_code_map_internal_engine, docs_code_map_internal_git, docs_code_map_internal_forge, docs_code_map_layering_rule [EXTRACTED 1.00]
- **Release pipeline: conventional commits classified by release-please into CHANGELOG and tag, GoReleaser publishes binaries consumed by the composite Action** — contributing_conventional_commits, release_release_please_workflow, changelog_changelog, release_goreleaser, docs_code_map_action_yml [EXTRACTED 1.00]

## Communities (55 total, 4 thin omitted)

### Community 0 - "oiax"
Cohesion: 0.12
Nodes (18): Design proposal in skaphos-resources under tools/oiax/, Conventional Commits, Signed commits + DCO sign-off, Branch Promotion (capability), argoproj-labs/gitops-promoter (prior art), Kargo (prior art), Promotion Graph (DAG model), release-please (prior art / inspiration) (+10 more)

### Community 1 - "plan_reconcile_test.go"
Cohesion: 0.07
Nodes (39): exitCodeError, options, versionInfo, main(), Command, newGenCommand(), writeCommandReference(), Command (+31 more)

### Community 2 - "ChangeRequest"
Cohesion: 0.14
Nodes (29): FromConfig(), BuildPlan(), Graph, Graph, planDownstream(), planPromotion(), summarizeEdge(), edge() (+21 more)

### Community 3 - "Equivalence Ladder"
Cohesion: 0.24
Nodes (11): ADR 0001: Adopt the name Oiax, Rationale: Tiller collided with Helm v2's Tiller in the target ecosystem; Oiax is the literal Greek for tiller and keeps the hand-on-the-helm intent, ADR 0002: Detect divergence by content, not ancestry, Rationale: squash/rebase merges rewrite SHAs; ancestry-only detection leaves edges permanently diverged and PR creation fails with HTTP 422; a private state database would violate the no-control-plane posture, Equivalence Ladder, Rung 3: Head-Tree Equality, Managed Change Requests, Rung 2: Stable Patch Identity (+3 more)

### Community 4 - "0004-backflow-execution.md"
Cohesion: 0.15
Nodes (11): 0002 — Detect divergence by content, not ancestry, Consequences, Context, Decision, Options considered, 0007 — Keep the git layer a shell-out to the git binary, Consequences, Context (+3 more)

### Community 5 - "github_test.go"
Cohesion: 0.10
Nodes (68): marker, prSpec, assertAuth(), assertNoToken(), decode(), T, newProvider(), runGit() (+60 more)

### Community 6 - "Code map"
Cohesion: 0.25
Nodes (9): `cmd/oiax`, Code map, `internal/forge`, `internal/forge/github`, `internal/reconcile`, `internal/version`, Not Go, `pkg/api/v1` (+1 more)

### Community 7 - "Agent Safety Rules (do not violate)"
Cohesion: 0.24
Nodes (11): Agent Safety Rules (do not violate), Design invariants, ADR 0003: Read configuration from a pinned ref, Rationale: config is itself promoted and differs per branch; reading the triggering ref is nondeterministic and lets untrusted PR config run with write credentials, Engine Purity Rules, Reconciliation Loop, Security posture, `internal/git` (+3 more)

### Community 8 - "fakeForge"
Cohesion: 0.17
Nodes (31): T, runCode(), setupRepo(), TestPlanAssertsGitFloorBeforeConfigRead(), TestPlanExitCode(), TestPlanForgeErrorExitsOne(), TestPlanInSyncExitsZero(), TestPlanJSONShape() (+23 more)

### Community 9 - "run"
Cohesion: 0.35
Nodes (17): T, run(), TestDeprecatedAPIVersionWarns(), TestGenDocs(), TestGraphCommand(), TestInvalidOutputFlagRejected(), TestPlanAndReconcileAreHonestAboutScope(), TestRootRejectsJSONOutputWithoutSubcommand() (+9 more)

### Community 10 - "types.go"
Cohesion: 0.09
Nodes (34): `internal/config`, BackflowPolicy, Branch, Expectations, Promotion, config.DefaultPath, IsDeprecatedAPIVersion(), Load() (+26 more)

### Community 11 - "Provider"
Cohesion: 0.06
Nodes (41): fakeForge, Client, Duration, ChangeRequest, RequestType, BranchPush, CreateRequest, MergeMethods (+33 more)

### Community 12 - "Runner"
Cohesion: 0.14
Nodes (11): capWriter, CherryPickConflict, Commit, Runner, checkMinVersion(), Buffer, Context, parseGitVersion() (+3 more)

### Community 14 - "Plan"
Cohesion: 0.22
Nodes (45): Plan, BackflowBranchName(), assertExclusionReason(), checkout(), commitOn(), diamondGraph(), findLogRecord(), gitExec() (+37 more)

### Community 15 - "Coordinator"
Cohesion: 0.10
Nodes (36): `internal/engine`, Action, ActionType, BackflowExclusion, BackflowExclusionReason, BranchState, Commit, EdgeObservation (+28 more)

### Community 16 - "git_test.go"
Cohesion: 0.25
Nodes (31): T, newRepo(), oidLike(), requireGit(), runGit(), TestCherryPickConflict(), TestCherryPickDropsRedundant(), TestCherryPickHappyPath() (+23 more)

### Community 17 - "oiax plan"
Cohesion: 0.11
Nodes (22): `internal/cli`, oiax graph, oiax plan, oiax (root command), oiax validate, oiax version, Options, Options (+14 more)

### Community 18 - "CLI reference"
Cohesion: 0.10
Nodes (20): CLI reference, oiax, oiax completion, oiax completion fish, oiax completion powershell, Options, Options, Options (+12 more)

### Community 19 - "NewLogger"
Cohesion: 0.16
Nodes (13): Attr, Handler, escapeAnnotation(), Context, Logger, Writer, NewLogger(), T (+5 more)

### Community 20 - "Modeling your promotion graph"
Cohesion: 0.12
Nodes (17): A minimal graph, Branches, Drift policy, Fan-out, Linear pipeline, Merge-method expectations, Migrating the apiVersion, Modeling your promotion graph (+9 more)

### Community 21 - "Troubleshooting"
Cohesion: 0.12
Nodes (17): A backflow halted on a conflict, A configured branch does not exist, `apiVersion "…v1alpha1" is deprecated`, Cannot determine owner/repo, `cannot resolve the repository default branch`, Configuration won't load, `git 2.45 or newer is required`, Managed pull request checks wait for approval (+9 more)

### Community 22 - "Architecture"
Cohesion: 0.14
Nodes (16): Skaphos Glossary Discipline (branch promotion vs Promotion vs backflow), Architecture, Backflow, Deterministic Backflow Branch Naming, Drift Policy (forbidden/expected), Execution model, Failure handling and observability, Layers (+8 more)

### Community 24 - "Installing Oiax with an AI agent"
Cohesion: 0.14
Nodes (14): 1. Check preconditions, 2. Discover the repository shape, 3. Infer the promotion graph, 4. Confirm the inference with the user (gate — do not skip), 5. Write and locally verify `.oiax.yaml`, 6. Handle adoption (existing promotion PRs), 7. Write the workflow, 8. Set up the token (human step) (+6 more)

### Community 25 - "Operating Oiax day to day"
Cohesion: 0.14
Nodes (14): Approvals can go stale, Branch protection and required checks, Exit codes and CI gating, Next steps, Observability, Obsolete requests are closed, not deleted, Operating Oiax day to day, Reading a plan (+6 more)

### Community 26 - "oiax completion bash"
Cohesion: 0.29
Nodes (7): Linux:, macOS:, oiax completion bash, Options, Options inherited from parent commands, SEE ALSO, Synopsis

### Community 27 - "oiax reconcile"
Cohesion: 0.14
Nodes (14): oiax reconcile, Options, Options inherited from parent commands, SEE ALSO, Synopsis, Configuration reference, Environment variables, Exit codes (+6 more)

### Community 28 - "Getting started"
Cohesion: 0.17
Nodes (12): 1. Install, 2. Write your first graph, 3. Inspect it locally, 4. See what Oiax would do, 5. Apply, 6. Deploy it, Adopting Oiax on an existing repository, From source (works today) (+4 more)

### Community 29 - "Deploying Oiax as a GitHub Action"
Cohesion: 0.17
Nodes (12): Choosing a mode, Concurrency, Deploying Oiax as a GitHub Action, `fetch-depth: 0` is not optional, Inputs, Large repositories: partial clone, Next steps, Permissions (+4 more)

### Community 30 - "Changelog"
Cohesion: 0.18
Nodes (10): 1.0.0 (2026-07-12), [1.0.1](https://github.com/skaphos/oiax/compare/v1.0.0...v1.0.1) (2026-07-13), [1.0.2](https://github.com/skaphos/oiax/compare/v1.0.1...v1.0.2) (2026-07-13), ⚠ BREAKING CHANGES, Bug Fixes, Bug Fixes, Changelog, Changelog (+2 more)

### Community 31 - "Backflow: returning hotfixes"
Cohesion: 0.18
Nodes (11): "Already returned" — the identity check, Backflow: returning hotfixes, Configuration, Next steps, One active request, superseded on a new hotfix, Requirements, The backflow branch name, The `Oiax-Backflow: skip` escape hatch (+3 more)

### Community 32 - "`Action`"
Cohesion: 0.17
Nodes (12): `Action`, `branch`, Compatibility, `Edge`, `equivalence`, `Exclusion`, `Plan`, Plan JSON format (`planFormatVersion: 1`) (+4 more)

### Community 33 - "Recipes"
Cohesion: 0.22
Nodes (9): Consume the plan as JSON, Gate CI on drift (read-only policy check), Next steps, Preview a graph change before it lands, Recipes, Roll back a promotion that landed, Roll out plan-first, Run multiple pipelines in one repository (monorepo) (+1 more)

### Community 34 - "AGENTS.md"
Cohesion: 0.29
Nodes (5): Building and testing, Conventions, Release constraints, Safety rules (do not violate), What this is

### Community 35 - "0006 — Merge-commit backflow strategy"
Cohesion: 0.29
Nodes (6): 0006 — Merge-commit backflow strategy, Consequences, Context, Decision, Links, Options considered

### Community 36 - "Contributing to oiax"
Cohesion: 0.33
Nodes (6): Commits, Contributing to oiax, Documentation, Generated artifacts, Local validation, Workflow

### Community 37 - "action_test.go"
Cohesion: 0.50
Nodes (3): actionMetadata, T, TestPublishedActionRunnerContract()

### Community 38 - "Copilot instructions for oiax"
Cohesion: 0.50
Nodes (3): Copilot instructions for oiax, Invariants that must not be violated, Workflow expectations

### Community 44 - "RenderText"
Cohesion: 0.20
Nodes (19): actionVerb(), edgeSummaryText(), exclusionCounts(), Writer, RenderJSON(), RenderMarkdown(), RenderText(), T (+11 more)

### Community 46 - "Minimizing divergence"
Cohesion: 0.29
Nodes (7): Gate on drift so divergence is loud, Isolate environment-specific configuration, Keep hotfixes short-lived, Minimizing divergence, Next steps, Prefer merge commits on promotion targets, When divergence still happens

### Community 47 - "oiax completion zsh"
Cohesion: 0.29
Nodes (7): Linux:, macOS:, oiax completion zsh, Options, Options inherited from parent commands, SEE ALSO, Synopsis

### Community 49 - "0001 — Adopt the name Oiax"
Cohesion: 0.33
Nodes (5): 0001 — Adopt the name Oiax, Consequences, Context, Decision, Options considered

### Community 50 - "0005 — Config API v1"
Cohesion: 0.33
Nodes (5): 0005 — Config API v1, Consequences, Context, Decision, Options considered

### Community 51 - "Oiax documentation"
Cohesion: 0.33
Nodes (6): Decisions, Design and internals, Guides, Oiax documentation, Process, Reference

### Community 52 - "0003 — Read configuration from a pinned ref"
Cohesion: 0.40
Nodes (5): 0003 — Read configuration from a pinned ref, Consequences, Context, Decision, Options considered

### Community 53 - "0004 — Backflow execution"
Cohesion: 0.40
Nodes (5): 0004 — Backflow execution, Consequences, Context, Decision, Options considered

### Community 54 - "Release process"
Cohesion: 0.40
Nodes (5): Governance, How a release happens, Publishing to the GitHub Marketplace, Release process, Required credentials

### Community 55 - "Guides"
Cohesion: 0.50
Nodes (4): Guides, Reference, Set up, Use

## Ambiguous Edges - Review These
- `oiax` → `Design proposal in skaphos-resources under tools/oiax/`  [AMBIGUOUS]
  AGENTS.md · relation: rationale_for

## Knowledge Gaps
- **250 isolated node(s):** `github.com/skaphos/oiax`, `actionMetadata`, `versionInfo`, `Graph`, `github.com/skaphos/oiax/tools` (+245 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **4 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **What is the exact relationship between `oiax` and `Design proposal in skaphos-resources under tools/oiax/`?**
  _Edge tagged AMBIGUOUS (relation: rationale_for) - confidence is low._
- **Why does `Code map` connect `Code map` to `Agent Safety Rules (do not violate)`, `types.go`, `Coordinator`, `README.md`, `oiax plan`?**
  _High betweenness centrality (0.298) - this node is a cross-community bridge._
- **Why does ``internal/engine`` connect `Coordinator` to `ChangeRequest`, `Code map`, `Agent Safety Rules (do not violate)`, `Provider`, `Plan`, `oiax plan`?**
  _High betweenness centrality (0.220) - this node is a cross-community bridge._
- **Why does `ChangeRequest` connect `Provider` to `Equivalence Ladder`, `Coordinator`?**
  _High betweenness centrality (0.157) - this node is a cross-community bridge._
- **Are the 2 inferred relationships involving `gitHarness()` (e.g. with `InitRepo()` and `Run()`) actually correct?**
  _`gitHarness()` has 2 INFERRED edges - model-reasoned connections that need verification._
- **What connects `github.com/skaphos/oiax`, `actionMetadata`, `versionInfo` to the rest of the system?**
  _250 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `oiax` be split into smaller, more focused modules?**
  _Cohesion score 0.12418300653594772 - nodes in this community are weakly interconnected._