package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRunExitCodes drives run() the way main does: through os.Args and the
// process streams. Both are process globals, swapped for the test and restored
// by cleanup, so this test must stay serial.
func TestRunExitCodes(t *testing.T) {
	sink, err := os.Create(filepath.Join(t.TempDir(), "sink"))
	if err != nil {
		t.Fatal(err)
	}
	prevArgs, prevOut, prevErr := os.Args, os.Stdout, os.Stderr
	os.Stdout, os.Stderr = sink, sink
	t.Cleanup(func() {
		os.Args, os.Stdout, os.Stderr = prevArgs, prevOut, prevErr
		if err := sink.Close(); err != nil {
			t.Error(err)
		}
	})

	os.Args = []string{"oiax", "--help"}
	if code := run(); code != 0 {
		t.Errorf("run() with --help = %d, want 0", code)
	}

	os.Args = []string{"oiax", "no-such-command"}
	if code := run(); code != 1 {
		t.Errorf("run() with an unknown command = %d, want 1", code)
	}
}
