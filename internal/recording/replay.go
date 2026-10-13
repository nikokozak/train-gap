package recording

import (
	"fmt"
	"slices"
	"time"

	"github.com/nikokozak/train-gap/internal/lines"
	"github.com/nikokozak/train-gap/internal/sim"
)

// A probe on a new connection takes about three round trips: TCP, TLS, and the request. On the
// 3-train rides (2026-10-13) such probes had a median of 237-261 ms, against 75-85 ms on a reused
// connection.
const roundTripsPerNewConnection = 3

// Timeline turns a recorded ride into a timeline the simulator can play: the detected stops and
// runs, each with the network condition measured in every second of it. The simulator plays the
// ride from the start station to the end station, then starts over.
func Timeline(recording Recording, line *lines.Line) (sim.Timeline, error) {
	detection := DetectStops(recording, DefaultSettings)
	if detection.Runs == nil {
		return sim.Timeline{}, fmt.Errorf("cannot find the stops in the recording (stop detection: %s); see docs/recordings.md", detection.Status)
	}

	// The route has one station more than there are runs.
	route := recording.Start.RouteStations
	stationIndexes := make([]int, len(detection.Runs)+1)
	for position := range stationIndexes {
		stationID := route[position].ID
		stationIndexes[position] = slices.IndexFunc(line.Stations, func(station lines.Station) bool { return station.ID == stationID })
		if stationIndexes[position] < 0 {
			return sim.Timeline{}, fmt.Errorf("station %s (%s) is not on the %s line", stationID, route[position].Name, line.ID)
		}
	}
	isForward := stationIndexes[1] > stationIndexes[0]

	// The last run can end up to a second after the Stop tap: runs end on whole motion batches.
	rideEndMS := max(recording.EndMS(), detection.Runs[len(detection.Runs)-1].EndMS)
	conditions := measuredConditions(recording.Samples, rideEndMS)
	newPhase := func(kind sim.PhaseKind, fromPosition, toPosition int, startMS, endMS int64) sim.Phase {
		recorded := conditions[min(startMS/1000, int64(len(conditions))):min((endMS+999)/1000, int64(len(conditions)))]
		return sim.Phase{
			Kind:       kind,
			FromIndex:  stationIndexes[fromPosition],
			ToIndex:    stationIndexes[toPosition],
			IsForward:  isForward,
			Duration:   time.Duration(endMS-startMS) * time.Millisecond,
			HasService: !slices.ContainsFunc(recorded, func(condition sim.Condition) bool { return !condition.IsConnected }),
			Recorded:   recorded,
		}
	}

	var phases []sim.Phase
	previousEndMS := int64(0)
	for position, run := range detection.Runs {
		phases = append(phases,
			newPhase(sim.AtStation, position, position, previousEndMS, run.StartMS),
			newPhase(sim.Running, position, position+1, run.StartMS, run.EndMS))
		previousEndMS = run.EndMS
	}
	lastPosition := len(detection.Runs)
	phases = append(phases, newPhase(sim.AtStation, lastPosition, lastPosition, previousEndMS, rideEndMS))

	timeline := sim.NewTimelineFromPhases(phases)
	// Not knowing the start time only changes what the screen shows, so a parse error is ignored.
	timeline.RecordedAt, _ = time.Parse(time.RFC3339, recording.Start.StartedAt)
	return timeline, nil
}

// measuredConditions returns the condition in each second of the ride, from the probe that
// started in it. A second without a probe keeps the condition before it.
func measuredConditions(samples []Sample, endMS int64) []sim.Condition {
	secondCount := int((endMS + 999) / 1000)
	conditions := make([]sim.Condition, secondCount)
	hasProbe := make([]bool, secondCount)
	for _, sample := range samples {
		second := int(sample.TMS / 1000)
		if second < 0 || second >= secondCount {
			continue
		}
		condition := sample.condition()
		// Probes overlap, so two can start in the same second; a success wins.
		if hasProbe[second] && !condition.IsConnected {
			continue
		}
		conditions[second], hasProbe[second] = condition, true
	}

	firstProbed := slices.Index(hasProbe, true)
	if firstProbed < 0 {
		return conditions // No probes: no service throughout.
	}
	for second := range conditions {
		switch {
		case second < firstProbed:
			conditions[second] = conditions[firstProbed]
		case !hasProbe[second]:
			conditions[second] = conditions[second-1]
		}
	}
	return conditions
}

// condition is what the probe measured. The proxy adds Latency once in each direction, so one
// round trip adds twice Latency.
func (sample Sample) condition() sim.Condition {
	if !sample.Probe.OK || sample.Probe.RTTMS == nil {
		return sim.Condition{IsConnected: false}
	}
	roundTrip := time.Duration(*sample.Probe.RTTMS) * time.Millisecond
	if isReused := sample.Probe.ReusedConnection; isReused == nil || !*isReused {
		roundTrip /= roundTripsPerNewConnection
	}
	return sim.Condition{IsConnected: true, Latency: roundTrip / 2}
}
