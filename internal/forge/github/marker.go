package github

import (
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
)

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
func serializeMarker(m marker) string {
	var b strings.Builder
	b.WriteString("<!--\n")
	b.WriteString("oiax:\n")
	b.WriteString("  version: " + m.Version + "\n")
	b.WriteString("  graph: " + m.Graph + "\n")
	b.WriteString("  type: " + m.Type + "\n")
	b.WriteString("  source: " + m.Source + "\n")
	b.WriteString("  destination: " + m.Destination + "\n")
	b.WriteString("  sourceHead: " + m.SourceHead + "\n")
	b.WriteString("-->")
	return b.String()
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
