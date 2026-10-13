package main

import (
	"math"
	"sort"
)

// Stop detection for recordings made without station markers (recorder builds from 2026-10).
//
// The rider's start and end stations give the number of segments, so the only question is
// where the stops are. Two kinds of evidence answer it:
//
//   - Motion. A running train makes the phone jolt; a stopped train does not. The jolt is the
//     RMS change between consecutive accelerometer readings: it follows rail vibration and mostly
//     ignores slow hand and body movement. It is measured relative to the ride's own median, so it
//     does not matter whether the phone is in a hand or a pocket.
//   - Service. Stations have cell service. On lines without tunnel coverage, each tunnel shows as
//     one long stretch of failed probes. A quiet stretch without service is a train crawling in a
//     tunnel, not a stop.
//
// segmentRide picks the stops that fit both kinds of evidence best over the whole ride. When the
// number of tunnel gaps equals the number of segments, each segment must contain exactly one gap.
//
// Calibrated on two 3-train rides (Hoyt St <-> Nostrand Av, 2026-10-13): one with the phone in a
// hand and Mark taps, one in a pocket. Fixed motion thresholds failed on those rides because the
// jolt level depends on where the phone is. Tune the flags with the -energy trace.

// detectionSettings are the tuning knobs. Each one has a command-line flag.
type detectionSettings struct {
	minimumStopSeconds      int // A shorter quiet stretch is a slowdown, not a stop. Brooklyn 3-train dwells were 6-25 s.
	minimumSegmentSeconds   int // Shortest ride between two stations.
	smoothingSeconds        int // Width of the rolling median, in one-second batches.
	minimumTunnelGapSeconds int // Failed probes for at least this long count as one tunnel.
}

var defaultDetectionSettings = detectionSettings{
	minimumStopSeconds:      3,
	minimumSegmentSeconds:   20,
	smoothingSeconds:        3,
	minimumTunnelGapSeconds: 10,
}

// The recorder writes one motion batch per second.
const batchDurationMS = 1000

// Upper limits that keep the search fast. A 10-minute hold at a station is rare; NYC's longest
// run between stops is about 6 minutes (Broad Channel to Howard Beach on the A).
const (
	maximumStopMS    = 600_000
	maximumSegmentMS = 900_000
)

// Walking jolts far harder than a train. The cap keeps a short walk from outweighing many
// seconds of other evidence.
const maximumRelativeJolt = 2.0

// Extra cost per second for a stop where probes failed: stations have service.
const noServiceStopPenalty = 1.0

// Probes that start within this time of a second describe that second's service.
const serviceLookaroundMS = 2000

// A success this short inside failed probes is a blip in a tunnel, not a station.
const maximumServiceBlipMS = 3000

// The rider taps Mark when the doors open, a moment after the train stops.
const markToleranceMS = 3000

// Outcomes, written to the summary as stop_detection.status.
const (
	statusMatched      = "matched"        // Tunnel gaps confirm the segment count; motion places the stops.
	statusMotionOnly   = "motion_only"    // Tunnel gaps did not match the count (wired tunnel, elevated track, express run). Check the stops.
	statusUnverified   = "unverified_end" // The rider chose "Not sure"; the tunnel gaps give the count.
	statusCountUnknown = "count_unknown"  // "Not sure", and the tunnel gaps do not give a count either.
	statusNoFit        = "no_fit"         // No split fits the time limits; check the end station.
	statusNoMotion     = "no_motion"      // The recording has no motion data.
)

// motionBatch is one `motion` event: about one second of accelerometer readings
// without gravity, in milli-g.
type motionBatch struct {
	TMS     int64 `json:"t_ms"`
	Hz      int   `json:"hz"`
	XMilliG []int `json:"x_mg"`
	YMilliG []int `json:"y_mg"`
	ZMilliG []int `json:"z_mg"`
}

// joltMilliG is the RMS change between consecutive readings in the batch.
func (batch motionBatch) joltMilliG() float64 {
	readingCount := min(len(batch.XMilliG), len(batch.YMilliG), len(batch.ZMilliG))
	if readingCount < 2 {
		return 0
	}
	sumOfSquares := 0.0
	for index := 1; index < readingCount; index++ {
		changeX := float64(batch.XMilliG[index] - batch.XMilliG[index-1])
		changeY := float64(batch.YMilliG[index] - batch.YMilliG[index-1])
		changeZ := float64(batch.ZMilliG[index] - batch.ZMilliG[index-1])
		sumOfSquares += changeX*changeX + changeY*changeY + changeZ*changeZ
	}
	return math.Sqrt(sumOfSquares / float64(readingCount-1))
}

