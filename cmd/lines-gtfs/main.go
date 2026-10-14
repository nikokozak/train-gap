package main

import (
	"archive/zip"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type outBundle struct {
	GeneratedAt string    `json:"generated_at"`
	Lines       []outLine `json:"lines"`
}

type outLine struct {
	ID                     string       `json:"id"`
	Name                   string       `json:"name"`
	Color                  string       `json:"color"`
	DirectionForwardLabel  string       `json:"direction_forward_label"`
	DirectionBackwardLabel string       `json:"direction_backward_label"`
	Stations               []outStation `json:"stations"`
}

type outStation struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
}

type routeInfo struct {
	RouteID   string
	ShortName string
	LongName  string
	Color     string
}

type stopInfo struct {
	StopID        string
	Name          string
	ParentStation string
	LocationType  int
	Lat           *float64
	Lon           *float64
}

type tripInfo struct {
	TripID    string
	RouteID   string
	ServiceID string
}

type stationStop struct {
	StationID   string
	StationName string
	ShortName   string
	ArrivalSec  *int
	DepartSec   *int
}

type bestTrip struct {
	Stations []stationStop
}

type tripPattern struct {
	Count    int
	Stations []stationStop
	FirstID  string
	LastID   string
}

func main() {
	var inPath string
	var outPath string
	var routesCSV string
	var listRoutes bool
	var replaceExisting bool
	var weekdayOnly bool
	var timeStartHHMM string
	var timeEndHHMM string

	flag.StringVar(&inPath, "in", "", "path to MTA GTFS zip (e.g. google_transit.zip)")
	flag.StringVar(&outPath, "out", "internal/lines/lines.json", "output lines.json path (the Xcode project links to this file)")
	flag.StringVar(&routesCSV, "routes", defaultRoutesCSV(), "comma-separated route short names to include (e.g. A,C,F,3,4,5)")
	flag.BoolVar(&listRoutes, "list-routes", false, "list available GTFS routes and exit")
	flag.BoolVar(&replaceExisting, "replace-existing", false, "replace an existing line with the GTFS-generated one (default: keep existing)")
	flag.BoolVar(&weekdayOnly, "weekday-only", true, "use weekday service_ids only (Mon–Fri)")
	flag.StringVar(&timeStartHHMM, "time-start", "10:00", "only consider trips starting at/after this local time (HH:MM)")
	flag.StringVar(&timeEndHHMM, "time-end", "16:00", "only consider trips starting before this local time (HH:MM)")
	flag.Parse()

	if inPath == "" {
		fatal(errors.New("give -in (GTFS zip)"))
	}
	generatedByID, warnings := generateLines(inPath, routesCSV, listRoutes, weekdayOnly, timeStartHHMM, timeEndHHMM)

	var out outBundle
	out.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	existingByID := map[string]outLine{}
	if b, err := readExistingBundle(outPath); err == nil {
		for _, l := range b.Lines {
			if l.ID != "" {
				existingByID[l.ID] = l
			}
		}
	}

	unionIDs := map[string]struct{}{}
	for id := range generatedByID {
		unionIDs[id] = struct{}{}
	}
	for id := range existingByID {
		unionIDs[id] = struct{}{}
	}
	var ids []string
	for id := range unionIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		existing, hasExisting := existingByID[id]
		gen, hasGen := generatedByID[id]

		switch {
		case hasGen && hasExisting && !replaceExisting:
			out.Lines = append(out.Lines, existing)
		case hasGen:
			out.Lines = append(out.Lines, gen)
		case hasExisting:
			out.Lines = append(out.Lines, existing)
		}
	}

	for _, w := range uniqueSorted(warnings) {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}

	enc, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fatal(fmt.Errorf("marshal output: %w", err))
	}
	enc = append(enc, '\n')

	if err := os.WriteFile(outPath, enc, 0o644); err != nil {
		fatal(fmt.Errorf("write %s: %w", outPath, err))
	}
}

