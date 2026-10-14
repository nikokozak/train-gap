import SwiftUI

/// The screen during a ride. The rider only needs to see that recording is healthy and where the
/// mark buttons are; stops and station labels are worked out after the ride. The same buttons
/// are on the lock screen (RecordingLiveActivity).
struct RideRecordingView: View {
    @Environment(\.dismiss) private var dismiss
    @EnvironmentObject private var recorder: Recorder

    var onFinished: (() -> Void)?

    @State private var isShowingFinishSheet = false
    /// The mark the confirmation pill shows. It hides itself after `confirmationSeconds`.
    @State private var confirmedMark: MarkConfirmation?

    /// Long enough to read with a glance down on a moving train.
    private static let confirmationSeconds = 3.0

    var body: some View {
        ScrollView {
            VStack(spacing: 20) {
                routeHeader

                // Redraws every second so the elapsed time and the sample age stay current.
                TimelineView(.periodic(from: .now, by: 1)) { context in
                    statusCard(now: context.date)
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
        .overlay(alignment: .top) {
            if let confirmedMark {
                MarkConfirmationPill(mark: confirmedMark)
                    .padding(.top, 8)
                    .transition(.move(edge: .top).combined(with: .opacity))
            }
        }
        // Marks from the lock screen arrive here too, so the pill shows whichever button was used.
        .onChange(of: recorder.lastMark) { _, mark in
            withAnimation(.snappy) { confirmedMark = mark }
            if let mark {
                AccessibilityNotification.Announcement("\(mark.kind.buttonTitle) marked").post()
            }
        }
        .task(id: confirmedMark?.id) {
            guard confirmedMark != nil else { return }
            try? await Task.sleep(for: .seconds(Self.confirmationSeconds))  // Throws only when a newer mark replaces this one.
            withAnimation(.snappy) { confirmedMark = nil }
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

    /// Recording state, elapsed time, the signal now, the last minute of service, and the marks.
    private func statusCard(now: Date) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .firstTextBaseline) {
                Label(recorder.isRecording ? "Recording" : "Stopped", systemImage: "record.circle")
                    .font(.headline)
                    .foregroundStyle(recorder.isRecording ? .red : .secondary)
                Spacer()
                Text(elapsedText(now: now))
                    .font(.system(.largeTitle, design: .rounded).monospacedDigit().bold())
            }

            HStack(alignment: .center, spacing: 10) {
                SignalBars(bars: signalBars, color: serviceColor)
                Text(serviceText)
                    .font(.title3.weight(.semibold))
                    .foregroundStyle(serviceColor)
                Spacer()
                Text(sampleAgeText)
                    .font(.footnote)
                    .foregroundStyle(isSampleStale(now: now) ? .red : .secondary)
            }

            VStack(alignment: .leading, spacing: 4) {
                ServiceStrip(history: recorder.serviceHistory)
                Text("Service in the last minute. Red is a second without service.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            HStack(spacing: 20) {
                Label("\(recorder.stationMarkCount)", systemImage: MarkKind.station.systemImage)
                    .accessibilityLabel("\(recorder.stationMarkCount) station marks")
                Label("\(recorder.anomalyMarkCount)", systemImage: MarkKind.anomaly.systemImage)
                    .accessibilityLabel("\(recorder.anomalyMarkCount) anomaly marks")
                Spacer()
            }
            .font(.callout.monospacedDigit())
            .foregroundStyle(.secondary)
        }
        .padding()
        .background(.background.secondary, in: RoundedRectangle(cornerRadius: 16))
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
            DetailRow(name: "Carrier", value: recorder.config?.carrier ?? "—")
            if let sample = recorder.lastSample {
                DetailRow(name: "Path", value: "\(sample.path.status) \(sample.path.interface ?? "") \(sample.path.radio ?? "")")
                DetailRow(name: "Last probe", value: probeText(sample.probe))
            }
            if let udpProbe = recorder.lastUDPProbe {
                DetailRow(name: "UDP", value: udpText(udpProbe))
            }
        }
        .font(.callout)
    }

    private var controls: some View {
        VStack(spacing: 10) {
            HStack(spacing: 12) {
                markButton(.station)
                markButton(.anomaly)
            }

            Text("Optional. Station when the doors open; Anomaly when the train stops between stations or something else is odd. Both are on the lock screen too.")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)

            Button(role: .destructive) {
                recorder.noteStopTapped()
                isShowingFinishSheet = true
            } label: {
                Label("Stop", systemImage: "stop.fill")
                    .font(.title3)
                    .frame(maxWidth: .infinity, minHeight: 48)
            }
            .buttonStyle(.borderedProminent)
            .tint(.red)
        }
        .disabled(!recorder.isRecording)
    }

    private func markButton(_ kind: MarkKind) -> some View {
        Button {
            Task { await recorder.mark(kind) }
        } label: {
            Label(kind.buttonTitle, systemImage: kind.systemImage)
                .font(.title3)
                .frame(maxWidth: .infinity, minHeight: 56)
        }
        .buttonStyle(.bordered)
        .tint(kind == .station ? .accentColor : .orange)
    }

    // MARK: - Signal

    /// Bars from the last probe's round trip. The limits are a display choice, the same as in the
    /// simulator's terminal view (half of each is its per-direction delay limit).
    private var signalBars: Int {
        guard let probe = recorder.lastSample?.probe, probe.ok else { return 0 }
        let roundTripMS = probe.rttMS ?? 0
        switch roundTripMS {
        case 1000...: return 1
        case 300...: return 2
        case 120...: return 3
        default: return 4
        }
    }

    private var serviceColor: Color {
        guard let probe = recorder.lastSample?.probe else { return .secondary }
        return probe.ok ? (signalBars <= 1 ? .orange : .green) : .red
    }

    private var serviceText: String {
        guard let probe = recorder.lastSample?.probe else { return "Waiting…" }
        guard probe.ok else { return "No service" }
        return "Service · \(probe.rttMS ?? 0) ms"
    }

    // MARK: - Text

    private func elapsedText(now: Date) -> String {
        guard let startedAt = recorder.startedAt else { return "0:00" }
        return formatElapsed(seconds: max(0, Int(now.timeIntervalSince(startedAt))))
    }

    private var sampleAge: TimeInterval? {
        recorder.lastSavedSampleTime.map { Recorder.clockSeconds() - $0 }
    }

    private var sampleAgeText: String {
        guard let sampleAge else { return "No sample yet" }
        return "Saved \(Int(sampleAge)) s ago"
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

    /// "4 of 5 replies · slowest 38 ms".
    private func udpText(_ udpProbe: RecorderUDPProbe) -> String {
        let roundTripsMS = udpProbe.rttMS.compactMap { $0 }
        let slowest = roundTripsMS.max().map { " · slowest \($0) ms" } ?? ""
        return "\(roundTripsMS.count) of \(udpProbe.rttMS.count) replies\(slowest)"
    }

    private func probeText(_ probe: ProbeSnapshot) -> String {
        let host = probe.url.flatMap { URL(string: $0)?.host } ?? ""
        let status = probe.httpStatus.map(String.init) ?? "—"
        let roundTrip = probe.rttMS.map { "\($0) ms" } ?? "—"
        let newConnection = probe.reusedConnection == false ? " new conn" : ""
        return "\(host) \(status) \(roundTrip)\(newConnection)"
    }
}

/// "Station marked · 4:12": confirms that a mark reached the file.
private struct MarkConfirmationPill: View {
    var mark: MarkConfirmation

    var body: some View {
        Label("\(mark.kind.buttonTitle) marked · \(formatElapsed(seconds: mark.elapsedSeconds))", systemImage: mark.kind.systemImage)
            .font(.headline)
            .padding(.horizontal, 18)
            .padding(.vertical, 10)
            .background(.regularMaterial, in: Capsule())
            .shadow(radius: 6, y: 2)
    }
}

/// One cell per recent probe, newest at the right: green when it got through, red when not.
private struct ServiceStrip: View {
    var history: [Bool]

    var body: some View {
        let emptyCellCount = Recorder.serviceHistoryLength - history.count
        HStack(spacing: 1) {
            ForEach(0..<Recorder.serviceHistoryLength, id: \.self) { index in
                let historyIndex = index - emptyCellCount
                RoundedRectangle(cornerRadius: 1)
                    .fill(historyIndex < 0 ? Color.secondary.opacity(0.15) : (history[historyIndex] ? Color.green : Color.red))
            }
        }
        .frame(height: 16)
        .accessibilityElement()
        .accessibilityLabel("Service in the last minute: \(history.filter { $0 }.count) of \(history.count) probes got through")
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
