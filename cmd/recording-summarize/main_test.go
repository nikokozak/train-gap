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