// generateLines builds one line per selected route from the GTFS zip, using a typical
// weekday trip. It returns the lines by ID and any warnings.
func generateLines(inPath, routesCSV string, listRoutes, weekdayOnly bool, timeStartHHMM, timeEndHHMM string) (map[string]outLine, []string) {

	z, err := zip.OpenReader(inPath)
	if err != nil {
		fatal(fmt.Errorf("open zip %s: %w", inPath, err))
	}
	defer z.Close()

	routesByID, routesByShort, err := readRoutes(&z.Reader)
	if err != nil {
		fatal(err)
	}

	if listRoutes {
		printRoutes(routesByID)
		os.Exit(0)
	}

	selected := parseCSVSet(routesCSV)
	if len(selected) == 0 {
		fatal(errors.New("routes set is empty"))
	}

	stops, err := readStops(&z.Reader)
	if err != nil {
		fatal(err)
	}

	weekdayServiceIDs := map[string]struct{}{}
	if weekdayOnly {
		if svc, err := readCalendarWeekdayServiceIDs(&z.Reader); err == nil {
			weekdayServiceIDs = svc
		} else {
			fmt.Fprintln(os.Stderr, "warning: failed to read calendar.txt; continuing without weekday filter:", err)
		}
	}

	startSec, okStart := parseHHMMToSec(timeStartHHMM)
	endSec, okEnd := parseHHMMToSec(timeEndHHMM)
	if !okStart || !okEnd {
		fatal(fmt.Errorf("invalid time window: %s-%s (expected HH:MM)", timeStartHHMM, timeEndHHMM))
	}
	if endSec <= startSec {
		fatal(fmt.Errorf("invalid time window: end must be after start (%s-%s)", timeStartHHMM, timeEndHHMM))
	}

	trips, warnings, err := readTrips(&z.Reader, routesByID, selected, weekdayServiceIDs)
	if err != nil {
		fatal(err)
	}

	bestByShort, moreWarnings, err := pickBestTrips(&z.Reader, trips, stops, startSec, endSec)
	if err != nil {
		fatal(err)
	}
	warnings = append(warnings, moreWarnings...)

	// Build output lines in stable order.
	generatedByID := map[string]outLine{}
	for short := range selected {
		route := routesByShort[short]
		if route.ShortName == "" {
			route.ShortName = short
		}
		if route.LongName == "" {
			route.LongName = short
		}

		bt, ok := bestByShort[short]
		if !ok || len(bt.Stations) < 2 {
			warnings = append(warnings, fmt.Sprintf("no suitable trip found for route %s", short))
			continue
		}

		line := buildLine(route, bt)
		generatedByID[line.ID] = line
	}
	return generatedByID, warnings
}

func readExistingBundle(path string) (outBundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return outBundle{}, err
	}
	var b outBundle
	if err := json.Unmarshal(data, &b); err != nil {
		return outBundle{}, err
	}
	return b, nil
}

func defaultRoutesCSV() string {
	// "Major" MTA services (no branches/reroutes; we'll pick a canonical trip per route).
	return strings.Join([]string{
		"A", "C", "E",
		"B", "D", "F", "M",
		"G",
		"J", "Z",
		"L",
		"N", "Q", "R", "W",
		"1", "2", "3",
		"4", "5", "6",
		"7",
		"S",
	}, ",")
}

func buildLine(route routeInfo, bt bestTrip) outLine {
	stations := make([]outStation, 0, len(bt.Stations))
	for _, st := range bt.Stations {
		stations = append(stations, outStation{
			ID:        st.StationID,
			Name:      st.StationName,
			ShortName: st.ShortName,
		})
	}

	forward := "To " + bt.Stations[len(bt.Stations)-1].ShortName
	backward := "To " + bt.Stations[0].ShortName

	return outLine{
		ID:                     route.ShortName,
		Name:                   route.LongName,
		Color:                  normalizeHexColor(route.Color, route.ShortName),
		DirectionForwardLabel:  forward,
		DirectionBackwardLabel: backward,
		Stations:               stations,
	}
}

func normalizeHexColor(s string, fallbackKey string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
	if len(s) == 6 {
		if _, err := strconv.ParseUint(s, 16, 32); err == nil {
			return "#" + strings.ToUpper(s)
		}
	}

	// Fallback to a subset of our TUI map (good enough for recording UI).
	if c, ok := fallbackLineColor[fallbackKey]; ok {
		return c
	}
	return "#808183"
}

