// Package version carries build-time version metadata, injected via
// -ldflags by GoReleaser (see .goreleaser.yaml).
package version

var (
	// Version is the semantic version of the build ("dev" for local builds).
	Version = "dev"
	// Commit is the git SHA the binary was built from.
	Commit = "none"
	// Date is the RFC 3339 build timestamp.
	Date = "unknown"
)
