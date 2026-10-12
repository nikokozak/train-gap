package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Motion strengths for synthetic rides, in milli-g RMS.
const (
	stillMilliG      = 5
	trainMilliG      = 40
	walkMilliG       = 300
	noMotionData     = -1 // The app was paused: no batches for this phase.
	readingsPerBatch = 20 // One-second batches at 20 Hz.
)

// phase is one stretch of a synthetic ride: how long it lasts and how hard the phone shakes.
type phase struct {
	seconds   int
	rmsMilliG int
}

// syntheticMotion builds one motion batch per second. Each batch alternates +rms and -rms
// on the x axis, so its RMS is exactly rmsMilliG.
func syntheticMotion(phases ...phase) []motionBatch {
	var batches []motionBatch
	timeMS := int64(0)
	for _, ridePhase := range phases {
		for second := 0; second < ridePhase.seconds; second++ {
			if ridePhase.rmsMilliG != noMotionData {
				xMilliG := make([]int, readingsPerBatch)
				for index := range xMilliG {
					xMilliG[index] = ridePhase.rmsMilliG
					if index%2 == 1 {
						xMilliG[index] = -ridePhase.rmsMilliG
					}
				}
				batches = append(batches, motionBatch{
					TMS:     timeMS,
					Hz:      readingsPerBatch,
					XMilliG: xMilliG,
					YMilliG: make([]int, readingsPerBatch),
					ZMilliG: make([]int, readingsPerBatch),
				})
			}
			timeMS += batchDurationMS
		}
	}
	return batches
}

func TestRMSMilliG(t *testing.T) {
	batch := motionBatch{XMilliG: []int{3, -3}, YMilliG: []int{4, -4}, ZMilliG: []int{0, 0}}
	if got := batch.rmsMilliG(); got != 5 {
		t.Fatalf("rmsMilliG() = %v; want 5", got)
	}
}

func TestFindMovingRuns(t *testing.T) {
	tests := []struct {
		name   string
		phases []phase
		want   []movingRun
	}{
		{
			name:   "two rides with a station stop between them",
			phases: []phase{{30, stillMilliG}, {60, trainMilliG}, {25, stillMilliG}, {50, trainMilliG}, {20, stillMilliG}},
			want:   []movingRun{{30_000, 90_000}, {115_000, 165_000}},
		},
		{
			name:   "a short slowdown inside a ride is not a stop",
			phases: []phase{{10, stillMilliG}, {40, trainMilliG}, {4, stillMilliG}, {40, trainMilliG}, {10, stillMilliG}},
			want:   []movingRun{{10_000, 94_000}},
		},
		{
			name:   "walking onto the train is not a segment",
			phases: []phase{{20, stillMilliG}, {10, walkMilliG}, {20, stillMilliG}, {60, trainMilliG}, {20, stillMilliG}},
			want:   []movingRun{{50_000, 110_000}},
		},
		{
			name:   "walking off after the last stop is not train motion",
			phases: []phase{{10, stillMilliG}, {60, trainMilliG}, {5, stillMilliG}, {30, walkMilliG}},
			want:   []movingRun{{10_000, 70_000}},
		},
		{
			name:   "missing data splits a run, so it cannot hide a stop",
			phases: []phase{{10, stillMilliG}, {30, trainMilliG}, {20, noMotionData}, {30, trainMilliG}, {10, stillMilliG}},
			want:   []movingRun{{10_000, 40_000}, {60_000, 90_000}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			seconds := classifySeconds(syntheticMotion(test.phases...), math.MaxInt64, defaultDetectionSettings)
			got := findMovingRuns(seconds, defaultDetectionSettings)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("findMovingRuns() = %v; want %v", got, test.want)
			}
		})
	}
}