var fallbackLineColor = map[string]string{
	"L": "#A7A9AC",
	"A": "#0039A6",
	"C": "#0039A6",
	"E": "#0039A6",
	"B": "#FF6319",
	"D": "#FF6319",
	"F": "#FF6319",
	"M": "#FF6319",
	"G": "#6CBE45",
	"J": "#996633",
	"Z": "#996633",
	"N": "#FCCC0A",
	"Q": "#FCCC0A",
	"R": "#FCCC0A",
	"W": "#FCCC0A",
	"1": "#EE352E",
	"2": "#EE352E",
	"3": "#EE352E",
	"4": "#00933C",
	"5": "#00933C",
	"6": "#00933C",
	"7": "#B933AD",
	"S": "#808183",
}

func readRoutes(z *zip.Reader) (map[string]routeInfo, map[string]routeInfo, error) {
	r := map[string]routeInfo{}
	byShort := map[string]routeInfo{}

	cr, closeFn, err := openCSVFromZip(z, "routes.txt")
	if err != nil {
		return nil, nil, err
	}
	defer closeFn()

	h, err := readHeader(cr)
	if err != nil {
		return nil, nil, err
	}

	idxRouteID := mustIndex(h, "route_id")
	idxShort := optionalIndex(h, "route_short_name")
	idxLong := optionalIndex(h, "route_long_name")
	idxColor := optionalIndex(h, "route_color")

	for {
		row, err := cr.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, nil, fmt.Errorf("routes.txt: %w", err)
		}
		routeID := field(row, idxRouteID)
		if routeID == "" {
			continue
		}
		short := routeID
		if idxShort >= 0 {
			if v := field(row, idxShort); v != "" {
				short = v
			}
		}
		long := short
		if idxLong >= 0 {
			if v := field(row, idxLong); v != "" {
				long = v
			}
		}
		color := ""
		if idxColor >= 0 {
			color = field(row, idxColor)
		}
		info := routeInfo{
			RouteID:   routeID,
			ShortName: short,
			LongName:  long,
			Color:     color,
		}
		r[routeID] = info
		if _, exists := byShort[short]; !exists {
			byShort[short] = info
		}
	}

	return r, byShort, nil
}

func readStops(z *zip.Reader) (map[string]stopInfo, error) {
	out := map[string]stopInfo{}

	cr, closeFn, err := openCSVFromZip(z, "stops.txt")
	if err != nil {
		return nil, err
	}
	defer closeFn()

	h, err := readHeader(cr)
	if err != nil {
		return nil, err
	}

	idxID := mustIndex(h, "stop_id")
	idxName := mustIndex(h, "stop_name")
	idxParent := optionalIndex(h, "parent_station")
	idxLocType := optionalIndex(h, "location_type")
	idxLat := optionalIndex(h, "stop_lat")
	idxLon := optionalIndex(h, "stop_lon")

	for {
		row, err := cr.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("stops.txt: %w", err)
		}

		id := field(row, idxID)
		if id == "" {
			continue
		}
		name := field(row, idxName)
		parent := ""
		if idxParent >= 0 {
			parent = field(row, idxParent)
		}
		locType := 0
		if idxLocType >= 0 {
			if v := field(row, idxLocType); v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					locType = n
				}
			}
		}

		var latPtr, lonPtr *float64
		if idxLat >= 0 {
			if v := field(row, idxLat); v != "" {
				if n, err := strconv.ParseFloat(v, 64); err == nil {
					latPtr = &n
				}
			}
		}
		if idxLon >= 0 {
			if v := field(row, idxLon); v != "" {
				if n, err := strconv.ParseFloat(v, 64); err == nil {
					lonPtr = &n
				}
			}
		}

		out[id] = stopInfo{
			StopID:        id,
			Name:          name,
			ParentStation: parent,
			LocationType:  locType,
			Lat:           latPtr,
			Lon:           lonPtr,
		}
	}

	return out, nil
}

