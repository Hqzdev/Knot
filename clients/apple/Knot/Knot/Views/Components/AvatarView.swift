import SwiftUI

struct AvatarView: View {
  let username: String
  var size: CGFloat = 46

  var body: some View {
    Circle()
      .fill(
        LinearGradient(
          colors: palette,
          startPoint: .topLeading,
          endPoint: .bottomTrailing
        )
      )
      .frame(width: size, height: size)
      .overlay {
        Text(initial)
          .font(.system(size: size * 0.4, weight: .semibold, design: .rounded))
          .foregroundStyle(.white)
      }
      .accessibilityHidden(true)
  }

  private var initial: String {
    String(username.trimmingCharacters(in: .whitespacesAndNewlines).prefix(1)).uppercased()
  }

  private var palette: [Color] {
    let palettes: [[Color]] = [
      [KnotStyle.accent, .cyan],
      [.indigo, .purple],
      [.orange, .pink],
      [.teal, .green],
      [.blue, .indigo],
    ]
    let value = username.unicodeScalars.reduce(0) { partialResult, scalar in
      partialResult + Int(scalar.value)
    }
    return palettes[value % palettes.count]
  }
}