// secondOfRide is one motion batch with the evidence for that second.
type secondOfRide struct {
	startMS        int64
	joltMilliG     float64
	smoothedMilliG float64
	relativeJolt   float64 // smoothedMilliG divided by the ride's median, capped at maximumRelativeJolt.
	hasProbe       bool
	hasService     bool
}

// stopCost is low where a stop is likely: the phone is quiet and probes get through.
func (second secondOfRide) stopCost() float64 {
	if second.hasProbe && !second.hasService {
		return second.relativeJolt + noServiceStopPenalty
	}
	return second.relativeJolt
}

// moveCost is low where the train is likely moving: the phone jolts at least as hard as usual.
func (second secondOfRide) moveCost() float64 {
	return max(0, 1-second.relativeJolt)
}

// timeSpan is a half-open span of time: [startMS, endMS).
type timeSpan struct {
	startMS int64
	endMS   int64
}

// movingRun is the time the train moved between two stops.
type movingRun = timeSpan

// readSecondsOfRide combines motion and probes into one value per second, in time order.
// Batches that start after stopMS (when the rider tapped Stop) are ignored.
func readSecondsOfRide(batches []motionBatch, samples []sample, stopMS int64, settings detectionSettings) []secondOfRide {
	var seconds []secondOfRide
	for _, batch := range batches {
		if batch.TMS <= stopMS {
			seconds = append(seconds, secondOfRide{startMS: batch.TMS, joltMilliG: batch.joltMilliG()})
		}
	}
	sort.Slice(seconds, func(i, j int) bool { return seconds[i].startMS < seconds[j].startMS })
	if len(seconds) == 0 {
		return nil
	}

	// A rolling median removes single bumps without blurring the start of a stop.
	halfWindow := settings.smoothingSeconds / 2
	smoothedValues := make([]float64, len(seconds))
	for index := range seconds {
		window := make([]float64, 0, settings.smoothingSeconds)
		for windowIndex := max(0, index-halfWindow); windowIndex < min(len(seconds), index+halfWindow+1); windowIndex++ {
			window = append(window, seconds[windowIndex].joltMilliG)
		}
		seconds[index].smoothedMilliG = median(window)
		smoothedValues[index] = seconds[index].smoothedMilliG
	}

	// The train runs most of the ride, so the median is the usual running level for this phone position.
	rideLevel := median(smoothedValues)
	successBySecond := map[int64]bool{} // Second -> any probe that started in it succeeded.
	for _, s := range samples {
		successBySecond[s.TMS/1000] = successBySecond[s.TMS/1000] || s.Probe.OK
	}
	for index := range seconds {
		if rideLevel > 0 {
			seconds[index].relativeJolt = min(seconds[index].smoothedMilliG/rideLevel, maximumRelativeJolt)
		}
		centerSecond := seconds[index].startMS / 1000
		for second := centerSecond - serviceLookaroundMS/1000; second <= centerSecond+serviceLookaroundMS/1000; second++ {
			succeeded, hasProbe := successBySecond[second]
			seconds[index].hasProbe = seconds[index].hasProbe || hasProbe
			seconds[index].hasService = seconds[index].hasService || succeeded
		}
	}
	return seconds
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

// findTunnelGaps returns the stretches of failed probes that last at least
// minimumTunnelGapSeconds, in time order. A short success inside failures does not split a gap.
func findTunnelGaps(samples []sample, stopMS int64, settings detectionSettings) []timeSpan {
	sorted := append([]sample(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].TMS < sorted[j].TMS })

	var gaps []timeSpan
	var current *timeSpan
	keepCurrent := func() {
		if current != nil && current.endMS-current.startMS >= int64(settings.minimumTunnelGapSeconds)*1000 {
			gaps = append(gaps, *current)
		}
	}
	for _, s := range sorted {
		if s.TMS > stopMS || s.Probe.OK {
			continue
		}
		// endMS is one second after the last failure, so this is the time since that failure ended.
		if current != nil && s.TMS-current.endMS <= maximumServiceBlipMS {
			current.endMS = s.TMS + batchDurationMS
			continue
		}
		keepCurrent()
		current = &timeSpan{startMS: s.TMS, endMS: s.TMS + batchDurationMS}
	}
	keepCurrent()
	return gaps
}

