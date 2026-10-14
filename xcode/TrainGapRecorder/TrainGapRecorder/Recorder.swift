import ActivityKit
import CoreLocation
import CoreMotion
import CoreTelephony
import CryptoKit
import Foundation
import Network
import Synchronization
import UIKit

/// Records one subway ride as an NDJSON file of raw evidence: a network probe every second,
/// the network path, and accelerometer readings. It does not label stations during the ride.
/// `cmd/recording-summarize` finds the stops in the motion trace after the ride and labels them
/// from the start and end stations, so the rider has no tap to forget.
@MainActor
final class Recorder: ObservableObject {
    /// One probe starts every tick, on a fixed clock. A slow probe does not delay the next one.
    static let tickInterval: TimeInterval = 1.0
    /// A probe that gets no response gives up after this time. During a blackout,
    /// about this many probes are in flight at once.
    static let probeTimeout: TimeInterval = 4.0
    /// The UI warns when no sample reached the file for this long. A blackout alone
    /// should not trigger it: failed probes are saved too, after `probeTimeout`.
    static let staleSampleAge: TimeInterval = probeTimeout + 2.0
    /// Accelerometer rate. 20 Hz shows train vibration and braking; about 1.5 MB per 40-minute ride.
    static let motionHz = 20
    /// The service strip on the ride screen shows this many recent probes, one per second.
    static let serviceHistoryLength = 60

    /// Rotated round-robin so that no single endpoint rate-limits us.
    static let probeURLs = [
        URL(string: "https://www.google.com/generate_204")!,
        URL(string: "https://captive.apple.com/hotspot-detect.html")!,
        URL(string: "https://cloudflare.com/cdn-cgi/trace")!,
    ]

    // MARK: - State shown in the UI

    @Published private(set) var isRecording = false
    /// The last ride's route. Kept after the ride ends so the screen does not go blank.
    @Published private(set) var line: Line?
    @Published private(set) var config: SessionConfig?
    @Published private(set) var startedAt: Date?
    @Published private(set) var sampleCount = 0
    @Published private(set) var probeOKCount = 0
    @Published private(set) var stationMarkCount = 0
    @Published private(set) var anomalyMarkCount = 0
    /// The last mark, for the "Station marked" confirmation. A new mark replaces it.
    @Published private(set) var lastMark: MarkConfirmation?
    /// Recent probe results, oldest first: true when the probe got through.
    @Published private(set) var serviceHistory: [Bool] = []
    @Published private(set) var lastSample: RecorderSample?
    /// `clockSeconds()` when the last sample reached the file. The UI shows "saved N s ago" from it.
    @Published private(set) var lastSavedSampleTime: TimeInterval?
    /// Accelerometer readings in the last one-second batch. 0 means motion is not being recorded.
    @Published private(set) var motionReadingsPerSecond = 0
    @Published private(set) var isMotionAvailable = true
    @Published private(set) var isUsingWiFi = false
    /// Location access keeps the app running while the phone is locked.
    @Published private(set) var canRecordWhileLocked = false
    @Published private(set) var errorMessage: String?

    var probeOKRate: Double {
        sampleCount == 0 ? 0 : Double(probeOKCount) / Double(sampleCount)
    }

    // MARK: - Private state

    /// Nil when no session is open. In-flight probes check it before they write.
    private var writer: NDJSONWriter?
    /// All event times (`t_ms`) count from here, on `clockSeconds()`.
    private var sessionStartTime: TimeInterval = 0
    private var stopTappedTime: TimeInterval?
    private var tickLoopTask: Task<Void, Never>?

    private let pathMonitorQueue = DispatchQueue(label: "TrainGapRecorder.NWPathMonitor")
    private var pathMonitor: NWPathMonitor?
    private var currentPath: NWPath?

    private let motionManager = CMMotionManager()
    private var motionBatch = MotionBatch()

    private let locationManager = CLLocationManager()
    private var backgroundActivitySession: CLBackgroundActivitySession?
    private let telephonyInfo = CTTelephonyNetworkInfo()

    private struct ProbeBackoffState {
        var backoffUntilTime: TimeInterval
        var strikes: Int
    }

    private var probeCursor = 0
    private var probeBackoff: [URL: ProbeBackoffState] = [:]

    /// The lock screen view of the ride. Nil when Live Activities are off in Settings.
    private var activity: Activity<RecordingActivityAttributes>?

    // MARK: - Ride controls

