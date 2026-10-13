// Package sim turns a subway line into a timeline of network conditions and plays it.
package sim

import (
	"time"

	"github.com/nikokozak/train-gap/internal/lines"
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

// Measured on two 3-train rides, Hoyt St <-> Nostrand Av (2026-10-13), with the iPhone recorder.
const (
	// Median time from departure to the last successful probe before a tunnel.
	serviceAfterDeparture = 16 * time.Second
	// Median time from the first successful probe after a tunnel to arrival.
	serviceBeforeArrival = 24 * time.Second
	// Round trips near a tunnel edge were slow (median 117 ms, slowest 10% over 3 s).
	weakSignalWindow  = 5 * time.Second
	weakSignalLatency = 500 * time.Millisecond
	// Half the 78 ms median round trip measured with service.
	serviceLatency = 40 * time.Millisecond
)

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
	HasService bool // False for a run through a tunnel without coverage.
	// Recorded holds the condition measured in each second of the phase, for a replayed ride.
	// Nil for the model.
	Recorded []Condition
}

// ConditionAt returns the network condition at offset into the phase.
func (phase Phase) ConditionAt(offset time.Duration) Condition {
	if len(phase.Recorded) > 0 {
		second := min(max(0, int(offset/time.Second)), len(phase.Recorded)-1)
		return phase.Recorded[second]
	}

	connected := Condition{IsConnected: true, Latency: serviceLatency}
	weak := Condition{IsConnected: true, Latency: weakSignalLatency}
	if phase.Kind == AtStation || phase.HasService {
		return connected
	}

	blackoutStart := serviceAfterDeparture
	blackoutEnd := phase.Duration - serviceBeforeArrival
	switch {
	case blackoutEnd <= blackoutStart:
		// A run this short never fully loses service, but the signal is weak on the way.
		return weak
	case offset >= blackoutStart && offset < blackoutEnd:
		return Condition{IsConnected: false}
	case offset >= blackoutStart-weakSignalWindow && offset < blackoutEnd+weakSignalWindow:
		return weak
	default:
		return connected
	}
}

// Timeline is a ride as a list of phases, repeated forever. For the model it goes from the first
// station to the last and back; for a replay it is the recorded ride.
type Timeline struct {
	Phases     []Phase
	starts     []time.Duration // starts[i] is when Phases[i] begins; the last entry is one full loop.
	RecordedAt time.Time       // When the replayed ride was recorded. Zero for the model.
}

// NewTimeline builds the round trip for a line, using its scheduled running and dwell times.
func NewTimeline(line *lines.Line) Timeline {
	var phases []Phase
	addStop := func(stationIndex int, isForward bool) {
		phases = append(phases, Phase{
			Kind:      AtStation,
			FromIndex: stationIndex,
			ToIndex:   stationIndex,
			IsForward: isForward,
			Duration:  line.Stations[stationIndex].Dwell,
		})
	}
	addRun := func(segmentIndex int, isForward bool) {
		fromIndex, toIndex := segmentIndex, segmentIndex+1
		if !isForward {
			fromIndex, toIndex = toIndex, fromIndex
		}
		phases = append(phases, Phase{
			Kind:       Running,
			FromIndex:  fromIndex,
			ToIndex:    toIndex,
			IsForward:  isForward,
			Duration:   line.Segments[segmentIndex].Duration,
			HasService: line.Segments[segmentIndex].HasService,
		})
	}

	lastStation := len(line.Stations) - 1
	for stationIndex := 0; stationIndex < lastStation; stationIndex++ {
		addStop(stationIndex, true)
		addRun(stationIndex, true)
	}
	for stationIndex := lastStation; stationIndex > 0; stationIndex-- {
		addStop(stationIndex, false)
		addRun(stationIndex-1, false)
	}
	return NewTimelineFromPhases(phases)
}

// NewTimelineFromPhases plays the phases in order and loops.
func NewTimelineFromPhases(phases []Phase) Timeline {
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
	// A linear scan is enough: a round trip has under 200 phases. Measure before replacing it
	// with a binary search, even though the proxy calls this once per chunk of data.
	for index = range timeline.Phases {
		if inLoop < timeline.starts[index+1] {
			return index, inLoop - timeline.starts[index]
		}
	}
	return len(timeline.Phases) - 1, timeline.Phases[len(timeline.Phases)-1].Duration
}