func readTrips(z *zip.Reader, routesByID map[string]routeInfo, selectedShort map[string]struct{}, allowServiceIDs map[string]struct{}) (map[string]tripInfo, []string, error) {
	out := map[string]tripInfo{}
	var warnings []string

	cr, closeFn, err := openCSVFromZip(z, "trips.txt")
	if err != nil {
		return nil, nil, err
	}
	defer closeFn()

	h, err := readHeader(cr)
	if err != nil {
		return nil, nil, err
	}

	idxTripID := mustIndex(h, "trip_id")
	idxRouteID := mustIndex(h, "route_id")
	idxServiceID := optionalIndex(h, "service_id")

	for {
		row, err := cr.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, nil, fmt.Errorf("trips.txt: %w", err)
		}

		tripID := field(row, idxTripID)
		routeID := field(row, idxRouteID)
		if tripID == "" || routeID == "" {
			continue
		}
		serviceID := ""
		if idxServiceID >= 0 {
			serviceID = field(row, idxServiceID)
		}
		if len(allowServiceIDs) > 0 {
			if serviceID == "" {
				continue
			}
			if _, ok := allowServiceIDs[serviceID]; !ok {
				continue
			}
		}
		route, ok := routesByID[routeID]
		if !ok {
			continue
		}
		short := route.ShortName
		if short == "" {
			short = routeID
		}
		if _, ok := selectedShort[short]; !ok {
			continue
		}
		out[tripID] = tripInfo{TripID: tripID, RouteID: short, ServiceID: serviceID}
	}

	// Warn for selected routes missing from routes.txt entirely.
	for short := range selectedShort {
		found := false
		for _, r := range routesByID {
			if r.ShortName == short || r.RouteID == short {
				found = true
				break
			}
		}
		if !found {
			warnings = append(warnings, fmt.Sprintf("requested route %s not found in routes.txt", short))
		}
	}

	return out, warnings, nil
}

