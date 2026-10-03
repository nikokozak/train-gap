package sim

import (
	"math"
	"time"
)

// State represents the current state of the train
type State int

const (
	// AtStation - train is stopped at a station, full connectivity
	AtStation State = iota
	// Departing - train is leaving the station, signal may be degrading
	Departing
	// InTransit - train is between stations (tunnel or underground)
	InTransit
	// Arriving - train is approaching a station, signal may be recovering
	Arriving
)

func (s State) String() string {
	switch s {
	case AtStation:
		return "AT STATION"
	case Departing:
		return "DEPARTING"
	case InTransit:
		return "IN TRANSIT"
	case Arriving:
		return "ARRIVING"
	default:
		return "UNKNOWN"
	}
}

// NetworkCondition represents the current network state
type NetworkCondition struct {
	PacketLoss float64       // 0.0 - 1.0 (percentage as decimal)
	Latency    time.Duration // Additional latency to add
	Connected  bool          // If false, complete blackout
	InTunnel   bool          // Whether we're in a tunnel (for UI)
}

func (c NetworkCondition) Normalized() NetworkCondition {
	if math.IsNaN(c.PacketLoss) || math.IsInf(c.PacketLoss, 0) {
		c.PacketLoss = 0
	}
	if c.PacketLoss < 0 {
		c.PacketLoss = 0
	} else if c.PacketLoss > 1 {
		c.PacketLoss = 1
	}
	if c.Latency < 0 {
		c.Latency = 0
	}
	if !c.Connected {
		c.PacketLoss = 1
		c.InTunnel = true
	}
	return c
}

// FullConnectivity returns a NetworkCondition with full connectivity
func FullConnectivity() NetworkCondition {
	return NetworkCondition{
		PacketLoss: 0,
		Latency:    0,
		Connected:  true,
		InTunnel:   false,
	}.Normalized()
}

// Disconnected returns a NetworkCondition for complete disconnection
func Disconnected() NetworkCondition {
	return NetworkCondition{
		PacketLoss: 1.0,
		Latency:    0,
		Connected:  false,
		InTunnel:   true,
	}.Normalized()
}

// Degraded returns a NetworkCondition with partial connectivity
func Degraded(quality float64) NetworkCondition {
	if math.IsNaN(quality) || math.IsInf(quality, 0) {
		quality = 0
	}
	if quality < 0 {
		quality = 0
	} else if quality > 1 {
		quality = 1
	}
	// quality is 0.0 (worst) to 1.0 (best)
	// invert for packet loss
	loss := 1.0 - quality
	// Scale latency based on quality (worse quality = more latency)
	latency := time.Duration(float64(500*time.Millisecond) * (1.0 - quality))

	return NetworkCondition{
		PacketLoss: loss * 0.6, // Cap at 60% loss for degraded (not tunnel)
		Latency:    latency,
		Connected:  true,
		InTunnel:   false,
	}.Normalized()
}
