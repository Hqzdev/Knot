import SwiftUI

@main
struct KnotApp: App {
  #if os(iOS)
    @UIApplicationDelegateAdaptor(KnotIOSAppDelegate.self) private var appDelegate
  #elseif os(macOS)
    @NSApplicationDelegateAdaptor(KnotMacAppDelegate.self) private var appDelegate
  #endif

  @Environment(\.scenePhase) private var scenePhase
  @State private var store: AppStore

  init() {
    let configuration = AppConfiguration.live()
    let keychain = KeychainStore(service: "knot.Knot")
    let api = APIClient(baseURL: configuration.apiBaseURL)
    let crypto = CryptoService(keychain: keychain)
    let messageStore = EncryptedMessageStore(
      keyProvider: KeychainDatabaseKeyProvider(keychain: keychain)
    )
    let gateway = GatewayClient(baseURL: configuration.gatewayBaseURL)
    let attachments = AttachmentService(
      baseURL: configuration.attachmentsBaseURL,
      maximumCiphertextSize: configuration.maximumAttachmentCiphertextSize
    )
    let messagePipeline = MessagePipeline(
      api: api,
      crypto: crypto,
      store: messageStore,
      maximumAttachmentCiphertextSize: configuration.maximumAttachmentCiphertextSize
    )
    let authenticationStore = AuthenticationStore(keychain: keychain)
    let deviceProfile = LocalDeviceProfile.current()
    let appStore = AppStore(
      api: api,
      crypto: crypto,
      gateway: gateway,
      messagePipeline: messagePipeline,
      attachments: attachments,
      pushRegistration: PushRegistrationService(baseURL: configuration.pushBaseURL),
      deviceLinkService: DeviceLinkService(
        api: api,
        crypto: crypto,
        keychain: keychain,
        authenticationStore: authenticationStore,
        deviceProfile: deviceProfile,
        historyArchives: HistoryArchiveService(
          store: messageStore,
          attachments: attachments
        )
      ),
      authenticationStore: authenticationStore,
      deviceProfile: deviceProfile
    )
    _store = State(initialValue: appStore)
  }

  var body: some Scene {
    #if os(macOS)
      WindowGroup {
        rootView
          .frame(minWidth: 760, minHeight: 560)
      }
      .defaultSize(width: 1120, height: 720)
      .commands {
        MacConversationCommands()
      }

      Settings {
        MacSettingsView(store: store)
      }
    #else
      WindowGroup {
        rootView
          .frame(minWidth: 340, minHeight: 520)
      }
    #endif
  }

  private var rootView: some View {
    RootView(store: store)
      .onOpenURL { url in
        store.handleDeepLink(url)
      }
      .task {
        store.bindPushNotifications(appDelegate.pushBroker)
      }
      .onChange(of: scenePhase) {
        if scenePhase == .active {
          store.becameActive()
        }
      }
  }
}
