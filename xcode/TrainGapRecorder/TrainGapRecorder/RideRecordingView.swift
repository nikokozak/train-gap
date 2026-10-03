import SwiftUI

/// The screen during a ride. Quiet on purpose: the rider only needs to see that recording
/// is healthy. Stops and station labels are worked out after the ride.
struct RideRecordingView: View {
    @Environment(\.dismiss) private var dismiss
    @EnvironmentObject private var recorder: Recorder

    var onFinished: (() -> Void)?

    @State private var isShowingFinishSheet = false

    var body: some View {
        ScrollView {
            VStack(spacing: 24) {
                routeHeader

                // Redraws every second so the elapsed time and the sample age stay current.
                TimelineView(.periodic(from: .now, by: 1)) { context in
                    statusPanel(now: context.date)
                }

                warnings

                DisclosureGroup("Details") {
                    details.padding(.top, 8)
                }
            }
            .padding()
        }
        // The buttons stay in one place, whatever the content above them does.
        .safeAreaInset(edge: .bottom) {
            controls
                .padding()
                .background(.bar)
        }
        // No tab switching during a ride; it also gives the buttons the bottom of the screen.
        .toolbar(.hidden, for: .tabBar)
        .navigationTitle("Ride")
        .navigationBarTitleDisplayMode(.inline)
        .navigationBarBackButtonHidden(recorder.isRecording)
        .sheet(isPresented: $isShowingFinishSheet) {
            FinishRideSheet {
                isShowingFinishSheet = false
                onFinished?()
                dismiss()
            }
        }
    }

    // MARK: - Sections

    private var routeHeader: some View {
        HStack(spacing: 14) {
            LineBadge(lineID: recorder.config?.lineID ?? "?", hexColor: recorder.line?.color)
            VStack(alignment: .leading, spacing: 2) {
                Text(recorder.config?.directionLabel ?? "")
                    .font(.headline)
                Text("From \(recorder.config?.startStationName ?? "")")
                    .foregroundStyle(.secondary)
            }
            Spacer()
        }
    }

    private func statusPanel(now: Date) -> some View {
        VStack(spacing: 8) {
            Label(recorder.isRecording ? "Recording" : "Stopped", systemImage: "circle.fill")
                .font(.largeTitle.bold())
                .foregroundStyle(recorder.isRecording ? .red : .secondary)
            Text(elapsedText(now: now))
                .font(.title2.monospacedDigit())
            Text(sampleAgeText)
                .foregroundStyle(isSampleStale(now: now) ? .red : .secondary)
        }
    }

    private var warnings: some View {
        VStack(spacing: 8) {
            if !recorder.canRecordWhileLocked {
                WarningRow(text: "Location access is off. Keep the app open with the screen on, or iOS stops the recording.")
            }
            if recorder.isUsingWiFi {
                WarningRow(text: "Wi-Fi is on. Turn it off to measure cellular service.")
            }
            if !recorder.isMotionAvailable {
                WarningRow(text: "No motion sensor. Stops cannot be found in this recording.")
            }
            if let errorMessage = recorder.errorMessage {
                WarningRow(text: errorMessage, color: .red)
            }
        }
    }

    private var details: some View {
        VStack(alignment: .leading, spacing: 6) {
            DetailRow(name: "Samples", value: "\(recorder.sampleCount)")
            DetailRow(name: "Probe OK", value: "\(recorder.probeOKCount) (\(Int((recorder.probeOKRate * 100).rounded()))%)")
            DetailRow(name: "Motion", value: "\(recorder.motionReadingsPerSecond) readings/s")
            DetailRow(name: "Marks", value: "\(recorder.markCount)")
            DetailRow(name: "Carrier", value: recorder.config?.carrier ?? "—")
            if let sample = recorder.lastSample {
                DetailRow(name: "Path", value: "\(sample.path.status) \(sample.path.interface ?? "") \(sample.path.radio ?? "")")
                DetailRow(name: "Last probe", value: probeText(sample.probe))
            }
        }
        .font(.callout)
    }

    private var controls: some View {
        VStack(spacing: 12) {
            Button {
                Task { await recorder.mark() }
            } label: {
                Label("Mark", systemImage: "flag")
                    .font(.title2)
                    .frame(maxWidth: .infinity, minHeight: 56)
            }
            .buttonStyle(.bordered)

            Text("Optional. Tap when the doors open.")
                .font(.footnote)
                .foregroundStyle(.secondary)

            Button(role: .destructive) {
                recorder.noteStopTapped()
                isShowingFinishSheet = true
            } label: {
                Label("Stop", systemImage: "stop.fill")
                    .font(.title2)
                    .frame(maxWidth: .infinity, minHeight: 56)
            }
            .buttonStyle(.borderedProminent)
            .tint(.red)
        }
        .disabled(!recorder.isRecording)
    }

    // MARK: - Text

