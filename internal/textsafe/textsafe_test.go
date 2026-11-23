package textsafe

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

const wj = "\u2060"

func TestSubject(t *testing.T) {
	long := strings.Repeat("a", 500)
	tests := []struct {
		name, in, want string
	}{
		{"plain", "fix: handle nil", "fix: handle nil"},
		{"first line only", "fix: a\nsecond line", "fix: a"},
		{"carriage return ends the line", "fix: a\rinjected", "fix: a"},
		{"unicode line separator ends the line", "fix: a b", "fix: a"},
		{"controls removed", "a\x00b\x1bc\x7fd", "abcd"},
		{"tab removed", "a\tb", "ab"},
		{"bidi removed", "a\u202Eb\u2066c", "abc"},
		{"word joiner removed", "#" + wj + "#[x]", "##[x]"},
		{"invalid utf-8 replaced", "a\xffb", "a�b"},
		{"exactly 200 runes kept", strings.Repeat("é", 200), strings.Repeat("é", 200)},
		{"500 runes capped to 199 plus mark", long, strings.Repeat("a", 199) + "…"},
		{"201 runes capped", strings.Repeat("b", 201), strings.Repeat("b", 199) + "…"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Subject(tt.in)
			if got != tt.want {
				t.Fatalf("Subject(%q) = %q, want %q", tt.in, got, tt.want)
			}
			checkSubjectProps(t, tt.in, got)
		})
	}
}

// checkSubjectProps asserts the contract properties of Subject's output.
func checkSubjectProps(t *testing.T, in, got string) {
	t.Helper()
	if !utf8.ValidString(got) {
		t.Fatalf("Subject(%q) is not valid UTF-8: %q", in, got)
	}
	if n := utf8.RuneCountInString(got); n > maxSubjectRunes {
		t.Fatalf("Subject(%q) has %d runes, want at most %d", in, n, maxSubjectRunes)
	}
	for _, r := range got {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == ' ' || r == ' ' {
			t.Fatalf("Subject(%q) = %q keeps %U", in, got, r)
		}
	}
	if again := Subject(got); again != got {
		t.Fatalf("Subject is not idempotent: Subject(%q) = %q", got, again)
	}
}

func TestLogCommand(t *testing.T) {
	tests := []string{
		"::error::x",
		"prefix ::warning file=a::x",
		"%0A::warning::x",
		"##[error]x",
		"##[add-mask]development",
		"##vso[task.setvariable variable=OIAX_INJECTED]1",
		"##VSO[task.complete result=Succeeded]",
		"##VsO[task.logissue]",
		"#" + wj + "#vso[already broken]",
		":::",
		"####[x]",
		"plain text",
	}
	for _, in := range tests {
		got := LogCommand(in)
		checkLogCommandProps(t, in, got)
	}
	if got := LogCommand("plain text"); got != "plain text" {
		t.Fatalf("LogCommand changed plain text: %q", got)
	}
	if got, want := LogCommand("::x"), ":"+wj+":x"; got != want {
		t.Fatalf("LogCommand(::x) = %q, want %q", got, want)
	}
}

func checkLogCommandProps(t *testing.T, in, got string) {
	t.Helper()
	low := strings.ToLower(got)
	for _, bad := range []string{"##[", "##vso[", "::"} {
		if strings.Contains(low, bad) {
			t.Fatalf("LogCommand(%q) = %q still contains %q", in, got, bad)
		}
	}
	if stripWJ(got) != stripWJ(in) {
		t.Fatalf("LogCommand(%q) = %q changed more than word joiners", in, got)
	}
}

func TestPlainLink(t *testing.T) {
	tests := []string{
		"see https://attacker.example/x",
		"HTTP://X",
		"www.attacker.example",
		"go to WWW.attacker.example",
		"(www.x.y)",
		"ftp://a",
		"plain",
	}
	for _, in := range tests {
		got := PlainLink(in)
		checkPlainLinkProps(t, in, got)
	}
	if got := PlainLink("awww.example"); got != "awww.example" {
		t.Fatalf("PlainLink broke a non-word-initial www: %q", got)
	}
}