    /// Asks for location access once. Call it before the ride, so the prompt does not appear on the platform.
    func requestLocationAccess() {
        if locationManager.authorizationStatus == .notDetermined {
            locationManager.requestWhenInUseAuthorization()
        }
    }

    /// Opens the session file, writes `session_start`, and starts all sensors.
    /// Returns false when the file cannot be created.
    func start(line: Line, config: SessionConfig) async -> Bool {
        // `writer` is set before the first await, so a second tap during start-up returns here.
        guard !isRecording, writer == nil else { return false }
        errorMessage = nil

        let sessionID = UUID().uuidString.lowercased()
        let startDate = Date()
        sessionStartTime = Self.clockSeconds()
        let fileURL = Self.sessionFileURL(sessionID: sessionID, startedAt: startDate, config: config)

        do {
            let newWriter = try NDJSONWriter(fileURL: fileURL)
            writer = newWriter
            try await newWriter.append(makeSessionStartEvent(sessionID: sessionID, startedAt: startDate, line: line, config: config))
        } catch {
            writer = nil
            errorMessage = "Could not create the session file: \(error.localizedDescription)"
            return false
        }

        self.line = line
        self.config = config
        startedAt = startDate
        stopTappedTime = nil
        sampleCount = 0
        probeOKCount = 0
        stationMarkCount = 0
        anomalyMarkCount = 0
        lastMark = nil
        serviceHistory = []
        lastSample = nil
        lastSavedSampleTime = nil
        motionReadingsPerSecond = 0
        probeCursor = 0
        probeBackoff = [:]
        isRecording = true

        startPathMonitor()
        startMotionUpdates()
        startLocationKeepAlive()
        refreshStatus()
        // Without location access, iOS suspends the app when the screen locks. Keep the screen on instead.
        UIApplication.shared.isIdleTimerDisabled = !canRecordWhileLocked
        tickLoopTask = Task { await runTickLoop() }
        startActivity(line: line, config: config, startedAt: startDate)
        playHaptic()
        return true
    }

    /// Writes a timestamp of this kind. Optional: Station when the doors open, Anomaly when
    /// something unusual happens. Called from the ride screen and from the lock screen.
    func mark(_ kind: MarkKind) async {
        guard isRecording else { return }
        let tMS = nowTMS()
        guard await write(RecorderMark(tMS: tMS, kind: kind.rawValue)) else { return }
        switch kind {
        case .station: stationMarkCount += 1
        case .anomaly: anomalyMarkCount += 1
        }
        lastMark = MarkConfirmation(kind: kind, elapsedSeconds: tMS / 1000)
        playHaptic()  // No effect while the app is in the background; the lock screen view updates instead.
        await updateActivity()
    }

    /// Remembers when the rider tapped Stop. Recording continues until `finish` or `cancelStop`.
    func noteStopTapped() {
        stopTappedTime = Self.clockSeconds()
    }

    func cancelStop() {
        stopTappedTime = nil
    }

    /// Ends the session. `endStationIndex` is nil when the rider is not sure where they got off.
    func finish(endStationIndex: Int?, note: String?) async {
        guard isRecording, let finishingWriter = writer else { return }

        stopSensors()
        let lastMotionEvent = takeMotionBatch()
        // Clear the writer first: probes still in flight then drop their result
        // instead of writing it after session_end.
        writer = nil
        isRecording = false
        tickLoopTask?.cancel()
        tickLoopTask = nil
        UIApplication.shared.isIdleTimerDisabled = false

        let nowTime = Self.clockSeconds()
        let endTime = stopTappedTime ?? nowTime
        var endStation: Station?
        if let endStationIndex, let line, line.stations.indices.contains(endStationIndex) {
            endStation = line.stations[endStationIndex]
        }
        let endEvent = RecorderSessionEnd(
            tMS: tMS(atTime: endTime),
            endedAt: iso8601String(Date().addingTimeInterval(endTime - nowTime)),
            reason: "user_stop",
            endStationID: endStation?.id,
            endStationName: endStation?.shortName,
            endStationIndex: endStation == nil ? nil : endStationIndex,
            note: note,
            summary: RecorderSessionSummary(marks: stationMarkCount + anomalyMarkCount, samples: sampleCount, probeOK: probeOKCount)
        )

        do {
            if let lastMotionEvent {
                try await finishingWriter.append(lastMotionEvent)
            }
            try await finishingWriter.append(endEvent)
        } catch {
            errorMessage = "Could not save the end of the ride (\(error.localizedDescription)). The samples are saved."
        }
        await finishingWriter.close()
        stopTappedTime = nil
        await endActivity()
    }

