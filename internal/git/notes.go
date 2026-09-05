package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ErrNotesAbsent      = errors.New("notification notes absent")
	ErrNotesConflict    = errors.New("notification notes expected-tip conflict")
	ErrNotesInvalid     = errors.New("invalid notification notes state or target")
	ErrNotesUnavailable = errors.New("notification notes unavailable; verify remote access and notes permissions")
	ErrNotesCapacity    = errors.New("notification notes exceed capacity")
)

const maxNotesBytes = 8 << 20

var notesKeyPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var notesOIDPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// NotesOptions accepts a non-secret HTTPS remote or an absolute local bare
// repository path. Provider credentials travel only in Git's config environment,
// using the existing provider authentication helpers; never put them in Remote.
type NotesOptions struct {
	Remote, GraphKey string
	Env              []string
}

// NotificationNotes owns an isolated bare object database. There is no index,
// checkout, branch writer or arbitrary ref parameter. Close removes only the
// private temporary database allocated by OpenNotificationNotes.
type NotificationNotes struct {
	dir, remote, ref string
	env              []string
}

func OpenNotificationNotes(ctx context.Context, options NotesOptions) (*NotificationNotes, error) {
	if !notesKeyPattern.MatchString(options.GraphKey) || !validNotesRemote(options.Remote) || !validNotesAuthEnv(options.Env) {
		return nil, ErrNotesInvalid
	}
	dir, err := os.MkdirTemp("", "oiax-notifications-")
	if err != nil {
		return nil, ErrNotesUnavailable
	}
	n := &NotificationNotes{dir: dir, remote: options.Remote, ref: "refs/notes/oiax/notifications/v1/" + options.GraphKey, env: append([]string{}, options.Env...)}
	if _, err := n.run(ctx, nil, 1024, "init", "--bare", "--quiet", "--template=", "."); err != nil {
		_ = n.Close()
		return nil, err
	}
	if _, err := n.run(ctx, nil, 1024, "check-ref-format", n.ref); err != nil {
		_ = n.Close()
		return nil, ErrNotesInvalid
	}
	return n, nil
}

func validNotesRemote(remote string) bool {
	if remote == "" || strings.ContainsAny(remote, "\x00\r\n") {
		return false
	}
	if filepath.IsAbs(remote) {
		return true
	}
	u, err := url.Parse(remote)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == "" && u.RawQuery == ""
}

// Only the existing provider auth helper's one HTTP header is accepted. In
// particular, GIT_DIR/WORK_TREE/INDEX_FILE and executable Git config cannot
// redirect the isolated writer back into a caller-owned repository.
func validNotesAuthEnv(env []string) bool {
	if len(env) == 0 {
		return true
	}
	return len(env) == 3 && env[0] == "GIT_CONFIG_COUNT=1" && env[1] == "GIT_CONFIG_KEY_0=http.extraHeader" &&
		strings.HasPrefix(env[2], "GIT_CONFIG_VALUE_0=AUTHORIZATION: ") && !strings.ContainsAny(env[2], "\x00\r\n")
}

func (n *NotificationNotes) Close() error {
	if n.dir == "" {
		return nil
	}
	if err := os.RemoveAll(n.dir); err != nil {
		return ErrNotesUnavailable
	}
	n.dir = ""
	return nil
}

// run disables hooks, global/system config, signing, prompts, redirects and Git
// URL rewrite/config extensions in the private database. Only bounded stdout is
// retained. Neither command arguments nor stderr are returned in errors.
func (n *NotificationNotes) run(ctx context.Context, input []byte, limit int, args ...string) ([]byte, error) {
	if n.dir == "" {
		return nil, ErrNotesInvalid
	}
	base := []string{"-c", "core.hooksPath=" + os.DevNull, "-c", "commit.gpgSign=false", "-c", "http.followRedirects=false", "-c", "protocol.ext.allow=never", "-c", "protocol.file.allow=always", "-c", "gc.auto=0"}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	cmd.Dir = n.dir
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, n.env...)
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME=Oiax", "GIT_AUTHOR_EMAIL=oiax@skaphos.io", "GIT_COMMITTER_NAME=Oiax", "GIT_COMMITTER_EMAIL=oiax@skaphos.io")
	cmd.Stdin = bytes.NewReader(input)
	output := capWriter{limit: limit}
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrNotesUnavailable
	}
	return output.buf.Bytes(), nil
}

type NoteSnapshot struct {
	Tip, AnchorOID string
	Data           []byte
}

// Read distinguishes successful empty advertisement from denial/transport errors.
// Fetch writes only inside the private object database, never to the remote.
func (n *NotificationNotes) Read(ctx context.Context) (NoteSnapshot, error) {
	output, err := n.run(ctx, nil, 1024, "ls-remote", "--refs", "--", n.remote, n.ref)
	if err != nil {
		return NoteSnapshot{}, err
	}
	if len(bytes.TrimSpace(output)) == 0 {
		return NoteSnapshot{}, ErrNotesAbsent
	}
	fields := strings.Fields(string(output))
	if len(fields) != 2 || !notesOIDPattern.MatchString(fields[0]) || fields[1] != n.ref {
		return NoteSnapshot{}, ErrNotesInvalid
	}
	if _, err := n.run(ctx, nil, 1024, "fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--", n.remote, "+"+n.ref+":"+n.ref); err != nil {
		return NoteSnapshot{}, err
	}
	tipBytes, err := n.run(ctx, nil, 128, "rev-parse", "--verify", n.ref+"^{commit}")
	if err != nil {
		return NoteSnapshot{}, ErrNotesInvalid
	}
	tip := strings.TrimSpace(string(tipBytes))
	if !notesOIDPattern.MatchString(tip) {
		return NoteSnapshot{}, ErrNotesInvalid
	}
	return n.readTip(ctx, tip)
}

