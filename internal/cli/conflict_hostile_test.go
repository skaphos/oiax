package cli

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/skaphos/oiax/v2/internal/gittest"
)

// corpusSubjects reads internal/textsafe's adversarial corpus, decoding the
// same escapes its loader does (\n, \r, \xNN, \uNNNN).
func corpusSubjects(t *testing.T) []string {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "textsafe", "testdata", "adversarial.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "// ") {
			continue
		}
		var b strings.Builder
		for i := 0; i < len(line); i++ {
			if line[i] == '\\' && i+1 < len(line) {
				switch c := line[i+1]; {
				case c == 'n':
					b.WriteByte('\n')
					i++
					continue
				case c == 'r':
					b.WriteByte('\r')
					i++
					continue
				case c == 'x' && i+4 <= len(line):
					if v, err := strconv.ParseUint(line[i+2:i+4], 16, 8); err == nil {
						b.WriteByte(byte(v))
						i += 3
						continue
					}
				case c == 'u' && i+6 <= len(line):
					if v, err := strconv.ParseUint(line[i+2:i+6], 16, 32); err == nil {
						b.WriteRune(rune(v))
						i += 5
						continue
					}
				}
			}
			b.WriteByte(line[i])
		}
		out = append(out, b.String())
	}
	return out
}

// setHotfixSubject rewrites the hotfix commit on main (the conflict's
// failing commit) to carry subject, and refreshes origin/main.
func setHotfixSubject(t *testing.T, subject string) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	msg := filepath.Join(t.TempDir(), "msg")
	// git strips control characters git itself cannot store (NUL); write the
	// raw bytes and let git keep what it keeps, as a real push would.
	if err := os.WriteFile(msg, []byte(strings.ReplaceAll(subject, "\x00", "")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "commit", "-q", "--amend", "--allow-empty-message", "--cleanup=verbatim", "-F", msg)
	head := gittest.Run(t, dir, "rev-parse", "main")
	gittest.Run(t, dir, "update-ref", "refs/remotes/origin/main", head)
}

// ownCommand matches the host command lines Oiax itself writes.
var ownCommand = regexp.MustCompile(`^(::(warning|error)::|##vso\[task\.(logissue type=(warning|error)|uploadsummary)\])`)

// assertOnlyOiaxCommands fails when any output line carries a host command
// opener other than at the start of a command Oiax writes, or an Oiax
// command line carries a second opener (spec 002 FR-014).
func assertOnlyOiaxCommands(t *testing.T, subject, out string) {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		body := ownCommand.ReplaceAllString(line, "")
		low := strings.ToLower(body)
		for _, bad := range []string{"##vso[", "##[", "::"} {
			if strings.Contains(low, bad) {
				t.Fatalf("subject %q: output line carries %q:\n%s", subject, bad, line)
			}
		}
	}
}

// assertInertSummary fails when the summary holds a Markdown link, image,
// autolink, HTML tag, or mention derived from the subject.
func assertInertSummary(t *testing.T, subject, sum string) {
	t.Helper()
	for _, bad := range []string{"attacker.example)", "<img", "<a ", "<details", "<b>", "@org/team", "@octocat", "](https://attacker", "<https://attacker"} {
		if strings.Contains(sum, bad) {
			t.Fatalf("subject %q: summary keeps %q:\n%s", subject, bad, sum)
		}
	}
	if regexp.MustCompile(`(?i)https?://attacker`).MatchString(sum) || regexp.MustCompile(`(?i)\bwww\.attacker`).MatchString(sum) {
		t.Fatalf("subject %q: summary keeps a linkable attacker address:\n%s", subject, sum)
	}
}