    // MARK: - Tick loop

    /// Runs `tick()` every `tickInterval` on a fixed schedule until recording stops.
    private func runTickLoop() async {
        var nextTickTime = Self.clockSeconds()
        while isRecording && !Task.isCancelled {
            tick()
            nextTickTime += Self.tickInterval
            let delay = nextTickTime - Self.clockSeconds()
            if delay > 0 {
                // Sleep only throws when cancelled (at finish); the loop condition then ends the loop.
                try? await Task.sleep(for: .seconds(delay))
            } else {
                // We fell behind, for example because iOS paused the app. Skip the missed
                // ticks instead of bursting; the gap shows in the file as missing samples.
                nextTickTime = Self.clockSeconds()
            }
        }
    }

    private func tick() {
        startProbe()
        if let motionEvent = takeMotionBatch() {
            Task { await write(motionEvent) }
        }
        refreshStatus()
    }

    private func refreshStatus() {
        isUsingWiFi = currentPath?.usesInterfaceType(.wifi) ?? false
        let authorization = locationManager.authorizationStatus
        canRecordWhileLocked = authorization == .authorizedWhenInUse || authorization == .authorizedAlways
    }

    /// Appends one event to the session file. Returns false when the session has ended or the write failed.
    @discardableResult
    private func write<Event: Encodable>(_ event: Event) async -> Bool {
        guard let writer else { return false }
        do {
            try await writer.append(event)
            // Writes work again: drop an old error, so the rider does not stop a healthy recording.
            errorMessage = nil
            return true
        } catch {
            errorMessage = "Could not save to the session file: \(error.localizedDescription)"
            return false
        }
    }

    // MARK: - Network probes

    /// Starts one probe and writes its sample when it finishes. Probes may overlap,
    /// so the time when service returns is known to within one tick.
    private func startProbe() {
        let startTMS = nowTMS()
        let path = makePathSnapshot(currentPath)
        let url = nextProbeURL()
        Task {
            let probe = await runProbe(url: url)
            let sample = RecorderSample(tMS: startTMS, path: path, probe: probe)
            guard await write(sample) else { return }
            sampleCount += 1
            if probe.ok {
                probeOKCount += 1
            }
            let hadService = serviceHistory.last
            serviceHistory = Array((serviceHistory + [probe.ok]).suffix(Self.serviceHistoryLength))
            lastSample = sample
            lastSavedSampleTime = Self.clockSeconds()
            if hadService != probe.ok {
                await updateActivity()
            }
        }
    }

    private func runProbe(url: URL) async -> ProbeSnapshot {
        var request = URLRequest(url: url)
        request.cachePolicy = .reloadIgnoringLocalAndRemoteCacheData
        request.timeoutInterval = Self.probeTimeout
        request.setValue("no-cache", forHTTPHeaderField: "Cache-Control")

        let reuseObserver = ConnectionReuseObserver()
        let probeStartTime = Self.clockSeconds()
        func elapsedMilliseconds() -> Int {
            max(0, Int(((Self.clockSeconds() - probeStartTime) * 1000).rounded()))
        }

        do {
            let (_, response) = try await URLSession.shared.data(for: request, delegate: reuseObserver)
            let httpStatus = (response as? HTTPURLResponse)?.statusCode
            var probeError: String?
            if httpStatus == 429 {
                noteRateLimit(for: url)
                probeError = "rate_limited"
            } else {
                clearRateLimit(for: url)
            }
            // Any HTTP response means the network reached the server, whatever the status code.
            return ProbeSnapshot(
                ok: true,
                url: url.absoluteString,
                rttMS: elapsedMilliseconds(),
                httpStatus: httpStatus,
                error: probeError,
                reusedConnection: reuseObserver.isReusedConnection
            )
        } catch {
            // A short, stable code (e.g. "URLError -1001" for a timeout) is easier to count than the full description.
            let errorDescription = (error as? URLError).map { "URLError \($0.code.rawValue)" } ?? String(describing: error)
            return ProbeSnapshot(
                ok: false,
                url: url.absoluteString,
                rttMS: elapsedMilliseconds(),
                httpStatus: nil,
                error: errorDescription,
                reusedConnection: reuseObserver.isReusedConnection
            )
        }
    }

