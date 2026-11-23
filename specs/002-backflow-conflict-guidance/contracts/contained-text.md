# Contract: Contained Text (`internal/textsafe`)

**Spec**: [../spec.md](../spec.md) "Contained text", FR-013 to FR-016. **Research**: [../research.md](../research.md) R5, R6.

Pure functions over strings. Untrusted input today is the failing commit's subject; any future untrusted value on these surfaces uses the same functions.

## Subject(s string) string

1. `strings.ToValidUTF8(s, "�")`.
2. Keep the first line only.
3. Remove control characters (`unicode.IsControl`) and format characters (`unicode.Is(unicode.Cf, r)`, which includes the bidi controls and U+2060).
4. Cap at **200 runes in total**: when longer, keep the first 199 runes and append `…` (U+2026).

Used for the shared render context (artifact and summary, before any template decision) and the warning. Idempotent.

## LogCommand(s string) string

Inserts U+2060 WORD JOINER so that none of these appears anywhere in the output: `##[`, `##vso[` (case-insensitive), `::`. Applied by the annotation handler to every warning message before both the annotation and the plain log copy (R6). Host percent-escaping (`escapeAnnotation`, `escapeAzureAnnotation`) still runs afterwards.

## PlainLink(s string) string

Inserts U+2060 so that `://` and a word-initial `www.` do not appear. Used for the subject in the plain-text warning (FR-015).

## Markdown(s string) string

Makes `s` render as visible, inert text **in any Markdown position**, including inside a template-authored code span, by **look-alike substitution** (spec FR-016; backslash escaping alone fails inside code spans):

| Input | Output | | Input | Output |
| --- | --- | --- | --- | --- |
| `` ` `` | `ˋ` U+02CB | | `\|` | `｜` U+FF5C |
| `*` | `∗` U+2217 | | `!` | `ǃ` U+01C3 |
| `_` | `＿` U+FF3F | | `~` | `∼` U+223C |
| `[` `]` | `［` `］` U+FF3B/FF3D | | `\` | `＼` U+FF3C |
| `(` `)` | `（` `）` U+FF08/FF09 | | `@` | `＠` U+FF20 |
| `<` `>` | `‹` `›` U+2039/203A | | `&` | `＆` U+FF06 |
| `#` | `＃` U+FF03 | | `:` in `://` | `꞉` U+A789 |

Plus, for a value that starts a line in the template: a leading `-` → `‐` (U+2010), `+` → `＋` (U+FF0B), and a leading digit run followed by `.` → the same digits with `．` (U+FF0E); and the first `.` of a word-initial `www.` → `․` (U+2024). Output contains no line break (input is single-line from `Subject`).

Applied **only when the value is emitted** into the summary output (see [forge-and-templates.md](forge-and-templates.md) "Summary render context"). Applied to trusted branch names in the Oiax-owned section header. Not applied to the artifact render (OOS-004).

## UnsafePlacements(rendered string, sentinel string) []string

Given a summary-surface render in which each untrusted value was emitted as a unique `sentinel`, reports the placements that no value transform can make inert (spec FR-016): the sentinel inside a Markdown link destination `](…)`, a link reference definition `[…]: …`, an autolink `<…>`, or an HTML tag or attribute. Code spans and fences are **not** unsafe, because the substituted value cannot close them. An empty result means the configured template is safe to use on the summary surface; otherwise the coordinator renders the built-in body with the FR-016 note.

## Properties (tested; fuzzed)

| Property | Function |
| --- | --- |
| Output is valid UTF-8, single line, **at most 200 runes** | `Subject` |
| `Subject(Subject(s)) == Subject(s)` | `Subject` |
| Output contains no `##[`, `##vso[` (any case), or `::` | `LogCommand` |
| Output contains no `://` and no word-initial `www.` | `PlainLink` |
| Output contains none of `` ` * _ [ ] ( ) < > # \| ! ~ \ @ & `` and no `://` or word-initial `www.` | `Markdown` |
| Output, placed in plain text, a table cell, or inside a code span by a GFM renderer, yields only literal text | `Markdown` (corpus test plus live acceptance on both hosts) |
| Sentinel in `](…)`, `[…]: …`, `<…>`, or an HTML attribute is reported; in plain text or a code span it is not | `UnsafePlacements` |

Adversarial corpus: `internal/textsafe/testdata/adversarial.txt`, at least 50 lines, including every vector in spec User Story 2.
