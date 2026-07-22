import Foundation
import XCTest
@testable import Knot

final class AppConfigurationTests: XCTestCase {
  func testUnifiedServerUsesSingleReverseProxyOrigin() throws {
    let server = try XCTUnwrap(URL(string: "https://knot-server.example.ts.net"))
    let configuration = AppConfiguration.unified(
      serverBaseURL: server,
      maximumAttachmentCiphertextSize: 1_024
    )

    XCTAssertEqual(configuration.apiBaseURL.absoluteString, "https://knot-server.example.ts.net/api")
    XCTAssertEqual(
      configuration.gatewayBaseURL.absoluteString,
      "https://knot-server.example.ts.net/gateway"
    )
    XCTAssertEqual(
      configuration.attachmentsBaseURL.absoluteString,
      "https://knot-server.example.ts.net/attachments"
    )
    XCTAssertEqual(configuration.pushBaseURL.absoluteString, "https://knot-server.example.ts.net/push")
    XCTAssertEqual(configuration.maximumAttachmentCiphertextSize, 1_024)
  }
}