    private func nextProbeURL() -> URL {
        let urls = Self.probeURLs
        let nowTime = Self.clockSeconds()

        for _ in 0..<urls.count {
            let url = urls[probeCursor % urls.count]
            probeCursor += 1
            if nowTime >= (probeBackoff[url]?.backoffUntilTime ?? 0) {
                return url
            }
        }

        // Every endpoint is backed off. Probe anyway; rotation keeps the load on each one low.
        let url = urls[probeCursor % urls.count]
        probeCursor += 1
        return url
    }

    /// Exponential backoff after HTTP 429: 15 s, 30 s, 60 s, ... up to 5 minutes, plus jitter.
    private func noteRateLimit(for url: URL) {
        let nowTime = Self.clockSeconds()
        var state = probeBackoff[url] ?? ProbeBackoffState(backoffUntilTime: 0, strikes: 0)
        state.strikes += 1

        let baseDelay = 15.0
        let maximumDelay = 300.0
        let delay = min(maximumDelay, baseDelay * pow(2.0, Double(state.strikes - 1)))
        let jitter = Double.random(in: 0...min(5.0, delay * 0.1))
        state.backoffUntilTime = nowTime + delay + jitter
        probeBackoff[url] = state
    }

    private func clearRateLimit(for url: URL) {
        probeBackoff[url] = nil
    }

    // MARK: - Network path

    private func startPathMonitor() {
        let monitor = NWPathMonitor()
        monitor.pathUpdateHandler = { [weak self] path in
            Task { @MainActor in self?.currentPath = path }
        }
        monitor.start(queue: pathMonitorQueue)
        pathMonitor = monitor
    }

    private func makePathSnapshot(_ path: NWPath?) -> PathSnapshot {
        guard let path else {
            return PathSnapshot(status: "unknown", interface: nil, expensive: false, constrained: false, radio: currentRadio())
        }

        let status: String
        switch path.status {
        case .satisfied: status = "satisfied"
        case .unsatisfied: status = "unsatisfied"
        case .requiresConnection: status = "requires_connection"
        @unknown default: status = "unknown"
        }

        let interface: String
        if path.usesInterfaceType(.wifi) {
            interface = "wifi"
        } else if path.usesInterfaceType(.cellular) {
            interface = "cellular"
        } else if path.usesInterfaceType(.wiredEthernet) {
            interface = "wired"
        } else if path.usesInterfaceType(.loopback) {
            interface = "loopback"
        } else {
            interface = "other"
        }

        return PathSnapshot(
            status: status,
            interface: interface,
            expensive: path.isExpensive,
            constrained: path.isConstrained,
            radio: currentRadio()
        )
    }

    /// Radio type of the data SIM, e.g. "LTE" or "NR" (5G). Nil when iOS does not report it.
    private func currentRadio() -> String? {
        guard
            let dataServiceID = telephonyInfo.dataServiceIdentifier,
            let technology = telephonyInfo.serviceCurrentRadioAccessTechnology?[dataServiceID]
        else { return nil }
        return technology.replacingOccurrences(of: "CTRadioAccessTechnology", with: "")
    }

    // MARK: - Motion

    private func startMotionUpdates() {
        isMotionAvailable = motionManager.isDeviceMotionAvailable
        guard isMotionAvailable else { return }  // The Simulator has no motion sensors.

        motionBatch = MotionBatch()
        motionManager.deviceMotionUpdateInterval = 1.0 / Double(Self.motionHz)
        motionManager.startDeviceMotionUpdates(to: .main) { [weak self] motion, _ in
            guard let motion else { return }
            // CoreMotion timestamps use system uptime, which stops while the phone sleeps.
            // A reading arrives milliseconds after it is taken, so convert it to our clock now.
            let readingTime = Self.clockSeconds() - (ProcessInfo.processInfo.systemUptime - motion.timestamp)
            // Delivered on the main queue, so main-actor state is safe to change here.
            MainActor.assumeIsolated { self?.motionBatch.add(motion, readingTime: readingTime) }
        }
    }

    /// Returns the readings since the last call as one event, and starts a new batch.
    private func takeMotionBatch() -> RecorderMotion? {
        let batch = motionBatch
        motionBatch = MotionBatch()
        motionReadingsPerSecond = batch.xMilliG.count
        guard let firstReadingTime = batch.firstReadingTime else { return nil }

        return RecorderMotion(
            tMS: tMS(atTime: firstReadingTime),
            hz: Self.motionHz,
            xMilliG: batch.xMilliG,
            yMilliG: batch.yMilliG,
            zMilliG: batch.zMilliG,
            gravityMilliG: batch.meanGravityMilliG
        )
    }

