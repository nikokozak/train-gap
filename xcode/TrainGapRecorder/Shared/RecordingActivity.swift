import ActivityKit
import Foundation

// Shared by the app (which starts and updates the Live Activity) and the widget extension
// (which draws it on the lock screen and in the Dynamic Island).

/// What a mark button records. The raw value is the `kind` in the `mark` event.
enum MarkKind: String, Codable, Hashable {
    /// The doors opened at a station.
    case station
    /// Something unusual, for example the train stopped between stations.
    case anomaly

    var buttonTitle: String {
        switch self {
        case .station: "Station"
        case .anomaly: "Anomaly"
        }
    }

    var systemImage: String {
        switch self {
        case .station: "flag.fill"
        case .anomaly: "exclamationmark.triangle.fill"
        }
    }
}

/// The Live Activity that shows a recording on the lock screen and in the Dynamic Island.
struct RecordingActivityAttributes: ActivityAttributes {
    /// Changes during the ride. The app sends an update only when it changes, not every second:
    /// iOS limits how often a Live Activity may update.
    struct ContentState: Codable, Hashable {
        /// The last probe got through.
        var hasService: Bool
        var stationMarkCount: Int
        var anomalyMarkCount: Int
        var lastMarkKind: MarkKind?
        /// Seconds into the ride of the last mark, for the "Station marked · 4:12" confirmation.
        var lastMarkElapsedSeconds: Int?
    }

    var lineID: String
    /// "#RRGGBB" from lines.json.
    var lineColorHex: String?
    /// "To New Lots Av".
    var directionLabel: String
    /// The timer on the lock screen counts from here, without updates from the app.
    var startedAt: Date
}

/// "4:12" for 252 seconds.
func formatElapsed(seconds: Int) -> String {
    String(format: "%d:%02d", seconds / 60, seconds % 60)
}
