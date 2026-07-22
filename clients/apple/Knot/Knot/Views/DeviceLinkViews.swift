import CryptoKit
import SwiftUI

#if os(iOS)
  import UIKit
  import Vision
  import VisionKit
#elseif os(macOS)
  import AppKit
#endif

struct DeviceLinkCreationView: View {
  @Bindable var coordinator: DeviceLinkCoordinator

  @Environment(\.dismiss) private var dismiss

  var body: some View {
    NavigationStack {
      ZStack {
        KnotStyle.groupedBackground.ignoresSafeArea()
        content
          .padding(24)
          .frame(maxWidth: 520)
      }
      .navigationTitle("Link This Device")
      .toolbar {
        ToolbarItem(placement: .cancellationAction) {
          Button("Cancel") {
            Task {
              await coordinator.cancel()
              dismiss()
            }
          }
        }
      }
    }
    .frame(minWidth: 340, minHeight: 520)
    .task {
      await coordinator.restore()
      if coordinator.phase == .idle, coordinator.pending == nil {
        await coordinator.create()
      }
    }
    .onChange(of: coordinator.phase) {
      if coordinator.phase == .linked {
        dismiss()
      }
    }
  }

  @ViewBuilder
  private var content: some View {
    switch coordinator.phase {
    case .creating:
      ProgressView("Creating a secure device link…")
        .padding(30)
    case .waiting:
      if let pending = coordinator.pending {
        pendingContent(pending)
      } else {
        failureContent("The device link is unavailable.")
      }
    case .claiming:
      VStack(spacing: 16) {
        ProgressView()
        Text("Authenticating encrypted approval…")
          .font(.headline)
        Text("Your account is installed only after the transfer passes verification.")
          .font(.caption)
          .foregroundStyle(.secondary)
          .multilineTextAlignment(.center)
      }
      .padding(30)
    case .idle:
      failureContent(coordinator.errorMessage ?? "Create a link to an existing Knot account.")
    case .linked:
      VStack(spacing: 14) {
        Image(systemName: "checkmark.shield.fill")
          .font(.system(size: 52))
          .foregroundStyle(.green)
        Text("Device Linked")
          .font(.title2.bold())
      }
    }
  }

  private func pendingContent(_ pending: PendingDeviceLink) -> some View {
    VStack(spacing: 18) {
      let url = try? pending.descriptor.url
      VStack(spacing: 5) {
        Text("Scan to Link This Device")
          .font(.title3.bold())
        Text("Open Devices on a signed-in Knot device and scan this code.")
          .font(.subheadline)
          .foregroundStyle(.secondary)
          .multilineTextAlignment(.center)
      }

      if let url {
        QRCodeView(value: url.absoluteString)
          .frame(width: 242, height: 242)
          .padding(18)
          .background(.white, in: RoundedRectangle(cornerRadius: 24, style: .continuous))
          .shadow(color: .black.opacity(0.1), radius: 20, y: 8)

        HStack(spacing: 12) {
          ShareLink(item: url) {
            Label("Share", systemImage: "square.and.arrow.up")
          }
          Button("Copy", systemImage: "doc.on.doc") {
            copy(url)
          }
        }
        .knotGlassButton()
      }

      Label {
        HStack(spacing: 4) {
          Text("Expires in")
          Text(pending.creation.expiresAt, style: .timer)
            .monospacedDigit()
        }
      } icon: {
        Image(systemName: "timer")
      }
      .font(.caption)
      .foregroundStyle(.secondary)

      HStack(spacing: 8) {
        ProgressView()
          .controlSize(.small)
        Text("Waiting for encrypted approval")
          .font(.caption)
          .foregroundStyle(.secondary)
      }
    }
  }

  private func failureContent(_ message: String) -> some View {
    VStack(spacing: 16) {
      Image(systemName: "link.badge.plus")
        .font(.system(size: 52))
        .foregroundStyle(.white)
        .frame(width: 92, height: 92)
        .background(KnotStyle.accent, in: Circle())
      Text(message)
        .multilineTextAlignment(.center)
        .foregroundStyle(.secondary)
      Button("Create Device Link") {
        Task { await coordinator.create() }
      }
      .controlSize(.large)
      .knotProminentButton()
    }
    .padding(28)
  }

  private func copy(_ url: URL) {
    #if os(iOS)
      UIPasteboard.general.url = url
    #elseif os(macOS)
      NSPasteboard.general.clearContents()
      NSPasteboard.general.setString(url.absoluteString, forType: .string)
    #endif
  }
}

struct DeviceLinkEntryView: View {
  let onLink: (URL) -> Void

  @Environment(\.dismiss) private var dismiss
  @State private var pastedLink = ""
  @State private var errorMessage: String?
  #if os(iOS)
    @State private var isScanning = false
  #endif

