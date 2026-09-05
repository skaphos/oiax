# Notification fixture responsibilities

Fixtures support deterministic notification tests, not production behavior.
Typed fixtures follow the shared model and capability definitions (T008/T010).

- Clocks expose explicitly captured UTC instants and advance only at a test's
  request. Lease and retry tests do not sleep or depend on wall-clock timing.
- Lifecycle doubles supply validated managed-request facts, complete/incomplete
  pages, historical commit snapshots and typed missing/denied failures. They
  record calls so no-policy and read-only tests can prove effect bypasses.
- Store doubles apply transitions to fresh snapshots, simulate expected-tip
  conflicts and denial/corruption, and preserve terminal receipts. Tests must
  recompute revision-order evidence after conflicts, not replay replacements.
- Attempt recorders capture immutable payload copies and return safe outcomes.
  Endpoint credential values never enter recorded fixtures or durable state.
- Bare Git integration fixtures independently verify the real notes writer's
  exact namespace, absent/create lease, sole-parent ancestry and conflict rules.

Production pure code must not import these fixtures or effect packages. Use
per-test resources and explicit synchronization to remain race/shuffle safe.