    // MARK: - Location (keeps the app running while locked)

    /// The "location" background mode is only a way to stay awake: location updates are
    /// requested but never read or saved.
    private func startLocationKeepAlive() {
        locationManager.desiredAccuracy = kCLLocationAccuracyThreeKilometers  // Lowest power; we ignore the fixes.
        locationManager.pausesLocationUpdatesAutomatically = false  // Underground there are no fixes; a pause would suspend the app.
        locationManager.allowsBackgroundLocationUpdates = true  // Needs UIBackgroundModes "location" in Info.plist.
        locationManager.showsBackgroundLocationIndicator = true  // The blue status pill shows that recording is alive.
        locationManager.startUpdatingLocation()
        // Tells iOS that a When-In-Use app has an ongoing activity, so it keeps running in the background.
        backgroundActivitySession = CLBackgroundActivitySession()
    }

    private func stopSensors() {
        pathMonitor?.cancel()
        pathMonitor = nil
        currentPath = nil
        motionManager.stopDeviceMotionUpdates()
        locationManager.stopUpdatingLocation()
        locationManager.allowsBackgroundLocationUpdates = false
        backgroundActivitySession?.invalidate()
        backgroundActivitySession = nil
    }

    // MARK: - Live Activity (lock screen and Dynamic Island)

    /// Ends Live Activities left from a ride that did not finish, for example when iOS ended the app.
    static func endStaleActivities() async {
        for staleActivity in Activity<RecordingActivityAttributes>.activities {
            await staleActivity.end(nil, dismissalPolicy: .immediate)
        }
    }

    private func startActivity(line: Line, config: SessionConfig, startedAt: Date) {
        // Off when the rider turned Live Activities off in Settings; the recording does not need it.
        guard ActivityAuthorizationInfo().areActivitiesEnabled else { return }
        let attributes = RecordingActivityAttributes(
            lineID: config.lineID,
            lineColorHex: line.color,
            directionLabel: config.directionLabel,
            startedAt: startedAt
        )
        // A failure only means no lock screen view; the recording goes on.
        activity = try? Activity.request(attributes: attributes, content: ActivityContent(state: activityState, staleDate: nil))
    }

    private var activityState: RecordingActivityAttributes.ContentState {
        RecordingActivityAttributes.ContentState(
            hasService: serviceHistory.last ?? true,
            stationMarkCount: stationMarkCount,
            anomalyMarkCount: anomalyMarkCount,
            lastMarkKind: lastMark?.kind,
            lastMarkElapsedSeconds: lastMark?.elapsedSeconds
        )
    }

    /// Sends the current state to the lock screen. Called only when it changes: on a mark, and
    /// when service is lost or comes back.
    private func updateActivity() async {
        await activity?.update(ActivityContent(state: activityState, staleDate: nil))
    }

    private func endActivity() async {
        await activity?.end(ActivityContent(state: activityState, staleDate: nil), dismissalPolicy: .immediate)
        activity = nil
    }

    // MARK: - Time

    /// Seconds on a clock that keeps counting while the phone sleeps. Every event time uses it,
    /// so time spent asleep shows up in the recording as a gap. System uptime (and CoreMotion's
    /// timestamps) stop during sleep, which would make that time silently disappear.
    nonisolated static func clockSeconds() -> TimeInterval {
        Double(clock_gettime_nsec_np(CLOCK_MONOTONIC_RAW)) / 1_000_000_000
    }

    private func nowTMS() -> Int {
        tMS(atTime: Self.clockSeconds())
    }

    private func tMS(atTime time: TimeInterval) -> Int {
        max(0, Int(((time - sessionStartTime) * 1000).rounded()))
    }

    // MARK: - Session file

    static func sessionsDirectoryURL() -> URL {
        let documentsURL = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
        return documentsURL.appendingPathComponent("train-gap-sessions", isDirectory: true)
    }

    static func sessionFileURL(sessionID: String, startedAt: Date, config: SessionConfig) -> URL {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        // Colons are not safe in file names on every system that may receive the file.
        let dateText = formatter.string(from: startedAt).replacingOccurrences(of: ":", with: "-")
        let fileName = "\(dateText)_\(config.lineID)_\(config.startStationID)-\(config.directionTerminalStationID)_\(sessionID.prefix(8)).ndjson"
        return sessionsDirectoryURL().appendingPathComponent(fileName)
    }

