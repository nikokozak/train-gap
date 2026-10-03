package sim

import (
	"testing"
	"time"

	"github.com/nikokozak/train-gap/internal/lines"
)

func TestTrainUpdateCarriesOverTimeAcrossStates(t *testing.T) {
	line := &lines.Line{
		ID: "X",
		Stations: []lines.Station{
			{ID: "a", ShortName: "A", HasService: true, DwellTime: 10 * time.Second},
			{ID: "b", ShortName: "B", HasService: true, DwellTime: 10 * time.Second},
		},
		Segments: []lines.Segment{
			{FromID: "a", ToID: "b", IsTunnel: false, Duration: 30 * time.Second, Degradation: 1.0},
		},
	}

	tr := NewTrain(line)
	tr.SetSpeed(1.0)

	tr.Update(20 * time.Second)
	if tr.State != InTransit {
		t.Fatalf("expected state %v; got %v", InTransit, tr.State)
	}
	if tr.StationIndex != 0 {
		t.Fatalf("expected StationIndex 0; got %d", tr.StationIndex)
	}
	// 20s total: 10s dwell + 3s departing => 7s into 10s transit => 0.7
	if tr.Progress < 0.65 || tr.Progress > 0.75 {
		t.Fatalf("expected progress ~0.7; got %f", tr.Progress)
	}
}

func TestTrainUpdateAdvancesStationsAndReversesAtEndpoint(t *testing.T) {
	line := &lines.Line{
		ID: "X",
		Stations: []lines.Station{
			{ID: "a", ShortName: "A", HasService: true, DwellTime: 10 * time.Second},
			{ID: "b", ShortName: "B", HasService: true, DwellTime: 10 * time.Second},
		},
		Segments: []lines.Segment{
			{FromID: "a", ToID: "b", IsTunnel: false, Duration: 30 * time.Second, Degradation: 1.0},
		},
	}

	tr := NewTrain(line)
	tr.SetSpeed(1.0)

	tr.Update(30 * time.Second)
	if tr.State != AtStation {
		t.Fatalf("expected state %v; got %v", AtStation, tr.State)
	}
	if tr.StationIndex != 1 {
		t.Fatalf("expected StationIndex 1; got %d", tr.StationIndex)
	}
	// Total to arrive and begin dwell at next station:
	// 10s dwell + 3s depart + 10s transit + 3s arrive = 26s, leaving 4s into 5s dwell => 0.8
	if tr.Progress < 0.75 || tr.Progress > 0.85 {
		t.Fatalf("expected progress ~0.8; got %f", tr.Progress)
	}

	// Advance enough to finish remaining 1s of dwell and enter Departing.
	tr.Update(2 * time.Second)
	if tr.State != Departing {
		t.Fatalf("expected state %v; got %v", Departing, tr.State)
	}
	if tr.Direction != DirectionBackward {
		t.Fatalf("expected DirectionBackward; got %d", tr.Direction)
	}
}
