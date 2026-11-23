# Contract: Forge Artifact Address and Template Surface

**Spec**: [../spec.md](../spec.md) FR-003, FR-004, FR-006, FR-007. **Research**: [../research.md](../research.md) R3, R4.

## forge.ConflictArtifact.URL

```go
type ConflictArtifact struct {
    ID         ConflictArtifactID
    Source     string
    Target     string
    SourceHead string
    URL        string // forge web page; built from resolved identity + ID, never from forge text
}
```

- Set by every provider on `ListConflictArtifacts` and `CreateConflictArtifact`. The `forge.Forge` method set is unchanged.
- GitHub: `https://github.com/{PathEscape(Owner)}/{PathEscape(Repo)}/issues/{number}`.
- Azure DevOps: `https://dev.azure.com/{PathEscape(Organization)}/{PathEscape(Project)}/_workitems/edit/{id}` (project **name**).
- Conformance (`internal/forge/forgetest`): every created and listed artifact's `URL` equals the provider's expected address for its ID. Both providers' runners pass.
- Out of scope (spec OOS-007): GitHub Enterprise Server and Azure DevOps Server hosts.

## tmpl.Context.Surface

| Value | Rendered for |
| --- | --- |
| `artifact` | the conflict artifact, managed requests, merge messages (all existing renders) |
| `summary` | the conflict section in the CI summary (new) |

- Additive under ADR 0011; documented in `docs/reference/templates.md` with an example.
- `tmpl.Resolve` sample-renders the conflict pair on both surfaces, so a template that fails on either is rejected at load (existing validation behavior).
- Built-in conflict body: the closing sentence is artifact-only and forge-neutral:

  ```gotemplate
  {{- if eq .Surface "artifact"}}

  Oiax manages this conflict record. It closes automatically once the conflict
  clears (the replay succeeds, the edge converges, or the commit becomes
  returned/skipped). Do not edit the metadata block below.
  {{- end}}
  ```

### Summary template gate (spec FR-016)

Containing commit text inside an arbitrary custom template is not airtight (`printf "%#v"` bypasses a `String()` wrapper; a conditional can emit a link only for the real value; `trunc` can strip a probe sentinel). So the summary uses a custom conflict template **only if it never reads commit text**, decided once from the parse tree:

```go
// ReadsCommitText reports whether the conflict body template can read an
// untrusted value: .Conflict.Subject, .Commits[].Subject, or a whole value
// that contains them.
func (s *Set) ConflictBodyReadsCommitText() bool
```

The walk over `text/template/parse` nodes reports **true** (conservatively) when the template:

- references a field chain ending in `Subject` anywhere, including inside `range .Commits` (where `.Subject` is the element's subject);
- uses `.Conflict`, `.Commits`, an element of `.Commits` (dot inside `range .Commits`), `.` at the top level, or `$` as a **whole value**, meaning printed or passed as an argument to any function or comparison, rather than only as the root of a field chain to an allowed field;
- defines or invokes templates (`define`/`template`/`block`), which the walk does not follow.

Allowed reads include `.Conflict.SHA`, `.Conflict.Applied`, `.Conflict.Whole`, `.Conflict.SquashCommits`, `.Commits[].SHA`, `.Commits[].ShortSHA`, `len .Commits`, `range .Commits` (iteration itself), and every other `Context` field. Any node kind the walk does not recognize counts as reading commit text.

Rendering:

| Template | Artifact | Summary guidance body |
| --- | --- | --- |
| built-in (unconfigured) | built-in, `.Surface = "artifact"`, `.Conflict.Subject = textsafe.Subject(raw)` | built-in, `.Surface = "summary"`, `.Conflict.Subject` and `.Commits[].Subject` = `textsafe.Markdown(textsafe.Subject(raw))` (the built-in body only prints the subject, never branches on it) |
| custom, does **not** read commit text | custom, `.Surface = "artifact"` | custom, `.Surface = "summary"` (subject fields never read; identical decisions by construction) |
| custom, **reads** commit text | custom, `.Surface = "artifact"` | **built-in**, rendered as in the first row, preceded by the note `The configured conflict text includes commit text, so Oiax shows its built-in steps here.` |

- Nothing changes at load: previously accepted templates still load (Constitution XII), and the artifact always uses the configured template.
- `Conflict.Subject` and `Commit.Subject` stay `string`; no new template type.
- **Branch names in the built-in body** (spec "Branch names"): two new template funcs, available to every template:
  - `text`: identity on the artifact surface, `textsafe.Markdown` on the summary surface (bound per render). The built-in body uses `{{text .From}}`, `{{text .To}}`, and `{{text .SourceHeadShort}}` in prose and inline code spans.
  - `shellArg`: returns the ref unchanged when it contains only `[A-Za-z0-9._/@+=:,-]`, otherwise wraps it in single quotes (an embedded `'` becomes `'\''`). The built-in body uses it for every ref in the fenced `sh` commands. Inside the fence the literal ref is inert: no line starts with a backtick run, so it cannot close the fence.
  - For ordinary branch names (`development`, `release/1.2`) both funcs return their input, so existing artifact text is unchanged.
- `TestDefaultConflictBody` is updated for the new closing text.
