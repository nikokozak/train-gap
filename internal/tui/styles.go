package tui

import "github.com/charmbracelet/lipgloss"

// MTA Official Line Colors
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

// UI State Colors
var (
	ColorConnected    = lipgloss.Color("#6CBE45") // Green
	ColorDisconnected = lipgloss.Color("#EE352E") // MTA Red
	ColorDegrading    = lipgloss.Color("#FCCC0A") // MTA Yellow
	ColorRecovering   = lipgloss.Color("#FF6319") // MTA Orange
	ColorMuted        = lipgloss.Color("#6B6B6B") // Dim gray
	ColorBright       = lipgloss.Color("#FFFFFF") // White
	ColorDim          = lipgloss.Color("#4A4A4A") // Dark gray
)

// Box drawing characters
const (
	BoxTopLeft     = "╭"
	BoxTopRight    = "╮"
	BoxBottomLeft  = "╰"
	BoxBottomRight = "╯"
	BoxHorizontal  = "─"
	BoxVertical    = "│"
	BoxLeftT       = "├"
	BoxRightT      = "┤"
	BoxTopT        = "┬"
	BoxBottomT     = "┴"
	BoxCross       = "┼"
)

// Track characters
const (
	TrackLine      = "━"
	TrackStation   = "●"
	TrackEmpty     = "○"
	TrackTunnelIn  = "╲"
	TrackTunnelOut = "╱"
	TrackTunnelMid = "_"
)

// Block elements for progress bars and train
const (
	BlockFull   = "█"
	BlockMedium = "▓"
	BlockLight  = "▒"
	BlockEmpty  = "░"
)

// Train display characters
const (
	TrainBodyFull  = "▓"
	TrainBodyLight = "▒"
	TrainBodyDim   = "░"
)

// Arrows
const (
	ArrowLeft  = "←"
	ArrowRight = "→"
)

// Styles creates all the lipgloss styles for the TUI
type Styles struct {
	// Main container
	App lipgloss.Style

	// Header
	Header      lipgloss.Style
	HeaderTitle lipgloss.Style
	HeaderLine  lipgloss.Style
	LineBadge   lipgloss.Style

	// Track area
	TrackContainer lipgloss.Style
	TrackLine      lipgloss.Style
	StationName    lipgloss.Style
	StationActive  lipgloss.Style
	DirectionHint  lipgloss.Style

	// Train indicator
	TrainConnected    lipgloss.Style
	TrainDisconnected lipgloss.Style
	TrainDegrading    lipgloss.Style

	// Status area
	StatusContainer lipgloss.Style
	StatusLabel     lipgloss.Style
	StatusValue     lipgloss.Style
	StatusDot       lipgloss.Style
	ProgressFull    lipgloss.Style
	ProgressEmpty   lipgloss.Style

	// Help bar
	HelpContainer lipgloss.Style
	HelpKey       lipgloss.Style
	HelpDesc      lipgloss.Style

	// Divider
	Divider lipgloss.Style
}

// NewStyles creates a new Styles instance with the given width
func NewStyles(width int) *Styles {
	s := &Styles{}

	// Main app container with border
	s.App = lipgloss.NewStyle().
		Width(width).
		Padding(0)

	// Header styles
	s.Header = lipgloss.NewStyle().
		Padding(0, 1)

	s.HeaderTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorBright)

	s.HeaderLine = lipgloss.NewStyle().
		Foreground(ColorMuted)

	s.LineBadge = lipgloss.NewStyle().
		Bold(true).
		Padding(0, 1).
		Background(lipgloss.Color("#A7A9AC")).
		Foreground(lipgloss.Color("#000000"))

	// Track styles
	s.TrackContainer = lipgloss.NewStyle().
		Padding(1, 2)

	s.TrackLine = lipgloss.NewStyle().
		Foreground(ColorMuted)

	s.StationName = lipgloss.NewStyle().
		Foreground(ColorMuted)

	s.StationActive = lipgloss.NewStyle().
		Foreground(ColorBright).
		Bold(true)

	s.DirectionHint = lipgloss.NewStyle().
		Foreground(ColorDim).
		Italic(true)

	// Train styles for different states
	s.TrainConnected = lipgloss.NewStyle().
		Foreground(ColorConnected).
		Bold(true)

	s.TrainDisconnected = lipgloss.NewStyle().
		Foreground(ColorDisconnected)

	s.TrainDegrading = lipgloss.NewStyle().
		Foreground(ColorDegrading)

	// Status area
	s.StatusContainer = lipgloss.NewStyle().
		Padding(1, 2)

	s.StatusLabel = lipgloss.NewStyle().
		Foreground(ColorMuted).
		Width(10)

	s.StatusValue = lipgloss.NewStyle().
		Foreground(ColorBright)

	s.StatusDot = lipgloss.NewStyle().
		Bold(true)

	s.ProgressFull = lipgloss.NewStyle().
		Foreground(ColorConnected)

	s.ProgressEmpty = lipgloss.NewStyle().
		Foreground(ColorDim)

	// Help bar
	s.HelpContainer = lipgloss.NewStyle().
		Padding(0, 2).
		Foreground(ColorMuted)

	s.HelpKey = lipgloss.NewStyle().
		Foreground(ColorBright).
		Bold(true)

	s.HelpDesc = lipgloss.NewStyle().
		Foreground(ColorMuted)

	// Divider line
	s.Divider = lipgloss.NewStyle().
		Foreground(ColorDim)

	return s
}

// UpdateLineBadge updates the line badge style for a specific line
func (s *Styles) UpdateLineBadge(lineID string) {
	if color, ok := LineColors[lineID]; ok {
		s.LineBadge = s.LineBadge.Background(color)
		// Use black text for light backgrounds, white for dark
		switch lineID {
		case "N", "Q", "R", "W": // Yellow lines need black text
			s.LineBadge = s.LineBadge.Foreground(lipgloss.Color("#000000"))
		default:
			s.LineBadge = s.LineBadge.Foreground(lipgloss.Color("#FFFFFF"))
		}
	}
}