func checkPlainLinkProps(t *testing.T, in, got string) {
	t.Helper()
	if strings.Contains(got, "://") {
		t.Fatalf("PlainLink(%q) = %q still contains ://", in, got)
	}
	if hasWordInitialWWW(got) {
		t.Fatalf("PlainLink(%q) = %q still contains a word-initial www.", in, got)
	}
	if stripWJ(got) != stripWJ(in) {
		t.Fatalf("PlainLink(%q) = %q changed more than word joiners", in, got)
	}
}

func TestMarkdown(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "fix handle nil", "fix handle nil"},
		{"link", "[Resolution steps](https://attacker.example)", "［Resolution steps］（https꞉//attacker.example）"},
		{"image", "![x](y)", "ǃ［x］（y）"},
		{"html", "<img src=x>", "‹img src＝x›"},
		{"mention", "@org/team", "＠org/team"},
		{"table", "| a | b |", "｜ a ｜ b ｜"},
		{"heading", "# h", "＃ h"},
		{"backticks", "`code` ``x``", "ˋcodeˋ ˋˋxˋˋ"},
		{"emphasis", "*a* _b_ ~c~", "∗a∗ ＿b＿ ∼c∼"},
		{"backslash", `\*`, "＼∗"},
		{"entity", "&lt;", "＆lt;"},
		{"setext equals", "===", "＝＝＝"},
		{"setext dashes", "---", "‐--"},
		{"leading plus", "+ item", "＋ item"},
		{"leading dash after spaces", "  - item", "  ‐ item"},
		{"ordered list", "12. item", "12． item"},
		{"digit without dot", "12 items", "12 items"},
		{"inner dash kept", "a-b", "a-b"},
		{"www", "www.attacker.example", "www․attacker.example"},
		{"www upper", "see WWW.x", "see WWW․x"},
		{"www inside word kept", "awww.x", "awww.x"},
		{"scheme", "http://x", "http꞉//x"},
		{"newline flattened", "a\nb", "a b"},
		{"blockquote", "> q", "› q"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Markdown(tt.in)
			if got != tt.want {
				t.Fatalf("Markdown(%q) = %q, want %q", tt.in, got, tt.want)
			}
			checkMarkdownProps(t, tt.in, got)
		})
	}
}

// checkMarkdownProps asserts the contract properties of Markdown's output.
func checkMarkdownProps(t *testing.T, in, got string) {
	t.Helper()
	if i := strings.IndexAny(got, markdownActive); i >= 0 {
		t.Fatalf("Markdown(%q) = %q keeps active character %q", in, got, got[i])
	}
	if strings.Contains(got, "://") {
		t.Fatalf("Markdown(%q) = %q keeps ://", in, got)
	}
	if hasWordInitialWWW(got) {
		t.Fatalf("Markdown(%q) = %q keeps a word-initial www.", in, got)
	}
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("Markdown(%q) = %q keeps a line break", in, got)
	}
	lead := strings.TrimLeft(got, " \t")
	if strings.HasPrefix(lead, "-") || strings.HasPrefix(lead, "+") {
		t.Fatalf("Markdown(%q) = %q starts with a list marker", in, got)
	}
	digits := strings.TrimLeft(lead, "0123456789")
	if len(digits) < len(lead) && strings.HasPrefix(digits, ".") {
		t.Fatalf("Markdown(%q) = %q starts with an ordered-list marker", in, got)
	}
	if isSetextUnderline(got) {
		t.Fatalf("Markdown(%q) = %q can form a Setext underline", in, got)
	}
}

// markdownActive is every ASCII character Markdown must not leave behind.
const markdownActive = "`*_[]()<>#|!~\\@&="

func isSetextUnderline(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return strings.Trim(s, "=") == "" || strings.Trim(s, "-") == ""
}

func hasWordInitialWWW(s string) bool {
	low := strings.ToLower(s)
	for i := 0; ; {
		j := strings.Index(low[i:], "www.")
		if j < 0 {
			return false
		}
		at := i + j
		if at == 0 {
			return true
		}
		prev, _ := utf8.DecodeLastRuneInString(low[:at])
		if !isWordRune(prev) {
			return true
		}
		i = at + 1
	}
}

func stripWJ(s string) string { return strings.ReplaceAll(s, wj, "") }
