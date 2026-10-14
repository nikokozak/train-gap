import SwiftUI

/// An MTA-style route bullet. Used on the ride screen and the lock screen.
struct LineBadge: View {
    var lineID: String
    var hexColor: String?
    var size: CGFloat = 52

    /// MTA prints black letters on the yellow Broadway-line bullets and white letters on all others.
    private static let blackTextLines: Set<String> = ["N", "Q", "R", "W"]

    var body: some View {
        Text(lineID)
            .font(.system(size: size * 0.55, weight: .bold))
            .foregroundStyle(Self.blackTextLines.contains(lineID) ? .black : .white)
            .frame(width: size, height: size)
            .background(Circle().fill(Color(hex: hexColor) ?? .gray))
    }
}

extension Color {
    /// Parses "#RRGGBB" (the format in lines.json). Returns nil for anything else.
    init?(hex: String?) {
        guard let hex, hex.hasPrefix("#"), hex.count == 7, let value = Int(hex.dropFirst(), radix: 16) else { return nil }
        self.init(
            red: Double((value >> 16) & 0xFF) / 255,
            green: Double((value >> 8) & 0xFF) / 255,
            blue: Double(value & 0xFF) / 255
        )
    }
}

/// Phone-style signal bars, 0 to 4.
struct SignalBars: View {
    var bars: Int
    var color: Color

    var body: some View {
        HStack(alignment: .bottom, spacing: 2) {
            ForEach(1...4, id: \.self) { bar in
                RoundedRectangle(cornerRadius: 1.5)
                    .fill(bar <= bars ? color : Color.secondary.opacity(0.3))
                    .frame(width: 5, height: CGFloat(4 + bar * 4))
            }
        }
        .accessibilityElement()
        .accessibilityLabel("\(bars) of 4 bars")
    }
}
