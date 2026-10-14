import Foundation
import Network
import Testing
@testable import TrainGapRecorder

/// The UDP probe matches each reply to its request by transaction ID (RFC 8489, section 5).
struct StunMessageTests {
    private let transactionID = Data(1...12)

    @Test func requestIsABindingRequestWithNoAttributes() {
        let request = StunProber.bindingRequest(transactionID: transactionID)
        #expect([UInt8](request) == [0x00, 0x01, 0x00, 0x00, 0x21, 0x12, 0xA4, 0x42] + Array(1...12))
    }

    @Test func successAndErrorResponsesGiveTheTransactionID() {
        for messageType: [UInt8] in [[0x01, 0x01], [0x01, 0x11]] {
            // Length 12: one XOR-MAPPED-ADDRESS attribute follows the header. Its content does not matter here.
            let response = Data(messageType + [0x00, 0x0C] + StunProber.magicCookie) + transactionID + Data(count: 12)
            #expect(StunProber.transactionID(ofResponse: response) == transactionID)
        }
    }

    @Test func otherDatagramsAreIgnored() {
        let header = Data([0x01, 0x01, 0x00, 0x00])
        #expect(StunProber.transactionID(ofResponse: header + Data([0, 0, 0, 0]) + transactionID) == nil)  // No magic cookie.
        #expect(StunProber.transactionID(ofResponse: Data([0x00, 0x01, 0x00, 0x00] + StunProber.magicCookie) + transactionID) == nil)  // A request.
        #expect(StunProber.transactionID(ofResponse: header + Data(StunProber.magicCookie)) == nil)  // Too short.
    }

    @Test func eventWritesLostRequestsAsNull() throws {
        let event = RecorderUDPProbe(tMS: 1000, intervalMS: 200, rttMS: [31, nil, 40])
        let json = String(decoding: try JSONEncoder().encode(event), as: UTF8.self)
        #expect(json.contains(#""rtt_ms":[31,null,40]"#))
        #expect(json.contains(#""type":"udp_probe""#))
    }
}

/// Without replies, as in a tunnel, every request counts as lost.
struct StunProberLossTests {
    @Test func requestsWithoutRepliesAreLost() async throws {
        let silentServer = try SilentUDPServer()
        defer { silentServer.close() }
        let prober = StunProber(host: "127.0.0.1", port: NWEndpoint.Port(rawValue: silentServer.port)!)
        prober.start()
        defer { prober.stop() }

        let roundTripsMS = await prober.measure(over: 0.1)
        #expect(roundTripsMS == Array(repeating: nil, count: StunProber.requestsPerTick))
    }
}

/// A UDP socket on 127.0.0.1 that receives requests and never answers. Unlike a closed port, it
/// sends no ICMP error back, so the requests just vanish.
private struct SilentUDPServer {
    let descriptor: Int32
    let port: UInt16

    init() throws {
        let descriptor = socket(AF_INET, SOCK_DGRAM, 0)
        var address = sockaddr_in()
        address.sin_family = sa_family_t(AF_INET)
        address.sin_addr.s_addr = inet_addr("127.0.0.1")
        var length = socklen_t(MemoryLayout<sockaddr_in>.size)
        let result = withUnsafeMutablePointer(to: &address) { pointer in
            pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) { socketAddress in
                bind(descriptor, socketAddress, length) == 0 ? getsockname(descriptor, socketAddress, &length) : -1
            }
        }
        guard descriptor >= 0, result == 0 else { throw POSIXError(.EADDRNOTAVAIL) }
        self.descriptor = descriptor
        port = UInt16(bigEndian: address.sin_port)
    }

    func close() {
        Darwin.close(descriptor)
    }
}
