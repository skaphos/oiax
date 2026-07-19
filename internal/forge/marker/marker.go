// Package marker owns the machine-readable Oiax metadata block a managed
// change request carries in its body, and the forge-neutral labels that
// accompany it. The marker is load-bearing for managed-request identity:
// it is how Oiax recognizes its own requests across runs without a private
// state database, so its serialized format is a compatibility contract
// (see docs/architecture.md "Managed change requests").
//
// The block is pure body text — it manipulates strings only and reads no
// forge DTOs — so every provider (GitHub, Azure DevOps, and any future one)
// shares a single implementation of it, and in particular a single copy of
// the injection defenses (validate/sanitize). Provider packages layer their
// own DTO-reading recognizers (managedMarker/conflictMarker) on top.
package marker

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/skaphos/oiax/internal/engine"
)

// Managed requests carry these labels: LabelOiax on every one, plus exactly
// one of the type labels. The type labels also encode which
// promotion/backflow request this is, redundantly with the marker, so a
// human scanning the forge UI can tell them apart.
const (
	LabelOiax      = "oiax"
	LabelPromotion = "oiax/promotion"
	LabelBackflow  = "oiax/backflow"
	// LabelConflict marks a durable backflow-conflict artifact (a forge
	// issue or work item). Unlike the decorative label on a managed PR, this
	// label is load-bearing for artifact identity: an artifact has no
	// head/base+same-repo provenance, so the label is the authorization
	// substitute — only a collaborator can label an in-repo artifact.
	LabelConflict = "oiax/conflict"
)

// ConflictType is the marker type token a durable conflict artifact carries.
// It is deliberately NOT an engine.RequestType, so it can never leak into
// RequestFilter change-request discovery.
const ConflictType = "conflict"

// Version is the marker schema version this build writes and fully
// understands. Callers stamp it into Marker.Version. Readers are
// deliberately more lenient than writers: recognition accepts any
// well-formed version (VersionPattern), so a request written by a newer
// release is still identified as oiax's own and never duplicated.
const Version = "v1"

// VersionPattern matches a well-formed marker version token: a "v" followed
// by one or more digits ("v1", "v2", ...). Recognition accepts any version
// of this shape; the numeric value decides only whether this build
// understands the schema well enough to mutate the request (VersionNum).
var VersionPattern = regexp.MustCompile(`^v[0-9]+$`)

// Marker is the parsed content of the machine-readable oiax block a managed
// request carries in its body. The field named destination in the marker
// maps to engine.ChangeRequest.Target.
type Marker struct {
	Version     string
	Graph       string
	Type        string
	Source      string
	Destination string
	SourceHead  string
}

// Serialize renders the HTML-comment marker verbatim in the frozen format
// (two-space YAML indent, version first, destination not target). It is the
// inverse of Parse for the fields it carries.
//
// The marker is load-bearing for managed-request identity, so every value is
// passed through Sanitize: a newline or "-->" in a value must never be able
// to forge an extra marker line or close the HTML comment early. Values oiax
// actually writes (branch and graph names, hex heads, the version and type
// tokens) contain none of those, so this is a no-op for them; callers should
// also reject hostile values up front with Validate.
func Serialize(m Marker) string {
	var b strings.Builder
	b.WriteString("<!--\n")
	b.WriteString("oiax:\n")
	b.WriteString("  version: " + Sanitize(m.Version) + "\n")
	b.WriteString("  graph: " + Sanitize(m.Graph) + "\n")
	b.WriteString("  type: " + Sanitize(m.Type) + "\n")
	b.WriteString("  source: " + Sanitize(m.Source) + "\n")
	b.WriteString("  destination: " + Sanitize(m.Destination) + "\n")
	b.WriteString("  sourceHead: " + Sanitize(m.SourceHead) + "\n")
	b.WriteString("-->")
	return b.String()
}

// markerControl matches any ASCII control character (newline, carriage return,
// tab, and the rest of C0 plus DEL). A marker value oiax writes never contains
// one; a value that does is treated as hostile.
var markerControl = regexp.MustCompile(`[\x00-\x1f\x7f]`)

// ValidValue reports whether v is safe to place in a marker line verbatim: no
// control character, and nothing that could open or close the surrounding HTML
// comment. It is the predicate Validate enforces at the boundary before a
// marker is written to the forge.
func ValidValue(v string) bool {
	if markerControl.MatchString(v) {
		return false
	}
	return !strings.Contains(v, "-->") && !strings.Contains(v, "<!--")
}

