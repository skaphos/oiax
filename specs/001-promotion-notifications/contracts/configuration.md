# Proposed notification configuration contract

Add an optional `spec.notifications` object to the existing
`oiax.skaphos.dev/v1` `PromotionGraph`. The deprecated v1alpha1 alias follows the
same existing parser behavior. This fragment must be combined with a valid graph:

```yaml
spec:
  notifications:
    environmentNames:
      test: test
    templates:
      title: '{{if eq .Event "request-merged"}}Branch promotion completed{{else}}Branch promotion ready for review{{end}}'
      body: |
        {{if eq .Event "request-merged"}}These commits were promoted to the {{.DestinationEnvironment}} environment:{{else}}These commits are ready for review for the {{.DestinationEnvironment}} environment:{{end}}
        {{range .Commits}}- {{.ShortSHA}} {{.Subject}}
        {{end}}
    destinations:
      - name: operations-teams
        type: teams
        endpointEnv: OIAX_TEAMS_WEBHOOK
      - name: review-slack
        type: slack
        endpointEnv: OIAX_SLACK_WEBHOOK
        events: [request-created, request-merged]
        requestTypes: [promotion]
      - name: audit-webhook
        type: webhook
        endpointEnv: OIAX_AUDIT_WEBHOOK
        enabled: true
        allowPrivateNetwork: false
```

| Field | Required/default | Validation |
|---|---|---|
| `destinations` | empty means off | At most 20 entries; unique names |
| `environmentNames` | optional map | Keys must be declared graph branches; nonempty labels up to 100 runes |
| `templates` | optional graph-wide title/body defaults | See presentation contract; pinned bodyFile supported |
| `name` | required | 1–63 lowercase ASCII letters, digits, hyphens; starts/ends alphanumeric |
| `type` | required | `teams`, `slack`, `webhook`; `email` rejected as unsupported |
| `endpointEnv` | required | `[A-Za-z_][A-Za-z0-9_]*`; explicit variable name only |
| `enabled` | omitted = true | Explicit false suppresses attempts |
| `events` | omitted = `[request-merged]` | Subset of created/merged enums; no duplicates; explicit empty disables all events |
| `requestTypes` | omitted = `[promotion, backflow]` | Subset of these enums; no duplicates; explicit empty disables all request types |
| `allowPrivateNetwork` | false | Only generic webhooks may enable private unicast addresses |
| destination `templates` | optional per-slot overrides | Title and body/bodyFile inherit independently |

Use pointer booleans and nil-versus-empty slice semantics to preserve explicit
false/empty choices across YAML and JSON round trips. `Default` is idempotent;
`Validate` accepts defaults before or after application. Unknown fields remain
errors. Aggregate validation reports all structural errors using the existing
public validation mechanism, without echoing a rejected secret URL as a value.

The example body is appropriate for promotion-only audiences; a destination
receiving backflow should branch on `.RequestType` or retain built-in backflow
wording. See [presentation](presentation.md) for the complete context.

The endpoint is the entire HTTPS URL in the named environment variable. This
includes any secret path/query used by Teams Workflows, Slack, or the generic
receiver. There are no inline URLs, literal tokens, custom HTTP headers, or shell
commands in v1 config. Distinct destinations may name distinct variables.

Read-only commands do not resolve these variables or contact destinations.
During reconciliation an absent, empty, or invalid value is a safe delivery
diagnostic and a pending retry, not a core exit-code failure. Disabled
destinations do not read their variables. Reject userinfo, fragments, non-HTTPS,
loopback, link-local, multicast, unspecified, and metadata-service targets.
Connection-time DNS validation prevents rebinding; private unicast is opt-in
only for generic webhooks. TLS verification is never disabled by user config.

Default request-type selection implements the accepted clarification: both types
are enabled; a destination can disable either independently. Creation events
require opt-in even when both types are selected. Empty overall policy skips
all notification observations, ledger calls, and endpoint calls.

The activation/generation rules in [data model](../data-model.md#destination-state-and-routing-lifetime)
define when new subscriptions start. Changing recipients requires a new
destination name; rotating credentials in the same variable retains history.

All-disabled invocations also skip durable configuration revision/retirement
updates. Re-enabling unchanged identities resumes their last durable cutoffs and
pending work, including discovery during the disabled interval; it does not
implicitly start fresh. A disabled destination only gets a recorded retirement
when another destination keeps notification processing active. To intentionally
start with a fresh cutoff after a global disable, use a new destination name.
Global disable does not notify already running workers through the untouched
ledger and must not be presented as an immediate cross-worker kill switch.

Enabled policy transitions advance only from an accepted config OID to a verified
descendant. Older/divergent/unprovable revisions defer notification work with a
safe diagnostic; core reconciliation keeps its original result. To revert
notification settings, commit the reverted content on top of current history,
rather than pinning an older OID. No timestamp or destination fingerprint alone
authorizes replacing a newer recorded policy.

Compatibility: see [ADR 0013](../../../docs/adr/0013-notification-configuration-contract.md).
Old configurations remain valid and old plan JSON stays byte-identical when
notifications are absent. New config must not be deployed to an older binary
whose strict decoder cannot recognize it.
