// Package sim plays a recorded ride: a timeline of station stops and runs, each with the network
// condition measured in every second of it.
package sim

import (
	"time"
)

// Condition is the network condition the proxy applies at one moment.
type Condition struct {
	IsConnected bool
	Latency     time.Duration // Added one-way delay per chunk of data while connected.
}

// IsWeak reports a connection near a tunnel edge: it works, but slowly.
func (condition Condition) IsWeak() bool {
	return condition.IsConnected && condition.Latency >= weakSignalLatency
}

// Describe names the condition for people: "service", "weak signal", or "no service".
func (condition Condition) Describe() string {
	switch {
	case !condition.IsConnected:
		return "no service"
	case condition.IsWeak():
		return "weak signal"
	default:
		return "service"
	}
}

// From this added delay up, the screen calls the signal weak. Near tunnel edges on the 3-train
// rides (2026-10), the slowest tenth of round trips took over 1 s: 500 ms each way.
const weakSignalLatency = 500 * time.Millisecond

// PhaseKind says whether the train stands at a station or runs between two.
type PhaseKind int

const (
	AtStation PhaseKind = iota
	Running
)

// Phase is one station stop or one run between neighbouring stations.
type Phase struct {
	Kind       PhaseKind
	FromIndex  int  // Index into the line's stations: the station, or where the run starts.
	ToIndex    int  // Where the run ends. Equal to FromIndex at a station.
	IsForward  bool // True while the train travels toward the last station of the line.
	Duration   time.Duration
	HasService bool // False when any recorded second had no service.
	// Recorded holds the condition measured in each second of the phase.
	Recorded []Condition
	// Source says where the data comes from, for people: "recorded Oct 2026 · Verizon".
	Source string
}

// ConditionAt returns the condition recorded at offset into the phase.
func (phase Phase) ConditionAt(offset time.Duration) Condition {
	if len(phase.Recorded) == 0 {
		return Condition{IsConnected: false} // A phase without seconds has no duration, so no one asks.
	}
	second := min(max(0, int(offset/time.Second)), len(phase.Recorded)-1)
	return phase.Recorded[second]
}

// Timeline is a list of phases, played in order and repeated forever.
type Timeline struct {
	Phases []Phase
	starts []time.Duration // starts[i] is when Phases[i] begins; the last entry is one full loop.
}

// NewTimeline plays the phases in order and loops.
func NewTimeline(phases []Phase) Timeline {
	starts := make([]time.Duration, len(phases)+1)
	for index, phase := range phases {
		starts[index+1] = starts[index] + phase.Duration
	}
	return Timeline{Phases: phases, starts: starts}
}

// LoopDuration is the time for one round trip.
func (timeline Timeline) LoopDuration() time.Duration {
	return timeline.starts[len(timeline.starts)-1]
}

// Start returns when the phase at index begins, within the first loop.
func (timeline Timeline) Start(index int) time.Duration {
	return timeline.starts[index]
}

// At returns the phase at elapsed time, looping over the round trip, and the offset into it.
func (timeline Timeline) At(elapsed time.Duration) (index int, offset time.Duration) {
	if timeline.LoopDuration() <= 0 {
		return 0, 0
	}
	inLoop := elapsed % timeline.LoopDuration()
	if inLoop < 0 {
		inLoop += timeline.LoopDuration()
	}
	// A linear scan is enough: a ride has under 200 phases. Measure before replacing it
	// with a binary search, even though the proxy calls this once per chunk of data.
	for index = range timeline.Phases {
		if inLoop < timeline.starts[index+1] {
			return index, inLoop - timeline.starts[index]
		}
	}
	return len(timeline.Phases) - 1, timeline.Phases[len(timeline.Phases)-1].Duration
}
