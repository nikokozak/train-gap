package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

type sessionStart struct {
	Type                      string   `json:"type"`
	Version                   int      `json:"v"`
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
	SampleIntervalMS          *int     `json:"sample_interval_ms"`
	SampleIntervalInTransitMS *int     `json:"sample_interval_in_transit_ms"`
	SampleIntervalAtStationMS *int     `json:"sample_interval_at_station_ms"`
}

type sample struct {
	Type string `json:"type"`
	TMS  int64  `json:"t_ms"`
	Path struct {
		Status      string  `json:"status"`
		Interface   *string `json:"interface"`
		Expensive   bool    `json:"expensive"`
		Constrained bool    `json:"constrained"`
	} `json:"path"`
	Probe struct {
		OK         bool    `json:"ok"`
		URL        *string `json:"url"`
		RTTMS      *int64  `json:"rtt_ms"`
		HTTPStatus *int    `json:"http_status"`
		Error      *string `json:"error"`
	} `json:"probe"`
}

type stationDepart struct {
	Type             string `json:"type"`
	TMS              int64  `json:"t_ms"`
	StationID        string `json:"station_id"`
	StationName      string `json:"station_name"`
	StationIndex     *int   `json:"station_index"`
	NextStationID    string `json:"next_station_id"`
	NextStationName  string `json:"next_station_name"`
	NextStationIndex *int   `json:"next_station_index"`
}

type stationArrive struct {
	Type         string `json:"type"`
	TMS          int64  `json:"t_ms"`
	StationID    string `json:"station_id"`
	StationName  string `json:"station_name"`
	StationIndex *int   `json:"station_index"`
}

type undo struct {
	Type  string `json:"type"`
	TMS   int64  `json:"t_ms"`
	Count *int   `json:"count"`
}

type sessionEnd struct {
	Type       string `json:"type"`
	EndedAt    string `json:"ended_at"`
	Reason     string `json:"reason"`
	Successful *bool  `json:"successful"`
	Note       string `json:"note"`
}

type summary struct {
	File                    string           `json:"file"`
	SessionID               string           `json:"session_id,omitempty"`
	LineID                  string           `json:"line_id,omitempty"`
	LineName                string           `json:"line_name,omitempty"`
	FromStationID           string           `json:"from_station_id,omitempty"`
	FromStationName         string           `json:"from_station_name,omitempty"`
	ToStationID             string           `json:"to_station_id,omitempty"`
	ToStationName           string           `json:"to_station_name,omitempty"`
	StartedAt               string           `json:"started_at,omitempty"`
	EndedAt                 string           `json:"ended_at,omitempty"`
	Successful              *bool            `json:"successful,omitempty"`
	Note                    string           `json:"note,omitempty"`
	Direction               string           `json:"direction,omitempty"`
	DirectionLabel          string           `json:"direction_label,omitempty"`
	ProbeURLs               []string         `json:"probe_urls,omitempty"`
	SampleIntervalInTransit *int             `json:"sample_interval_in_transit_ms,omitempty"`
	SampleIntervalAtStation *int             `json:"sample_interval_at_station_ms,omitempty"`
	Samples                 int              `json:"samples"`
	ProbeOK                 int              `json:"probe_ok"`
	PathSatisfied           int              `json:"path_satisfied"`
	PathUnsatisfied         int              `json:"path_unsatisfied"`
	PathRequiresConn        int              `json:"path_requires_connection"`
	PathUnknown             int              `json:"path_unknown"`
	InterfaceCounts         map[string]int   `json:"interface_counts"`
	HTTPStatusCounts        map[string]int   `json:"http_status_counts"`
	ProbeErrorCounts        map[string]int   `json:"probe_error_counts"`
	RTTMinMS                *int64           `json:"rtt_min_ms,omitempty"`
	RTTP50MS                *int64           `json:"rtt_p50_ms,omitempty"`
	RTTP95MS                *int64           `json:"rtt_p95_ms,omitempty"`
	RTTMaxMS                *int64           `json:"rtt_max_ms,omitempty"`
	Segments                []segmentSummary `json:"segments,omitempty"`
	SegmentCount            int              `json:"segment_count"`
}

type segmentSummary struct {
	FromStationID   string `json:"from_station_id"`
	FromStationName string `json:"from_station_name"`
	ToStationID     string `json:"to_station_id"`
	ToStationName   string `json:"to_station_name"`
	DepartTMS       int64  `json:"depart_t_ms"`
	ArriveTMS       int64  `json:"arrive_t_ms"`
	DurationMS      int64  `json:"duration_ms"`

	Samples  int    `json:"samples"`
	ProbeOK  int    `json:"probe_ok"`
	RTTMinMS *int64 `json:"rtt_min_ms,omitempty"`
	RTTP50MS *int64 `json:"rtt_p50_ms,omitempty"`
	RTTP95MS *int64 `json:"rtt_p95_ms,omitempty"`
	RTTMaxMS *int64 `json:"rtt_max_ms,omitempty"`
}

