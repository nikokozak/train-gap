package main

import (
	"math"
	"sort"
)

// Stop detection for recordings made without station markers (recorder builds from 2026-10).
//
// A moving train shakes the phone much more than a stopped one. Each one-second motion
// batch becomes one number (RMS acceleration), which is smoothed and classified as still,
// moving, or walking. Long runs of movement are the segments between stations.
// The rider's start and end stations say how many segments to expect, so a detection error
// shows up as a count mismatch instead of a silently wrong label.
//
// The default thresholds are estimates. Tune them with a calibration ride (Mark tapped at
// every stop) and the -energy trace.

// detectSegments finds the segments and stops in a recording's motion trace, labels them
// from the route and the end station, and adds the probe statistics for each one.
func detectSegments(batches []motionBatch, marks []mark, samples []sample, route []routeStation, end sessionEnd, settings detectionSettings) ([]segmentSummary, []stopSummary, *stopDetection) {
	detection := &stopDetection{Marks: len(marks)}
	if len(batches) == 0 {
		detection.Status = statusNoMotion
		return nil, nil, detection
	}

	// Ignore motion after the rider tapped Stop; they are off the train by then.
	stopMS := int64(math.MaxInt64)
	recordingEndMS := int64(0)
	for _, batch := range batches {
		recordingEndMS = max(recordingEndMS, batch.TMS+batchDurationMS)
	}
	if end.TMS != nil {
		stopMS = *end.TMS
		recordingEndMS = stopMS
	}

	runs := findMovingRuns(classifySeconds(batches, stopMS, settings), settings)
	detection.DetectedSegments = len(runs)

	endPosition := unknownRoutePosition
	for position, station := range route {
		if end.EndStationID != "" && station.ID == end.EndStationID {
			endPosition = position
		}
	}
	if endPosition != unknownRoutePosition {
		expectedSegments := endPosition
		detection.ExpectedSegments = &expectedSegments
	}

	gapHasNoService := make([]bool, max(0, len(runs)-1))
	for index := range gapHasNoService {
		gapStats := probeStatsBetween(samples, runs[index].endMS, runs[index+1].startMS)
		gapHasNoService[index] = gapStats.Samples > 0 && gapStats.ProbeOK == 0
	}

	labelled, status, holdsRemoved := labelRuns(runs, gapHasNoService, len(route), endPosition)
	detection.Status = status
	detection.HoldsRemoved = holdsRemoved

	// Each segment starts from the stop before it. The last stop is where the rider got off.
	var segments []segmentSummary
	var stops []stopSummary
	previousEndMS := int64(0)
	for _, run := range labelled {
		stops = append(stops, makeStop(route, run.fromPosition, previousEndMS, run.startMS, samples))
		segment := segmentSummary{
			DepartTMS:  run.startMS,
			ArriveTMS:  run.endMS,
			DurationMS: run.endMS - run.startMS,
			probeStats: probeStatsBetween(samples, run.startMS, run.endMS),
		}
		if run.fromPosition != unknownRoutePosition {
			from, to := route[run.fromPosition], route[run.fromPosition+1]
			segment.FromStationID, segment.FromStationName = from.ID, from.Name
			segment.ToStationID, segment.ToStationName = to.ID, to.Name
		}
		segments = append(segments, segment)
		previousEndMS = run.endMS
	}
	finalPosition := unknownRoutePosition
	if status != statusCountMismatch {
		finalPosition = len(labelled)
	}
	stops = append(stops, makeStop(route, finalPosition, previousEndMS, recordingEndMS, samples))

	for _, markEvent := range marks {
		insideSegment := false
		for _, run := range labelled {
			if markEvent.TMS >= run.startMS && markEvent.TMS < run.endMS {
				insideSegment = true
				break
			}
		}
		if !insideSegment {
			detection.MarksInsideStops++
		}
	}

	return segments, stops, detection
}

func makeStop(route []routeStation, position int, startMS, endMS int64, samples []sample) stopSummary {
	stop := stopSummary{
		StartTMS:   startMS,
		EndTMS:     endMS,
		DurationMS: max(0, endMS-startMS),
		probeStats: probeStatsBetween(samples, startMS, endMS),
	}
	if position >= 0 && position < len(route) {
		stop.StationID, stop.StationName = route[position].ID, route[position].Name
	}
	return stop
}

