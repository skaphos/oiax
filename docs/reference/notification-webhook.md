# Notification webhook JSON v1

The generic webhook sends an HTTPS POST with `Content-Type: application/json`.
`X-Oiax-Event-ID` repeats the JSON `id` for receiver correlation/deduplication.
The endpoint is supplied only by the configured runtime variable. Redirects are
not followed. Any 2xx response is acceptance; response bodies are not interpreted
as event data. Outbound JSON is limited to 24 KiB; responses to 16 KiB.

| Field | Type | Meaning |
| --- | --- | --- |
| `schemaVersion` | integer | `1`. |
| `id` | string | Stable `sha256:` lifecycle event ID; receiver deduplication key. |
| `kind` | string | `request-created` or `request-merged`. |
| `repository` | object | Provider, host, immutable repository ID, display name, and Azure organization/project IDs where applicable. |
| `graph` | string | Graph name. |
| `request` | object | `id`, `type`, actual `source`/`destination`, verified `url`, optional logical edge fields. |
| `message` | object | Persisted custom/built-in `title` and `body` strings. |
| `commits` | array | Up to 100 `{sha, shortSha, subject}` records; optional URL is currently omitted. Never null. |
| `commitCount` | integer | Authoritative total only when `commitCountKnown` is true. |
| `commitCountKnown` | boolean | False means the total is unknown, not zero commits. |
| `commitsTruncated` | boolean | Commit list or subjects have been bounded/truncated. |
| `commitsUnavailable` | boolean | Exact event membership could not be established; consult the review link. |
| `occurredAt` | string | Authoritative creation/merge time in RFC3339 UTC. |
| `observedAt` | string | Original observation time in RFC3339 UTC. |
| `facts` | string | Non-templatable identity and completeness text. |

The `facts` section always includes repository identity, graph, event kind,
request type, actual source and destination branches, event ID, explicit request
ID, review link, original observed-at timestamp, and completeness information.
Constant or empty templates cannot remove it. Custom text never changes routing
or event truth, and a branch merge is not proof of deployment.

Event IDs are independent of destination, template text, secret rotation and
display-name changes. Creation and merge have different IDs. Different
destinations can receive different saved messages for the same ID. Retries reuse
the original event and attempted message; successful receipt persistence prevents
further intentional delivery. Acceptance without a saved receipt can result in
duplicates, so acknowledge only after your receiver durably accepts/deduplicates
the event. Oiax provides no payload signing header or exactly-once guarantee in
this version; secure the endpoint through HTTPS and its runtime secret address.

See [setup and recovery](../guides/notifications.md), [templates](templates.md#notification-templates),
and the [golden wire example](../../internal/notification/delivery/testdata/webhook.golden.json).
