import SwiftUI

@main
struct TrainGapRecorderApp: App {
    @StateObject private var linesStore = LinesStore()
    @StateObject private var recorder = Recorder()
    @StateObject private var sessionsStore = SessionsStore()

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(linesStore)
                .environmentObject(recorder)
                .environmentObject(sessionsStore)
                .onAppear {
                    linesStore.loadBundledLinesJSON()
                    sessionsStore.refresh()
                    // The lock screen buttons write marks through the same recorder.
                    MarkIntent.handler = { [recorder] kind in await recorder.mark(kind) }
                }
                .task {
                    await Recorder.endStaleActivities()
                }
        }
    }
}

