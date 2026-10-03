package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"train-gap/internal/lines"
)

type exportLine struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Color    string          `json:"color"`
	Stations []exportStation `json:"stations"`
	Segments []exportSegment `json:"segments"`
}

type exportStation struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ShortName  string `json:"short_name"`
	HasService bool   `json:"has_service"`
	DwellMS    int64  `json:"dwell_ms"`
}

type exportSegment struct {
	FromID      string  `json:"from_id"`
	ToID        string  `json:"to_id"`
	IsTunnel    bool    `json:"is_tunnel"`
	DurationMS  int64   `json:"duration_ms"`
	Degradation float64 `json:"degradation"`
}

type exportBundle struct {
	GeneratedAt time.Time    `json:"generated_at"`
	Lines       []exportLine `json:"lines"`
}

func main() {
	var outPath string
	flag.StringVar(&outPath, "out", "", "output path (default stdout). Note: for iPhone-as-source-of-truth use cmd/lines-sync instead.")
	flag.Parse()

	available := lines.AvailableLines()
	sort.Strings(available)

	var out exportBundle
	out.GeneratedAt = time.Now().UTC()
	for _, id := range available {
		l := lines.GetLine(id)
		if l == nil {
			continue
		}
		out.Lines = append(out.Lines, toExportLine(l))
	}

	enc, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal: %v\n", err)
		os.Exit(1)
	}
	enc = append(enc, '\n')

	if outPath == "" {
		_, _ = os.Stdout.Write(enc)
		return
	}
	if err := os.WriteFile(outPath, enc, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", outPath, err)
		os.Exit(1)
	}
}

func toExportLine(l *lines.Line) exportLine {
	var stations []exportStation
	stations = make([]exportStation, 0, len(l.Stations))
	for _, s := range l.Stations {
		stations = append(stations, exportStation{
			ID:         s.ID,
			Name:       s.Name,
			ShortName:  s.ShortName,
			HasService: s.HasService,
			DwellMS:    s.DwellTime.Milliseconds(),
		})
	}

	var segments []exportSegment
	segments = make([]exportSegment, 0, len(l.Segments))
	for _, s := range l.Segments {
		segments = append(segments, exportSegment{
			FromID:      s.FromID,
			ToID:        s.ToID,
			IsTunnel:    s.IsTunnel,
			DurationMS:  s.Duration.Milliseconds(),
			Degradation: s.Degradation,
		})
	}

	return exportLine{
		ID:       l.ID,
		Name:     l.Name,
		Color:    l.Color,
		Stations: stations,
		Segments: segments,
	}
}
