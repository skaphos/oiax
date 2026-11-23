# Contract: Contained Text (`internal/textsafe`)

**Spec**: [../spec.md](../spec.md) "Contained text", FR-013 to FR-016. **Research**: [../research.md](../research.md) R5, R6.

Pure functions over strings. Untrusted input today is the failing commit's subject; any future untrusted value on these surfaces uses the same functions.

## Subject(s string) string

1. `strings.ToValidUTF8(s, "�")`.
2. Keep the first line only.
3. Remove control characters (`unicode.IsControl`) and format characters (`unicode.Is(unicode.Cf, r)`, which includes the bidi controls and U+2060).
4. Cap at 200 runes; when truncated, append `…`.

Used for the shared render context (artifact and summary) and the warning. Idempotent.

## LogCommand(s string) string

Inserts U+2060 WORD JOINER so that none of these appears anywhere in the output: `##[`, `##vso[` (case-insensitive), `::`. Applied by the annotation handler to every warning message before both the annotation and the plain log copy (R6). Host percent-escaping (`escapeAnnotation`, `escapeAzureAnnotation`) still runs afterwards.

## PlainLink(s string) string

Inserts U+2060 so that `://` and a word-initial `www.` do not appear. Used for the subject in the plain-text warning (FR-015).

## Markdown(s string) string

Makes `s` render as visible literal text, inline, on GitHub and Azure DevOps Markdown renderers:

- backslash-escapes `` \ ` * _ { } [ ] ( ) # + - . ! | ~ > ``;
- replaces `<` with `&lt;` and `&` with `&amp;`;
- inserts U+2060 after `@`, and inside `://` and a word-initial `www.`;
- output contains no line break (input already single-line from `Subject`).

Applied to untrusted values before the summary render, and to trusted branch names in the Oiax-owned section header. Not applied to the artifact render (OOS-004).

## Properties (tested; fuzzed)

| Property | Function |
| --- | --- |
| Output is valid UTF-8, single line, at most 201 runes | `Subject` |
| `Subject(Subject(s)) == Subject(s)` | `Subject` |
| Output contains no `##[`, `##vso[` (any case), or `::` | `LogCommand` |
| Output contains no `://` and no word-initial `www.` | `PlainLink` |
| Output, rendered by a CommonMark/GFM renderer, yields only text nodes (no link, image, HTML, emphasis, code, heading, list, blockquote, table) | `Markdown` (corpus test + live acceptance on both hosts) |

Adversarial corpus: `internal/textsafe/testdata/adversarial.txt`, at least 50 lines, including every vector in spec User Story 2.
