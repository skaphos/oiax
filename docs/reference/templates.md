# Request and notification text templates

> Key-by-key reference for `spec.templates`. For the worked
> change-management examples this feature exists for, see the
> [governance templates guide](../guides/governance-templates.md); for
> the design and its constraints, [ADR 0011](../adr/0011-templatable-request-text.md).

Oiax authors four pieces of human-facing text, and each is templatable
from configuration:

| Surface | Default text | Template slot |
| --- | --- | --- |
| Promotion request title + body | `oiax: promote <from> to <to>` + a short managed-request body | `spec.templates.promotion`, overridable per edge via `spec.promotions[].templates` |
| Backflow request title + body | `oiax: backflow <from> to <to>` + a short managed-request body | `spec.templates.backflow` |
| Backflow-conflict artifact title + body | `oiax: backflow conflict <from> -> <to>` + the operator playbook | `spec.templates.backflowConflict` |
| Backflow `--no-ff` merge-commit message (merge strategy only) | git's default merge message | `spec.templates.backflowMergeMessage` |

What Oiax does **not** author, and templates cannot reach: the promotion
merge commit (the forge's merge button owns it, per the repository's
merge-message settings) and cherry-pick backflow commit messages (git
owns them; the `cherry-pick -x` provenance trailer is load-bearing
identity, [ADR 0004](../adr/0004-backflow-execution.md)).

