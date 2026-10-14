// Package lines holds the subway lines and their stations in ride order. The data comes from MTA
// GTFS (cmd/lines-gtfs); the iPhone recorder uses the same file.
package lines

import "slices"

// Line is one subway service with its stations in ride order.
type Line struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Color    string    `json:"color"`
	Stations []Station `json:"stations"`
}

// Station is one stop on a line.
type Station struct {
	ID        string `json:"id"` // GTFS stop ID, e.g. "L06". Recordings use the same IDs.
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
}

// StationIndex returns the position of the station on the line, or -1.
func (line *Line) StationIndex(stationID string) int {
	return slices.IndexFunc(line.Stations, func(station Station) bool { return station.ID == stationID })
}
