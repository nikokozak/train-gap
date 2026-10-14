package rides

import (
	"reflect"
	"testing"

	"github.com/nikokozak/train-gap/internal/lines"
	"github.com/nikokozak/train-gap/internal/sim"
)

// Every shipped ride must play on its line: known stations, runs between neighbours.
func TestEmbeddedRides(t *testing.T) {
	if len(All()) == 0 {
		t.Fatal("no rides in rides/")
	}
	for _, ride := range All() {
		line := lines.Get(ride.LineID)
		if line == nil {
			t.Fatalf("ride %s: line %q is not in the station dataset", ride.Name, ride.LineID)
		}
		timeline, err := RoundTrip(ride, line)
		if err != nil {
			t.Fatal(err)
		}
		if timeline.LoopDuration() <= ride.Duration() {
			t.Fatalf("ride %s: a round trip of %v for a %v ride; want there and back", ride.Name, timeline.LoopDuration(), ride.Duration())
		}
	}
}

func TestEncodeParse(t *testing.T) {
	ride := Ride{Name: "x", LineID: "X", Month: "2026-10", Carrier: "Verizon", Phases: []Phase{
		{StationID: "A", DelayMS: []int{40, 41}},
		{FromID: "A", ToID: "B", DelayMS: []int{40, NoService, 500}},
	}}
	data, err := Encode(ride)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse("x", data)
	if err != nil {
		t.Fatalf("Parse(Encode(ride)): %v\n%s", err, data)
	}
	if !reflect.DeepEqual(parsed, ride) {
		t.Fatalf("Parse(Encode(ride)) = %+v; want %+v", parsed, ride)
	}
	if _, err := Parse("bad", []byte(`{"line_id":"X","phases":[{"station":"A","from":"A","to":"B","delay_ms":[1]}]}`)); err == nil {
		t.Fatal("Parse accepted a phase that is both a stop and a run")
	}
}

// A to C, and the way back: the recorded return ride if there is one, else the ride reversed.
func TestTheWayBack(t *testing.T) {
	there := Ride{Name: "there", LineID: "X", Phases: []Phase{
		{StationID: "A", DelayMS: []int{1}},
		{FromID: "A", ToID: "B", DelayMS: []int{2, 3}},
		{StationID: "B", DelayMS: []int{4}},
		{FromID: "B", ToID: "C", DelayMS: []int{5, NoService}},
	}}
	back := Ride{Name: "back", LineID: "X", Phases: []Phase{{FromID: "C", ToID: "B", DelayMS: []int{7}}, {FromID: "B", ToID: "A", DelayMS: []int{8}}}}
	elsewhere := Ride{Name: "elsewhere", LineID: "X", Phases: []Phase{{FromID: "C", ToID: "B", DelayMS: []int{9}}}}

	if found, ok := returnRide(there, []Ride{elsewhere, there, back}); !ok || found.Name != "back" {
		t.Fatalf("returnRide() = %s, %v; want back", found.Name, ok)
	}
	if _, ok := returnRide(there, []Ride{there, elsewhere}); ok {
		t.Fatal("returnRide() found a ride that does not go back to A")
	}

	wantReversed := []Phase{
		{FromID: "C", ToID: "B", DelayMS: []int{NoService, 5}},
		{StationID: "B", DelayMS: []int{4}},
		{FromID: "B", ToID: "A", DelayMS: []int{3, 2}},
		{StationID: "A", DelayMS: []int{1}},
	}
	if got := reversed(there).Phases; !reflect.DeepEqual(got, wantReversed) {
		t.Fatalf("reversed() = %+v; want %+v", got, wantReversed)
	}
}

func TestToPhases(t *testing.T) {
	line := &lines.Line{ID: "X", Stations: []lines.Station{{ID: "Z"}, {ID: "A"}, {ID: "B"}}}
	ride := Ride{LineID: "X", Phases: []Phase{{StationID: "A", DelayMS: []int{40}}, {FromID: "A", ToID: "B", DelayMS: []int{40, NoService}}}}
	phases, err := toPhases(ride, line, "test")
	if err != nil {
		t.Fatal(err)
	}
	run := phases[1]
	if run.Kind != sim.Running || run.FromIndex != 1 || run.ToIndex != 2 || !run.IsForward || run.HasService || len(run.Recorded) != 2 {
		t.Fatalf("run = %+v; want A(1) -> B(2), forward, a second without service", run)
	}

	skipping := Ride{LineID: "X", Phases: []Phase{{FromID: "Z", ToID: "B", DelayMS: []int{40}}}}
	if _, err := toPhases(skipping, line, "test"); err == nil {
		t.Fatal("toPhases accepted a run that skips a station")
	}
}