Templates are [Go `text/template`](https://pkg.go.dev/text/template)
documents. Configuration stays declarative data: the function set is
curated and deterministic (no clock, no environment), and there is no
arbitrary code execution surface.

## Configuration keys

### `spec.templates.promotion`, `.backflow`, `.backflowConflict`

| Key | Type | Meaning |
| --- | --- | --- |
| `title` | string | Inline template for the title. Rendered output is reduced to a single line (control characters become spaces) and capped at 256 runes. |
| `body` | string | Inline template for the human body. Mutually exclusive with `bodyFile`. |
| `bodyFile` | string | Repository-relative path (forward slashes; no `..`; not absolute) to a template file for the body. Mutually exclusive with `body`. |

A slot may set only `title`, only a body source, or both; unset members
keep the built-in text. `spec.promotions[].templates` (promotion edges
only) accepts the same keys and overrides `spec.templates.promotion`
**per field** — an edge that sets only `title` keeps the graph-wide
custom body.

### `spec.templates.backflowMergeMessage`

| Key | Type | Meaning |
| --- | --- | --- |
| `text` | string | Inline template for the merge-commit message. Mutually exclusive with `file`. |
| `file` | string | Repository-relative path to a template file. Mutually exclusive with `text`. |

Requires `spec.backflow.strategy: merge`. Unset keeps git's default
message. The rendered message is part of the merge commit, so editing the
template changes the replayed commit's SHA once — the managed branch is
force-pushed once and self-heals, the same bounded churn as a target
advance.

### Where template files are read from

`bodyFile` / `file` references are read from the **same pinned source as
the configuration document** ([ADR 0003](../adr/0003-pinned-configuration-ref.md)):
the pinned config ref for `plan` and `reconcile`, the working tree for
`validate` and `graph`. Template content proposed in a pull request is
therefore never executed with privileged credentials until it lands on
the pinned ref. Files are capped at 1 MiB.

## Variable context

Every template renders over the same context; fields not meaningful for
a surface hold their zero value. **Trusted** fields come from validated
configuration or git object ids. **UNTRUSTED** fields are
attacker-influenceable free text (anyone who can land a commit on an
observed branch controls them) — see [Untrusted variables](#untrusted-variables).

| Variable | Type | Trust | Promotion | Backflow | Conflict | Merge message |
| --- | --- | --- | --- | --- | --- | --- |
| `.Graph` | string | trusted | graph name | graph name | graph name | graph name |
| `.Type` | string | trusted | `promotion` | `backflow` | `conflict` | `backflow` |
| `.From` | string | trusted | edge source branch | backflow source (downstream) branch | backflow source branch | backflow source branch |
| `.To` | string | trusted | edge destination branch | backflow target branch | backflow target branch | backflow target branch |
| `.Count` | int | trusted | commits the request promotes | commits the request returns | commits that failed to return | commits the merge returns |
| `.Commits` | list | **subjects UNTRUSTED** | post-ladder unpromoted commits, newest first¹ | the return set, newest first | the return set, newest first | the return set, newest first |
| `.SourceHead` | string | trusted | full source head object id | full backflow source head | full backflow source head | full backflow source head |
| `.SourceHeadShort` | string | trusted | abbreviated source head¹ | abbreviated source head | abbreviated source head | abbreviated source head |
| `.Strategy` | string | trusted | (empty) | `cherry-pick` or `merge` | `cherry-pick` or `merge` | `merge` |
| `.Mechanism` | string | trusted | (empty) | `cherry-pick` or `merge commit` | `cherry-pick` or `merge commit` | `merge commit` |
| `.Conflict` | object | see below | nil | nil | set | nil |

¹ On the promotion surface, `.Commits` and `.SourceHeadShort` are
populated only when the edge's promotion text is customized — assembling
the post-ladder commit list costs extra observation, so the built-in
defaults skip it. `.Count` is always populated.

Each `.Commits` entry:

| Field | Type | Trust | Meaning |
| --- | --- | --- | --- |
| `.SHA` | string | trusted | full object id |
| `.ShortSHA` | string | trusted | first 7 hex digits |
| `.Subject` | string | **UNTRUSTED** | commit subject line, capped at 200 runes |

`.Commits` is capped at **100 entries**; `.Count` always carries the true
total, so a template can note truncation with
`{{if gt .Count (len .Commits)}}…and more (see the diff){{end}}`.

`.Conflict` (backflow-conflict surface only):

| Field | Type | Trust | Meaning |
| --- | --- | --- | --- |
| `.Conflict.SHA` | string | trusted | object id of the failing commit (cherry-pick) or source head (merge) |
| `.Conflict.Subject` | string | **UNTRUSTED** | its subject line |
| `.Conflict.Applied` | int | trusted | commits applied cleanly before the conflict (cherry-pick only) |
| `.Conflict.Whole` | bool | trusted | `true` for the merge strategy (whole set attempted), `false` for cherry-pick |

There is deliberately **no clock variable** (`RunTime` or similar):
bodies render once at creation and never again, so a timestamp would
only misstate the request's age — use the forge's own timestamps.

## Functions

Beyond `text/template`'s builtins (`printf`, `len`, `index`, `range`,
`if`, comparisons, …), the curated set is:

| Function | Example | Meaning |
| --- | --- | --- |
| `trunc` | `{{trunc 72 .Subject}}` | cap a string at n runes |
| `shortSHA` | `{{shortSHA .SourceHead}}` | abbreviate an object id to 7 hex digits |

Nothing volatile is available (no time, no environment, no exec):
rendered output must be a deterministic function of the context.

## Rendering rules and constraints

- **Render once, at creation.** Oiax never re-renders the human body of
  an existing managed request: baseline updates rewrite only the
  metadata marker, and a create that adopts an existing request (the
  duplicate-422 path) leaves its body untouched. Counts and commit lists
  in a body therefore describe the request *as created*. The conflict
  artifact refreshes only when its recorded source head advances.
- **The marker is not templatable.** The machine-readable marker block
  is appended by the provider *after* the rendered body. A rendered body
  is rejected when it contains a recognizable oiax marker block or an
  unclosed `<!--`. Balanced HTML comments without an `oiax:` key are
  fine (and useful as reviewer fill-in prompts).
- **Validated at load.** Every configured template is compiled and
  sample-rendered when configuration loads, so `oiax validate` (and
  every `plan`/`reconcile`) rejects a broken template up front, naming
  the slot. The sample context carries two commits; conflict variables
  are only offered to the conflict slot.
- **Render failures fail closed.** At apply time, a promotion or
  backflow render error aborts the apply (exit 1) rather than opening a
  request without its scaffold. The conflict artifact follows its
  best-effort posture ([ADR 0008](../adr/0008-durable-backflow-conflict-artifact.md)):
  a render failure warns and retries next run, never masking the exit-3
  divergence.
- **Output caps.** Titles: single line, 256 runes (reduced silently).
  Bodies and merge messages: 60 000 bytes (error). Template files: 1 MiB.

## Untrusted variables

Commit subjects (`.Commits[].Subject`, `.Conflict.Subject`) are free
text controlled by whoever lands commits on observed branches. Oiax
contains them — capped, kept out of marker fields and labels, and
stripped of line breaks in titles — but **does not escape them**: bodies
are Markdown and escaping policy belongs to your template. Treat
subjects accordingly:

- Prefer subjects in code spans or list items, not in headings or link
  targets.
- Do not build titles from subjects if your review tooling keys on
  title text; titles are presentation-only to Oiax, but maybe not to
  your org.
- Never assume a subject is a single "word" — it can contain Markdown
  syntax, mentions, and up to 200 runes of anything printable.

## Notification templates

`spec.notifications.templates` and each notification destination's `templates`
are independent of request-body templates. Optional `title`, `body` and
`bodyFile` slots inherit independently from graph defaults and then built-ins.
Body and bodyFile are mutually exclusive. Explicit `""` suppresses only the
custom slot; required event facts remain on every transport. Files are read from
the same pinned commit as configuration, with a 1 MiB source limit.

The closed context is:

| Fields | Captured meaning |
| --- | --- |
| `.Event`, `.RequestType` | `request-created`/`request-merged`, `promotion`/`backflow`. |
| `.Repository`, `.Graph` | Repository display name and graph name. |
| `.RequestID`, `.RequestURL`, `.EventID` | Explicit forge identifier, validated review link and stable event ID. |
| `.SourceBranch`, `.DestinationBranch` | Actual PR head/base branch names. |
| `.LogicalSourceBranch`, `.LogicalDestinationBranch` | Logical edge when known; otherwise empty. |
| `.SourceEnvironment`, `.DestinationEnvironment` | Captured logical branch labels, with actual branch fallback. |
| `.OccurredAt`, `.ObservedAt` | Original RFC3339 UTC strings, not clock objects. |
| `.Commits` | Up to 100 `{SHA, ShortSHA, Subject, URL}` summaries. URLs are currently omitted; use `.RequestURL`. |
| `.CommitCount`, `.CommitCountKnown` | Authoritative total when known; otherwise zero/false. |
| `.CommitsTruncated`, `.CommitsUnavailable` | Explicit incomplete or unavailable details. |

`trunc` and `shortSHA`, comparisons, `if`, `else`, `printf`, `len` and `index`
are deterministic. `range .Commits` bounds iteration to captured summaries.
Unknown fields are rejected even in inactive branches. Inclusion/definitions,
method calls, variable assignment, `with` and integer ranges are not exposed.
No environment, current time, randomness, process, file or network access exists.
All four event/request combinations are sample-rendered before mutation.

Titles become inert single lines capped at 256 runes; bodies are limited to
12 KiB and transport payloads to 24 KiB. Controls and bidi formatting are removed.
Non-request HTTP(S) addresses are redacted. Teams/Slack encode user text as inert
text, not Markdown or mentions. Dynamic render failures defer notification
delivery without replacing the core result.

Commit facts and labels are fixed at first event admission. GitHub creation
details are explicitly unavailable rather than guessed from pre-POST hints;
Azure can use a verified first iteration. Squash/rebase source SHAs are review
identities, not a promise of destination SHAs. Truncation and unknown totals are
reported independently of custom wording.

Each destination's rendered message is saved before its first attempt and reused
after errors or uncertain acceptance. Template edits affect only not-yet-rendered
deliveries. The complete fixed identity/completeness section is never templatable.
See the [notification guide](../guides/notifications.md#customize-wording).
