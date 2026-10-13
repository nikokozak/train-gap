package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Jolt levels for synthetic rides, in milli-g. Real rides: stops 10-40, running 60-150, walking 250+.
const (
	quietJolt   = 5
	runningJolt = 100
	walkingJolt = 400
)

const readingsPerBatch = 20 // One-second batches at 20 Hz.

// phase is one stretch of a synthetic ride: its length, how hard the phone jolts, and whether
// probes get through.
type phase struct {
	seconds    int
	joltMilliG int
	hasService bool
}

// syntheticRide builds one motion batch and one probe sample per second. Each batch alternates
// between +jolt/2 and -jolt/2 on the x axis, so its jolt is exactly joltMilliG.
func syntheticRide(phases ...phase) ([]motionBatch, []sample) {
	var batches []motionBatch
	var samples []sample
	timeMS := int64(0)
	for _, ridePhase := range phases {
		for second := 0; second < ridePhase.seconds; second++ {
			xMilliG := make([]int, readingsPerBatch)
			for index := range xMilliG {
				xMilliG[index] = ridePhase.joltMilliG / 2
				if index%2 == 1 {
					xMilliG[index] = -ridePhase.joltMilliG / 2
				}
			}
			batches = append(batches, motionBatch{
				TMS:     timeMS,
				Hz:      readingsPerBatch,
				XMilliG: xMilliG,
				YMilliG: make([]int, readingsPerBatch),
				ZMilliG: make([]int, readingsPerBatch),
			})
			probe := sample{TMS: timeMS}
			probe.Probe.OK = ridePhase.hasService
			samples = append(samples, probe)
			timeMS += batchDurationMS
		}
	}
	return batches, samples
}

func TestJoltMilliG(t *testing.T) {
	batch := motionBatch{XMilliG: []int{0, 10, 0, 10}, YMilliG: []int{0, 0, 0, 0}, ZMilliG: []int{0, 0, 0, 0}}
	if got := batch.joltMilliG(); got != 10 {
		t.Fatalf("joltMilliG() = %v; want 10", got)
	}
}

func TestFindTunnelGaps(t *testing.T) {
	tests := []struct {
		name   string
		phases []phase
		want   []timeSpan
	}{
		{
			name:   "a tunnel between two stations",
			phases: []phase{{10, 0, true}, {30, 0, false}, {10, 0, true}},
			want:   []timeSpan{{10_000, 40_000}},
		},
		{
			name:   "a one-second success inside a tunnel does not split it",
			phases: []phase{{10, 0, true}, {15, 0, false}, {1, 0, true}, {14, 0, false}, {10, 0, true}},
			want:   []timeSpan{{10_000, 40_000}},
		},
		{
			name:   "a few failed probes at a station are not a tunnel",
			phases: []phase{{10, 0, true}, {5, 0, false}, {10, 0, true}},
			want:   nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, samples := syntheticRide(test.phases...)
			got := findTunnelGaps(samples, 1<<62, defaultDetectionSettings)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("findTunnelGaps() = %v; want %v", got, test.want)
			}
		})
	}
}

// Station A, a tunnel, station B, a tunnel, station C. Each tunnel has 10 s of service at each
// end (the platforms), as on lines without tunnel coverage.
func stationTunnelStation(stationB phase, tunnelMiddle ...phase) []phase {
	phases := []phase{{20, quietJolt, true}, {10, runningJolt, true}}
	phases = append(phases, tunnelMiddle...)
	phases = append(phases, phase{10, runningJolt, true}, stationB, phase{10, runningJolt, true},
		phase{40, runningJolt, false}, phase{10, runningJolt, true}, phase{20, quietJolt, true})
	return phases
}

