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

- Summary render context: `.Conflict.Subject` and every `.Commits[].Subject` are `textsafe.Markdown(textsafe.Subject(raw))`; all other fields are trusted and unchanged.
- Artifact render context: `.Conflict.Subject` is `textsafe.Subject(raw)` (capped and stripped; spec FR-013). `TestDefaultConflictBody` is updated for the new closing text.
