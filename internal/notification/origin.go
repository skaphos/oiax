package notification

import (
	"unicode"
	"unicode/utf8"
)

// ValidOrigin validates immutable creation evidence, not request ownership.
// Providers must separately verify their existing managed-request marker and
// actual repository/refs; event admission additionally checks current topology.
func ValidOrigin(o NotificationOriginV1) bool {
	text := func(s string, limit int) bool {
		if s == "" || len(s) > limit || !utf8.ValidString(s) {
			return false
		}
		for _, r := range s {
			if unicode.IsControl(r) {
				return false
			}
		}
		return true
	}
	return o.Version == 1 && text(o.OperationID, 128) && text(o.Graph, 1024) &&
		text(o.LogicalSource, 1024) && text(o.LogicalTarget, 1024) &&
		ValidOID(o.ConfigOID) && ValidOID(o.SourceOID) && ValidOID(o.BaseOID) && !o.ObservedAt.IsZero()
}
