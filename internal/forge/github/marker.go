package github

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/skaphos/oiax/internal/engine"
)

// Managed requests carry these labels: oiax on every one, plus exactly
// one of the type labels. The type labels also encode which
// promotion/backflow request this is, redundantly with the marker, so a
// human scanning the forge UI can tell them apart.
const (
	LabelOiax      = "oiax"
	LabelPromotion = "oiax/promotion"
	LabelBackflow  = "oiax/backflow"
	// LabelConflict marks a durable backflow-conflict artifact (a forge
	// issue). Unlike the decorative label on a managed PR, this label is
	// load-bearing for issue identity (see conflictMarker): an issue has no
	// head/base+same-repo provenance, so the label is the authorization
	// substitute — only a collaborator can label an in-repo issue.
	LabelConflict = "oiax/conflict"
)

// conflictMarkerType is the marker type token a durable conflict artifact
// carries. It is a package-local string, deliberately NOT an
// engine.RequestType, so it can never leak into RequestFilter PR
// discovery.
const conflictMarkerType = "conflict"

// markerVersion is the marker schema version this build writes and fully
// understands. serializeMarker always stamps it. Readers are deliberately
// more lenient than writers: managedMarker recognizes any well-formed
// version (markerVersionPattern), so a request written by a newer release is
// still identified as oiax's own and never duplicated.
const markerVersion = "v1"

// markerVersionPattern matches a well-formed marker version token: a "v"
// followed by one or more digits ("v1", "v2", ...). Recognition accepts any
// version of this shape; the numeric value decides only whether this build
// understands the schema well enough to mutate the request (markerVersionNum).
var markerVersionPattern = regexp.MustCompile(`^v[0-9]+$`)

// marker is the parsed content of the machine-readable oiax block a
// managed request carries in its body. The field named destination in
// the marker maps to engine.ChangeRequest.Target.
type marker struct {
	Version     string
	Graph       string
	Type        string
	Source      string
	Destination string
	SourceHead  string
}

// serializeMarker renders the HTML-comment marker verbatim in the frozen
// format (two-space YAML indent, version first, destination not target).
// It is the inverse of parseMarker for the fields it carries.
//
// The marker is load-bearing for managed-request identity, so every value is
// passed through sanitizeMarkerValue: a newline or "-->" in a value must never
// be able to forge an extra marker line or close the HTML comment early. Values
// oiax actually writes (branch and graph names, hex heads, the version and type
// tokens) contain none of those, so this is a no-op for them; callers should
// also reject hostile values up front with validateMarker.
func serializeMarker(m marker) string {
	var b strings.Builder
	b.WriteString("<!--\n")
	b.WriteString("oiax:\n")
	b.WriteString("  version: " + sanitizeMarkerValue(m.Version) + "\n")
	b.WriteString("  graph: " + sanitizeMarkerValue(m.Graph) + "\n")
	b.WriteString("  type: " + sanitizeMarkerValue(m.Type) + "\n")
	b.WriteString("  source: " + sanitizeMarkerValue(m.Source) + "\n")
	b.WriteString("  destination: " + sanitizeMarkerValue(m.Destination) + "\n")
	b.WriteString("  sourceHead: " + sanitizeMarkerValue(m.SourceHead) + "\n")
	b.WriteString("-->")
	return b.String()
}

// markerControl matches any ASCII control character (newline, carriage return,
// tab, and the rest of C0 plus DEL). A marker value oiax writes never contains
// one; a value that does is treated as hostile.
var markerControl = regexp.MustCompile(`[\x00-\x1f\x7f]`)

// validMarkerValue reports whether v is safe to place in a marker line
// verbatim: no control character, and nothing that could open or close the
// surrounding HTML comment. It is the predicate validateMarker enforces at the
// boundary before a marker is written to the forge.
func validMarkerValue(v string) bool {
	if markerControl.MatchString(v) {
		return false
	}
	return !strings.Contains(v, "-->") && !strings.Contains(v, "<!--")
}