    private func makeSessionStartEvent(sessionID: String, startedAt: Date, line: Line, config: SessionConfig) -> RecorderSessionStart {
        let device = DeviceInfo(
            model: UIDevice.current.model,
            systemName: UIDevice.current.systemName,
            systemVersion: UIDevice.current.systemVersion
        )
        let dataset = LinesDatasetInfo(source: "bundle:lines.json", generatedAt: nil, sha256: bundledLinesJSONSHA256())
        let probeURLs = Self.probeURLs.map(\.absoluteString)
        let routeIndices = [config.startStationIndex] + line.stationIndicesAfter(config.startStationIndex, direction: config.direction)
        let routeStations = routeIndices.map { RouteStation(id: line.stations[$0].id, name: line.stations[$0].shortName) }

        return RecorderSessionStart(
            sessionID: sessionID,
            startedAt: iso8601String(startedAt),
            lineID: config.lineID,
            lineName: config.lineName,
            fromStationID: config.startStationID,
            fromStationName: config.startStationName,
            toStationID: config.directionTerminalStationID,
            toStationName: config.directionTerminalStationName,
            direction: config.direction,
            directionLabel: config.directionLabel,
            directionTerminalStationID: config.directionTerminalStationID,
            directionTerminalStationName: config.directionTerminalStationName,
            probeURL: probeURLs.first,
            probeURLs: probeURLs,
            sampleIntervalMS: Int((Self.tickInterval * 1000).rounded()),
            carrier: config.carrier,
            // Ask the sensor directly: this event is written before motion updates start.
            motionHz: motionManager.isDeviceMotionAvailable ? Self.motionHz : nil,
            routeStations: routeStations,
            linesDataset: dataset,
            device: device
        )
    }

    /// Lets the summarizer check that a recording used the same station list as the simulator.
    private func bundledLinesJSONSHA256() -> String? {
        guard
            let url = Bundle.main.url(forResource: "lines", withExtension: "json"),
            let data = try? Data(contentsOf: url)
        else { return nil }
        return SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
    }

    private func playHaptic() {
        UIImpactFeedbackGenerator(style: .medium).impactOccurred()
    }
}

/// A mark that was saved, for the confirmation pill.
struct MarkConfirmation: Equatable {
    let id = UUID()  // Two marks of the same kind in the same second are still two confirmations.
    let kind: MarkKind
    let elapsedSeconds: Int
}

/// Accelerometer readings collected between two ticks.
private struct MotionBatch {
    var firstReadingTime: TimeInterval?
    var xMilliG: [Int] = []
    var yMilliG: [Int] = []
    var zMilliG: [Int] = []
    var gravitySumX = 0.0
    var gravitySumY = 0.0
    var gravitySumZ = 0.0

    /// `readingTime` is when the reading was taken, on `Recorder.clockSeconds()`.
    mutating func add(_ motion: CMDeviceMotion, readingTime: TimeInterval) {
        if firstReadingTime == nil {
            firstReadingTime = readingTime
        }
        xMilliG.append(Self.milliG(motion.userAcceleration.x))
        yMilliG.append(Self.milliG(motion.userAcceleration.y))
        zMilliG.append(Self.milliG(motion.userAcceleration.z))
        gravitySumX += motion.gravity.x
        gravitySumY += motion.gravity.y
        gravitySumZ += motion.gravity.z
    }

    var meanGravityMilliG: [Int] {
        let readingCount = Double(max(1, xMilliG.count))
        return [
            Self.milliG(gravitySumX / readingCount),
            Self.milliG(gravitySumY / readingCount),
            Self.milliG(gravitySumZ / readingCount),
        ]
    }

    /// Whole milli-g keeps the file small; 1 mg is far below the phone's sensor noise.
    private static func milliG(_ valueInG: Double) -> Int {
        Int((valueInG * 1000).rounded())
    }
}

/// Records whether a probe reused an open connection. A new connection adds DNS, TCP, and TLS time.
/// URLSession reports metrics before the request completes, so the value is set when `data(for:)` returns.
/// The callback runs on URLSession's queue and the read on the main actor, so a mutex guards the value.
private final class ConnectionReuseObserver: NSObject, URLSessionTaskDelegate, Sendable {
    private let reusedConnection = Mutex<Bool?>(nil)

    var isReusedConnection: Bool? {
        reusedConnection.withLock { $0 }
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didFinishCollecting metrics: URLSessionTaskMetrics) {
        let isReused = metrics.transactionMetrics.last?.isReusedConnection
        reusedConnection.withLock { $0 = isReused }
    }
}
