import Foundation
import XCTest
@testable import Knot

final class MessagePresentationTests: XCTestCase {
  func testGroupsAdjacentMessagesInTheSameDirectionWithinFiveMinutes() throws {
    let calendar = calendar
    let messages = [
      message(id: "1", minute: 0, outgoing: true),
      message(id: "2", minute: 2, outgoing: true),
      message(id: "3", minute: 4, outgoing: true),
      message(id: "4", minute: 5, outgoing: false),
    ]

    let sections = MessagePresentationBuilder.sections(messages: messages, calendar: calendar)
    let presented = try XCTUnwrap(sections.first).messages

    XCTAssertEqual(sections.count, 1)
    XCTAssertEqual(presented.map(\.position), [.first, .middle, .last, .single])
  }

  func testSeparatesMessagesByCalendarDay() {
    let calendar = calendar
    let messages = [
      message(id: "late", day: 1, hour: 23, minute: 59, outgoing: true),
      message(id: "early", day: 2, hour: 0, minute: 1, outgoing: true),
    ]

    let sections = MessagePresentationBuilder.sections(messages: messages, calendar: calendar)

    XCTAssertEqual(sections.count, 2)
    XCTAssertEqual(sections.map { $0.messages.map(\.id) }, [["late"], ["early"]])
    XCTAssertEqual(sections.flatMap(\.messages).map(\.position), [.single, .single])
  }

  func testBreaksGroupAfterFiveMinutes() throws {
    let messages = [
      message(id: "1", minute: 0, outgoing: false),
      message(id: "2", minute: 6, outgoing: false),
    ]

    let section = try XCTUnwrap(
      MessagePresentationBuilder.sections(messages: messages, calendar: calendar).first
    )

    XCTAssertEqual(section.messages.map(\.position), [.single, .single])
  }

  private var calendar: Calendar {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(secondsFromGMT: 0)!
    return calendar
  }

  private func message(
    id: String,
    day: Int = 1,
    hour: Int = 12,
    minute: Int,
    outgoing: Bool
  ) -> ChatMessage {
    let date = calendar.date(
      from: DateComponents(year: 2026, month: 7, day: day, hour: hour, minute: minute)
    )!
    return ChatMessage(
      id: id,
      body: id,
      sentAt: date,
      isOutgoing: outgoing,
      deliveryState: .sent
    )
  }
}