func TestDetectSegments(t *testing.T) {
	route := []routeStation{{"A", "Alpha"}, {"B", "Bravo"}, {"C", "Charlie"}, {"D", "Delta"}}
	stationB := phase{15, quietJolt, true}
	tunnel := []phase{{40, runningJolt, false}}
	tunnelWithCrawl := []phase{{10, runningJolt, false}, {20, quietJolt, false}, {10, runningJolt, false}}
	wiredTunnel := []phase{{40, runningJolt, true}}

	tests := []struct {
		name         string
		phases       []phase
		endStationID string
		wantStatus   string
		wantRuns     []movingRun // nil: check only that each run contains its tunnel gap
	}{
		{
			name:         "tunnel gaps confirm the count and motion places the stops",
			phases:       stationTunnelStation(stationB, tunnel...),
			endStationID: "C",
			wantStatus:   statusMatched,
			wantRuns:     []movingRun{{20_000, 80_000}, {95_000, 155_000}},
		},
		{
			name:         "a quiet crawl in a tunnel is not a stop",
			phases:       stationTunnelStation(stationB, tunnelWithCrawl...),
			endStationID: "C",
			wantStatus:   statusMatched,
			wantRuns:     []movingRun{{20_000, 80_000}, {95_000, 155_000}},
		},
		{
			name:         "walking in the car at a station does not hide the stop",
			phases:       stationTunnelStation(phase{15, walkingJolt, true}, tunnel...),
			endStationID: "C",
			wantStatus:   statusMatched,
		},
		{
			name:         "a wired tunnel: motion alone places the stops",
			phases:       stationTunnelStation(stationB, wiredTunnel...),
			endStationID: "C",
			wantStatus:   statusMotionOnly,
			wantRuns:     []movingRun{{20_000, 80_000}, {95_000, 155_000}},
		},
		{
			name:       "not sure: the tunnel gaps give the count",
			phases:     stationTunnelStation(stationB, tunnel...),
			wantStatus: statusUnverified,
			wantRuns:   []movingRun{{20_000, 80_000}, {95_000, 155_000}},
		},
		{
			name: "not sure and no tunnel gaps: no labels",
			phases: []phase{{20, quietJolt, true}, {60, runningJolt, true}, stationB,
				{60, runningJolt, true}, {20, quietJolt, true}},
			wantStatus: statusCountUnknown,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			batches, samples := syntheticRide(test.phases...)
			end := sessionEnd{EndStationID: test.endStationID}
			segments, stops, detection := detectSegments(batches, nil, samples, route, end, defaultDetectionSettings)
			if detection.Status != test.wantStatus {
				t.Fatalf("status = %q; want %q", detection.Status, test.wantStatus)
			}
			if test.wantStatus == statusCountUnknown {
				if segments != nil || stops != nil {
					t.Fatalf("got labels for an unknown count: %v %v", segments, stops)
				}
				return
			}

			var gotRuns []movingRun
			for index, segment := range segments {
				if segment.FromStationID != route[index].ID || segment.ToStationID != route[index+1].ID {
					t.Fatalf("segment %d labelled %s->%s", index, segment.FromStationID, segment.ToStationID)
				}
				gotRuns = append(gotRuns, movingRun{segment.DepartTMS, segment.ArriveTMS})
			}
			if test.wantRuns != nil && !reflect.DeepEqual(gotRuns, test.wantRuns) {
				t.Fatalf("runs = %v; want %v", gotRuns, test.wantRuns)
			}
			tunnelGaps := findTunnelGaps(samples, 1<<62, defaultDetectionSettings)
			if test.wantStatus == statusMatched {
				for index, run := range gotRuns {
					if run.startMS > tunnelGaps[index].startMS || run.endMS < tunnelGaps[index].endMS {
						t.Fatalf("run %v does not contain tunnel gap %v", run, tunnelGaps[index])
					}
				}
			}
			if len(stops) != len(gotRuns)+1 || stops[len(stops)-1].StationID != route[len(gotRuns)].ID {
				t.Fatalf("stops = %+v; want one per station from A to the end station", stops)
			}
		})
	}
}

func TestDetectSegmentsWithoutMotion(t *testing.T) {
	_, _, detection := detectSegments(nil, nil, nil, []routeStation{{"A", "Alpha"}}, sessionEnd{}, defaultDetectionSettings)
	if detection.Status != statusNoMotion {
		t.Fatalf("status = %q; want %q", detection.Status, statusNoMotion)
	}
}

func TestCountMarksNearStops(t *testing.T) {
	runs := []movingRun{{20_000, 80_000}, {95_000, 155_000}}
	marks := []mark{
		{TMS: 90_000},  // In the stop at B.
		{TMS: 93_000},  // 2 s before departure: within tolerance.
		{TMS: 120_000}, // Mid-run: the detection missed a stop here.
	}
	if got := countMarksNearStops(marks, runs); got != 2 {
		t.Fatalf("countMarksNearStops() = %d; want 2", got)
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
	batches, samples := syntheticRide(stationTunnelStation(phase{15, quietJolt, true}, phase{40, runningJolt, false})...)
	for index, batch := range batches {
		addEvent(struct {
			Type string `json:"type"`
			motionBatch
		}{"motion", batch})
		addEvent(map[string]any{"type": "sample", "t_ms": samples[index].TMS, "path": map[string]any{"status": "satisfied"}, "probe": map[string]any{"ok": samples[index].Probe.OK, "rtt_ms": 50}})
	}
	addEvent(map[string]any{"type": "mark", "t_ms": 85_000})
	addEvent(map[string]any{"type": "session_end", "t_ms": 175_000, "ended_at": "2026-10-03T12:03:00Z", "end_station_id": "C", "end_station_name": "Charlie"})

	path := filepath.Join(t.TempDir(), "ride.ndjson")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := summarizeFile(path, defaultDetectionSettings)
	if err != nil {
		t.Fatal(err)
	}
	detection := result.StopDetection
	if detection == nil || detection.Status != statusMatched || detection.TunnelGaps != 2 {
		t.Fatalf("stop detection = %+v; want matched with 2 tunnel gaps", detection)
	}
	if detection.Marks != 1 || detection.MarksNearStops != 1 {
		t.Fatalf("marks = %d, near stops = %d; want 1 and 1", detection.Marks, detection.MarksNearStops)
	}
	if len(result.Segments) != 2 || result.Segments[1].FromStationName != "Bravo" || result.Segments[1].ToStationName != "Charlie" {
		t.Fatalf("segments = %+v; want Alpha->Bravo, Bravo->Charlie", result.Segments)
	}
}
