# 0015 — Extend Oiax ownership to the standard Git notes namespace

- Status: accepted (maintainer decision; repository publication remains PR-based)
- Date: 2026-09-04
- Decider: Shawn Stratton, repository maintainer
- Scope: namespace authority only; notification implementation is not yet shipped

## Context

Notification delivery needs shared durable receipts across disposable CI workers.
The proposed ledger in ADR 0014 uses a standard Git notes ref, but the existing
blanket restriction allowed force-push only inside the branch-oriented `oiax/`
namespace. That wording left the notes operation blocked under C1/T001.

Git standardizes notes under `refs/notes/`; `oiax/` identifies our subnamespace
within it. After that distinction and the append-only/expected-tip safeguards
were explained, the maintainer explicitly authorized the extension in this
session: “I'm fine with extending the namespace boundary to include notes/oiax”.
This is explicit governance authorization, not an inference from the Git
standard, repository credentials, or general approval to implement the feature.

## Options considered

- Keep the existing boundary and redesign storage: avoids additional ref
  authority, but discards the proposed standard-notes mechanism and requires a
  different durable concurrency design.
- Move notes beneath a custom top-level `refs/oiax/` hierarchy: keeps the literal
  prefix but departs from the standard notes namespace and its normal naming.
- Permit `refs/notes/oiax/` with explicit expected-tip and append-only constraints:
  preserves standard notes organization and limits ownership to Oiax's subtree.

## Decision

Accept the third option. The owned ref families are `refs/heads/oiax/` and
`refs/notes/oiax/`. The notes capability may use an explicit expected-tip lease
to create an absent ref or append a snapshot commit whose sole parent is the
expected current tip. It must reject a changed tip, rewinds, history deletion,
other tools' notes, and branch/tag targets. A force-capable flag is never
authorization for a non-fast-forward notes update.

The notification writer remains narrower:
`refs/notes/oiax/notifications/v1/<64-hex-graph-key>`. No generic arbitrary-ref
writer is authorized. Long-lived branches remain protected against force-push
under all circumstances, and repository settings and unmanaged requests remain
outside Oiax's mutation scope.

Constitution XI is explicitly amended; its version becomes 2.0.0 because the
permitted mutation boundary is redefined, not merely reworded. This is a
governance-document version, not an Oiax software release or configuration API
version. ADR 0004's blanket ref-namespace statement is superseded only to add
these notes operations; its backflow algorithms and branch permissions are intact.
ADRs 0013/0014 remain proposed for their broader contracts.

## Consequences

- Notification storage can proceed past its namespace-authority gate without
  introducing a private database or a state branch.
- Implementations must enforce two distinct ref families; substring matching
  `oiax` anywhere in a ref is insufficient. Each feature still uses a narrower
  validated target, with concurrency and adversarial-ref tests.
- Notes access adds permissions and growing Git history; append-only receipts
  cannot be deleted automatically to recover capacity.
- This decision does not establish forge notes support, prove concurrency
  correctness, approve live endpoint sends, or mark the feature ready to release.

## Links

- [Git notes documentation](https://git-scm.com/docs/git-notes).
- [Constitution XI](../../.specify/memory/constitution.md#xi-bounded-blast-radius-oiax-specific).
- Partially supersedes [0004](0004-backflow-execution.md), namespace scope only.
- Supports proposed [0014](0014-notification-delivery-ledger.md) and
  [0013](0013-notification-configuration-contract.md).
- [T001 decision evidence](../../specs/001-promotion-notifications/checklists/implementation-validation.md).
