import Foundation

enum MessageGroupPosition: Hashable {
  case single
  case first
  case middle
  case last
}

struct PresentedMessage: Identifiable, Hashable {
  let message: ChatMessage
  let position: MessageGroupPosition

  var id: String { message.id }
}

struct MessageDaySection: Identifiable, Hashable {
  let day: Date
  let messages: [PresentedMessage]

  var id: Date { day }
}

enum MessagePresentationBuilder {
  static let groupingInterval: TimeInterval = 5 * 60

  static func sections(
    messages: [ChatMessage],
    calendar: Calendar = .autoupdatingCurrent
  ) -> [MessageDaySection] {
    Dictionary(grouping: messages) { message in
      calendar.startOfDay(for: message.sentAt)
    }
    .map { day, messages in
      let sortedMessages = messages.sorted { lhs, rhs in
        if lhs.sentAt == rhs.sentAt {
          return lhs.id < rhs.id
        }
        return lhs.sentAt < rhs.sentAt
      }
      return MessageDaySection(
        day: day,
        messages: sortedMessages.indices.map { index in
          PresentedMessage(
            message: sortedMessages[index],
            position: position(at: index, messages: sortedMessages)
          )
        }
      )
    }
    .sorted { $0.day < $1.day }
  }

  private static func position(at index: Int, messages: [ChatMessage])
    -> MessageGroupPosition
  {
    let joinsPrevious =
      index > messages.startIndex
      && messagesJoin(messages[index - 1], messages[index])
    let joinsNext =
      index < messages.index(before: messages.endIndex)
      && messagesJoin(messages[index], messages[index + 1])

    switch (joinsPrevious, joinsNext) {
    case (false, false):
      return .single
    case (false, true):
      return .first
    case (true, true):
      return .middle
    case (true, false):
      return .last
    }
  }

  private static func messagesJoin(_ first: ChatMessage, _ second: ChatMessage) -> Bool {
    let interval = second.sentAt.timeIntervalSince(first.sentAt)
    return first.isOutgoing == second.isOutgoing
      && interval >= 0
      && interval <= groupingInterval
  }
}
