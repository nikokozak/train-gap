package tui

import (
	"strconv"

	"github.com/charmbracelet/lipgloss"
)

// LineColors are the MTA's official line colors.
var LineColors = map[string]lipgloss.Color{
	"L": lipgloss.Color("#A7A9AC"), // Canarsie - Gray
	"A": lipgloss.Color("#0039A6"), // 8th Ave - Blue
	"C": lipgloss.Color("#0039A6"),
	"E": lipgloss.Color("#0039A6"),
	"B": lipgloss.Color("#FF6319"), // 6th Ave - Orange
	"D": lipgloss.Color("#FF6319"),
	"F": lipgloss.Color("#FF6319"),
	"M": lipgloss.Color("#FF6319"),
	"G": lipgloss.Color("#6CBE45"), // Crosstown - Lime
	"J": lipgloss.Color("#996633"), // Nassau - Brown
	"Z": lipgloss.Color("#996633"),
	"N": lipgloss.Color("#FCCC0A"), // Broadway - Yellow
	"Q": lipgloss.Color("#FCCC0A"),
	"R": lipgloss.Color("#FCCC0A"),
	"W": lipgloss.Color("#FCCC0A"),
	"1": lipgloss.Color("#EE352E"), // 7th Ave - Red
	"2": lipgloss.Color("#EE352E"),
	"3": lipgloss.Color("#EE352E"),
	"4": lipgloss.Color("#00933C"), // Lexington - Green
	"5": lipgloss.Color("#00933C"),
	"6": lipgloss.Color("#00933C"),
	"7": lipgloss.Color("#B933AD"), // Flushing - Purple
	"S": lipgloss.Color("#808183"), // Shuttle - Gray
}

// The panel colors. Text colors have a contrast of at least 4.5:1 on the panel; colorFaint is
// for decoration only (unlit signal bars).
var (
	colorPanel     = lipgloss.Color("#16161e")
	colorBright    = lipgloss.Color("#eeeef4") // 15.6:1
	colorText      = lipgloss.Color("#b9bdcc") // 9.6:1
	colorDim       = lipgloss.Color("#8a8fa6") // 5.6:1
	colorFaint     = lipgloss.Color("#3d4152") // 1.8:1
	colorConnected = lipgloss.Color("#6CBE45") // 7.8:1
	colorWeak      = lipgloss.Color("#FCCC0A") // 11.8:1
	colorNoService = lipgloss.Color("#ff5c55") // 5.9:1; the MTA red (#EE352E) is 4.4:1.
)

// lineColor returns the line's color, or gray for a line without one.
func lineColor(lineID string) lipgloss.Color {
	if color, ok := LineColors[lineID]; ok {
		return color
	}
	return lipgloss.Color("#808183")
}

// badgeTextColor picks black or white, whichever reads better on the line color. The MTA prints
// white on most bullets, but white on the L's gray is 2.4:1 and unreadable in a terminal.
func badgeTextColor(background lipgloss.Color) lipgloss.Color {
	hex := string(background)
	if len(hex) != 7 {
		return lipgloss.Color("#ffffff")
	}
	red, _ := strconv.ParseUint(hex[1:3], 16, 8) // The colors above are valid hex; a bad one reads as 0.
	green, _ := strconv.ParseUint(hex[3:5], 16, 8)
	blue, _ := strconv.ParseUint(hex[5:7], 16, 8)
	// Perceived brightness (ITU-R BT.601). Above the middle, black text has more contrast.
	brightness := (299*red + 587*green + 114*blue) / 1000
	if brightness > 128 {
		return lipgloss.Color("#000000")
	}
	return lipgloss.Color("#ffffff")
}

// paint colors text on the panel. Every piece of the view goes through paint or paintBold: each
// styled piece ends with a reset, and a piece without the panel background would leave a hole.
func paint(color lipgloss.Color, text string) string {
	return lipgloss.NewStyle().Foreground(color).Background(colorPanel).Render(text)
}

func paintBold(color lipgloss.Color, text string) string {
	return lipgloss.NewStyle().Foreground(color).Background(colorPanel).Bold(true).Render(text)
}

// badge draws a line bullet: the line ID on the line color.
func badge(lineID string) string {
	color := lineColor(lineID)
	return lipgloss.NewStyle().Bold(true).Foreground(badgeTextColor(color)).Background(color).Render(" " + lineID + " ")
}
