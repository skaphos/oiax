package reconcile

// Compiled only through the CLI test's overlay, which points the notification
// runtime's Now at fixtureNotificationNow. Production keeps time.Now.

import (
	"os"
	"time"
)

var fixtureClockStart = time.Now()

// fixtureNotificationNow is the wall clock, never earlier than
// OIAX_FIXTURE_CLOCK_FLOOR plus the time this process has run (monotonic).
//
// The test plays the forge. It stamps the merge it seeds with its own clock and
// expects the next invocation to admit it, which holds only if no invocation's
// clock is behind that stamp. A host whose wall clock is stepped backwards
// breaks that: a WSL2 guest, for one, is pulled back about 2.5 s every ~30 s
// (#143). The floor restores the runner clock that cutoffs and scan watermarks
// assume never goes backwards between runs. It does not move a correct clock.
func fixtureNotificationNow() time.Time {
	now := time.Now().UTC()
	floor, err := time.Parse(time.RFC3339Nano, os.Getenv("OIAX_FIXTURE_CLOCK_FLOOR"))
	if err != nil {
		return now
	}
	if floored := floor.Add(time.Since(fixtureClockStart)).UTC(); now.Before(floored) {
		return floored
	}
	return now
}
