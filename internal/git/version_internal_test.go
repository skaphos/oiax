package git

import (
	"strings"
	"testing"
)

// TestParseGitVersion covers the version-string parser, including the vendor
// suffixes real-world git executables emit (Apple, Windows) that a naive
// "strip the third dotted component" parser would mishandle.
func TestParseGitVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		in           string
		major, minor int
		ok           bool
	}{
		{"canonical", "git version 2.45.1", 2, 45, true},
		{"apple suffix", "git version 2.39.5 (Apple Git-154)", 2, 39, true},
		{"windows suffix", "git version 2.40.0.windows.1", 2, 40, true},
		{"two component", "git version 2.45", 2, 45, true},
		{"future major", "git version 3.0.0", 3, 0, true},
		{"no version number", "git version unknown", 0, 0, false},
		{"empty", "", 0, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			major, minor, ok := parseGitVersion(tc.in)
			if ok != tc.ok {
				t.Fatalf("parseGitVersion(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			}
			if ok && (major != tc.major || minor != tc.minor) {
				t.Fatalf("parseGitVersion(%q) = %d.%d, want %d.%d", tc.in, major, minor, tc.major, tc.minor)
			}
		})
	}
}

// TestCheckMinVersion is the floor guard: versions at or above 2.45 are
// accepted, versions below it (Ubuntu 22.04's 2.34, Debian bookworm's 2.39,
// older major series) are rejected with an error naming the floor and the
// detected version, and vendor suffixes are handled on both sides of the line.
func TestCheckMinVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"at the floor", "git version 2.45.0", false},
		{"above by minor", "git version 2.46.1", false},
		{"above by major", "git version 3.0.0", false},
		{"apple above floor", "git version 2.45.5 (Apple Git-160)", false},
		{"windows above floor", "git version 2.47.0.windows.1", false},
		{"below by minor", "git version 2.39.5", true},
		{"ubuntu 22.04 below floor", "git version 2.34.1", true},
		{"below by major", "git version 1.9.0", true},
		{"apple below floor", "git version 2.39.5 (Apple Git-154)", true},
		{"unparseable", "git version banana", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := checkMinVersion(tc.in)
			if tc.wantErr && err == nil {
				t.Fatalf("checkMinVersion(%q) = nil, want a below-floor error", tc.in)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("checkMinVersion(%q) = %v, want nil", tc.in, err)
			}
			// A rejection of a parseable-but-too-old version must name both the
			// required floor and the detected version so the operator can act.
			if tc.wantErr && err != nil && tc.in != "git version banana" {
				if !strings.Contains(err.Error(), "2.45") {
					t.Errorf("error %q does not name the required floor 2.45", err)
				}
				if !strings.Contains(err.Error(), tc.in) {
					t.Errorf("error %q does not name the detected version %q", err, tc.in)
				}
			}
		})
	}
}
