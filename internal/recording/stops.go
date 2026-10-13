package recording

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

// Settings are the tuning knobs. Each one has a recording-summarize flag.
type Settings struct {
	MinimumStopSeconds      int // A shorter quiet stretch is a slowdown, not a stop. Brooklyn 3-train dwells were 6-25 s.
	MinimumSegmentSeconds   int // Shortest ride between two stations.
	SmoothingSeconds        int // Width of the rolling median, in one-second batches.
	MinimumTunnelGapSeconds int // Failed probes for at least this long count as one tunnel.
}

// DefaultSettings fit both calibration rides.
var DefaultSettings = Settings{
	MinimumStopSeconds:      3,
	MinimumSegmentSeconds:   20,
	SmoothingSeconds:        3,
	MinimumTunnelGapSeconds: 10,
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
	StatusMatched      = "matched"        // Tunnel gaps confirm the segment count; motion places the stops.
	StatusMotionOnly   = "motion_only"    // Tunnel gaps did not match the count (wired tunnel, elevated track, express run). Check the stops.
	StatusUnverified   = "unverified_end" // The rider chose "Not sure"; the tunnel gaps give the count.
	StatusCountUnknown = "count_unknown"  // "Not sure", and the tunnel gaps do not give a count either.
	StatusNoFit        = "no_fit"         // No split fits the time limits; check the end station.
	StatusNoMotion     = "no_motion"      // The recording has no motion data.
)

