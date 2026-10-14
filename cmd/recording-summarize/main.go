package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"slices"
	"sort"

	"github.com/nikokozak/train-gap/internal/recording"
)

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
	DamagedLines            int              `json:"damaged_lines,omitempty"` // Not JSON; skipped.
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
	// Set for recordings without station markers: how the segments were found from motion.
	StopDetection *stopDetection `json:"stop_detection,omitempty"`
	Stops         []stopSummary  `json:"stops,omitempty"`
	// STUN requests (udp_probe events). Recorder builds from 2026-10-14 send them.
	UDPProbeServer string `json:"udp_probe_server,omitempty"`
	udpStats
}

type stopDetection struct {
	Status string `json:"status"`
	// Nil when no segment count could be found.
	ExpectedSegments *int `json:"expected_segments,omitempty"`
	DetectedSegments int  `json:"detected_segments"`
	// Stretches without service long enough to be a tunnel. On lines without tunnel coverage,
	// one per segment.
	TunnelGaps int `json:"tunnel_gaps"`
	// Calibration check: station marks should fall in detected stops (within 3 s), not inside segments.
	Marks          int `json:"marks"`
	MarksNearStops int `json:"marks_near_stops"`
	// When the rider marked an anomaly (for example a stop between stations), in seconds into the ride.
	AnomalyMarksS []int64 `json:"anomaly_marks_s,omitempty"`
}

// probeStats summarizes the probes that started inside a time window.
// Round-trip times count successful probes only; a failed probe's time is just the timeout.
type probeStats struct {
	Samples  int    `json:"samples"`
	ProbeOK  int    `json:"probe_ok"`
	RTTMinMS *int64 `json:"rtt_min_ms,omitempty"`
	RTTP50MS *int64 `json:"rtt_p50_ms,omitempty"`
	RTTP95MS *int64 `json:"rtt_p95_ms,omitempty"`
	RTTMaxMS *int64 `json:"rtt_max_ms,omitempty"`
}

// udpStats summarizes the STUN requests (udp_probe events) sent inside a time window. A request
// without a reply is lost. Round trips count answered requests only.
type udpStats struct {
	UDPRequests int    `json:"udp_requests,omitempty"`
	UDPReplies  int    `json:"udp_replies,omitempty"`
	UDPRTTP50MS *int64 `json:"udp_rtt_p50_ms,omitempty"`
	UDPRTTP95MS *int64 `json:"udp_rtt_p95_ms,omitempty"`
}

// udpStatsBetween summarizes the STUN requests sent in [startMS, endMS).
func udpStatsBetween(probes []recording.UDPProbe, startMS, endMS int64) udpStats {
	var stats udpStats
	var roundTrips []int64
	for _, probe := range probes {
		for index, roundTripMS := range probe.RTTMS {
			sentMS := probe.TMS + int64(index)*probe.IntervalMS
			if sentMS < startMS || sentMS >= endMS {
				continue
			}
			stats.UDPRequests++
			if roundTripMS != nil {
				stats.UDPReplies++
				roundTrips = append(roundTrips, *roundTripMS)
			}
		}
	}
	if len(roundTrips) > 0 {
		slices.Sort(roundTrips)
		median, ninetyFifth := percentile(roundTrips, 0.50), percentile(roundTrips, 0.95)
		stats.UDPRTTP50MS, stats.UDPRTTP95MS = &median, &ninetyFifth
	}
	return stats
}

// udpLossText is ", udp lost 12%" for a window with STUN requests, and "" without.
func udpLossText(stats udpStats) string {
	if stats.UDPRequests == 0 {
		return ""
	}
	return fmt.Sprintf(", udp lost %.0f%%", pct(stats.UDPRequests-stats.UDPReplies, stats.UDPRequests))
}

type segmentSummary struct {
	FromStationID   string `json:"from_station_id"`
	FromStationName string `json:"from_station_name"`
	ToStationID     string `json:"to_station_id"`
	ToStationName   string `json:"to_station_name"`
	DepartTMS       int64  `json:"depart_t_ms"`
	ArriveTMS       int64  `json:"arrive_t_ms"`
	DurationMS      int64  `json:"duration_ms"`
	probeStats
	udpStats
}

