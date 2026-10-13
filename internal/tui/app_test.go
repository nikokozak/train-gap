package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nikokozak/train-gap/internal/lines"
	"github.com/nikokozak/train-gap/internal/proxy"
	"github.com/nikokozak/train-gap/internal/sim"
)

func TestSignalBars(t *testing.T) {
	tests := []struct {
		condition sim.Condition
		want      int
	}{
		{sim.Condition{IsConnected: false}, 0},
		{sim.Condition{IsConnected: true, Latency: 500 * time.Millisecond}, 1},
		{sim.Condition{IsConnected: true, Latency: 200 * time.Millisecond}, 2},
		{sim.Condition{IsConnected: true, Latency: 100 * time.Millisecond}, 3},
		{sim.Condition{IsConnected: true, Latency: 40 * time.Millisecond}, 4},
	}
	for _, test := range tests {
		if got := signalBars(test.condition); got != test.want {
			t.Errorf("signalBars(%+v) = %d; want %d", test.condition, got, test.want)
		}
	}
}

// The view must stay inside the terminal at every size, from a wide window to a small pane.
func TestViewFitsTheTerminal(t *testing.T) {
	proxyServer := proxy.NewServer(0, func() sim.Condition { return sim.Condition{IsConnected: true} })
	if err := proxyServer.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proxyServer.Stop() })

	sizes := []struct{ width, height int }{{120, 30}, {80, 24}, {60, 20}, {40, 12}, {80, 9}, {24, 10}, {20, 8}, {10, 3}}
	for _, lineID := range []string{"L", "3", "S"} { // S, the shuttle, has the fewest stations.
		line := lines.Get(lineID)
		ride := sim.NewRide(line, sim.NewTimeline(line))
		ride.SetSpeed(10)
		for _, size := range sizes {
			model, _ := NewModel(ride, proxyServer).Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
			rows := strings.Split(model.View(), "\n")
			if len(rows) > size.height {
				t.Errorf("line %s at %dx%d: %d rows", lineID, size.width, size.height, len(rows))
			}
			for _, row := range rows {
				if lipgloss.Width(row) > size.width {
					t.Errorf("line %s at %dx%d: row %q is %d wide", lineID, size.width, size.height, row, lipgloss.Width(row))
				}
			}
		}
	}
}
