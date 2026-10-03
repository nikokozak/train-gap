import Foundation

actor NDJSONWriter {
    private var handle: FileHandle
    private let encoder: JSONEncoder

    init(fileURL: URL) throws {
        let dir = fileURL.deletingLastPathComponent()
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        if !FileManager.default.fileExists(atPath: fileURL.path) {
            FileManager.default.createFile(atPath: fileURL.path, contents: nil)
        }
        handle = try FileHandle(forWritingTo: fileURL)
        try handle.seekToEnd()

        encoder = JSONEncoder()
        encoder.outputFormatting = []
    }

    func append<T: Encodable>(_ event: T) throws {
        let data = try encoder.encode(event)
        try handle.write(contentsOf: data)
        try handle.write(contentsOf: Data([0x0A]))
    }

    func close() {
        try? handle.close()
    }
}

