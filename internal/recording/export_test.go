package recording

import (
	"reflect"
	"testing"

	"github.com/nikokozak/train-gap/rides"
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

func TestMeasuredDelays(t *testing.T) {
	samples := []Sample{
		probe(1_200, 80, true),   // Second 1. Second 0 has no probe: it takes the first value.
		probe(2_100, -1, false),  // Second 2: failed.
		probe(2_900, 300, false), // Second 2 again: a success wins. A new connection is three round trips.
		probe(3_000, -1, false),  // Second 3: failed. Second 4 has no probe: it keeps the failure.
	}
	want := []int{40, 40, 50, rides.NoService, rides.NoService}
	if got := measuredDelays(samples, 4_500); !reflect.DeepEqual(got, want) {
		t.Fatalf("measuredDelays() = %v; want %v", got, want)
	}
}

// A recorded ride A -> B -> C becomes stops and runs with whole seconds, and keeps only the month.
func TestToRide(t *testing.T) {
	batches, samples := syntheticRide(stationTunnelStation(phase{15, quietJolt, true}, phase{40, runningJolt, false})...)
	for index := range samples {
		if samples[index].Probe.OK {
			samples[index] = probe(samples[index].TMS, 50, true)
		}
	}
	stopTapMS := int64(175_000)
	recorded := Recording{
		Start: SessionStart{
			StartedAt:     "2026-10-03T12:00:00.250Z",
			LineID:        "X",
			Carrier:       "Verizon",
			RouteStations: []RouteStation{{"A", "Alpha"}, {"B", "Bravo"}, {"C", "Charlie"}},
		},
		Motion:  batches,
		Samples: samples,
		End:     SessionEnd{TMS: &stopTapMS, EndStationID: "C"},
	}

	ride, detection, err := ToRide(recorded)
	if err != nil {
		t.Fatal(err)
	}
	if detection.Status != StatusMatched || ride.LineID != "X" || ride.Month != "2026-10" || ride.Carrier != "Verizon" {
		t.Fatalf("ride = %+v, status %s; want line X, month 2026-10, Verizon, matched", ride, detection.Status)
	}
	type step struct {
		station, from, to string
		seconds           int
	}
	var got []step
	for _, phase := range ride.Phases {
		got = append(got, step{phase.StationID, phase.FromID, phase.ToID, len(phase.DelayMS)})
	}
	// Runs from stop detection: 20-80 s and 95-155 s; the ride ends with the Stop tap at 175 s.
	want := []step{{"A", "", "", 20}, {"", "A", "B", 60}, {"B", "", "", 15}, {"", "B", "C", 60}, {"C", "", "", 20}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("phases = %+v; want %+v", got, want)
	}
	// The tunnel is 20-60 s into the ride: 30 s is 10 s into the first run.
	if delay := ride.Phases[1].DelayMS[10]; delay != rides.NoService {
		t.Fatalf("in the tunnel: delay %d; want no service", delay)
	}
	if delay := ride.Phases[2].DelayMS[5]; delay != 25 {
		t.Fatalf("at B: delay %d ms; want 25 (half the 50 ms round trip)", delay)
	}
}
