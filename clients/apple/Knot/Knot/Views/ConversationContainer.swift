import SwiftUI

enum AppTab: Hashable {
  case chats
  case settings
}

enum ChatRoute: Hashable {
  case conversation(String)
}

enum SettingsRoute: Hashable {
  case devices
}

struct ConversationContainer: View {
  @Bindable var store: AppStore
  @State private var selectedTab = AppTab.chats
  @State private var chatPath: [ChatRoute] = []
  @State private var settingsPath: [SettingsRoute] = []
  @State private var presentedSheet: SheetDestination?

  #if os(iOS)
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
  #endif

  var body: some View {
    content
      .tint(KnotStyle.accent)
  }

  @ViewBuilder
  private var content: some View {
    #if os(iOS)
      if horizontalSizeClass == .compact {
        compactContent
      } else {
        splitContent
      }
    #else
      MacConversationContainer(store: store)
    #endif
  }

  #if os(iOS)
    private var compactContent: some View {
      TabView(selection: $selectedTab) {
        NavigationStack(path: $chatPath) {
          ConversationList(
            store: store,
            navigatesToConversations: true,
            onCompose: { presentedSheet = .newConversation }
          )
          .navigationDestination(for: ChatRoute.self) { route in
            switch route {
            case .conversation(let conversationID):
              ConversationView(store: store, conversationID: conversationID)
                .toolbar(.hidden, for: .tabBar)
                .onAppear {
                  store.selectedConversationID = conversationID
                }
                .onDisappear {
                  if store.selectedConversationID == conversationID {
                    store.selectedConversationID = nil
                  }
                }
            }
          }
        }
        .tabItem {
          Label("Chats", systemImage: "message.fill")
        }
        .tag(AppTab.chats)

        NavigationStack(path: $settingsPath) {
          SettingsView(store: store)
            .navigationDestination(for: SettingsRoute.self) { route in
              switch route {
              case .devices:
                DeviceListView(store: store)
              }
            }
        }
        .tabItem {
          Label("Settings", systemImage: "gearshape.fill")
        }
        .tag(AppTab.settings)
      }
      .sheet(item: $presentedSheet) { destination in
        switch destination {
        case .newConversation:
          NewConversationView(store: store) { conversationID in
            selectedTab = .chats
            chatPath.append(.conversation(conversationID))
          }
        case .devices:
          NavigationStack {
            DeviceListView(store: store, showsDismissButton: true)
          }
        }
      }
    }
  #endif

  #if os(iOS)
    private var splitContent: some View {
      NavigationSplitView {
        ConversationList(
          store: store,
          navigatesToConversations: false,
          onCompose: { presentedSheet = .newConversation }
        )
        .toolbar {
          ToolbarItem(placement: .primaryAction) {
            Menu("Account", systemImage: "ellipsis.circle") {
              Button("Devices", systemImage: "laptopcomputer.and.iphone") {
                presentedSheet = .devices
              }
              Divider()
              Button("Sign Out", systemImage: "rectangle.portrait.and.arrow.right") {
                store.signOut()
              }
            }
          }
        }
      } detail: {
        if let conversationID = store.selectedConversationID {
          ConversationView(store: store, conversationID: conversationID)
        } else {
          ContentUnavailableView(
            "Choose a conversation",
            systemImage: "message.fill",
            description: Text("Your end-to-end encrypted messages appear here.")
          )
        }
      }
      .sheet(item: $presentedSheet) { destination in
        switch destination {
        case .newConversation:
          NewConversationView(store: store)
        case .devices:
          NavigationStack {
            DeviceListView(store: store, showsDismissButton: true)
          }
        }
      }
    }
  #endif
}

private enum SheetDestination: String, Identifiable {
  case newConversation
  case devices

  var id: Self { self }
}
