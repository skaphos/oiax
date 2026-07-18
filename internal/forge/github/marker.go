package github

import (
	mk "github.com/skaphos/oiax/internal/forge/marker"
)

// The managed-request marker and its forge-neutral labels live in the shared
// internal/forge/marker package so every provider serializes identity — and
// its injection defenses — from one implementation. These aliases keep the
// GitHub provider's existing unqualified spellings (marker{...},
// serializeMarker, LabelOiax, ...) pointing at the shared code, so github.go
// and its tests read unchanged. The DTO-reading recognizers managedMarker and
// conflictMarker, which must inspect GitHub pull/issue payloads, stay in
// github.go.

// marker is the shared marker record; the GitHub provider builds and reads it
// with the unqualified name.
type marker = mk.Marker

const (
	LabelOiax      = mk.LabelOiax
	LabelPromotion = mk.LabelPromotion
	LabelBackflow  = mk.LabelBackflow
	LabelConflict  = mk.LabelConflict

	markerVersion      = mk.Version
	conflictMarkerType = mk.ConflictType
)

var (
	serializeMarker = mk.Serialize
	parseMarker     = mk.Parse
	replaceMarker   = mk.Replace
	validateMarker  = mk.Validate

	markerVersionPattern = mk.VersionPattern
	markerVersionNum     = mk.VersionNum

	typeLabel = mk.TypeLabel
)
