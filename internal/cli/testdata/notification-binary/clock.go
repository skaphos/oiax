package reconcile

// Compiled only through the CLI test's overlay, which points the notification
// runtime's Now, and its pre-POST creation-origin stamp, at
// fixtureNotificationNow. Production keeps time.Now.

import (
	"os"
	"sync"
	"time"
)

// fixtureClockStart carries both a wall and a monotonic reading of process
// start.
var fixtureClockStart = time.Now()

// fixtureNotificationNow is the wall clock, but never earlier than a floor plus
// the time this process has run, measured monotonically. The floor is
// OIAX_FIXTURE_CLOCK_FLOOR when that is later than this process's own start,
// and the process start otherwise.
//
// The test plays the forge and stamps events with its own clock. The binary's
// cutoffs, scan watermarks and origin evidence must be ordered against those
// stamps, and across invocations. A host whose wall clock is stepped backwards
// breaks that ordering: a WSL2 guest, for one, is pulled back about 2.5 s every
// ~30 s (#143). Without the process-start floor, a step between activation and
// a create's origin stamp in the same run would put that create before its own
// cutoff. Without the cross-invocation floor, a later run could read before an
// earlier run's watermark. Cutoffs and watermarks assume a runner clock that
// never goes backwards, and the floor restores it. It does not move a clock
// that only goes forward.
func fixtureNotificationNow() time.Time {
	floor := fixtureClockStart.Round(0)
	if env, err := time.Parse(time.RFC3339Nano, os.Getenv("OIAX_FIXTURE_CLOCK_FLOOR")); err == nil && env.After(floor) {
		floor = env
	}
	// Sample under the lock, so concurrent destination workers apply their
	// samples in the order they were taken.
	fixtureClockMu.Lock()
	defer fixtureClockMu.Unlock()
	return fixtureClockNextLocked(time.Now().UTC(), time.Since(fixtureClockStart), floor)
}

var (
	fixtureClockMu       sync.Mutex
	fixtureClockLast     time.Time     // last value returned
	fixtureClockLastMono time.Duration // monotonic elapsed when it was returned
)

// fixtureClockNext is fixtureClockNextLocked under the clock's mutex.
func fixtureClockNext(wall time.Time, elapsed time.Duration, floor time.Time) time.Time {
	fixtureClockMu.Lock()
	defer fixtureClockMu.Unlock()
	return fixtureClockNextLocked(wall, elapsed, floor)
}

// fixtureClockNextLocked returns the latest of: the wall reading, the floor
// plus the monotonic time since process start, and the previous result plus
// the monotonic time since that result. The last term is a high-water mark: a
// wall clock that first jumps ahead of the floor and then steps back would
// otherwise make readings decrease (103.5 s, then 101.1 s), and two such reads
// could set an activation cutoff after the origin stamp that follows it.
//
// elapsed is clamped to at least the elapsed value of the previous result, so
// a sample applied out of order (taken earlier, processed later) can never
// produce a negative monotonic delta. The caller holds fixtureClockMu.
func fixtureClockNextLocked(wall time.Time, elapsed time.Duration, floor time.Time) time.Time {
	if elapsed < fixtureClockLastMono {
		elapsed = fixtureClockLastMono
	}
	out := wall
	if f := floor.Add(elapsed); out.Before(f) {
		out = f
	}
	if !fixtureClockLast.IsZero() {
		if hw := fixtureClockLast.Add(elapsed - fixtureClockLastMono); out.Before(hw) {
			out = hw
		}
	}
	fixtureClockLast, fixtureClockLastMono = out, elapsed
	return out.UTC()
}
