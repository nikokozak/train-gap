package lines

import "time"

// Line represents a subway line with its stations and segments
type Line struct {
	ID       string    // Single letter/number identifier (e.g., "L", "A", "7")
	Name     string    // Full name (e.g., "Canarsie", "8th Avenue")
	Color    string    // Hex color code
	Stations []Station // Ordered list of stations
	Segments []Segment // Segments between stations
}

// Station represents a subway station on a line
type Station struct {
	ID         string        // Short identifier
	Name       string        // Full name
	ShortName  string        // Abbreviated name for display
	HasService bool          // Whether cell service is available at platform
	DwellTime  time.Duration // How long the train stops (default ~25s)
}

// Segment represents the track between two adjacent stations
type Segment struct {
	FromID      string        // Starting station ID
	ToID        string        // Ending station ID
	IsTunnel    bool          // Complete blackout tunnel
	Duration    time.Duration // Time to traverse this segment
	Degradation float64       // Signal quality 0.0 (dead) to 1.0 (full) for non-tunnel underground
}

// GetSegment returns the segment between two stations
func (l *Line) GetSegment(fromID, toID string) *Segment {
	for i := range l.Segments {
		if l.Segments[i].FromID == fromID && l.Segments[i].ToID == toID {
			return &l.Segments[i]
		}
		// Also check reverse direction
		if l.Segments[i].FromID == toID && l.Segments[i].ToID == fromID {
			return &l.Segments[i]
		}
	}
	return nil
}

// StationByID returns a station by its ID
func (l *Line) StationByID(id string) *Station {
	for i := range l.Stations {
		if l.Stations[i].ID == id {
			return &l.Stations[i]
		}
	}
	return nil
}

// StationIndex returns the index of a station by ID
func (l *Line) StationIndex(id string) int {
	for i, s := range l.Stations {
		if s.ID == id {
			return i
		}
	}
	return -1
}