func pickBestTrips(z *zip.Reader, trips map[string]tripInfo, stops map[string]stopInfo, startSec int, endSec int) (map[string]bestTrip, []string, error) {
	best := map[string]bestTrip{}
	patternsPreferred := map[string]map[string]*tripPattern{}
	patternsAll := map[string]map[string]*tripPattern{}
	var warnings []string

	cr, closeFn, err := openCSVFromZip(z, "stop_times.txt")
	if err != nil {
		return nil, nil, err
	}
	defer closeFn()

	h, err := readHeader(cr)
	if err != nil {
		return nil, nil, err
	}

	idxTrip := mustIndex(h, "trip_id")
	idxStop := mustIndex(h, "stop_id")
	idxArr := optionalIndex(h, "arrival_time")
	idxDep := optionalIndex(h, "departure_time")
	idxSeq := optionalIndex(h, "stop_sequence")

	var curTripID string
	var curRoute string
	var cur []stationStop
	var curPreferred bool
	var curHasStartTime bool
	var lastStation string
	var lastSeq int
	var lastSeqOK bool

	flushInto := func(dst map[string]map[string]*tripPattern) {
		if curRoute == "" || len(cur) < 2 {
			return
		}

		key := stationSequenceKey(cur)
		if key == "" {
			return
		}

		routePatterns, ok := dst[curRoute]
		if !ok {
			routePatterns = map[string]*tripPattern{}
			dst[curRoute] = routePatterns
		}

		p := routePatterns[key]
		if p == nil {
			p = &tripPattern{
				Stations: append([]stationStop(nil), cur...),
				FirstID:  cur[0].StationID,
				LastID:   cur[len(cur)-1].StationID,
			}
			routePatterns[key] = p
		}
		p.Count++
	}

	flush := func() {
		flushInto(patternsAll)
		if curPreferred {
			flushInto(patternsPreferred)
		}
	}

	for {
		row, err := cr.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, nil, fmt.Errorf("stop_times.txt: %w", err)
		}

		tripID := field(row, idxTrip)
		if tripID == "" {
			continue
		}

		if tripID != curTripID {
			flush()
			curTripID = tripID

			if t, ok := trips[tripID]; ok {
				curRoute = t.RouteID
				curPreferred = false
				curHasStartTime = false
			} else {
				curRoute = ""
				curPreferred = false
				curHasStartTime = false
			}

			cur = cur[:0]
			lastStation = ""
			lastSeq = 0
			lastSeqOK = false
		}

		if curRoute == "" {
			continue
		}

		if idxSeq >= 0 {
			if s := field(row, idxSeq); s != "" {
				if n, err := strconv.Atoi(s); err == nil {
					if lastSeqOK && n < lastSeq {
						return nil, nil, fmt.Errorf("stop_times.txt is not grouped/sorted by trip_id+stop_sequence (trip %s)", tripID)
					}
					lastSeq = n
					lastSeqOK = true
				}
			}
		}

		stopID := field(row, idxStop)
		if stopID == "" {
			continue
		}

		stationID, stationName := resolveStation(stopID, stops)
		if stationID == "" {
			continue
		}
		if stationID == lastStation {
			continue
		}
		lastStation = stationID

		var arrSecPtr, depSecPtr *int
		if idxArr >= 0 {
			if sec, ok := parseGTFSTimeToSec(field(row, idxArr)); ok {
				arrSecPtr = &sec
			}
		}
		if idxDep >= 0 {
			if sec, ok := parseGTFSTimeToSec(field(row, idxDep)); ok {
				depSecPtr = &sec
			}
		}

		if curRoute != "" && !curHasStartTime {
			var probe *int
			if depSecPtr != nil {
				probe = depSecPtr
			} else if arrSecPtr != nil {
				probe = arrSecPtr
			}
			if probe != nil {
				curHasStartTime = true
				curPreferred = *probe >= startSec && *probe < endSec
			}
		}

		cur = append(cur, stationStop{
			StationID:   stationID,
			StationName: stationName,
			ShortName:   abbreviateStationName(stationName),
			ArrivalSec:  arrSecPtr,
			DepartSec:   depSecPtr,
		})
	}

	flush()

	unionRoutes := map[string]struct{}{}
	for route := range patternsAll {
		unionRoutes[route] = struct{}{}
	}
	for route := range patternsPreferred {
		unionRoutes[route] = struct{}{}
	}

	for route := range unionRoutes {
		all := patternsAll[route]
		pref := patternsPreferred[route]

		bestAll := pickRoutePattern(route, all)
		bestPref := pickRoutePattern(route, pref)

		chosen := bestAll
		if bestPref != nil {
			chosen = bestPref
			// If the preferred-window pattern is much shorter than the best overall pattern, fall back.
			if bestAll != nil && len(bestPref.Stations) < int(math.Ceil(float64(len(bestAll.Stations))*0.90)) {
				chosen = bestAll
			}
		}

		if chosen == nil || len(chosen.Stations) < 2 {
			continue
		}
		best[route] = bestTrip{Stations: append([]stationStop(nil), chosen.Stations...)}

		candidateCount := 0
		if bestPref != nil {
			candidateCount = len(pref)
		} else {
			candidateCount = len(all)
		}
		if candidateCount > 1 {
			warnings = append(warnings, fmt.Sprintf("route %s has %d stop patterns; chose %d stops seen %d times", route, candidateCount, len(chosen.Stations), chosen.Count))
		}
	}

	if len(best) == 0 {
		warnings = append(warnings, "no trips matched selection; did you pass the right -routes?")
	}

	return best, warnings, nil
}

