// Command ride-export turns iPhone recordings into ride files for rides/, the rides that ship
// with Train-Gap. A ride file keeps only what the simulator needs (see recording.ToRide).
//
//	go run ./cmd/ride-export path/to/session.ndjson [more.ndjson...]
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nikokozak/train-gap/internal/lines"
	"github.com/nikokozak/train-gap/internal/recording"
	"github.com/nikokozak/train-gap/rides"
)

func main() {
	outputDirectory := flag.String("out", "rides", "directory for the ride files")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: ride-export [-out rides] <session.ndjson> [more.ndjson...]")
		os.Exit(2)
	}
	for _, path := range flag.Args() {
		if err := export(path, *outputDirectory); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			os.Exit(1)
		}
	}
}

func export(path, outputDirectory string) error {
	recorded, err := recording.Read(path)
	if err != nil {
		return err
	}
	ride, detection, err := recording.ToRide(recorded)
	if err != nil {
		return err
	}
	line := lines.Get(ride.LineID)
	if line == nil {
		return fmt.Errorf("line %q is not in the station dataset", ride.LineID)
	}
	data, err := rides.Encode(ride)
	if err != nil {
		return err
	}
	// The check that the shipped rides get: every station is on the line, every run joins neighbours.
	if _, err := rides.RoundTrip(ride, line); err != nil {
		return err
	}

	filePath := filepath.Join(outputDirectory, fileName(ride, line, data))
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return err
	}
	from := line.Stations[line.StationIndex(ride.FirstStationID())].ShortName
	to := line.Stations[line.StationIndex(ride.LastStationID())].ShortName
	fmt.Printf("wrote %s: %s %s -> %s, %s, %d runs\n", filePath, ride.LineID, from, to, ride.Duration(), len(detection.Runs))
	if detection.Status != recording.StatusMatched {
		fmt.Printf("  check the stops: stop detection is %q, not %q. go run ./cmd/recording-summarize -- %s\n",
			detection.Status, recording.StatusMatched, path)
	}
	return nil
}

// fileName is "3-nostrand-av-to-hoyt-st-2026-10-1a2b.json". The end is from the content, so the
// same recording always gets the same name and different rides do not collide.
func fileName(ride rides.Ride, line *lines.Line, data []byte) string {
	from := line.Stations[line.StationIndex(ride.FirstStationID())].ShortName
	to := line.Stations[line.StationIndex(ride.LastStationID())].ShortName
	hash := sha256.Sum256(data)
	name := strings.Join([]string{ride.LineID, from, "to", to, ride.Month, hex.EncodeToString(hash[:2])}, "-")
	return slug(name) + ".json"
}

var nonAlphanumerics = regexp.MustCompile(`[^a-z0-9]+`)

// slug lowercases text and joins its words with hyphens.
func slug(text string) string {
	return strings.Trim(nonAlphanumerics.ReplaceAllString(strings.ToLower(text), "-"), "-")
}
