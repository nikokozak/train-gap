// Package tui draws a ride in the terminal: the strip map, the network condition, and the
// connections through the proxy.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nikokozak/train-gap/internal/lines"
	"github.com/nikokozak/train-gap/internal/proxy"
	"github.com/nikokozak/train-gap/internal/sim"
	"github.com/nikokozak/train-gap/rides"
)

const (
	tickInterval = 50 * time.Millisecond
	speedStep    = 1.5 // Each arrow key press changes the speed by this factor.

	maxContentWidth = 72 // Wide enough for the connection log; wider only adds empty space.
	minContentWidth = 36 // Below this, the view is one summary line.
	// Vertical space around the content: one row of padding above and below.
	verticalPadding = 2
	// Horizontal space around the content: two columns of padding on each side.
	horizontalPadding = 4

	visibleStations   = 5
	minStationColumns = 8 // Narrower columns cannot show a station name.
	maxLogRows        = 6
	narrowLogWidth    = 60 // Below this, log rows leave out the time and the byte count.
)

type tickMsg time.Time

// Model draws a ride. The ride runs on its own clock and the proxy keeps its own log; the model
// only reads them, passes keys on, and switches rides from the ride list.
type Model struct {
	ride        *sim.Ride
	line        *lines.Line // The ride's line; changes only when the model switches rides.
	proxyServer *proxy.Server
	choices     []rides.Ride // The ride list.
	current     int          // Index into choices of the ride that plays.
	isChoosing  bool         // The ride list is open.
	cursor      int          // Index into choices of the highlighted ride.
	width       int
	height      int
	quitting    bool
}

// NewModel shows the ride, the proxy that applies it, and a list of rides to switch to.
// choices[current] is the ride that plays.
func NewModel(ride *sim.Ride, proxyServer *proxy.Server, choices []rides.Ride, current int) Model {
	return Model{ride: ride, line: ride.Line(), proxyServer: proxyServer, choices: choices, current: current,
		cursor: current, width: 80, height: 24}
}

func (m Model) Init() tea.Cmd {
	return tick()
}

func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.isChoosing {
			return m.updateRideList(msg)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "tab":
			m.isChoosing, m.cursor = true, m.current
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
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		return m, tick() // The ride keeps its own time; a tick only redraws.
	}
	return m, nil
}

// updateRideList moves through the ride list and switches to the chosen ride.
func (m Model) updateRideList(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(len(m.choices)-1, m.cursor+1)
	case "esc", "tab":
		m.isChoosing = false
	case "enter":
		chosen := m.choices[m.cursor]
		line := lines.Get(chosen.LineID)
		// TestEmbeddedRides checks every shipped ride, and main checked a ride from a file, so an
		// error here would be a bug; the ride that plays then stays.
		if timeline, err := rides.RoundTrip(chosen, line); err == nil {
			m.ride.Switch(line, timeline)
			m.line, m.current = line, m.cursor
		}
		m.isChoosing = false
	}
	return m, nil
}

