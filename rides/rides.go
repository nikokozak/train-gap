// Package rides holds the recorded rides that ship with Train-Gap, one JSON file per ride in this
// directory. cmd/ride-export makes a ride file from an iPhone recording; docs/recordings.md says
// how to add one.
package rides

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nikokozak/train-gap/internal/lines"
	"github.com/nikokozak/train-gap/internal/sim"
)

//go:embed *.json
var files embed.FS

// Ride is one recorded trip in one direction.
type Ride struct {
	Name   string `json:"-"` // The file name without ".json".
	LineID string `json:"line_id"`
	// "2026-10". The day and time are left out: a daily route shows where the rider lives or works.
	Month   string  `json:"month"`
	Carrier string  `json:"carrier"` // As the rider chose it: AT&T, Verizon, T-Mobile, or Other.
	Phases  []Phase `json:"phases"`
}

// Phase is one station stop (StationID is set) or one run between neighbouring stations (FromID
// and ToID are set).
type Phase struct {
	StationID string `json:"station,omitempty"`
	FromID    string `json:"from,omitempty"`
	ToID      string `json:"to,omitempty"`
	// One value per second: the delay the proxy adds in each direction, in milliseconds (half the
	// measured round trip), or NoService.
	DelayMS []int `json:"delay_ms"`
}

// NoService marks a second in which the probe failed.
const NoService = -1

func (phase Phase) isStop() bool {
	return phase.StationID != ""
}

// FirstStationID is where the ride starts.
func (ride Ride) FirstStationID() string {
	first := ride.Phases[0]
	if first.isStop() {
		return first.StationID
	}
	return first.FromID
}

// LastStationID is where the ride ends.
func (ride Ride) LastStationID() string {
	last := ride.Phases[len(ride.Phases)-1]
	if last.isStop() {
		return last.StationID
	}
	return last.ToID
}

// Duration is how long the ride takes.
func (ride Ride) Duration() time.Duration {
	seconds := 0
	for _, phase := range ride.Phases {
		seconds += len(phase.DelayMS)
	}
	return time.Duration(seconds) * time.Second
}

// Route names the ride's end stations: "Nostrand Av → Hoyt St".
func (ride Ride) Route(line *lines.Line) string {
	name := func(stationID string) string {
		if index := line.StationIndex(stationID); index >= 0 {
			return line.Stations[index].ShortName
		}
		return stationID
	}
	return name(ride.FirstStationID()) + " → " + name(ride.LastStationID())
}

// Source says where the data comes from, for people: "recorded Oct 2026 · Verizon".
func (ride Ride) Source() string {
	month := ride.Month
	if parsed, err := time.Parse("2006-01", ride.Month); err == nil {
		month = parsed.Format("Jan 2006")
	}
	carrier := ride.Carrier
	switch carrier {
	case "", "Other":
		carrier = "other carrier"
	}
	return fmt.Sprintf("recorded %s · %s", month, carrier)
}

// embeddedRides decodes the ride files once, sorted by name. A broken file means a broken build,
// not a runtime condition, so it panics; TestEmbeddedRides catches it first.
var embeddedRides = sync.OnceValue(func() []Ride {
	entries, err := files.ReadDir(".")
	if err != nil {
		panic(fmt.Sprintf("rides: %v", err))
	}
	var all []Ride
	for _, entry := range entries {
		data, err := files.ReadFile(entry.Name())
		if err != nil {
			panic(fmt.Sprintf("rides: %v", err))
		}
		ride, err := Parse(strings.TrimSuffix(entry.Name(), ".json"), data)
		if err != nil {
			panic(fmt.Sprintf("rides: %s: %v", entry.Name(), err))
		}
		all = append(all, ride)
	}
	slices.SortFunc(all, func(a, b Ride) int { return strings.Compare(a.Name, b.Name) })
	return all
})

// All returns every ride that ships with Train-Gap, sorted by name.
func All() []Ride {
	return slices.Clone(embeddedRides())
}

// Get returns the shipped ride with this name.
func Get(name string) (Ride, bool) {
	index := slices.IndexFunc(embeddedRides(), func(ride Ride) bool { return ride.Name == name })
	if index < 0 {
		return Ride{}, false
	}
	return embeddedRides()[index], true
}

// Parse reads a ride file.
func Parse(name string, data []byte) (Ride, error) {
	var ride Ride
	if err := json.Unmarshal(data, &ride); err != nil {
		return Ride{}, err
	}
	ride.Name = name
	if ride.LineID == "" || len(ride.Phases) == 0 {
		return Ride{}, errors.New("a ride needs a line_id and phases")
	}
	for index, phase := range ride.Phases {
		isRun := phase.FromID != "" && phase.ToID != ""
		if phase.isStop() == isRun || len(phase.DelayMS) == 0 {
			return Ride{}, fmt.Errorf("phase %d: want a station or a from and to, and at least one second", index)
		}
	}
	return ride, nil
}

