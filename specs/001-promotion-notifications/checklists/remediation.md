# Analysis remediation record

Date: 2026-09-04. Scope: specification/design/task corrections only, following
`speckit-analyze`. No production implementation, endpoint sends, remote ref writes,
constitution changes or ADR acceptance are performed by these corrections.

| Finding | Concrete remediation | Implementation verification | Disposition |
|---|---|---|---|
| C1 — unjustified XI PASS | [Plan](../plan.md#constitution-check), [ADR 0014](../../../docs/adr/0014-notification-delivery-ledger.md) and store contract now make exact notes-operation conformity a prerequisite, not an ADR-granted exception | T001 records maintainer disposition; T012 and dependent writes/delivery cannot proceed until conformity is established or design is revised | False PASS removed; external authority gate still pending |
| A1 — stale config can replace policy | [Revision ordering](../data-model.md#configuration-revision-ordering) persists accepted OID/digest, allows only proven descendants and recomputes evidence after CAS conflicts; late receipts cannot restore old policy | T005, T009, T013, T030–T034, T052, T061, T067 test out-of-order workers, divergent/missing ancestry and descendant content reverts | Remediated in design; implementation tests pending |
| I1 — global disable cannot record an epoch | [Configuration](../contracts/configuration.md) and FR-019 explicitly distinguish zero-I/O global disable/resumption from durably recorded per-destination retirement | T015, T049, T052 and quickstart steps 10–11 test both sequences and new-name fresh cutoffs | Remediated in design; implementation tests pending |
| I2 — custom templates can omit required facts | [Presentation](../contracts/presentation.md#delivery-encoding) mandates the complete FR-008 set independently, including explicit request ID and observed-at time on every transport | T018, T035, T050, T058–T060 test constant/empty templates and retries | Remediated in design; implementation tests pending |
| I3 — adapter lacks saved message input | [Delivery contract](../contracts/delivery.md) accepts `DeliveryPayloadV1` joining immutable event and saved per-destination message; adapters cannot retrieve state or re-render | T008, T010, T018, T031, T058–T060 test distinct destination messages and retry reuse | Remediated in design; implementation tests pending |
| I4 — typed fixtures precede their interfaces | T002 now scaffolds fixture documentation only; T010 creates typed fixtures after model/interface definitions, with early tests self-contained | T002/T008/T010 dependency order and foundation checkpoint | Remediated in task ordering |
| A2 — latency workload differs between artifacts | SC-001, plan and [quickstart](../quickstart.md#recipient-visible-validation-and-rollback) share normal-load limits, at least 100 predeclared deliveries per transport, actual visibility timing and separate recovery tests | T076–T077 record denominators, missing samples and deadline results; unavailable resources remain unchecked gates | Remediated in acceptance design; live evidence pending |

## Remaining gate: C1 / T001

The current constitution is unchanged and authoritative. Before implementing the
notes writer, the maintainer must record the exact ref and write operation and
why they satisfy the existing namespace restriction. If they do not, revise the
mechanism or address governance in a separate explicit change. Ordinary ADR
approval, platform permissions, successful CAS tests, and this remediation request
do not themselves amend the restriction. Do not mark T001 complete until its
evidence exists. The plan must not be reported as unconditionally implementation-ready.

## Document verification

The task list retains 80 unique sequential IDs and its four story phases; all
26 functional requirements and eight acceptance criteria remain mapped. Source
and documentation references, checklist formatting, Markdown whitespace and
REUSE are checked during remediation. These checks do not stand in for the future
Go, concurrency, transport or live-provider tests.
