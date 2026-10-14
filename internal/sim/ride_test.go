package sim

import (
	"testing"
	"time"
)

// A ride with a clock the test moves by hand.
func testRide() (*Ride, func(time.Duration)) {
	ride := NewRide(testLine(), testTimeline())
	wallTime := time.Unix(0, 0)
	ride.now = func() time.Time { return wallTime }
	ride.anchorWall = wallTime
	advance := func(duration time.Duration) { wallTime = wallTime.Add(duration) }
	return ride, advance
}

func TestRideClock(t *testing.T) {
	ride, advance := testRide()

	advance(10 * time.Second)
	if got := ride.Elapsed(); got != 10*time.Second {
		t.Fatalf("after 10 s at speed 1: Elapsed() = %v", got)
	}

	ride.SetSpeed(2)
	advance(10 * time.Second)
	if got := ride.Elapsed(); got != 30*time.Second {
		t.Fatalf("after 10 more s at speed 2: Elapsed() = %v; want 30s", got)
	}

	ride.SetPaused(true)
	advance(time.Hour)
	if got := ride.Elapsed(); got != 30*time.Second {
		t.Fatalf("while paused: Elapsed() = %v; want 30s", got)
	}

	ride.SetPaused(false)
	advance(5 * time.Second)
	if got := ride.Elapsed(); got != 40*time.Second {
		t.Fatalf("after resuming: Elapsed() = %v; want 40s", got)
	}

	ride.SetSpeed(1000)
	if got := ride.Speed(); got != maximumSpeed {
		t.Fatalf("SetSpeed(1000): Speed() = %v; want %v", got, maximumSpeed)
	}
}

func TestRideSkipToNextPhase(t *testing.T) {
	ride, advance := testRide()
	advance(5 * time.Second) // 5 s into the 20 s stop at A.
	ride.SkipToNextPhase()
	phase, index, offset := ride.Position()
	if index != 1 || offset != 0 || phase.Kind != Running {
		t.Fatalf("after skip: phase %d (%v) offset %v; want the run A->B from its start", index, phase.Kind, offset)
	}
	ride.SkipToNextPhase() // To the stop at B.
	ride.SkipToNextPhase() // To the run B->C.
	ride.SkipToNextPhase() // To the stop at C.
	if phase, _, _ := ride.Position(); phase.Kind != AtStation || phase.FromIndex != 2 {
		t.Fatalf("after four skips: %+v; want the stop at C", phase)
	}
}

func TestRideConditionFollowsTheTimeline(t *testing.T) {
	ride, advance := testRide()
	if !ride.Condition().IsConnected {
		t.Fatal("at station A: not connected")
	}
	advance(20*time.Second + 30*time.Second) // 30 s into the tunnel A->B.
	if ride.Condition().IsConnected {
		t.Fatal("in the tunnel: connected")
	}
}

// Switch starts the new timeline from its start, at the same speed.
func TestRideSwitch(t *testing.T) {
	ride, advance := testRide()
	ride.SetSpeed(2)
	advance(30 * time.Second)
	other := NewTimeline([]Phase{testPhase(AtStation, 2, 2, 5, testConnected)})
	ride.Switch(testLine(), other)
	if phase, index, offset := ride.Position(); index != 0 || offset != 0 || phase.FromIndex != 2 {
		t.Fatalf("after Switch: phase %d at %v, %+v; want the new timeline's first phase from its start", index, offset, phase)
	}
	if ride.Speed() != 2 {
		t.Fatalf("after Switch: Speed() = %v; want 2", ride.Speed())
	}
}
