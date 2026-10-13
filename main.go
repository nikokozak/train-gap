package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikokozak/train-gap/internal/lines"
	"github.com/nikokozak/train-gap/internal/proxy"
	"github.com/nikokozak/train-gap/internal/recording"
	"github.com/nikokozak/train-gap/internal/sim"
	"github.com/nikokozak/train-gap/internal/tui"
)

const usage = `Train-Gap: NYC Subway Network Simulator

A SOCKS5 proxy that rides an NYC subway line. Route your traffic through it to test
how your app handles signal loss between stations, stalls, and slow recovery.
Stations have service; tunnels without coverage stall connections until the next station.

Usage:
  train-gap [flags]                    Show the ride in the terminal and run the proxy.
  train-gap run [flags] -- <command>   Run a command through the proxy while the ride plays.

Flags:
`

const examples = `
Examples:
  train-gap                                 # L train, proxy on port 1080
  train-gap --line 3 --speed 4              # 3 train, four times real time
  train-gap run --speed 4 -- npm test       # run your tests on the L train
  train-gap --replay ride.ndjson            # replay a ride recorded with the iPhone recorder
  curl --proxy socks5://localhost:1080 https://example.com

Lines: %v
`

// rideOptions are the flags shared by both modes.
type rideOptions struct {
	lineID     *string
	replayPath *string
	port       *int
	speed      *float64
	isPaused   *bool
}

func addRideFlags(flags *flag.FlagSet, defaultPort int) rideOptions {
	return rideOptions{
		lineID:     flags.String("line", "L", "subway line to ride"),
		replayPath: flags.String("replay", "", "replay a ride recorded with the iPhone recorder (.ndjson) instead of the line model"),
		port:       flags.Int("port", defaultPort, "SOCKS5 proxy port (0 picks a free port)"),
		speed:      flags.Float64("speed", 1.0, "simulation speed, 0.1 to 10 (1 is real time)"),
		isPaused:   flags.Bool("manual", false, "start paused; press n to step through stops and runs"),
	}
}

func (options rideOptions) newRide() (*sim.Ride, error) {
	line, timeline, err := options.timeline()
	if err != nil {
		return nil, err
	}
	ride := sim.NewRide(line, timeline)
	ride.SetSpeed(*options.speed)
	ride.SetPaused(*options.isPaused)
	return ride, nil
}

// timeline returns the line and what to play on it: the recording given by --replay, or else the
// model of --line.
func (options rideOptions) timeline() (*lines.Line, sim.Timeline, error) {
	if *options.replayPath == "" {
		line := lines.Get(*options.lineID)
		if line == nil {
			return nil, sim.Timeline{}, fmt.Errorf("unknown line %q; lines: %v", *options.lineID, lines.IDs())
		}
		return line, sim.NewTimeline(line), nil
	}

	recorded, err := recording.Read(*options.replayPath)
	if err != nil {
		return nil, sim.Timeline{}, err
	}
	line := lines.Get(recorded.Start.LineID)
	if line == nil {
		return nil, sim.Timeline{}, fmt.Errorf("%s: line %q is not in the station dataset", *options.replayPath, recorded.Start.LineID)
	}
	timeline, err := recording.Timeline(recorded, line)
	if err != nil {
		return nil, sim.Timeline{}, fmt.Errorf("%s: %w", *options.replayPath, err)
	}
	return line, timeline, nil
}

// describeRide says what plays: "riding the L train" or "replaying the 3 train ride of 2026-10-13 09:19".
func describeRide(ride *sim.Ride) string {
	if ride.Timeline.RecordedAt.IsZero() {
		return fmt.Sprintf("riding the %s train", ride.Line.ID)
	}
	return fmt.Sprintf("replaying the %s train ride of %s", ride.Line.ID, ride.Timeline.RecordedAt.Local().Format("2006-01-02 15:04"))
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "run" {
		os.Exit(runCommand(os.Args[2:]))
	}
	runTerminalUI()
}

// runTerminalUI shows the ride and runs the proxy until the user quits.
func runTerminalUI() {
	options := addRideFlags(flag.CommandLine, 1080)
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, examples, lines.IDs())
	}
	flag.Parse()

	ride, err := options.newRide()
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

	program := tea.NewProgram(tui.NewModel(ride, proxyServer.Port()), tea.WithAltScreen())
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

	ride, err := options.newRide()
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

	// curl, Go, and most CLI tools read these. Node's fetch and Python's requests (without PySocks)
	// do not speak SOCKS5; route those through a SOCKS-aware agent.
	proxyURL := fmt.Sprintf("socks5://127.0.0.1:%d", proxyServer.Port())
	child := exec.Command(command[0], command[1:]...)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	child.Env = os.Environ()
	for _, name := range []string{"ALL_PROXY", "all_proxy", "HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		child.Env = append(child.Env, name+"="+proxyURL)
	}

	fmt.Fprintf(os.Stderr, "train-gap: %s; proxy %s\n", describeRide(ride), proxyURL)
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
			fmt.Fprintf(output, "train-gap %s  %s: %s\n", formatElapsed(ride.Elapsed()), describePlace(ride.Line, phase), description)
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
