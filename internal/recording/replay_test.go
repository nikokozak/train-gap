package recording

import (
	"reflect"
	"testing"
	"time"

	"github.com/nikokozak/train-gap/internal/lines"
	"github.com/nikokozak/train-gap/internal/sim"
)

// probe builds one sample. A negative rtt is a failed probe.
func probe(timeMS int64, rttMS int64, isReused bool) Sample {
	var sample Sample
	sample.TMS = timeMS
	if rttMS >= 0 {
		sample.Probe.OK = true
		sample.Probe.RTTMS = &rttMS
		sample.Probe.ReusedConnection = &isReused
	}
	return sample
}

func TestMeasuredConditions(t *testing.T) {
	connected := func(latency time.Duration) sim.Condition { return sim.Condition{IsConnected: true, Latency: latency} }
	blackout := sim.Condition{IsConnected: false}
	samples := []Sample{
		probe(1_200, 80, true),   // Second 1. Second 0 has no probe: it takes the first condition.
		probe(2_100, -1, false),  // Second 2: failed.
		probe(2_900, 300, false), // Second 2 again: a success wins. A new connection is three round trips.
		probe(3_000, -1, false),  // Second 3: failed. Second 4 has no probe: it keeps the failure.
	}
	want := []sim.Condition{connected(40 * time.Millisecond), connected(40 * time.Millisecond), connected(50 * time.Millisecond), blackout, blackout}
	if got := measuredConditions(samples, 4_500); !reflect.DeepEqual(got, want) {
		t.Fatalf("measuredConditions() = %+v; want %+v", got, want)
	}
}

// A recorded ride A -> B -> C on a line that starts one station earlier, at Z.
func TestTimeline(t *testing.T) {
	batches, samples := syntheticRide(stationTunnelStation(phase{15, quietJolt, true}, phase{40, runningJolt, false})...)
	for index := range samples {
		if samples[index].Probe.OK {
			samples[index] = probe(samples[index].TMS, 50, true)
		}
	}
	stopTapMS := int64(175_000)
	recording := Recording{
		Start: SessionStart{
			StartedAt:     "2026-10-03T12:00:00.250Z",
			RouteStations: []RouteStation{{"A", "Alpha"}, {"B", "Bravo"}, {"C", "Charlie"}},
		},
		Motion:  batches,
		Samples: samples,
		End:     SessionEnd{TMS: &stopTapMS, EndStationID: "C"},
	}
	line := &lines.Line{ID: "X", Stations: []lines.Station{{ID: "Z"}, {ID: "A"}, {ID: "B"}, {ID: "C"}}}

	timeline, err := Timeline(recording, line)
	if err != nil {
		t.Fatal(err)
	}
	type step struct {
		kind               sim.PhaseKind
		fromIndex, toIndex int
		duration           time.Duration
	}
	var got []step
	for _, phase := range timeline.Phases {
		if !phase.IsForward {
			t.Fatalf("phase %+v runs backward; the ride goes A -> C", phase)
		}
		got = append(got, step{phase.Kind, phase.FromIndex, phase.ToIndex, phase.Duration})
	}
	// Runs from stop detection: 20-80 s and 95-155 s; the ride ends with the Stop tap at 175 s.
	want := []step{
		{sim.AtStation, 1, 1, 20 * time.Second},
		{sim.Running, 1, 2, 60 * time.Second},
		{sim.AtStation, 2, 2, 15 * time.Second},
		{sim.Running, 2, 3, 60 * time.Second},
		{sim.AtStation, 3, 3, 20 * time.Second},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("phases = %+v; want %+v", got, want)
	}

	// The tunnel is 20-60 s into the ride: 30 s is 10 s into the first run.
	if condition := timeline.Phases[1].ConditionAt(10 * time.Second); condition.IsConnected {
		t.Fatalf("in the tunnel: %+v; want no service", condition)
	}
	if condition := timeline.Phases[2].ConditionAt(5 * time.Second); condition != (sim.Condition{IsConnected: true, Latency: 25 * time.Millisecond}) {
		t.Fatalf("at B: %+v; want connected with 25 ms", condition)
	}
	if want := time.Date(2026, 10, 3, 12, 0, 0, 250_000_000, time.UTC); !timeline.RecordedAt.Equal(want) {
		t.Fatalf("RecordedAt = %v; want %v", timeline.RecordedAt, want)
	}
}
