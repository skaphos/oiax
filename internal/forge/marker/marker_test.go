package marker_test

import (
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge/marker"
)

// full is a marker with every field populated, used wherever a test needs a
// representative valid marker.
var full = marker.Marker{
	Version:     marker.Version,
	Graph:       "release",
	Type:        "promotion",
	Source:      "development",
	Destination: "test",
	SourceHead:  "0123456789abcdef0123456789abcdef01234567",
}

func TestSerializeFrozenFormat(t *testing.T) {
	t.Parallel()
	// The serialized format is a compatibility contract (two-space indent,
	// version first, "destination" not "target"), so assert it byte-exactly.
	want := "<!--\n" +
		"oiax:\n" +
		"  version: v1\n" +
		"  graph: release\n" +
		"  type: promotion\n" +
		"  source: development\n" +
		"  destination: test\n" +
		"  sourceHead: 0123456789abcdef0123456789abcdef01234567\n" +
		"-->"
	if got := marker.Serialize(full); got != want {
		t.Errorf("Serialize =\n%s\nwant\n%s", got, want)
	}
}

func TestParseRoundTrip(t *testing.T) {
	t.Parallel()
	cases := map[string]marker.Marker{
		"full":         full,
		"empty fields": {},
		"future version": {
			Version: "v2", Graph: "g", Type: "backflow",
			Source: "main", Destination: "development", SourceHead: "abc1234",
		},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := marker.Parse(marker.Serialize(m))
			if !ok {
				t.Fatal("Parse did not recognize a serialized marker")
			}
			if got != m {
				t.Errorf("round trip = %+v, want %+v", got, m)
			}
		})
	}
}

