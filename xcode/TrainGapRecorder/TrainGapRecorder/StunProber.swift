import Foundation
import Network
import Synchronization

/// Measures UDP loss and round trips with STUN Binding requests (RFC 8489): the small "what is my
/// address" request that WebRTC sends. Calls, voice, and games feel packet loss and jitter, which
/// the one-per-second HTTPS probe cannot show: TCP hides loss behind resends. The reply contains
/// the phone's public address; the app does not save it.
final class StunProber: Sendable {
    /// Cloudflare's public STUN server. It is anycast, so a nearby city answers.
    static let serverHost = "stun.cloudflare.com"
    static let serverPort: NWEndpoint.Port = 3478
    static var serverDescription: String { "\(serverHost):\(serverPort)" }
    /// Requests per tick, evenly spaced: loss in steps of 20% per second, about 0.5 KB/s in all.
    static let requestsPerTick = 5
    /// A later reply counts as lost. Real-time media is useless after about 1 s; 2 s shows how late replies get.
    static let replyTimeout: TimeInterval = 2.0
    /// After this many lost requests in a row while the HTTPS probe gets through, the UDP flow
    /// opens again (`restartIfStuck`).
    static let restartAfterLostCount = 10

    private let host: NWEndpoint.Host
    private let port: NWEndpoint.Port
    private let queue = DispatchQueue(label: "TrainGapRecorder.STUN")
    private let state = Mutex(State())

    /// Tests pass a local server.
    init(host: String = serverHost, port: NWEndpoint.Port = serverPort) {
        self.host = NWEndpoint.Host(host)
        self.port = port
    }

    private struct State {
        /// Between start and stop. A burst that runs past stop sends nothing more.
        var isRunning = false
        var connection: NWConnection?
        /// On `Recorder.clockSeconds()`, by transaction ID. A request leaves both when it is measured.
        var sentTimes: [Data: TimeInterval] = [:]
        var replyTimes: [Data: TimeInterval] = [:]
        var lostInARow = 0
    }

    /// Opens the UDP flow. Call it when the ride starts, so the first tick finds it ready.
    func start() {
        // makeConnection does not take the lock (its handlers run later, on `queue`), so it can run inside it.
        let oldConnection = state.withLock { state in
            let oldConnection = state.connection
            state.connection = makeConnection()
            state.isRunning = true
            state.lostInARow = 0
            return oldConnection
        }
        oldConnection?.cancel()
    }

    func stop() {
        let connection = state.withLock { state in
            let connection = state.connection
            state.connection = nil
            state.isRunning = false
            return connection
        }
        connection?.cancel()
    }

    /// Sends `requestsPerTick` requests spread over `duration` and returns each round trip in
    /// milliseconds, in send order: nil when no reply came within `replyTimeout`.
    func measure(over duration: TimeInterval) async -> [Int?] {
        var transactionIDs: [Data?] = []
        for index in 0..<Self.requestsPerTick {
            if index > 0 {
                // Sleep only throws when the task is cancelled; nothing cancels these tasks.
                try? await Task.sleep(for: .seconds(duration / Double(Self.requestsPerTick)))
            }
            transactionIDs.append(send())
        }
        try? await Task.sleep(for: .seconds(Self.replyTimeout))  // As above.
        let roundTripsMS = transactionIDs.map { transactionID in transactionID.flatMap(takeRoundTripMS) }
        state.withLock { state in
            for roundTripMS in roundTripsMS {
                state.lostInARow = roundTripMS == nil ? state.lostInARow + 1 : 0
            }
        }
        return roundTripsMS
    }

    /// Opens a new UDP flow when the last `restartAfterLostCount` requests were lost. Call it
    /// when the HTTPS probe gets through: after a tunnel, the carrier may give the phone a new
    /// address, and the old flow can stop working without failing.
    func restartIfStuck() {
        let isStuck = state.withLock { $0.isRunning && $0.lostInARow >= Self.restartAfterLostCount }
        if isStuck {
            start()
        }
    }

    // MARK: - UDP flow