// segmentRide splits the ride into exactly segmentCount moving runs separated by stops, choosing
// the split with the lowest total cost. The stops at the start and end may have any length; the
// others last minimumStopSeconds to maximumStopMS, and runs last minimumSegmentSeconds to
// maximumSegmentMS. When tunnelGaps is not nil, it has one gap per run, and run k must contain gap k.
// It returns nil when no split fits.
//
// This is a dynamic program over boundary seconds. For each run k and second t it keeps the best
// cost of the ride up to "run k departs at t" and up to "run k arrives at t".
// Time: runs x seconds x seconds within the duration limits.
func segmentRide(seconds []secondOfRide, segmentCount int, tunnelGaps []timeSpan, settings detectionSettings) []movingRun {
	secondCount := len(seconds)
	if segmentCount < 1 || secondCount == 0 {
		return nil
	}

	// Boundary b is the start of seconds[b]; boundary secondCount is the end of the last second.
	boundaryMS := func(boundary int) int64 {
		if boundary == secondCount {
			return seconds[secondCount-1].startMS + batchDurationMS
		}
		return seconds[boundary].startMS
	}
	stopPrefix := make([]float64, secondCount+1)
	movePrefix := make([]float64, secondCount+1)
	for index, second := range seconds {
		stopPrefix[index+1] = stopPrefix[index] + second.stopCost()
		movePrefix[index+1] = movePrefix[index] + second.moveCost()
	}

	// With tunnel gaps, run k departs before gap k and after gap k-1, and arrives after gap k and before gap k+1.
	firstBoundaryAtOrAfter := func(timeMS int64) int {
		return sort.Search(secondCount+1, func(boundary int) bool { return boundaryMS(boundary) >= timeMS })
	}
	departureAllowed := func(run, boundary int) bool {
		if tunnelGaps == nil {
			return true
		}
		return boundary <= firstBoundaryAtOrAfter(tunnelGaps[run].startMS) &&
			(run == 0 || boundary >= firstBoundaryAtOrAfter(tunnelGaps[run-1].endMS))
	}
	arrivalAllowed := func(run, boundary int) bool {
		if tunnelGaps == nil {
			return true
		}
		return boundary >= firstBoundaryAtOrAfter(tunnelGaps[run].endMS) &&
			(run == segmentCount-1 || boundary <= firstBoundaryAtOrAfter(tunnelGaps[run+1].startMS))
	}

	infinity := math.Inf(1)
	newTable := func() [][]float64 {
		table := make([][]float64, segmentCount)
		for run := range table {
			table[run] = make([]float64, secondCount+1)
			for boundary := range table[run] {
				table[run][boundary] = infinity
			}
		}
		return table
	}
	newLinks := func() [][]int {
		links := make([][]int, segmentCount)
		for run := range links {
			links[run] = make([]int, secondCount+1)
		}
		return links
	}
	bestDepart, bestArrive := newTable(), newTable()
	departFrom, arriveFrom := newLinks(), newLinks() // The arrival (or departure) boundary that led here.

	minimumStopMS := int64(settings.minimumStopSeconds) * 1000
	minimumSegmentMS := int64(settings.minimumSegmentSeconds) * 1000
	for run := 0; run < segmentCount; run++ {
		for departure := 0; departure <= secondCount; departure++ {
			if !departureAllowed(run, departure) {
				continue
			}
			if run == 0 {
				bestDepart[0][departure] = stopPrefix[departure] // The start stop: any length.
				continue
			}
			for arrival := departure; arrival >= 0; arrival-- {
				stopMS := boundaryMS(departure) - boundaryMS(arrival)
				if stopMS > maximumStopMS {
					break
				}
				if stopMS < minimumStopMS {
					continue
				}
				cost := bestArrive[run-1][arrival] + stopPrefix[departure] - stopPrefix[arrival]
				if cost < bestDepart[run][departure] {
					bestDepart[run][departure], departFrom[run][departure] = cost, arrival
				}
			}
		}
		for arrival := 0; arrival <= secondCount; arrival++ {
			if !arrivalAllowed(run, arrival) {
				continue
			}
			for departure := arrival; departure >= 0; departure-- {
				runMS := boundaryMS(arrival) - boundaryMS(departure)
				if runMS > maximumSegmentMS {
					break
				}
				if runMS < minimumSegmentMS {
					continue
				}
				cost := bestDepart[run][departure] + movePrefix[arrival] - movePrefix[departure]
				if cost < bestArrive[run][arrival] {
					bestArrive[run][arrival], arriveFrom[run][arrival] = cost, departure
				}
			}
		}
	}

	// The end stop: any length, up to the end of the recording.
	lastRun := segmentCount - 1
	bestCost, finalArrival := infinity, -1
	for arrival := 0; arrival <= secondCount; arrival++ {
		cost := bestArrive[lastRun][arrival] + stopPrefix[secondCount] - stopPrefix[arrival]
		if cost < bestCost {
			bestCost, finalArrival = cost, arrival
		}
	}
	if finalArrival < 0 {
		return nil
	}

	runs := make([]movingRun, segmentCount)
	arrival := finalArrival
	for run := lastRun; run >= 0; run-- {
		departure := arriveFrom[run][arrival]
		runs[run] = movingRun{startMS: boundaryMS(departure), endMS: boundaryMS(arrival)}
		if run > 0 {
			arrival = departFrom[run][departure]
		}
	}
	return runs
}

