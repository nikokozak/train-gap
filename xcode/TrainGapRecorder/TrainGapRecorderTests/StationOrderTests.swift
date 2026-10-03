import Testing
@testable import TrainGapRecorder

/// The "Where did you get off?" list must follow ride order, nearest station first, in both directions.
struct StationOrderTests {
    private let line = Line(
        id: "X",
        name: "Test line",
        color: "#000000",
        stations: ["A", "B", "C", "D"].map { Station(id: $0, name: $0, shortName: $0) }
    )

    @Test func forwardListsLaterStationsNearestFirst() {
        #expect(line.stationIndicesAfter(1, direction: .forward) == [2, 3])
    }

    @Test func backwardListsEarlierStationsNearestFirst() {
        #expect(line.stationIndicesAfter(2, direction: .backward) == [1, 0])
    }

    @Test func terminalHasNoStationsAfterIt() {
        #expect(line.stationIndicesAfter(3, direction: .forward).isEmpty)
        #expect(line.stationIndicesAfter(0, direction: .backward).isEmpty)
    }
}
