import SwiftUI

/// Chooses the route and carrier, then starts recording. Choices are remembered between rides,
/// so on a repeat commute the rider only taps Start.
struct RecordSetupView: View {
    @EnvironmentObject private var linesStore: LinesStore
    @EnvironmentObject private var recorder: Recorder
    @EnvironmentObject private var sessionsStore: SessionsStore

    @AppStorage("setup.lineID") private var lineID = "L"
    @AppStorage("setup.direction") private var direction = Direction.forward
    @AppStorage("setup.startStationID") private var startStationID = ""
    @AppStorage("setup.carrier") private var carrier = ""

    @State private var isStarting = false
    @State private var isShowingRide = false

    /// iOS no longer reports the carrier name, so the rider chooses it.
    private static let carriers = ["AT&T", "Verizon", "T-Mobile", "Other"]

    var body: some View {
        Form {
            if let loadError = linesStore.loadError {
                Section("Error") {
                    Text(loadError).foregroundStyle(.red)
                }
            }

            if let line = selectedLine {
                Section("Route") {
                    Picker("Line", selection: $lineID) {
                        ForEach(linesStore.lines) { line in
                            Text("\(line.id) — \(line.name)").tag(line.id)
                        }
                    }
                    Picker("Direction", selection: $direction) {
                        Text(line.directionLabel(for: .forward)).tag(Direction.forward)
                        Text(line.directionLabel(for: .backward)).tag(Direction.backward)
                    }
                    Picker("Starting station", selection: $startStationID) {
                        ForEach(startStationChoices) { station in
                            Text(station.shortName).tag(station.id)
                        }
                    }
                }
            }

            Section {
                Picker("Carrier", selection: $carrier) {
                    Text("Choose").tag("")
                    ForEach(Self.carriers, id: \.self) { carrierName in
                        Text(carrierName).tag(carrierName)
                    }
                }
            } footer: {
                Text("Coverage differs by carrier.")
            }

            Section {
                Button {
                    Task { await startRecording() }
                } label: {
                    Text("Start recording")
                        .bold()
                        .frame(maxWidth: .infinity)
                }
                .disabled(!canStart)
            } footer: {
                Text("Tap Start on the platform. Then lock the phone and put it away. You choose where you got off when you stop.")
            }

            if !recorder.isRecording, let errorMessage = recorder.errorMessage {
                Section {
                    Text(errorMessage).foregroundStyle(.red)
                }
            }
        }
        .navigationTitle("Train-Gap Recorder")
        .navigationDestination(isPresented: $isShowingRide) {
            RideRecordingView(onFinished: { sessionsStore.refresh() })
        }
        .onChange(of: lineID) { keepStartStationValid() }
        .onChange(of: direction) { keepStartStationValid() }
        .onAppear {
            if linesStore.lines.isEmpty {
                linesStore.loadBundledLinesJSON()
            }
            keepStartStationValid()
            recorder.requestLocationAccess()
        }
    }

    private var selectedLine: Line? {
        linesStore.lines.first { $0.id == lineID }
    }

    /// Every station except the terminal in the chosen direction: a ride needs a next station.
    private var startStationChoices: [Station] {
        guard let line = selectedLine, let terminal = line.terminalStation(for: direction) else { return [] }
        return line.stations.filter { $0.id != terminal.id }
    }

    private var canStart: Bool {
        !isStarting
            && !recorder.isRecording
            && !carrier.isEmpty
            && startStationChoices.contains { $0.id == startStationID }
    }

    /// Repairs remembered choices that no longer fit, e.g. after a line change or a new dataset.
    private func keepStartStationValid() {
        if selectedLine == nil, let firstLine = linesStore.lines.first {
            lineID = firstLine.id
        }
        if !startStationChoices.contains(where: { $0.id == startStationID }) {
            startStationID = startStationChoices.first?.id ?? ""
        }
    }

    private func startRecording() async {
        guard
            let line = selectedLine,
            let terminal = line.terminalStation(for: direction),
            let startStationIndex = line.stations.firstIndex(where: { $0.id == startStationID })
        else { return }
        let startStation = line.stations[startStationIndex]

        let config = SessionConfig(
            lineID: line.id,
            lineName: line.name,
            direction: direction,
            directionLabel: line.directionLabel(for: direction),
            directionTerminalStationID: terminal.id,
            directionTerminalStationName: terminal.shortName,
            startStationID: startStation.id,
            startStationName: startStation.shortName,
            startStationIndex: startStationIndex,
            carrier: carrier
        )

        isStarting = true
        if await recorder.start(line: line, config: config) {
            isShowingRide = true
        }
        isStarting = false
    }
}
