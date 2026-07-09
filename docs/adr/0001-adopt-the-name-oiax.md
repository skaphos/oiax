# 0001 — Adopt the name Oiax

- Status: accepted
- Date: 2026-07-08

## Context

The tool's working name was `Tiller` — apt for a hand-on-the-helm Git
workflow tool, but colliding with Helm v2's in-cluster component of that
name, in exactly the Kubernetes/CD ecosystem this tool serves. Skaphos
has renamed for ecosystem collisions before (`Pilot` → `Keleustes`,
2026-05-14).

## Options considered

- **Oiax** (Greek οἴαξ — the tiller itself, the handle fitted to the
  rudder head). Literal Greek for the retired working name; keeps the
  steering-by-hand intent; matches the Greek register of Skaphos, Tropis,
  and Keleustes.
- **Kedge** — recorded runner-up.
- **Halyard** — rejected: Spinnaker's configuration tool.
- **Warp** — rejected.
- **Capstan** — rejected: already an alternate considered for Tropis.

## Decision

Adopt **Oiax**. Collision review found no Kubernetes/CD-ecosystem usage.
The nearest namesake is a Japanese web-development publisher (Oiax Inc.,
`github.com/oiax`) that has itself renamed to Coregenik — outside the
ecosystem and moving away from the name. The GitHub org handle is held
by them; Skaphos tools publish under `skaphos/oiax`, so this does not
block.

## Consequences

- Repository, module (`github.com/skaphos/oiax`), CLI binary, Action,
  configuration group (`oiax.skaphos.dev`), labels, and the managed ref
  namespace (`oiax/`) all use the name.
- The name carries the design point: the οἴαξ is the tiller — a hand
  stays on it. Oiax removes Git workflow toil, not human gates.
