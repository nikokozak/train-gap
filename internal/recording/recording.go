// Package recording reads rides recorded with the iPhone recorder (schemas/recording.v1.schema.json),
// finds their stops, and turns them into timelines the simulator can replay.
package recording

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// SessionStart is the first event: the route and the recorder settings.
type SessionStart struct {
	SessionID                 string   `json:"session_id"`
	StartedAt                 string   `json:"started_at"`
	LineID                    string   `json:"line_id"`
	LineName                  string   `json:"line_name"`
	FromStationID             string   `json:"from_station_id"`
	FromStationName           string   `json:"from_station_name"`
	ToStationID               string   `json:"to_station_id"`
	ToStationName             string   `json:"to_station_name"`
	Direction                 string   `json:"direction"`
	DirectionLabel            string   `json:"direction_label"`
	ProbeURL                  string   `json:"probe_url"`
	ProbeURLs                 []string `json:"probe_urls"`
	SampleIntervalInTransitMS *int     `json:"sample_interval_in_transit_ms"`
	SampleIntervalAtStationMS *int     `json:"sample_interval_at_station_ms"`
	// RouteStations lists the stations in ride order, starting at the start station.
	// Recorder builds from 2026-10 write it; stop labels come from it.
	RouteStations []RouteStation `json:"route_stations"`
}

// RouteStation is one station of the route, by GTFS stop ID.
type RouteStation struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Sample is one network probe. TMS is when the probe started.
type Sample struct {
	TMS  int64 `json:"t_ms"`
	Path struct {
		Status    string  `json:"status"`
		Interface *string `json:"interface"`
	} `json:"path"`
	Probe struct {
		OK         bool    `json:"ok"`
		RTTMS      *int64  `json:"rtt_ms"`
		HTTPStatus *int    `json:"http_status"`
		Error      *string `json:"error"`
		// False when the probe opened a new connection, so RTTMS includes DNS, TCP, and TLS setup.
		ReusedConnection *bool `json:"reused_connection"`
	} `json:"probe"`
}

// MotionBatch is one `motion` event: about one second of accelerometer readings
// without gravity, in milli-g.
type MotionBatch struct {
	TMS     int64 `json:"t_ms"`
	Hz      int   `json:"hz"`
	XMilliG []int `json:"x_mg"`
	YMilliG []int `json:"y_mg"`
	ZMilliG []int `json:"z_mg"`
}

// Mark is a `mark` event: the rider tapped Mark (optional, usually when the doors open).
type Mark struct {
	TMS int64 `json:"t_ms"`
}

// SessionEnd is the last event.
type SessionEnd struct {
	TMS        *int64 `json:"t_ms"` // When the rider tapped Stop.
	EndedAt    string `json:"ended_at"`
	Successful *bool  `json:"successful"`
	Note       string `json:"note"`
	// Where the rider got off. Empty when the rider chose "Not sure".
	EndStationID   string `json:"end_station_id"`
	EndStationName string `json:"end_station_name"`
}

// LegacyMarker is a station_depart or station_arrive event. Recorder builds before 2026-10 wrote
// these instead of motion; the rider tapped them at every station.
type LegacyMarker struct {
	Kind        string `json:"type"` // "station_depart" or "station_arrive"
	TMS         int64  `json:"t_ms"`
	StationID   string `json:"station_id"`
	StationName string `json:"station_name"`
}

// Recording is one ride file.
type Recording struct {
	Start         SessionStart
	Samples       []Sample
	Motion        []MotionBatch
	Marks         []Mark
	End           SessionEnd
	LegacyMarkers []LegacyMarker // Undo events are already applied.
	// Lines that are not JSON, for example the last line of a ride cut off by a crash. They are skipped.
	DamagedLines int
}

// EndMS is when the ride ends: when the rider tapped Stop, or else the end of the motion data.
func (recording Recording) EndMS() int64 {
	if recording.End.TMS != nil {
		return *recording.End.TMS
	}
	endMS := int64(0)
	for _, batch := range recording.Motion {
		endMS = max(endMS, batch.TMS+batchDurationMS)
	}
	return endMS
}

// Read reads a recording. Events of unknown types are ignored.
func Read(path string) (Recording, error) {
	file, err := os.Open(path)
	if err != nil {
		return Recording{}, err
	}
	defer file.Close()

	var recording Recording
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // A motion event is about 1 KB.
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var event struct {
			Type  string `json:"type"`
			Count *int   `json:"count"` // undo only
		}
		if json.Unmarshal(line, &event) != nil {
			recording.DamagedLines++
			continue
		}

		var target any
		switch event.Type {
		case "session_start":
			target = &recording.Start
		case "session_end":
			target = &recording.End
		case "sample":
			recording.Samples = append(recording.Samples, Sample{})
			target = &recording.Samples[len(recording.Samples)-1]
		case "motion":
			recording.Motion = append(recording.Motion, MotionBatch{})
			target = &recording.Motion[len(recording.Motion)-1]
		case "mark":
			recording.Marks = append(recording.Marks, Mark{})
			target = &recording.Marks[len(recording.Marks)-1]
		case "station_depart", "station_arrive":
			recording.LegacyMarkers = append(recording.LegacyMarkers, LegacyMarker{})
			target = &recording.LegacyMarkers[len(recording.LegacyMarkers)-1]
		case "undo":
			removeCount := 1
			if event.Count != nil && *event.Count > 0 {
				removeCount = *event.Count
			}
			removeCount = min(removeCount, len(recording.LegacyMarkers))
			recording.LegacyMarkers = recording.LegacyMarkers[:len(recording.LegacyMarkers)-removeCount]
			continue
		default:
			continue
		}
		if err := json.Unmarshal(line, target); err != nil {
			return Recording{}, fmt.Errorf("decode %s: %w", event.Type, err)
		}
	}
	return recording, scanner.Err()
}
