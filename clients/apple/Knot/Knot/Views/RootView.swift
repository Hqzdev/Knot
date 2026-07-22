import SwiftUI

struct RootView: View {
  @Bindable var store: AppStore

  var body: some View {
    Group {
      switch store.phase {
      case .restoring:
        LaunchView()
      case .signedOut:
        AuthenticationView(store: store)
      case .signedIn:
        ConversationContainer(store: store)
      }
    }
    .task {
      await store.restore()
    }
    .sheet(
      item: Binding(
        get: { store.deviceLink.approvalCandidate },
        set: { candidate in
          if candidate == nil {
            store.deviceLink.dismissApproval()
          }
        }
      )
    ) { link in
      DeviceLinkApprovalView(store: store, link: link)
    }
  }
}

private struct LaunchView: View {
  var body: some View {
    ZStack {
      KnotBackground()
      VStack(spacing: 20) {
        Image(systemName: "point.3.connected.trianglepath.dotted")
          .font(.system(size: 40, weight: .medium))
          .foregroundStyle(.white)
          .frame(width: 88, height: 88)
          .background(KnotStyle.accent, in: Circle())
          .shadow(color: KnotStyle.accent.opacity(0.25), radius: 16, y: 8)
        ProgressView()
      }
    }
  }
}
