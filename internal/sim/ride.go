package sim

import (
	"sync"
	"time"

	"github.com/nikokozak/train-gap/internal/lines"
)

// Speed limits for the simulation clock.
const (
	minimumSpeed = 0.1
	maximumSpeed = 10.0
)

// Ride plays a timeline on a line in simulated time. Simulated time runs at speed times wall time
// and stops while paused. It is safe for concurrent use: proxy connections read the condition
// while the TUI changes the speed or switches to another ride.
type Ride struct {
	mu       sync.Mutex
	line     *lines.Line
	timeline Timeline
	// Simulated time is anchorElapsed plus the wall time since anchorWall, times speed.
	// Every change of speed, pause, or position moves the anchor to "now".
	anchorWall    time.Time
	anchorElapsed time.Duration
	speed         float64
	isPaused      bool
	now           func() time.Time // Tests replace it.
}

// NewRide starts playing the timeline from its first phase, at normal speed.
func NewRide(line *lines.Line, timeline Timeline) *Ride {
	ride := &Ride{line: line, timeline: timeline, speed: 1, now: time.Now}
	ride.anchorWall = ride.now()
	return ride
}

// Switch starts another timeline from its first phase. Speed and pause stay as they are.
func (ride *Ride) Switch(line *lines.Line, timeline Timeline) {
	ride.mu.Lock()
	defer ride.mu.Unlock()
	ride.line, ride.timeline = line, timeline
	ride.anchorElapsed, ride.anchorWall = 0, ride.now()
}

// Line returns the line the ride is on.
func (ride *Ride) Line() *lines.Line {
	ride.mu.Lock()
	defer ride.mu.Unlock()
	return ride.line
}

// Timeline returns what the ride plays.
func (ride *Ride) Timeline() Timeline {
	ride.mu.Lock()
	defer ride.mu.Unlock()
	return ride.timeline
}

// elapsedLocked returns the simulated time. The caller holds mu.
func (ride *Ride) elapsedLocked() time.Duration {
	if ride.isPaused {
		return ride.anchorElapsed
	}
	return ride.anchorElapsed + time.Duration(float64(ride.now().Sub(ride.anchorWall))*ride.speed)
}

// reanchorLocked fixes the current simulated time before a change. The caller holds mu.
func (ride *Ride) reanchorLocked() {
	ride.anchorElapsed = ride.elapsedLocked()
	ride.anchorWall = ride.now()
}

// Elapsed returns the simulated time since the ride started.
func (ride *Ride) Elapsed() time.Duration {
	ride.mu.Lock()
	defer ride.mu.Unlock()
	return ride.elapsedLocked()
}

// Position returns the current phase, its index in the timeline, and the offset into it.
func (ride *Ride) Position() (phase Phase, index int, offset time.Duration) {
	ride.mu.Lock()
	timeline, elapsed := ride.timeline, ride.elapsedLocked()
	ride.mu.Unlock()
	index, offset = timeline.At(elapsed)
	return timeline.Phases[index], index, offset
}

// Condition returns the network condition now.
func (ride *Ride) Condition() Condition {
	phase, _, offset := ride.Position()
	return phase.ConditionAt(offset)
}

// Speed returns the simulation speed (1 = real time).
func (ride *Ride) Speed() float64 {
	ride.mu.Lock()
	defer ride.mu.Unlock()
	return ride.speed
}

// SetSpeed changes the simulation speed, kept within 0.1 to 10.
func (ride *Ride) SetSpeed(speed float64) {
	ride.mu.Lock()
	defer ride.mu.Unlock()
	ride.reanchorLocked()
	ride.speed = min(max(speed, minimumSpeed), maximumSpeed)
}

// IsPaused reports whether simulated time is stopped.
func (ride *Ride) IsPaused() bool {
	ride.mu.Lock()
	defer ride.mu.Unlock()
	return ride.isPaused
}

// SetPaused stops or restarts simulated time.
func (ride *Ride) SetPaused(isPaused bool) {
	ride.mu.Lock()
	defer ride.mu.Unlock()
	ride.reanchorLocked()
	ride.isPaused = isPaused
}

// SkipToNextPhase jumps to the start of the next station stop or run.
func (ride *Ride) SkipToNextPhase() {
	ride.mu.Lock()
	defer ride.mu.Unlock()
	elapsed := ride.elapsedLocked()
	index, offset := ride.timeline.At(elapsed)
	ride.anchorElapsed = elapsed - offset + ride.timeline.Phases[index].Duration
	ride.anchorWall = ride.now()
}
