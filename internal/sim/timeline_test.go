package sim

import (
	"testing"
	"time"

	"github.com/nikokozak/train-gap/internal/lines"
)

// Three stations: A -60 s tunnel without service- B -30 s with service- C.
func testLine() *lines.Line {
	return &lines.Line{
		ID: "X",
		Stations: []lines.Station{
			{ID: "A", Dwell: 20 * time.Second},
			{ID: "B", Dwell: 10 * time.Second},
			{ID: "C", Dwell: 20 * time.Second},
		},
		Segments: []lines.Segment{
			{FromID: "A", ToID: "B", Duration: 60 * time.Second, HasService: false},
			{FromID: "B", ToID: "C", Duration: 30 * time.Second, HasService: true},
		},
	}
}

func TestConditionAt(t *testing.T) {
	tunnel := Phase{Kind: Running, Duration: 60 * time.Second}
	shortTunnel := Phase{Kind: Running, Duration: 30 * time.Second}
	connected := Condition{IsConnected: true, Latency: serviceLatency}
	weak := Condition{IsConnected: true, Latency: weakSignalLatency}
	blackout := Condition{IsConnected: false}

	tests := []struct {
		name   string
		phase  Phase
		offset time.Duration
		want   Condition
	}{
		{"at a station", Phase{Kind: AtStation, Duration: 20 * time.Second}, 5 * time.Second, connected},
		{"a run with service", Phase{Kind: Running, Duration: 60 * time.Second, HasService: true}, 30 * time.Second, connected},
		{"just after departure", tunnel, 5 * time.Second, connected},
		{"approaching the tunnel", tunnel, 13 * time.Second, weak},
		{"in the tunnel", tunnel, 16 * time.Second, blackout},
		{"just before service returns", tunnel, 35 * time.Second, blackout},
		{"service back but weak", tunnel, 37 * time.Second, weak},
		{"approaching the station", tunnel, 50 * time.Second, connected},
		{"a tunnel too short to lose service", shortTunnel, 15 * time.Second, weak},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.phase.ConditionAt(test.offset); got != test.want {
				t.Fatalf("ConditionAt(%v) = %+v; want %+v", test.offset, got, test.want)
			}
		})
	}
}

func TestNewTimelineIsARoundTrip(t *testing.T) {
	timeline := NewTimeline(testLine())
	type step struct {
		kind               PhaseKind
		fromIndex, toIndex int
	}
	var got []step
	for _, phase := range timeline.Phases {
		got = append(got, step{phase.Kind, phase.FromIndex, phase.ToIndex})
	}
	want := []step{
		{AtStation, 0, 0}, {Running, 0, 1}, {AtStation, 1, 1}, {Running, 1, 2},
		{AtStation, 2, 2}, {Running, 2, 1}, {AtStation, 1, 1}, {Running, 1, 0},
	}
	if len(got) != len(want) {
		t.Fatalf("phases = %v; want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("phase %d = %v; want %v", index, got[index], want[index])
		}
	}
	// Dwells 20+10+20+10 and runs 60+30+30+60.
	if timeline.LoopDuration() != 240*time.Second {
		t.Fatalf("LoopDuration() = %v; want 4m0s", timeline.LoopDuration())
	}
}

func TestTimelineAtLoops(t *testing.T) {
	timeline := NewTimeline(testLine())
	tests := []struct {
		elapsed    time.Duration
		wantIndex  int
		wantOffset time.Duration
	}{
		{0, 0, 0},
		{25 * time.Second, 1, 5 * time.Second},
		{240 * time.Second, 0, 0},                // One full loop later.
		{265 * time.Second, 1, 5 * time.Second},  // Second loop.
		{239 * time.Second, 7, 59 * time.Second}, // Last second of the loop.
		{-1 * time.Second, 7, 59 * time.Second},  // Negative time wraps backward.
	}
	for _, test := range tests {
		index, offset := timeline.At(test.elapsed)
		if index != test.wantIndex || offset != test.wantOffset {
			t.Fatalf("At(%v) = %d, %v; want %d, %v", test.elapsed, index, offset, test.wantIndex, test.wantOffset)
		}
	}
}

// Every line in the dataset makes a usable timeline.
func TestTimelineForEveryLine(t *testing.T) {
	for _, id := range lines.IDs() {
		timeline := NewTimeline(lines.Get(id))
		if timeline.LoopDuration() <= 0 {
			t.Fatalf("line %s: loop duration %v", id, timeline.LoopDuration())
		}
	}
}
