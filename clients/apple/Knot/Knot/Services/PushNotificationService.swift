import Foundation

#if os(iOS)
  import UIKit
#elseif os(macOS)
  import AppKit
#endif

struct APNSRegistrationRequest: Encodable, Sendable {
  let deviceToken: String

  enum CodingKeys: String, CodingKey {
    case deviceToken = "device_token"
  }
}

enum PushNotificationEvent: Equatable, Sendable {
  case messageAvailable

  init?(type: String?) {
    guard type == "message_available" else {
      return nil
    }
    self = .messageAvailable
  }
}

actor PushRegistrationService {
  private let baseURL: URL
  private let session: URLSession

  init(baseURL: URL, session: URLSession = .shared) {
    self.baseURL = baseURL
    self.session = session
  }

  func register(deviceToken: String, accessToken: String) async throws {
    var request = URLRequest(url: baseURL.appending(path: "v1/push/subscriptions/apns"))
    request.httpMethod = "PUT"
    request.httpBody = try JSONEncoder().encode(
      APNSRegistrationRequest(deviceToken: deviceToken)
    )
    request.setValue("application/json", forHTTPHeaderField: "Content-Type")
    request.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")
    try await send(request)
  }

  func revoke(accessToken: String) async throws {
    var request = URLRequest(url: baseURL.appending(path: "v1/push/subscriptions/apns"))
    request.httpMethod = "DELETE"
    request.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")
    try await send(request)
  }

  private func send(_ request: URLRequest) async throws {
    let (data, response) = try await session.data(for: request)
    guard let http = response as? HTTPURLResponse else {
      throw APIClientError.invalidResponse("Missing HTTP response")
    }
    guard 200..<300 ~= http.statusCode else {
      let serverError = try? JSONDecoder().decode(PushServerError.self, from: data)
      throw APIClientError.server(status: http.statusCode, message: serverError?.error)
    }
  }
}

@MainActor
final class PushNotificationBroker {
  private(set) var deviceToken: String?
  private(set) var registrationError: String?

  private var tokenHandler: ((String) -> Void)?
  private var syncHandler: (() async -> Bool)?
  private var failureHandler: ((String) -> Void)?

  func bind(
    tokenHandler: @escaping (String) -> Void,
    syncHandler: @escaping () async -> Bool,
    failureHandler: @escaping (String) -> Void
  ) {
    self.tokenHandler = tokenHandler
    self.syncHandler = syncHandler
    self.failureHandler = failureHandler
    if let deviceToken {
      tokenHandler(deviceToken)
    }
    if let registrationError {
      failureHandler(registrationError)
    }
  }

  func didRegister(deviceToken data: Data) {
    guard !data.isEmpty else {
      let message = "APNs returned an invalid device token."
      registrationError = message
      failureHandler?(message)
      return
    }
    let token = data.map { String(format: "%02x", $0) }.joined()
    deviceToken = token
    registrationError = nil
    tokenHandler?(token)
  }

  func didFailToRegister(_ error: Error) {
    let message = error.localizedDescription
    registrationError = message
    failureHandler?(message)
  }

  func handleRemoteNotification(_ userInfo: [AnyHashable: Any]) async -> Bool {
    guard PushNotificationEvent(type: userInfo["type"] as? String) == .messageAvailable else {
      return false
    }
    return await syncHandler?() ?? false
  }
}

#if os(iOS)
  @MainActor
  final class KnotIOSAppDelegate: NSObject, UIApplicationDelegate {
    let pushBroker = PushNotificationBroker()

    func application(
      _ application: UIApplication,
      didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
      application.registerForRemoteNotifications()
      return true
    }

    func application(
      _ application: UIApplication,
      didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data
    ) {
      pushBroker.didRegister(deviceToken: deviceToken)
    }

    func application(
      _ application: UIApplication,
      didFailToRegisterForRemoteNotificationsWithError error: Error
    ) {
      pushBroker.didFailToRegister(error)
    }

    func application(
      _ application: UIApplication,
      didReceiveRemoteNotification userInfo: [AnyHashable: Any],
      fetchCompletionHandler completionHandler: @escaping (UIBackgroundFetchResult) -> Void
    ) {
      Task {
        let synchronized = await pushBroker.handleRemoteNotification(userInfo)
        completionHandler(synchronized ? .newData : .noData)
      }
    }
  }
#elseif os(macOS)
  @MainActor
  final class KnotMacAppDelegate: NSObject, NSApplicationDelegate {
    let pushBroker = PushNotificationBroker()

    func applicationDidFinishLaunching(_ notification: Notification) {
      NSApplication.shared.registerForRemoteNotifications()
    }

    func application(
      _ application: NSApplication,
      didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data
    ) {
      pushBroker.didRegister(deviceToken: deviceToken)
    }

    func application(
      _ application: NSApplication,
      didFailToRegisterForRemoteNotificationsWithError error: Error
    ) {
      pushBroker.didFailToRegister(error)
    }

    func application(
      _ application: NSApplication,
      didReceiveRemoteNotification userInfo: [String: Any]
    ) {
      Task {
        _ = await pushBroker.handleRemoteNotification(userInfo)
      }
    }
  }
#endif

private struct PushServerError: Decodable {
  let error: String
}