    private func makeConnection() -> NWConnection {
        let connection = NWConnection(host: host, port: port, using: .udp)
        connection.stateUpdateHandler = { [weak self, weak connection] newState in
            guard case .failed = newState, let connection else { return }
            // The next request opens a new flow.
            connection.cancel()
            self?.state.withLock { state in
                if state.connection === connection {
                    state.connection = nil
                }
            }
        }
        receive(on: connection)
        connection.start(queue: queue)
        return connection
    }

    /// Sends one request. Returns its transaction ID, or nil when there is no network path: on the
    /// phone, that request is lost.
    private func send() -> Data? {
        let connection: NWConnection? = state.withLock { state in
            guard state.isRunning else { return nil }
            let connection = state.connection ?? makeConnection()  // After a failure. See start().
            state.connection = connection
            return connection
        }
        guard let connection else { return nil }
        switch connection.state {
        case .setup, .preparing, .ready:
            break  // Before .ready, the send waits only for the socket to open.
        default:
            return nil  // .waiting: no network path. iOS would hold the request and send it late.
        }
        let transactionID = Self.randomTransactionID()
        state.withLock { $0.sentTimes[transactionID] = Recorder.clockSeconds() }
        // A failed send loses the request; the missing reply records that.
        connection.send(content: Self.bindingRequest(transactionID: transactionID), completion: .contentProcessed { _ in })
        return transactionID
    }

    /// After a receive error, the next read waits this long, so an error that repeats cannot spin.
    private static let receiveRetryDelay: TimeInterval = 0.1

    /// Reads replies, one datagram per call, until the flow ends. A receive error (for example
    /// when the radio loses its path) does not end the flow, so reading goes on.
    private func receive(on connection: NWConnection) {
        connection.receiveMessage { [weak self, weak connection] data, _, _, error in
            let receivedTime = Recorder.clockSeconds()
            guard let self, let connection else { return }
            if let data, let transactionID = Self.transactionID(ofResponse: data) {
                state.withLock { state in
                    if state.sentTimes[transactionID] != nil {
                        state.replyTimes[transactionID] = receivedTime
                    }
                }
            }
            switch connection.state {
            case .cancelled, .failed:
                return  // The flow ended; the next request opens a new one.
            default:
                break
            }
            if error == nil {
                receive(on: connection)
            } else {
                queue.asyncAfter(deadline: .now() + Self.receiveRetryDelay) { [weak self] in
                    self?.receive(on: connection)
                }
            }
        }
    }

    private func takeRoundTripMS(_ transactionID: Data) -> Int? {
        state.withLock { state in
            let sentTime = state.sentTimes.removeValue(forKey: transactionID)
            let replyTime = state.replyTimes.removeValue(forKey: transactionID)
            guard let sentTime, let replyTime, replyTime - sentTime <= Self.replyTimeout else { return nil }
            return Int(((replyTime - sentTime) * 1000).rounded())
        }
    }

    // MARK: - STUN messages (RFC 8489, section 5)

    /// Every STUN message carries it in bytes 4 to 7.
    static let magicCookie: [UInt8] = [0x21, 0x12, 0xA4, 0x42]

    static func randomTransactionID() -> Data {
        Data((0..<12).map { _ in UInt8.random(in: .min ... .max) })
    }

    /// A Binding request: type 0x0001, no attributes (length 0), the magic cookie, and the 12-byte
    /// transaction ID.
    static func bindingRequest(transactionID: Data) -> Data {
        Data([0x00, 0x01, 0x00, 0x00] + magicCookie) + transactionID
    }

    /// The transaction ID of a Binding response (success 0x0101 or error 0x0111), or nil for any
    /// other datagram. An error response still proves the round trip.
    static func transactionID(ofResponse data: Data) -> Data? {
        let bytes = [UInt8](data)
        guard bytes.count >= 20, Array(bytes[4..<8]) == magicCookie else { return nil }
        let messageType = UInt16(bytes[0]) << 8 | UInt16(bytes[1])
        guard messageType == 0x0101 || messageType == 0x0111 else { return nil }
        return Data(bytes[8..<20])
    }
}
