package lines

import "testing"

func TestLTrainIntegrity(t *testing.T) {
	line := GetLine("L")
	if line == nil {
		t.Fatalf("expected GetLine(%q) to return a line", "L")
	}
	if len(line.Stations) < 2 {
		t.Fatalf("expected at least 2 stations; got %d", len(line.Stations))
	}

	seen := make(map[string]struct{}, len(line.Stations))
	for i, st := range line.Stations {
		if st.ID == "" {
			t.Fatalf("station %d has empty ID", i)
		}
		if _, ok := seen[st.ID]; ok {
			t.Fatalf("duplicate station ID %q", st.ID)
		}
		seen[st.ID] = struct{}{}
	}

	for i := 0; i < len(line.Stations)-1; i++ {
		from := line.Stations[i]
		to := line.Stations[i+1]
		seg := line.GetSegment(from.ID, to.ID)
		if seg == nil {
			t.Fatalf("missing segment %q -> %q", from.ID, to.ID)
		}
		if seg.Duration <= 0 {
			t.Fatalf("segment %q -> %q has non-positive duration %v", from.ID, to.ID, seg.Duration)
		}
		if seg.Degradation < 0 || seg.Degradation > 1 {
			t.Fatalf("segment %q -> %q has degradation out of range: %f", from.ID, to.ID, seg.Degradation)
		}
	}
}

func TestAvailableLinesIncludesL(t *testing.T) {
	found := false
	for _, id := range AvailableLines() {
		if id == "L" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected AvailableLines to include %q", "L")
	}
}