    private func elapsedText(now: Date) -> String {
        guard let startedAt = recorder.startedAt else { return "0:00" }
        let elapsedSeconds = max(0, Int(now.timeIntervalSince(startedAt)))
        return String(format: "%d:%02d", elapsedSeconds / 60, elapsedSeconds % 60)
    }

    private var sampleAge: TimeInterval? {
        recorder.lastSavedSampleTime.map { Recorder.clockSeconds() - $0 }
    }

    private var sampleAgeText: String {
        guard let sampleAge else { return "Waiting for the first sample…" }
        return "Last sample saved \(Int(sampleAge)) s ago"
    }

    private func isSampleStale(now: Date) -> Bool {
        guard recorder.isRecording else { return false }
        if let sampleAge {
            return sampleAge > Recorder.staleSampleAge
        }
        // No sample yet: stale only once the first probe should have finished.
        let elapsed = recorder.startedAt.map { now.timeIntervalSince($0) } ?? 0
        return elapsed > Recorder.staleSampleAge
    }

    private func probeText(_ probe: ProbeSnapshot) -> String {
        let host = probe.url.flatMap { URL(string: $0)?.host } ?? ""
        let status = probe.httpStatus.map(String.init) ?? "—"
        let roundTrip = probe.rttMS.map { "\($0) ms" } ?? "—"
        let newConnection = probe.reusedConnection == false ? " new conn" : ""
        return "\(host) \(status) \(roundTrip)\(newConnection)"
    }
}

/// Asks where the rider got off. This is the second anchor for station labels,
/// so the rider must choose; "Not sure" is a valid answer.
private struct FinishRideSheet: View {
    @Environment(\.dismiss) private var dismiss
    @EnvironmentObject private var recorder: Recorder

    var onSaved: () -> Void

    /// A station index, `notSureChoice`, or nil before the rider chooses.
    @State private var endStationChoice: Int?
    @State private var note = ""
    @State private var isSaving = false

    private static let notSureChoice = -1

    var body: some View {
        NavigationStack {
            Form {
                Section("Where did you get off?") {
                    Picker("Station", selection: $endStationChoice) {
                        // First, so it is reachable without scrolling a long line.
                        Text("Not sure").tag(Optional(Self.notSureChoice))
                        ForEach(endStationIndices, id: \.self) { stationIndex in
                            Text(recorder.line?.stations[stationIndex].shortName ?? "").tag(Optional(stationIndex))
                        }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                }

                Section("Note (optional)") {
                    TextField("Note", text: $note, axis: .vertical)
                        .lineLimit(3, reservesSpace: true)
                }
            }
            .navigationTitle("Finish ride")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Keep recording") {
                        recorder.cancelStop()
                        dismiss()
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        Task { await save() }
                    }
                    .disabled(endStationChoice == nil || isSaving)
                }
            }
        }
        // The rider must choose Save or Keep recording; a swipe would leave the stop half done.
        .interactiveDismissDisabled()
    }

    /// Stations after the start station, in ride order, nearest first.
    private var endStationIndices: [Int] {
        guard let line = recorder.line, let config = recorder.config else { return [] }
        return line.stationIndicesAfter(config.startStationIndex, direction: config.direction)
    }

    private func save() async {
        isSaving = true
        let endStationIndex = endStationChoice == Self.notSureChoice ? nil : endStationChoice
        await recorder.finish(endStationIndex: endStationIndex, note: note.isEmpty ? nil : note)
        onSaved()
    }
}

/// An MTA-style route bullet.
private struct LineBadge: View {
    var lineID: String
    var hexColor: String?

    /// MTA prints black letters on the yellow Broadway-line bullets and white letters on all others.
    private static let blackTextLines: Set<String> = ["N", "Q", "R", "W"]

    var body: some View {
        Text(lineID)
            .font(.title.bold())
            .foregroundStyle(Self.blackTextLines.contains(lineID) ? .black : .white)
            .frame(width: 52, height: 52)
            .background(Circle().fill(Color(hex: hexColor) ?? .gray))
    }
}

private struct WarningRow: View {
    var text: String
    var color: Color = .orange

    var body: some View {
        Label(text, systemImage: "exclamationmark.triangle.fill")
            .font(.callout)
            .foregroundStyle(color)
            .fixedSize(horizontal: false, vertical: true)  // Wrap; never truncate a warning.
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct DetailRow: View {
    var name: String
    var value: String

    var body: some View {
        HStack {
            Text(name)
            Spacer()
            Text(value)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.trailing)
        }
    }
}

private extension Color {
    /// Parses "#RRGGBB" (the format in lines.json). Returns nil for anything else.
    init?(hex: String?) {
        guard let hex, hex.hasPrefix("#"), hex.count == 7, let value = Int(hex.dropFirst(), radix: 16) else { return nil }
        self.init(
            red: Double((value >> 16) & 0xFF) / 255,
            green: Double((value >> 8) & 0xFF) / 255,
            blue: Double(value & 0xFF) / 255
        )
    }
}
