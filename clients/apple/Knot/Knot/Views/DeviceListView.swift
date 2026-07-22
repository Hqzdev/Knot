import SwiftUI

struct DeviceListView: View {
  @Bindable var store: AppStore
  var showsDismissButton = false

  @Environment(\.dismiss) private var dismiss
  @State private var deviceToRevoke: Device?
  @State private var isLinkEntryPresented = false

  var body: some View {
    Group {
      if store.isLoadingDevices && store.devices.isEmpty {
        ProgressView("Loading devices")
      } else if store.devices.isEmpty {
        ContentUnavailableView(
          "No Devices",
          systemImage: "laptopcomputer.and.iphone",
          description: Text("Linked devices will appear here.")
        )
      } else {
        List {
          Section {
            ForEach(activeDevices) { device in
              DeviceRow(device: device)
                .swipeActions {
                  if !device.isCurrent {
                    Button("Revoke", role: .destructive) {
                      deviceToRevoke = device
                    }
                  }
                }
            }
          } header: {
            Text("Active Devices")
          } footer: {
            Text("Each device holds its own encryption keys. Revoke anything you do not recognize.")
          }

          if !revokedDevices.isEmpty {
            Section("Revoked") {
              ForEach(revokedDevices) { device in
                DeviceRow(device: device)
              }
            }
          }
        }
        .knotSettingsListStyle()
      }
    }
    .navigationTitle("Devices")
    .toolbar {
      ToolbarItem(placement: .primaryAction) {
        Button("Link Device", systemImage: "qrcode.viewfinder") {
          isLinkEntryPresented = true
        }
      }
      if showsDismissButton {
        ToolbarItem(placement: .confirmationAction) {
          Button("Done") { dismiss() }
        }
      }
    }
    .sheet(isPresented: $isLinkEntryPresented) {
      DeviceLinkEntryView { url in
        isLinkEntryPresented = false
        Task { @MainActor in
          try? await Task.sleep(for: .milliseconds(300))
          store.handleDeepLink(url)
        }
      }
    }
    .task {
      await store.loadDevices()
    }
    .refreshable {
      await store.loadDevices()
    }
    .alert(item: $deviceToRevoke) { device in
      Alert(
        title: Text("Revoke \(device.name)?"),
        message: Text("This device will lose access to new messages."),
        primaryButton: .destructive(Text("Revoke")) {
          Task { await store.revokeDevice(id: device.id) }
        },
        secondaryButton: .cancel()
      )
    }
  }

  private var activeDevices: [Device] {
    store.devices.filter { $0.revokedAt == nil }
  }

  private var revokedDevices: [Device] {
    store.devices.filter { $0.revokedAt != nil }
  }
}

struct DeviceRow: View {
  let device: Device

  var body: some View {
    HStack(spacing: 14) {
      Image(systemName: symbol)
        .font(.title3)
        .foregroundStyle(.white)
        .frame(width: 42, height: 42)
        .background(iconColor, in: RoundedRectangle(cornerRadius: 11, style: .continuous))
        .accessibilityHidden(true)

      VStack(alignment: .leading, spacing: 4) {
        HStack(spacing: 7) {
          Text(device.name)
            .fontWeight(.semibold)
            .lineLimit(1)
          if device.isCurrent {
            Text("This Device")
              .font(.caption2.weight(.semibold))
              .foregroundStyle(KnotStyle.accent)
          }
        }

        Text(status)
          .font(.caption)
          .foregroundStyle(.secondary)
      }

      Spacer(minLength: 8)

      if device.revokedAt == nil {
        Circle()
          .fill(.green)
          .frame(width: 8, height: 8)
          .accessibilityLabel("Active")
      }
    }
    .padding(.vertical, 4)
    .accessibilityElement(children: .combine)
  }

  private var symbol: String {
    device.platform == DevicePlatform.macOS.rawValue ? "laptopcomputer" : "iphone"
  }

  private var iconColor: Color {
    device.revokedAt == nil ? KnotStyle.accent : .secondary
  }

  private var status: String {
    if let revokedAt = device.revokedAt {
      return "Revoked \(revokedAt.formatted(date: .abbreviated, time: .omitted))"
    }
    return "Added \(device.createdAt.formatted(date: .abbreviated, time: .omitted))"
  }
}
