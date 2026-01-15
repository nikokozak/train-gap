package sim

import (
	"time"
	"train-gap/internal/lines"
)

// Direction constants
const (
	DirectionForward  = 1  // Towards end of line (higher index)
	DirectionBackward = -1 // Towards start of line (lower index)
)

// Train represents a train running on a line
type Train struct {
	Line         *lines.Line
	StationIndex int           // Current station index (where we are or just left)
	Direction    int           // 1 = forward, -1 = backward
	State        State         // Current state
	Progress     float64       // 0.0 - 1.0 progress through current state/segment
	Speed        float64       // Speed multiplier (1.0 = normal)
	Paused       bool          // Whether simulation is paused
	ManualMode   bool          // Whether in manual control mode
	stateStart   time.Time     // When current state began
	stateDur     time.Duration // Duration of current state
}

// NewTrain creates a new train on the given line
func NewTrain(line *lines.Line) *Train {
	t := &Train{
		Line:         line,
		StationIndex: 0,
		Direction:    DirectionForward,
		State:        AtStation,
		Progress:     0,
		Speed:        1.0,
		Paused:       false,
		ManualMode:   false,
		stateStart:   time.Now(),
	}
	// Set initial dwell time
	if len(line.Stations) > 0 {
		t.stateDur = line.Stations[0].DwellTime
	} else {
		t.stateDur = 20 * time.Second
	}
	return t
}

// Update advances the simulation by dt and returns the current network condition
func (t *Train) Update(dt time.Duration) NetworkCondition {
	if t.Paused || t.ManualMode {
		return t.currentCondition()
	}

	// Apply speed multiplier
	effectiveDt := time.Duration(float64(dt) * t.Speed)

	// Advance progress
	if t.stateDur > 0 {
		t.Progress += float64(effectiveDt) / float64(t.stateDur)
	} else {
		// Prevent division by zero - force state advance
		t.Progress = 1.0
	}

	// Handle state transitions - allow multiple transitions per tick if needed
	// (e.g., at high speed, we might skip through short states)
	maxTransitions := 5 // Safety limit to prevent infinite loops
	for t.Progress >= 1.0 && maxTransitions > 0 {
		overflow := t.Progress - 1.0
		t.advanceState()
		// Carry over the overflow progress
		if t.stateDur > 0 {
			t.Progress = overflow * float64(effectiveDt) / float64(t.stateDur)
			if t.Progress > 1.0 {
				t.Progress = 1.0 // Cap it, let next iteration handle it
			}
		}
		maxTransitions--
	}

	return t.currentCondition()
}

// advanceState moves to the next state
func (t *Train) advanceState() {
	t.Progress = 0
	t.stateStart = time.Now()

	switch t.State {
	case AtStation:
		// Before departing, check if we need to reverse at endpoint
		t.checkAndReverseAtEndpoint()

		// Start departing
		t.State = Departing
		t.stateDur = 3 * time.Second // Shorter departure phase

	case Departing:
		// Now in transit
		t.State = InTransit
		segment := t.currentSegment()
		if segment != nil {
			// Scale down long tunnel times for better UX
			// Real time would be boring - use 1/3 of actual duration
			t.stateDur = segment.Duration / 3
			if t.stateDur < 10*time.Second {
				t.stateDur = 10 * time.Second
			}
			if t.stateDur > 60*time.Second {
				t.stateDur = 60 * time.Second // Cap at 1 minute
			}
		} else {
			t.stateDur = 20 * time.Second // Default
		}

	case InTransit:
		// Start arriving
		t.State = Arriving
		t.stateDur = 3 * time.Second // Shorter arrival phase

	case Arriving:
		// Move to next station
		t.moveToNextStation()
		t.State = AtStation
		station := t.CurrentStation()
		if station != nil {
			// Scale down dwell times too
			t.stateDur = station.DwellTime / 2
			if t.stateDur < 5*time.Second {
				t.stateDur = 5 * time.Second
			}
		} else {
			t.stateDur = 10 * time.Second
		}
	}
}

// checkAndReverseAtEndpoint reverses direction if we're at an endpoint
func (t *Train) checkAndReverseAtEndpoint() {
	if t.Line == nil {
		return
	}

	nextIndex := t.StationIndex + t.Direction

	// If next station would be out of bounds, reverse direction now
	if nextIndex < 0 || nextIndex >= len(t.Line.Stations) {
		t.Direction = -t.Direction
	}
}

// moveToNextStation advances to the next station
func (t *Train) moveToNextStation() {
	if t.Line == nil {
		return
	}

	nextIndex := t.StationIndex + t.Direction

	// Safety check - should have already reversed in checkAndReverseAtEndpoint
	// but handle edge cases
	if nextIndex < 0 {
		t.Direction = DirectionForward
		nextIndex = 0
	} else if nextIndex >= len(t.Line.Stations) {
		t.Direction = DirectionBackward
		nextIndex = len(t.Line.Stations) - 1
	}

	t.StationIndex = nextIndex
}

