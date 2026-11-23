package textsafe

import (
	"strings"
	"unicode"
)

const (
	// maxSubjectRunes caps a cleaned subject, in runes, including the
	// truncation mark.
	maxSubjectRunes = 200
	// truncationMark ends a capped subject.
	truncationMark = "…"
	// wordJoiner is U+2060 WORD JOINER: invisible, so text still reads
	// correctly, while a host's byte-level match on a command or link
	// sequence fails.
	wordJoiner = '⁠'
)

// Subject cleans a raw untrusted value once, before any surface sees it:
// invalid UTF-8 becomes U+FFFD, only the first line is kept (a line ends at
// CR, LF, NEL, U+2028, or U+2029), control and Unicode format (Cf)
// characters are removed (this includes the bidi controls and U+2060, so a
// value cannot smuggle its own word joiners past the later functions), and
// the result is capped at 200 runes in total: a longer value keeps its
// first 199 runes and ends with "…". Subject is idempotent.
func Subject(s string) string {
	s = strings.ToValidUTF8(s, "�")
	if i := strings.IndexFunc(s, isLineEnd); i >= 0 {
		s = s[:i]
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	if r := []rune(s); len(r) > maxSubjectRunes {
		s = string(r[:maxSubjectRunes-1]) + truncationMark
	}
	return s
}

func isLineEnd(r rune) bool {
	switch r {
	case '\n', '\r', '\u0085', ' ', ' ':
		return true
	}
	return false
}

// LogCommand breaks every CI host command opener in s by inserting a word
// joiner between doubled '#' and doubled ':' characters, so none of "::"
// (GitHub Actions workflow commands), "##vso[" (Azure Pipelines logging
// commands, matched case-insensitively), or "##[" (Azure Pipelines
// formatting commands) can appear anywhere in the output. Removing the
// word joiners gives back the input.
func LogCommand(s string) string {
	if !strings.Contains(s, "##") && !strings.Contains(s, "::") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	var prev rune
	for _, r := range s {
		if (r == '#' || r == ':') && r == prev {
			b.WriteRune(wordJoiner)
		}
		b.WriteRune(r)
		prev = r
	}
	return b.String()
}

// PlainLink breaks the sequences a log viewer turns into links, "://" and
// a word-initial "www." (any case), by inserting a word joiner. It is for
// plain-text surfaces such as the conflict warning. Removing the word
// joiners gives back the input.
func PlainLink(s string) string {
	rs := []rune(s)
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i, r := range rs {
		b.WriteRune(r)
		switch {
		case r == ':' && hasPrefixAt(rs, i+1, "//"):
			b.WriteRune(wordJoiner)
		case (r == 'w' || r == 'W') && wordInitialWWW(rs, i):
			b.WriteRune(wordJoiner)
		}
	}
	return b.String()
}

// lookAlike maps every Markdown-active ASCII character to a visible
// look-alike that no Markdown renderer treats as syntax. Look-alikes, not
// backslash escapes: a backslash is literal inside a code span, so an
// escaped backtick could still close a template-authored span.
var lookAlike = map[rune]rune{
	'`':  'ˋ', // ˋ MODIFIER LETTER GRAVE ACCENT
	'*':  '∗', // ∗ ASTERISK OPERATOR
	'_':  '＿', // ＿ FULLWIDTH LOW LINE
	'[':  '［', // ［
	']':  '］', // ］
	'(':  '（', // （
	')':  '）', // ）
	'<':  '‹', // ‹
	'>':  '›', // ›
	'#':  '＃', // ＃
	'|':  '｜', // ｜
	'!':  'ǃ', // ǃ LATIN LETTER RETROFLEX CLICK
	'~':  '∼', // ∼ TILDE OPERATOR
	'\\': '＼', // ＼
	'@':  '＠', // ＠
	'&':  '＆', // ＆
	'=':  '＝', // ＝
}

const (
	schemeColon  = '꞉' // ꞉ MODIFIER LETTER COLON, for the ':' of "://"
	leadingDash  = '‐' // ‐ HYPHEN
	leadingPlus  = '＋' // ＋
	orderedDot   = '．' // ． FULLWIDTH FULL STOP
	wwwDotLeader = '․' // ․ ONE DOT LEADER
)

// Markdown makes s render as visible, literal text in any Markdown
// position a template can place it, including inside a code span and on a
// line of its own. It replaces every Markdown-active ASCII character with
// a look-alike, the ':' of "://" and the first '.' of a word-initial
// "www.", a leading '-', '+', or digits-then-'.' (after any leading
// spaces), and turns line breaks and other control characters into spaces.
func Markdown(s string) string {
	rs := []rune(s)
	lead := 0
	for lead < len(rs) && (rs[lead] == ' ' || rs[lead] == '\t') {
		lead++
	}
	digitsEnd := lead
	for digitsEnd < len(rs) && rs[digitsEnd] >= '0' && rs[digitsEnd] <= '9' {
		digitsEnd++
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	wwwDot := -1
	for i, r := range rs {
		if i == wwwDot {
			b.WriteRune(wwwDotLeader)
			continue
		}
		switch {
		case unicode.IsControl(r) || isLineEnd(r):
			b.WriteRune(' ')
		case i == lead && r == '-':
			b.WriteRune(leadingDash)
		case i == lead && r == '+':
			b.WriteRune(leadingPlus)
		case i == digitsEnd && digitsEnd > lead && r == '.':
			b.WriteRune(orderedDot)
		case r == ':' && hasPrefixAt(rs, i+1, "//"):
			b.WriteRune(schemeColon)
		default:
			if (r == 'w' || r == 'W') && wordInitialWWW(rs, i) {
				wwwDot = i + 3
			}
			if l, ok := lookAlike[r]; ok {
				r = l
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

// wordInitialWWW reports whether rs[i:] starts "www." (any case) and rs[i]
// begins a word.
func wordInitialWWW(rs []rune, i int) bool {
	if i > 0 && isWordRune(rs[i-1]) {
		return false
	}
	if i+4 > len(rs) {
		return false
	}
	for k := range 3 {
		if c := rs[i+k]; c != 'w' && c != 'W' {
			return false
		}
	}
	return rs[i+3] == '.'
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func hasPrefixAt(rs []rune, i int, prefix string) bool {
	for _, p := range prefix {
		if i >= len(rs) || rs[i] != p {
			return false
		}
		i++
	}
	return true
}
