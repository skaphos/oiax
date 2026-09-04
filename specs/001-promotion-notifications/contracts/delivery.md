# Proposed outbound delivery contract

All transports implement
`Send(ctx, resolvedEndpoint, DeliveryPayloadV1) -> AttemptResult`.
`DeliveryPayloadV1` contains `schemaVersion: 1`, the immutable admitted `event`,
and the destination's persisted `message: {title, body}`. The coordinator builds
it only after durable message admission; on retry it uses the stored message,
not current templates. The endpoint remains a separate runtime-only argument.
Adapters never read the store, resolve policy, mutate event facts, or re-render
templates. They encode the supplied message and independently add the complete
FR-008 identity section, including explicit request ID and observed-at time, plus
commit completeness from the event. The generic wire envelope below stays flat;
the internal payload shape does not introduce a new public JSON nesting level.
The shared client owns HTTPS validation, connection-time address policy, timeouts,
redirect refusal, bounded reads, and redaction. Pure adapters encode payloads
and interpret responses. Raw HTTP errors/responses are never persisted or logged.
Response bodies are capped at 16 KiB; outbound messages at 24 KiB. If required
identity cannot fit, record `payload-too-large` and keep the delivery pending.
Do not silently truncate a branch or request identity. Optional content may be
omitted to stay under the limit.

## Generic webhook schema v1

POST with `Content-Type: application/json` and `X-Oiax-Event-ID` equal to `id`:

```json
{
  "schemaVersion": 1,
  "id": "sha256:<64-lowercase-hex>",
  "kind": "request-merged",
  "repository": {"provider":"github","host":"github.com","id":"123456","name":"example/environments"},
  "graph": "environments",
  "request": {
    "id":"123",
    "type":"promotion",
    "url":"https://github.com/example/environments/pull/123",
    "source":"development",
    "destination":"test",
    "logicalSource":"development",
    "logicalDestination":"test"
  },
  "message":{"title":"Branch promotion completed","body":"These commits were promoted to the test environment."},
  "commits":[],
  "commitCount":0,
  "commitCountKnown":false,
  "commitsTruncated":false,
  "commitsUnavailable":true,
  "occurredAt":"2026-09-04T18:00:00Z",
  "observedAt":"2026-09-04T18:01:00Z"
}
```

Required fields are all shown except `logicalSource`/`logicalDestination`, which
are absent when unavailable for legacy backflow requests. `source` is always
the actual PR head ref. Azure identity includes the normalized organization and
project IDs in addition to repository ID. IDs are strings, including numeric
forge request IDs. Times are RFC3339 UTC. No full PR body, title, diff, or commit
message is included wholesale in v1. Bounded commit subjects and immutable commit
IDs are included in `commits`; this example explicitly shows unavailable details.
The customizable `message` follows [presentation](presentation.md). A required
identity/completeness footer is added independently. Receivers must tolerate additional fields within
schema v1; removals/retypes require a new schema version.

Success means endpoint acceptance (2xx); it does not prove downstream processing.
The whole callback URL comes from `endpointEnv`. No additional authentication
headers are supported in this release. Receiver authentication may use its secret
URL; endpoints requiring another scheme need a compatible gateway or later
transport extension. Receivers that need deduplication key on event ID.

## Teams Workflows

Use the Workflows webhook trigger configured for `Anyone`; the secret URL is the
credential. POST a `type: message` envelope containing one
`application/vnd.microsoft.card.adaptive` attachment, `contentUrl: null`, and
an Adaptive Card v1.2. Fields use RichTextBlock/TextRun, which does not interpret
Markdown; a fixed-label OpenUrl action contains only a validated forge PR URL.
No mention entities or executable actions. A 2xx is acceptance; Workflow execution
and actual channel visibility require a separate acceptance test. Do not use
retired Office 365 connector endpoints. Tenant-authenticated OAuth Workflows
are not implemented by this URL-only contract.

## Slack incoming webhook

Use an app incoming webhook; the installation binds the destination channel.
Send static top-level fallback text plus Block Kit `plain_text` sections/fields
for identity and inert user-influenced content. A fixed-label link button may
point to a validated forge PR URL. Never put branch names or arbitrary text into
`mrkdwn`. Success is HTTP 200 plus trimmed response body `ok`.

Keep section/field values under Slack's respective limits. Multiple configured
URLs may resolve to one channel without Oiax knowing; remote 429 handling remains
necessary even with one-second per-destination pacing.

## Outcomes and retries

| Result | Durable treatment |
|---|---|
| Accepted and receipt committed | terminal delivered |
| Accepted but receipt commit fails | uncertain; correlation ID retained |
| Timeout/network failure/408/5xx | retryable; exponential spacing |
| 429 | retryable; honor bounded Retry-After |
| 3xx | rejected redirect; no follow; retry after endpoint correction |
| Other 4xx or unexpected success body | safe configuration/service diagnostic; delayed retry |
| Missing secret/invalid endpoint/payload too large | no HTTP; diagnostic; delayed retry |
| Disabled/retired subscription | skipped; never reroute |

All failures preserve core exit semantics. No retry is performed inline for the
same event/destination during one invocation. Never claim exactly-once delivery
for ambiguous sends or recovered leases. Use [the model](../data-model.md) for
durable transitions and [research](../research.md) for source evidence and bounds.
