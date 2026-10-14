package lines

import "testing"

// Rides find their stations by ID, so every line needs stations, each ID once.
func TestDataset(t *testing.T) {
	if len(IDs()) == 0 {
		t.Fatal("no lines in the embedded dataset")
	}
	for _, id := range IDs() {
		line := Get(id)
		if len(line.Stations) < 2 {
			t.Fatalf("line %s: %d stations; want at least 2", id, len(line.Stations))
		}
		for index, station := range line.Stations {
			if station.ID == "" || station.ShortName == "" {
				t.Fatalf("line %s: station %d has no ID or name: %+v", id, index, station)
			}
			if line.StationIndex(station.ID) != index {
				t.Fatalf("line %s: station %s appears twice", id, station.ID)
			}
		}
	}
}
