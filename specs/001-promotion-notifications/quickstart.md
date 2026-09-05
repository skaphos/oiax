# Notification validation guide

This is an acceptance guide for the planned implementation. The notification
config, previews, and new test suites are not implemented in the current binary.
It is not a completed test report.

## Prerequisites

- Implementation of [#75](https://github.com/skaphos/oiax/issues/75), Go from
  `go.mod`, Git ≥2.45, and a full-history clone.
- Preserve the T001 namespace decision in ADR 0015/Constitution XI v2.0.0:
  only the exact notification notes ref and expected-tip, append-only operation
  are in scope. Namespace authority is resolved; implementation conformance and
  operator authorization for disposable live resources are still prerequisites.
- Disposable GitHub and Azure repositories with a valid Oiax graph and branches
  created by an operator. Runtime forge credentials need notes read/write access.
  Tests must not automatically change repository permissions/settings.
- Test Teams Workflow (`Anyone` mode, with co-owner), Slack incoming webhook,
  and an HTTPS receiver recording event IDs. Bind URLs from the CI secret store
  to the explicit configured environment-variable names. Never put secret URLs
  in YAML, shell history, test output, or tickets.
- Branch-scoped push triggers and scheduled repair. Check that notes pushes do
  not recursively trigger broad push automation.

## Build and fixture validation

Run from the repository root after implementation:

```sh
go -C tools tool task build
go test -race -shuffle=on ./...
go test ./internal/notification/... ./internal/reconcile/... ./internal/forge/... -run 'TestNotification|TestLifecycle'
go -C tools tool task lint
go -C tools tool task staticcheck
go -C tools tool task vuln
go -C tools tool task verify-generated
reuse lint
```

Implementation adds the named test suites and a task enforcing 85% statement
coverage on new notification packages. An empty `-run` match is not evidence:
verify suites execute. Add seeded fuzzing for ledger/origin/URL/template parsing.
Supported OSes execute the same behavioral contracts.

## Configure and preview

Combine [configuration.md](contracts/configuration.md) with the fixture's valid
graph, using a `test` branch label and the environment-oriented template. Land
the config on the fixture's pinned `main` ref through normal review, then run:

```sh
./oiax validate --config .oiax.yaml
./oiax plan --config .oiax.yaml --config-ref main --output json
./oiax plan --config .oiax.yaml --config-ref main --detailed-exitcode
```

Valid structure succeeds with endpoint variables absent. Before first reconcile,
the preview identifies an uninitialized ledger. Read-only commands make no
endpoint calls or remote writes. No-policy JSON retains its original shape;
notification-only backlog does not change detailed branch exit codes. Invalid
template fields/files fail configuration validation without reading secrets.

## Lifecycle and presentation acceptance

1. Bind test runtime secrets and run
   `./oiax reconcile --config .oiax.yaml --config-ref main` to establish activation.
   Historical merged requests produce no notifications.
2. Introduce changes on the fixture source branch and reconcile. Only destinations
   subscribed to creation receive a created message, worded as ready for review.
3. Have a human or existing repository policy merge the managed request; Oiax
   performs no merge. Reconcile and observe the custom title/body, “These commits
   were promoted to the test environment,” commit IDs/subjects, and fixed PR link
   and identity footer. Verify per-destination text overrides independently.
4. Advance the source again before retrying a deliberately failed delivery. Its
   captured commits must still describe the original PR. Repeat with squash,
   rebase and a deleted backflow ref; source commit IDs must not be presented as
   newly invented destination commit IDs.
5. Exercise backflow with one destination opting out. Built-in backflow wording
   remains distinct; creation wording must never claim the request already merged.
6. Use requests with over 100 commits and unavailable history. Show a partial or
   unavailable indicator and PR link; do not invent a total from an Azure page count.
7. Repeat unchanged runs after a durable receipt: no new send. Automated fixtures
   evaluate the same event 1,000 times. Editing templates does not replay success
   or rewrite an already attempted retry payload.
8. Run equivalent scenarios on both forges, including adopted requests,
   closed-unmerged requests, source-head updates and creation-side partial errors.
9. Use constant or empty custom titles/bodies on every transport. Independently
   inspect all FR-008 fields, including explicit request identifier and original
   observed-at time, plus commit completeness. Give two destinations different
   saved messages for the same event; editing templates before retry changes
   neither destination's attempted message nor shared facts.
10. Create pending work, disable **all** destinations and invoke reconciliation:
    notification call counters stay zero and the ledger is unchanged. Re-enable
    the same identity in a descendant config commit: its old epoch/backlog resumes,
    and discovery may include disabled-interval events after the old cutoff. Repeat
    with a new destination name: it starts at a fresh cutoff without rerouting old
    work. Do not claim global disable cancels other already running workers.
11. Disable one destination while another remains enabled and reconcile to record
    the transition. Re-enable the first in a descendant commit: it gets a new
    generation and no replay of retired backlog. Contrast explicitly with step 10.

Read-only inspection of the state ref in a disposable clone:

```sh
git ls-remote origin 'refs/notes/oiax/notifications/v1/*'
```

Fetch the exact returned ref and inspect its note using the initialization anchor.
Confirm no release tag or long-lived branch was created. Never delete production
state to make a test pass.

## Failure and concurrency matrix

Disruptive cases use injected clocks, HTTP fixtures and local bare Git remotes;
live tests require opt-in disposable resources.

| Scenario | Expected evidence |
|---|---|
| Workers initialize/claim concurrently | One expected-tip winner; loser rereads; no ordinary duplicate |
| New config commits before an older worker resumes or retries CAS | Old worker cannot restore policy/admit events/claim sends; late attempt results remain monotone |
| Equal OID/different policy digest, divergent or unknown config ancestry | Safe mismatch/unordered diagnostic; no policy reset or new send; core result preserved |
| Settings reverted in a new descendant commit | Valid ordered policy update; pinning an old OID instead reports stale revision |
| Termination after PR POST | Initial origin recovers the same created event |
| Timeout after receiver acceptance | Same ID on retry, explicit uncertainty |
| Accepted send, receipt write fails | No false durable-success report |
| Lease expires while worker suspended | Document possible duplicate; no success-to-failure regression |
| Destination outage or missing secret | Others progress; core exit 0/1/3 unchanged |
| Notes permission denied | Safe diagnostic; no unsafe send or core blockage |
| Throttling/repeated server errors | Persisted next-attempt time, no repeated inline sends |
| Dense/incomplete history or long-lived PR newly merged | No false completed watermark; pending work progresses |
| Ledger/payload overflow, duplicate keys | Bounded failure, preserved receipts |
| Hostile text/template, redirects, DNS rebinding, TLS failure | No unintended mentions, secret leaks or forbidden access |
| Recorded per-destination disable/rename or secret rotation | Correct durable cutoff/generation; no rerouted backlog or success replay |
| All-disabled invocation then unchanged re-enable | Zero I/O while disabled; previous durable epoch/backlog resumes, unlike recorded per-destination retirement |
| Unknown ledger version/state loss | Safe refusal or explicit current-cutoff reinitialization |

## Recipient-visible validation and rollback

Use the following predeclared SC-001 protocol, not acceptance-response timing:

- At most 20 configured destinations, 10 due deliveries/destination and 100 total
  per observing run; no existing retry/backfill backlog and discovery completes
  within scan budget. Establish endpoint health before each batch. Schedule fixture
  reconciliation every minute; keep this cadence in the recorded test conditions.
- Predeclare at least 100 logical event/destination deliveries **per transport**,
  spread across bounded runs and including both events/request types. Do not remove
  slow or missing messages from the denominator after observing results. An actual
  outage invalidates the normal-load batch and is reported separately, never
  relabeled as a passing batch by excluding affected messages.
- For each delivery record the first observing run's completion time and actual
  recipient-visible time using a test observer or operator evidence. Correlate by
  event ID and destination; an HTTP receipt or Teams 202 is insufficient. Latency
  is `max(0, visibleAt - observingRunCompletedAt)`, so messages visible before run
  completion count as zero. Count each logical pair once, retaining any duplicates
  as separate diagnostics rather than extra successful samples.
- Require at least 95% visible by 60 seconds and 99% by five minutes per transport;
  a message missing at a deadline fails that threshold. Record sample counts,
  conditions, observer method and actual results. If visibility cannot be observed,
  leave this gate unverified; do not substitute acceptance status.
- Separately exercise outages, backlog above per-run caps, incomplete discovery
  and capacity suspension for bounded work, retained state and later progress.
  Do not apply normal-load latency promises to these recovery scenarios.

Record evidence in `checklists/implementation-validation.md` and the eventual
implementation PR; unrun live checks remain release gates. Have a first-time
operator follow published setup and measure the under-ten-minute criterion.

Disable/remove the optional policy on the pinned ref before downgrading the
binary. Check that no notification observation/delivery occurs with all
destinations disabled. Retain notes receipts and origin metadata. Email remains
outside this release, tracked in #76.