func TestLabelRuns(t *testing.T) {
	const routeLength = 4 // Stations A, B, C, D in ride order; A is the start.
	first := movingRun{0, 60_000}
	second := movingRun{80_000, 120_000}
	third := movingRun{150_000, 200_000}
	fourth := movingRun{220_000, 260_000}

	tests := []struct {
		name             string
		runs             []movingRun
		gapHasNoService  []bool
		endPosition      int
		wantStatus       string
		wantRuns         []movingRun
		wantPositions    []int
		wantHoldsRemoved int
	}{
		{
			name:            "segment count matches the end station",
			runs:            []movingRun{first, second},
			gapHasNoService: []bool{false},
			endPosition:     2,
			wantStatus:      statusMatched,
			wantRuns:        []movingRun{first, second},
			wantPositions:   []int{0, 1},
		},
		{
			name:             "a stop without service is a hold in a tunnel",
			runs:             []movingRun{first, second, third},
			gapHasNoService:  []bool{false, true},
			endPosition:      2,
			wantStatus:       statusMatchedAfterHolds,
			wantRuns:         []movingRun{first, {80_000, 200_000}},
			wantPositions:    []int{0, 1},
			wantHoldsRemoved: 1,
		},
		{
			name:            "an extra stop with service needs review",
			runs:            []movingRun{first, second, third},
			gapHasNoService: []bool{false, false},
			endPosition:     2,
			wantStatus:      statusCountMismatch,
			wantRuns:        []movingRun{first, second, third},
			wantPositions:   []int{unknownRoutePosition, unknownRoutePosition, unknownRoutePosition},
		},
		{
			name:          "a missed stop needs review",
			runs:          []movingRun{first},
			endPosition:   2,
			wantStatus:    statusCountMismatch,
			wantRuns:      []movingRun{first},
			wantPositions: []int{unknownRoutePosition},
		},
		{
			name:            "no end station: labels count forward from the start",
			runs:            []movingRun{first, second},
			gapHasNoService: []bool{false},
			endPosition:     unknownRoutePosition,
			wantStatus:      statusUnverifiedEnd,
			wantRuns:        []movingRun{first, second},
			wantPositions:   []int{0, 1},
		},
		{
			name:            "no end station and more segments than the route has",
			runs:            []movingRun{first, second, third, fourth},
			gapHasNoService: []bool{false, false, false},
			endPosition:     unknownRoutePosition,
			wantStatus:      statusCountMismatch,
			wantRuns:        []movingRun{first, second, third, fourth},
			wantPositions:   []int{unknownRoutePosition, unknownRoutePosition, unknownRoutePosition, unknownRoutePosition},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			labelled, status, holdsRemoved := labelRuns(test.runs, test.gapHasNoService, routeLength, test.endPosition)
			if status != test.wantStatus {
				t.Fatalf("status = %q; want %q", status, test.wantStatus)
			}
			if holdsRemoved != test.wantHoldsRemoved {
				t.Fatalf("holdsRemoved = %d; want %d", holdsRemoved, test.wantHoldsRemoved)
			}
			var gotRuns []movingRun
			var gotPositions []int
			for _, run := range labelled {
				gotRuns = append(gotRuns, run.movingRun)
				gotPositions = append(gotPositions, run.fromPosition)
			}
			if !reflect.DeepEqual(gotRuns, test.wantRuns) {
				t.Fatalf("runs = %v; want %v", gotRuns, test.wantRuns)
			}
			if !reflect.DeepEqual(gotPositions, test.wantPositions) {
				t.Fatalf("positions = %v; want %v", gotPositions, test.wantPositions)
			}
		})
	}
}

