package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"train-gap/internal/lines"
	"train-gap/internal/sim"
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
	width     int
	height    int
	quitting  bool
	frame     int
}

func NewModel(line *lines.Line, proxyPort int) Model {
	return Model{
		train:     sim.NewTrain(line),
		line:      line,
		condition: sim.FullConnectivity(),
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
		m.condition = m.train.Update(tickInterval)
		m.frame++
		return m, tick()
	}

	return m, nil
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	w := maxWidth
	if m.width < maxWidth+4 {
		w = m.width - 4
	}

	bg := lipgloss.Color("#16161e")
	lineColor := LineColors[m.line.ID]

	container := lipgloss.NewStyle().
		Width(w + 4).
		Background(bg).
		Padding(1, 2)

	var b strings.Builder
	b.WriteString(m.viewHeader(w, lineColor))
	b.WriteString("\n\n")
	b.WriteString(m.viewTrain(w, lineColor))
	b.WriteString("\n\n")
	b.WriteString(m.viewTrack(w, lineColor))
	b.WriteString("\n\n")
	b.WriteString(m.viewState(w, lineColor))
	b.WriteString("\n\n")
	b.WriteString(m.viewStatus(w))
	b.WriteString("\n\n")
	b.WriteString(m.viewHelp(w))

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
	if m.train.Direction > 0 {
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

func (m Model) viewTrain(w int, lineColor lipgloss.Color) string {
	var color lipgloss.Color
	if !m.condition.Connected {
		color = ColorDisconnected
	} else if m.condition.PacketLoss > 0.3 {
		color = ColorDegrading
	} else {
		color = lineColor
	}

	style := lipgloss.NewStyle().Foreground(color)

	// Clean subway car - no wheels, integrated with track
	car1 := "┌─────────────────────┐"
	car2 := fmt.Sprintf("│ ▐███▌   %s   ▐███▌ │",
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#fff")).Background(color).Render(" "+m.line.ID+" "))
	car3 := "└─────────────────────┘"

	trainW := 23
	pad := (w - trainW) / 2
	if pad < 0 {
		pad = 0
	}
	sp := strings.Repeat(" ", pad)

	return sp + style.Render(car1) + "\n" +
		sp + style.Render(car2) + "\n" +
		sp + style.Render(car3)
}

func (m Model) viewTrack(w int, lineColor lipgloss.Color) string {
	idx, prog := m.train.DisplayPosition()

	// Show 5 stations
	n := 5
	if n > len(m.line.Stations) {
		n = len(m.line.Stations)
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

	lineStyle := lipgloss.NewStyle().Foreground(lineColor)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#555"))
	bright := lipgloss.NewStyle().Foreground(lipgloss.Color("#fff")).Bold(true)

	// Calculate train position on track
	// prog goes from 0 (at station) to ~1 (arriving at next)
	trainStationIdx := idx - start // relative index in visible stations

	// Build track as continuous line with station dots
	var trackChars []string
	totalLen := n * colW

	// Pre-fill with spaces
	for i := 0; i < totalLen; i++ {
		trackChars = append(trackChars, " ")
	}

	// Place track segments and stations
	for i := 0; i < n; i++ {
		stationPos := i*colW + colW/2 // center of column

		// Draw track before this station (from previous station center to this station center)
		if i > 0 {
			prevPos := (i-1)*colW + colW/2
			isTunnel := false
			if seg := m.line.GetSegment(stations[i-1].ID, stations[i].ID); seg != nil && seg.IsTunnel {
				isTunnel = true
			}

			for j := prevPos + 1; j < stationPos; j++ {
				if isTunnel {
					trackChars[j] = dim.Render("┄")
				} else {
					trackChars[j] = lineStyle.Render("─")
				}
			}
		}

		// Place station dot
		realIdx := start + i
		isCurrentStation := realIdx == idx && prog < 0.1
		if isCurrentStation {
			trackChars[stationPos] = bright.Render("◉")
		} else {
			trackChars[stationPos] = lineStyle.Render("●")
		}
	}

	// Place moving train dot if in transit
	if prog >= 0.1 && prog < 0.95 {
		// Determine direction and calculate positions
		var fromPos, toPos int
		canShowDot := false

		if m.train.Direction > 0 {
			// Forward: moving from trainStationIdx to trainStationIdx+1
			if trainStationIdx >= 0 && trainStationIdx < n-1 {
				fromPos = trainStationIdx*colW + colW/2
				toPos = (trainStationIdx+1)*colW + colW/2
				canShowDot = true
			}
		} else {
			// Backward: moving from trainStationIdx to trainStationIdx-1
			if trainStationIdx > 0 && trainStationIdx < n {
				fromPos = trainStationIdx*colW + colW/2
				toPos = (trainStationIdx-1)*colW + colW/2
				canShowDot = true
			}
		}

		if canShowDot {
			// Map prog (0.1 to 0.9) to position in segment
			normalizedProg := (prog - 0.1) / 0.8 // 0 to 1
			if normalizedProg > 1.0 {
				normalizedProg = 1.0
			}

			// Interpolate between from and to
			trainPos := fromPos + int(normalizedProg*float64(toPos-fromPos))

			// Ensure we're within bounds and not on a station dot
			minPos, maxPos := fromPos, toPos
			if toPos < fromPos {
				minPos, maxPos = toPos, fromPos
			}
			if trainPos > minPos && trainPos < maxPos && trainPos >= 0 && trainPos < len(trackChars) {
				trackChars[trainPos] = bright.Render("●")
			}
		}
	}

	trackLine := strings.Join(trackChars, "")

	// Build name line
	var nameRow strings.Builder
	for i, st := range stations {
		name := st.ShortName
		if len(name) > colW-1 {
			name = name[:colW-2] + "."
		}

		totalPad := colW - len(name)
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

	return trackLine + "\n" + nameRow.String()
}

func (m Model) viewState(w int, lineColor lipgloss.Color) string {
	prog := m.train.Progress

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
	remaining := (100 - pct) / 10

	return fmt.Sprintf("%-10s %s %3d%% %s",
		style.Render(label),
		bar,
		pct,
		dim.Render(fmt.Sprintf("~%ds", remaining)))
}

func (m Model) viewStatus(w int) string {
	var icon, text string
	var color lipgloss.Color

	switch {
	case !m.condition.Connected:
		icon, text, color = "◯", "DISCONNECTED", ColorDisconnected
	case m.condition.PacketLoss > 0.5:
		icon, text, color = "◔", "POOR SIGNAL", ColorDisconnected
	case m.condition.PacketLoss > 0.3:
		icon, text, color = "◐", "DEGRADED", ColorDegrading
	case m.condition.PacketLoss > 0.1:
		icon, text, color = "◑", "RECOVERING", ColorRecovering
	default:
		icon, text, color = "●", "CONNECTED", ColorConnected
	}

	style := lipgloss.NewStyle().Foreground(color).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#666"))
	val := lipgloss.NewStyle().Foreground(lipgloss.Color("#fff"))

	left := style.Render(icon + " " + text)

	latency := fmt.Sprintf("+%dms", m.condition.Latency.Milliseconds())
	loss := fmt.Sprintf("%.0f%%", m.condition.PacketLoss*100)

	right := dim.Render("Lat ") + val.Render(latency) + dim.Render("  Loss ") + val.Render(loss)

	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	line1 := left + strings.Repeat(" ", gap) + right

	// Line 2: proxy
	uptime := time.Since(m.startTime).Round(time.Second)
	proxy := dim.Render("Proxy ") + val.Render(fmt.Sprintf("localhost:%d", m.proxyPort))

	mode := ""
	if m.train.Paused {
		mode = style.Render(" PAUSED")
	} else if m.train.ManualMode {
		mode = lipgloss.NewStyle().Foreground(ColorRecovering).Bold(true).Render(" MANUAL")
	} else if m.train.Speed != 1.0 {
		mode = dim.Render(fmt.Sprintf(" %.1fx", m.train.Speed))
	}

	up := dim.Render(fmt.Sprintf("Up %dm%02ds", int(uptime.Minutes()), int(uptime.Seconds())%60))

	gap2 := w - lipgloss.Width(proxy) - lipgloss.Width(mode) - lipgloss.Width(up)
	if gap2 < 1 {
		gap2 = 1
	}

	line2 := proxy + mode + strings.Repeat(" ", gap2) + up

	return line1 + "\n" + line2
}

func (m Model) viewHelp(w int) string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#444"))
	key := lipgloss.NewStyle().Foreground(lipgloss.Color("#666"))

	help := key.Render("spc") + dim.Render(" pause  ") +
		key.Render("←→") + dim.Render(" speed  ") +
		key.Render("m") + dim.Render(" manual  ") +
		key.Render("q") + dim.Render(" quit")

	pad := (w - lipgloss.Width(help)) / 2
	if pad < 0 {
		pad = 0
	}

	return strings.Repeat(" ", pad) + help
}

func (m Model) GetCondition() sim.NetworkCondition {
	return m.condition
}

func (m Model) GetTrain() *sim.Train {
	return m.train
}