func TestParseRecognition(t *testing.T) {
	t.Parallel()
	serialized := marker.Serialize(full)
	cases := []struct {
		name string
		body string
		ok   bool
		want marker.Marker
	}{
		{name: "no comment at all", body: "just a description", ok: false},
		{name: "empty body", body: "", ok: false},
		{
			name: "oiax mentioned in prose only",
			body: "this request is managed by oiax:\nsee docs",
			ok:   false,
		},
		{name: "unclosed comment", body: "<!--\noiax:\n  graph: g\n", ok: false},
		// Regression: the "-->" here overlaps the "<!--"; bounding the block
		// on it used to invert the inner range and panic (found by FuzzParse).
		{name: "overlapping opener and closer", body: "<!-->", ok: false},
		{name: "marker after an overlapping token", body: "<!-->\n" + serialized, ok: true, want: full},
		{
			name: "comment without the oiax key",
			body: "<!-- reviewer note -->\ntext",
			ok:   false,
		},
		{
			name: "marker surrounded by prose",
			body: "Promotes development into test.\n\n" + serialized + "\n\nDo not edit.",
			ok:   true,
			want: full,
		},
		{
			name: "marker after an unrelated comment",
			body: "<!-- template boilerplate -->\n" + serialized,
			ok:   true,
			want: full,
		},
		{
			name: "unknown keys are ignored",
			body: "<!--\noiax:\n  version: v1\n  graph: g\n  color: mauve\n-->",
			ok:   true,
			want: marker.Marker{Version: "v1", Graph: "g"},
		},
		{
			name: "extra indentation is tolerated",
			body: "<!--\n    oiax:\n      version: v1\n      graph: g\n-->",
			ok:   true,
			want: marker.Marker{Version: "v1", Graph: "g"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := marker.Parse(tc.body)
			if ok != tc.ok {
				t.Fatalf("Parse ok = %v, want %v", ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Errorf("Parse = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestReplace(t *testing.T) {
	t.Parallel()

	t.Run("keeps surrounding prose and other comments", func(t *testing.T) {
		t.Parallel()
		body := "Intro prose.\n<!-- unrelated -->\n" + marker.Serialize(full) + "\nTrailing prose."
		updated := full
		updated.SourceHead = "fedcba9876543210fedcba9876543210fedcba98"

		got, ok := marker.Replace(body, updated)
		if !ok {
			t.Fatal("Replace did not find the marker block")
		}
		for _, want := range []string{"Intro prose.", "<!-- unrelated -->", "Trailing prose."} {
			if !strings.Contains(got, want) {
				t.Errorf("Replace lost %q:\n%s", want, got)
			}
		}
		parsed, ok := marker.Parse(got)
		if !ok || parsed != updated {
			t.Errorf("Parse after Replace = %+v (ok=%v), want %+v", parsed, ok, updated)
		}
	})

	t.Run("no marker block", func(t *testing.T) {
		t.Parallel()
		got, ok := marker.Replace("plain body", full)
		if ok || got != "" {
			t.Errorf(`Replace = (%q, %v), want ("", false)`, got, ok)
		}
	})
}

func TestValidValueAndValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		value string
		valid bool
	}{
		{"plain branch name", "release/1.2", true},
		{"empty", "", true},
		{"newline", "a\nb", false},
		{"carriage return", "a\rb", false},
		{"tab", "a\tb", false},
		{"DEL", "a\x7fb", false},
		{"comment close", "a-->b", false},
		{"comment open", "a<!--b", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := marker.ValidValue(tc.value); got != tc.valid {
				t.Errorf("ValidValue(%q) = %v, want %v", tc.value, got, tc.valid)
			}
			m := full
			m.Graph = tc.value
			err := marker.Validate(m)
			if tc.valid && err != nil {
				t.Errorf("Validate = %v, want nil", err)
			}
			if !tc.valid {
				if err == nil {
					t.Fatal("Validate = nil, want error")
				}
				if !strings.Contains(err.Error(), "graph") {
					t.Errorf("Validate error %q does not name the offending field", err)
				}
			}
		})
	}
}

func TestSanitize(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, in, want string }{
		{"clean value unchanged", "release/1.2", "release/1.2"},
		{"newline neutralized", "a\nb", "a�b"},
		{"comment close neutralized", "a-->b", "a--�b"},
		{"comment open neutralized", "a<!--b", "a�!--b"},
		{"run of closers leaves none", "---->", "----�"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := marker.Sanitize(tc.in)
			if got != tc.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if !marker.ValidValue(got) {
				t.Errorf("Sanitize(%q) = %q is still not a valid marker value", tc.in, got)
			}
		})
	}
}

// TestSerializeDefendsAgainstInjection is the security property behind
// Sanitize: a hostile field must not be able to forge a marker line or
// terminate the HTML comment early.
func TestSerializeDefendsAgainstInjection(t *testing.T) {
	t.Parallel()
	m := full
	m.Graph = "g\n  sourceHead: attacker-controlled"

	got, ok := marker.Parse(marker.Serialize(m))
	if !ok {
		t.Fatal("Parse did not recognize the sanitized marker")
	}
	if got.SourceHead != full.SourceHead {
		t.Errorf("hostile graph value forged sourceHead = %q", got.SourceHead)
	}

	m = full
	m.Graph = "g-->\n# free text outside the comment"
	s := marker.Serialize(m)
	if idx := strings.Index(s, "-->"); idx != len(s)-len("-->") {
		t.Errorf("hostile value closed the comment early:\n%s", s)
	}
}

func TestTypeLabel(t *testing.T) {
	t.Parallel()
	if got := marker.TypeLabel(engine.RequestTypeBackflow); got != marker.LabelBackflow {
		t.Errorf("TypeLabel(backflow) = %q, want %q", got, marker.LabelBackflow)
	}
	if got := marker.TypeLabel(engine.RequestTypePromotion); got != marker.LabelPromotion {
		t.Errorf("TypeLabel(promotion) = %q, want %q", got, marker.LabelPromotion)
	}
}

func TestVersionNum(t *testing.T) {
	t.Parallel()
	cases := []struct {
		token string
		n     int
		ok    bool
	}{
		{"v1", 1, true},
		{"v0", 0, true},
		{"v10", 10, true},
		{"", 0, false},
		{"1", 0, false},
		{"v", 0, false},
		{"V1", 0, false},
		{"v1x", 0, false},
		{"vx", 0, false},
		// Well-formed per VersionPattern but beyond int range: the Atoi
		// failure must report not-ok, never panic or wrap around.
		{"v99999999999999999999999999", 0, false},
	}
	for _, tc := range cases {
		t.Run("token "+tc.token, func(t *testing.T) {
			t.Parallel()
			n, ok := marker.VersionNum(tc.token)
			if n != tc.n || ok != tc.ok {
				t.Errorf("VersionNum(%q) = (%d, %v), want (%d, %v)", tc.token, n, ok, tc.n, tc.ok)
			}
		})
	}
}

// FuzzParse asserts Parse never panics on arbitrary bodies, and that a body
// Parse recognizes is also one Replace can rewrite (the two share block
// discovery, and callers rely on that agreement).
func FuzzParse(f *testing.F) {
	f.Add("plain prose, no marker")
	f.Add(marker.Serialize(full))
	f.Add("<!--")
	f.Add("<!-- -->")
	f.Add("<!--\noiax:\n-->")
	f.Add("prose <!-- x --> <!--\noiax:\n  graph: g\n--> tail")
	f.Add("<!--\noiax:\n  graph: <!-- nested\n-->")
	f.Fuzz(func(t *testing.T, body string) {
		m, ok := marker.Parse(body)
		if !ok {
			return
		}
		replaced, rok := marker.Replace(body, m)
		if !rok {
			t.Fatalf("Parse recognized a marker but Replace found none in %q", body)
		}
		if _, ok := marker.Parse(replaced); !ok {
			t.Fatalf("marker lost after Replace: %q -> %q", body, replaced)
		}
	})
}

// FuzzSerializeParse asserts the writer/reader invariant for arbitrary field
// values: Serialize must always produce a parseable single marker whose fields
// are the sanitized, whitespace-trimmed inputs — hostile values degrade, they
// never corrupt the block.
func FuzzSerializeParse(f *testing.F) {
	f.Add("v1", "release", "promotion", "development", "test", "abc1234")
	f.Add("", "", "", "", "", "")
	f.Add("v1", "g-->", "t<!--", "a\nb", "c:d", " padded ")
	f.Fuzz(func(t *testing.T, version, graph, typ, source, dest, head string) {
		m := marker.Marker{
			Version: version, Graph: graph, Type: typ,
			Source: source, Destination: dest, SourceHead: head,
		}
		got, ok := marker.Parse(marker.Serialize(m))
		if !ok {
			t.Fatalf("Parse failed on Serialize(%+v)", m)
		}
		want := marker.Marker{
			Version:     strings.TrimSpace(marker.Sanitize(version)),
			Graph:       strings.TrimSpace(marker.Sanitize(graph)),
			Type:        strings.TrimSpace(marker.Sanitize(typ)),
			Source:      strings.TrimSpace(marker.Sanitize(source)),
			Destination: strings.TrimSpace(marker.Sanitize(dest)),
			SourceHead:  strings.TrimSpace(marker.Sanitize(head)),
		}
		if got != want {
			t.Errorf("Parse(Serialize) = %+v, want %+v", got, want)
		}
	})
}