// View fits the panel to the terminal. Space goes first to the strip map, the condition, and the
// controls; the connection log gets what is left; blank separators go first when space is short.
func (m Model) View() string {
	if m.quitting || m.width <= 0 || m.height <= 0 {
		return ""
	}
	width := min(maxContentWidth, m.width-horizontalPadding)
	availableRows := m.height - verticalPadding
	const rowsWithoutLog = 7 // header 1, track 2, condition 2, footer 2
	if width < minContentWidth || availableRows < rowsWithoutLog {
		return m.viewSummaryLine()
	}

	sections := [][]string{m.viewHeader(width), m.viewTrack(width), m.viewCondition(width), m.viewFooter(width)}
	if m.isChoosing {
		// The list takes the place of the strip map, the condition, and the log.
		const headerAndFooterRows, separatorRows = 3, 2
		list := m.viewRideList(width, availableRows-headerAndFooterRows-separatorRows)
		sections = [][]string{sections[0], list, sections[len(sections)-1]}
	}
	isSpacious := availableRows >= rowsWithoutLog+len(sections)-1
	separatorRows := 0
	if isSpacious {
		separatorRows = len(sections) - 1
	}
	// The log needs its own separator, a title row, and at least one connection row.
	if logRows := min(maxLogRows, availableRows-rowsWithoutLog-separatorRows-2); isSpacious && logRows >= 1 && !m.isChoosing {
		footer := sections[len(sections)-1]
		sections = append(sections[:len(sections)-1], m.viewLog(width, logRows), footer)
	}

	var rows []string
	for index, section := range sections {
		if index > 0 && isSpacious {
			rows = append(rows, "")
		}
		rows = append(rows, section...)
	}
	for index, row := range rows {
		rows[index] = fitWidth(row, width)
	}

	panel := lipgloss.NewStyle().
		Background(colorPanel).
		Padding(verticalPadding/2, horizontalPadding/2).
		Width(width + horizontalPadding).
		Render(strings.Join(rows, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

// viewSummaryLine is the whole view in a pane too small for the panel.
func (m Model) viewSummaryLine() string {
	condition := m.ride.Condition()
	summary := fmt.Sprintf("%s %s  localhost:%d", m.line.ID, strings.ToUpper(condition.Describe()), m.proxyServer.Port())
	return fitWidth(summary, m.width)
}

// viewHeader shows the line, where the train is heading, and the app name.
func (m Model) viewHeader(width int) []string {
	phase, _, _ := m.ride.Position()
	destination := m.line.Stations[0]
	if phase.IsForward {
		destination = m.line.Stations[len(m.line.Stations)-1]
	}
	left := badge(m.line.ID) + paint(colorText, " ▸ ") + paintBold(colorBright, destination.ShortName)
	return []string{spread(left, paintBold(colorDim, "TRAIN-GAP"), width)}
}

// viewTrack draws the strip map: a few stations around the train, the track between them, and
// the train moving along it. Track the ride recorded is solid where every second had service and
// dotted where some had none; track it did not record is faint.
func (m Model) viewTrack(width int) []string {
	phase, _, offset := m.ride.Position()
	segmentHasService := map[int]bool{} // Segment k joins stations k and k+1. Absent: not recorded.
	isOnRide := map[int]bool{}
	for _, ridePhase := range m.ride.Timeline().Phases {
		isOnRide[ridePhase.FromIndex], isOnRide[ridePhase.ToIndex] = true, true
		if ridePhase.Kind == sim.Running {
			segment := min(ridePhase.FromIndex, ridePhase.ToIndex)
			hasService, isKnown := segmentHasService[segment]
			segmentHasService[segment] = ridePhase.HasService && (hasService || !isKnown)
		}
	}

	stations := m.line.Stations
	stationCount := min(visibleStations, len(stations), width/minStationColumns)
	firstIndex := min(max(0, phase.FromIndex-stationCount/2), len(stations)-stationCount)
	columnWidth := width / stationCount
	trackWidth := stationCount * columnWidth

	// The train's position in columns from the first visible station. A run moves it smoothly.
	trainColumns := float64(phase.FromIndex - firstIndex)
	if phase.Kind == sim.Running && phase.Duration > 0 {
		progress := min(1, float64(offset)/float64(phase.Duration))
		trainColumns += float64(phase.ToIndex-phase.FromIndex) * progress
	}
	train := badge(m.line.ID)
	trainWidth := lipgloss.Width(train)
	trainStart := int(trainColumns*float64(columnWidth)) + columnWidth/2 - trainWidth/2
	trainStart = min(max(0, trainStart), trackWidth-trainWidth)

	color := lineColor(m.line.ID)
	lastCenter := (stationCount-1)*columnWidth + columnWidth/2
	var track strings.Builder
	for position := 0; position < trackWidth; position++ {
		if position == trainStart {
			track.WriteString(train)
			position += trainWidth - 1
			continue
		}
		// Station k sits in the middle of column k; segment k runs between stations k and k+1.
		fromFirstCenter := position - columnWidth/2
		switch {
		case fromFirstCenter < 0 || position > lastCenter:
			track.WriteString(paint(colorText, " ")) // Before the first or after the last station.
		case fromFirstCenter%columnWidth == 0 && isOnRide[firstIndex+fromFirstCenter/columnWidth]:
			track.WriteString(paint(color, "●"))
		case fromFirstCenter%columnWidth == 0:
			track.WriteString(paint(colorFaint, "●"))
		default:
			hasService, isRecorded := segmentHasService[firstIndex+fromFirstCenter/columnWidth]
			switch {
			case !isRecorded:
				track.WriteString(paint(colorFaint, "─"))
			case hasService:
				track.WriteString(paint(color, "━"))
			default:
				track.WriteString(paint(colorDim, "┄"))
			}
		}
	}

	// The station the train stands at, or runs to, is bright.
	var names strings.Builder
	for index := firstIndex; index < firstIndex+stationCount; index++ {
		name := truncateText(stations[index].ShortName, columnWidth-1)
		leftPad := (columnWidth - len([]rune(name))) / 2
		rightPad := columnWidth - len([]rune(name)) - leftPad
		names.WriteString(paint(colorText, strings.Repeat(" ", leftPad)))
		if index == phase.ToIndex {
			names.WriteString(paintBold(colorBright, name))
		} else {
			names.WriteString(paint(colorDim, name))
		}
		names.WriteString(paint(colorText, strings.Repeat(" ", rightPad)))
	}
	return []string{track.String(), names.String()}
}

// viewCondition shows the network condition the proxy applies, where the train is, and how long
// until the next stop or run.
func (m Model) viewCondition(width int) []string {
	phase, _, offset := m.ride.Position()
	condition := phase.ConditionAt(offset)

	var color lipgloss.Color
	var label, delay string
	switch {
	case !condition.IsConnected:
		color, label, delay = colorNoService, "NO SERVICE", "connections wait"
	case condition.IsWeak():
		color, label = colorWeak, "WEAK SIGNAL"
	default:
		color, label = colorConnected, "CONNECTED"
	}
	if condition.IsConnected {
		delay = fmt.Sprintf("+%d ms each way", condition.Latency.Milliseconds())
	}
	bars := []rune("▂▄▆█")
	lit := signalBars(condition)
	status := paint(color, string(bars[:lit])) + paint(colorFaint, string(bars[lit:])) +
		paint(colorText, "  ") + paintBold(color, label) + paint(colorText, "  ") + paint(colorDim, delay)

	// Time left in this phase, in wall-clock seconds at the current speed.
	timeLeft := time.Duration(float64(phase.Duration-offset) / m.ride.Speed()).Round(time.Second)
	var timing string
	switch {
	case m.ride.IsPaused():
		timing = "held"
	case phase.Kind == sim.AtStation:
		timing = fmt.Sprintf("departs in %ds", int(timeLeft.Seconds()))
	default:
		timing = fmt.Sprintf("arrives in %ds", int(timeLeft.Seconds()))
	}

	place := "At " + m.line.Stations[phase.FromIndex].ShortName
	if phase.Kind == sim.Running {
		place = m.line.Stations[phase.FromIndex].ShortName + " → " + m.line.Stations[phase.ToIndex].ShortName
	}
	// A long station name gives way to the source, which says where the numbers come from.
	place = truncateText(place, width-len([]rune(phase.Source))-2)
	return []string{spread(status, paint(colorText, timing), width), spread(paint(colorText, place), paint(colorDim, phase.Source), width)}
}

// signalBars is the number of bars a phone would show: none without service, one for a weak
// signal, more for a shorter delay. The delay limits are a display choice, not a measurement.
func signalBars(condition sim.Condition) int {
	switch {
	case !condition.IsConnected:
		return 0
	case condition.IsWeak():
		return 1
	case condition.Latency > 150*time.Millisecond:
		return 2
	case condition.Latency > 60*time.Millisecond:
		return 3
	default:
		return 4
	}
}

// viewLog lists the newest connections through the proxy, so it is clear whether the app uses it
// and which connections a tunnel holds up.
func (m Model) viewLog(width, rowCount int) []string {
	connections := m.proxyServer.RecentConnections()
	openCount, waitingCount := 0, 0
	for _, connection := range connections {
		if connection.IsConnected && !connection.IsClosed {
			openCount++
		}
		if connection.IsWaiting {
			waitingCount++
		}
	}
	counts := paint(colorDim, fmt.Sprintf("%d open", openCount))
	if waitingCount > 0 {
		counts += paint(colorDim, " · ") + paint(colorNoService, fmt.Sprintf("%d waiting for service", waitingCount))
	}
	rows := []string{spread(paintBold(colorDim, "CONNECTIONS"), counts, width)}

	if len(connections) == 0 {
		hint := fmt.Sprintf("None yet. Try: curl -x http://localhost:%d https://example.com", m.proxyServer.Port())
		rows = append(rows, paint(colorText, hint))
	}
	for _, connection := range connections[:min(rowCount, len(connections))] {
		rows = append(rows, logRow(connection, width))
	}
	// Empty rows keep the panel's height, so it does not move as connections arrive.
	for len(rows) < rowCount+1 {
		rows = append(rows, "")
	}
	return rows
}

// logRow is one connection: when it opened, where to, bytes received, and its state.
func logRow(connection proxy.Connection, width int) string {
	var state string
	switch {
	case connection.IsWaiting:
		state = paint(colorNoService, fmt.Sprintf("waiting %ds", int(connection.Stalled.Seconds())))
	case connection.HasFailed:
		state = paint(colorNoService, "failed")
	case connection.IsClosed:
		state = paint(colorDim, "closed")
	default:
		state = paint(colorConnected, "open")
	}
	if !connection.IsWaiting && connection.Stalled >= time.Second {
		state += paint(colorDim, fmt.Sprintf(" · stalled %ds", int(connection.Stalled.Seconds())))
	}
	const stateWidth = 21 // "closed · stalled 120s"
	state += paint(colorText, strings.Repeat(" ", max(0, stateWidth-lipgloss.Width(state))))

	if width < narrowLogWidth {
		destination := truncateText(connection.Destination, width-stateWidth-1)
		return spread(paint(colorText, destination), state, width)
	}
	opened := paint(colorDim, connection.OpenedAt.Format("15:04:05")+"  ")
	received := paint(colorDim, fmt.Sprintf("%10s  ", "↓ "+formatBytes(connection.BytesReceived)))
	destinationWidth := width - lipgloss.Width(opened) - lipgloss.Width(received) - stateWidth - 1
	destination := paint(colorText, truncateText(connection.Destination, destinationWidth))
	return spread(opened+destination, received+state, width)
}

// viewRideList lists the rides, a window of them around the cursor, in rowCount rows.
func (m Model) viewRideList(width, rowCount int) []string {
	hint := paintBold(colorBright, "↑↓") + paint(colorDim, " choose  ") + paintBold(colorBright, "enter") +
		paint(colorDim, " ride  ") + paintBold(colorBright, "esc") + paint(colorDim, " back")
	rows := []string{spread(paintBold(colorDim, "RIDES"), hint, width)}

	visibleCount := max(1, rowCount-1)
	first := min(max(0, m.cursor-visibleCount/2), max(0, len(m.choices)-visibleCount))
	for index := first; index < min(len(m.choices), first+visibleCount); index++ {
		ride := m.choices[index]
		marker, nameColor := paint(colorText, "  "), colorText
		if index == m.cursor {
			marker, nameColor = paintBold(colorBright, "▸ "), colorBright
		}
		minutes := int(ride.Duration().Round(time.Minute).Minutes())
		left := marker + badge(ride.LineID) + paint(colorText, " ") + paint(nameColor, ride.Route(lines.Get(ride.LineID)))
		right := paint(colorDim, fmt.Sprintf("%d min · %s", minutes, ride.Source()))
		rows = append(rows, spread(left, right, width))
	}
	// Empty rows keep the panel's height while the list is open.
	for len(rows) < rowCount {
		rows = append(rows, "")
	}
	return rows
}

// formatBytes shows a byte count in B, KB, or MB.
func formatBytes(byteCount int64) string {
	switch {
	case byteCount < 1000:
		return fmt.Sprintf("%d B", byteCount)
	case byteCount < 1_000_000:
		return fmt.Sprintf("%.1f KB", float64(byteCount)/1000)
	default:
		return fmt.Sprintf("%.1f MB", float64(byteCount)/1_000_000)
	}
}

// viewFooter shows the proxy address, the mode, and the keys.
func (m Model) viewFooter(width int) []string {
	address := paint(colorDim, "proxy ") + paint(colorText, fmt.Sprintf("http://localhost:%d", m.proxyServer.Port()))

	var modes []string
	if m.ride.IsPaused() {
		modes = append(modes, paintBold(colorWeak, "PAUSED"))
	}
	modes = append(modes, paint(colorText, fmt.Sprintf("%.1f×", m.ride.Speed())))
	mode := strings.Join(modes, paint(colorDim, " · "))

	var keys []string
	for _, key := range [][2]string{{"tab", "rides"}, {"spc", "pause"}, {"n", "next"}, {"←→", "speed"}, {"r", "1×"}, {"q", "quit"}} {
		keys = append(keys, paintBold(colorBright, key[0])+paint(colorDim, " "+key[1]))
	}
	help := strings.Join(keys, paint(colorText, "  "))
	return []string{spread(address, mode, width), center(help, width)}
}

// spread puts left at the start of the line and right at the end. When both do not fit, the
// end of the line is cut.
func spread(left, right string, width int) string {
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	return fitWidth(left+paint(colorText, strings.Repeat(" ", gap))+right, width)
}

// center puts text in the middle of the line.
func center(text string, width int) string {
	leftPad := max(0, (width-lipgloss.Width(text))/2)
	return fitWidth(paint(colorText, strings.Repeat(" ", leftPad))+text, width)
}

// fitWidth cuts a styled line to width columns.
func fitWidth(line string, width int) string {
	return lipgloss.NewStyle().MaxWidth(width).Render(line)
}

// truncateText shortens plain text to width columns, ending with "…" when it cuts.
func truncateText(text string, width int) string {
	characters := []rune(text)
	switch {
	case width <= 0:
		return ""
	case len(characters) <= width:
		return text
	default:
		return string(characters[:width-1]) + "…"
	}
}
