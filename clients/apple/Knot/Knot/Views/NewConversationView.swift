import SwiftUI

struct NewConversationView: View {
  let store: AppStore
  let onConversationCreated: (String) -> Void

  @Environment(\.dismiss) private var dismiss
  @State private var username = ""
  @FocusState private var isFocused: Bool

  init(store: AppStore, onConversationCreated: @escaping (String) -> Void = { _ in }) {
    self.store = store
    self.onConversationCreated = onConversationCreated
  }

  var body: some View {
    NavigationStack {
      ZStack {
        KnotStyle.groupedBackground.ignoresSafeArea()

        VStack(spacing: 22) {
          AvatarView(username: username.isEmpty ? "?" : username, size: 76)

          VStack(spacing: 6) {
            Text("Start a Private Chat")
              .font(.title3.bold())
            Text("Enter the Knot username you want to message.")
              .font(.subheadline)
              .foregroundStyle(.secondary)
              .multilineTextAlignment(.center)
          }

          TextField("Username", text: $username)
            .textContentType(.username)
            .autocorrectionDisabled()
            .textFieldStyle(.plain)
            .focused($isFocused)
            .submitLabel(.go)
            .onSubmit(start)
            .padding(.horizontal, 16)
            .padding(.vertical, 14)
            .background(
              KnotStyle.primaryBackground,
              in: RoundedRectangle(cornerRadius: KnotStyle.compactCornerRadius)
            )

          SecureStatusLabel()

          Button("Start Chat", systemImage: "message.fill", action: start)
            .frame(maxWidth: .infinity)
            .controlSize(.large)
            .knotProminentButton()
            .disabled(!isValid)
        }
        .padding(24)
        .frame(maxWidth: 420)
      }
      .navigationTitle("New Message")
      .toolbar {
        ToolbarItem(placement: .cancellationAction) {
          Button("Cancel") { dismiss() }
        }
      }
      .task {
        isFocused = true
      }
    }
    .presentationDetents([.medium])
  }

  private var normalizedUsername: String {
    username.trimmingCharacters(in: .whitespacesAndNewlines)
  }

  private var isValid: Bool {
    normalizedUsername.count >= 3
      && normalizedUsername.caseInsensitiveCompare(store.session?.username ?? "") != .orderedSame
  }

  private func start() {
    guard isValid else {
      return
    }
    let conversationID = store.addConversation(username: normalizedUsername)
    onConversationCreated(conversationID)
    dismiss()
  }
}