type marker struct {
	kind            string
	tms             int64
	stationID       string
	stationName     string
	nextStationID   string
	nextStationName string
}

func main() {
	var jsonOut bool
	flag.BoolVar(&jsonOut, "json", false, "emit JSON")
	flag.Parse()

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "usage: recording-summarize [-json] <file.ndjson> [more.ndjson...]")
		os.Exit(2)
	}

	var sums []summary
	for _, p := range files {
		s, err := summarizeFile(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", p, err)
			os.Exit(1)
		}
		sums = append(sums, s)
	}

	if jsonOut {
		enc, _ := json.MarshalIndent(sums, "", "  ")
		enc = append(enc, '\n')
		_, _ = os.Stdout.Write(enc)
		return
	}

	for _, s := range sums {
		printSummary(s)
	}
}

func summarizeFile(path string) (summary, error) {
	f, err := os.Open(path)
	if err != nil {
		return summary{}, err
	}
	defer f.Close()

	out := summary{
		File:             path,
		InterfaceCounts:  map[string]int{},
		HTTPStatusCounts: map[string]int{},
		ProbeErrorCounts: map[string]int{},
	}

	var rtts []int64
	var samples []sample
	var markers []marker

	sc := bufio.NewScanner(f)
	// Allow long lines (though our events are small).
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var kind struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &kind); err != nil {
			return summary{}, fmt.Errorf("invalid JSON: %w", err)
		}

		switch kind.Type {
		case "session_start":
			var st sessionStart
			if err := json.Unmarshal([]byte(line), &st); err != nil {
				return summary{}, fmt.Errorf("decode session_start: %w", err)
			}
			out.SessionID = st.SessionID
			out.LineID = st.LineID
			out.FromStationID = st.FromStationID
			out.ToStationID = st.ToStationID
			out.StartedAt = st.StartedAt
			out.Direction = st.Direction
			out.LineName = st.LineName
			out.FromStationName = st.FromStationName
			out.ToStationName = st.ToStationName
			out.DirectionLabel = st.DirectionLabel
			if len(st.ProbeURLs) > 0 {
				out.ProbeURLs = st.ProbeURLs
			} else if st.ProbeURL != "" {
				out.ProbeURLs = []string{st.ProbeURL}
			}
			out.SampleIntervalInTransit = st.SampleIntervalInTransitMS
			out.SampleIntervalAtStation = st.SampleIntervalAtStationMS

		case "station_depart":
			var d stationDepart
			if err := json.Unmarshal([]byte(line), &d); err != nil {
				return summary{}, fmt.Errorf("decode station_depart: %w", err)
			}
			markers = append(markers, marker{
				kind:            "depart",
				tms:             d.TMS,
				stationID:       d.StationID,
				stationName:     d.StationName,
				nextStationID:   d.NextStationID,
				nextStationName: d.NextStationName,
			})

		case "station_arrive":
			var a stationArrive
			if err := json.Unmarshal([]byte(line), &a); err != nil {
				return summary{}, fmt.Errorf("decode station_arrive: %w", err)
			}
			markers = append(markers, marker{
				kind:        "arrive",
				tms:         a.TMS,
				stationID:   a.StationID,
				stationName: a.StationName,
			})

		case "undo":
			var u undo
			if err := json.Unmarshal([]byte(line), &u); err != nil {
				return summary{}, fmt.Errorf("decode undo: %w", err)
			}
			n := 1
			if u.Count != nil && *u.Count > 0 {
				n = *u.Count
			}
			for i := 0; i < n; i++ {
				if len(markers) == 0 {
					break
				}
				markers = markers[:len(markers)-1]
			}

		case "sample":
			var s sample
			if err := json.Unmarshal([]byte(line), &s); err != nil {
				return summary{}, fmt.Errorf("decode sample: %w", err)
			}
			samples = append(samples, s)
			out.Samples++
			if s.Probe.OK {
				out.ProbeOK++
			}
			if s.Probe.RTTMS != nil {
				rtts = append(rtts, *s.Probe.RTTMS)
			}
			if s.Probe.HTTPStatus != nil {
				out.HTTPStatusCounts[fmt.Sprintf("%d", *s.Probe.HTTPStatus)]++
			}
			if s.Probe.Error != nil && *s.Probe.Error != "" {
				out.ProbeErrorCounts[*s.Probe.Error]++
			}

			switch s.Path.Status {
			case "satisfied":
				out.PathSatisfied++
			case "unsatisfied":
				out.PathUnsatisfied++
			case "requires_connection":
				out.PathRequiresConn++
			default:
				out.PathUnknown++
			}

			if s.Path.Interface != nil && *s.Path.Interface != "" {
				out.InterfaceCounts[*s.Path.Interface]++
			} else {
				out.InterfaceCounts["(nil)"]++
			}

		case "session_end":
			var e sessionEnd
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				return summary{}, fmt.Errorf("decode session_end: %w", err)
			}
			out.EndedAt = e.EndedAt
			out.Successful = e.Successful
			out.Note = e.Note

		default:
			// ignore unknown events
		}
	}
	if err := sc.Err(); err != nil {
		return summary{}, err
	}

	if len(rtts) > 0 {
		sort.Slice(rtts, func(i, j int) bool { return rtts[i] < rtts[j] })
		min := rtts[0]
		max := rtts[len(rtts)-1]
		p50 := percentile(rtts, 0.50)
		p95 := percentile(rtts, 0.95)
		out.RTTMinMS = &min
		out.RTTMaxMS = &max
		out.RTTP50MS = &p50
		out.RTTP95MS = &p95
	}

	out.Segments = computeSegments(markers, samples)
	out.SegmentCount = len(out.Segments)

	return out, nil
}