func pickRoutePattern(route string, candidates map[string]*tripPattern) *tripPattern {
	if len(candidates) == 0 {
		return nil
	}

	type pairAgg struct {
		TotalCount int
		MaxStops   int
	}

	// Group patterns by unordered terminal pair (ignore direction).
	pairs := map[string]*pairAgg{}
	maxTotal := 0
	for _, pat := range candidates {
		if pat == nil || len(pat.Stations) < 2 || pat.FirstID == "" || pat.LastID == "" {
			continue
		}
		a, b := orderedPairKey(pat.FirstID, pat.LastID)
		key := a + "|" + b
		agg := pairs[key]
		if agg == nil {
			agg = &pairAgg{}
			pairs[key] = agg
		}
		agg.TotalCount += pat.Count
		if n := len(pat.Stations); n > agg.MaxStops {
			agg.MaxStops = n
		}
		if agg.TotalCount > maxTotal {
			maxTotal = agg.TotalCount
		}
	}

	if len(pairs) == 0 {
		return nil
	}

	// Exclude extremely rare terminal pairs to avoid picking a one-off reroute/short-turn as "canonical".
	const minPairAbs = 10
	const minPairFrac = 0.10
	minPair := int(math.Ceil(float64(maxTotal) * minPairFrac))
	if minPair < minPairAbs {
		minPair = minPairAbs
	}

	// Special case: MTA GTFS uses route short name "S" for multiple shuttles. Prefer the most common pair.
	// (Picking the "longest" shuttle is usually the wrong thing for a simple recording UI.)
	var chosenPair string
	if route == "S" {
		for k, agg := range pairs {
			if agg.TotalCount < minPair {
				continue
			}
			if chosenPair == "" {
				chosenPair = k
				continue
			}
			cur := pairs[chosenPair]
			switch {
			case agg.TotalCount > cur.TotalCount:
				chosenPair = k
			case agg.TotalCount == cur.TotalCount && agg.MaxStops > cur.MaxStops:
				chosenPair = k
			case agg.TotalCount == cur.TotalCount && agg.MaxStops == cur.MaxStops && k < chosenPair:
				chosenPair = k
			}
		}
	} else {
		for k, agg := range pairs {
			if agg.TotalCount < minPair {
				continue
			}
			if chosenPair == "" {
				chosenPair = k
				continue
			}
			cur := pairs[chosenPair]
			switch {
			case agg.MaxStops > cur.MaxStops:
				chosenPair = k
			case agg.MaxStops == cur.MaxStops && agg.TotalCount > cur.TotalCount:
				chosenPair = k
			case agg.MaxStops == cur.MaxStops && agg.TotalCount == cur.TotalCount && k < chosenPair:
				chosenPair = k
			}
		}
	}

	if chosenPair == "" {
		// If all pairs were below the rarity cutoff (tiny route), fall back to considering all.
		for k := range pairs {
			if chosenPair == "" || k < chosenPair {
				chosenPair = k
			}
		}
	}

	// Select a single ordered station list within the chosen pair.
	const keepFrac = 0.80
	const minKeep = 3

	maxCount := 0
	var pairCandidates []*tripPattern
	for _, pat := range candidates {
		if pat == nil || len(pat.Stations) < 2 {
			continue
		}
		a, b := orderedPairKey(pat.FirstID, pat.LastID)
		if a+"|"+b != chosenPair {
			continue
		}
		pairCandidates = append(pairCandidates, pat)
		if pat.Count > maxCount {
			maxCount = pat.Count
		}
	}
	if len(pairCandidates) == 0 {
		return nil
	}

	threshold := int(math.Ceil(float64(maxCount) * keepFrac))
	if threshold < minKeep {
		threshold = minKeep
	}

	pickMinStopsAmong := func(minCount int) *tripPattern {
		var best *tripPattern
		for _, pat := range pairCandidates {
			if pat.Count < minCount {
				continue
			}
			if best == nil {
				best = pat
				continue
			}
			switch {
			case len(pat.Stations) < len(best.Stations):
				best = pat
			case len(pat.Stations) == len(best.Stations) && pat.Count > best.Count:
				best = pat
			}
		}
		return best
	}

	// Prefer an express-style stop pattern (fewer stops) as long as it is still common for this terminal pair.
	if best := pickMinStopsAmong(threshold); best != nil {
		return best
	}
	if best := pickMinStopsAmong(1); best != nil {
		return best
	}
	return nil
}

func orderedPairKey(a, b string) (string, string) {
	if a <= b {
		return a, b
	}
	return b, a
}

func stationSequenceKey(stops []stationStop) string {
	if len(stops) == 0 {
		return ""
	}
	var b strings.Builder
	for i, s := range stops {
		if s.StationID == "" {
			return ""
		}
		if i > 0 {
			b.WriteByte('>')
		}
		b.WriteString(s.StationID)
	}
	return b.String()
}

