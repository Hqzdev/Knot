#if os(macOS)
  import SwiftUI

  struct MacConversationContainer: View {
    @Bindable var store: AppStore

    @State private var search = ""
    @State private var isSearchPresented = false
    @State private var presentedSheet: MacSheetDestination?
    @State private var isInspectorPresented = false

    var body: some View {
      NavigationSplitView {
        sidebar
          .navigationSplitViewColumnWidth(min: 260, ideal: 320, max: 420)
      } detail: {
        detail
      }
      .navigationTitle("Knot")
      .searchable(
        text: $search,
        isPresented: $isSearchPresented,
        placement: .sidebar,
        prompt: "Search chats"
      )
      .toolbar { toolbar }
      .inspector(isPresented: $isInspectorPresented) {
        if let conversation = selectedConversation {
          MacConversationInspector(conversation: conversation)
            .inspectorColumnWidth(min: 260, ideal: 300, max: 360)
        }
      }
      .focusedSceneValue(
        \.macConversationActions,
        MacConversationActions(
          compose: presentComposer,
          focusSearch: focusSearch,
          toggleInspector: toggleInspector
        )
      )
      .sheet(item: $presentedSheet) { destination in
        switch destination {
        case .newConversation:
          NewConversationView(store: store) { conversationID in
            store.selectedConversationID = conversationID
          }
          .frame(minWidth: 460, minHeight: 360)
        case .devices:
          NavigationStack {
            DeviceListView(store: store, showsDismissButton: true)
          }
          .frame(minWidth: 520, minHeight: 460)
        }
      }
      .onChange(of: store.selectedConversationID) {
        if store.selectedConversationID == nil {
          isInspectorPresented = false
        }
      }
    }

    private var sidebar: some View {
      List(selection: $store.selectedConversationID) {
        ForEach(filteredConversations) { conversation in
          MacConversationRow(conversation: conversation)
            .tag(conversation.id)
        }
      }
      .listStyle(.sidebar)
      .navigationTitle("Chats")
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
    private var detail: some View {
      if let conversationID = store.selectedConversationID {
        ConversationView(store: store, conversationID: conversationID)
      } else {
        MacConversationEmptyView(onCompose: presentComposer)
      }
    }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
      ToolbarItem(placement: .navigation) {
        Menu {
          if let session = store.session {
            Text("@\(session.username)")
            if let email = session.email {
              Text(email)
            }
            Divider()
          }

          SettingsLink {
            Label("Settings", systemImage: "gear")
          }

          Button("Devices", systemImage: "laptopcomputer.and.iphone") {
            presentedSheet = .devices
          }

          Divider()

          Button(
            "Sign Out",
            systemImage: "rectangle.portrait.and.arrow.right",
            role: .destructive
          ) {
            store.signOut()
          }
        } label: {
          AvatarView(username: store.session?.username ?? "K", size: 28)
        }
        .menuIndicator(.hidden)
        .help("Account")
      }

      ToolbarItem(placement: .primaryAction) {
        Button("New conversation", systemImage: "square.and.pencil", action: presentComposer)
          .help("New Conversation")
          .accessibilityIdentifier("new-conversation")
      }

      ToolbarItem(placement: .secondaryAction) {
        Button(
          isInspectorPresented ? "Hide Profile" : "Show Profile",
          systemImage: "info.circle",
          action: toggleInspector
        )
        .disabled(selectedConversation == nil)
        .help(isInspectorPresented ? "Hide Profile" : "Show Profile")
      }
    }

    private var filteredConversations: [Conversation] {
      let query = search.trimmingCharacters(in: .whitespacesAndNewlines)
      guard !query.isEmpty else {
        return store.conversations
      }
      return store.conversations.filter {
        $0.username.localizedCaseInsensitiveContains(query)
          || $0.lastMessage?.body.localizedCaseInsensitiveContains(query) == true
      }
    }

    private var selectedConversation: Conversation? {
      guard let selectedConversationID = store.selectedConversationID else {
        return nil
      }
      return store.conversation(id: selectedConversationID)
    }

    private func presentComposer() {
      presentedSheet = .newConversation
    }

    private func focusSearch() {
      isSearchPresented = true
    }

    private func toggleInspector() {
      guard selectedConversation != nil else {
        return
      }
      isInspectorPresented.toggle()
    }
  }

  private struct MacConversationRow: View {
    let conversation: Conversation

    var body: some View {
      HStack(spacing: 11) {
        AvatarView(username: conversation.username, size: 42)

        VStack(alignment: .leading, spacing: 4) {
          HStack(alignment: .firstTextBaseline, spacing: 6) {
            Text(conversation.username)
              .font(.body.weight(.semibold))
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
                .padding(.horizontal, 6)
                .frame(minWidth: 20, minHeight: 20)
                .background(KnotStyle.accent, in: Capsule())
            }
          }
        }
      }
      .padding(.vertical, 4)
      .contentShape(Rectangle())
      .accessibilityElement(children: .combine)
    }

    @ViewBuilder
    private var preview: some View {
      if let message = conversation.lastMessage {
        if message.attachment != nil || message.attachmentTransferState != nil {
          Label(message.attachment?.filename ?? message.body, systemImage: "paperclip")
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

  private struct MacConversationEmptyView: View {
    let onCompose: () -> Void

    var body: some View {
      ContentUnavailableView {
        Label("Choose a Conversation", systemImage: "message.fill")
      } description: {
        Text("Your end-to-end encrypted messages appear here.")
      } actions: {
        Button("New Conversation", systemImage: "square.and.pencil", action: onCompose)
          .buttonStyle(.borderedProminent)
      }
    }
  }

  private struct MacConversationInspector: View {
    let conversation: Conversation

    var body: some View {
      ScrollView {
        VStack(spacing: 22) {
          VStack(spacing: 10) {
            AvatarView(username: conversation.username, size: 88)
            Text(conversation.username)
              .font(.title2.bold())
            Text("@\(conversation.username)")
              .foregroundStyle(.secondary)
          }

          GroupBox("Privacy") {
            Label("End-to-end encrypted", systemImage: "lock.shield.fill")
              .foregroundStyle(KnotStyle.accent)
              .frame(maxWidth: .infinity, alignment: .leading)
              .padding(6)
          }

          GroupBox("Conversation") {
            VStack(spacing: 10) {
              LabeledContent("Messages", value: "\(conversation.messages.count)")
              Divider()
              LabeledContent("Attachments", value: "\(attachmentCount)")
              Divider()
              LabeledContent("Unread", value: "\(conversation.unreadCount)")
            }
            .padding(6)
          }
        }
        .padding(20)
      }
      .navigationTitle("Profile")
    }

    private var attachmentCount: Int {
      conversation.messages.filter {
        $0.attachment != nil || $0.attachmentTransferState != nil
      }.count
    }
  }

  struct MacConversationActions {
    let compose: () -> Void
    let focusSearch: () -> Void
    let toggleInspector: () -> Void
  }

  private struct MacConversationActionsKey: FocusedValueKey {
    typealias Value = MacConversationActions
  }

  extension FocusedValues {
    var macConversationActions: MacConversationActions? {
      get { self[MacConversationActionsKey.self] }
      set { self[MacConversationActionsKey.self] = newValue }
    }
  }

  struct MacConversationCommands: Commands {
    @FocusedValue(\.macConversationActions) private var actions

    var body: some Commands {
      CommandGroup(replacing: .newItem) {
        Button("New Conversation") {
          actions?.compose()
        }
        .keyboardShortcut("n", modifiers: .command)
        .disabled(actions == nil)
      }

      CommandMenu("Conversation") {
        Button("Find Chat") {
          actions?.focusSearch()
        }
        .keyboardShortcut("f", modifiers: .command)
        .disabled(actions == nil)

        Button("Show Profile") {
          actions?.toggleInspector()
        }
        .keyboardShortcut("i", modifiers: [.command, .option])
        .disabled(actions == nil)
      }
    }
  }

  private enum MacSheetDestination: String, Identifiable {
    case newConversation
    case devices

    var id: Self { self }
  }
#endif
