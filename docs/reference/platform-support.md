# Platform support

Linux is Oiax's supported platform for production automation, including
standalone `plan`/`reconcile` jobs and the GitHub Action/Azure Pipelines wrappers.
This policy takes effect with the major release containing ADR 0016; it does
not retroactively change older releases.

| Platform | Support scope | Validation |
| --- | --- | --- |
| Linux amd64/arm64 | Production automation | Full coverage/integration and race/shuffle suites on Linux amd64; both architectures cross-built |
| macOS amd64/arm64 | Best-effort local CLI use | Native runner build and selected unit tests; both architectures cross-built |
| Windows amd64/arm64 | Best-effort local CLI use | Native runner build and selected unit tests; both architectures cross-built |

Release archives and checksums remain available for every listed platform.
Native CI jobs exercise their runner's architecture, not every published
architecture. Cross-building alone is not runtime verification.

Best-effort local use includes inspection and development, but does not promise
Git-backed automation reliability on macOS/Windows. Commands are not disabled
on those OSes. Reports and portability fixes are welcome; an OS-specific
integration issue is not part of the supported production contract.
The configuration API, CLI arguments and exit codes are unchanged.

## Migration

If production automation invokes Oiax directly on macOS or Windows, move that
job to a Linux amd64/arm64 runner before upgrading to the major release with
this policy. This also applies when the forge is GitHub or Azure DevOps:
the forge and the runner OS are independent choices.

Retain the same reviewed graph configuration and repository credentials, and
verify checkout paths, shell commands and Git ≥ 2.45 on the new runner.
The supplied GitHub Action and Azure Pipelines template already require Linux,
so their existing Linux consumers need no runner migration.

Local users can keep their native binary. Users unable to migrate automation
may pin their existing release while planning the move; this does not create
a new promise of maintenance for older releases.

## Contributor checks

`go -C tools tool task test-portability` builds all packages and runs a bounded
unit suite without Git-heavy integration fixtures. Linux CI retains all tests
through `test-cover` and `test-race`; no tests are deleted or globally skipped.
Non-Linux CI has a ten-minute job budget and three-minute unit-package
deadlines. Linux CI has a 25-minute job budget and ten-minute package deadlines.

See [ADR 0016](../adr/0016-linux-automation-support.md) for rationale and
[CONTRIBUTING](../../CONTRIBUTING.md) for maintenance of the test selection.
