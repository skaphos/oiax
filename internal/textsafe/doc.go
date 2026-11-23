// Package textsafe contains untrusted text for the surfaces Oiax writes it
// to. Untrusted text today is a failing commit's subject, which any author
// who can land a commit controls; it reaches CI logs, host annotations, and
// CI job summaries.
//
// Each function makes its input inert for one surface:
//
//   - Subject cleans the raw value once, for every surface: valid UTF-8,
//     first line only, no control or format characters, capped length.
//   - LogCommand breaks CI host command openers (GitHub Actions "::",
//     Azure Pipelines "##vso[" and "##[") anywhere in a log line.
//   - PlainLink breaks the sequences a log viewer turns into links.
//   - Markdown makes a value render as visible, literal text in any
//     Markdown position, including inside a code span, by look-alike
//     substitution of every Markdown-active character.
//
// The package is a leaf: it imports nothing from this module, so every
// surface can depend on it.
package textsafe