func resolveStation(stopID string, stops map[string]stopInfo) (string, string) {
	s, ok := stops[stopID]
	if !ok {
		return "", ""
	}
	stationID := stopID
	if s.ParentStation != "" {
		stationID = s.ParentStation
	}

	name := s.Name
	if stationID != stopID {
		if p, ok := stops[stationID]; ok && p.Name != "" {
			name = p.Name
		}
	}
	if name == "" {
		name = stationID
	}
	return stationID, name
}

func abbreviateStationName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, " Station")

	words := strings.Fields(name)
	for i, w := range words {
		switch w {
		case "Avenue":
			words[i] = "Av"
		case "Street":
			words[i] = "St"
		case "Boulevard":
			words[i] = "Blvd"
		case "Parkway":
			words[i] = "Pkwy"
		case "Terminal":
			words[i] = "Term"
		}
	}
	return strings.Join(words, " ")
}

func parseGTFSTimeToSec(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	sec, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, false
	}
	if m < 0 || m >= 60 || sec < 0 || sec >= 60 || h < 0 {
		return 0, false
	}
	return h*3600 + m*60 + sec, true
}

func parseHHMMToSec(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, false
	}
	if h < 0 || h > 47 || m < 0 || m >= 60 {
		return 0, false
	}
	return h*3600 + m*60, true
}

func readCalendarWeekdayServiceIDs(z *zip.Reader) (map[string]struct{}, error) {
	out := map[string]struct{}{}

	cr, closeFn, err := openCSVFromZip(z, "calendar.txt")
	if err != nil {
		return nil, err
	}
	defer closeFn()

	h, err := readHeader(cr)
	if err != nil {
		return nil, err
	}

	idxService := mustIndex(h, "service_id")
	idxMon := mustIndex(h, "monday")
	idxTue := mustIndex(h, "tuesday")
	idxWed := mustIndex(h, "wednesday")
	idxThu := mustIndex(h, "thursday")
	idxFri := mustIndex(h, "friday")

	for {
		row, err := cr.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("calendar.txt: %w", err)
		}

		serviceID := field(row, idxService)
		if serviceID == "" {
			continue
		}
		if field(row, idxMon) == "1" &&
			field(row, idxTue) == "1" &&
			field(row, idxWed) == "1" &&
			field(row, idxThu) == "1" &&
			field(row, idxFri) == "1" {
			out[serviceID] = struct{}{}
		}
	}

	return out, nil
}

func openCSVFromZip(z *zip.Reader, filename string) (*csv.Reader, func(), error) {
	for _, f := range z.File {
		// Some GTFS zips nest files in a folder; match by suffix.
		if strings.EqualFold(f.Name, filename) || strings.HasSuffix(strings.ToLower(f.Name), "/"+strings.ToLower(filename)) {
			rc, err := f.Open()
			if err != nil {
				return nil, func() {}, fmt.Errorf("open %s in zip: %w", filename, err)
			}
			r := csv.NewReader(rc)
			r.FieldsPerRecord = -1
			return r, func() { _ = rc.Close() }, nil
		}
	}
	return nil, func() {}, fmt.Errorf("missing %s in GTFS zip", filename)
}

func readHeader(r *csv.Reader) (map[string]int, error) {
	row, err := r.Read()
	if err != nil {
		return nil, err
	}
	h := make(map[string]int, len(row))
	for i, v := range row {
		h[strings.TrimSpace(v)] = i
	}
	return h, nil
}

func mustIndex(h map[string]int, name string) int {
	if idx, ok := h[name]; ok {
		return idx
	}
	panic("missing required column: " + name)
}

func optionalIndex(h map[string]int, name string) int {
	if idx, ok := h[name]; ok {
		return idx
	}
	return -1
}

func field(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}

func parseCSVSet(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out[part] = struct{}{}
	}
	return out
}

func uniqueSorted(in []string) []string {
	seen := map[string]struct{}{}
	for _, s := range in {
		seen[s] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func printRoutes(routesByID map[string]routeInfo) {
	var list []routeInfo
	for _, r := range routesByID {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].ShortName < list[j].ShortName
	})
	for _, r := range list {
		short := r.ShortName
		if short == "" {
			short = r.RouteID
		}
		fmt.Printf("%-4s  %s\n", short, r.LongName)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
