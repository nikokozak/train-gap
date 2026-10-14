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
	"github.com/nikokozak/train-gap/rides"
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

// The view must stay inside the terminal at every size, from a wide window to a small pane,
// with the ride list closed and open.
func TestViewFitsTheTerminal(t *testing.T) {
	proxyServer := startTestProxy(t)
	choices := rides.All()
	sizes := []struct{ width, height int }{{120, 30}, {80, 24}, {60, 20}, {40, 12}, {80, 9}, {24, 10}, {20, 8}, {10, 3}}
	for current, chosen := range choices {
		ride := testRide(t, chosen)
		for _, size := range sizes {
			for _, isChoosing := range []bool{false, true} {
				model := NewModel(ride, proxyServer, choices, current)
				model.isChoosing = isChoosing
				updated, _ := model.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
				rows := strings.Split(updated.View(), "\n")
				if len(rows) > size.height {
					t.Errorf("ride %s at %dx%d (list open %v): %d rows", chosen.Name, size.width, size.height, isChoosing, len(rows))
				}
				for _, row := range rows {
					if lipgloss.Width(row) > size.width {
						t.Errorf("ride %s at %dx%d (list open %v): row %q is %d wide", chosen.Name, size.width, size.height, isChoosing, row, lipgloss.Width(row))
					}
				}
			}
		}
	}
}

// tab, down, enter switches to the next ride in the list.
func TestRideListSwitchesTheRide(t *testing.T) {
	choices := rides.All()
	if len(choices) < 2 {
		t.Skip("needs two shipped rides")
	}
	ride := testRide(t, choices[0])
	var model tea.Model = NewModel(ride, startTestProxy(t), choices, 0)
	for _, key := range []tea.KeyMsg{{Type: tea.KeyTab}, {Type: tea.KeyDown}, {Type: tea.KeyEnter}} {
		model, _ = model.Update(key)
	}
	if got := model.(Model); got.current != 1 || got.isChoosing {
		t.Fatalf("after tab, down, enter: current %d, list open %v; want 1, closed", got.current, got.isChoosing)
	}
	if phase, _, _ := ride.Position(); ride.Line().Stations[phase.FromIndex].ID != choices[1].FirstStationID() {
		t.Fatalf("the ride did not switch: at %s; want the start of %s", ride.Line().Stations[phase.FromIndex].ID, choices[1].Name)
	}
}

func startTestProxy(t *testing.T) *proxy.Server {
	t.Helper()
	proxyServer := proxy.NewServer(0, func() sim.Condition { return sim.Condition{IsConnected: true} })
	if err := proxyServer.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proxyServer.Stop() })
	return proxyServer
}

func testRide(t *testing.T, chosen rides.Ride) *sim.Ride {
	t.Helper()
	line := lines.Get(chosen.LineID)
	timeline, err := rides.RoundTrip(chosen, line)
	if err != nil {
		t.Fatal(err)
	}
	return sim.NewRide(line, timeline)
}