// Every adversarial subject stays inert on both hosts: no extra host command
// in stderr, and nothing in the summary a renderer would turn into a link,
// image, tag, or mention (spec 002 US2, SC-004; T038).
func TestReconcileConflictAdversarialSubjects(t *testing.T) {
	subjects := corpusSubjects(t)
	if testing.Short() {
		subjects = subjects[:10]
	}
	conflictRepo(t, exampleConfig, "main")
	for _, host := range []string{"github", "azure"} {
		t.Run(host, func(t *testing.T) {
			for _, subject := range subjects {
				setHotfixSubject(t, subject)
				var summaryText func() string
				if host == "github" {
					path := githubActions(t)
					summaryText = func() string { return readFile(t, path) }
				} else {
					t.Setenv("GITHUB_ACTIONS", "")
					t.Setenv("GITHUB_STEP_SUMMARY", "")
					dir := t.TempDir()
					t.Setenv("TF_BUILD", "True")
					t.Setenv("AGENT_TEMPDIRECTORY", dir)
					summaryText = func() string {
						var all strings.Builder
						matches, _ := filepath.Glob(filepath.Join(dir, "*.md"))
						for _, m := range matches {
							all.WriteString(readFile(t, m))
						}
						return all.String()
					}
				}
				useForge(t, &fakeForge{})
				out, code := runCode(t, "reconcile")
				if code != 3 {
					t.Fatalf("subject %q: exit %d, want 3\n%s", subject, code, out)
				}
				assertOnlyOiaxCommands(t, subject, out)
				sum := summaryText()
				if !strings.Contains(sum, "### Backflow conflict:") {
					t.Fatalf("subject %q: no conflict section\n%s", subject, sum)
				}
				assertInertSummary(t, subject, sum)
			}
		})
	}
}

// No credential reaches a new surface: not the warnings, the plain log, the
// exit-3 message, or any summary file, for any artifact status on either
// host (spec 002 SC-007, FR-018; T039).
func TestReconcileConflictSurfacesNoCredentials(t *testing.T) {
	const sentinel = "oiax-sentinel-credential-0f3c"
	conflictRepo(t, exampleConfig, "main")
	forges := map[string]func() *fakeForge{
		"known":        func() *fakeForge { return &fakeForge{} },
		"unhostable":   func() *fakeForge { return &fakeForge{artifactsUnsupported: true} },
		"not recorded": func() *fakeForge { return &fakeForge{createConflictErr: errSentinel(sentinel)} },
	}
	for _, host := range []string{"github", "azure"} {
		for name, mk := range forges {
			t.Run(host+"/"+name, func(t *testing.T) {
				for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN", "OIAX_GITHUB_TOKEN", "AZURE_DEVOPS_EXT_PAT", "SYSTEM_ACCESSTOKEN", "OIAX_AZURE_DEVOPS_TOKEN"} {
					t.Setenv(k, sentinel)
				}
				var summaryText func() string
				if host == "github" {
					path := githubActions(t)
					summaryText = func() string { return readFile(t, path) }
				} else {
					t.Setenv("GITHUB_ACTIONS", "")
					t.Setenv("GITHUB_STEP_SUMMARY", "")
					dir := t.TempDir()
					t.Setenv("TF_BUILD", "True")
					t.Setenv("AGENT_TEMPDIRECTORY", dir)
					summaryText = func() string {
						var all strings.Builder
						matches, _ := filepath.Glob(filepath.Join(dir, "*.md"))
						for _, m := range matches {
							all.WriteString(readFile(t, m))
						}
						return all.String()
					}
				}
				useForge(t, mk())
				out, code := runCode(t, "reconcile")
				if code != 3 {
					t.Fatalf("exit %d, want 3\n%s", code, out)
				}
				// The not-recorded fake's error carries the sentinel on purpose: a
				// forge error can echo a credential-bearing URL, and Oiax must not
				// copy it into the new surfaces. The log line may keep the raw
				// error (unchanged behavior), so check the conflict surfaces only.
				for _, line := range strings.Split(out, "\n") {
					if strings.Contains(line, "halted on") || strings.HasPrefix(line, "converged") || strings.HasPrefix(line, "  ") {
						if strings.Contains(line, sentinel) {
							t.Fatalf("credential sentinel on a conflict surface:\n%s", line)
						}
					}
				}
				if strings.Contains(summaryText(), sentinel) {
					t.Fatalf("credential sentinel in the summary:\n%s", summaryText())
				}
			})
		}
	}
}

type errSentinel string

func (e errSentinel) Error() string { return "create failed: token " + string(e) }