// validateMarker rejects a marker any of whose fields could forge marker lines
// or break out of the HTML comment. CreateRequest and UpdateRequest call it
// before serializing so a hostile graph name, branch name, or head is refused
// with a clear error rather than silently defanged.
func validateMarker(m marker) error {
	for _, f := range []struct{ name, val string }{
		{"version", m.Version},
		{"graph", m.Graph},
		{"type", m.Type},
		{"source", m.Source},
		{"destination", m.Destination},
		{"sourceHead", m.SourceHead},
	} {
		if !validMarkerValue(f.val) {
			return fmt.Errorf("marker %s value contains a forbidden character (control character or HTML-comment delimiter)", f.name)
		}
	}
	return nil
}

// sanitizeMarkerValue neutralizes anything in a marker value that could forge a
// marker line or close the HTML comment. A value that passes validMarkerValue is
// returned unchanged; a hostile control character or comment delimiter is
// replaced with the Unicode replacement character. It is defense in depth for
// any path that reaches serializeMarker without the boundary check.
func sanitizeMarkerValue(v string) string {
	v = markerControl.ReplaceAllString(v, "�")
	v = strings.ReplaceAll(v, "-->", "--�")
	v = strings.ReplaceAll(v, "<!--", "�!--")
	return v
}

// parseMarker extracts the oiax marker from a request body. It returns
// (marker, true) when an HTML comment containing an `oiax:` key is
// present and parses; version and branch-relationship validation are the
// caller's job. Prose that merely mentions oiax outside an HTML comment
// is never mistaken for a marker.
func parseMarker(body string) (marker, bool) {
	_, _, inner, ok := markerBlockBounds(body)
	if !ok {
		return marker{}, false
	}
	return parseInner(inner)
}

// replaceMarker rewrites the marker block in body with a freshly
// serialized marker, leaving the human text before and after it intact.
// It returns ("", false) when body carries no marker block.
func replaceMarker(body string, m marker) (string, bool) {
	start, end, _, ok := markerBlockBounds(body)
	if !ok {
		return "", false
	}
	return body[:start] + serializeMarker(m) + body[end:], true
}

// markerBlockBounds locates the first HTML comment whose content carries
// an `oiax:` key and returns its byte bounds ([start,end) over body) and
// inner text. Scanning comment-by-comment keeps a stray "oiax:" in prose
// from being read as a marker.
func markerBlockBounds(body string) (start, end int, inner string, ok bool) {
	const openTok, closeTok = "<!--", "-->"
	from := 0
	for {
		rel := strings.Index(body[from:], openTok)
		if rel < 0 {
			return 0, 0, "", false
		}
		open := from + rel
		crel := strings.Index(body[open:], closeTok)
		if crel < 0 {
			return 0, 0, "", false
		}
		innerStart := open + len(openTok)
		innerEnd := open + crel
		closeEnd := innerEnd + len(closeTok)
		candidate := body[innerStart:innerEnd]
		if hasOiaxKey(candidate) {
			return open, closeEnd, candidate, true
		}
		from = closeEnd
	}
}

// hasOiaxKey reports whether inner has a line that is exactly `oiax:`
// after trimming, i.e. the marker's top-level key.
func hasOiaxKey(inner string) bool {
	for _, line := range strings.Split(inner, "\n") {
		if strings.TrimSpace(line) == "oiax:" {
			return true
		}
	}
	return false
}

// parseInner reads the key/value lines of a marker block. It tolerates
// the nested indentation by matching known keys anywhere in the block;
// the fields are uniquely named so nesting depth carries no meaning.
func parseInner(inner string) (marker, bool) {
	var m marker
	foundOiax := false
	for _, line := range strings.Split(inner, "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "oiax":
			foundOiax = true
		case "version":
			m.Version = val
		case "graph":
			m.Graph = val
		case "type":
			m.Type = val
		case "source":
			m.Source = val
		case "destination":
			m.Destination = val
		case "sourceHead":
			m.SourceHead = val
		}
	}
	return m, foundOiax
}

// typeLabel maps a request type to its forge label.
func typeLabel(t engine.RequestType) string {
	if t == engine.RequestTypeBackflow {
		return LabelBackflow
	}
	return LabelPromotion
}

// markerVersionNum returns the integer N of a well-formed "vN" marker
// version. ok is false for any token markerVersionPattern rejects.
func markerVersionNum(v string) (n int, ok bool) {
	if !markerVersionPattern.MatchString(v) {
		return 0, false
	}
	n, err := strconv.Atoi(v[1:])
	if err != nil {
		return 0, false
	}
	return n, true
}
