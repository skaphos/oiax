# Local notification CLI fixture

The notification CLI tests share one `cmd/oiax` build per package run, made into
a disposable directory using Go's build overlay. The overlay adds these three files and substitutes **only**
three expressions in `internal/reconcile/notification_runtime.go`: the sender
constructor call, the runtime's `Now` clock, and the clock read for a create's
pre-POST origin stamp. No production source on disk is changed, and the
ordinary binary cannot read these fixture variables.

The fixture clock is the wall clock, but it never reads earlier than a floor plus
the process's own monotonic elapsed time. The floor is the process's own start,
or `OIAX_FIXTURE_CLOCK_FLOOR` when that is later. A test that seeds a forge event
with its own clock sets that floor, and the test fixture carries the floor
forward before every launch, past whatever the ledger records. Within a process
the clock also never returns less than its previous reading plus the monotonic
time since, so a wall clock that jumps ahead and then steps back cannot make two
readings decrease. Together they keep cutoffs, scan watermarks, origin evidence
and seeded events ordered on a host whose wall clock is stepped backwards (#143),
inside one run and across runs. `clock_test.go` holds the clock's deterministic
unit tests; `TestFixtureClockNeverDecreases` overlays both files into
`internal/reconcile` and runs them.

The forge factory supplies the real GitHub/Azure providers with a local HTTPS
API, a fixture-only CA, and a bare Git remote. The notification client uses its
existing DNS/dial seams to map `example.com` to the local HTTPS receiver. TLS
hostname verification remains enabled. Production DNS/SSRF behavior is tested
separately in `internal/notification/delivery/client_test.go`.

Config loading, provider parsing, planning, apply, observation, scheduling,
rendering, adapters, remote notes CAS, receipts, and process exit handling run
from production code. Each invocation is a fresh process; delivery success must
survive in the bare remote, not a test double or process memory.

This is fixture-built CLI integration evidence, not a stock-binary live-service
test or proof that Teams/Slack displayed an accepted message.
