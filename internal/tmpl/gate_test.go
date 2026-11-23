package tmpl

import "testing"

// The summary template gate (spec 002 FR-016, SC-003): a configured
// conflict body that can read commit text must not render in the CI
// summary. Each case is the parse-tree verdict for one body.
func TestReadsCommitText(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		reads bool
	}{
		// Reads: the summary falls back to the built-in body.
		{"subject printed", `{{.Conflict.Subject}}`, true},
		{"printf %#v bypass", `{{printf "%#v" .Conflict.Subject}}`, true},
		{"printf inside comparison", `{{if eq (printf "%s" .Conflict.Subject) "*"}}x{{end}}`, true},
		{"conditional link", `{{if eq .Conflict.Subject "attacker.example"}}[help](https://{{.Conflict.Subject}}){{end}}`, true},
		{"trunc", `{{trunc 10 .Conflict.Subject}}`, true},
		{"commit subjects in range", `{{range .Commits}}{{.Subject}}{{end}}`, true},
		{"range variable subject", `{{range $i, $c := .Commits}}{{$c.Subject}}{{end}}`, true},
		{"whole conflict to printf", `{{printf "%v" .Conflict}}`, true},
		{"whole conflict printed", `{{.Conflict}}`, true},
		{"commit element printed", `{{range .Commits}}{{.}}{{end}}`, true},
		{"dot printed", `{{.}}`, true},
		{"root variable printed", `{{$}}`, true},
		{"with conflict then subject", `{{with .Conflict}}{{.Subject}}{{end}}`, true},
		{"variable holds conflict", `{{$x := .Conflict}}{{$x.Subject}}`, true},
		{"index of commits", `{{index .Commits 0}}`, true},
		{"subject piped", `{{.Conflict.Subject | printf "%s"}}`, true},
		{"subject in condition", `{{if .Conflict.Subject}}x{{end}}`, true},
		{"template call", `{{template "x" .}}`, true},
		{"define", `{{define "x"}}hi{{end}}{{template "x"}}`, true},
		{"block", `{{block "b" .}}x{{end}}`, true},
		{"unknown field", `{{.Nope}}`, true},
		{"reassignment escapes block", `{{$x := ""}}{{if .Commits}}{{$x = .Conflict}}{{end}}{{$x}}`, true},
		{"shadowing in condition", `{{$x := .Conflict}}{{if $x := ""}}{{end}}{{$x}}`, true},

		// Does not read: the configured body renders as written.
		{"conflict sha", `{{.Conflict.SHA}} {{shortSHA .Conflict.SHA}}`, false},
		{"conflict counts", `{{.Conflict.Applied}} {{.Conflict.SquashCommits}}{{if .Conflict.Whole}}w{{end}}`, false},
		{"commit short shas", `{{range .Commits}}{{.ShortSHA}} {{.SHA}}{{end}}`, false},
		{"commit count", `{{len .Commits}} of {{.Count}}`, false},
		{"surface guard", `{{if eq .Surface "artifact"}}artifact only{{end}}`, false},
		{"branches and funcs", `{{text .From}} {{shellArg .To}} {{trunc 3 .Graph}} {{.Strategy}} {{.Mechanism}}`, false},
		{"truthiness of conflict and commits", `{{if .Conflict}}c{{end}}{{if .Commits}}m{{end}}`, false},
		{"with conflict scalar fields", `{{with .Conflict}}{{.SHA}}{{end}}`, false},
		{"scalar variable", `{{$f := .From}}{{$f}}`, false},
		{"range variable short sha", `{{range $i, $c := .Commits}}{{$i}} {{$c.ShortSHA}}{{end}}`, false},
		{"comments and text", `{{/* note */}}plain text`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tpl, err := compileTemplate("gate test", tt.body)
			if err != nil {
				t.Fatalf("compile %q: %v", tt.body, err)
			}
			if got := readsCommitText(tpl); got != tt.reads {
				t.Fatalf("readsCommitText(%q) = %v, want %v", tt.body, got, tt.reads)
			}
		})
	}
}

// The built-in body prints the subject, but the gate applies only to a
// configured body: the built-in one is Oiax-controlled and contained.
func TestConflictBodyReadsCommitTextOnSet(t *testing.T) {
	if Default().ConflictBodyReadsCommitText() {
		t.Fatal("built-in set reports reading commit text")
	}
	reads := resolve(t, conflictTemplates("Failing: {{.Conflict.Subject}}"))
	if !reads.ConflictBodyReadsCommitText() {
		t.Fatal("configured body printing the subject is not gated")
	}
	safe := resolve(t, conflictTemplates("Failing: {{.Conflict.SHA}}"))
	if safe.ConflictBodyReadsCommitText() {
		t.Fatal("configured body reading only the SHA is gated")
	}
}
