package lines

import "time"

// LTrain defines the L train (Canarsie line)
// The L runs from 8th Avenue in Manhattan to Canarsie-Rockaway Pkwy in Brooklyn
// Famous for the long East River tunnel between 1st Ave and Bedford Ave
var LTrain = Line{
	ID:    "L",
	Name:  "Canarsie",
	Color: "#A7A9AC",
	Stations: []Station{
		// Manhattan (West to East)
		{ID: "8av", Name: "8th Avenue", ShortName: "8th Ave", HasService: true, DwellTime: 30 * time.Second},
		{ID: "6av", Name: "6th Avenue", ShortName: "6th Ave", HasService: true, DwellTime: 25 * time.Second},
		{ID: "uni", Name: "14th St-Union Sq", ShortName: "Union Sq", HasService: true, DwellTime: 35 * time.Second},
		{ID: "3av", Name: "3rd Avenue", ShortName: "3rd Ave", HasService: true, DwellTime: 20 * time.Second},
		{ID: "1av", Name: "1st Avenue", ShortName: "1st Ave", HasService: true, DwellTime: 25 * time.Second},

		// Brooklyn (West to East, after East River tunnel)
		{ID: "bed", Name: "Bedford Avenue", ShortName: "Bedford", HasService: true, DwellTime: 30 * time.Second},
		{ID: "lor", Name: "Lorimer Street", ShortName: "Lorimer", HasService: true, DwellTime: 20 * time.Second},
		{ID: "gra", Name: "Graham Avenue", ShortName: "Graham", HasService: false, DwellTime: 20 * time.Second},
		{ID: "grd", Name: "Grand Street", ShortName: "Grand", HasService: false, DwellTime: 20 * time.Second},
		{ID: "mon", Name: "Montrose Avenue", ShortName: "Montrose", HasService: false, DwellTime: 20 * time.Second},
		{ID: "mor", Name: "Morgan Avenue", ShortName: "Morgan", HasService: false, DwellTime: 20 * time.Second},
		{ID: "jef", Name: "Jefferson Street", ShortName: "Jefferson", HasService: false, DwellTime: 20 * time.Second},
		{ID: "dek", Name: "DeKalb Avenue", ShortName: "DeKalb", HasService: false, DwellTime: 20 * time.Second},
		{ID: "myr", Name: "Myrtle-Wyckoff", ShortName: "Myrtle", HasService: true, DwellTime: 25 * time.Second},
		{ID: "hal", Name: "Halsey Street", ShortName: "Halsey", HasService: false, DwellTime: 20 * time.Second},
		{ID: "wil", Name: "Wilson Avenue", ShortName: "Wilson", HasService: false, DwellTime: 20 * time.Second},
		{ID: "bus", Name: "Bushwick-Aberdeen", ShortName: "Bushwick", HasService: false, DwellTime: 20 * time.Second},
		{ID: "bwy", Name: "Broadway Junction", ShortName: "Broadway", HasService: true, DwellTime: 30 * time.Second},
		{ID: "atl", Name: "Atlantic Avenue", ShortName: "Atlantic", HasService: false, DwellTime: 20 * time.Second},
		{ID: "sut", Name: "Sutter Avenue", ShortName: "Sutter", HasService: false, DwellTime: 20 * time.Second},
		{ID: "liv", Name: "Livonia Avenue", ShortName: "Livonia", HasService: false, DwellTime: 20 * time.Second},
		{ID: "nlt", Name: "New Lots Avenue", ShortName: "New Lots", HasService: false, DwellTime: 20 * time.Second},
		{ID: "e105", Name: "East 105th Street", ShortName: "E 105 St", HasService: false, DwellTime: 20 * time.Second},
		{ID: "can", Name: "Canarsie-Rockaway", ShortName: "Canarsie", HasService: true, DwellTime: 45 * time.Second},
	},
	Segments: []Segment{
		// Manhattan segments - underground but with decent service
		{FromID: "8av", ToID: "6av", IsTunnel: false, Duration: 90 * time.Second, Degradation: 0.8},
		{FromID: "6av", ToID: "uni", IsTunnel: false, Duration: 60 * time.Second, Degradation: 0.9},
		{FromID: "uni", ToID: "3av", IsTunnel: false, Duration: 75 * time.Second, Degradation: 0.7},
		{FromID: "3av", ToID: "1av", IsTunnel: false, Duration: 60 * time.Second, Degradation: 0.8},

		// THE FAMOUS EAST RIVER TUNNEL - complete blackout for ~3 minutes
		{FromID: "1av", ToID: "bed", IsTunnel: true, Duration: 180 * time.Second, Degradation: 0.0},

		// Brooklyn segments - mixed service
		{FromID: "bed", ToID: "lor", IsTunnel: false, Duration: 60 * time.Second, Degradation: 0.6},
		{FromID: "lor", ToID: "gra", IsTunnel: false, Duration: 60 * time.Second, Degradation: 0.4},
		{FromID: "gra", ToID: "grd", IsTunnel: false, Duration: 60 * time.Second, Degradation: 0.3},
		{FromID: "grd", ToID: "mon", IsTunnel: false, Duration: 60 * time.Second, Degradation: 0.3},
		{FromID: "mon", ToID: "mor", IsTunnel: false, Duration: 60 * time.Second, Degradation: 0.4},
		{FromID: "mor", ToID: "jef", IsTunnel: false, Duration: 60 * time.Second, Degradation: 0.4},
		{FromID: "jef", ToID: "dek", IsTunnel: false, Duration: 60 * time.Second, Degradation: 0.5},
		{FromID: "dek", ToID: "myr", IsTunnel: false, Duration: 75 * time.Second, Degradation: 0.6},
		{FromID: "myr", ToID: "hal", IsTunnel: false, Duration: 60 * time.Second, Degradation: 0.4},
		{FromID: "hal", ToID: "wil", IsTunnel: false, Duration: 60 * time.Second, Degradation: 0.4},
		{FromID: "wil", ToID: "bus", IsTunnel: false, Duration: 60 * time.Second, Degradation: 0.5},
		{FromID: "bus", ToID: "bwy", IsTunnel: false, Duration: 75 * time.Second, Degradation: 0.7},
		{FromID: "bwy", ToID: "atl", IsTunnel: false, Duration: 60 * time.Second, Degradation: 0.5},
		{FromID: "atl", ToID: "sut", IsTunnel: false, Duration: 60 * time.Second, Degradation: 0.4},
		{FromID: "sut", ToID: "liv", IsTunnel: false, Duration: 60 * time.Second, Degradation: 0.4},
		{FromID: "liv", ToID: "nlt", IsTunnel: false, Duration: 60 * time.Second, Degradation: 0.5},
		{FromID: "nlt", ToID: "e105", IsTunnel: false, Duration: 90 * time.Second, Degradation: 0.6},
		{FromID: "e105", ToID: "can", IsTunnel: false, Duration: 90 * time.Second, Degradation: 0.8},
	},
}

// GetLine returns a line by ID
func GetLine(id string) *Line {
	switch id {
	case "L":
		return &LTrain
	default:
		return nil
	}
}

// AvailableLines returns the IDs of all available lines
func AvailableLines() []string {
	return []string{"L"}
}
