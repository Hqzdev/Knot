import SwiftUI

struct SettingsView: View {
  @Bindable var store: AppStore

  var body: some View {
    List {
      profileSection

      Section("Privacy and Security") {
        Label {
          VStack(alignment: .leading, spacing: 3) {
            Text("End-to-End Encryption")
            Text("Messages are encrypted on your devices")
              .font(.caption)
              .foregroundStyle(.secondary)
          }
        } icon: {
          SettingsIcon(symbol: "lock.shield.fill", color: .green)
        }

        NavigationLink(value: SettingsRoute.devices) {
          Label {
            HStack {
              Text("Devices")
              Spacer()
              if !store.devices.isEmpty {
                Text("\(activeDeviceCount)")
                  .foregroundStyle(.secondary)
              }
            }
          } icon: {
            SettingsIcon(symbol: "laptopcomputer.and.iphone", color: KnotStyle.accent)
          }
        }
      }

      Section {
        Button("Sign Out", systemImage: "rectangle.portrait.and.arrow.right", role: .destructive) {
          store.signOut()
        }
      }
    }
    .knotSettingsListStyle()
    .navigationTitle("Settings")
    .task {
      if store.devices.isEmpty {
        await store.loadDevices()
      }
    }
  }

  private var profileSection: some View {
    Section {
      VStack(spacing: 12) {
        AvatarView(username: username, size: 92)

        VStack(spacing: 4) {
          Text(username)
            .font(.title2.bold())
          if let email = store.session?.email {
            Text(email)
              .font(.subheadline)
              .foregroundStyle(.secondary)
          }
          Text("@\(username)")
            .font(.subheadline)
            .foregroundStyle(.secondary)
        }

        Label("End-to-end encrypted", systemImage: "checkmark.shield.fill")
          .font(.caption.weight(.medium))
          .foregroundStyle(KnotStyle.accent)
      }
      .frame(maxWidth: .infinity)
      .padding(.vertical, 14)
      .accessibilityElement(children: .combine)
    }
  }

  private var username: String {
    store.session?.username ?? "Knot User"
  }

  private var activeDeviceCount: Int {
    store.devices.filter { $0.revokedAt == nil }.count
  }
}

private struct SettingsIcon: View {
  let symbol: String
  let color: Color

  var body: some View {
    Image(systemName: symbol)
      .font(.caption.weight(.bold))
      .foregroundStyle(.white)
      .frame(width: 28, height: 28)
      .background(color, in: RoundedRectangle(cornerRadius: 7, style: .continuous))
      .accessibilityHidden(true)
  }
}
