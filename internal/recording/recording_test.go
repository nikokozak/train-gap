package recording

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRecording writes events as NDJSON, plus any raw lines, and returns the file path.
func writeRecording(t *testing.T, events []any, rawLines ...string) string {
	t.Helper()
	var lines []string
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(encoded))
	}
	lines = append(lines, rawLines...)
	path := filepath.Join(t.TempDir(), "ride.ndjson")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Read must use the exact JSON names the iPhone app writes (RecorderEvents.swift). A wrong tag
// would otherwise skip detection or drop the end station silently.
func TestReadNewRecorderEvents(t *testing.T) {
	events := []any{map[string]any{
		"type": "session_start", "v": 1, "session_id": "test", "started_at": "2026-10-03T12:00:00.250Z",
		"line_id": "X", "from_station_id": "A", "direction": "forward",
		"route_stations": []map[string]string{{"id": "A", "name": "Alpha"}, {"id": "B", "name": "Bravo"}, {"id": "C", "name": "Charlie"}},
	}}
	batches, samples := syntheticRide(stationTunnelStation(phase{15, quietJolt, true}, phase{40, runningJolt, false})...)
	for index, batch := range batches {
		events = append(events,
			struct {
				Type string `json:"type"`
				MotionBatch
			}{"motion", batch},
			map[string]any{"type": "sample", "t_ms": samples[index].TMS, "path": map[string]any{"status": "satisfied"},
				"probe": map[string]any{"ok": samples[index].Probe.OK, "rtt_ms": 50, "reused_connection": true}},
		)
	}
	events = append(events,
		map[string]any{"type": "mark", "t_ms": 85_000},
		map[string]any{"type": "session_end", "t_ms": 175_000, "ended_at": "2026-10-03T12:03:00Z", "end_station_id": "C", "end_station_name": "Charlie"},
	)
	path := writeRecording(t, events, `{"type":"sample","t_ms":176000,"pro`) // Cut off by a crash.

	recording, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if recording.DamagedLines != 1 || len(recording.Motion) != len(batches) || len(recording.Samples) != len(samples) {
		t.Fatalf("damaged %d, motion %d, samples %d; want 1, %d, %d", recording.DamagedLines, len(recording.Motion), len(recording.Samples), len(batches), len(samples))
	}
	if reused := recording.Samples[0].Probe.ReusedConnection; reused == nil || !*reused {
		t.Fatalf("reused_connection not read")
	}
	if recording.EndMS() != 175_000 {
		t.Fatalf("EndMS() = %d; want 175000", recording.EndMS())
	}

	detection := DetectStops(recording, DefaultSettings)
	if detection.Status != StatusMatched || detection.TunnelGaps != 2 || len(detection.Runs) != 2 {
		t.Fatalf("detection = %+v; want matched with 2 tunnel gaps and 2 runs", detection)
	}
	if detection.MarksNearStops != 1 {
		t.Fatalf("marks near stops = %d; want 1", detection.MarksNearStops)
	}
}

// Recordings from before 2026-10: undo removes the last marker.
func TestReadLegacyMarkers(t *testing.T) {
	path := writeRecording(t, []any{
		map[string]any{"type": "station_depart", "t_ms": 1000, "station_id": "A"},
		map[string]any{"type": "station_arrive", "t_ms": 60_000, "station_id": "B"},
		map[string]any{"type": "undo", "t_ms": 61_000},
	})
	recording, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recording.LegacyMarkers) != 1 || recording.LegacyMarkers[0].Kind != "station_depart" {
		t.Fatalf("legacy markers = %+v; want the depart only", recording.LegacyMarkers)
	}
}