// stopSummary is the time the train stood still between two segments (or before the first
// and after the last). Station fields are empty when the stop could not be labelled.
type stopSummary struct {
	StationID   string `json:"station_id,omitempty"`
	StationName string `json:"station_name,omitempty"`
	StartTMS    int64  `json:"start_t_ms"`
	EndTMS      int64  `json:"end_t_ms"`
	DurationMS  int64  `json:"duration_ms"`
	probeStats
	udpStats
}

func main() {
	var jsonOut bool
	var energyOut bool
	settings := recording.DefaultSettings
	flag.BoolVar(&jsonOut, "json", false, "emit JSON")
	flag.BoolVar(&energyOut, "energy", false, "print the per-second motion and service trace as CSV (for tuning stop detection)")
	flag.IntVar(&settings.MinimumStopSeconds, "min-stop-s", settings.MinimumStopSeconds, "shortest quiet stretch that counts as a stop")
	flag.IntVar(&settings.MinimumSegmentSeconds, "min-segment-s", settings.MinimumSegmentSeconds, "shortest ride between two stations, in seconds")
	flag.IntVar(&settings.SmoothingSeconds, "smooth-s", settings.SmoothingSeconds, "rolling median width, in seconds")
	flag.IntVar(&settings.MinimumTunnelGapSeconds, "tunnel-gap-s", settings.MinimumTunnelGapSeconds, "failed probes for at least this long count as one tunnel")
	flag.Parse()

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "usage: recording-summarize [-json | -energy] [tuning flags] <file.ndjson> [more.ndjson...]")
		os.Exit(2)
	}

	if energyOut {
		for _, path := range files {
			if err := printEnergyTrace(path, settings); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
				os.Exit(1)
			}
		}
		return
	}

	var sums []summary
	for _, p := range files {
		s, err := summarizeFile(p, settings)
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

func summarizeFile(path string, settings recording.Settings) (summary, error) {
	ride, err := recording.Read(path)
	if err != nil {
		return summary{}, err
	}
	start, end := ride.Start, ride.End
	out := summary{
		File:                    path,
		SessionID:               start.SessionID,
		LineID:                  start.LineID,
		LineName:                start.LineName,
		FromStationID:           start.FromStationID,
		FromStationName:         start.FromStationName,
		ToStationID:             start.ToStationID,
		ToStationName:           start.ToStationName,
		StartedAt:               start.StartedAt,
		EndedAt:                 end.EndedAt,
		Successful:              end.Successful,
		Note:                    end.Note,
		Direction:               start.Direction,
		DirectionLabel:          start.DirectionLabel,
		ProbeURLs:               start.ProbeURLs,
		SampleIntervalInTransit: start.SampleIntervalInTransitMS,
		SampleIntervalAtStation: start.SampleIntervalAtStationMS,
		UDPProbeServer:          start.UDPProbeServer,
		udpStats:                udpStatsBetween(ride.UDPProbes, 0, math.MaxInt64),
		DamagedLines:            ride.DamagedLines,
		InterfaceCounts:         map[string]int{},
		HTTPStatusCounts:        map[string]int{},
		ProbeErrorCounts:        map[string]int{},
	}
	if len(out.ProbeURLs) == 0 && start.ProbeURL != "" {
		out.ProbeURLs = []string{start.ProbeURL}
	}

	var rtts []int64
	for _, s := range ride.Samples {
		out.Samples++
		if s.Probe.OK {
			out.ProbeOK++
		}
		// Only successful probes have a real round trip; a failed one reports its timeout.
		if s.Probe.OK && s.Probe.RTTMS != nil {
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

	switch {
	case len(ride.LegacyMarkers) > 0:
		// Recorder builds before 2026-10: the rider marked every departure and arrival.
		out.Segments = computeSegments(ride.LegacyMarkers, ride.Samples)
	case len(start.RouteStations) > 0:
		// Current recorder builds: find the segments in the motion trace.
		out.Segments, out.Stops, out.StopDetection = detectSegments(ride, settings)
	}
	out.SegmentCount = len(out.Segments)

	return out, nil
}

// detectSegments finds the stops, labels them from the route, and adds the probe statistics
// for each segment and stop.
func detectSegments(ride recording.Recording, settings recording.Settings) ([]segmentSummary, []stopSummary, *stopDetection) {
	result := recording.DetectStops(ride, settings)
	detection := &stopDetection{
		Status:         result.Status,
		TunnelGaps:     result.TunnelGaps,
		MarksNearStops: result.MarksNearStops,
	}
	for _, mark := range ride.Marks {
		if mark.IsStation() {
			detection.Marks++
		} else {
			detection.AnomalyMarksS = append(detection.AnomalyMarksS, mark.TMS/1000)
		}
	}
	if result.Runs == nil {
		return nil, nil, detection
	}
	segmentCount := len(result.Runs)
	detection.ExpectedSegments = &segmentCount
	detection.DetectedSegments = segmentCount
	segments, stops := labelRuns(ride.Start.RouteStations, result.Runs, ride.EndMS(), ride.Samples)
	for index, segment := range segments {
		segments[index].udpStats = udpStatsBetween(ride.UDPProbes, segment.DepartTMS, segment.ArriveTMS)
	}
	for index, stop := range stops {
		stops[index].udpStats = udpStatsBetween(ride.UDPProbes, stop.StartTMS, stop.EndTMS)
	}
	return segments, stops, detection
}

// labelRuns names each run and stop: run k goes from route[k] to route[k+1]. Each stop is the
// time before a run; the last stop, until endMS, is where the rider got off.
func labelRuns(route []recording.RouteStation, runs []recording.TimeSpan, endMS int64, samples []recording.Sample) ([]segmentSummary, []stopSummary) {
	var segments []segmentSummary
	var stops []stopSummary
	previousEndMS := int64(0)
	for position, run := range runs {
		stops = append(stops, makeStop(route[position], previousEndMS, run.StartMS, samples))
		segments = append(segments, segmentSummary{
			FromStationID:   route[position].ID,
			FromStationName: route[position].Name,
			ToStationID:     route[position+1].ID,
			ToStationName:   route[position+1].Name,
			DepartTMS:       run.StartMS,
			ArriveTMS:       run.EndMS,
			DurationMS:      run.EndMS - run.StartMS,
			probeStats:      probeStatsBetween(samples, run.StartMS, run.EndMS),
		})
		previousEndMS = run.EndMS
	}
	stops = append(stops, makeStop(route[len(runs)], previousEndMS, endMS, samples))
	return segments, stops
}

func makeStop(station recording.RouteStation, startMS, endMS int64, samples []recording.Sample) stopSummary {
	return stopSummary{
		StationID:   station.ID,
		StationName: station.Name,
		StartTMS:    startMS,
		EndTMS:      endMS,
		DurationMS:  max(0, endMS-startMS),
		probeStats:  probeStatsBetween(samples, startMS, endMS),
	}
}

// probeStatsBetween summarizes the probes that started in [startMS, endMS).
func probeStatsBetween(samples []recording.Sample, startMS, endMS int64) probeStats {
	var stats probeStats
	var successfulRTTs []int64
	for _, s := range samples {
		if s.TMS < startMS || s.TMS >= endMS {
			continue
		}
		stats.Samples++
		if s.Probe.OK {
			stats.ProbeOK++
			if s.Probe.RTTMS != nil {
				successfulRTTs = append(successfulRTTs, *s.Probe.RTTMS)
			}
		}
	}
	if len(successfulRTTs) > 0 {
		sort.Slice(successfulRTTs, func(i, j int) bool { return successfulRTTs[i] < successfulRTTs[j] })
		minimum := successfulRTTs[0]
		median := percentile(successfulRTTs, 0.50)
		ninetyFifth := percentile(successfulRTTs, 0.95)
		maximum := successfulRTTs[len(successfulRTTs)-1]
		stats.RTTMinMS = &minimum
		stats.RTTP50MS = &median
		stats.RTTP95MS = &ninetyFifth
		stats.RTTMaxMS = &maximum
	}
	return stats
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
	if s.DamagedLines > 0 {
		fmt.Printf("  damaged lines (skipped): %d\n", s.DamagedLines)
	}
	fmt.Printf("  samples: %d, probe ok: %d (%.1f%%)\n", s.Samples, s.ProbeOK, pct(s.ProbeOK, s.Samples))
	fmt.Printf("  path satisfied: %d (%.1f%%)\n", s.PathSatisfied, pct(s.PathSatisfied, s.Samples))
	if s.RTTMinMS != nil {
		fmt.Printf("  rtt ms: min %d, p50 %d, p95 %d, max %d\n", *s.RTTMinMS, *s.RTTP50MS, *s.RTTP95MS, *s.RTTMaxMS)
	}
	if s.UDPRequests > 0 {
		fmt.Printf("  udp (STUN, %s): %d requests, %d replies (%.1f%% lost)", s.UDPProbeServer, s.UDPRequests, s.UDPReplies, pct(s.UDPRequests-s.UDPReplies, s.UDPRequests))
		if s.UDPRTTP50MS != nil {
			fmt.Printf(", rtt ms p50 %d, p95 %d", *s.UDPRTTP50MS, *s.UDPRTTP95MS)
		}
		fmt.Println()
	}
	if detection := s.StopDetection; detection != nil {
		fmt.Printf("  stop detection: %s (segments %d, tunnel gaps without service %d)\n", detection.Status, detection.DetectedSegments, detection.TunnelGaps)
		if detection.Marks > 0 {
			fmt.Printf("  station marks near detected stops: %d of %d\n", detection.MarksNearStops, detection.Marks)
		}
		for _, seconds := range detection.AnomalyMarksS {
			fmt.Printf("  anomaly marked at %d:%02d\n", seconds/60, seconds%60)
		}
	}
	if len(s.Segments) > 0 {
		fmt.Printf("  segments: %d\n", len(s.Segments))
		for i, seg := range s.Segments {
			fmt.Printf("    %2d) %s -> %s (%ds) probe ok %.1f%%%s\n",
				i+1,
				coalesce(coalesce(seg.FromStationName, seg.FromStationID), "?"),
				coalesce(coalesce(seg.ToStationName, seg.ToStationID), "?"),
				int(seg.DurationMS/1000),
				pct(seg.ProbeOK, seg.Samples),
				udpLossText(seg.udpStats),
			)
		}
	}
	if len(s.Stops) > 0 {
		fmt.Printf("  stops: %d\n", len(s.Stops))
		for index, stop := range s.Stops {
			fmt.Printf("    %2d) %s at %ds for %ds, probe ok %d of %d%s\n",
				index+1,
				coalesce(stop.StationName, "?"),
				stop.StartTMS/1000,
				stop.DurationMS/1000,
				stop.ProbeOK,
				stop.Samples,
				udpLossText(stop.udpStats),
			)
		}
	}
}

// printEnergyTrace prints one CSV row per second of motion, for tuning the detection flags.
// With good settings, rows that contain a Station tap (mark=station) are "still".
func printEnergyTrace(path string, settings recording.Settings) error {
	ride, err := recording.Read(path)
	if err != nil {
		return err
	}
	fmt.Printf("# %s\n", path)
	// relative = smoothed jolt / ride median (stops are well below 1); service: 1 = a nearby probe
	// succeeded, 0 = probes failed, blank = no probe.
	fmt.Println("t_s,jolt_mg,smoothed_mg,relative,service,mark")
	for _, second := range recording.SecondsOfRide(ride, settings) {
		service := ""
		if second.HasProbe {
			service = "0"
			if second.HasService {
				service = "1"
			}
		}
		markKind := ""
		for _, mark := range ride.Marks {
			if mark.TMS >= second.StartMS && mark.TMS < second.StartMS+1000 {
				markKind = recording.MarkStation
				if !mark.IsStation() {
					markKind = recording.MarkAnomaly
				}
			}
		}
		fmt.Printf("%.1f,%.1f,%.1f,%.2f,%s,%s\n", float64(second.StartMS)/1000, second.JoltMilliG, second.SmoothedMilliG, second.RelativeJolt, service, markKind)
	}
	return nil
}

func pct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return (float64(n) / float64(d)) * 100.0
}

// computeSegments pairs each departure marker with the next arrival marker.
func computeSegments(markers []recording.LegacyMarker, samples []recording.Sample) []segmentSummary {
	var out []segmentSummary
	var depart *recording.LegacyMarker
	for index := range markers {
		marker := &markers[index]
		switch marker.Kind {
		case "station_depart":
			depart = marker
		case "station_arrive":
			if depart == nil {
				continue
			}
			out = append(out, segmentSummary{
				FromStationID:   depart.StationID,
				FromStationName: depart.StationName,
				ToStationID:     marker.StationID,
				ToStationName:   marker.StationName,
				DepartTMS:       depart.TMS,
				ArriveTMS:       marker.TMS,
				DurationMS:      max(0, marker.TMS-depart.TMS),
				probeStats:      probeStatsBetween(samples, depart.TMS, marker.TMS),
			})
			depart = nil
		}
	}
	return out
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
