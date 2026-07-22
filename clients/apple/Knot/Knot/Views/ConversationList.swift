import SwiftUI

struct ConversationList: View {
  @Bindable var store: AppStore
  let navigatesToConversations: Bool
  let onCompose: () -> Void

  @State private var search = ""

  var body: some View {
    List {
      ForEach(filteredConversations) { conversation in
        conversationRow(conversation)
          .listRowInsets(
            EdgeInsets(top: 0, leading: 16, bottom: 0, trailing: 12)
          )
          .listRowSeparatorTint(KnotStyle.separator.opacity(0.65))
          .alignmentGuide(.listRowSeparatorLeading) { _ in
            KnotStyle.avatarSize + 28
          }
      }
    }
    .listStyle(.plain)
    .environment(\.defaultMinListRowHeight, KnotStyle.conversationRowHeight)
    .navigationTitle("Chats")
    .searchable(text: $search, prompt: "Search chats")
    .toolbar {
      ToolbarItem(placement: .primaryAction) {
        Button("New conversation", systemImage: "square.and.pencil", action: onCompose)
          .accessibilityIdentifier("new-conversation")
      }
    }
    .overlay {
      if filteredConversations.isEmpty {
        ContentUnavailableView(
          search.isEmpty ? "No Chats Yet" : "No Results",
          systemImage: search.isEmpty ? "message.badge" : "magnifyingglass",
          description: Text(
            search.isEmpty
              ? "Start a private conversation with the compose button."
              : "Try another username."
          )
        )
      }
    }
  }

  @ViewBuilder
  private func conversationRow(_ conversation: Conversation) -> some View {
    if navigatesToConversations {
      NavigationLink(value: ChatRoute.conversation(conversation.id)) {
        ConversationRow(conversation: conversation)
      }
    } else {
      Button {
        store.selectedConversationID = conversation.id
      } label: {
        ConversationRow(conversation: conversation)
      }
      .buttonStyle(.plain)
      .listRowBackground(
        store.selectedConversationID == conversation.id
          ? KnotStyle.accent.opacity(0.1)
          : KnotStyle.primaryBackground
      )
    }
  }

  private var filteredConversations: [Conversation] {
    let query = search.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !query.isEmpty else {
      return store.conversations
    }
    return store.conversations.filter { $0.username.localizedCaseInsensitiveContains(query) }
  }
}

struct ConversationRow: View {
  let conversation: Conversation

  var body: some View {
    HStack(spacing: 12) {
      AvatarView(username: conversation.username, size: KnotStyle.avatarSize)

      VStack(alignment: .leading, spacing: 5) {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
          Text(conversation.username)
            .font(.body.weight(.semibold))
            .foregroundStyle(.primary)
            .lineLimit(1)

          Spacer(minLength: 4)

          if let message = conversation.lastMessage {
            Text(timestamp(for: message.sentAt))
              .font(.caption)
              .foregroundStyle(conversation.unreadCount > 0 ? KnotStyle.accent : .secondary)
              .monospacedDigit()
          }
        }

        HStack(spacing: 5) {
          if let message = conversation.lastMessage, message.isOutgoing {
            DeliveryStateIcon(state: message.deliveryState)
          }

          preview

          Spacer(minLength: 4)

          if conversation.unreadCount > 0 {
            Text("\(conversation.unreadCount)")
              .font(.caption2.weight(.bold))
              .foregroundStyle(.white)
              .padding(.horizontal, 7)
              .frame(minWidth: 22, minHeight: 22)
              .background(KnotStyle.accent, in: Capsule())
              .accessibilityLabel("\(conversation.unreadCount) unread messages")
          }
        }
      }
    }
    .frame(minHeight: KnotStyle.conversationRowHeight)
    .contentShape(Rectangle())
    .accessibilityElement(children: .combine)
  }

  @ViewBuilder
  private var preview: some View {
    if let message = conversation.lastMessage {
      if message.attachment != nil || message.attachmentTransferState != nil {
        Label(message.attachment?.filename ?? message.body, systemImage: "paperclip")
          .labelStyle(.titleAndIcon)
          .font(.subheadline)
          .foregroundStyle(.secondary)
          .lineLimit(1)
      } else {
        Text(message.body)
          .font(.subheadline)
          .foregroundStyle(.secondary)
          .lineLimit(1)
      }
    } else {
      Text("Start a secure conversation")
        .font(.subheadline)
        .foregroundStyle(.secondary)
        .lineLimit(1)
    }
  }

  private func timestamp(for date: Date) -> String {
    let calendar = Calendar.autoupdatingCurrent
    if calendar.isDateInToday(date) {
      return date.formatted(date: .omitted, time: .shortened)
    }
    if calendar.isDateInYesterday(date) {
      return "Yesterday"
    }
    return date.formatted(.dateTime.month(.abbreviated).day())
  }
}

struct DeliveryStateIcon: View {
  let state: DeliveryState

  var body: some View {
    Image(systemName: symbol)
      .font(.caption2.weight(.semibold))
      .foregroundStyle(color)
      .accessibilityLabel(label)
  }

  private var symbol: String {
    switch state {
    case .sending:
      "clock"
    case .sent:
      "checkmark"
    case .failed:
      "exclamationmark.circle.fill"
    }
  }

  private var color: Color {
    state == .failed ? .red : KnotStyle.accent
  }

  private var label: String {
    switch state {
    case .sending:
      "Sending"
    case .sent:
      "Sent"
    case .failed:
      "Failed"
    }
  }
}
