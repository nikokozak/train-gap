import SwiftUI

struct SessionsView: View {
    @EnvironmentObject private var sessionsStore: SessionsStore

    var body: some View {
        List {
            if sessionsStore.sessions.isEmpty {
                Text("No sessions yet.")
                    .foregroundStyle(.secondary)
            } else {
                ForEach(sessionsStore.sessions) { session in
                    SessionRow(session: session)
                }
                .onDelete(perform: delete)
            }
        }
        .navigationTitle("Sessions")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    sessionsStore.refresh()
                } label: {
                    Image(systemName: "arrow.clockwise")
                }
            }
        }
    }

    private func delete(at offsets: IndexSet) {
        for idx in offsets {
            sessionsStore.delete(sessionsStore.sessions[idx])
        }
    }
}

private struct SessionRow: View {
    var session: SessionInfo

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("\(session.lineID) \(session.startStationName) → \(session.directionTerminalStationName)")
                    .font(.headline)
                Spacer()
                ShareLink(item: session.fileURL) {
                    Image(systemName: "square.and.arrow.up")
                }
            }
            Text(session.startedAt == Date.distantPast ? session.fileURL.lastPathComponent : dateString(session.startedAt))
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(session.fileURL.lastPathComponent)
                .font(.caption2)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 4)
    }

    private func dateString(_ d: Date) -> String {
        let fmt = DateFormatter()
        fmt.dateStyle = .medium
        fmt.timeStyle = .medium
        return fmt.string(from: d)
    }
}
