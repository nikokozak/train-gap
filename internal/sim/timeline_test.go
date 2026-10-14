package sim

import (
	"testing"
	"time"

	"github.com/nikokozak/train-gap/internal/lines"
)

var (
	testConnected = Condition{IsConnected: true, Latency: 40 * time.Millisecond}
	testBlackout  = Condition{IsConnected: false}
)

// testPhase is one phase whose recorded seconds are all the same condition.
func testPhase(kind PhaseKind, fromIndex, toIndex, seconds int, condition Condition) Phase {
	recorded := make([]Condition, seconds)
	for second := range recorded {
		recorded[second] = condition
	}
	return Phase{Kind: kind, FromIndex: fromIndex, ToIndex: toIndex, IsForward: true,
		Duration: time.Duration(seconds) * time.Second, Recorded: recorded}
}

// Stations A, B, C. A 20 s stop at A, a 60 s tunnel without service to B, a 10 s stop at B,
// a 30 s run with service to C, a 20 s stop at C: 140 s in all.
func testLine() *lines.Line {
	return &lines.Line{ID: "X", Stations: []lines.Station{{ID: "A"}, {ID: "B"}, {ID: "C"}}}
}

func testTimeline() Timeline {
	return NewTimeline([]Phase{
		testPhase(AtStation, 0, 0, 20, testConnected),
		testPhase(Running, 0, 1, 60, testBlackout),
		testPhase(AtStation, 1, 1, 10, testConnected),
		testPhase(Running, 1, 2, 30, testConnected),
		testPhase(AtStation, 2, 2, 20, testConnected),
	})
}

func TestConditionAt(t *testing.T) {
	recorded := Phase{Kind: Running, Duration: 3 * time.Second, Recorded: []Condition{testConnected, testBlackout, testConnected}}
	tests := []struct {
		name   string
		offset time.Duration
		want   Condition
	}{
		{"the first second", 500 * time.Millisecond, testConnected},
		{"the second second", 1500 * time.Millisecond, testBlackout},
		{"past the recording: the last second", 5 * time.Second, testConnected},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := recorded.ConditionAt(test.offset); got != test.want {
				t.Fatalf("ConditionAt(%v) = %+v; want %+v", test.offset, got, test.want)
			}
		})
	}
}

func TestTimelineAtLoops(t *testing.T) {
	timeline := testTimeline()
	if timeline.LoopDuration() != 140*time.Second {
		t.Fatalf("LoopDuration() = %v; want 2m20s", timeline.LoopDuration())
	}
	tests := []struct {
		elapsed    time.Duration
		wantIndex  int
		wantOffset time.Duration
	}{
		{0, 0, 0},
		{25 * time.Second, 1, 5 * time.Second},
		{140 * time.Second, 0, 0},                // One full loop later.
		{165 * time.Second, 1, 5 * time.Second},  // Second loop.
		{139 * time.Second, 4, 19 * time.Second}, // Last second of the loop.
		{-1 * time.Second, 4, 19 * time.Second},  // Negative time wraps backward.
	}
	for _, test := range tests {
		index, offset := timeline.At(test.elapsed)
		if index != test.wantIndex || offset != test.wantOffset {
			t.Fatalf("At(%v) = %d, %v; want %d, %v", test.elapsed, index, offset, test.wantIndex, test.wantOffset)
		}
	}
}
