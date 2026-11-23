package reconcile

// Compiled only through the CLI test's overlay, which points the notification
// runtime's Now, and its pre-POST creation-origin stamp, at
// fixtureNotificationNow. Production keeps time.Now.

import (
	"os"
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
	now := time.Now().UTC()
	floor := fixtureClockStart.Round(0)
	if env, err := time.Parse(time.RFC3339Nano, os.Getenv("OIAX_FIXTURE_CLOCK_FLOOR")); err == nil && env.After(floor) {
		floor = env
	}
	if floored := floor.Add(time.Since(fixtureClockStart)).UTC(); now.Before(floored) {
		return floored
	}
	return now
}
