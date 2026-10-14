import AppIntents

/// The Station and Anomaly buttons on the lock screen and in the Dynamic Island. They work
/// without unlocking the phone. iOS performs a LiveActivityIntent in the app's process, where the
/// recorder runs, so the mark goes into the same session file as a tap in the app.
struct MarkIntent: LiveActivityIntent {
    static let title: LocalizedStringResource = "Mark"
    static let description = IntentDescription("Marks a station or an anomaly in the ride that is recording.")

    /// A MarkKind raw value.
    @Parameter(title: "Kind")
    var kind: String

    init() {}

    init(kind: MarkKind) {
        self.kind = kind.rawValue
    }

    func perform() async throws -> some IntentResult {
        if let handler = await MarkIntent.handler {
            await handler(MarkKind(rawValue: kind) ?? .station)
        }
        return .result()
    }

    /// The app sets this when it starts. The widget extension never performs the intent, so there
    /// it stays nil.
    @MainActor static var handler: ((MarkKind) async -> Void)?
}
