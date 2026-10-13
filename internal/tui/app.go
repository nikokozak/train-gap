package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nikokozak/train-gap/internal/lines"
	"github.com/nikokozak/train-gap/internal/sim"
)

const (
	maxWidth     = 64
	tickInterval = 50 * time.Millisecond
	speedStep    = 1.5 // Each arrow key press changes the speed by this factor.
)

type TickMsg time.Time

// Model draws a ride. The ride runs on its own clock; the model only reads it and passes keys on.
type Model struct {
	ride      *sim.Ride
	line      *lines.Line
	proxyPort int
	startTime time.Time
	width     int
	height    int
	quitting  bool
	frame     int
}

// NewModel shows the ride and the proxy port that applies it.
func NewModel(ride *sim.Ride, proxyPort int) Model {
	return Model{
		ride:      ride,
		line:      ride.Line,
		proxyPort: proxyPort,
		startTime: time.Now(),
		width:     80,
		height:    24,
	}
}

func (m Model) Init() tea.Cmd {
	return tick()
}

func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case " ":
			m.ride.SetPaused(!m.ride.IsPaused())
		case "right", "l":
			m.ride.SetSpeed(m.ride.Speed() * speedStep)
		case "left", "h":
			m.ride.SetSpeed(m.ride.Speed() / speedStep)
		case "n", "enter":
			m.ride.SkipToNextPhase()
		case "r":
			m.ride.SetSpeed(1.0)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case TickMsg:
		// The ride keeps its own time; a tick only redraws.
		m.frame++
		return m, tick()
	}

	return m, nil
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	if m.line == nil || m.ride == nil {
		return "Train-Gap: internal error (missing model)"
	}

	if m.width <= 0 || m.height <= 0 {
		return ""
	}

	w := maxWidth
	if m.width < maxWidth+4 {
		w = m.width - 4
	}
	if w < 20 {
		return lipgloss.NewStyle().
			Padding(1, 2).
			Render("TRAIN-GAP\n\nWindow too small.\nResize your terminal.")
	}

	bg := lipgloss.Color("#16161e")
	lineColor := LineColors[m.line.ID]

	container := lipgloss.NewStyle().
		Width(w+4).
		Background(bg).
		Padding(1, 2)

	var b strings.Builder
	b.WriteString(m.viewHeader(w, lineColor))
	b.WriteString("\n\n")
	b.WriteString(m.viewTrack(w, lineColor))
	b.WriteString("\n\n")
	b.WriteString(m.viewState(w, lineColor))
	b.WriteString("\n\n")
	b.WriteString(m.viewMetrics(w))
	b.WriteString("\n\n")
	b.WriteString(m.viewFooter(w))

	box := container.Render(b.String())

	// Center
	if m.width > lipgloss.Width(box) {
		pad := (m.width - lipgloss.Width(box)) / 2
		box = strings.Repeat(" ", pad) + box
	}

	return box
}

func (m Model) viewHeader(w int, lineColor lipgloss.Color) string {
	// Destination based on direction
	phase, _, _ := m.ride.Position()
	destination := m.line.Stations[0].ShortName
	if phase.IsForward {
		destination = m.line.Stations[len(m.line.Stations)-1].ShortName
	}

	badge := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#000")).
		Background(lineColor).
		Padding(0, 1).
		Render(m.line.ID)

	dest := lipgloss.NewStyle().
		Foreground(lineColor).
		Render(destination)

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#fff")).
		Render("TRAIN-GAP")

	from := m.line.Stations[phase.FromIndex].ShortName
	to := m.line.Stations[phase.ToIndex].ShortName
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#666"))
	bright := lipgloss.NewStyle().Foreground(lipgloss.Color("#fff")).Bold(true)
	right := dim.Render("@ ") + bright.Render(from)
	if phase.Kind == sim.Running {
		right = bright.Render(from) + dim.Render(" → ") + bright.Render(to)
	}

	left := badge + " " + dest + "  " + title

	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	return left + strings.Repeat(" ", gap) + right
}

