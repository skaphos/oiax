# Plan JSON format (`planFormatVersion: 1`)

`oiax plan -o json` and `oiax reconcile -o json` emit a `Plan` document.
`planFormatVersion: 1` is **frozen**: every field, type, and
always-present/`omitempty` distinction on this page is part of the 1.0
compatibility contract (see the [Roadmap](../architecture.md#roadmap) —
"managed-request compatibility across minor releases"). A future
incompatible change ships as `planFormatVersion: 2`, never a silent
change to the shape of version 1. Field names below are cross-checked
against the `json` struct tags in
[`internal/engine/types.go`](../../internal/engine/types.go); consumers
should treat this page as authoritative.

> For the human-readable (`-o text`) rendering and how to read a plan, see
> [Operating Oiax — reading a plan](../guides/operating.md#reading-a-plan);
> for a worked JSON-consumption recipe, [Recipes — consume the plan as
> JSON](../guides/recipes.md#consume-the-plan-as-json).
>
> **A note on endpoint field names.** The same edge endpoints are named
> differently across shapes: an [`Action`](#action) uses `from`/`to`, the
> nested [`Request`](#request) uses `source`/`target`, and the
> managed-request body marker uses `source`/`destination`. They all denote
> the edge's source and destination branches.

## `Plan`

| Field | Type | Presence | Meaning |
| --- | --- | --- | --- |
| `planFormatVersion` | int | always | Format version. `1` for this page. |
| `graph` | string | always | The graph's `metadata.name` (see the [configuration reference](configuration.md)). |
| `actions` | array of [`Action`](#action) | always | The ordered actions required to converge the graph. **Always a JSON array, never `null`** — including when the graph is fully in sync, in which case it is `[]`. |

Example, fully in sync:

```json
{
  "planFormatVersion": 1,
  "graph": "environments",
  "actions": []
}
```

## `Action`

One planned step. Every field not marked `omitempty` below is always
present in the JSON, even when its value is the type's zero value (e.g.
`reason` is always emitted, even if — hypothetically — empty).

| Field | Type | Presence | Meaning |
| --- | --- | --- | --- |
| `type` | string, [enum](#type) | always | What the action does. |
| `from` | string | always | The source branch name of the edge the action acts on. |
| `to` | string | always | The destination branch name of the edge the action acts on. |
| `unpromoted` | int | `omitempty` | Commit count; see [`unpromoted`](#unpromoted) below for its overloaded meaning. Absent (not `0`) when the action carries no commit count. |
| `equivalence` | string, [enum](#equivalence) | `omitempty` | Present only on `createPromotionRequest` and `updateManagedRequest`, always alongside a populated `unpromoted`. Currently always `reachability`; see [`equivalence`](#equivalence) below. |
| `request` | object, [`Request`](#request) | `omitempty` | The managed request the action reads or acts on. Present only on `updateManagedRequest` and `closeObsoleteRequest`. |
| `branch` | string | `omitempty` | The deterministic backflow branch name. Present only on `createBackflowRequest`. |
| `reason` | string | always | Human-readable explanation of why the action exists. |

### `type`

`type` is a closed enum. No other value is ever emitted:

| Value | Meaning |
| --- | --- |
| `createPromotionRequest` | Unpromoted source commits exist and no managed promotion request is open for the edge. |
| `createBackflowRequest` | Downstream-only commits remain to be returned from a backflow source to the backflow target. |
| `updateManagedRequest` | An open managed promotion request exists but its recorded `sourceHead` baseline is stale; the source branch advanced. |
| `closeObsoleteRequest` | An open managed request now proposes nothing — the edge synchronized out-of-band — or the request is otherwise obsolete. |
| `reportDivergence` | Destination content is not represented in the source and no backflow or drift policy accounts for it. This is not a create/update/close action; nothing is proposed. |
| `noOp` | Reserved; not currently emitted by `BuildPlan`. Consumers must still accept it as a no-effect action per the closed enum. |

### `unpromoted`

`unpromoted` is `omitempty` and its meaning depends on `type` — it is a
plain commit count, not a reference to the `EdgeState.Unpromoted` field
of the same name:

- **`createPromotionRequest` / `updateManagedRequest`** — the number of
  source commits not yet represented in the destination (commits the
  action *moves* toward the destination).
- **`createBackflowRequest`** — the number of downstream-only commits
  being returned to the backflow target (commits the action *returns*),
  i.e. `len(ToReturn)`, which may be smaller than the raw
  downstream-only count once already-returned commits are filtered out.
- **`reportDivergence`** — the number of destination commits not
  represented in the source.
- **`closeObsoleteRequest` / `noOp`** — absent; these actions carry no
  commit count.

### `equivalence`

Same enum as `EdgeState.Equivalence`
(see [Architecture — the equivalence ladder](../architecture.md#the-equivalence-ladder)),
but only one value is currently observable on an `Action`. `equivalence`
is only emitted alongside a populated `unpromoted` count, and
`EvaluateEdge`'s `Unpromoted` field is only ever populated by the final
"Rung 5: promotion required" fallback, which unconditionally records
`reachability`. Rungs 2-4 (`patch-identity`, `head-tree`, `baseline`) all
settle the edge as fully in sync instead — producing no action, or
`closeObsoleteRequest` (which never carries `equivalence`) — so those
three values can never appear on `createPromotionRequest` or
`updateManagedRequest`. They are reserved for a future
`planFormatVersion` in which some rung other than reachability might
settle an edge that still carries an action:

| Value | Ladder rung |
| --- | --- |
| `reachability` | No content-based equivalence rung (`patch-identity`, `head-tree`, `baseline`) matched; the recorded `Unpromoted` commits are the raw ancestry-reachable survivors. The only value currently emitted on an `Action`. |
| `patch-identity` | Stable patch-id (`git patch-id --stable`) match. Reserved; not currently emitted on any `Action`. |
| `head-tree` | `tree(from) == tree(to)`. Reserved; not currently emitted on any `Action`. |
| `baseline` | The recorded promotion baseline (`sourceHead`) settled the edge. Reserved; not currently emitted on any `Action`. |

### `request`

The `Request` object, present only when `request` is non-null:

| Field | Type | Presence | Meaning |
| --- | --- | --- | --- |
| `id` | string | always | Forge-assigned identifier of the managed request. |
| `type` | string (`promotion` or `backflow`) | always | Which kind of managed request. |
| `source` | string | always | The request's source branch. |
| `target` | string | always | The request's target branch. |
| `sourceHead` | string | always | The source head SHA the request currently proposes — the promotion baseline once merged. |

### `branch`

Present only on `createBackflowRequest`. The deterministic backflow
branch name, built by `BackflowBranchName`:

```text
oiax/backflow/<source>-to-<target>/<shortSHA>
```

`<shortSHA>` is the short SHA of the backflow source (downstream) branch
head — not the replayed commits — so the name is a pure function of what
is being returned. See
[Architecture — Backflow](../architecture.md#backflow) and
[ADR 0004](../adr/0004-backflow-execution.md).

### `reason`

Always present. A human-readable, one-line explanation of why the
action exists. Its exact wording is not part of the frozen contract —
only its presence and type (string) are; do not pattern-match on its
text.

## Compatibility

`planFormatVersion: 1` will not gain required fields, change a field's
JSON type, rename a field, or turn an always-present field `omitempty`.
Additive `omitempty` fields on `Action` are permitted within version 1
(new optional metadata for future action types); strict consumers should
ignore unrecognized fields rather than reject the document. A change
that breaks any of the above ships as `planFormatVersion: 2`.
