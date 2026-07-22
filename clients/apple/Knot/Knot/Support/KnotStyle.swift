import SwiftUI

#if os(iOS)
  import UIKit
#elseif os(macOS)
  import AppKit
#endif

enum KnotStyle {
  static let accent = Color(red: 0.12, green: 0.55, blue: 0.96)
  static let outgoing = Color(red: 0.08, green: 0.49, blue: 0.91)
  static let cornerRadius: CGFloat = 20
  static let compactCornerRadius: CGFloat = 14
  static let avatarSize: CGFloat = 54
  static let conversationRowHeight: CGFloat = 70
  static let contentWidth: CGFloat = 780

  #if os(iOS)
    static let primaryBackground = Color(uiColor: .systemBackground)
    static let secondaryBackground = Color(uiColor: .secondarySystemBackground)
    static let groupedBackground = Color(uiColor: .systemGroupedBackground)
    static let separator = Color(uiColor: .separator)
  #elseif os(macOS)
    static let primaryBackground = Color(nsColor: .windowBackgroundColor)
    static let secondaryBackground = Color(nsColor: .controlBackgroundColor)
    static let groupedBackground = Color(nsColor: .underPageBackgroundColor)
    static let separator = Color(nsColor: .separatorColor)
  #endif
}

struct KnotBackground: View {
  var body: some View {
    ZStack {
      KnotStyle.primaryBackground
      LinearGradient(
        colors: [KnotStyle.accent.opacity(0.11), .clear, Color.cyan.opacity(0.05)],
        startPoint: .topLeading,
        endPoint: .bottomTrailing
      )
    }
    .ignoresSafeArea()
  }
}

struct KnotChatBackground: View {
  @Environment(\.colorScheme) private var colorScheme
  @Environment(\.accessibilityReduceTransparency) private var reduceTransparency

  var body: some View {
    ZStack {
      KnotStyle.groupedBackground
      Canvas { context, size in
        var symbol = context.resolve(
          Image(systemName: "point.3.connected.trianglepath.dotted")
        )
        symbol.shading = .color(KnotStyle.accent.opacity(patternOpacity))
        let tileSize: CGFloat = 26
        let horizontalSpacing: CGFloat = 72
        let verticalSpacing: CGFloat = 64
        var row = 0
        var y: CGFloat = -verticalSpacing
        while y < size.height + verticalSpacing {
          let offset = row.isMultiple(of: 2) ? 0.0 : horizontalSpacing / 2
          var x = -horizontalSpacing + offset
          while x < size.width + horizontalSpacing {
            context.draw(
              symbol,
              in: CGRect(x: x, y: y, width: tileSize, height: tileSize)
            )
            x += horizontalSpacing
          }
          row += 1
          y += verticalSpacing
        }
      }
    }
    .ignoresSafeArea()
  }

  private var patternOpacity: Double {
    guard !reduceTransparency else {
      return 0.025
    }
    return colorScheme == .dark ? 0.1 : 0.075
  }
}

private struct KnotGlassPanel: ViewModifier {
  @Environment(\.accessibilityReduceTransparency) private var reduceTransparency

  let cornerRadius: CGFloat
  let interactive: Bool

  @ViewBuilder
  func body(content: Content) -> some View {
    if reduceTransparency {
      content
        .background(
          KnotStyle.secondaryBackground,
          in: RoundedRectangle(cornerRadius: cornerRadius, style: .continuous)
        )
        .overlay {
          RoundedRectangle(cornerRadius: cornerRadius, style: .continuous)
            .stroke(KnotStyle.separator.opacity(0.55), lineWidth: 0.5)
        }
    } else if #available(iOS 26, macOS 26, *) {
      if interactive {
        content.glassEffect(
          .regular.interactive(),
          in: .rect(cornerRadius: cornerRadius)
        )
      } else {
        content.glassEffect(.regular, in: .rect(cornerRadius: cornerRadius))
      }
    } else {
      content.background(
        .ultraThinMaterial,
        in: RoundedRectangle(cornerRadius: cornerRadius, style: .continuous)
      )
    }
  }
}

private struct KnotProminentButton: ViewModifier {
  @ViewBuilder
  func body(content: Content) -> some View {
    if #available(iOS 26, macOS 26, *) {
      content.buttonStyle(.glassProminent)
    } else {
      content.buttonStyle(.borderedProminent)
    }
  }
}

private struct KnotGlassButton: ViewModifier {
  @ViewBuilder
  func body(content: Content) -> some View {
    if #available(iOS 26, macOS 26, *) {
      content.buttonStyle(.glass)
    } else {
      content.buttonStyle(.bordered)
    }
  }
}

private struct KnotSettingsListStyle: ViewModifier {
  @ViewBuilder
  func body(content: Content) -> some View {
    #if os(iOS)
      content.listStyle(.insetGrouped)
    #elseif os(macOS)
      content.listStyle(.inset)
    #endif
  }
}

extension View {
  func knotGlassPanel(cornerRadius: CGFloat = KnotStyle.cornerRadius, interactive: Bool = false)
    -> some View
  {
    modifier(KnotGlassPanel(cornerRadius: cornerRadius, interactive: interactive))
  }

  func knotProminentButton() -> some View {
    modifier(KnotProminentButton())
  }

  func knotGlassButton() -> some View {
    modifier(KnotGlassButton())
  }

  func knotSettingsListStyle() -> some View {
    modifier(KnotSettingsListStyle())
  }
}