// joltMilliG is the RMS change between consecutive readings in the batch.
func (batch MotionBatch) joltMilliG() float64 {
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

// SecondOfRide is one motion batch with the evidence for that second.
type SecondOfRide struct {
	StartMS        int64
	JoltMilliG     float64
	SmoothedMilliG float64
	RelativeJolt   float64 // SmoothedMilliG divided by the ride's median, capped at maximumRelativeJolt.
	HasProbe       bool
	HasService     bool
}

// stopCost is low where a stop is likely: the phone is quiet and probes get through.
func (second SecondOfRide) stopCost() float64 {
	if second.HasProbe && !second.HasService {
		return second.RelativeJolt + noServiceStopPenalty
	}
	return second.RelativeJolt
}

// moveCost is low where the train is likely moving: the phone jolts at least as hard as usual.
func (second SecondOfRide) moveCost() float64 {
	return max(0, 1-second.RelativeJolt)
}

// TimeSpan is a half-open span of time: [StartMS, EndMS).
type TimeSpan struct {
	StartMS int64
	EndMS   int64
}

// SecondsOfRide combines motion and probes into one value per second, in time order, up to the
// end of the ride. The -energy trace prints it.
func SecondsOfRide(recording Recording, settings Settings) []SecondOfRide {
	endMS := recording.EndMS()
	var seconds []SecondOfRide
	for _, batch := range recording.Motion {
		if batch.TMS <= endMS {
			seconds = append(seconds, SecondOfRide{StartMS: batch.TMS, JoltMilliG: batch.joltMilliG()})
		}
	}
	sort.Slice(seconds, func(i, j int) bool { return seconds[i].StartMS < seconds[j].StartMS })
	if len(seconds) == 0 {
		return nil
	}

	// A rolling median removes single bumps without blurring the start of a stop.
	halfWindow := settings.SmoothingSeconds / 2
	smoothedValues := make([]float64, len(seconds))
	for index := range seconds {
		window := make([]float64, 0, settings.SmoothingSeconds)
		for windowIndex := max(0, index-halfWindow); windowIndex < min(len(seconds), index+halfWindow+1); windowIndex++ {
			window = append(window, seconds[windowIndex].JoltMilliG)
		}
		seconds[index].SmoothedMilliG = median(window)
		smoothedValues[index] = seconds[index].SmoothedMilliG
	}

	// The train runs most of the ride, so the median is the usual running level for this phone position.
	rideLevel := median(smoothedValues)
	successBySecond := map[int64]bool{} // Second -> any probe that started in it succeeded.
	for _, sample := range recording.Samples {
		successBySecond[sample.TMS/1000] = successBySecond[sample.TMS/1000] || sample.Probe.OK
	}
	for index := range seconds {
		if rideLevel > 0 {
			seconds[index].RelativeJolt = min(seconds[index].SmoothedMilliG/rideLevel, maximumRelativeJolt)
		}
		centerSecond := seconds[index].StartMS / 1000
		for second := centerSecond - serviceLookaroundMS/1000; second <= centerSecond+serviceLookaroundMS/1000; second++ {
			succeeded, hasProbe := successBySecond[second]
			seconds[index].HasProbe = seconds[index].HasProbe || hasProbe
			seconds[index].HasService = seconds[index].HasService || succeeded
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
// MinimumTunnelGapSeconds, in time order. A short success inside failures does not split a gap.
func findTunnelGaps(samples []Sample, endMS int64, settings Settings) []TimeSpan {
	sorted := append([]Sample(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].TMS < sorted[j].TMS })

	var gaps []TimeSpan
	var current *TimeSpan
	keepCurrent := func() {
		if current != nil && current.EndMS-current.StartMS >= int64(settings.MinimumTunnelGapSeconds)*1000 {
			gaps = append(gaps, *current)
		}
	}
	for _, sample := range sorted {
		if sample.TMS > endMS || sample.Probe.OK {
			continue
		}
		// EndMS is one second after the last failure, so this is the time since that failure ended.
		if current != nil && sample.TMS-current.EndMS <= maximumServiceBlipMS {
			current.EndMS = sample.TMS + batchDurationMS
			continue
		}
		keepCurrent()
		current = &TimeSpan{StartMS: sample.TMS, EndMS: sample.TMS + batchDurationMS}
	}
	keepCurrent()
	return gaps
}

// segmentRide splits the ride into exactly segmentCount moving runs separated by stops, choosing
// the split with the lowest total cost. The stops at the start and end may have any length; the
// others last MinimumStopSeconds to maximumStopMS, and runs last MinimumSegmentSeconds to
// maximumSegmentMS. When tunnelGaps is not nil, it has one gap per run, and run k must contain gap k.
// It returns nil when no split fits.
//
// This is a dynamic program over boundary seconds. For each run k and second t it keeps the best
// cost of the ride up to "run k departs at t" and up to "run k arrives at t".
// Time: runs x seconds x seconds within the duration limits.
func segmentRide(seconds []SecondOfRide, segmentCount int, tunnelGaps []TimeSpan, settings Settings) []TimeSpan {
	secondCount := len(seconds)
	if segmentCount < 1 || secondCount == 0 {
		return nil
	}

	// Boundary b is the start of seconds[b]; boundary secondCount is the end of the last second.
	boundaryMS := func(boundary int) int64 {
		if boundary == secondCount {
			return seconds[secondCount-1].StartMS + batchDurationMS
		}
		return seconds[boundary].StartMS
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
		return boundary <= firstBoundaryAtOrAfter(tunnelGaps[run].StartMS) &&
			(run == 0 || boundary >= firstBoundaryAtOrAfter(tunnelGaps[run-1].EndMS))
	}
	arrivalAllowed := func(run, boundary int) bool {
		if tunnelGaps == nil {
			return true
		}
		return boundary >= firstBoundaryAtOrAfter(tunnelGaps[run].EndMS) &&
			(run == segmentCount-1 || boundary <= firstBoundaryAtOrAfter(tunnelGaps[run+1].StartMS))
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

	minimumStopMS := int64(settings.MinimumStopSeconds) * 1000
	minimumSegmentMS := int64(settings.MinimumSegmentSeconds) * 1000
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

	runs := make([]TimeSpan, segmentCount)
	arrival := finalArrival
	for run := lastRun; run >= 0; run-- {
		departure := arriveFrom[run][arrival]
		runs[run] = TimeSpan{StartMS: boundaryMS(departure), EndMS: boundaryMS(arrival)}
		if run > 0 {
			arrival = departFrom[run][departure]
		}
	}
	return runs
}

// Detection is where the train moved during a recording.
type Detection struct {
	Status string
	// Runs[k] is the train moving from route station k to route station k+1. The stops are the
	// times between runs. Nil when the stops could not be found.
	Runs []TimeSpan
	// Stretches without service long enough to be a tunnel. On lines without tunnel coverage,
	// one per segment.
	TunnelGaps int
	// Calibration check: marks should fall in detected stops (within 3 s), not inside runs.
	MarksNearStops int
}

// DetectStops finds the runs between stations from the route, the end station, motion, and service.
func DetectStops(recording Recording, settings Settings) Detection {
	if len(recording.Motion) == 0 {
		return Detection{Status: StatusNoMotion}
	}

	// Ignore everything after the rider tapped Stop; they are off the train by then.
	endMS := recording.EndMS()
	seconds := SecondsOfRide(recording, settings)
	tunnelGaps := findTunnelGaps(recording.Samples, endMS, settings)
	detection := Detection{TunnelGaps: len(tunnelGaps)}

	route := recording.Start.RouteStations
	endPosition := -1
	for position, station := range route {
		if recording.End.EndStationID != "" && station.ID == recording.End.EndStationID {
			endPosition = position
		}
	}

	switch {
	case endPosition > 0 && len(tunnelGaps) == endPosition:
		detection.Runs = segmentRide(seconds, endPosition, tunnelGaps, settings)
		detection.Status = StatusMatched
		if detection.Runs == nil {
			// The gaps and the time limits disagree; fall back to motion and service alone.
			detection.Runs = segmentRide(seconds, endPosition, nil, settings)
			detection.Status = StatusMotionOnly
		}
	case endPosition > 0:
		detection.Runs = segmentRide(seconds, endPosition, nil, settings)
		detection.Status = StatusMotionOnly
	case len(tunnelGaps) >= 1 && len(tunnelGaps) < len(route):
		detection.Runs = segmentRide(seconds, len(tunnelGaps), tunnelGaps, settings)
		detection.Status = StatusUnverified
	default:
		detection.Status = StatusCountUnknown
		return detection
	}
	if detection.Runs == nil {
		detection.Status = StatusNoFit
		return detection
	}
	detection.MarksNearStops = countMarksNearStops(recording.Marks, detection.Runs)
	return detection
}

// countMarksNearStops is the calibration check: the rider taps Mark when the doors open, so with
// good settings every mark falls in a stop, or within markToleranceMS of one.
func countMarksNearStops(marks []Mark, runs []TimeSpan) int {
	count := 0
	for _, mark := range marks {
		insideRun := false
		for _, run := range runs {
			if mark.TMS >= run.StartMS+markToleranceMS && mark.TMS < run.EndMS-markToleranceMS {
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
