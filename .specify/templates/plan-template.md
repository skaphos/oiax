# Implementation Plan: [FEATURE]

**Branch**: `[###-feature-name]` | **Date**: [DATE] | **Spec**: [link]

**Input**: Feature specification from `/specs/[###-feature-name]/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

[Extract from feature spec: primary requirement + technical approach from research]

## Technical Context

<!--
  ACTION REQUIRED: Replace the content in this section with the technical details
  for the project. The structure here is presented in advisory capacity to guide
  the iteration process.
-->

**Language/Version**: [e.g., Python 3.11, Swift 5.9, Rust 1.75 or NEEDS CLARIFICATION]

**Primary Dependencies**: [e.g., FastAPI, UIKit, LLVM or NEEDS CLARIFICATION]

**Storage**: [if applicable, e.g., PostgreSQL, CoreData, files or N/A]

**Testing**: [e.g., pytest, XCTest, cargo test or NEEDS CLARIFICATION]

**Target Platform**: [e.g., Linux server, iOS 15+, WASM or NEEDS CLARIFICATION]

**Project Type**: [e.g., library/cli/web-service/mobile-app/compiler/desktop-app or NEEDS CLARIFICATION]

**Performance Goals**: [domain-specific, e.g., 1000 req/s, 10k lines/sec, 60 fps or NEEDS CLARIFICATION]

**Constraints**: [domain-specific, e.g., <200ms p95, <100MB memory, offline-capable or NEEDS CLARIFICATION]

**Scale/Scope**: [domain-specific, e.g., 10k users, 1M LOC, 50 screens or NEEDS CLARIFICATION]

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Gates derived from `.specify/memory/constitution.md` v2.0.0. Answer each with
evidence, not assertion. Any "no" is either justified in Complexity Tracking
below (naming the principle) or the design changes.

| # | Gate | Status |
|---|------|--------|
| I | New operational concepts are declared config or machine-readable metadata, not convention or title-matching | [PASS/FAIL/N-A] |
| II | Configuration is read from the pinned ref; nothing this feature reads from config is executed | [PASS/FAIL/N-A] |
| III | Identical observed state yields equivalent plans; any new generated artifact is drift-gated | [PASS/FAIL/N-A] |
| IV | Config surface follows declarative API conventions; git/forge behavior is surfaced, not hidden | [PASS/FAIL/N-A] |
| V | No new hard dependency on another Skaphos tool; output stays ordinary Git and forge artifacts | [PASS/FAIL/N-A] |
| VI | Every new decision path can state its reason; failures name a next safe action; no credentials in output | [PASS/FAIL/N-A] |
| VII | `validate`/`graph`/`plan` still work without mutation credentials; mutation stays in `reconcile` | [PASS/FAIL/N-A] |
| VIII | Topology facts are modeled and validated, not inferred from names or ordering | [PASS/FAIL/N-A] |
| IX | Spec states what this does NOT do; no unverified claims; Skaphos glossary terms used correctly | [PASS/FAIL/N-A] |
| X | `internal/engine` stays pure — no API calls, no `internal/git` or `internal/forge` imports | [PASS/FAIL/N-A] |
| XI | No merge/approve/deploy/settings mutation; unmanaged requests untouched; force-push confined to `refs/heads/oiax/` and `refs/notes/oiax/`; notes updates expected-tip and append-only, no deletion/rewind; long-lived branches never force-pushed; branch names validated and never shell-interpolated | [PASS/FAIL/N-A] |
| XII | Changes to exit codes, JSON plan format, request metadata, config `apiVersion`, or `pkg/api` carry an ADR and a deprecation window | [PASS/FAIL/N-A] |

Engineering gates: regression test for every bug fix; tests run `-race -shuffle=on`;
coverage floor lands in the same change; untrusted-input parsers are fuzzed;
user-visible changes update `docs/` in the same change.

## Project Structure

### Documentation (this feature)

```text
specs/[###-feature]/
├── plan.md              # This file (/speckit-plan command output)
├── research.md          # Phase 0 output (/speckit-plan command)
├── data-model.md        # Phase 1 output (/speckit-plan command)
├── quickstart.md        # Phase 1 output (/speckit-plan command)
├── contracts/           # Phase 1 output (/speckit-plan command)
└── tasks.md             # Phase 2 output (/speckit-tasks command - NOT created by /speckit-plan)
```

### Source Code (repository root)
<!--
  ACTION REQUIRED: Replace the placeholder tree below with the concrete layout
  for this feature. Delete unused options and expand the chosen structure with
  real paths (e.g., apps/admin, packages/something). The delivered plan must
  not include Option labels.
-->

```text
# [REMOVE IF UNUSED] Option 1: Single project (DEFAULT)
src/
├── models/
├── services/
├── cli/
└── lib/

tests/
├── contract/
├── integration/
└── unit/

# [REMOVE IF UNUSED] Option 2: Web application (when "frontend" + "backend" detected)
backend/
├── src/
│   ├── models/
│   ├── services/
│   └── api/
└── tests/

frontend/
├── src/
│   ├── components/
│   ├── pages/
│   └── services/
└── tests/

# [REMOVE IF UNUSED] Option 3: Mobile + API (when "iOS/Android" detected)
api/
└── [same as backend above]

ios/ or android/
└── [platform-specific structure: feature modules, UI flows, platform tests]
```

**Structure Decision**: [Document the selected structure and reference the real
directories captured above]

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| [e.g., 4th project] | [current need] | [why 3 projects insufficient] |
| [e.g., Repository pattern] | [specific problem] | [why direct DB access insufficient] |
