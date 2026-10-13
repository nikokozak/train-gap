package lines

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

//go:embed lines.json
var embeddedLinesJSON []byte

// The JSON layout written by cmd/lines-gtfs.
type bundleJSON struct {
	Lines []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Color    string `json:"color"`
		Stations []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			ShortName string `json:"short_name"`
			DwellMS   int64  `json:"dwell_ms"`
			Structure string `json:"structure"`
		} `json:"stations"`
		Segments []struct {
			FromID     string `json:"from_id"`
			ToID       string `json:"to_id"`
			DurationMS int64  `json:"duration_ms"`
			HasService bool   `json:"has_service"`
		} `json:"segments"`
	} `json:"lines"`
}

// linesByID decodes the embedded dataset once. A decode error means a broken build, not a
// runtime condition, so it panics; TestDataset catches it first.
var linesByID = sync.OnceValue(func() map[string]*Line {
	var bundle bundleJSON
	if err := json.Unmarshal(embeddedLinesJSON, &bundle); err != nil {
		panic(fmt.Sprintf("lines: embedded lines.json: %v", err))
	}
	byID := make(map[string]*Line, len(bundle.Lines))
	for _, lineJSON := range bundle.Lines {
		line := &Line{ID: lineJSON.ID, Name: lineJSON.Name, Color: lineJSON.Color}
		for _, station := range lineJSON.Stations {
			line.Stations = append(line.Stations, Station{
				ID:        station.ID,
				Name:      station.Name,
				ShortName: station.ShortName,
				Dwell:     time.Duration(station.DwellMS) * time.Millisecond,
				Structure: station.Structure,
			})
		}
		for _, segment := range lineJSON.Segments {
			line.Segments = append(line.Segments, Segment{
				FromID:     segment.FromID,
				ToID:       segment.ToID,
				Duration:   time.Duration(segment.DurationMS) * time.Millisecond,
				HasService: segment.HasService,
			})
		}
		byID[line.ID] = line
	}
	return byID
})

// Get returns the line with this ID ("L", "3", ...), or nil.
func Get(id string) *Line {
	return linesByID()[id]
}

// IDs returns every line ID, sorted.
func IDs() []string {
	ids := make([]string, 0, len(linesByID()))
	for id := range linesByID() {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
