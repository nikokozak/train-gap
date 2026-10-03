package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikokozak/train-gap/internal/lines"
	"github.com/nikokozak/train-gap/internal/proxy"
	"github.com/nikokozak/train-gap/internal/sim"
	"github.com/nikokozak/train-gap/internal/tui"
)

var (
	flagLine   = flag.String("line", "L", "Subway line to simulate (L)")
	flagPort   = flag.Int("port", 1080, "SOCKS5 proxy port")
	flagSpeed  = flag.Float64("speed", 1.0, "Simulation speed multiplier (0.1-10.0)")
	flagManual = flag.Bool("manual", false, "Start in manual control mode")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Train-Gap: NYC Subway Network Simulator

A SOCKS5 proxy that simulates NYC subway connectivity patterns.
Route your traffic through the proxy to test how your app handles
signal loss between stations, packet loss, and latency spikes.

Usage:
  train-gap [flags]

Flags:
`)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  train-gap                           # Start with L train, default settings
  train-gap --port 8080               # Use different port
  train-gap --speed 2.0               # Run simulation faster
  train-gap --manual                  # Manual control mode

Testing:
  curl --proxy socks5://localhost:1080 https://httpbin.org/get

`)
	}
	flag.Parse()

	// Load the line
	line := lines.GetLine(*flagLine)
	if line == nil {
		fmt.Fprintf(os.Stderr, "Error: Unknown line '%s'\n", *flagLine)
		fmt.Fprintf(os.Stderr, "Available lines: %v\n", lines.AvailableLines())
		os.Exit(1)
	}

	// Create and start proxy server
	proxyServer := proxy.NewServer(*flagPort)
	if err := proxyServer.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting proxy: %v\n", err)
		os.Exit(1)
	}
	defer proxyServer.Stop()

	// Create TUI model
	model := tui.NewModel(line, proxyServer.Port())

	// Apply initial settings
	train := model.GetTrain()
	train.SetSpeed(*flagSpeed)
	if *flagManual {
		train.ToggleManualMode()
	}

	// Create program with custom update hook to sync proxy state
	p := tea.NewProgram(
		&syncedModel{
			Model:       model,
			proxyServer: proxyServer,
		},
		tea.WithAltScreen(),
	)

	// Run the TUI
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}

// syncedModel wraps the TUI model to sync proxy state on each update
type syncedModel struct {
	tui.Model
	proxyServer *proxy.Server
	lastSync    time.Time
}

func (m *syncedModel) Init() tea.Cmd {
	return m.Model.Init()
}

func (m *syncedModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	newModel, cmd := m.Model.Update(msg)

	// Type assert back to our model
	if model, ok := newModel.(tui.Model); ok {
		m.Model = model

		// Sync proxy state on tick (not every message)
		if _, ok := msg.(tui.TickMsg); ok {
			condition := m.Model.GetCondition()
			m.proxyServer.SetCondition(condition)
		}
	}

	return m, cmd
}

func (m *syncedModel) View() string {
	return m.Model.View()
}

// GetCondition implements the interface for proxy sync
func (m *syncedModel) GetCondition() sim.NetworkCondition {
	return m.Model.GetCondition()
}
