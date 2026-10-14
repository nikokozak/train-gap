import ActivityKit
import AppIntents
import SwiftUI
import WidgetKit

@main
struct TrainGapRecorderWidgets: WidgetBundle {
    var body: some Widget {
        RecordingLiveActivity()
    }
}

/// A recording on the lock screen and in the Dynamic Island: that it runs, for how long, whether
/// there is service, and buttons that mark a station or an anomaly without unlocking the phone.
/// The timer counts on its own; the app updates the rest only when it changes.
struct RecordingLiveActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: RecordingActivityAttributes.self) { context in
            LockScreenView(context: context)
        } dynamicIsland: { context in
            DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    LineBadge(lineID: context.attributes.lineID, hexColor: context.attributes.lineColorHex, size: 36)
                }
                DynamicIslandExpandedRegion(.center) {
                    VStack(spacing: 2) {
                        Text(context.attributes.directionLabel)
                            .font(.headline)
                            .lineLimit(1)
                        ServiceLabel(hasService: context.state.hasService)
                    }
                }
                DynamicIslandExpandedRegion(.trailing) {
                    ElapsedTimer(startedAt: context.attributes.startedAt)
                        .font(.title3.monospacedDigit().bold())
                }
                DynamicIslandExpandedRegion(.bottom) {
                    VStack(spacing: 6) {
                        MarkButtons(state: context.state)
                        LastMarkText(state: context.state)
                    }
                }
            } compactLeading: {
                Image(systemName: "record.circle")
                    .foregroundStyle(.red)
            } compactTrailing: {
                ElapsedTimer(startedAt: context.attributes.startedAt)
                    .font(.caption.monospacedDigit())
                    .frame(maxWidth: 48)
            } minimal: {
                Image(systemName: "record.circle")
                    .foregroundStyle(.red)
            }
        }
    }
}

private struct LockScreenView: View {
    let context: ActivityViewContext<RecordingActivityAttributes>

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 10) {
                LineBadge(lineID: context.attributes.lineID, hexColor: context.attributes.lineColorHex, size: 36)
                VStack(alignment: .leading, spacing: 2) {
                    // Terminal names are long ("To Canarsie-Rockaway Pkwy"): two lines, slightly smaller if needed.
                    Text(context.attributes.directionLabel)
                        .font(.headline)
                        .lineLimit(2)
                        .minimumScaleFactor(0.8)
                    ServiceLabel(hasService: context.state.hasService)
                }
                Spacer()
                VStack(alignment: .trailing, spacing: 2) {
                    Label("REC", systemImage: "record.circle")
                        .font(.caption.bold())
                        .foregroundStyle(.red)
                    ElapsedTimer(startedAt: context.attributes.startedAt)
                        .font(.title2.monospacedDigit().bold())
                }
            }
            MarkButtons(state: context.state)
            LastMarkText(state: context.state)
        }
        .padding()
    }
}

/// Counts up from the start of the ride without updates from the app.
private struct ElapsedTimer: View {
    let startedAt: Date

    var body: some View {
        Text(startedAt, style: .timer)
            .multilineTextAlignment(.trailing)
    }
}

private struct ServiceLabel: View {
    let hasService: Bool

    var body: some View {
        Label(hasService ? "Service" : "No service", systemImage: hasService ? "antenna.radiowaves.left.and.right" : "antenna.radiowaves.left.and.right.slash")
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(hasService ? .green : .red)
    }
}

/// The two mark buttons, with how many of each this ride has.
private struct MarkButtons: View {
    let state: RecordingActivityAttributes.ContentState

    var body: some View {
        HStack(spacing: 10) {
            button(.station, count: state.stationMarkCount, color: .blue)
            button(.anomaly, count: state.anomalyMarkCount, color: .orange)
        }
    }

    private func button(_ kind: MarkKind, count: Int, color: Color) -> some View {
        Button(intent: MarkIntent(kind: kind)) {
            Label("\(kind.buttonTitle) · \(count)", systemImage: kind.systemImage)
                .font(.subheadline.weight(.semibold))
                .frame(maxWidth: .infinity, minHeight: 36)
        }
        .tint(color)
    }
}

/// "Station marked at 4:12". It changes with every mark, so a tap on the lock screen is confirmed.
private struct LastMarkText: View {
    let state: RecordingActivityAttributes.ContentState

    var body: some View {
        if let kind = state.lastMarkKind, let elapsedSeconds = state.lastMarkElapsedSeconds {
            Label("\(kind.buttonTitle) marked at \(formatElapsed(seconds: elapsedSeconds))", systemImage: "checkmark.circle.fill")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }
}