func (m Model) viewTrack(w int, lineColor lipgloss.Color) string {
	phase, _, offset := m.ride.Position()
	idx := phase.FromIndex

	// Show 5 stations
	n := 5
	if n > len(m.line.Stations) {
		n = len(m.line.Stations)
	}

	// Ensure we have enough width per station to avoid division-by-zero and negative padding.
	minColW := 4
	if n > 1 && w/n < minColW {
		n = w / minColW
		if n < 1 {
			n = 1
		}
		if n > len(m.line.Stations) {
			n = len(m.line.Stations)
		}
	}

	start := idx - n/2
	if start < 0 {
		start = 0
	}
	end := start + n
	if end > len(m.line.Stations) {
		end = len(m.line.Stations)
		start = end - n
		if start < 0 {
			start = 0
		}
	}

	stations := m.line.Stations[start:end]
	colW := w / n
	if colW < 1 {
		colW = 1
	}
	totalLen := n * colW

	lineStyle := lipgloss.NewStyle().Foreground(lineColor)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#555"))

	// Train badge style - MTA grey background with white text
	trainBadge := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#fff")).
		Background(lineColor).
		Render(" " + m.line.ID + " ")
	trainBadgeWidth := lipgloss.Width(trainBadge)
	if trainBadgeWidth < 1 {
		trainBadgeWidth = 1
	}

	// Calculate station center positions
	stationCenters := make([]int, n)
	for i := 0; i < n; i++ {
		stationCenters[i] = i*colW + colW/2
	}

	// Calculate train position
	trainFloatPos := float64(idx - start)
	if phase.Kind == sim.Running && phase.Duration > 0 {
		runProgress := float64(offset) / float64(phase.Duration)
		trainFloatPos += float64(phase.ToIndex-phase.FromIndex) * runProgress
	}

	// Convert to character position
	trainCharPos := int(trainFloatPos*float64(colW) + float64(colW)/2)
	trainStart := trainCharPos - trainBadgeWidth/2
	trainEnd := trainStart + trainBadgeWidth
	useBadge := trainStart >= 0 && trainEnd <= totalLen
	if trainCharPos < 0 {
		trainCharPos = 0
	} else if trainCharPos >= totalLen {
		trainCharPos = totalLen - 1
	}
	trainMarker := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#fff")).
		Background(lineColor).
		Render("■")

	// Build track line
	var trackLine strings.Builder

	for i := 0; i < totalLen; i++ {
		// Check if this position is where the train badge goes
		if useBadge && i >= trainStart && i < trainEnd {
			if i == trainStart {
				trackLine.WriteString(trainBadge)
			}
			continue
		}
		if !useBadge && i == trainCharPos {
			trackLine.WriteString(trainMarker)
			continue
		}

		// Check if this is a station center
		isStation := false
		for _, center := range stationCenters {
			if i == center {
				isStation = true
				break
			}
		}

		if isStation {
			trackLine.WriteString(lineStyle.Render("●"))
			continue
		}

		// Check if we're between two stations (should draw track)
		inTrackSegment := false
		var segmentIdx int
		for j := 0; j < n-1; j++ {
			if i > stationCenters[j] && i < stationCenters[j+1] {
				inTrackSegment = true
				segmentIdx = j
				break
			}
		}

		if inTrackSegment {
			// A dotted track is a tunnel without service.
			if !m.line.Segments[start+segmentIdx].HasService {
				trackLine.WriteString(dim.Render("┄"))
			} else {
				trackLine.WriteString(lineStyle.Render("─"))
			}
		} else {
			trackLine.WriteString(" ")
		}
	}

	// Build name line
	bright := lipgloss.NewStyle().Foreground(lipgloss.Color("#fff")).Bold(true)
	var nameRow strings.Builder
	for i, st := range stations {
		name := st.ShortName
		maxName := colW - 1
		if maxName < 1 {
			maxName = 1
		}
		if len(name) > maxName {
			if maxName == 1 {
				name = "."
			} else {
				name = name[:maxName-1] + "."
			}
		}

		totalPad := colW - len(name)
		if totalPad < 0 {
			totalPad = 0
		}
		leftPad := totalPad / 2
		rightPad := totalPad - leftPad

		realIdx := start + i
		isHere := realIdx == idx

		if isHere {
			nameRow.WriteString(strings.Repeat(" ", leftPad))
			nameRow.WriteString(bright.Render(name))
			nameRow.WriteString(strings.Repeat(" ", rightPad))
		} else {
			nameRow.WriteString(strings.Repeat(" ", leftPad))
			nameRow.WriteString(dim.Render(name))
			nameRow.WriteString(strings.Repeat(" ", rightPad))
		}
	}

	return trackLine.String() + "\n" + nameRow.String()
}

