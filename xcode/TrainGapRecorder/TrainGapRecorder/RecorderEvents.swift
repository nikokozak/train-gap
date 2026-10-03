import Foundation

// NDJSON events. Must match schemas/recording.v1.schema.json.
// Optional properties that are nil are left out of the JSON.

enum RecorderEventType: String, Codable {
    case sessionStart = "session_start"
    case sample = "sample"
    case motion = "motion"
    case mark = "mark"
    case sessionEnd = "session_end"
}

struct RecorderSessionStart: Codable {
    var type: RecorderEventType = .sessionStart
    var v: Int = 1
    var sessionID: String
    var startedAt: String
    var lineID: String
    var lineName: String
    var fromStationID: String
    var fromStationName: String
    var toStationID: String
    var toStationName: String
    var direction: Direction
    var directionLabel: String?
    var directionTerminalStationID: String?
    var directionTerminalStationName: String?
    var probeURL: String?
    var probeURLs: [String]?
    var sampleIntervalMS: Int?
    var carrier: String?
    var motionHz: Int?
    var linesDataset: LinesDatasetInfo?
    var device: DeviceInfo

    enum CodingKeys: String, CodingKey {
        case type
        case v
        case sessionID = "session_id"
        case startedAt = "started_at"
        case lineID = "line_id"
        case lineName = "line_name"
        case fromStationID = "from_station_id"
        case fromStationName = "from_station_name"
        case toStationID = "to_station_id"
        case toStationName = "to_station_name"
        case direction
        case directionLabel = "direction_label"
        case directionTerminalStationID = "direction_terminal_station_id"
        case directionTerminalStationName = "direction_terminal_station_name"
        case probeURL = "probe_url"
        case probeURLs = "probe_urls"
        case sampleIntervalMS = "sample_interval_ms"
        case carrier
        case motionHz = "motion_hz"
        case linesDataset = "lines_dataset"
        case device
    }
}

struct LinesDatasetInfo: Codable {
    var source: String
    var generatedAt: String?
    var sha256: String?

    enum CodingKeys: String, CodingKey {
        case source
        case generatedAt = "generated_at"
        case sha256
    }
}

struct DeviceInfo: Codable {
    var model: String
    var systemName: String
    var systemVersion: String

    enum CodingKeys: String, CodingKey {
        case model
        case systemName = "system_name"
        case systemVersion = "system_version"
    }
}

/// One network probe. `tMS` is when the probe started.
struct RecorderSample: Codable {
    var type: RecorderEventType = .sample
    var tMS: Int
    var path: PathSnapshot
    var probe: ProbeSnapshot

    enum CodingKeys: String, CodingKey {
        case type
        case tMS = "t_ms"
        case path
        case probe
    }
}

struct PathSnapshot: Codable {
    var status: String
    var interface: String?
    var expensive: Bool
    var constrained: Bool
    /// Cellular radio type for the data SIM ("LTE", "NR", "NRNSA", ...). Nil when iOS does not report it.
    var radio: String?
}

struct ProbeSnapshot: Codable {
    /// True when any HTTP response arrived, whatever its status code.
    var ok: Bool
    var url: String?
    var rttMS: Int?
    var httpStatus: Int?
    var error: String?
    /// False means `rttMS` includes DNS, TCP, and TLS setup, e.g. the first probe after a blackout.
    var reusedConnection: Bool?

    enum CodingKeys: String, CodingKey {
        case ok
        case url
        case rttMS = "rtt_ms"
        case httpStatus = "http_status"
        case error
        case reusedConnection = "reused_connection"
    }
}

/// One batch (about one second) of accelerometer readings without gravity, in milli-g, device axes.
/// Readings are evenly spaced at `hz`, starting at `tMS`.
struct RecorderMotion: Codable {
    var type: RecorderEventType = .motion
    var tMS: Int
    var hz: Int
    var xMilliG: [Int]
    var yMilliG: [Int]
    var zMilliG: [Int]
    /// Mean gravity vector over the batch. Shows how the phone is held or carried.
    var gravityMilliG: [Int]

    enum CodingKeys: String, CodingKey {
        case type
        case tMS = "t_ms"
        case hz
        case xMilliG = "x_mg"
        case yMilliG = "y_mg"
        case zMilliG = "z_mg"
        case gravityMilliG = "gravity_mg"
    }
}

/// The rider tapped Mark. A timestamp only; the station label is derived after the ride.
struct RecorderMark: Codable {
    var type: RecorderEventType = .mark
    var tMS: Int

    enum CodingKeys: String, CodingKey {
        case type
        case tMS = "t_ms"
    }
}

struct RecorderSessionSummary: Codable {
    var marks: Int
    var samples: Int
    var probeOK: Int

    enum CodingKeys: String, CodingKey {
        case marks
        case samples
        case probeOK = "probe_ok"
    }
}

struct RecorderSessionEnd: Codable {
    var type: RecorderEventType = .sessionEnd
    /// When the rider tapped Stop.
    var tMS: Int
    var endedAt: String
    var reason: String?
    /// Where the rider got off. All three are nil when the rider chose "Not sure".
    var endStationID: String?
    var endStationName: String?
    var endStationIndex: Int?
    var note: String?
    var summary: RecorderSessionSummary?

    enum CodingKeys: String, CodingKey {
        case type
        case tMS = "t_ms"
        case endedAt = "ended_at"
        case reason
        case endStationID = "end_station_id"
        case endStationName = "end_station_name"
        case endStationIndex = "end_station_index"
        case note
        case summary
    }
}

func iso8601String(_ date: Date) -> String {
    let formatter = ISO8601DateFormatter()
    formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    return formatter.string(from: date)
}