// detectionSettings are the tuning knobs. Each one has a command-line flag.
type detectionSettings struct {
	movingMilliG          float64 // Smoothed RMS at or above this means the train is moving.
	walkingMilliG         float64 // At or above this, the rider is walking, which is not train motion.
	minimumStopSeconds    float64 // A shorter still gap is a slowdown, not a stop.
	minimumSegmentSeconds float64 // A shorter moving run is boarding or shuffling, not a ride between stations.
	smoothingSeconds      int     // Width of the rolling median, in one-second batches.
}

var defaultDetectionSettings = detectionSettings{
	movingMilliG:          25,
	walkingMilliG:         200,
	minimumStopSeconds:    8,
	minimumSegmentSeconds: 20,
	smoothingSeconds:      5,
}

// The recorder writes one motion batch per second.
const batchDurationMS = 1000

// A larger jump between batches means data is missing (for example, iOS paused the app).
// It ends a moving run, so missing data cannot hide a stop.
const missingDataGapMS = 2000

// motionBatch is one `motion` event: about one second of accelerometer readings
// without gravity, in milli-g.
type motionBatch struct {
	TMS     int64 `json:"t_ms"`
	Hz      int   `json:"hz"`
	XMilliG []int `json:"x_mg"`
	YMilliG []int `json:"y_mg"`
	ZMilliG []int `json:"z_mg"`
}

// rmsMilliG is the root mean square of the acceleration magnitude over the batch.
func (batch motionBatch) rmsMilliG() float64 {
	readingCount := min(len(batch.XMilliG), len(batch.YMilliG), len(batch.ZMilliG))
	if readingCount == 0 {
		return 0
	}
	sumOfSquares := 0.0
	for index := 0; index < readingCount; index++ {
		x, y, z := float64(batch.XMilliG[index]), float64(batch.YMilliG[index]), float64(batch.ZMilliG[index])
		sumOfSquares += x*x + y*y + z*z
	}
	return math.Sqrt(sumOfSquares / float64(readingCount))
}

type motionState string

const (
	stateStill   motionState = "still"
	stateMoving  motionState = "moving"
	stateWalking motionState = "walking"
)

// secondOfMotion is one motion batch reduced to a single classified value.
type secondOfMotion struct {
	startMS        int64
	rmsMilliG      float64
	smoothedMilliG float64
	state          motionState
}

// movingRun is a span of train movement: one segment between two stops.
type movingRun struct {
	startMS int64
	endMS   int64
}

// classifySeconds turns motion batches into one classified value per second, in time order.
// Batches that start after stopMS (when the rider tapped Stop) are ignored.
func classifySeconds(batches []motionBatch, stopMS int64, settings detectionSettings) []secondOfMotion {
	var seconds []secondOfMotion
	for _, batch := range batches {
		if batch.TMS > stopMS {
			continue
		}
		seconds = append(seconds, secondOfMotion{startMS: batch.TMS, rmsMilliG: batch.rmsMilliG()})
	}
	sort.Slice(seconds, func(i, j int) bool { return seconds[i].startMS < seconds[j].startMS })

	// A rolling median removes single jolts (a bump, a door) without blurring the start of a stop.
	halfWindow := settings.smoothingSeconds / 2
	for index := range seconds {
		windowStart := max(0, index-halfWindow)
		windowEnd := min(len(seconds), index+halfWindow+1)
		seconds[index].smoothedMilliG = medianRMS(seconds[windowStart:windowEnd])

		switch {
		case seconds[index].smoothedMilliG >= settings.walkingMilliG:
			seconds[index].state = stateWalking
		case seconds[index].smoothedMilliG >= settings.movingMilliG:
			seconds[index].state = stateMoving
		default:
			seconds[index].state = stateStill
		}
	}
	return seconds
}

func medianRMS(window []secondOfMotion) float64 {
	values := make([]float64, len(window))
	for index, second := range window {
		values[index] = second.rmsMilliG
	}
	sort.Float64s(values)
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return (values[middle-1] + values[middle]) / 2
}