  var body: some View {
    NavigationStack {
      Form {
        Section {
          VStack(spacing: 10) {
            Image(systemName: "qrcode.viewfinder")
              .font(.system(size: 44))
              .foregroundStyle(KnotStyle.accent)
            Text("Scan the QR code shown by the new device or paste its Knot link.")
              .font(.subheadline)
              .foregroundStyle(.secondary)
              .multilineTextAlignment(.center)
          }
          .frame(maxWidth: .infinity)
          .padding(.vertical, 12)

          #if os(iOS)
            Button("Scan QR Code", systemImage: "qrcode.viewfinder") {
              isScanning = true
            }
            .disabled(!DeviceLinkScannerView.isAvailable)
          #endif
        }

        Section("Manual Link") {
          TextField("knot://device-link?…", text: $pastedLink, axis: .vertical)
            .autocorrectionDisabled()
            .lineLimit(2...5)
          Button("Continue") {
            accept(pastedLink)
          }
          .disabled(pastedLink.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        }

        if let errorMessage {
          Section {
            Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
              .foregroundStyle(.red)
          }
        }
      }
      .navigationTitle("Scan Device Link")
      .toolbar {
        ToolbarItem(placement: .cancellationAction) {
          Button("Cancel") { dismiss() }
        }
      }
    }
    .frame(minWidth: 360, minHeight: 360)
    #if os(iOS)
      .sheet(isPresented: $isScanning) {
        NavigationStack {
          DeviceLinkScannerView { value in
            isScanning = false
            accept(value)
          }
          .ignoresSafeArea()
          .navigationTitle("Scan QR Code")
          .toolbar {
            ToolbarItem(placement: .cancellationAction) {
              Button("Cancel") { isScanning = false }
            }
          }
        }
      }
    #endif
  }

  private func accept(_ value: String) {
    let normalized = value.trimmingCharacters(in: .whitespacesAndNewlines)
    guard let url = URL(string: normalized) else {
      errorMessage = DeviceLinkError.invalidLink.localizedDescription
      return
    }
    do {
      _ = try DeviceLinkDescriptor(url: url)
      onLink(url)
      dismiss()
    } catch {
      errorMessage = error.localizedDescription
    }
  }
}

struct DeviceLinkApprovalView: View {
  @Bindable var store: AppStore
  let link: DeviceLinkDescriptor

  @Environment(\.dismiss) private var dismiss

  var body: some View {
    NavigationStack {
      ZStack {
        KnotStyle.groupedBackground.ignoresSafeArea()
        VStack(spacing: 20) {
          Image(
            systemName: store.deviceLink.approvalSucceeded
              ? "checkmark.shield.fill" : "iphone.gen3.radiowaves.left.and.right"
          )
          .font(.system(size: 46))
          .foregroundStyle(.white)
          .frame(width: 92, height: 92)
          .background(
            store.deviceLink.approvalSucceeded ? Color.green : KnotStyle.accent,
            in: Circle()
          )

          Text(store.deviceLink.approvalSucceeded ? "Device Approved" : "Approve New Device")
            .font(.title2.bold())

          if store.deviceLink.approvalSucceeded {
            Text("The new device can now authenticate the encrypted transfer.")
              .foregroundStyle(.secondary)
              .multilineTextAlignment(.center)
          } else {
            Text("Only approve if you initiated this link on the device in front of you.")
              .foregroundStyle(.secondary)
              .multilineTextAlignment(.center)

            VStack(alignment: .leading, spacing: 8) {
              Text("Link fingerprint")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
              Text(fingerprint)
                .font(.caption.monospaced())
                .textSelection(.enabled)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(16)
            .background(
              KnotStyle.primaryBackground,
              in: RoundedRectangle(cornerRadius: KnotStyle.compactCornerRadius)
            )
          }

          if let error = store.deviceLink.approvalError {
            Label(error, systemImage: "exclamationmark.triangle.fill")
              .font(.footnote)
              .foregroundStyle(.red)
          }

          Button(store.deviceLink.approvalSucceeded ? "Done" : "Approve Device") {
            if store.deviceLink.approvalSucceeded {
              store.deviceLink.dismissApproval()
              dismiss()
            } else {
              Task { await store.approveDeviceLink() }
            }
          }
          .controlSize(.large)
          .frame(maxWidth: .infinity)
          .knotProminentButton()
          .disabled(store.deviceLink.isApproving)
        }
        .padding(28)
        .frame(maxWidth: 440)
        .padding(24)
      }
      .navigationTitle("Device Link")
      .toolbar {
        ToolbarItem(placement: .cancellationAction) {
          Button("Cancel") {
            store.deviceLink.dismissApproval()
            dismiss()
          }
        }
      }
    }
    .frame(minWidth: 360, minHeight: 420)
  }

  private var fingerprint: String {
    let digest = SHA256.hash(data: link.linkingPublicKey)
    return digest.prefix(10).map { String(format: "%02X", $0) }.joined(separator: " ")
  }
}

#if os(iOS)
  struct DeviceLinkScannerView: UIViewControllerRepresentable {
    static var isAvailable: Bool {
      DataScannerViewController.isSupported && DataScannerViewController.isAvailable
    }

    let onScan: (String) -> Void

    func makeCoordinator() -> Coordinator {
      Coordinator(onScan: onScan)
    }

    func makeUIViewController(context: Context) -> DataScannerViewController {
      let controller = DataScannerViewController(
        recognizedDataTypes: [.barcode(symbologies: [.qr])],
        qualityLevel: .balanced,
        recognizesMultipleItems: false,
        isHighFrameRateTrackingEnabled: false,
        isPinchToZoomEnabled: true,
        isGuidanceEnabled: true,
        isHighlightingEnabled: true
      )
      controller.delegate = context.coordinator
      return controller
    }

    func updateUIViewController(_ controller: DataScannerViewController, context: Context) {
      if !controller.isScanning {
        try? controller.startScanning()
      }
    }

    final class Coordinator: NSObject, DataScannerViewControllerDelegate {
      private let onScan: (String) -> Void
      private var hasScanned = false

      init(onScan: @escaping (String) -> Void) {
        self.onScan = onScan
      }

      func dataScanner(
        _ dataScanner: DataScannerViewController,
        didAdd addedItems: [RecognizedItem],
        allItems: [RecognizedItem]
      ) {
        guard !hasScanned else {
          return
        }
        for item in addedItems {
          guard case .barcode(let barcode) = item,
            let payload = barcode.payloadStringValue
          else {
            continue
          }
          hasScanned = true
          dataScanner.stopScanning()
          onScan(payload)
          return
        }
      }
    }
  }
#endif