// A whole ride from A to C: service at stations, none between them, two Mark taps,
// a walk out, and train-like motion after the Stop tap that must be ignored.
func TestDetectSegmentsLabelsARide(t *testing.T) {
	batches := syntheticMotion(
		phase{30, stillMilliG}, // 0-30 s: waiting at A
		phase{60, trainMilliG}, // 30-90 s: A to B
		phase{25, stillMilliG}, // 90-115 s: stopped at B
		phase{70, trainMilliG}, // 115-185 s: B to C
		phase{10, stillMilliG}, // 185-195 s: stopped at C
		phase{20, walkMilliG},  // 195-215 s: walking out; Stop tapped at 215 s
		phase{40, trainMilliG}, // 215-255 s: after Stop, must be ignored
	)
	stationSpans := [][2]int64{{0, 30_000}, {90_000, 115_000}, {185_000, 255_000}}
	var samples []sample
	for timeMS := int64(0); timeMS < 255_000; timeMS += 1000 {
		probe := sample{TMS: timeMS}
		for _, span := range stationSpans {
			if timeMS >= span[0] && timeMS < span[1] {
				probe.Probe.OK = true
			}
		}
		samples = append(samples, probe)
	}
	route := []routeStation{{"A", "Alpha"}, {"B", "Bravo"}, {"C", "Charlie"}, {"D", "Delta"}}
	stopTMS := int64(215_000)
	end := sessionEnd{TMS: &stopTMS, EndStationID: "C"}
	marks := []mark{{TMS: 100_000}, {TMS: 190_000}}

	segments, stops, detection := detectSegments(batches, marks, samples, route, end, defaultDetectionSettings)

	if detection.Status != statusMatched {
		t.Fatalf("status = %q; want %q", detection.Status, statusMatched)
	}
	if detection.MarksInsideStops != 2 {
		t.Fatalf("marks inside stops = %d; want 2", detection.MarksInsideStops)
	}

	type segmentLabel struct {
		from, to          string
		departMS, arrived int64
		probeOK           int
	}
	var gotSegments []segmentLabel
	for _, segment := range segments {
		gotSegments = append(gotSegments, segmentLabel{segment.FromStationName, segment.ToStationName, segment.DepartTMS, segment.ArriveTMS, segment.ProbeOK})
	}
	wantSegments := []segmentLabel{
		{"Alpha", "Bravo", 30_000, 90_000, 0},
		{"Bravo", "Charlie", 115_000, 185_000, 0},
	}
	if !reflect.DeepEqual(gotSegments, wantSegments) {
		t.Fatalf("segments = %+v; want %+v", gotSegments, wantSegments)
	}

	type stopLabel struct {
		station          string
		startMS, endMS   int64
		samples, probeOK int
	}
	var gotStops []stopLabel
	for _, stop := range stops {
		gotStops = append(gotStops, stopLabel{stop.StationName, stop.StartTMS, stop.EndTMS, stop.Samples, stop.ProbeOK})
	}
	wantStops := []stopLabel{
		{"Alpha", 0, 30_000, 30, 30},
		{"Bravo", 90_000, 115_000, 25, 25},
		{"Charlie", 185_000, 215_000, 30, 30},
	}
	if !reflect.DeepEqual(gotStops, wantStops) {
		t.Fatalf("stops = %+v; want %+v", gotStops, wantStops)
	}
}

// summarizeFile must read the new recorder events by the exact JSON names the iPhone app writes
// (RecorderEvents.swift). A wrong tag would otherwise skip detection or drop the end station silently.
func TestSummarizeFileReadsNewRecorderEvents(t *testing.T) {
	var lines []string
	addEvent := func(event any) {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(encoded))
	}

	addEvent(map[string]any{
		"type": "session_start", "v": 1, "session_id": "test", "started_at": "2026-10-03T12:00:00Z",
		"line_id": "X", "from_station_id": "A", "direction": "forward",
		"route_stations": []map[string]string{{"id": "A", "name": "Alpha"}, {"id": "B", "name": "Bravo"}, {"id": "C", "name": "Charlie"}},
	})
	batches := syntheticMotion(phase{20, stillMilliG}, phase{60, trainMilliG}, phase{20, stillMilliG}, phase{60, trainMilliG}, phase{20, stillMilliG})
	for _, batch := range batches {
		addEvent(struct {
			Type string `json:"type"`
			motionBatch
		}{"motion", batch})
		addEvent(map[string]any{"type": "sample", "t_ms": batch.TMS, "path": map[string]any{"status": "satisfied"}, "probe": map[string]any{"ok": true, "rtt_ms": 50}})
	}
	addEvent(map[string]any{"type": "mark", "t_ms": 90_000})
	addEvent(map[string]any{"type": "session_end", "t_ms": 180_000, "ended_at": "2026-10-03T12:03:00Z", "end_station_id": "C", "end_station_name": "Charlie"})

	path := filepath.Join(t.TempDir(), "ride.ndjson")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := summarizeFile(path, defaultDetectionSettings)
	if err != nil {
		t.Fatal(err)
	}
	detection := result.StopDetection
	if detection == nil || detection.Status != statusMatched || detection.ExpectedSegments == nil || *detection.ExpectedSegments != 2 {
		t.Fatalf("stop detection = %+v; want matched with 2 expected segments", detection)
	}
	if detection.Marks != 1 || detection.MarksInsideStops != 1 {
		t.Fatalf("marks = %d, inside stops = %d; want 1 and 1", detection.Marks, detection.MarksInsideStops)
	}
	if len(result.Segments) != 2 || result.Segments[1].FromStationName != "Bravo" || result.Segments[1].ToStationName != "Charlie" {
		t.Fatalf("segments = %+v; want Alpha->Bravo, Bravo->Charlie", result.Segments)
	}
}