// Encode writes a ride file with one phase per line, so a diff shows which phases changed.
func Encode(ride Ride) ([]byte, error) {
	header, err := json.Marshal(struct {
		LineID  string `json:"line_id"`
		Month   string `json:"month"`
		Carrier string `json:"carrier"`
	}{ride.LineID, ride.Month, ride.Carrier})
	if err != nil {
		return nil, err
	}
	var file bytes.Buffer
	file.Write(header[:len(header)-1]) // Without the closing brace.
	file.WriteString(",\n\"phases\": [\n")
	for index, phase := range ride.Phases {
		encoded, err := json.Marshal(phase)
		if err != nil {
			return nil, err
		}
		file.Write(encoded)
		if index < len(ride.Phases)-1 {
			file.WriteString(",")
		}
		file.WriteString("\n")
	}
	file.WriteString("]}\n")
	return file.Bytes(), nil
}

// RoundTrip is the timeline for a ride: there and back, repeated. The way back is a shipped ride
// in the other direction between the same two stations, if there is one; otherwise it is this
// ride played backward.
func RoundTrip(ride Ride, line *lines.Line) (sim.Timeline, error) {
	there, err := toPhases(ride, line, ride.Source())
	if err != nil {
		return sim.Timeline{}, fmt.Errorf("ride %s: %w", ride.Name, err)
	}
	back, found := returnRide(ride, embeddedRides())
	backSource := back.Source()
	if !found {
		back, backSource = reversed(ride), ride.Source()+" · played backward"
	}
	backPhases, err := toPhases(back, line, backSource)
	if err != nil {
		return sim.Timeline{}, fmt.Errorf("ride %s: %w", back.Name, err)
	}
	return sim.NewTimeline(append(there, backPhases...)), nil
}

// returnRide finds a ride on the same line from this ride's last station to its first.
func returnRide(ride Ride, candidates []Ride) (Ride, bool) {
	for _, candidate := range candidates {
		if candidate.LineID == ride.LineID && candidate.Name != ride.Name &&
			candidate.FirstStationID() == ride.LastStationID() && candidate.LastStationID() == ride.FirstStationID() {
			return candidate, true
		}
	}
	return Ride{}, false
}

// reversed plays a ride backward: last phase first, each phase's seconds in reverse.
func reversed(ride Ride) Ride {
	backward := ride
	backward.Phases = make([]Phase, len(ride.Phases))
	for index, phase := range ride.Phases {
		delays := slices.Clone(phase.DelayMS)
		slices.Reverse(delays)
		backward.Phases[len(ride.Phases)-1-index] = Phase{StationID: phase.StationID, FromID: phase.ToID, ToID: phase.FromID, DelayMS: delays}
	}
	return backward
}

// toPhases places a ride's phases on the line.
func toPhases(ride Ride, line *lines.Line, source string) ([]sim.Phase, error) {
	if ride.LineID != line.ID {
		return nil, fmt.Errorf("the ride is on the %s, not the %s", ride.LineID, line.ID)
	}
	isForward := line.StationIndex(ride.LastStationID()) > line.StationIndex(ride.FirstStationID())

	var phases []sim.Phase
	for _, ridePhase := range ride.Phases {
		phase := sim.Phase{
			Kind:       sim.Running,
			FromIndex:  line.StationIndex(ridePhase.FromID),
			ToIndex:    line.StationIndex(ridePhase.ToID),
			IsForward:  isForward,
			Duration:   time.Duration(len(ridePhase.DelayMS)) * time.Second,
			HasService: !slices.Contains(ridePhase.DelayMS, NoService),
			Source:     source,
		}
		if ridePhase.isStop() {
			phase.Kind = sim.AtStation
			phase.FromIndex = line.StationIndex(ridePhase.StationID)
			phase.ToIndex = phase.FromIndex
		}
		if phase.FromIndex < 0 || phase.ToIndex < 0 {
			return nil, fmt.Errorf("a station of %+v is not on the %s", ridePhase, line.ID)
		}
		if phase.Kind == sim.Running && (phase.ToIndex-phase.FromIndex)*(phase.ToIndex-phase.FromIndex) != 1 {
			return nil, fmt.Errorf("the run %s -> %s does not join neighbouring stations", ridePhase.FromID, ridePhase.ToID)
		}
		for _, delayMS := range ridePhase.DelayMS {
			phase.Recorded = append(phase.Recorded, condition(delayMS))
		}
		phases = append(phases, phase)
	}
	return phases, nil
}

func condition(delayMS int) sim.Condition {
	if delayMS == NoService {
		return sim.Condition{IsConnected: false}
	}
	return sim.Condition{IsConnected: true, Latency: time.Duration(delayMS) * time.Millisecond}
}
