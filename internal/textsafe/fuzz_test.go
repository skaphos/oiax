package textsafe

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Fuzz targets for every contained-text function (constitution: untrusted
// input is fuzzed). Seeds come from the adversarial corpus; each target
// asserts the function's contract properties.

func seedCorpus(f *testing.F) {
	for _, s := range loadCorpus(f) {
		f.Add(s)
	}
}

func FuzzSubject(f *testing.F) {
	seedCorpus(f)
	f.Fuzz(func(t *testing.T, s string) {
		checkSubjectProps(t, s, Subject(s))
	})
}

func FuzzLogCommand(f *testing.F) {
	seedCorpus(f)
	f.Fuzz(func(t *testing.T, s string) {
		if !utf8.ValidString(s) {
			// LogCommand works rune by rune; invalid input is cleaned by
			// Subject first on every surface that carries commit text.
			s = strings.ToValidUTF8(s, string(utf8.RuneError))
		}
		checkLogCommandProps(t, s, LogCommand(s))
	})
}

func FuzzPlainLink(f *testing.F) {
	seedCorpus(f)
	f.Fuzz(func(t *testing.T, s string) {
		if !utf8.ValidString(s) {
			s = strings.ToValidUTF8(s, string(utf8.RuneError))
		}
		checkPlainLinkProps(t, s, PlainLink(s))
	})
}

func FuzzMarkdown(f *testing.F) {
	seedCorpus(f)
	f.Fuzz(func(t *testing.T, s string) {
		checkMarkdownProps(t, s, Markdown(Subject(s)))
	})
}
