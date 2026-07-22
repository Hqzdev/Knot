#if os(macOS)
  import SwiftUI

  struct MacSettingsView: View {
    @Bindable var store: AppStore

    var body: some View {
      TabView {
        MacAccountSettingsView(store: store)
          .tabItem {
            Label("Account", systemImage: "person.crop.circle")
          }

        MacSecuritySettingsView()
          .tabItem {
            Label("Security", systemImage: "lock.shield")
          }

        NavigationStack {
          DeviceListView(store: store)
        }
        .tabItem {
          Label("Devices", systemImage: "laptopcomputer.and.iphone")
        }
      }
      .frame(width: 560, height: 430)
    }
  }

  private struct MacAccountSettingsView: View {
    @Bindable var store: AppStore

    var body: some View {
      Group {
        if let session = store.session {
          VStack(spacing: 22) {
            VStack(spacing: 10) {
              AvatarView(username: session.username, size: 84)
              Text(session.username)
                .font(.title2.bold())
              Text("@\(session.username)")
                .foregroundStyle(.secondary)
            }

            Form {
              LabeledContent("Email", value: session.email ?? "Not available")
              LabeledContent("Username", value: session.username)
              LabeledContent("Device ID", value: session.deviceID)
            }
            .formStyle(.grouped)

            Button(
              "Sign Out",
              systemImage: "rectangle.portrait.and.arrow.right",
              role: .destructive
            ) {
              store.signOut()
            }
          }
          .padding(24)
        } else {
          ContentUnavailableView(
            "Not Signed In",
            systemImage: "person.crop.circle.badge.xmark",
            description: Text("Sign in from the main Knot window to manage your account.")
          )
        }
      }
    }
  }

  private struct MacSecuritySettingsView: View {
    var body: some View {
      Form {
        Section("Messages") {
          LabeledContent {
            Label("Enabled", systemImage: "checkmark.circle.fill")
              .foregroundStyle(.green)
          } label: {
            Text("End-to-end encryption")
          }

          LabeledContent("Key agreement", value: "X3DH")
          LabeledContent("Session encryption", value: "Double Ratchet")
        }

        Section("Local Data") {
          LabeledContent {
            Label("Encrypted", systemImage: "lock.fill")
              .foregroundStyle(KnotStyle.accent)
          } label: {
            Text("Message database")
          }

          LabeledContent("Database key", value: "Keychain")
          LabeledContent("Pending messages", value: "Durable outbox")
        }
      }
      .formStyle(.grouped)
      .padding(20)
    }
  }
#endif