func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	idx := int(float64(len(sorted)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func printSummary(s summary) {
	fmt.Printf("%s\n", s.File)
	if s.SessionID != "" {
		fmt.Printf("  session: %s\n", s.SessionID)
	}
	if s.LineID != "" {
		routeFrom := s.FromStationID
		if s.FromStationName != "" {
			routeFrom = s.FromStationName
		}
		routeTo := s.ToStationID
		if s.ToStationName != "" {
			routeTo = s.ToStationName
		}
		dir := s.Direction
		if s.DirectionLabel != "" {
			dir = s.DirectionLabel
		}
		fmt.Printf("  route: %s %s -> %s (%s)\n", s.LineID, routeFrom, routeTo, dir)
	}
	if s.Successful != nil {
		fmt.Printf("  successful: %v\n", *s.Successful)
	}
	fmt.Printf("  samples: %d, probe ok: %d (%.1f%%)\n", s.Samples, s.ProbeOK, pct(s.ProbeOK, s.Samples))
	fmt.Printf("  path satisfied: %d (%.1f%%)\n", s.PathSatisfied, pct(s.PathSatisfied, s.Samples))
	if s.RTTMinMS != nil {
		fmt.Printf("  rtt ms: min %d, p50 %d, p95 %d, max %d\n", *s.RTTMinMS, *s.RTTP50MS, *s.RTTP95MS, *s.RTTMaxMS)
	}
	if len(s.Segments) > 0 {
		fmt.Printf("  segments: %d\n", len(s.Segments))
		for i, seg := range s.Segments {
			fmt.Printf("    %2d) %s -> %s (%ds) probe ok %.1f%%\n",
				i+1,
				coalesce(seg.FromStationName, seg.FromStationID),
				coalesce(seg.ToStationName, seg.ToStationID),
				int(seg.DurationMS/1000),
				pct(seg.ProbeOK, seg.Samples),
			)
		}
	}
}

func pct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return (float64(n) / float64(d)) * 100.0
}

func computeSegments(markers []marker, samples []sample) []segmentSummary {
	var out []segmentSummary

	var depart *marker

	for i := range markers {
		m := &markers[i]
		switch m.kind {
		case "depart":
			depart = m
		case "arrive":
			if depart == nil {
				continue
			}

			from := *depart
			to := *m
			depart = nil

			seg := segmentSummary{
				FromStationID:   from.stationID,
				FromStationName: from.stationName,
				ToStationID:     to.stationID,
				ToStationName:   to.stationName,
				DepartTMS:       from.tms,
				ArriveTMS:       to.tms,
				DurationMS:      max64(0, to.tms-from.tms),
			}

			var rtts []int64
			for _, s := range samples {
				if s.TMS < seg.DepartTMS || s.TMS > seg.ArriveTMS {
					continue
				}
				seg.Samples++
				if s.Probe.OK {
					seg.ProbeOK++
				}
				if s.Probe.RTTMS != nil {
					rtts = append(rtts, *s.Probe.RTTMS)
				}
			}
			if len(rtts) > 0 {
				sort.Slice(rtts, func(i, j int) bool { return rtts[i] < rtts[j] })
				min := rtts[0]
				max := rtts[len(rtts)-1]
				p50 := percentile(rtts, 0.50)
				p95 := percentile(rtts, 0.95)
				seg.RTTMinMS = &min
				seg.RTTMaxMS = &max
				seg.RTTP50MS = &p50
				seg.RTTP95MS = &p95
			}

			out = append(out, seg)
		}
	}

	return out
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
