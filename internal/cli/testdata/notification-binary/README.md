# Local notification CLI fixture

`TestNotificationMergeBinary` builds `cmd/oiax` into a disposable directory using
Go's build overlay. The overlay adds these two files and substitutes **only**
the sender constructor call in `FinalizeNotifications`. No production source on
disk is changed, and the ordinary binary cannot read these fixture variables.

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
