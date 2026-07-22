import SwiftUI
import UniformTypeIdentifiers

struct ConversationView: View {
  let store: AppStore
  let conversationID: String

  @State private var draft = ""
  @State private var isFileImporterPresented = false
  @FocusState private var isComposerFocused: Bool

  var body: some View {
    Group {
      if let conversation = store.conversation(id: conversationID) {
        conversationContent(conversation)
      } else {
        ContentUnavailableView("Conversation unavailable", systemImage: "message.slash")
      }
    }
  }

  private func conversationContent(_ conversation: Conversation) -> some View {
    ZStack {
      KnotChatBackground()

      ScrollViewReader { proxy in
        ScrollView {
          LazyVStack(spacing: 0) {
            encryptionNotice

            ForEach(MessagePresentationBuilder.sections(messages: conversation.messages)) {
              section in
              DateChip(date: section.day)
                .padding(.vertical, 12)

              ForEach(section.messages) { presented in
                MessageBubble(
                  message: presented.message,
                  position: presented.position,
                  onDownload: {
                    Task {
                      await store.downloadAttachment(
                        messageID: presented.message.id,
                        conversationID: conversationID
                      )
                    }
                  },
                  onRetry: {
                    store.retryMessage(id: presented.message.id)
                  }
                )
                .id(presented.id)
                .padding(.top, messageSpacing(for: presented.position))
              }
            }
          }
          .padding(.horizontal, 10)
          .padding(.vertical, 12)
          .frame(maxWidth: KnotStyle.contentWidth)
          .frame(maxWidth: .infinity)
        }
        .scrollDismissesKeyboard(.interactively)
        .defaultScrollAnchor(.bottom)
        .onChange(of: conversation.messages.count) {
          if let lastMessage = conversation.messages.last {
            withAnimation(.easeOut(duration: 0.2)) {
              proxy.scrollTo(lastMessage.id, anchor: .bottom)
            }
          }
        }
      }
    }
    .navigationTitle(conversation.username)
    #if os(iOS)
      .navigationBarTitleDisplayMode(.inline)
    #endif
    .safeAreaInset(edge: .bottom, spacing: 0) {
      composer
    }
    .toolbar {
      ToolbarItem(placement: .principal) {
        ConversationHeader(username: conversation.username)
      }
    }
    .fileImporter(
      isPresented: $isFileImporterPresented,
      allowedContentTypes: [.item],
      allowsMultipleSelection: false
    ) { result in
      switch result {
      case .success(let urls):
        guard let fileURL = urls.first else {
          return
        }
        Task {
          await store.sendAttachment(fileURL: fileURL, conversationID: conversationID)
        }
      case .failure(let error):
        store.reportFileSelectionError(error)
      }
    }
  }

  private var encryptionNotice: some View {
    Label("Messages are end-to-end encrypted", systemImage: "lock.fill")
      .font(.caption.weight(.medium))
      .foregroundStyle(.secondary)
      .padding(.horizontal, 12)
      .padding(.vertical, 7)
      .knotGlassPanel(cornerRadius: 13)
      .padding(.bottom, 4)
      .accessibilityLabel("Messages are end-to-end encrypted on your devices")
  }

  private var composer: some View {
    VStack(spacing: 7) {
      if let error = store.messagingError {
        HStack(spacing: 8) {
          Image(systemName: "exclamationmark.triangle.fill")
          Text(error)
            .lineLimit(2)
          Spacer()
          Button("Dismiss", systemImage: "xmark") {
            store.clearMessagingError()
          }
          .labelStyle(.iconOnly)
        }
        .font(.caption)
        .foregroundStyle(.red)
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .knotGlassPanel(cornerRadius: 13)
      }

      composerSurface
    }
    .padding(.horizontal, 8)
    .padding(.top, 5)
    .padding(.bottom, 6)
    .frame(maxWidth: KnotStyle.contentWidth)
    .frame(maxWidth: .infinity)
  }

  @ViewBuilder
  private var composerSurface: some View {
    if #available(iOS 26, macOS 26, *) {
      GlassEffectContainer(spacing: 8) {
        composerControls
      }
    } else {
      composerControls
    }
  }

  private var composerControls: some View {
    HStack(alignment: .bottom, spacing: 8) {
      Button("Attach File", systemImage: "paperclip") {
        isFileImporterPresented = true
      }
      .labelStyle(.iconOnly)
      .font(.headline)
      .frame(width: 42, height: 42)
      .knotGlassButton()
      .disabled(store.isSending(conversationID: conversationID))
      .accessibilityIdentifier("attach-file")

      TextField("Message", text: $draft, axis: .vertical)
        .textFieldStyle(.plain)
        .lineLimit(1...6)
        .focused($isComposerFocused)
        .submitLabel(.send)
        .onSubmit(send)
        .padding(.horizontal, 14)
        .padding(.vertical, 11)
        .frame(minHeight: 42)
        .knotGlassPanel(cornerRadius: 21, interactive: true)

      Button("Send", systemImage: "arrow.up") {
        send()
      }
      .labelStyle(.iconOnly)
      .font(.headline.weight(.bold))
      .frame(width: 42, height: 42)
      .knotProminentButton()
      .disabled(normalizedDraft.isEmpty || store.isSending(conversationID: conversationID))
      .accessibilityIdentifier("send-message")
    }
  }

  private var normalizedDraft: String {
    draft.trimmingCharacters(in: .whitespacesAndNewlines)
  }

  private func messageSpacing(for position: MessageGroupPosition) -> CGFloat {
    position == .single || position == .first ? 7 : 2
  }

  private func send() {
    let body = normalizedDraft
    guard !body.isEmpty, !store.isSending(conversationID: conversationID) else {
      return
    }
    draft = ""
    Task {
      await store.send(body: body, conversationID: conversationID)
    }
  }
}

private struct ConversationHeader: View {
  let username: String

  var body: some View {
    HStack(spacing: 8) {
      AvatarView(username: username, size: 32)

      VStack(alignment: .leading, spacing: 1) {
        Text(username)
          .font(.subheadline.weight(.semibold))
          .lineLimit(1)
        Label("End-to-end encrypted", systemImage: "lock.fill")
          .font(.caption2)
          .foregroundStyle(.secondary)
          .lineLimit(1)
      }
    }
    .accessibilityElement(children: .combine)
  }
}

private struct DateChip: View {
  let date: Date

  var body: some View {
    Text(title)
      .font(.caption2.weight(.semibold))
      .foregroundStyle(.secondary)
      .padding(.horizontal, 10)
      .padding(.vertical, 5)
      .knotGlassPanel(cornerRadius: 11)
  }

  private var title: String {
    let calendar = Calendar.autoupdatingCurrent
    if calendar.isDateInToday(date) {
      return "Today"
    }
    if calendar.isDateInYesterday(date) {
      return "Yesterday"
    }
    return date.formatted(.dateTime.weekday(.abbreviated).month(.abbreviated).day())
  }
}
