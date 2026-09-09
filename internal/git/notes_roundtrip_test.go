package git_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/skaphos/oiax/v2/internal/git"
)

// gitInvocationLog records every git subcommand the notes writer runs by
// putting a logging shim ahead of the real executable on PATH. It observes
// network-shaped work (ls-remote, fetch, push) without touching production code.
type gitInvocationLog struct {
	t    *testing.T
	path string
}

func newGitInvocationLog(t *testing.T) *gitInvocationLog {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell shim requires a POSIX shell")
	}
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git executable not available")
	}
	shimDir := t.TempDir()
	log := filepath.Join(shimDir, "invocations.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$OIAX_TEST_GIT_LOG\"\nexec \"" + real + "\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(shimDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// Not parallel: PATH is process-wide. Only GIT_* variables are filtered
	// out of the notes writer's environment, so the log path passes through.
	t.Setenv("OIAX_TEST_GIT_LOG", log)
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return &gitInvocationLog{t: t, path: log}
}

// counts returns subcommand counts since the previous call and resets the log.
func (l *gitInvocationLog) counts() map[string]int {
	l.t.Helper()
	data, err := os.ReadFile(l.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		l.t.Fatal(err)
	}
	if err := os.WriteFile(l.path, nil, 0o600); err != nil {
		l.t.Fatal(err)
	}
	counts := map[string]int{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		for i := 0; i < len(fields); i++ {
			if fields[i] == "-c" {
				i++
				continue
			}
			counts[fields[i]]++
			break
		}
	}
	return counts
}

func TestNotificationNotesSkipsRedundantFetchesAndPreReads(t *testing.T) {
	ctx := context.Background()
	_, dir := newRepo(t)
	anchor := writeCommit(t, dir, "config", "graph", "config")
	remote := t.TempDir()
	runGit(t, remote, "init", "--bare", "-q")
	runGit(t, dir, "push", remote, "HEAD:refs/heads/main")
	key := strings.Repeat("d", 64)
	invocations := newGitInvocationLog(t)
	open := func() *git.NotificationNotes {
		t.Helper()
		n, err := git.OpenNotificationNotes(ctx, git.NotesOptions{Remote: remote, GraphKey: key})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = n.Close() })
		return n
	}
	a, b := open(), open()
	invocations.counts()

	if _, err := a.Read(ctx); !errors.Is(err, git.ErrNotesAbsent) {
		t.Fatal(err)
	}
	if got := invocations.counts(); got["ls-remote"] != 1 || got["fetch"] != 0 {
		t.Fatalf("absent read = %v", got)
	}

	// Initialization must advertise, verify the anchor and push once.
	first, err := a.Write(ctx, "", anchor, []byte(`{"version":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := invocations.counts(); got["ls-remote"] != 1 || got["fetch"] != 1 || got["push"] != 1 {
		t.Fatalf("initial write = %v", got)
	}

	// Appending to the tip this writer just pushed needs no advertisement or
	// fetch: the push lease alone is the compare-and-swap.
	second, err := a.Write(ctx, first, anchor, []byte(`{"version":1,"n":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := invocations.counts(); got["ls-remote"] != 0 || got["fetch"] != 0 || got["push"] != 1 {
		t.Fatalf("append to own tip = %v", got)
	}

	// Re-reading an unchanged tip advertises but does not fetch.
	read, err := a.Read(ctx)
	if err != nil || read.Tip != second || string(read.Data) != `{"version":1,"n":2}` {
		t.Fatalf("read = %+v, %v", read, err)
	}
	if got := invocations.counts(); got["ls-remote"] != 1 || got["fetch"] != 0 {
		t.Fatalf("unchanged read = %v", got)
	}

	// Another writer advances the ref; it has never fetched, so it must.
	third, err := b.Write(ctx, second, anchor, []byte(`{"version":1,"n":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := invocations.counts(); got["ls-remote"] != 1 || got["fetch"] != 1 || got["push"] != 1 {
		t.Fatalf("foreign append = %v", got)
	}

	// The first writer's cached tip is stale: the lease rejects the push and
	// one re-advertisement (with a fetch) classifies it as a conflict.
	if _, err := a.Write(ctx, second, anchor, []byte(`{"version":1,"n":4}`)); !errors.Is(err, git.ErrNotesConflict) {
		t.Fatalf("stale append = %v", err)
	}
	if got := invocations.counts(); got["ls-remote"] != 1 || got["fetch"] != 1 || got["push"] != 1 {
		t.Fatalf("rejected append = %v", got)
	}
	if parents := runGit(t, remote, "show", "-s", "--format=%P", third); parents != second {
		t.Fatalf("history rewritten: %q", parents)
	}
	invocations.counts()

	// After the conflict re-read the moved tip is known: a fresh append
	// re-advertises without fetching, then pushes.
	fourth, err := a.Write(ctx, third, anchor, []byte(`{"version":1,"n":5}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := invocations.counts(); got["ls-remote"] != 1 || got["fetch"] != 0 || got["push"] != 1 {
		t.Fatalf("append after conflict = %v", got)
	}
	if parents := runGit(t, remote, "show", "-s", "--format=%P", fourth); parents != third {
		t.Fatalf("not sole-parent append: %q", parents)
	}
	if got := runGit(t, remote, "notes", "--ref=refs/notes/oiax/notifications/v1/"+key, "show", anchor); got != `{"version":1,"n":5}` {
		t.Fatal("not standard notes", got)
	}
	// A different anchor never rides the cached tip; it is re-advertised and
	// refused exactly as before.
	otherAnchor := writeCommit(t, dir, "config", "new graph", "new config")
	invocations.counts()
	if _, err := a.Write(ctx, fourth, otherAnchor, []byte(`{}`)); !errors.Is(err, git.ErrNotesInvalid) {
		t.Fatal("anchor changed", err)
	}
	if got := invocations.counts(); got["ls-remote"] != 1 || got["push"] != 0 {
		t.Fatalf("anchor change = %v", got)
	}
}
