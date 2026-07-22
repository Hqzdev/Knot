import Foundation

struct AppConfiguration: Sendable {
  let apiBaseURL: URL
  let gatewayBaseURL: URL
  let attachmentsBaseURL: URL
  let pushBaseURL: URL
  let maximumAttachmentCiphertextSize: Int64

  static func live() -> Self {
    let maximumAttachmentCiphertextSize = configuredAttachmentLimit()
    if let serverBaseURL = configuredServerBaseURL() {
      return unified(
        serverBaseURL: serverBaseURL,
        maximumAttachmentCiphertextSize: maximumAttachmentCiphertextSize
      )
    }
    return Self(
      apiBaseURL: configuredURL(
        environment: "KNOT_API_BASE_URL",
        bundleKey: "KnotAPIBaseURL",
        fallback: "http://127.0.0.1:8080"
      ),
      gatewayBaseURL: configuredURL(
        environment: "KNOT_GATEWAY_BASE_URL",
        bundleKey: "KnotGatewayBaseURL",
        fallback: "http://127.0.0.1:8086"
      ),
      attachmentsBaseURL: configuredURL(
        environment: "KNOT_ATTACHMENTS_BASE_URL",
        bundleKey: "KnotAttachmentsBaseURL",
        fallback: "http://127.0.0.1:8082"
      ),
      pushBaseURL: configuredURL(
        environment: "KNOT_PUSH_BASE_URL",
        bundleKey: "KnotPushBaseURL",
        fallback: "http://127.0.0.1:8083"
      ),
      maximumAttachmentCiphertextSize: maximumAttachmentCiphertextSize
    )
  }

  static func unified(serverBaseURL: URL, maximumAttachmentCiphertextSize: Int64) -> Self {
    Self(
      apiBaseURL: serverBaseURL.appending(path: "api"),
      gatewayBaseURL: serverBaseURL.appending(path: "gateway"),
      attachmentsBaseURL: serverBaseURL.appending(path: "attachments"),
      pushBaseURL: serverBaseURL.appending(path: "push"),
      maximumAttachmentCiphertextSize: maximumAttachmentCiphertextSize
    )
  }

  private static func configuredServerBaseURL() -> URL? {
    let value = ProcessInfo.processInfo.environment["KNOT_SERVER_BASE_URL"]
      ?? Bundle.main.object(forInfoDictionaryKey: "KnotServerBaseURL") as? String
    guard let value, !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
      return nil
    }
    return URL(string: value)
  }

  private static func configuredURL(
    environment: String,
    bundleKey: String,
    fallback: String
  ) -> URL {
    let value =
      ProcessInfo.processInfo.environment[environment]
      ?? Bundle.main.object(forInfoDictionaryKey: bundleKey) as? String
      ?? fallback
    return URL(string: value) ?? URL(string: fallback)!
  }

  private static func configuredAttachmentLimit() -> Int64 {
    let environment = ProcessInfo.processInfo.environment["KNOT_ATTACHMENT_MAX_SIZE_BYTES"]
    let bundled = Bundle.main.object(forInfoDictionaryKey: "KnotAttachmentMaxSizeBytes") as? String
    let value = Int64(environment ?? bundled ?? "104857600") ?? 104_857_600
    return min(max(value, 1), 5 << 30)
  }
}

struct LocalDeviceProfile: Sendable {
  let name: String
  let platform: DevicePlatform

  static func current() -> Self {
    let hostname = ProcessInfo.processInfo.hostName.trimmingCharacters(in: .whitespacesAndNewlines)
    let fallback = DevicePlatform.current == .macOS ? "Mac" : "iPhone"
    return Self(name: hostname.isEmpty ? fallback : hostname, platform: .current)
  }
}
