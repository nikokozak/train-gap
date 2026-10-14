package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikokozak/train-gap/internal/lines"
	"github.com/nikokozak/train-gap/internal/proxy"
	"github.com/nikokozak/train-gap/internal/recording"
	"github.com/nikokozak/train-gap/internal/sim"
	"github.com/nikokozak/train-gap/internal/tui"
	"github.com/nikokozak/train-gap/rides"
)

const usage = `Train-Gap: ride the NYC subway from your terminal

A proxy (SOCKS5 and HTTP on one port) that replays real subway rides: the cell service and
delay measured in every second, there and back. Route your traffic through it to see how your
app handles tunnels without service, stalls, and slow recovery.

Usage:
  train-gap [flags]                    Show the ride in the terminal and run the proxy.
  train-gap run [flags] -- <command>   Run a command through the proxy while the ride plays.
  train-gap rides                      List the recorded rides.

Without --ride, Train-Gap picks a random ride (on --line, if given).

Flags:
`

const examples = `
Examples:
  train-gap                                 # a random ride; proxy on port 1080
  train-gap --line 3 --speed 4              # a random 3-train ride, four times real time
  train-gap run --speed 4 -- npm test       # run your tests on a ride
  train-gap --ride session.ndjson           # your own recording from the iPhone recorder
  curl --proxy http://localhost:1080 https://example.com
`

// rideOptions are the flags shared by both modes.
type rideOptions struct {
	lineID   *string
	rideName *string
	port     *int
	speed    *float64
	isPaused *bool
}

func addRideFlags(flags *flag.FlagSet, defaultPort int) rideOptions {
	return rideOptions{
		lineID:   flags.String("line", "", "ride a random recording on this line"),
		rideName: flags.String("ride", "", "a ride from `train-gap rides`, a ride file (.json), or an iPhone recording (.ndjson)"),
		port:     flags.Int("port", defaultPort, "proxy port, SOCKS5 and HTTP (0 picks a free port)"),
		speed:    flags.Float64("speed", 1.0, "simulation speed, 0.1 to 10 (1 is real time)"),
		isPaused: flags.Bool("manual", false, "start paused; press n to step through stops and runs"),
	}
}

// chooseRide returns the ride to play: the one --ride names, or else a random shipped ride, on
// --line if given.
func (options rideOptions) chooseRide() (rides.Ride, error) {
	name := *options.rideName
	switch {
	case strings.HasSuffix(name, ".ndjson"):
		recorded, err := recording.Read(name)
		if err != nil {
			return rides.Ride{}, err
		}
		ride, _, err := recording.ToRide(recorded)
		if err != nil {
			return rides.Ride{}, fmt.Errorf("%s: %w", name, err)
		}
		ride.Name = strings.TrimSuffix(filepath.Base(name), ".ndjson")
		return ride, nil
	case strings.HasSuffix(name, ".json"):
		data, err := os.ReadFile(name)
		if err != nil {
			return rides.Ride{}, err
		}
		return rides.Parse(strings.TrimSuffix(filepath.Base(name), ".json"), data)
	case name != "":
		ride, ok := rides.Get(name)
		if !ok {
			return rides.Ride{}, fmt.Errorf("no ride named %q; `train-gap rides` lists them", name)
		}
		return ride, nil
	}

	var candidates []rides.Ride
	linesWithRides := map[string]bool{}
	for _, ride := range rides.All() {
		linesWithRides[ride.LineID] = true
		if *options.lineID == "" || ride.LineID == *options.lineID {
			candidates = append(candidates, ride)
		}
	}
	if len(candidates) == 0 {
		return rides.Ride{}, fmt.Errorf("no recorded rides on the %s yet (lines with rides: %v); docs/recordings.md says how to record one",
			*options.lineID, slices.Sorted(maps.Keys(linesWithRides)))
	}
	return candidates[rand.IntN(len(candidates))], nil
}

// newRide chooses a ride and starts playing it there and back.
func (options rideOptions) newRide() (*sim.Ride, rides.Ride, error) {
	chosen, err := options.chooseRide()
	if err != nil {
		return nil, rides.Ride{}, err
	}
	line := lines.Get(chosen.LineID)
	if line == nil {
		return nil, rides.Ride{}, fmt.Errorf("ride %s: line %q is not in the station dataset", chosen.Name, chosen.LineID)
	}
	timeline, err := rides.RoundTrip(chosen, line)
	if err != nil {
		return nil, rides.Ride{}, err
	}
	ride := sim.NewRide(line, timeline)
	ride.SetSpeed(*options.speed)
	ride.SetPaused(*options.isPaused)
	return ride, chosen, nil
}

// describeRide says what plays: "the 3, Nostrand Av → Hoyt St (recorded Oct 2026 · Verizon)".
func describeRide(ride rides.Ride) string {
	return fmt.Sprintf("the %s, %s (%s)", ride.LineID, ride.Route(lines.Get(ride.LineID)), ride.Source())
}

