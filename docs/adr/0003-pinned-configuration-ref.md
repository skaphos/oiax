# 0003 — Read configuration from a pinned ref

- Status: accepted
- Date: 2026-07-08

## Context

`.oiax.yaml` lives in the repository, which means it is itself promoted
through the graph and will differ between branches. Reading
configuration from whatever ref triggered the run would make Oiax's
behavior depend on which branch moved last. Worse, Oiax runs with
repository write credentials: configuration proposed in an untrusted
pull request must never execute with those credentials.

## Options considered

- **Triggering ref.** Nondeterministic behavior across events; an
  untrusted-config execution path when combined with
  `pull_request_target`-style triggers.
- **Merged view of all branches.** No coherent semantics when branches
  disagree.
- **Configuration outside the repository.** Breaks the
  everything-is-in-Git posture and adds an external dependency.
- **One pinned ref per invocation.** Default: the repository default
  branch; overridable with `--config-ref` (CLI) / `config-ref` (Action).

## Decision

Read configuration from exactly one pinned ref per invocation.
Configuration on all other refs is ignored.

## Consequences

- Behavior is deterministic regardless of which event fired.
- The rule is a structural security boundary: untrusted pull-request
  configuration is never executed with privileged credentials, and
  `pull_request_target` must not be a default Action trigger.
- Configuration changes take effect when they land on the pinned ref
  (normally: when they finish promoting to the default branch), which is
  the intuitively correct moment for changes to promotion policy.