// currentCondition returns the network condition for current state
func (t *Train) currentCondition() NetworkCondition {
	switch t.State {
	case AtStation:
		station := t.CurrentStation()
		if station != nil && station.HasService {
			return FullConnectivity()
		}
		// Station without service - still connected but degraded
		return Degraded(0.7)

	case Departing:
		// Gradually degrade as we leave
		segment := t.currentSegment()
		if segment != nil && segment.IsTunnel {
			// Entering tunnel - rapid degradation
			quality := 1.0 - t.Progress
			if t.Progress > 0.5 {
				return Disconnected()
			}
			return Degraded(quality)
		}
		// Normal departure - slight degradation
		return Degraded(0.9 - t.Progress*0.3)

	case InTransit:
		segment := t.currentSegment()
		if segment == nil {
			// Unknown segment - heavy degradation
			return NetworkCondition{
				PacketLoss: 0.8,
				Latency:    300 * time.Millisecond,
				Connected:  true,
				InTunnel:   false,
			}
		}
		if segment.IsTunnel {
			return Disconnected()
		}
		// Underground transit has heavy packet loss (70-90%)
		// Use segment degradation to modulate, but floor is high
		baseLoss := 0.7 + (1.0-segment.Degradation)*0.2 // 0.7 to 0.9
		latency := time.Duration(200+int((1.0-segment.Degradation)*300)) * time.Millisecond
		return NetworkCondition{
			PacketLoss: baseLoss,
			Latency:    latency,
			Connected:  true,
			InTunnel:   false,
		}

	case Arriving:
		// Gradually recover as we approach
		segment := t.currentSegment()
		if segment != nil && segment.IsTunnel {
			// Exiting tunnel - gradual recovery
			if t.Progress < 0.5 {
				return Disconnected()
			}
			quality := (t.Progress - 0.5) * 2 // 0 to 1 over second half
			return Degraded(quality)
		}
		// Normal arrival - improving
		return Degraded(0.6 + t.Progress*0.4)

	default:
		return FullConnectivity()
	}
}

// currentSegment returns the segment we're currently traversing
func (t *Train) currentSegment() *lines.Segment {
	if t.Line == nil {
		return nil
	}

	from := t.CurrentStation()
	to := t.NextStation()
	if from == nil || to == nil {
		return nil
	}

	return t.Line.GetSegment(from.ID, to.ID)
}

// CurrentStation returns the current (or last departed) station
func (t *Train) CurrentStation() *lines.Station {
	if t.Line == nil || t.StationIndex < 0 || t.StationIndex >= len(t.Line.Stations) {
		return nil
	}
	return &t.Line.Stations[t.StationIndex]
}

// NextStation returns the station we're heading towards
func (t *Train) NextStation() *lines.Station {
	if t.Line == nil {
		return nil
	}

	nextIndex := t.StationIndex + t.Direction
	if nextIndex < 0 || nextIndex >= len(t.Line.Stations) {
		return nil
	}
	return &t.Line.Stations[nextIndex]
}

// TogglePause toggles the paused state
func (t *Train) TogglePause() {
	t.Paused = !t.Paused
}

// SetSpeed sets the simulation speed
func (t *Train) SetSpeed(speed float64) {
	if speed < 0.1 {
		speed = 0.1
	}
	if speed > 10.0 {
		speed = 10.0
	}
	t.Speed = speed
}

// IncreaseSpeed increases simulation speed
func (t *Train) IncreaseSpeed() {
	t.SetSpeed(t.Speed * 1.5)
}

// DecreaseSpeed decreases simulation speed
func (t *Train) DecreaseSpeed() {
	t.SetSpeed(t.Speed / 1.5)
}

// ToggleManualMode toggles manual control mode
func (t *Train) ToggleManualMode() {
	t.ManualMode = !t.ManualMode
}

// ManualAdvance manually advances to next state (for manual mode)
func (t *Train) ManualAdvance() {
	if t.ManualMode {
		t.Progress = 1.0
		t.advanceState()
	}
}

// DisplayPosition returns the position along the track for display purposes
// Returns (stationIndex, progress to next station) where progress is 0-1
func (t *Train) DisplayPosition() (int, float64) {
	switch t.State {
	case AtStation:
		return t.StationIndex, 0
	case Departing:
		return t.StationIndex, t.Progress * 0.1 // First 10% of segment
	case InTransit:
		return t.StationIndex, 0.1 + t.Progress*0.8 // Middle 80%
	case Arriving:
		return t.StationIndex, 0.9 + t.Progress*0.1 // Last 10%
	default:
		return t.StationIndex, 0
	}
}

// IsInTunnel returns whether the train is currently in a tunnel
func (t *Train) IsInTunnel() bool {
	if t.State == AtStation {
		return false
	}
	segment := t.currentSegment()
	return segment != nil && segment.IsTunnel
}
