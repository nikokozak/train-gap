package lines

import (
	_ "embed"
	"encoding/json"
	"sort"
	"sync"
	"time"
)

//go:embed lines.json
var embeddedLinesJSON []byte

type linesBundleJSON struct {
	Lines []lineJSON `json:"lines"`
}

type lineJSON struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Color    string        `json:"color"`
	Stations []stationJSON `json:"stations"`
	Segments []segmentJSON `json:"segments"`
}

type stationJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ShortName  string `json:"short_name"`
	HasService bool   `json:"has_service"`
	DwellMS    int64  `json:"dwell_ms"`
}

type segmentJSON struct {
	FromID      string  `json:"from_id"`
	ToID        string  `json:"to_id"`
	IsTunnel    bool    `json:"is_tunnel"`
	DurationMS  int64   `json:"duration_ms"`
	Degradation float64 `json:"degradation"`
}

var (
	storeOnce sync.Once
	storeMu   sync.RWMutex
	store     = map[string]*Line{}
	storeErr  error
)

func ensureStoreLoaded() error {
	storeOnce.Do(func() {
		var bundle linesBundleJSON
		if err := json.Unmarshal(embeddedLinesJSON, &bundle); err != nil {
			storeErr = err
			return
		}

		loaded := make(map[string]*Line, len(bundle.Lines))
		for _, l := range bundle.Lines {
			line := Line{
				ID:    l.ID,
				Name:  l.Name,
				Color: l.Color,
			}
			for _, st := range l.Stations {
				line.Stations = append(line.Stations, Station{
					ID:         st.ID,
					Name:       st.Name,
					ShortName:  st.ShortName,
					HasService: st.HasService,
					DwellTime:  time.Duration(st.DwellMS) * time.Millisecond,
				})
			}
			for _, seg := range l.Segments {
				line.Segments = append(line.Segments, Segment{
					FromID:      seg.FromID,
					ToID:        seg.ToID,
					IsTunnel:    seg.IsTunnel,
					Duration:    time.Duration(seg.DurationMS) * time.Millisecond,
					Degradation: seg.Degradation,
				})
			}
			loaded[line.ID] = &line
		}

		storeMu.Lock()
		store = loaded
		storeMu.Unlock()
	})

	return storeErr
}

func getLineFromStore(id string) *Line {
	if err := ensureStoreLoaded(); err != nil {
		return nil
	}
	storeMu.RLock()
	defer storeMu.RUnlock()
	return store[id]
}

func availableLinesFromStore() []string {
	if err := ensureStoreLoaded(); err != nil {
		return nil
	}
	storeMu.RLock()
	defer storeMu.RUnlock()
	ids := make([]string, 0, len(store))
	for id := range store {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
