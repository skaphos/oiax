# Managed-request notifications

Oiax can announce managed branch-promotion and backflow requests through Teams
Workflows, Slack incoming webhooks, or a generic HTTPS webhook. Merge alerts are
the default; creation alerts are opt-in. A merge establishes branch promotion,
not deployment. Unmanaged requests are never notification sources. Email is not
implemented (tracked separately in #76).

## Set up a destination

Notifications require Oiax v2.0.0 or newer. Upgrade the Action/template and binary
before adding notification configuration; see [Upgrading to v2](upgrading-v2.md).
Add the following under the existing graph's `spec`, using only branches
declared in that graph:

```yaml
notifications:
  environmentNames:
    test: test
  destinations:
    - name: operations
      type: slack
      endpointEnv: OIAX_OPERATIONS_WEBHOOK
      events: [request-created, request-merged]
      requestTypes: [promotion, backflow]
```

The example contains a variable **name**, never its value. Bind the endpoint
through your runner's secret store. Do not paste webhook addresses into graph
configuration, committed templates, command-line arguments, logs, or tickets.

- **Slack:** create an app incoming webhook for the intended channel. Its URL
  identifies the destination. Success requires HTTP 200 and the response `ok`.
  See [Slack's webhook setup](https://api.slack.com/messaging/webhooks).
- **Teams:** use a Workflows webhook configured for the URL-secret (`Anyone`)
  mode and assign a co-owner for continuity. Oiax sends an Adaptive Card v1.2 with
  inert text. Tenant-authenticated OAuth workflows and legacy connector endpoints
  are not this transport's supported authentication contract.
  See [Microsoft's webhook trigger](https://learn.microsoft.com/en-us/connectors/teams/#when-a-teams-webhook-request-is-received)
  and [workflow ownership guidance](https://learn.microsoft.com/en-us/microsoftteams/platform/webhooks-and-connectors/how-to/connectors-using).
- **Webhook:** use a receiver implementing the [versioned JSON contract](../reference/notification-webhook.md).
  Any 2xx response is HTTP acceptance. Receivers should deduplicate event IDs.

Only HTTPS is accepted. Redirects, URL userinfo, fragments and disallowed network
addresses are rejected. Every DNS answer is checked at connect time before
dialing; TLS still validates
the original hostname. `allowPrivateNetwork: true` explicitly permits private
addresses for an intentionally internal receiver; it does not disable TLS checks.

The existing forge credential must also be able to read and update the exact
`refs/notes/oiax/notifications/v1/<graph-key>` ref. GitHub and Azure use their
existing Git authentication; there is no extra state-service credential. Notes
updates use expected-tip checks and append-only ancestry. Oiax does not grant
permissions or change repository settings for you.

Keep push triggers restricted to graph **branches**, so notes updates do not
start recursive runs. Add scheduled repair appropriate to your latency needs:
delivery happens only during `reconcile`, not in a background daemon. Event
triggers are hints and missed runs are recovered by later observation.

## Validate, preview, activate

```sh
oiax validate --config .oiax.yaml
oiax plan --config .oiax.yaml --config-ref main --output json
oiax reconcile --config .oiax.yaml --config-ref main
```

Configuration and body files are pinned to the same commit. `validate` is local;
`plan` can read forge/notes state but never resolves endpoint variables, sends,
claims or writes remote refs. Its preview cannot confirm receiver credentials or
visibility. Notification-only backlog does not cause detailed exit 2.

The first enabled reconcile establishes a current cutoff before core request
creation. It does not replay historical merges. Creation provenance is captured
in a separate immutable comment in the actual POST, so later runs can recover
after a crash or failed follow-up metadata update. Adopting an existing request
does not manufacture a new creation or rewrite its original provenance.

## Customize wording

```yaml
notifications:
  environmentNames: {test: test}
  templates:
    title: '{{.RequestType}}: {{.Event}}'
    body: |
      {{if eq .Event "request-created"}}Ready for review for {{.DestinationEnvironment}}.
      {{else if eq .RequestType "backflow"}}These commits were returned to {{.DestinationEnvironment}} by backflow.
      {{else}}These commits were promoted to the {{.DestinationEnvironment}} environment.
      {{end}}{{range .Commits}}- {{.ShortSHA}} {{.Subject}}
      {{end}}
  destinations:
    - name: operations
      type: teams
      endpointEnv: OIAX_OPERATIONS_WEBHOOK
```

See the [closed notification context](../reference/templates.md#notification-templates).
Destination overrides inherit each missing title/body slot independently;
explicit empty strings suppress only custom text. Fixed identity and completeness
facts cannot be removed. Text is inert on Teams and Slack. Non-request HTTP(S)
addresses are redacted from free-form text to avoid exposing secret-bearing URLs.

Commit membership describes the event, never a later moving branch. GitHub merge
summaries use completed review membership. GitHub creation membership comes from
the origin Oiax wrote when it opened the request: the commits reachable from the
origin's source OID but not from its base OID. Right after the POST, Oiax reads
the request back; when its head still equals the origin's source OID the origin
is marked verified (`headVerified`) and the commit total is exact. Otherwise the
same commits are listed with an unknown total. Requests without an origin
(opened before notifications were enabled, or adopted rather than created)
produce no `request-created` event at all. Azure uses first-iteration OIDs for creation and
completed last-merge OIDs for merges.
Unavailable evidence does not discard the notification. At most 100 summaries
are included, with 200-rune subjects and explicit truncation/unknown-total flags.
Reviewed source SHAs need not equal destination SHAs after squash/rebase.

## Delivery and recovery

Messages are saved per destination before the first attempt. A retry uses the
same event ID, facts and wording even after template edits. A committed success
receipt is terminal. An HTTP acceptance followed by a failed receipt write is
**uncertain**, not durable success: retry can duplicate recipient visibility.
If a nonaccepted result cannot be persisted, Oiax reports
`delivery-receipt-not-persisted` when no higher-priority state, storage or
cancellation diagnostic applies, while retaining the underlying safe outcome
and HTTP status, when present. That attempt remains recoverable after its lease
expires; no terminal guarantee applies until its receipt is durable.
There is no exactly-once guarantee and no claim that HTTP acceptance proves a
person could see the message.

Each run schedules up to 10 deliveries per destination and 100 total, with
one-second destination pacing. Requests have a ten-second deadline; finalization
has a shared ten-minute budget that is independent of the two-minute claim
lease, which only fences concurrent runs. Each destination's batch costs two
ledger writes per run (one claim, one receipt write); a lease renewal is
written only while the batch is still sending as its lease nears expiry. Every
send first re-reads the ledger and confirms the batch still owns the
destination and record at the run's configuration revision, so a policy
accepted by another run between the claim and any POST stops the stale sends.
Once a POST has been issued its receipt is written even if the budget expires
(on its own 90-second bound; every notes command is bounded at 60 seconds so a
stalled push on one destination cannot consume another's receipt budget), and
a claim abandoned before its POST is recorded as `canceled` rather than keeping
an earlier attempt's code (when that receipt write itself fails, the claim
waits for its lease to expire). A record settled by another attempt while the
batch is sending is skipped alone; the batch continues. Every attempt logs one `notification attempt` line
with the destination, reason, HTTP status (when an exchange completed) and
elapsed milliseconds; endpoints and payload text are never logged. Backoff and
bounded Retry-After survive runs.
One failed receiver does not block others or alter core reconcile exits 0/1/3.
Runtime rendering overflow occurs before the message is saved, leaving only that record
pending without claiming an attempt or blocking the destination's other
deliveries; validation failures still fail before core mutation. Payloads cap
at 24 KiB and responses at 16 KiB.
A generic webhook body that would exceed the payload cap drops trailing commit
summaries and sets `commitsTruncated` rather than failing. If the entire envelope
is still too large after all optional commit summaries are dropped, the attempt
fails without truncating required repository, branch, request or event identity.

A transient fault (`network-failure`, `service-failure`, `rate-limited`,
`canceled`) remains retryable with exponential backoff up to one hour, including
after 24 claimed attempts. A persisted `payload-too-large` receipt makes the
record terminal on its first such result as `skipped/payload-too-large`. Any
other deterministic result persisted when the ledger records at least **24
total claimed attempts** makes the record terminal as `skipped/abandoned`.
Missing or unpersisted receipts remain recoverable and can lead to more than 24
claims. There is no universal attempt-ID bound and no receipt compaction; the
practical bound is ledger capacity. A late accepted receipt for any proven
attempt may still replace a skipped terminal outcome with durable delivery.
These one-way v1 state extensions and their coordinated rollout are recorded in
[ADR 0018](../adr/0018-notification-terminal-outcome-rollout.md).

### Ledger budget

The ledger caps at 8 MiB and 50,000 delivery records; capacity exhaustion
suspends new work without removing receipts, so plan against the budget:

| Merge shape (two destinations) | Ledger cost per merge | Merges per 8 MiB |
| --- | --- | --- |
| No commit summaries | ~3.0 KB | ~2,780 |
| 30 commit summaries | ~12.2 KB | ~686 |
| 100 commit summaries (the cap) | ~33 KB | ~252 |

Delivered and terminal records are retained: the budget is cumulative history,
not concurrent backlog. Mitigate growth before the ledger approaches its cap:

1. Read the current size with `git fetch origin
   'refs/notes/oiax/notifications/v1/*:refs/notes/oiax/notifications/v1/*'` and
   inspect the note for the graph key; the diagnostic reason is
   `notification-capacity-exhausted`.
2. While notification policy remains enabled, repair or remove failing
   destinations. Removal retires their records and stops their retries, but the
   receipts remain in the note and still consume capacity. If all destinations
   are disabled, Oiax performs no notification I/O and cannot record retirement.
3. To stop a generation before it grows further, remove the destination in a
   **new descendant** configuration commit and re-add it under a new name. The
   new name gets a fresh cutoff and does not inherit old deliveries, so it does
   not replay old events, including terminal ones. New events for the new
   generation still add new receipts; renaming does not reclaim bytes or compact
   the existing history.
4. If the cap is reached, new work is suspended. There is no supported capacity
   recovery, compaction, or automatic pruning operation. Rewriting or deleting
   the note would lose duplicate-prevention evidence and is not a repair.

Review backlog and retention requirements before adopting high-volume use.

Use preview decisions and safe reason/action diagnostics:

| Reason | Operator action |
| --- | --- |
| `missing-secret` | Bind the configured variable in the reconcile job. |
| `invalid-endpoint`, `redirect-rejected` | Check HTTPS, DNS, TLS and network policy. |
| `configuration-failure` | With an HTTP status, the receiver refused the request; without one, the payload could not be encoded and no exchange occurred. Check the webhook URL and signature (400/401/403), that the flow still exists and is enabled (404/410), and that its trigger schema accepts the documented payload. A receipt persisted at or after 24 total claimed attempts abandons the record. |
| `service-failure`, `rate-limited` | Restore the receiver and allow saved backoff to expire. |
| `network-failure` | The exchange failed before a decisive answer: no status for connection or TLS failures, or (Slack only) the status received before its body read failed. Saved backoff retries automatically. |
| `payload-too-large` | The entire encoded envelope exceeded the transport cap after supported optional content was dropped; required identity is never truncated. Reduce custom presentation for future events; changing the template cannot repair this saved event. Its first successfully persisted receipt makes the record terminal and skipped. |
| `response-too-large` | (Slack only) Reduce the receiver's response size, then retry. |
| `abandoned` | A deterministic failure receipt persisted when the record had at least 24 total claimed attempts, so the record is terminal. Use the reported HTTP status, when present, and earlier reasons to diagnose the cause. Repair the destination for future events; this record is not automatically resent, including under a new destination name. |
| `canceled`, `notification-canceled` | The run's budget or cancellation ended the attempt; it retries on the next scheduled run. |
| `subscription-retired` | A later configuration removed the destination or subscription; no retry is scheduled. |
| `delivery-claim-lost` | Another attempt settled the record while this batch was sending; its durable receipt is authoritative. |
| `notification-template-invalid` | Review the closed template fields and reduce rendered output to the documented limits. |
| `notification-ledger-absent` | The next reconcile establishes a current cutoff; restore lost notes first if prior receipts must be retained. |
| `notification-deferred` | A provider or notes failure deferred notification work; retry and inspect provider and notes permissions. |
| `accepted-receipt-uncertain` | Correlate by event ID; a retry may repeat visibility. |
| `delivery-receipt-not-persisted` | When no higher-priority state, storage or cancellation diagnostic applies, the nonaccepted outcome and HTTP status, when present, are reported, but its receipt is not durable. Repair notes persistence; the attempt remains recoverable and cannot establish terminality until a receipt is saved. |
| `notification-discovery-incomplete` | Run scheduled repair; bounded scans retain progress. |
| `invalid-notification-state` | Preserve notes; review corruption/version compatibility before restoring valid history. |
| `notification-ledger-initialized` | A current cutoff was established. If notes were lost, prior receipts require operator recovery. |
| `notification-capacity-exhausted` | Preserve receipts; new work is suspended and there is no supported compaction or capacity-recovery operation. |
| stale/unordered/mismatched revision | Use a reviewed descendant configuration commit and its pinned files. |

To revert configuration content, commit the revert as a **new descendant**. Do
not pin an old SHA or select a competing revision by timestamp. Never delete the
notes ref to clear a warning: that loses duplicate-prevention evidence. Missing
notes cannot be distinguished automatically from first activation, so protect and
back up the ref using your repository's normal reviewed recovery process.

## Changing destination identity and rollback

Rotating a secret value behind the same variable keeps the destination identity.
Changing transport or variable name starts a new generation. A new destination
name gets a fresh cutoff and does not inherit or reroute old deliveries.

Disabling/removing one destination while another remains enabled records its
retirement. Re-enabling it then starts a fresh generation. By contrast, when
**all** destinations are disabled, Oiax performs zero notification I/O: it cannot
record retirement. Unchanged re-enable resumes the prior epoch/backlog and can
discover eligible events from the disabled interval. Global disable also cannot
immediately cancel a worker already running or recall an in-flight HTTP request.

For rollback, remove optional notification configuration (or first disable it
while still using a compatible binary), then downgrade. Older binaries reject
unknown configuration fields, even disabled ones. Preserve notes and creation
origin comments; do not reset history, release-managed files, or tags manually.

The ledger only gains fields and values when a newer binary records them; its
reader rejects any it does not recognize (ADR 0014). Current records can carry
`lastStatus` after a receiver answers, and terminal skipped records can carry
`abandoned` or `payload-too-large`. These terminal receipts may also be created
by failures before a network exchange, so they do not necessarily have a
`lastStatus`. Once any of these newer fields or values has been recorded,
binaries older than the release that introduced it reject the whole ledger as
`invalid-notification-state` and suspend sends; ledgers never touched by one
stay byte-for-byte compatible. This one-way rollout is specified by
[ADR 0018](../adr/0018-notification-terminal-outcome-rollout.md). Downgrade before enabling a destination on the
newer release, keep the newer release once newer state has been recorded rather
than editing notes by hand, and do not run two binary versions against one graph.

Live provider/recipient visibility and setup-time acceptance are deferred by the
maintainer to post-release adoption testing. Automated local fixtures and CI are
not substitutes for that evidence; no latency percentile is asserted here.
