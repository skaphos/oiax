# Local notification CLI fixture

The notification CLI tests share one `cmd/oiax` build per package run, made into
a disposable directory using Go's build overlay. The overlay adds these three files and substitutes **only**
two lines of the notification runtime's construction: the sender constructor
call, and its `Now` clock. No production source on disk is changed, and the
ordinary binary cannot read these fixture variables.

The clock is the wall clock unless a test sets `OIAX_FIXTURE_CLOCK_FLOOR`. Then
it never reads earlier than that floor (plus the process's own elapsed time). A
test that seeds a forge event with its own clock uses the floor so that a host
whose wall clock is stepped backwards between invocations cannot place the event
before the binary's subscription cutoff or scan watermark (#143).

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