// findMovingRuns returns the segments between stops: runs of moving seconds, joined across
// still gaps too short to be a stop, keeping only runs long enough to be a ride between stations.
func findMovingRuns(seconds []secondOfMotion, settings detectionSettings) []movingRun {
	var runs []movingRun
	for index, second := range seconds {
		if second.state != stateMoving {
			continue
		}
		continuesRun := index > 0 &&
			seconds[index-1].state == stateMoving &&
			second.startMS-seconds[index-1].startMS <= missingDataGapMS
		if continuesRun {
			runs[len(runs)-1].endMS = second.startMS + batchDurationMS
		} else {
			runs = append(runs, movingRun{startMS: second.startMS, endMS: second.startMS + batchDurationMS})
		}
	}

	var joinedRuns []movingRun
	for _, run := range runs {
		lastIndex := len(joinedRuns) - 1
		if lastIndex >= 0 && float64(run.startMS-joinedRuns[lastIndex].endMS) < settings.minimumStopSeconds*1000 {
			joinedRuns[lastIndex].endMS = run.endMS
			continue
		}
		joinedRuns = append(joinedRuns, run)
	}

	var segments []movingRun
	for _, run := range joinedRuns {
		if float64(run.endMS-run.startMS) >= settings.minimumSegmentSeconds*1000 {
			segments = append(segments, run)
		}
	}
	return segments
}

// Labelling outcomes, written to the summary as stop_detection.status.
const (
	statusMatched           = "matched"                      // Segment count equals the count implied by the start and end stations.
	statusMatchedAfterHolds = "matched_after_removing_holds" // Equal after removing stops without service (trains held in a tunnel).
	statusUnverifiedEnd     = "unverified_end"               // The rider chose "Not sure"; labels count forward from the start station.
	statusCountMismatch     = "count_mismatch"               // Cannot label; review the stops by hand or tune the thresholds.
	statusNoMotion          = "no_motion"                    // The recording has no motion data.
)

// unknownRoutePosition marks a run or an end station whose place on the route is not known.
const unknownRoutePosition = -1

// A route needs a start and at least one more station before any segment can be labelled.
const minimumRouteStationsToLabel = 2

// labelledRun is a moving run with its position on the route: it goes from
// route[fromPosition] to route[fromPosition+1]. fromPosition is unknownRoutePosition when unlabelled.
type labelledRun struct {
	movingRun
	fromPosition int
}

// labelRuns gives each run its route position.
//
// route has routeLength stations in ride order; the start station is position 0.
// endPosition is the route position where the rider got off, or unknownRoutePosition.
// gapHasNoService[i] is true when no probe succeeded in the stop between runs i and i+1:
// underground stations have service, so such a stop is probably a train held in a tunnel.
func labelRuns(runs []movingRun, gapHasNoService []bool, routeLength int, endPosition int) (labelled []labelledRun, status string, holdsRemoved int) {
	if routeLength < minimumRouteStationsToLabel {
		return unlabelled(runs), statusCountMismatch, 0
	}

	if endPosition == unknownRoutePosition {
		if len(runs) > routeLength-1 {
			return unlabelled(runs), statusCountMismatch, 0
		}
		return labelledInOrder(runs), statusUnverifiedEnd, 0
	}

	expectedRunCount := endPosition
	if len(runs) > expectedRunCount {
		holdCount := 0
		for _, hasNoService := range gapHasNoService {
			if hasNoService {
				holdCount++
			}
		}
		// Remove holds only when they explain the whole difference; anything else needs review.
		if holdCount == len(runs)-expectedRunCount {
			runs = joinAcrossHolds(runs, gapHasNoService)
			holdsRemoved = holdCount
		}
	}

	if len(runs) != expectedRunCount {
		return unlabelled(runs), statusCountMismatch, 0
	}
	if holdsRemoved > 0 {
		return labelledInOrder(runs), statusMatchedAfterHolds, holdsRemoved
	}
	return labelledInOrder(runs), statusMatched, 0
}

func joinAcrossHolds(runs []movingRun, gapHasNoService []bool) []movingRun {
	joinedRuns := []movingRun{runs[0]}
	for index := 1; index < len(runs); index++ {
		if gapHasNoService[index-1] {
			joinedRuns[len(joinedRuns)-1].endMS = runs[index].endMS
			continue
		}
		joinedRuns = append(joinedRuns, runs[index])
	}
	return joinedRuns
}

func labelledInOrder(runs []movingRun) []labelledRun {
	labelled := make([]labelledRun, len(runs))
	for index, run := range runs {
		labelled[index] = labelledRun{movingRun: run, fromPosition: index}
	}
	return labelled
}

func unlabelled(runs []movingRun) []labelledRun {
	labelled := make([]labelledRun, len(runs))
	for index, run := range runs {
		labelled[index] = labelledRun{movingRun: run, fromPosition: unknownRoutePosition}
	}
	return labelled
}
