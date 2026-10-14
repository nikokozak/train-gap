package recording

import (
	"fmt"
	"slices"
	"time"

	"github.com/nikokozak/train-gap/rides"
)

// A probe on a new connection takes about three round trips: TCP, TLS, and the request. On the
// 3-train rides (2026-10-13) such probes had a median of 237-261 ms, against 75-85 ms on a reused
// connection.
const roundTripsPerNewConnection = 3

// ToRide turns a recording into a ride: the detected stops and runs, each with the delay measured
// in every second of it. It leaves out what could identify the rider or is not needed: the day
// and time, the device, the session ID, the probe URLs, and the motion data.
func ToRide(recording Recording) (rides.Ride, Detection, error) {
	detection := DetectStops(recording, DefaultSettings)
	if detection.Runs == nil {
		return rides.Ride{}, detection, fmt.Errorf("cannot find the stops (stop detection: %s); see docs/recordings.md", detection.Status)
	}
	startedAt, err := time.Parse(time.RFC3339, recording.Start.StartedAt)
	if err != nil {
		return rides.Ride{}, detection, fmt.Errorf("session_start started_at: %w", err)
	}

	// The last run can end up to a second after the Stop tap: runs end on whole motion batches.
	rideEndMS := max(recording.EndMS(), detection.Runs[len(detection.Runs)-1].EndMS)
	delays := measuredDelays(recording.Samples, rideEndMS)
	toSecond := func(timeMS int64) int { return min(int((timeMS+500)/1000), len(delays)) }

	ride := rides.Ride{LineID: recording.Start.LineID, Month: startedAt.UTC().Format("2006-01"), Carrier: recording.Start.Carrier}
	addPhase := func(phase rides.Phase, startSecond, endSecond int) {
		if endSecond > startSecond { // A stop shorter than half a second is left out.
			phase.DelayMS = slices.Clone(delays[startSecond:endSecond])
			ride.Phases = append(ride.Phases, phase)
		}
	}
	route := recording.Start.RouteStations
	previousEnd := 0
	for position, run := range detection.Runs {
		start, end := toSecond(run.StartMS), toSecond(run.EndMS)
		addPhase(rides.Phase{StationID: route[position].ID}, previousEnd, start)
		addPhase(rides.Phase{FromID: route[position].ID, ToID: route[position+1].ID}, start, end)
		previousEnd = end
	}
	addPhase(rides.Phase{StationID: route[len(detection.Runs)].ID}, previousEnd, len(delays))
	return ride, detection, nil
}

// measuredDelays returns the delay in each second of the ride, from the probe that started in it.
// A second without a probe keeps the value before it.
func measuredDelays(samples []Sample, endMS int64) []int {
	secondCount := int((endMS + 999) / 1000)
	delays := make([]int, secondCount)
	hasProbe := make([]bool, secondCount)
	for _, sample := range samples {
		second := int(sample.TMS / 1000)
		if second < 0 || second >= secondCount {
			continue
		}
		delay := sample.delayMS()
		// Probes overlap, so two can start in the same second; a success wins.
		if hasProbe[second] && delay == rides.NoService {
			continue
		}
		delays[second], hasProbe[second] = delay, true
	}

	firstProbed := slices.Index(hasProbe, true)
	if firstProbed < 0 {
		for second := range delays {
			delays[second] = rides.NoService
		}
		return delays
	}
	for second := range delays {
		switch {
		case second < firstProbed:
			delays[second] = delays[firstProbed]
		case !hasProbe[second]:
			delays[second] = delays[second-1]
		}
	}
	return delays
}

// delayMS is what the probe measured: half a round trip, the delay in each direction. The proxy
// adds it once each way, so one round trip adds the full round trip.
func (sample Sample) delayMS() int {
	if !sample.Probe.OK || sample.Probe.RTTMS == nil {
		return rides.NoService
	}
	roundTripMS := *sample.Probe.RTTMS
	if isReused := sample.Probe.ReusedConnection; isReused == nil || !*isReused {
		roundTripMS /= roundTripsPerNewConnection
	}
	return int(roundTripMS / 2)
}
