# 0016 — Support production automation on Linux, retain local CLI portability

- Status: accepted (maintainer decision; publication remains PR-based)
- Date: 2026-09-05
- Decider: Shawn Stratton, repository maintainer
- Scope: operating-system support and CI tiers; independent of notification contracts

## Context

Oiax is primarily a CI-invoked Git branch promotion reconciler. Both published
wrappers already require Linux amd64/arm64, while standalone release binaries
are published for Linux, macOS and Windows. Previously the same full coverage
suite ran on all three OSes, implying a broader integration-support commitment
than the wrappers' production model.

Notification work in PR #77 exposed the cost of that commitment. In
[CI run 33985957980](https://github.com/skaphos/oiax/actions/runs/33985957980),
coverage took 57 seconds on Linux and 3m09s on macOS; Windows was still in the
coverage step after 16 minutes, before its second test gate began. An earlier
run exhausted ten-minute package deadlines in both CLI and coordinator tests.
These are observations from particular hosted runs, not a benchmark or proof
of a Windows runtime defect. The suites include repeated binary builds, Git
subprocesses and temporary repositories; the exact Windows bottleneck has not
been profiled. Increasing timeouts permits more waiting but does not reduce it.

The maintainer chose Linux production support with lightweight non-Linux
portability checks and requested that this decision land independently, with
#77 rebased afterward. ADR numbers 0013–0015 are reserved by that parallel work;
this ADR neither accepts nor depends on those notification proposals.

## Alternatives

1. Keep full three-OS integration parity and increase budgets or optimize all
   fixtures. This preserves the broadest guarantee, but commits continuing CI
   and maintenance resources to automation OSes neither wrapper supports.
2. Run non-Linux integration only on schedules or releases. This reduces PR
   latency but postpones failures and retains the same broader support burden.
3. Remove macOS/Windows builds entirely. This yields the narrowest matrix but
   needlessly removes useful local inspection/development tools.
4. Make Linux the supported automation platform and retain bounded native
   build/unit checks plus release artifacts for best-effort local use elsewhere.

## Decision

Choose option 4 as a deliberate product support boundary, not a declaration that
the existing Windows performance problem has been fixed.

- Linux amd64/arm64 is supported for production automation on both forges,
  through wrappers or direct CLI execution.
- Linux CI keeps full coverage/integration and adds full race/shuffle testing.
  The native full-suite runner is currently amd64; arm64 remains cross-built.
- macOS/Windows keep their amd64/arm64 release archives, checksums and native
  runner builds. Their CI runs an explicit, bounded unit-package selection,
  excluding Git-backed CLI/git/coordinator and provider subprocess fixtures.
  Native tests cover the runner architecture, not every published architecture.
- Portability jobs remain required contributors to the aggregate build gate;
  they are not made advisory or silently allowed to fail. Existing check names
  remain stable. No branch-protection or repository-setting changes are needed.
- Keep all tests available locally. Do not add global OS skips or runtime OS
  refusal. New portable unit packages must be considered for the explicit list.
- Use a 25-minute Linux job budget with ten-minute package deadlines, and
  ten-minute portability jobs with three-minute unit-package deadlines.

## Compatibility and rollout

Treat the reduction in supported automation OSes as a breaking support-policy
change and ship it in a major release through ordinary release automation.
Preserve the breaking conventional-commit marker and migration footer on squash.
No configuration API, JSON plan format, exit code, managed-request metadata,
Git mutation boundary, or forge support is changed by this decision.

Existing native macOS/Windows automation consumers must move their job to Linux
before upgrading. Existing Linux wrapper users need no runner migration.
Native binaries remain available for best-effort local use, without an
integration-support guarantee; artifact availability and support are distinct.
See [platform support and migration](../reference/platform-support.md).

Land this prerequisite independently of #77. After it merges, rebase #77 onto
updated main, keep its notification integration/race/coverage gates Linux-only,
and add its eligible unit packages to the portability selection. Do not carry
its temporary Windows timeout enlargement back into this support policy.
No merge, tag push or release is authorized by the ADR itself.

## Consequences

The tested production contract now matches the CI automation focus. Non-Linux
build and pure-logic regressions remain visible, while platform-specific Git
integration regressions can escape those checks. That reduced assurance is
intentional and documented, not equivalent to full three-OS compatibility.
Restoring full non-Linux automation support would require a superseding decision
and appropriate integration validation.
