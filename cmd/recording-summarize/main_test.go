package main

import (
	"testing"

	"github.com/nikokozak/train-gap/internal/recording"
)

// Run k goes from route[k] to route[k+1]; the stops fill the time around the runs.
func TestLabelRuns(t *testing.T) {
	route := []recording.RouteStation{{ID: "A", Name: "Alpha"}, {ID: "B", Name: "Bravo"}, {ID: "C", Name: "Charlie"}}
	runs := []recording.TimeSpan{{StartMS: 20_000, EndMS: 80_000}, {StartMS: 95_000, EndMS: 155_000}}

	segments, stops := labelRuns(route, runs, 175_000, nil)
	if len(segments) != 2 || segments[1].FromStationName != "Bravo" || segments[1].ToStationName != "Charlie" || segments[1].DurationMS != 60_000 {
		t.Fatalf("segments = %+v; want Alpha->Bravo, Bravo->Charlie", segments)
	}
	wantStops := []struct {
		stationID      string
		startMS, endMS int64
	}{{"A", 0, 20_000}, {"B", 80_000, 95_000}, {"C", 155_000, 175_000}}
	if len(stops) != len(wantStops) {
		t.Fatalf("stops = %+v; want 3", stops)
	}
	for index, want := range wantStops {
		if stops[index].StationID != want.stationID || stops[index].StartTMS != want.startMS || stops[index].EndTMS != want.endMS {
			t.Fatalf("stop %d = %+v; want %+v", index, stops[index], want)
		}
	}
}

// Each request counts in the window it was sent in; a nil round trip is a lost request.
func TestUDPStatsBetween(t *testing.T) {
	milliseconds := func(value int64) *int64 { return &value }
	probes := []recording.UDPProbe{
		{TMS: 0, IntervalMS: 200, RTTMS: []*int64{milliseconds(30), nil, milliseconds(50), milliseconds(40), nil}},
		{TMS: 1_000, IntervalMS: 200, RTTMS: []*int64{nil, nil, nil, nil, nil}}, // A tunnel.
	}
	tests := []struct {
		name                   string
		startMS, endMS         int64
		wantRequests, wantLost int
		wantMedianMS           int64 // 0 when no request was answered.
	}{
		{"whole ride", 0, 2_000, 10, 7, 40},
		{"first second", 0, 1_000, 5, 2, 40},
		{"requests sent at 400 and 600 ms", 400, 700, 2, 0, 40}, // The lower middle value.
		{"the tunnel", 1_000, 2_000, 5, 5, 0},
	}
	for _, test := range tests {
		stats := udpStatsBetween(probes, test.startMS, test.endMS)
		medianMS := int64(0)
		if stats.UDPRTTP50MS != nil {
			medianMS = *stats.UDPRTTP50MS
		}
		if stats.UDPRequests != test.wantRequests || stats.UDPRequests-stats.UDPReplies != test.wantLost || medianMS != test.wantMedianMS {
			t.Errorf("%s: %d requests, %d lost, median %d ms; want %d, %d, %d", test.name,
				stats.UDPRequests, stats.UDPRequests-stats.UDPReplies, medianMS, test.wantRequests, test.wantLost, test.wantMedianMS)
		}
	}
}
