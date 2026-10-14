package recording

import (
	"reflect"
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
func syntheticRide(phases ...phase) ([]MotionBatch, []Sample) {
	var batches []MotionBatch
	var samples []Sample
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
			batches = append(batches, MotionBatch{
				TMS:     timeMS,
				Hz:      readingsPerBatch,
				XMilliG: xMilliG,
				YMilliG: make([]int, readingsPerBatch),
				ZMilliG: make([]int, readingsPerBatch),
			})
			probe := Sample{TMS: timeMS}
			probe.Probe.OK = ridePhase.hasService
			samples = append(samples, probe)
			timeMS += batchDurationMS
		}
	}
	return batches, samples
}

func TestJoltMilliG(t *testing.T) {
	batch := MotionBatch{XMilliG: []int{0, 10, 0, 10}, YMilliG: []int{0, 0, 0, 0}, ZMilliG: []int{0, 0, 0, 0}}
	if got := batch.joltMilliG(); got != 10 {
		t.Fatalf("joltMilliG() = %v; want 10", got)
	}
}

func TestFindTunnelGaps(t *testing.T) {
	tests := []struct {
		name   string
		phases []phase
		want   []TimeSpan
	}{
		{
			name:   "a tunnel between two stations",
			phases: []phase{{10, 0, true}, {30, 0, false}, {10, 0, true}},
			want:   []TimeSpan{{10_000, 40_000}},
		},
		{
			name:   "a one-second success inside a tunnel does not split it",
			phases: []phase{{10, 0, true}, {15, 0, false}, {1, 0, true}, {14, 0, false}, {10, 0, true}},
			want:   []TimeSpan{{10_000, 40_000}},
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
			got := findTunnelGaps(samples, 1<<62, DefaultSettings)
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

func TestDetectStops(t *testing.T) {
	route := []RouteStation{{"A", "Alpha"}, {"B", "Bravo"}, {"C", "Charlie"}, {"D", "Delta"}}
	stationB := phase{15, quietJolt, true}
	tunnel := []phase{{40, runningJolt, false}}
	tunnelWithCrawl := []phase{{10, runningJolt, false}, {20, quietJolt, false}, {10, runningJolt, false}}
	wiredTunnel := []phase{{40, runningJolt, true}}

	tests := []struct {
		name         string
		phases       []phase
		endStationID string
		wantStatus   string
		wantRuns     []TimeSpan // nil: check only that each run contains its tunnel gap
	}{
		{
			name:         "tunnel gaps confirm the count and motion places the stops",
			phases:       stationTunnelStation(stationB, tunnel...),
			endStationID: "C",
			wantStatus:   StatusMatched,
			wantRuns:     []TimeSpan{{20_000, 80_000}, {95_000, 155_000}},
		},
		{
			name:         "a quiet crawl in a tunnel is not a stop",
			phases:       stationTunnelStation(stationB, tunnelWithCrawl...),
			endStationID: "C",
			wantStatus:   StatusMatched,
			wantRuns:     []TimeSpan{{20_000, 80_000}, {95_000, 155_000}},
		},
		{
			name:         "walking in the car at a station does not hide the stop",
			phases:       stationTunnelStation(phase{15, walkingJolt, true}, tunnel...),
			endStationID: "C",
			wantStatus:   StatusMatched,
		},
		{
			name:         "a wired tunnel: motion alone places the stops",
			phases:       stationTunnelStation(stationB, wiredTunnel...),
			endStationID: "C",
			wantStatus:   StatusMotionOnly,
			wantRuns:     []TimeSpan{{20_000, 80_000}, {95_000, 155_000}},
		},
		{
			name:       "not sure: the tunnel gaps give the count",
			phases:     stationTunnelStation(stationB, tunnel...),
			wantStatus: StatusUnverified,
			wantRuns:   []TimeSpan{{20_000, 80_000}, {95_000, 155_000}},
		},
		{
			name: "not sure and no tunnel gaps: no labels",
			phases: []phase{{20, quietJolt, true}, {60, runningJolt, true}, stationB,
				{60, runningJolt, true}, {20, quietJolt, true}},
			wantStatus: StatusCountUnknown,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			batches, samples := syntheticRide(test.phases...)
			recording := Recording{
				Start:   SessionStart{RouteStations: route},
				Motion:  batches,
				Samples: samples,
				End:     SessionEnd{EndStationID: test.endStationID},
			}
			detection := DetectStops(recording, DefaultSettings)
			if detection.Status != test.wantStatus {
				t.Fatalf("status = %q; want %q", detection.Status, test.wantStatus)
			}
			if test.wantStatus == StatusCountUnknown {
				if detection.Runs != nil {
					t.Fatalf("got runs for an unknown count: %v", detection.Runs)
				}
				return
			}
			if test.wantRuns != nil && !reflect.DeepEqual(detection.Runs, test.wantRuns) {
				t.Fatalf("runs = %v; want %v", detection.Runs, test.wantRuns)
			}
			tunnelGaps := findTunnelGaps(samples, 1<<62, DefaultSettings)
			if test.wantStatus == StatusMatched {
				for index, run := range detection.Runs {
					if run.StartMS > tunnelGaps[index].StartMS || run.EndMS < tunnelGaps[index].EndMS {
						t.Fatalf("run %v does not contain tunnel gap %v", run, tunnelGaps[index])
					}
				}
			}
		})
	}
}

func TestDetectStopsWithoutMotion(t *testing.T) {
	recording := Recording{Start: SessionStart{RouteStations: []RouteStation{{"A", "Alpha"}}}}
	if got := DetectStops(recording, DefaultSettings).Status; got != StatusNoMotion {
		t.Fatalf("status = %q; want %q", got, StatusNoMotion)
	}
}

func TestCountMarksNearStops(t *testing.T) {
	runs := []TimeSpan{{20_000, 80_000}, {95_000, 155_000}}
	marks := []Mark{
		{TMS: 90_000},                    // In the stop at B.
		{TMS: 93_000},                    // 2 s before departure: within tolerance.
		{TMS: 120_000},                   // Mid-run: the detection missed a stop here.
		{TMS: 30_000, Kind: MarkAnomaly}, // Mid-run, but an anomaly mark does not count.
	}
	if got := countMarksNearStops(marks, runs); got != 2 {
		t.Fatalf("countMarksNearStops() = %d; want 2", got)
	}
}
