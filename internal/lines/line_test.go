package lines

import "testing"

// The simulator walks Segments by index, so every line must have one segment per pair of
// neighbouring stations, in order.
func TestDataset(t *testing.T) {
	if len(IDs()) == 0 {
		t.Fatal("no lines in the embedded dataset")
	}
	for _, id := range IDs() {
		line := Get(id)
		if len(line.Stations) < 2 {
			t.Fatalf("line %s: %d stations; want at least 2", id, len(line.Stations))
		}
		if len(line.Segments) != len(line.Stations)-1 {
			t.Fatalf("line %s: %d segments for %d stations", id, len(line.Segments), len(line.Stations))
		}
		for index, segment := range line.Segments {
			if segment.FromID != line.Stations[index].ID || segment.ToID != line.Stations[index+1].ID {
				t.Fatalf("line %s: segment %d is %s->%s; want %s->%s", id, index,
					segment.FromID, segment.ToID, line.Stations[index].ID, line.Stations[index+1].ID)
			}
			if segment.Duration <= 0 {
				t.Fatalf("line %s: segment %s->%s has no duration", id, segment.FromID, segment.ToID)
			}
		}
	}
}

// The L's East River (Canarsie) tunnel has service; the tunnels under 14 St do not.
func TestLTrainTunnels(t *testing.T) {
	line := Get("L")
	if line == nil {
		t.Fatal(`Get("L") = nil`)
	}
	for _, segment := range line.Segments {
		switch {
		case segment.FromID == "L06" && segment.ToID == "L08" && !segment.HasService:
			t.Fatal("Canarsie tunnel (1 Av - Bedford Av) has no service; want service")
		case segment.FromID == "L05" && segment.ToID == "L06" && segment.HasService:
			t.Fatal("3 Av - 1 Av has service; want a tunnel without service")
		}
	}
}
