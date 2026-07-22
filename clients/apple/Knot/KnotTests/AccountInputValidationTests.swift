import Foundation
import XCTest

@testable import Knot

final class AccountInputValidationTests: XCTestCase {
  func testAcceptsValidEmailAndRejectsIncompleteAddress() {
    XCTAssertTrue(AccountInputValidation.isValidEmail("Alice@Example.com"))
    XCTAssertFalse(AccountInputValidation.isValidEmail("alice@example"))
    XCTAssertFalse(AccountInputValidation.isValidEmail(" alice@example.com"))
    XCTAssertFalse(AccountInputValidation.isValidEmail("alice@@example.com"))
  }

  func testUsernameMatchesServerRules() {
    XCTAssertTrue(AccountInputValidation.isValidUsername("alice_2026"))
    XCTAssertFalse(AccountInputValidation.isValidUsername("al"))
    XCTAssertFalse(AccountInputValidation.isValidUsername("alice.smith"))
    XCTAssertFalse(AccountInputValidation.isValidUsername("алиса"))
  }

  func testPasswordRequiresTwelveUTF8Bytes() {
    XCTAssertTrue(AccountInputValidation.isValidPassword("twelve-chars"))
    XCTAssertFalse(AccountInputValidation.isValidPassword("short"))
  }

  func testLegacySessionWithoutEmailStillDecodes() throws {
    let data = Data(
      #"{"access_token":"access","refresh_token":"refresh","user_id":"user","username":"alice","device_id":"device"}"#
        .utf8
    )
    let session = try JSONDecoder().decode(AuthSession.self, from: data)
    XCTAssertNil(session.email)
    XCTAssertEqual(session.username, "alice")
  }
}
