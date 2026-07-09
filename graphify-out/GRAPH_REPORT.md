# Graph Report - .  (2026-07-08)

## Corpus Check
- Corpus is ~14,179 words - fits in a single context window. You may not need a graph.

## Summary
- 176 nodes · 265 edges · 14 communities detected
- Extraction: 82% EXTRACTED · 18% INFERRED · 0% AMBIGUOUS · INFERRED: 48 edges (avg confidence: 0.81)
- Token cost: 16,500 input · 9,000 output

## Community Hubs (Navigation)
- [[_COMMUNITY_Branch Promotion & Backflow Model|Branch Promotion & Backflow Model]]
- [[_COMMUNITY_CLI Commands (Cobra)|CLI Commands (Cobra)]]
- [[_COMMUNITY_Engine Planning & Validation|Engine Planning & Validation]]
- [[_COMMUNITY_ADRs & Equivalence Ladder|ADRs & Equivalence Ladder]]
- [[_COMMUNITY_Engine Graph Model|Engine Graph Model]]
- [[_COMMUNITY_Config API Types (v1alpha1)|Config API Types (v1alpha1)]]
- [[_COMMUNITY_Forge Abstraction|Forge Abstraction]]
- [[_COMMUNITY_Security & Layering Invariants|Security & Layering Invariants]]
- [[_COMMUNITY_Planning Domain Types|Planning Domain Types]]
- [[_COMMUNITY_CLI Test Suite|CLI Test Suite]]
- [[_COMMUNITY_Config Loading|Config Loading]]
- [[_COMMUNITY_GitHub Provider Stub|GitHub Provider Stub]]
- [[_COMMUNITY_Git Runner|Git Runner]]
- [[_COMMUNITY_Version Metadata|Version Metadata]]

## God Nodes (most connected - your core abstractions)
1. `Oiax (project)` - 14 edges
2. `BuildPlan()` - 10 edges
3. `FromConfig()` - 10 edges
4. `Equivalence Ladder` - 10 edges
5. `run()` - 9 edges
6. `NewRootCommand()` - 9 edges
7. `internal/engine (provider-neutral core)` - 9 edges
8. `loadGraph()` - 8 edges
9. `validGraph()` - 7 edges
10. `Runner` - 7 edges

## Surprising Connections (you probably didn't know these)
- `Reconciliation Loop` --conceptually_related_to--> `BuildPlan()`  [INFERRED]
  docs/architecture.md → internal/engine/plan.go
- `Equivalence Ladder` --shares_data_with--> `EdgeState`  [INFERRED]
  docs/architecture.md → internal/engine/types.go
- `internal/git (git layer, shells out to git)` --references--> `Runner`  [EXTRACTED]
  docs/code-map.md → internal/git/git.go
- `oiax validate` --conceptually_related_to--> `Graph.Validate`  [INFERRED]
  docs/reference/cli.md → internal/engine/validate.go
- `Managed Change Requests` --conceptually_related_to--> `ChangeRequest`  [INFERRED]
  docs/architecture.md → internal/engine/types.go

## Hyperedges (group relationships)
- **Ordered equivalence ladder: reachability, patch identity, head-tree equality, promotion baseline (first conclusive rung wins)** — architecture_equivalence_ladder, architecture_reachability, architecture_patch_identity, architecture_head_tree_equality, architecture_promotion_baseline [EXTRACTED 1.00]
- **Entrypoint to Engine to Git layer / Forge provider layering (depguard-enforced, engine never reaches down)** — code_map_internal_cli, code_map_internal_engine, code_map_internal_git, code_map_internal_forge, code_map_layering_rule [EXTRACTED 1.00]
- **Release pipeline: conventional commits classified by release-please into CHANGELOG and tag, GoReleaser publishes binaries consumed by the composite Action** — contributing_conventional_commits, release_release_please_workflow, changelog_changelog, release_goreleaser, code_map_action_yml [EXTRACTED 1.00]

## Communities

### Community 0 - "Branch Promotion & Backflow Model"
Cohesion: 0.12
Nodes (26): Skaphos Glossary Discipline (branch promotion vs Promotion vs backflow), Design proposal in skaphos-resources under tools/oiax/, Backflow (hotfix return), Branch Promotion (capability), Deterministic Backflow Branch Naming, Drift Policy (forbidden/expected), Execution Model (GitHub Action, no daemon), argoproj-labs/gitops-promoter (prior art) (+18 more)

### Community 1 - "CLI Commands (Cobra)"
Cohesion: 0.13
Nodes (14): options, newGenCommand(), writeCommandReference(), newGraphCommand(), printGraph(), main(), newPlanCommand(), newReconcileCommand() (+6 more)

### Community 2 - "Engine Planning & Validation"
Cohesion: 0.19
Nodes (18): internal/engine (provider-neutral core), FromConfig(), BuildPlan(), planDownstream(), planPromotion(), edge(), TestBuildPlan(), TestBuildPlanIsDeterministic() (+10 more)

