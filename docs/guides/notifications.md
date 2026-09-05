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
addresses are rejected. DNS is checked again when connecting; TLS still validates
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
summaries use completed review membership; exact GitHub creation membership is
unavailable because the API does not establish a first creation iteration. Azure
uses first-iteration OIDs for creation and completed last-merge OIDs for merges.
Unavailable evidence does not discard the notification. At most 100 summaries
are included, with 200-rune subjects and explicit truncation/unknown-total flags.
Reviewed source SHAs need not equal destination SHAs after squash/rebase.

## Delivery and recovery

Messages are saved per destination before the first attempt. A retry uses the
same event ID, facts and wording even after template edits. A committed success
receipt is terminal. An HTTP acceptance followed by a failed receipt write is
**uncertain**, not durable success: retry can duplicate recipient visibility.
There is no exactly-once guarantee and no claim that HTTP acceptance proves a
person could see the message.

Each run schedules up to 10 deliveries per destination and 100 total, with
one-second destination pacing. Requests have a ten-second deadline; finalization
has a shared two-minute budget. Backoff and bounded Retry-After survive runs.
One failed receiver does not block others or alter core reconcile exits 0/1/3.
Runtime rendering overflow stays pending until corrected; validation failures
still fail before core mutation. Payloads cap at 24 KiB and responses at 16 KiB.
The ledger caps at 8 MiB and 50,000 delivery records; capacity exhaustion suspends
new work without removing receipts. Review backlog and retention requirements
before adopting high-volume use; no destructive automatic pruning is provided.

Use preview decisions and safe reason/action diagnostics:

| Reason | Operator action |
| --- | --- |
| `missing-secret` | Bind the configured variable in the reconcile job. |
| `invalid-endpoint`, `redirect-rejected` | Check HTTPS, DNS, TLS and network policy. |
| `service-failure`, `rate-limited` | Restore the receiver and allow saved backoff to expire. |
| `accepted-receipt-uncertain` | Correlate by event ID; a retry may repeat visibility. |
| `notification-discovery-incomplete` | Run scheduled repair; bounded scans retain progress. |
| `invalid-notification-state` | Preserve notes; review corruption/version compatibility before restoring valid history. |
| `notification-ledger-initialized` | A current cutoff was established. If notes were lost, prior receipts require operator recovery. |
| `notification-capacity-exhausted` | Preserve receipts; review pending volume and capacity before retrying. |
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

Live provider/recipient visibility and setup-time acceptance are deferred by the
maintainer to post-release adoption testing. Automated local fixtures and CI are
not substitutes for that evidence; no latency percentile is asserted here.
