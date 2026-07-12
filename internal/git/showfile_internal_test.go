package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/gittest"
)

// TestShowFileRejectsOversizedBlob proves ShowFile caps the bytes it
// buffers from `git show`, so a pathological blob committed at the pinned
// configuration ref cannot exhaust memory before config.Parse's own size
// check ever runs (the pinned-ref read goes straight from ShowFile to
// config.Parse and never through config.Load's pre-read cap).
func TestShowFileRejectsOversizedBlob(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable not available")
	}

	dir := t.TempDir()
	gittest.InitRepo(t, dir)

	huge := bytes.Repeat([]byte("a"), maxShowFileSize+1)
	if err := os.WriteFile(filepath.Join(dir, "huge.yaml"), huge, 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", "huge.yaml")
	gittest.Run(t, dir, "commit", "-q", "-m", "huge")

	r := &Runner{Dir: dir}
	_, err := r.ShowFile(context.Background(), "main", "huge.yaml")
	if err == nil {
		t.Fatal("ShowFile succeeded on an oversized blob, want a size-limit error")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error = %v, want it to explain the size limit", err)
	}
}