func (n *NotificationNotes) readTip(ctx context.Context, tip string) (NoteSnapshot, error) {
	parents, err := n.run(ctx, nil, 256, "show", "-s", "--format=%P", tip, "--")
	if err != nil || len(strings.Fields(string(parents))) > 1 {
		return NoteSnapshot{}, ErrNotesInvalid
	}
	// Standard notes permit fanout; our writer uses a flat single-anchor tree.
	entries, err := n.run(ctx, nil, 1024, "ls-tree", "-r", "--full-tree", tip, "--")
	if err != nil {
		return NoteSnapshot{}, err
	}
	fields := strings.Fields(string(entries))
	if len(fields) != 4 || fields[0] != "100644" || fields[1] != "blob" || !notesOIDPattern.MatchString(fields[2]) || !notesOIDPattern.MatchString(strings.ReplaceAll(fields[3], "/", "")) {
		return NoteSnapshot{}, ErrNotesInvalid
	}
	anchor := strings.ReplaceAll(fields[3], "/", "")
	sizeBytes, err := n.run(ctx, nil, 128, "cat-file", "-s", fields[2])
	if err != nil {
		return NoteSnapshot{}, err
	}
	var size int
	if _, err := fmt.Sscanf(string(sizeBytes), "%d", &size); err != nil || size < 0 {
		return NoteSnapshot{}, ErrNotesInvalid
	}
	if size > maxNotesBytes {
		return NoteSnapshot{}, ErrNotesCapacity
	}
	data, err := n.run(ctx, nil, maxNotesBytes, "cat-file", "blob", fields[2])
	if err != nil {
		return NoteSnapshot{}, err
	}
	return NoteSnapshot{Tip: tip, AnchorOID: anchor, Data: data}, nil
}

// Write creates a sole-parent child of expected (or a parentless initialization
// requiring absence) and pushes using the FULL explicit lease. Callers cannot
// supply a replacement commit, rewind, delete or choose another ref.
func (n *NotificationNotes) Write(ctx context.Context, expected, anchor string, data []byte) (string, error) {
	if (expected != "" && !notesOIDPattern.MatchString(expected)) || !notesOIDPattern.MatchString(anchor) {
		return "", ErrNotesInvalid
	}
	if len(data) > maxNotesBytes {
		return "", ErrNotesCapacity
	}
	current, err := n.Read(ctx)
	if err != nil && !errors.Is(err, ErrNotesAbsent) {
		return "", err
	}
	if current.Tip != expected {
		return "", ErrNotesConflict
	}
	if expected != "" && current.AnchorOID != anchor {
		return "", ErrNotesInvalid
	}
	if expected == "" {
		// Anchor must be a real reachable configuration commit, not a blob or
		// invented OID. The fetched object never becomes a branch in this cache.
		if _, err := n.run(ctx, nil, 1024, "fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--", n.remote, anchor); err != nil {
			return "", err
		}
		kind, err := n.run(ctx, nil, 32, "cat-file", "-t", anchor)
		if err != nil || strings.TrimSpace(string(kind)) != "commit" {
			return "", ErrNotesInvalid
		}
	}
	blob, err := n.run(ctx, data, 128, "hash-object", "-w", "--stdin")
	if err != nil {
		return "", err
	}
	entry := []byte("100644 blob " + strings.TrimSpace(string(blob)) + "\t" + anchor + "\n")
	tree, err := n.run(ctx, entry, 128, "mktree")
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", strings.TrimSpace(string(tree))}
	if expected != "" {
		args = append(args, "-p", expected)
	}
	commit, err := n.run(ctx, []byte("Oiax notification ledger v1\n"), 128, args...)
	if err != nil {
		return "", err
	}
	tip := strings.TrimSpace(string(commit))
	if !notesOIDPattern.MatchString(tip) {
		return "", ErrNotesInvalid
	}
	parents, err := n.run(ctx, nil, 256, "show", "-s", "--format=%P", tip, "--")
	if err != nil || strings.TrimSpace(string(parents)) != expected {
		return "", ErrNotesInvalid
	}
	_, err = n.run(ctx, nil, 2048, "push", "--porcelain", "--no-verify", "--force-with-lease="+n.ref+":"+expected, "--", n.remote, tip+":"+n.ref)
	if err != nil {
		// Re-advertise to distinguish a real expected-tip race from denial.
		latest, readErr := n.Read(ctx)
		if (readErr == nil || errors.Is(readErr, ErrNotesAbsent)) && latest.Tip != expected {
			return "", ErrNotesConflict
		}
		return "", err
	}
	return tip, nil
}
