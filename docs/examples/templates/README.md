# Example request-text templates

Ready-to-copy `text/template` files for `spec.templates` — the
change-management scaffolds from the
[governance templates guide](../../guides/governance-templates.md).

Copy them into your repository (conventionally under `.oiax/templates/`)
and reference them from `.oiax.yaml`:

```yaml
spec:
  templates:
    promotion:
      bodyFile: .oiax/templates/promotion-change-record.md.tmpl
    backflow:
      bodyFile: .oiax/templates/backflow-change-record.md.tmpl
```

Adjust the control references, classifications, and ticket-system links
to your organization's change process, then verify with `oiax validate`.
Template files are read from the pinned config ref, like `.oiax.yaml`
itself. The full variable and constraint reference is
[docs/reference/templates.md](../../reference/templates.md).

| File | Slot |
| --- | --- |
| `promotion-change-record.md.tmpl` | `spec.templates.promotion.bodyFile` (or per-edge) |
| `backflow-change-record.md.tmpl` | `spec.templates.backflow.bodyFile` |
