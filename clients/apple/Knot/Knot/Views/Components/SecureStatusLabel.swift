import SwiftUI

struct SecureStatusLabel: View {
  var body: some View {
    Label("End-to-end encrypted", systemImage: "lock.fill")
      .font(.caption)
      .foregroundStyle(.secondary)
      .accessibilityLabel("This conversation is end-to-end encrypted")
  }
}
