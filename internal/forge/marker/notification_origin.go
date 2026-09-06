package marker

import (
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/skaphos/oiax/v2/internal/notification"
)

const notificationOriginKey = "oiax-notification-origin"
const maxNotificationOriginBytes = 4 << 10

// originBlock recognizes the reserved comment independently of valid JSON so
// duplicates, including malformed or unclosed copies, cannot be overlooked.
func originBlock(body string) (string, int, bool) {
	var block string
	count := 0
	for {
		_, rest, found := strings.Cut(body, "<!--")
		if !found {
			return block, count, true
		}
		inner, tail, closed := strings.Cut(rest, "-->")
		if strings.HasPrefix(strings.TrimSpace(inner), notificationOriginKey) {
			count++
			if !closed || count > 1 || len(inner)+len("<!---->") > maxNotificationOriginBytes {
				return "", count, false
			}
			block = strings.TrimSpace(inner)
		}
		if !closed {
			return block, count, false
		}
		body = tail
	}
}

// AppendNotificationOrigin keeps the existing body byte-for-byte and appends a
// non-templatable, HTML-safe block. Even with nil origin it rejects reserved
// comments supplied by a template. No origin ever grants ownership by itself.
func AppendNotificationOrigin(body string, origin *notification.NotificationOriginV1) (string, error) {
	_, count, complete := originBlock(body)
	if count != 0 || origin != nil && !complete {
		return "", notification.ErrInvalidState
	}
	if origin == nil {
		return body, nil
	}
	if !notification.ValidOrigin(*origin) {
		return "", notification.ErrInvalidState
	}
	data, err := json.Marshal(origin) // default HTML escaping is intentional
	if err != nil {
		return "", notification.ErrInvalidState
	}
	block := "<!-- " + notificationOriginKey + ":" + string(data) + " -->"
	if len(block) > maxNotificationOriginBytes {
		return "", notification.ErrCapacity
	}
	return body + "\n\n" + block, nil
}

// ParseNotificationOrigin accepts one closed, bounded block with exact field
// names. Decode each field independently to reject duplicate keys, case aliases,
// unknown fields and trailing JSON instead of encoding/json's last-key-wins.
func ParseNotificationOrigin(body string) (notification.NotificationOriginV1, bool) {
	var origin notification.NotificationOriginV1
	block, count, complete := originBlock(body)
	if !complete || count != 1 || !utf8.ValidString(block) {
		return origin, false
	}
	data, ok := strings.CutPrefix(block, notificationOriginKey+":")
	if !ok {
		return origin, false
	}
	d := json.NewDecoder(strings.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return origin, false
	}
	fields := map[string]any{
		"version": &origin.Version, "operationID": &origin.OperationID, "graph": &origin.Graph,
		"configOID": &origin.ConfigOID, "observedAt": &origin.ObservedAt,
		"logicalSource": &origin.LogicalSource, "logicalTarget": &origin.LogicalTarget,
		"sourceOID": &origin.SourceOID, "baseOID": &origin.BaseOID,
	}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return origin, false
		}
		name, ok := key.(string)
		if !ok || fields[name] == nil || d.Decode(fields[name]) != nil {
			return origin, false
		}
		delete(fields, name)
	}
	if _, err := d.Token(); err != nil || len(fields) != 0 || !notification.ValidOrigin(origin) {
		return origin, false
	}
	if _, err := d.Token(); err != io.EOF {
		return origin, false
	}
	// Reject inputs whose decoded form cannot be represented inside the same
	// bound (e.g. many raw '<' characters requiring HTML escaping on write).
	if _, err := AppendNotificationOrigin("", &origin); err != nil {
		return origin, false
	}
	return origin, true
}

// NotificationOriginMatches requires already established ownership and checks
// only origin/marker consistency. Backflow's logical source is deliberately not
// inferred from its actual candidate branch; topology validation comes later.
func NotificationOriginMatches(origin notification.NotificationOriginV1, m Marker) bool {
	if !notification.ValidOrigin(origin) || !VersionPattern.MatchString(m.Version) || Validate(m) != nil ||
		m.Graph != origin.Graph || m.Destination != origin.LogicalTarget || m.Source == "" {
		return false
	}
	return m.Type == "promotion" && m.Source == origin.LogicalSource || m.Type == "backflow"
}
