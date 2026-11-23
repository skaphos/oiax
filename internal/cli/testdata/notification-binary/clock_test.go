package reconcile

// Compiled only through TestFixtureClockNeverDecreases in
// internal/cli/notification_merge_test.go, which overlays this file and
// clock.go into internal/reconcile. Not part of the stock package.

import (
	"testing"
	"time"
)

func resetFixtureClock() {
	fixtureClockMu.Lock()
	defer fixtureClockMu.Unlock()
	fixtureClockLast, fixtureClockLastMono = time.Time{}, 0
}

// TestFixtureClockForwardThenBack is the case from review on #153: with a
// process start of 100 s, a wall reading of 103.5 s at elapsed 1 s followed by
// a wall reading of 101.1 s at elapsed 1.1 s must not go backwards.
func TestFixtureClockForwardThenBack(t *testing.T) {
	resetFixtureClock()
	start := time.Unix(100, 0).UTC()
	first := fixtureClockNext(time.Unix(103, 500_000_000).UTC(), time.Second, start)
	second := fixtureClockNext(time.Unix(101, 100_000_000).UTC(), 1100*time.Millisecond, start)
	if want := time.Unix(103, 500_000_000).UTC(); !first.Equal(want) {
		t.Fatalf("first = %v, want %v", first, want)
	}
	if want := time.Unix(103, 600_000_000).UTC(); !second.Equal(want) {
		t.Fatalf("second = %v, want %v (previous result plus 100 ms of monotonic time)", second, want)
	}
}

// TestFixtureClockSequenceNeverDecreases drives a wall clock that jumps ahead,
// steps back below the floor, and recovers, and asserts every result is at
// least the previous one and never earlier than the floor.
func TestFixtureClockSequenceNeverDecreases(t *testing.T) {
	resetFixtureClock()
	start := time.Unix(1000, 0).UTC()
	walls := []float64{1000.0, 1004.0, 1001.5, 999.0, 1002.0, 1010.0, 1003.0, 1011.0}
	var prev time.Time
	for i, w := range walls {
		elapsed := time.Duration(i) * 100 * time.Millisecond
		wall := time.Unix(0, int64(w*float64(time.Second))).UTC()
		got := fixtureClockNext(wall, elapsed, start)
		if got.Before(prev) {
			t.Fatalf("step %d: %v is before previous %v", i, got, prev)
		}
		if floor := start.Add(elapsed); got.Before(floor) {
			t.Fatalf("step %d: %v is before floor %v", i, got, floor)
		}
		prev = got
	}
}

// TestFixtureClockForwardOnlyIsUnchanged: a clock that only moves forward is
// returned as is.
func TestFixtureClockForwardOnlyIsUnchanged(t *testing.T) {
	resetFixtureClock()
	start := time.Unix(50, 0).UTC()
	for i := 0; i < 5; i++ {
		elapsed := time.Duration(i) * time.Second
		wall := start.Add(elapsed + 10*time.Millisecond)
		if got := fixtureClockNext(wall, elapsed, start); !got.Equal(wall) {
			t.Fatalf("step %d: got %v, want the wall reading %v", i, got, wall)
		}
	}
}
