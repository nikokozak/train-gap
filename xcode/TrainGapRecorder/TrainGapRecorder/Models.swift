import Foundation

struct LinesBundle: Codable {
    var generatedAt: Date?
    var lines: [Line]

    enum CodingKeys: String, CodingKey {
        case generatedAt = "generated_at"
        case lines
    }
}

struct Line: Codable, Hashable, Identifiable {
    var id: String
    var name: String
    var color: String
    var directionForwardLabel: String?
    var directionBackwardLabel: String?
    var stations: [Station]

    enum CodingKeys: String, CodingKey {
        case id
        case name
        case color
        case directionForwardLabel = "direction_forward_label"
        case directionBackwardLabel = "direction_backward_label"
        case stations
    }
}

struct Station: Codable, Hashable, Identifiable {
    var id: String
    var name: String
    var shortName: String

    enum CodingKeys: String, CodingKey {
        case id
        case name
        case shortName = "short_name"
    }
}

enum Direction: String, Codable, CaseIterable, Identifiable {
    case forward
    case backward
    case unknown

    var id: String { rawValue }
}

/// What the rider chose before the ride. The start station is the first anchor for
/// station labels; the end station (chosen at Stop) is the second.
struct SessionConfig: Hashable {
    var lineID: String
    var lineName: String
    var direction: Direction
    var directionLabel: String
    var directionTerminalStationID: String
    var directionTerminalStationName: String
    var startStationID: String
    var startStationName: String
    var startStationIndex: Int
    var carrier: String
}

struct SessionInfo: Identifiable, Hashable {
    var id: String
    var startedAt: Date
    var lineID: String
    var startStationID: String
    var startStationName: String
    var directionTerminalStationID: String
    var directionTerminalStationName: String
    var fileURL: URL
}

extension Line {
    func directionLabel(for direction: Direction) -> String {
        switch direction {
        case .forward:
            return directionForwardLabel ?? ("To " + (stations.last?.shortName ?? "?"))
        case .backward:
            return directionBackwardLabel ?? ("To " + (stations.first?.shortName ?? "?"))
        case .unknown:
            return "Unknown direction"
        }
    }

    /// Stations the train reaches after `stationIndex` when it travels in `direction`, in ride order.
    func stationIndicesAfter(_ stationIndex: Int, direction: Direction) -> [Int] {
        switch direction {
        case .forward: return stations.indices.filter { $0 > stationIndex }
        case .backward: return stations.indices.filter { $0 < stationIndex }.reversed()
        case .unknown: return []
        }
    }

    func terminalStation(for direction: Direction) -> Station? {
        switch direction {
        case .forward: return stations.last
        case .backward: return stations.first
        case .unknown: return nil
        }
    }
}
