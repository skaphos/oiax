// Package gittest is the hermetic git test harness shared by every package
// that drives real git subprocesses in tests: internal/git, internal/cli,
// internal/reconcile, and internal/forge/github. "Hermetic" means the host's
// global and system git configuration is neutralized so a suite cannot flake
// on a developer's or CI runner's own gpgsign, core.hooksPath, core.autocrlf,
// or credential-helper settings — Windows runners in particular default
// core.autocrlf=true, which rewrites LF->CRLF on checkout and breaks
// byte-for-byte content assertions and `git status --porcelain` cleanliness
// checks.
package gittest

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Env returns the current process environment plus overrides that disable
// the host's global and system git configuration (GIT_CONFIG_GLOBAL,
// GIT_CONFIG_SYSTEM, GIT_CONFIG_NOSYSTEM) and pin a deterministic
// author/committer identity and timestamp, so commits made by tests succeed
// and reproduce regardless of the developer's own git configuration.
func Env() []string {
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
	)
}

// Run runs a git command in dir (empty means the current directory) with the
// hermetic Env, failing t on error and returning trimmed combined output.
func Run(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = Env()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// InitRepo initializes an empty repository at dir on branch main with
// hermetic local config on top of Env's global/system neutralization:
//   - a fixed identity, belt-and-suspenders alongside the
//     GIT_AUTHOR_*/GIT_COMMITTER_* env vars, since production code paths
//     that build their own *exec.Cmd (rather than going through Run) don't
//     inherit them;
//   - core.autocrlf=false, so checked-out bytes are stable across platforms;
//   - commit.gpgsign=false, so commits don't require a signing key the test
//     environment doesn't have;
//   - core.hooksPath pointed at an empty directory, so no host-configured
//     hook can run during a test commit.
func InitRepo(t testing.TB, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create repo dir %s: %v", dir, err)
	}
	Run(t, dir, "init", "-q", "-b", "main")
	Run(t, dir, "config", "user.name", "Test")
	Run(t, dir, "config", "user.email", "test@example.com")
	Run(t, dir, "config", "core.autocrlf", "false")
	Run(t, dir, "config", "commit.gpgsign", "false")
	Run(t, dir, "config", "core.hooksPath", t.TempDir())
}