// detectSegments finds the segments and stops in a recording, labels them from the route and
// the end station, and adds the probe statistics for each one.
func detectSegments(batches []motionBatch, marks []mark, samples []sample, route []routeStation, end sessionEnd, settings detectionSettings) ([]segmentSummary, []stopSummary, *stopDetection) {
	detection := &stopDetection{Marks: len(marks)}
	if len(batches) == 0 {
		detection.Status = statusNoMotion
		return nil, nil, detection
	}

	// Ignore everything after the rider tapped Stop; they are off the train by then.
	stopMS := int64(math.MaxInt64)
	if end.TMS != nil {
		stopMS = *end.TMS
	}
	seconds := readSecondsOfRide(batches, samples, stopMS, settings)
	tunnelGaps := findTunnelGaps(samples, stopMS, settings)
	detection.TunnelGaps = len(tunnelGaps)

	endPosition := -1
	for position, station := range route {
		if end.EndStationID != "" && station.ID == end.EndStationID {
			endPosition = position
		}
	}

	var runs []movingRun
	switch {
	case endPosition > 0 && len(tunnelGaps) == endPosition:
		runs = segmentRide(seconds, endPosition, tunnelGaps, settings)
		detection.Status = statusMatched
		if runs == nil {
			// The gaps and the time limits disagree; fall back to motion and service alone.
			runs = segmentRide(seconds, endPosition, nil, settings)
			detection.Status = statusMotionOnly
		}
	case endPosition > 0:
		runs = segmentRide(seconds, endPosition, nil, settings)
		detection.Status = statusMotionOnly
	case len(tunnelGaps) >= 1 && len(tunnelGaps) < len(route):
		runs = segmentRide(seconds, len(tunnelGaps), tunnelGaps, settings)
		detection.Status = statusUnverified
	default:
		detection.Status = statusCountUnknown
		return nil, nil, detection
	}
	if runs == nil {
		detection.Status = statusNoFit
		return nil, nil, detection
	}
	expectedSegments := len(runs)
	detection.ExpectedSegments = &expectedSegments
	detection.DetectedSegments = len(runs)

	// Run k goes from route[k] to route[k+1]. Each stop is the time before a run; the last stop is
	// where the rider got off.
	recordingEndMS := seconds[len(seconds)-1].startMS + batchDurationMS
	if end.TMS != nil {
		recordingEndMS = *end.TMS
	}
	var segments []segmentSummary
	var stops []stopSummary
	previousEndMS := int64(0)
	for position, run := range runs {
		stops = append(stops, makeStop(route[position], previousEndMS, run.startMS, samples))
		segments = append(segments, segmentSummary{
			FromStationID:   route[position].ID,
			FromStationName: route[position].Name,
			ToStationID:     route[position+1].ID,
			ToStationName:   route[position+1].Name,
			DepartTMS:       run.startMS,
			ArriveTMS:       run.endMS,
			DurationMS:      run.endMS - run.startMS,
			probeStats:      probeStatsBetween(samples, run.startMS, run.endMS),
		})
		previousEndMS = run.endMS
	}
	stops = append(stops, makeStop(route[len(runs)], previousEndMS, recordingEndMS, samples))

	detection.MarksNearStops = countMarksNearStops(marks, runs)
	return segments, stops, detection
}

// countMarksNearStops is the calibration check: the rider taps Mark when the doors open, so with
// good settings every mark falls in a stop, or within markToleranceMS of one.
func countMarksNearStops(marks []mark, runs []movingRun) int {
	count := 0
	for _, markEvent := range marks {
		insideRun := false
		for _, run := range runs {
			if markEvent.TMS >= run.startMS+markToleranceMS && markEvent.TMS < run.endMS-markToleranceMS {
				insideRun = true
				break
			}
		}
		if !insideRun {
			count++
		}
	}
	return count
}

func makeStop(station routeStation, startMS, endMS int64, samples []sample) stopSummary {
	return stopSummary{
		StationID:   station.ID,
		StationName: station.Name,
		StartTMS:    startMS,
		EndTMS:      endMS,
		DurationMS:  max(0, endMS-startMS),
		probeStats:  probeStatsBetween(samples, startMS, endMS),
	}
}