// Validate rejects a marker any of whose fields could forge marker lines or
// break out of the HTML comment. Providers call it before serializing so a
// hostile graph name, branch name, or head is refused with a clear error
// rather than silently defanged.
func Validate(m Marker) error {
	for _, f := range []struct{ name, val string }{
		{"version", m.Version},
		{"graph", m.Graph},
		{"type", m.Type},
		{"source", m.Source},
		{"destination", m.Destination},
		{"sourceHead", m.SourceHead},
	} {
		if !ValidValue(f.val) {
			return fmt.Errorf("marker %s value contains a forbidden character (control character or HTML-comment delimiter)", f.name)
		}
	}
	return nil
}

// Sanitize neutralizes anything in a marker value that could forge a marker
// line or close the HTML comment. A value that passes ValidValue is returned
// unchanged; a hostile control character or comment delimiter is replaced with
// the Unicode replacement character. It is defense in depth for any path that
// reaches Serialize without the boundary check.
func Sanitize(v string) string {
	v = markerControl.ReplaceAllString(v, "�")
	v = strings.ReplaceAll(v, "-->", "--�")
	v = strings.ReplaceAll(v, "<!--", "�!--")
	return v
}

// Parse extracts the oiax marker from a request body. It returns
// (marker, true) when an HTML comment containing an `oiax:` key is present and
// parses; version and branch-relationship validation are the caller's job.
// Prose that merely mentions oiax outside an HTML comment is never mistaken
// for a marker.
func Parse(body string) (Marker, bool) {
	_, _, inner, ok := blockBounds(body)
	if !ok {
		return Marker{}, false
	}
	return parseInner(inner)
}

// Replace rewrites the marker block in body with a freshly serialized marker,
// leaving the human text before and after it intact. It returns ("", false)
// when body carries no marker block.
func Replace(body string, m Marker) (string, bool) {
	start, end, _, ok := blockBounds(body)
	if !ok {
		return "", false
	}
	return body[:start] + Serialize(m) + body[end:], true
}

// blockBounds locates the first HTML comment whose content carries an `oiax:`
// key and returns its byte bounds ([start,end) over body) and inner text.
// Scanning comment-by-comment keeps a stray "oiax:" in prose from being read
// as a marker.
func blockBounds(body string) (start, end int, inner string, ok bool) {
	const openTok, closeTok = "<!--", "-->"
	from := 0
	for {
		rel := strings.Index(body[from:], openTok)
		if rel < 0 {
			return 0, 0, "", false
		}
		open := from + rel
		innerStart := open + len(openTok)
		// Search for the closer from the end of the opener, never from the
		// opener itself: in "<!-->" the first "-->" overlaps the "<!--", and
		// bounding on it would invert the inner range.
		crel := strings.Index(body[innerStart:], closeTok)
		if crel < 0 {
			return 0, 0, "", false
		}
		innerEnd := innerStart + crel
		closeEnd := innerEnd + len(closeTok)
		candidate := body[innerStart:innerEnd]
		if hasOiaxKey(candidate) {
			return open, closeEnd, candidate, true
		}
		from = closeEnd
	}
}

// hasOiaxKey reports whether inner has a line that is exactly `oiax:` after
// trimming, i.e. the marker's top-level key.
func hasOiaxKey(inner string) bool {
	for _, line := range strings.Split(inner, "\n") {
		if strings.TrimSpace(line) == "oiax:" {
			return true
		}
	}
	return false
}

// parseInner reads the key/value lines of a marker block. It tolerates the
// nested indentation by matching known keys anywhere in the block; the fields
// are uniquely named so nesting depth carries no meaning.
func parseInner(inner string) (Marker, bool) {
	var m Marker
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

// TypeLabel maps a request type to its forge label.
func TypeLabel(t engine.RequestType) string {
	if t == engine.RequestTypeBackflow {
		return LabelBackflow
	}
	return LabelPromotion
}

// VersionNum returns the integer N of a well-formed "vN" marker version. ok is
// false for any token VersionPattern rejects.
func VersionNum(v string) (n int, ok bool) {
	if !VersionPattern.MatchString(v) {
		return 0, false
	}
	n, err := strconv.Atoi(v[1:])
	if err != nil {
		return 0, false
	}
	return n, true
}
