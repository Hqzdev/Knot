import XCTest

@testable import KnotCryptoBindings

final class KnotCryptoBindingsTests: XCTestCase {
  func testPrekeyStateRoundTrip() throws {
    let store = KnotPreKeyStore(oneTimePrekeyCount: 2)
    XCTAssertEqual(try store.remainingOneTimePrekeyCount(), 2)

    let state = try store.exportState()
    let restored = try KnotPreKeyStore.restore(state: state)

    XCTAssertEqual(try restored.remainingOneTimePrekeyCount(), 2)
    XCTAssertEqual(try restored.keyBundle().oneTimePrekeys.count, 2)
  }
}
