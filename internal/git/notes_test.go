package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/skaphos/oiax/v2/internal/git"
)

func TestNotificationNotesExpectedTip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, dir := newRepo(t)
	anchor := writeCommit(t, dir, "config", "graph", "config")
	remote := t.TempDir()
	runGit(t, remote, "init", "--bare", "-q")
	runGit(t, dir, "push", remote, "HEAD:refs/heads/main")
	key := strings.Repeat("a", 64)
	open := func() *git.NotificationNotes {
		t.Helper()
		n, err := git.OpenNotificationNotes(ctx, git.NotesOptions{Remote: remote, GraphKey: key})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := n.Close(); err != nil {
				t.Error(err)
			}
		})
		return n
	}
	a, b := open(), open()
	if _, err := a.Read(ctx); !errors.Is(err, git.ErrNotesAbsent) {
		t.Fatal("absence not distinct", err)
	}
	first, err := a.Write(ctx, "", anchor, []byte(`{"version":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write(ctx, "", anchor, []byte(`{"version":2}`)); !errors.Is(err, git.ErrNotesConflict) {
		t.Fatal("concurrent creation did not conflict", err)
	}
	read, err := b.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if read.Tip != first || read.AnchorOID != anchor || string(read.Data) != `{"version":1}` {
		t.Fatalf("wrong note: %+v", read)
	}
	second, err := b.Write(ctx, first, anchor, []byte(`{"version":1,"next":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Write(ctx, first, anchor, []byte(`{}`)); !errors.Is(err, git.ErrNotesConflict) {
		t.Fatal("stale update did not conflict", err)
	}
	if parents := runGit(t, remote, "show", "-s", "--format=%P", second); parents != first {
		t.Fatalf("not sole-parent append: %q", parents)
	}
	if runGit(t, remote, "rev-parse", "refs/heads/main") != anchor {
		t.Fatal("branch changed")
	}
	if got := runGit(t, remote, "notes", "--ref=refs/notes/oiax/notifications/v1/"+key, "show", anchor); got != `{"version":1,"next":true}` {
		t.Fatal("not standard notes", got)
	}
	if runGit(t, dir, "status", "--porcelain") != "" {
		t.Fatal("caller worktree touched")
	}
	otherAnchor := writeCommit(t, dir, "config", "new graph", "new config")
	if _, err := a.Write(ctx, second, otherAnchor, []byte(`{}`)); !errors.Is(err, git.ErrNotesInvalid) {
		t.Fatal("anchor changed", err)
	}
	if _, err := a.Write(ctx, "HEAD", anchor, []byte(`{}`)); !errors.Is(err, git.ErrNotesInvalid) {
		t.Fatal("symbolic expected tip accepted", err)
	}
	if _, err := a.Write(ctx, second, "-bad", []byte(`{}`)); !errors.Is(err, git.ErrNotesInvalid) {
		t.Fatal("invalid anchor accepted", err)
	}
	if _, err := a.Write(ctx, second, anchor, make([]byte, (8<<20)+1)); !errors.Is(err, git.ErrNotesCapacity) {
		t.Fatal("unbounded write", err)
	}
}

func TestNotificationNotesNamespaceAndUnavailable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, key := range []string{"", "main", "refs/heads/oiax/test", "refs/notes/other", "../main", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		if n, err := git.OpenNotificationNotes(ctx, git.NotesOptions{Remote: t.TempDir(), GraphKey: key}); err == nil {
			_ = n.Close()
			t.Fatal("invalid key accepted", key)
		}
	}
	for _, remote := range []string{"https://user:secret@example.invalid/repo", "https://example.invalid/repo?token=secret", "https://example.invalid/repo#fragment", "ext::command", "http://example.invalid/repo"} {
		if n, err := git.OpenNotificationNotes(ctx, git.NotesOptions{Remote: remote, GraphKey: strings.Repeat("a", 64)}); err == nil {
			_ = n.Close()
			t.Fatal("unsafe remote accepted")
		}
	}
	if n, err := git.OpenNotificationNotes(ctx, git.NotesOptions{Remote: t.TempDir(), GraphKey: strings.Repeat("a", 64), Env: []string{"GIT_DIR=/outside"}}); err == nil {
		_ = n.Close()
		t.Fatal("environment redirected isolated repository")
	}
	n, err := git.OpenNotificationNotes(ctx, git.NotesOptions{Remote: t.TempDir(), GraphKey: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	if _, err := n.Read(ctx); err == nil || errors.Is(err, git.ErrNotesAbsent) {
		t.Fatal("unavailable remote mistaken for absence", err)
	}
}

func TestNotificationNotesConcurrentWritersAndBoundedReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, dir := newRepo(t)
	anchor := writeCommit(t, dir, "config", "graph", "config")
	remote := t.TempDir()
	runGit(t, remote, "init", "--bare", "-q")
	runGit(t, dir, "push", remote, "HEAD:refs/heads/main")
	key := strings.Repeat("b", 64)
	ref := "refs/notes/oiax/notifications/v1/" + key
	var writers []*git.NotificationNotes
	for range 2 {
		n, err := git.OpenNotificationNotes(ctx, git.NotesOptions{Remote: remote, GraphKey: key})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = n.Close() })
		writers = append(writers, n)
	}
	start := make(chan struct{})
	outcomes := make(chan error, 2)
	var wg sync.WaitGroup
	for i, n := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := n.Write(ctx, "", anchor, []byte(strings.Repeat("x", i+1)))
			outcomes <- err
		}()
	}
	close(start)
	wg.Wait()
	close(outcomes)
	success, conflict := 0, 0
	for err := range outcomes {
		if err == nil {
			success++
		} else if errors.Is(err, git.ErrNotesConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("winners=%d conflicts=%d", success, conflict)
	}
	// Replace only the disposable fixture's note with oversized data through
	// ordinary Git, proving the production reader refuses before buffering it.
	large := filepath.Join(t.TempDir(), "large-note")
	if err := os.WriteFile(large, []byte(strings.Repeat("x", (8<<20)+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, remote, "notes", "--ref="+ref, "add", "-f", "-F", large, anchor)
	if _, err := writers[0].Read(ctx); !errors.Is(err, git.ErrNotesCapacity) {
		t.Fatal("oversized read accepted", err)
	}
}

func TestNotificationNotesRejectsMergeAncestry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, dir := newRepo(t)
	anchor := writeCommit(t, dir, "config", "graph", "config")
	remote := t.TempDir()
	runGit(t, remote, "init", "--bare", "-q")
	runGit(t, dir, "push", remote, "HEAD:refs/heads/main")
	key := strings.Repeat("c", 64)
	ref := "refs/notes/oiax/notifications/v1/" + key
	n, err := git.OpenNotificationNotes(ctx, git.NotesOptions{Remote: remote, GraphKey: key})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	tip, err := n.Write(ctx, "", anchor, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	tree := runGit(t, remote, "rev-parse", tip+"^{tree}")
	merge := runGit(t, remote, "commit-tree", tree, "-p", tip, "-p", anchor, "-m", "malformed fixture")
	runGit(t, remote, "update-ref", ref, merge, tip)
	if _, err := n.Read(ctx); !errors.Is(err, git.ErrNotesInvalid) {
		t.Fatal("multiple parents accepted", err)
	}
}
