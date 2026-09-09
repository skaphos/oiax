package git

import "time"

// SetNotesCommandTimeout lowers the per-command bound for tests and returns a
// restore function.
func SetNotesCommandTimeout(d time.Duration) func() {
	previous := notesCommandTimeout
	notesCommandTimeout = d
	return func() { notesCommandTimeout = previous }
}
