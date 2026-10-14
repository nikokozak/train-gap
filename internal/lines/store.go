package lines

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

//go:embed lines.json
var embeddedLinesJSON []byte

// linesByID decodes the embedded dataset once. A decode error means a broken build, not a
// runtime condition, so it panics; TestDataset catches it first.
var linesByID = sync.OnceValue(func() map[string]*Line {
	var bundle struct {
		Lines []*Line `json:"lines"`
	}
	if err := json.Unmarshal(embeddedLinesJSON, &bundle); err != nil {
		panic(fmt.Sprintf("lines: embedded lines.json: %v", err))
	}
	byID := make(map[string]*Line, len(bundle.Lines))
	for _, line := range bundle.Lines {
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