// viewState shows what the train is doing and how long until the next stop or run starts.
func (m Model) viewState(w int, lineColor lipgloss.Color) string {
	phase, _, offset := m.ride.Position()
	condition := phase.ConditionAt(offset)

	var color lipgloss.Color
	var label string
	switch {
	case phase.Kind == sim.AtStation:
		color, label = ColorConnected, "At station"
	case !condition.IsConnected:
		color, label = ColorDisconnected, "No service"
	default:
		color, label = ColorDegrading, "Running"
	}
	style := lipgloss.NewStyle().Foreground(color).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#555"))

	progress := 0.0
	if phase.Duration > 0 {
		progress = min(1, float64(offset)/float64(phase.Duration))
	}
	barW := max(10, w-24)
	filled := int(progress * float64(barW))

	// The bar pulses while there is no service, unless the ride is paused.
	char := "█"
	if !condition.IsConnected && !m.ride.IsPaused() {
		char = "▓"
		if (m.frame/6)%2 == 1 {
			char = "░"
		}
	}
	bar := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat(char, filled)) +
		dim.Render(strings.Repeat("░", barW-filled))

	// Time left in this phase, in real seconds at the current speed.
	remaining := "held"
	if !m.ride.IsPaused() {
		remainingTime := time.Duration(float64(phase.Duration-offset) / m.ride.Speed())
		remaining = fmt.Sprintf("%ds left", int(remainingTime.Round(time.Second).Seconds()))
	}

	return fmt.Sprintf("%-10s %s %s", style.Render(label), bar, dim.Render(remaining))
}

// viewMetrics shows the condition the proxy applies right now.
func (m Model) viewMetrics(w int) string {
	condition := m.ride.Condition()
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#666"))
	value := lipgloss.NewStyle().Foreground(lipgloss.Color("#fff")).Bold(true)

	var icon, statusText string
	var statusColor lipgloss.Color
	switch {
	case !condition.IsConnected:
		icon, statusText, statusColor = "◯", "NO SERVICE", ColorDisconnected
	case condition.IsWeak():
		icon, statusText, statusColor = "◐", "WEAK SIGNAL", ColorRecovering
	default:
		icon, statusText, statusColor = "●", "CONNECTED", ColorConnected
	}
	statusLine := lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(icon + " " + statusText)
	if !condition.IsConnected {
		statusLine += dim.Render("  connections wait for service")
	}

	// Added delay, on a scale where 500 ms fills the bar.
	barW := max(10, w-28)
	delayText := "    —"
	filled := 0
	if condition.IsConnected {
		delayText = fmt.Sprintf("%4dms", condition.Latency.Milliseconds())
		filled = min(barW, int(float64(barW)*float64(condition.Latency)/float64(500*time.Millisecond)))
	}
	delayBar := lipgloss.NewStyle().Foreground(statusColor).Render(strings.Repeat("█", filled)) +
		dim.Render(strings.Repeat("░", barW-filled))
	delayLine := dim.Render("Added delay  ") + value.Render(delayText) + "  " + delayBar

	return statusLine + "\n\n" + delayLine
}

func (m Model) viewFooter(w int) string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#555"))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("#888"))

	proxyAddr := accent.Render(fmt.Sprintf("socks5://localhost:%d", m.proxyPort))

	// Mode and speed are both shown, so a paused ride still says how fast it will run.
	var mode string
	if recordedAt := m.ride.Timeline.RecordedAt; !recordedAt.IsZero() {
		mode = accent.Render("REPLAY " + recordedAt.Local().Format("Jan 2 15:04"))
	}
	if m.ride.IsPaused() {
		mode = strings.TrimSpace(mode + " " + lipgloss.NewStyle().Foreground(ColorDegrading).Bold(true).Render("PAUSED"))
	}
	if speed := m.ride.Speed(); speed != 1.0 {
		mode = strings.TrimSpace(mode + " " + dim.Render(fmt.Sprintf("%.1fx", speed)))
	}

	uptime := time.Since(m.startTime).Round(time.Second)
	uptimeStr := accent.Render(fmt.Sprintf("Up %dm%02ds", int(uptime.Minutes()), int(uptime.Seconds())%60))

	left := proxyAddr
	right := uptimeStr
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if mode != "" {
		gap = gap - lipgloss.Width(mode) - 2
	}
	gap = max(gap, 2) // At least one space on each side of the mode.

	var line string
	if mode != "" {
		leftGap := gap / 2
		rightGap := gap - leftGap
		line = left + strings.Repeat(" ", leftGap) + mode + strings.Repeat(" ", rightGap) + right
	} else {
		line = left + strings.Repeat(" ", gap) + right
	}

	help := dim.Render("spc") + accent.Render(" pause  ") +
		dim.Render("n") + accent.Render(" next  ") +
		dim.Render("←→") + accent.Render(" speed  ") +
		dim.Render("q") + accent.Render(" quit")

	helpPad := (w - lipgloss.Width(help)) / 2
	if helpPad < 0 {
		helpPad = 0
	}

	return line + "\n\n" + strings.Repeat(" ", helpPad) + help
}