### Community 3 - "ADRs & Equivalence Ladder"
Cohesion: 0.14
Nodes (17): ADR 0001: Adopt the name Oiax, Rationale: Tiller collided with Helm v2's Tiller in the target ecosystem; Oiax is the literal Greek for tiller and keeps the hand-on-the-helm intent, ADR 0002: Detect divergence by content, not ancestry, Rationale: squash/rebase merges rewrite SHAs; ancestry-only detection leaves edges permanently diverged and PR creation fails with HTTP 422; a private state database would violate the no-control-plane posture, Equivalence Ladder, Rung 3: Head-Tree Equality, Rung 2: Stable Patch Identity, Rung 4: Promotion Baseline (sourceHead) (+9 more)

### Community 4 - "Engine Graph Model"
Cohesion: 0.19
Nodes (8): BackflowPolicy, Branch, Expectations, Graph, Promotion, findCycle(), formatCycle(), sortedBranchNames()

### Community 5 - "Config API Types (v1alpha1)"
Cohesion: 0.17
Nodes (11): Backflow, BackflowStrategy, Branch, DriftPolicy, Expectations, MergeMethod, Metadata, Promotion (+3 more)

### Community 6 - "Forge Abstraction"
Cohesion: 0.17
Nodes (11): Managed Change Requests, internal/forge (provider-neutral forge abstraction), internal/forge/github (GitHub provider stub), BranchPush, CreateRequest, Forge, Reason, RequestFilter (+3 more)

### Community 7 - "Security & Layering Invariants"
Cohesion: 0.24
Nodes (11): ADR 0003: Read configuration from a pinned ref, Rationale: config is itself promoted and differs per branch; reading the triggering ref is nondeterministic and lets untrusted PR config run with write credentials, Agent Safety Rules (do not violate), Engine Purity Rules, Reconciliation Loop, Security Posture, internal/git (git layer, shells out to git), Layering Rule: entrypoint to engine to git/forge, depguard-enforced (+3 more)

### Community 8 - "Planning Domain Types"
Cohesion: 0.2
Nodes (9): Action, ActionType, BranchState, ChangeRequest, Commit, EdgeState, Equivalence, Plan (+1 more)

### Community 9 - "CLI Test Suite"
Cohesion: 0.5
Nodes (8): run(), TestGenDocs(), TestGraphCommand(), TestPlanAndReconcileAreHonestAboutScope(), TestValidateCommand(), TestValidateCommandReportsEveryViolation(), TestVersionCommand(), writeConfig()

### Community 10 - "Config Loading"
Cohesion: 0.36
Nodes (6): internal/config (loading and syntactic validation), config.DefaultPath, Load(), Parse(), TestLoadExample(), TestParseRejections()

### Community 11 - "GitHub Provider Stub"
Cohesion: 0.29
Nodes (1): Provider

### Community 12 - "Git Runner"
Cohesion: 0.57
Nodes (1): Runner

### Community 13 - "Version Metadata"
Cohesion: 1.0
Nodes (0): 

## Ambiguous Edges - Review These
- `Oiax (project)` → `Design proposal in skaphos-resources under tools/oiax/`  [AMBIGUOUS]
  AGENTS.md · relation: rationale_for

## Knowledge Gaps
- **45 isolated node(s):** `options`, `RequestID`, `RequestFilter`, `CreateRequest`, `UpdateRequest` (+40 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **Thin community `Version Metadata`** (1 nodes): `version.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **What is the exact relationship between `Oiax (project)` and `Design proposal in skaphos-resources under tools/oiax/`?**
  _Edge tagged AMBIGUOUS (relation: rationale_for) - confidence is low._
- **Why does `FromConfig()` connect `Engine Planning & Validation` to `Branch Promotion & Backflow Model`, `CLI Commands (Cobra)`, `Engine Graph Model`?**
  _High betweenness centrality (0.291) - this node is a cross-community bridge._
- **Why does `loadGraph()` connect `CLI Commands (Cobra)` to `Config Loading`, `Engine Planning & Validation`, `Engine Graph Model`?**
  _High betweenness centrality (0.246) - this node is a cross-community bridge._
- **Why does `Oiax (project)` connect `Branch Promotion & Backflow Model` to `ADRs & Equivalence Ladder`?**
  _High betweenness centrality (0.198) - this node is a cross-community bridge._
- **Are the 4 inferred relationships involving `BuildPlan()` (e.g. with `TestBuildPlan()` and `TestBuildPlanRespectsExpectedDrift()`) actually correct?**
  _`BuildPlan()` has 4 INFERRED edges - model-reasoned connections that need verification._
- **Are the 8 inferred relationships involving `FromConfig()` (e.g. with `loadGraph()` and `TestValidateAcceptsCanonicalGraph()`) actually correct?**
  _`FromConfig()` has 8 INFERRED edges - model-reasoned connections that need verification._
- **Are the 2 inferred relationships involving `run()` (e.g. with `NewRootCommand()` and `Execute()`) actually correct?**
  _`run()` has 2 INFERRED edges - model-reasoned connections that need verification._