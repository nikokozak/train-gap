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
)

type TickMsg time.Time

type Model struct {
	train     *sim.Train
	line      *lines.Line
	condition sim.NetworkCondition
	proxyPort int
	startTime time.Time
	lastTick  time.Time
	width     int
	height    int
	quitting  bool
	frame     int
}

func NewModel(line *lines.Line, proxyPort int) Model {
	now := time.Now()
	return Model{
		train:     sim.NewTrain(line),
		line:      line,
		condition: sim.FullConnectivity(),
		proxyPort: proxyPort,
		startTime: now,
		lastTick:  now,
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
			m.train.TogglePause()
		case "right", "l":
			m.train.IncreaseSpeed()
		case "left", "h":
			m.train.DecreaseSpeed()
		case "m":
			m.train.ToggleManualMode()
		case "n", "enter":
			m.train.ManualAdvance()
		case "r":
			m.train.SetSpeed(1.0)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case TickMsg:
		now := time.Time(msg)
		dt := now.Sub(m.lastTick)
		if dt <= 0 {
			dt = tickInterval
		}
		// Avoid huge jumps after pauses/suspends.
		if dt > 2*time.Second {
			dt = 2 * time.Second
		}
		m.lastTick = now
		m.condition = m.train.Update(dt)
		m.frame++
		return m, tick()
	}

	return m, nil
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	if m.line == nil || m.train == nil {
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
	var destination string
	if len(m.line.Stations) == 0 {
		destination = "---"
	} else if m.train.Direction > 0 {
		// Going towards end of line (Brooklyn/Canarsie for L)
		destination = m.line.Stations[len(m.line.Stations)-1].ShortName
	} else {
		// Going towards start of line (Manhattan/8th Ave for L)
		destination = m.line.Stations[0].ShortName
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

	station := "---"
	if s := m.train.CurrentStation(); s != nil {
		station = s.ShortName
	}

	left := badge + " " + dest + "  " + title
	right := lipgloss.NewStyle().Foreground(lipgloss.Color("#666")).Render("@ ") +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#fff")).Bold(true).Render(station)

	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	return left + strings.Repeat(" ", gap) + right
}

func (m Model) viewTrack(w int, lineColor lipgloss.Color) string {
	idx, prog := m.train.DisplayPosition()

	if len(m.line.Stations) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#666")).Render("No stations configured.")
	}

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
	trainStationIdx := idx - start
	var trainFloatPos float64
	if prog < 0.1 {
		trainFloatPos = float64(trainStationIdx)
	} else {
		normalizedProg := (prog - 0.1) / 0.8
		if normalizedProg > 1.0 {
			normalizedProg = 1.0
		}
		if m.train.Direction > 0 {
			trainFloatPos = float64(trainStationIdx) + normalizedProg
		} else {
			trainFloatPos = float64(trainStationIdx) - normalizedProg
		}
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
			// Check if this segment is a tunnel
			isTunnel := false
			if seg := m.line.GetSegment(stations[segmentIdx].ID, stations[segmentIdx+1].ID); seg != nil && seg.IsTunnel {
				isTunnel = true
			}
			if isTunnel {
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

func (m Model) viewState(w int, lineColor lipgloss.Color) string {
	prog := m.train.Progress
	if prog < 0 {
		prog = 0
	} else if prog > 1 {
		prog = 1
	}

	var color lipgloss.Color
	var label string

	switch m.train.State {
	case sim.AtStation:
		color = ColorConnected
		label = "Boarding"
	case sim.Departing:
		color = ColorDegrading
		label = "Departing"
	case sim.InTransit:
		if m.train.IsInTunnel() {
			color = ColorDisconnected
			label = "In Tunnel"
		} else {
			color = ColorDisconnected // Show as bad since loss is high
			label = "In Transit"
		}
	case sim.Arriving:
		color = ColorRecovering
		label = "Arriving"
	}

	style := lipgloss.NewStyle().Foreground(color).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#555"))

	// Progress bar
	barW := w - 24
	if barW < 10 {
		barW = 10
	}

	filled := int(prog * float64(barW))
	empty := barW - filled

	// Pulse in tunnel or heavy loss
	char := "█"
	if m.train.State == sim.InTransit {
		if (m.frame/6)%2 == 0 {
			char = "▓"
		} else {
			char = "░"
		}
	}

	bar := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat(char, filled)) +
		dim.Render(strings.Repeat("░", empty))

	pct := int(prog * 100)
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	remaining := (100 - pct) / 10
	if remaining < 0 {
		remaining = 0
	}

	return fmt.Sprintf("%-10s %s %3d%% %s",
		style.Render(label),
		bar,
		pct,
		dim.Render(fmt.Sprintf("~%ds", remaining)))
}

func (m Model) viewMetrics(w int) string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#666"))
	val := lipgloss.NewStyle().Foreground(lipgloss.Color("#fff")).Bold(true)

	// Connection status
	var icon, statusText string
	var statusColor lipgloss.Color

	switch {
	case !m.condition.Connected:
		icon, statusText, statusColor = "◯", "DISCONNECTED", ColorDisconnected
	case m.condition.PacketLoss > 0.5:
		icon, statusText, statusColor = "◔", "POOR SIGNAL", ColorDisconnected
	case m.condition.PacketLoss > 0.3:
		icon, statusText, statusColor = "◐", "DEGRADED", ColorDegrading
	case m.condition.PacketLoss > 0.1:
		icon, statusText, statusColor = "◑", "RECOVERING", ColorRecovering
	default:
		icon, statusText, statusColor = "●", "CONNECTED", ColorConnected
	}

	statusStyle := lipgloss.NewStyle().Foreground(statusColor).Bold(true)
	statusLine := statusStyle.Render(icon + " " + statusText)

	// Latency bar
	latencyMs := m.condition.Latency.Milliseconds()
	if latencyMs < 0 {
		latencyMs = 0
	}
	latencyPct := float64(latencyMs) / 500.0 // 500ms = 100%
	if latencyPct > 1.0 {
		latencyPct = 1.0
	}
	barW := w - 28 // Space for label and value
	if barW < 10 {
		barW = 10
	}
	latencyFilled := int(latencyPct * float64(barW))
	latencyEmpty := barW - latencyFilled

	latencyBar := lipgloss.NewStyle().Foreground(statusColor).Render(strings.Repeat("█", latencyFilled)) +
		dim.Render(strings.Repeat("░", latencyEmpty))
	latencyLine := dim.Render("Latency      ") + val.Render(fmt.Sprintf("%4dms", latencyMs)) + "  " + latencyBar

	// Packet Loss bar
	lossPct := m.condition.PacketLoss
	if lossPct < 0 {
		lossPct = 0
	} else if lossPct > 1 {
		lossPct = 1
	}
	lossFilled := int(lossPct * float64(barW))
	lossEmpty := barW - lossFilled

	var lossColor lipgloss.Color
	if lossPct > 0.5 {
		lossColor = ColorDisconnected
	} else if lossPct > 0.2 {
		lossColor = ColorDegrading
	} else {
		lossColor = ColorConnected
	}

	lossBar := lipgloss.NewStyle().Foreground(lossColor).Render(strings.Repeat("█", lossFilled)) +
		dim.Render(strings.Repeat("░", lossEmpty))
	lossLine := dim.Render("Packet Loss  ") + val.Render(fmt.Sprintf("%5.0f%%", lossPct*100)) + "  " + lossBar

	return statusLine + "\n\n" + latencyLine + "\n" + lossLine
}

func (m Model) viewFooter(w int) string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#555"))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("#888"))

	// Left: full proxy address
	proxyAddr := accent.Render(fmt.Sprintf("socks5://localhost:%d", m.proxyPort))

	// Mode indicator (if any)
	var mode string
	if m.train.Paused {
		mode = lipgloss.NewStyle().Foreground(ColorDegrading).Bold(true).Render("PAUSED")
	} else if m.train.ManualMode {
		mode = lipgloss.NewStyle().Foreground(ColorRecovering).Bold(true).Render("MANUAL")
	} else if m.train.Speed != 1.0 {
		mode = dim.Render(fmt.Sprintf("%.1fx", m.train.Speed))
	}

	// Right: uptime
	uptime := time.Since(m.startTime).Round(time.Second)
	uptimeStr := accent.Render(fmt.Sprintf("Up %dm%02ds", int(uptime.Minutes()), int(uptime.Seconds())%60))

	// Build line with proper spacing
	left := proxyAddr
	right := uptimeStr

	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if mode != "" {
		gap = gap - lipgloss.Width(mode) - 2
	}
	if gap < 1 {
		gap = 1
	}

	var line string
	if mode != "" {
		leftGap := gap / 2
		rightGap := gap - leftGap
		line = left + strings.Repeat(" ", leftGap) + mode + strings.Repeat(" ", rightGap) + right
	} else {
		line = left + strings.Repeat(" ", gap) + right
	}

	// Help hints
	help := dim.Render("spc") + accent.Render(" pause  ") +
		dim.Render("←→") + accent.Render(" speed  ") +
		dim.Render("m") + accent.Render(" manual  ") +
		dim.Render("q") + accent.Render(" quit")

	helpPad := (w - lipgloss.Width(help)) / 2
	if helpPad < 0 {
		helpPad = 0
	}

	return line + "\n\n" + strings.Repeat(" ", helpPad) + help
}

func (m Model) GetCondition() sim.NetworkCondition {
	return m.condition
}

func (m Model) GetTrain() *sim.Train {
	return m.train
}
