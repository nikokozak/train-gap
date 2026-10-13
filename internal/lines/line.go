// Package lines holds the subway lines that the simulator rides. The data comes from MTA GTFS
// and the MTA Subway Stations dataset (cmd/lines-gtfs); the iPhone recorder uses the same file.
package lines

import "time"

// Line is one subway service with its stations in ride order.
type Line struct {
	ID       string
	Name     string
	Color    string
	Stations []Station
	Segments []Segment // Segments[i] runs from Stations[i] to Stations[i+1].
}

// Station is one stop on a line.
type Station struct {
	ID        string // GTFS stop ID, e.g. "L06". Recordings use the same IDs.
	Name      string
	ShortName string
	Dwell     time.Duration // How long the train usually stands here.
	Structure string        // "Subway" (underground), "Elevated", "At Grade", ...
}

// Segment is the track between two neighbouring stations.
type Segment struct {
	FromID     string
	ToID       string
	Duration   time.Duration // Scheduled running time from GTFS.
	HasService bool          // False for a tunnel without cell coverage.
}
