import SwiftUI

struct MessageBubble: View {
  let message: ChatMessage
  let position: MessageGroupPosition
  let onDownload: () -> Void
  let onRetry: () -> Void

  init(
    message: ChatMessage,
    position: MessageGroupPosition = .single,
    onDownload: @escaping () -> Void = {},
    onRetry: @escaping () -> Void = {}
  ) {
    self.message = message
    self.position = position
    self.onDownload = onDownload
    self.onRetry = onRetry
  }

  var body: some View {
    HStack(alignment: .bottom, spacing: 8) {
      if message.isOutgoing {
        Spacer(minLength: 46)
      }

      VStack(alignment: .leading, spacing: 5) {
        if message.attachment != nil || message.attachmentTransferState != nil {
          attachmentContent
        } else {
          Text(message.body)
            .textSelection(.enabled)
            .foregroundStyle(message.isOutgoing ? .white : .primary)
            .fixedSize(horizontal: false, vertical: true)
        }

        HStack(spacing: 4) {
          Spacer(minLength: 8)
          Text(message.sentAt, style: .time)
          if message.isOutgoing {
            deliveryIcon
          }
        }
        .font(.caption2)
        .foregroundStyle(message.isOutgoing ? .white.opacity(0.78) : .secondary)
        .monospacedDigit()
      }
      .padding(.horizontal, 12)
      .padding(.vertical, 8)
      .frame(maxWidth: 340, alignment: .leading)
      .background(bubbleColor, in: bubbleShape)
      .overlay {
        if !message.isOutgoing {
          bubbleShape
            .stroke(KnotStyle.separator.opacity(0.32), lineWidth: 0.5)
        }
      }
      .shadow(color: .black.opacity(message.isOutgoing ? 0.04 : 0.025), radius: 1, y: 1)

      if !message.isOutgoing {
        Spacer(minLength: 46)
      }
    }
    .accessibilityElement(children: .combine)
  }

  private var attachmentContent: some View {
    VStack(alignment: .leading, spacing: 9) {
      HStack(spacing: 10) {
        Image(systemName: "doc.fill")
          .font(.headline)
          .foregroundStyle(message.isOutgoing ? KnotStyle.outgoing : .white)
          .frame(width: 38, height: 38)
          .background(
            message.isOutgoing ? Color.white : KnotStyle.accent,
            in: Circle()
          )

        VStack(alignment: .leading, spacing: 2) {
          Text(message.attachment?.filename ?? message.body)
            .font(.subheadline.weight(.semibold))
            .lineLimit(2)

          if let attachment = message.attachment {
            Text(
              ByteCountFormatter.string(
                fromByteCount: attachment.plaintextSize,
                countStyle: .file
              )
            )
            .font(.caption)
            .opacity(0.75)
          }
        }
      }

      transferContent
    }
    .foregroundStyle(message.isOutgoing ? .white : .primary)
    .frame(maxWidth: 270, alignment: .leading)
  }

  @ViewBuilder
  private var transferContent: some View {
    switch message.attachmentTransferState {
    case .encrypting(let progress):
      transferProgress(title: "Encrypting", progress: progress)
    case .uploading(let progress):
      transferProgress(title: "Uploading", progress: progress)
    case .sending:
      Label("Sending securely", systemImage: "lock.fill")
        .font(.caption)
    case .downloading(let progress):
      transferProgress(title: "Downloading", progress: progress)
    case .decrypting:
      Label("Authenticating and decrypting", systemImage: "lock.open.fill")
        .font(.caption)
    case .ready(let url):
      ShareLink(item: url) {
        Label("Open or Share", systemImage: "square.and.arrow.up")
          .font(.caption.weight(.semibold))
      }
      .buttonStyle(.plain)
    case .failed(let error):
      VStack(alignment: .leading, spacing: 6) {
        Label(error, systemImage: "exclamationmark.triangle.fill")
          .font(.caption)
          .lineLimit(3)
        if message.attachment != nil {
          Button("Try Again", action: onDownload)
            .font(.caption.weight(.semibold))
            .buttonStyle(.plain)
        }
      }
    case nil:
      if message.attachment != nil {
        Button("Download and Decrypt", systemImage: "arrow.down.circle", action: onDownload)
          .font(.caption.weight(.semibold))
          .buttonStyle(.plain)
      }
    }
  }

  private func transferProgress(title: String, progress: Double) -> some View {
    let normalizedProgress = min(max(progress, 0), 1)
    return VStack(alignment: .leading, spacing: 4) {
      ProgressView(value: normalizedProgress)
        .tint(message.isOutgoing ? .white : KnotStyle.accent)
      Text("\(title) \(Int(normalizedProgress * 100))%")
        .font(.caption2.monospacedDigit())
    }
  }

  @ViewBuilder
  private var deliveryIcon: some View {
    switch message.deliveryState {
    case .sending:
      Image(systemName: "clock")
        .accessibilityLabel("Sending")
    case .sent:
      Image(systemName: "checkmark")
        .accessibilityLabel("Sent")
    case .failed:
      Button(action: onRetry) {
        Image(systemName: "arrow.clockwise.circle.fill")
          .foregroundStyle(.yellow)
      }
      .buttonStyle(.plain)
      .accessibilityLabel("Retry message")
    }
  }

  private var bubbleColor: Color {
    message.isOutgoing ? KnotStyle.outgoing : KnotStyle.primaryBackground
  }

  private var bubbleShape: UnevenRoundedRectangle {
    let joinedTop = position == .middle || position == .last
    let joinedBottom = position == .middle || position == .first
    let topLeading = !message.isOutgoing && joinedTop ? 6.0 : 18.0
    let bottomLeading = !message.isOutgoing && joinedBottom ? 6.0 : 18.0
    let topTrailing = message.isOutgoing && joinedTop ? 6.0 : 18.0
    let bottomTrailing = message.isOutgoing && joinedBottom ? 6.0 : 18.0
    let tailLeading = !message.isOutgoing && !joinedBottom ? 5.0 : bottomLeading
    let tailTrailing = message.isOutgoing && !joinedBottom ? 5.0 : bottomTrailing

    return UnevenRoundedRectangle(
      cornerRadii: RectangleCornerRadii(
        topLeading: topLeading,
        bottomLeading: tailLeading,
        bottomTrailing: tailTrailing,
        topTrailing: topTrailing
      ),
      style: .continuous
    )
  }
}

#Preview("Message bubbles") {
  ZStack {
    KnotChatBackground()
    VStack(spacing: 8) {
      MessageBubble(
        message: ChatMessage(
          id: "incoming",
          body: "The encrypted local history is ready.",
          sentAt: Date(timeIntervalSince1970: 1_768_000_000),
          isOutgoing: false,
          deliveryState: .sent
        )
      )
      MessageBubble(
        message: ChatMessage(
          id: "outgoing",
          body: "Perfect. Let's ship it.",
          sentAt: Date(timeIntervalSince1970: 1_768_000_120),
          isOutgoing: true,
          deliveryState: .sent
        )
      )
    }
    .padding()
  }
}
