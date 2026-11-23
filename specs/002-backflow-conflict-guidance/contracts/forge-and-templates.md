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

### Summary render context (spec FR-016: contain on emission, not before decisions)

- `.Conflict.Subject` and every `.Commits[].Subject` hold the **cleaned value** `textsafe.Subject(raw)` on both surfaces, so conditionals and functions behave identically. For example, `{{if eq .Conflict.Subject "fix: x.y"}}` takes the same branch for the artifact and the summary.
- On the summary surface those fields carry the cleaned value as `tmpl.Untrusted`, a named string type:

  ```go
  // Untrusted is a cleaned untrusted value. Templates compare and transform
  // the underlying string; printing it emits textsafe.Markdown of it.
  type Untrusted string

  func (u Untrusted) String() string { return textsafe.Markdown(string(u)) }
  ```

  - `text/template`'s `eq`, `ne`, `lt` and the other comparisons use the reflect string value, which is the cleaned value, not `String()`.
  - Printing (`{{.Conflict.Subject}}`, `printf "%s"`) goes through `fmt`, which calls `String()`, so the emitted value is contained.
  - The template funcs that take strings (`trunc`, `shortSHA`) accept `Untrusted` and return `Untrusted`, so a transformed value is still contained when printed.
- On the artifact surface the fields are plain strings holding the cleaned value (spec FR-013; no Markdown containment, OOS-004).
- **Unsafe placements**: before using a resolved custom template on the summary surface, the coordinator renders it once with each untrusted field set to a unique sentinel and runs `textsafe.UnsafePlacements`. If any placement is reported, the summary section uses the built-in body plus the note `The configured conflict text places commit text where it could form a link, so Oiax shows its built-in steps here.` Nothing changes at load: previously accepted templates still load (Constitution XII), and the artifact still uses the configured template.
- `TestDefaultConflictBody` is updated for the new closing text.