// listRides prints the shipped rides, one per line.
func listRides() {
	for _, ride := range rides.All() {
		minutes := int(ride.Duration().Round(time.Minute).Minutes())
		fmt.Printf("%-44s  %-2s  %-36s  %2d min  %s\n", ride.Name, ride.LineID, ride.Route(lines.Get(ride.LineID)), minutes, ride.Source())
	}
}

func main() {
	switch {
	case len(os.Args) > 1 && os.Args[1] == "run":
		os.Exit(runCommand(os.Args[2:]))
	case len(os.Args) > 1 && os.Args[1] == "rides":
		listRides()
	default:
		runTerminalUI()
	}
}

// runTerminalUI shows the ride and runs the proxy until the user quits.
func runTerminalUI() {
	options := addRideFlags(flag.CommandLine, 1080)
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
		fmt.Fprint(os.Stderr, examples)
	}
	flag.Parse()

	ride, chosen, err := options.newRide()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	proxyServer := proxy.NewServer(*options.port, ride.Condition)
	if err := proxyServer.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting proxy: %v\n", err)
		os.Exit(1)
	}
	defer proxyServer.Stop()

	// The ride list offers every shipped ride, and the chosen one if it came from a file.
	choices := rides.All()
	current := slices.IndexFunc(choices, func(ride rides.Ride) bool { return ride.Name == chosen.Name })
	if current < 0 {
		choices, current = append(choices, chosen), len(choices)
	}
	program := tea.NewProgram(tui.NewModel(ride, proxyServer, choices, current), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}

// runCommand runs one command with its proxy environment set to the simulator, while the ride
// plays, and returns the command's exit code. Each change of stop or service is printed to stderr,
// so a failure in the command's output can be matched to the moment it happened.
func runCommand(arguments []string) int {
	flags := flag.NewFlagSet("run", flag.ExitOnError)
	options := addRideFlags(flags, 0)
	flags.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: train-gap run [flags] -- <command> [arguments]")
		flags.PrintDefaults()
	}
	_ = flags.Parse(arguments) // ExitOnError: Parse exits on bad flags.
	command := flags.Args()
	if len(command) == 0 {
		flags.Usage()
		return 2
	}

	ride, chosen, err := options.newRide()
	if err != nil {
		fmt.Fprintln(os.Stderr, "train-gap:", err)
		return 2
	}
	proxyServer := proxy.NewServer(*options.port, ride.Condition)
	if err := proxyServer.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "train-gap: starting proxy:", err)
		return 2
	}
	defer proxyServer.Stop()

	// The proxy speaks SOCKS5 and HTTP on one port. An http:// URL works with the most clients:
	// curl, Go, Python's requests, pip, npm, git, and Node (with NODE_USE_ENV_PROXY, Node 22.21+
	// and 24.5+). Clients skip the proxy for hosts in NO_PROXY; Go always skips localhost.
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", proxyServer.Port())
	child := exec.Command(command[0], command[1:]...)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	child.Env = append(os.Environ(), "NODE_USE_ENV_PROXY=1")
	for _, name := range []string{"ALL_PROXY", "all_proxy", "HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		child.Env = append(child.Env, name+"="+proxyURL)
	}

	fmt.Fprintf(os.Stderr, "train-gap: riding %s; proxy %s\n", describeRide(chosen), proxyURL)
	stopLogging := make(chan struct{})
	go logRide(ride, os.Stderr, stopLogging)
	err = child.Run()
	close(stopLogging)

	var exitError *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exitError):
		return exitError.ExitCode()
	default:
		fmt.Fprintln(os.Stderr, "train-gap:", err)
		return 127 // The shell's code for a command that could not run.
	}
}

// How often logRide checks the ride. Fine enough to place a change within the second.
const rideLogInterval = 200 * time.Millisecond

// logRide writes a line whenever the train reaches a new stop or run, or the service changes.
func logRide(ride *sim.Ride, output io.Writer, stop <-chan struct{}) {
	lastPhaseIndex, lastDescription := -1, ""
	ticker := time.NewTicker(rideLogInterval)
	defer ticker.Stop()
	for {
		phase, phaseIndex, offset := ride.Position()
		description := phase.ConditionAt(offset).Describe()
		if phaseIndex != lastPhaseIndex || description != lastDescription {
			fmt.Fprintf(output, "train-gap %s  %s: %s\n", formatElapsed(ride.Elapsed()), describePlace(ride.Line(), phase), description)
			lastPhaseIndex, lastDescription = phaseIndex, description
		}
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
	}
}

// describePlace names where the train is: "at Bedford Av" or "Bedford Av -> Lorimer St".
func describePlace(line *lines.Line, phase sim.Phase) string {
	from := line.Stations[phase.FromIndex].ShortName
	if phase.Kind == sim.AtStation {
		return "at " + from
	}
	return from + " -> " + line.Stations[phase.ToIndex].ShortName
}

// formatElapsed shows simulated time as minutes:seconds.
func formatElapsed(elapsed time.Duration) string {
	seconds := int(elapsed.Round(time.Second).Seconds())
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}
