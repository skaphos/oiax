package textsafe

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
)

// corpusPath is the adversarial commit-subject corpus (spec 002 SC-004).
const corpusPath = "testdata/adversarial.txt"

// loadCorpus reads the corpus, one subject per line. Lines starting with
// "// " are comments. A backslash followed by n, r, xNN, or uNNNN is decoded;
// any other backslash is literal, so subjects can carry Markdown escapes.
func loadCorpus(t testing.TB) []string {
	t.Helper()
	f, err := os.Open(corpusPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "// ") || line == "" {
			continue
		}
		out = append(out, decodeCorpusLine(line))
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func decodeCorpusLine(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		switch s[i+1] {
		case 'n':
			b.WriteByte('\n')
			i++
		case 'r':
			b.WriteByte('\r')
			i++
		case 'x':
			if v, err := strconv.ParseUint(s[i+2:min(i+4, len(s))], 16, 8); err == nil && i+4 <= len(s) {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
			b.WriteByte(s[i])
		case 'u':
			if v, err := strconv.ParseUint(s[i+2:min(i+6, len(s))], 16, 32); err == nil && i+6 <= len(s) {
				b.WriteRune(rune(v))
				i += 5
				continue
			}
			b.WriteByte(s[i])
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// The corpus holds at least 50 subjects and every User Story 2 vector.
func TestCorpusCoversTheVectors(t *testing.T) {
	corpus := loadCorpus(t)
	if len(corpus) < 50 {
		t.Fatalf("corpus has %d subjects, want at least 50", len(corpus))
	}
	joined := strings.Join(corpus, "\n")
	for _, vector := range []string{
		"::error::x", "%0A::warning::x", "##[error]x", "##[add-mask]development", "##[stop-commands]t",
		"##vso[task.setvariable variable=OIAX_INJECTED]1", "##vso[task.complete result=Succeeded]",
		"[Resolution steps](https://attacker.example)", "https://attacker.example", "www.attacker.example",
		"<img src=x", "@org/team", "| a | b |", "# heading", "`code`", "##VSO[", string(rune(0x2060)),
	} {
		if !strings.Contains(joined, vector) {
			t.Errorf("corpus lacks the vector %q", vector)
		}
	}
	long := false
	for _, s := range corpus {
		long = long || len([]rune(s)) >= 500
	}
	if !long {
		t.Error("corpus lacks a 500-rune subject")
	}
}

// Every corpus subject meets every function's contract property
// (contracts/contained-text.md), applied the way the surfaces apply them:
// Subject first, then the surface function (T035).
func TestCorpusProperties(t *testing.T) {
	for _, raw := range loadCorpus(t) {
		clean := Subject(raw)
		checkSubjectProps(t, raw, clean)
		checkLogCommandProps(t, clean, LogCommand(clean))
		checkLogCommandProps(t, raw, LogCommand(raw))
		checkPlainLinkProps(t, clean, PlainLink(clean))
		checkMarkdownProps(t, clean, Markdown(clean))
		checkMarkdownProps(t, raw, Markdown(raw))
		// The warning applies PlainLink, then the logger LogCommand.
		warn := LogCommand(PlainLink(clean))
		if strings.Contains(warn, "://") || strings.Contains(strings.ToLower(warn), "##vso[") || strings.Contains(warn, "::") {
			t.Fatalf("warning text for %q keeps a link or command: %q", raw, warn)
		}
	}
}
