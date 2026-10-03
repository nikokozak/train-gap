import Foundation

@MainActor
final class LinesStore: ObservableObject {
    @Published private(set) var lines: [Line] = []
    @Published private(set) var loadError: String?

    func loadBundledLinesJSON() {
        guard let url = Bundle.main.url(forResource: "lines", withExtension: "json") else {
            loadError = "Missing lines.json in app bundle."
            lines = []
            return
        }

        do {
            let data = try Data(contentsOf: url)
            let decoder = JSONDecoder()
            decoder.dateDecodingStrategy = .iso8601
            let bundle = try decoder.decode(LinesBundle.self, from: data)
            lines = bundle.lines.sorted { $0.id < $1.id }
            loadError = nil
        } catch {
            loadError = "Failed to load lines.json: \(error.localizedDescription)"
            lines = []
        }
    }
}

