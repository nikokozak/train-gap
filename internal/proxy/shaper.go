package proxy

import (
	"math/rand"
	"sync"
	"time"

	"train-gap/internal/sim"
)

// Shaper controls network conditions for connections
type Shaper struct {
	mu        sync.RWMutex
	condition sim.NetworkCondition
}

// NewShaper creates a new Shaper with full connectivity
func NewShaper() *Shaper {
	return &Shaper{
		condition: sim.FullConnectivity(),
	}
}

// SetCondition updates the current network condition
func (s *Shaper) SetCondition(c sim.NetworkCondition) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.condition = c
}

// GetCondition returns the current network condition
func (s *Shaper) GetCondition() sim.NetworkCondition {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.condition
}

// ShouldDrop returns true if this packet should be dropped
func (s *Shaper) ShouldDrop() bool {
	s.mu.RLock()
	condition := s.condition
	s.mu.RUnlock()

	if !condition.Connected {
		return true
	}

	if condition.PacketLoss <= 0 {
		return false
	}

	return rand.Float64() < condition.PacketLoss
}

// Delay returns how long to delay this packet
func (s *Shaper) Delay() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.condition.Connected {
		return 0 // Will be dropped anyway
	}

	// Add some jitter to the latency (±20%)
	base := s.condition.Latency
	if base <= 0 {
		return 0
	}

	jitter := float64(base) * 0.2 * (rand.Float64()*2 - 1)
	return base + time.Duration(jitter)
}

// IsBlocked returns true if all traffic should be blocked
func (s *Shaper) IsBlocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.condition.Connected
}
