package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

type bundle struct {
	Lines []line `json:"lines"`
}

type line struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Stations []station `json:"stations"`
	Segments []segment `json:"segments"`
}

type station struct {
	ID        string `json:"id"`
	ShortName string `json:"short_name"`
}

type segment struct {
	FromID      string  `json:"from_id"`
	ToID        string  `json:"to_id"`
	DurationMS  int64   `json:"duration_ms"`
	Degradation float64 `json:"degradation"`
}

func main() {
	var inPath string
	var outPath string
	flag.StringVar(&inPath, "in", "xcode/TrainGapRecorder/TrainGapRecorder/lines.json", "input lines.json (authoritative recorder dataset)")
	flag.StringVar(&outPath, "out", "internal/lines/lines.json", "output path for Go runtime")
	flag.Parse()

	in, err := os.ReadFile(inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", inPath, err)
		os.Exit(1)
	}

	var b bundle
	if err := json.Unmarshal(in, &b); err != nil {
		fmt.Fprintf(os.Stderr, "invalid JSON %s: %v\n", inPath, err)
		os.Exit(1)
	}
	if len(b.Lines) == 0 {
		fmt.Fprintf(os.Stderr, "no lines in %s\n", inPath)
		os.Exit(1)
	}
	validateBundle(b)

	var out bytes.Buffer
	if err := json.Indent(&out, in, "", "  "); err != nil {
		fmt.Fprintf(os.Stderr, "indent %s: %v\n", inPath, err)
		os.Exit(1)
	}
	out.WriteByte('\n')

	if err := os.WriteFile(outPath, out.Bytes(), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", outPath, err)
		os.Exit(1)
	}
}

func validateBundle(b bundle) {
	seenLineIDs := map[string]struct{}{}
	for _, l := range b.Lines {
		if l.ID == "" {
			fatal("line has empty id")
		}
		if _, ok := seenLineIDs[l.ID]; ok {
			fatal("duplicate line id: %s", l.ID)
		}
		seenLineIDs[l.ID] = struct{}{}
		if len(l.Stations) < 2 {
			fatal("line %s has <2 stations", l.ID)
		}
		stIDs := map[string]struct{}{}
		for _, s := range l.Stations {
			if s.ID == "" {
				fatal("line %s has station with empty id", l.ID)
			}
			if _, ok := stIDs[s.ID]; ok {
				fatal("line %s has duplicate station id: %s", l.ID, s.ID)
			}
			stIDs[s.ID] = struct{}{}
		}
		for _, seg := range l.Segments {
			if seg.FromID == "" || seg.ToID == "" {
				fatal("line %s has segment with empty endpoint", l.ID)
			}
			if _, ok := stIDs[seg.FromID]; !ok {
				fatal("line %s segment from_id unknown: %s", l.ID, seg.FromID)
			}
			if _, ok := stIDs[seg.ToID]; !ok {
				fatal("line %s segment to_id unknown: %s", l.ID, seg.ToID)
			}
			if seg.DurationMS <= 0 {
				fatal("line %s segment %s->%s has non-positive duration_ms %d", l.ID, seg.FromID, seg.ToID, seg.DurationMS)
			}
			if seg.Degradation < 0 || seg.Degradation > 1 {
				fatal("line %s segment %s->%s has degradation out of range: %f", l.ID, seg.FromID, seg.ToID, seg.Degradation)
			}
		}
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
