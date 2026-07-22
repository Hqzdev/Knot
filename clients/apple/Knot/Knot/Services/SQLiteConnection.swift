import Foundation
import SQLite3

private let sqliteTransient = unsafeBitCast(-1, to: sqlite3_destructor_type.self)

enum SQLiteValue {
  case text(String)
  case data(Data)
  case integer(Int64)
  case double(Double)
  case null
}

final class SQLiteConnection {
  private var database: OpaquePointer?

  init(url: URL) throws {
    let flags = SQLITE_OPEN_CREATE | SQLITE_OPEN_READWRITE | SQLITE_OPEN_FULLMUTEX
    let status = sqlite3_open_v2(url.path, &database, flags, nil)
    guard status == SQLITE_OK else {
      let message = database.map { String(cString: sqlite3_errmsg($0)) } ?? "Unknown error"
      sqlite3_close(database)
      database = nil
      throw SQLiteError.openFailed(message)
    }
    sqlite3_busy_timeout(database, 5_000)
  }

  deinit {
    sqlite3_close(database)
  }

  func execute(_ sql: String) throws {
    var errorMessage: UnsafeMutablePointer<CChar>?
    let status = sqlite3_exec(database, sql, nil, nil, &errorMessage)
    guard status == SQLITE_OK else {
      let message = errorMessage.map { String(cString: $0) } ?? lastError
      sqlite3_free(errorMessage)
      throw SQLiteError.executionFailed(message)
    }
  }

  func execute(_ sql: String, values: [SQLiteValue]) throws {
    let statement = try prepare(sql)
    try statement.bind(values)
    try statement.execute()
  }

  func query(_ sql: String, values: [SQLiteValue] = []) throws -> SQLiteStatement {
    let statement = try prepare(sql)
    try statement.bind(values)
    return statement
  }

  func transaction<Value>(_ operation: () throws -> Value) throws -> Value {
    try execute("BEGIN IMMEDIATE")
    do {
      let value = try operation()
      try execute("COMMIT")
      return value
    } catch {
      try? execute("ROLLBACK")
      throw error
    }
  }

  private func prepare(_ sql: String) throws -> SQLiteStatement {
    var statement: OpaquePointer?
    let status = sqlite3_prepare_v2(database, sql, -1, &statement, nil)
    guard status == SQLITE_OK, let statement else {
      throw SQLiteError.executionFailed(lastError)
    }
    return SQLiteStatement(statement: statement, connection: self)
  }

  fileprivate var lastError: String {
    database.map { String(cString: sqlite3_errmsg($0)) } ?? "Database is closed"
  }
}

final class SQLiteStatement {
  private let statement: OpaquePointer
  private unowned let connection: SQLiteConnection

  fileprivate init(statement: OpaquePointer, connection: SQLiteConnection) {
    self.statement = statement
    self.connection = connection
  }

  deinit {
    sqlite3_finalize(statement)
  }

  func bind(_ values: [SQLiteValue]) throws {
    for (offset, value) in values.enumerated() {
      let index = Int32(offset + 1)
      let status: Int32
      switch value {
      case .text(let text):
        status = sqlite3_bind_text(statement, index, text, -1, sqliteTransient)
      case .data(let data):
        status = data.withUnsafeBytes { bytes in
          sqlite3_bind_blob(statement, index, bytes.baseAddress, Int32(data.count), sqliteTransient)
        }
      case .integer(let integer):
        status = sqlite3_bind_int64(statement, index, integer)
      case .double(let double):
        status = sqlite3_bind_double(statement, index, double)
      case .null:
        status = sqlite3_bind_null(statement, index)
      }
      guard status == SQLITE_OK else {
        throw SQLiteError.executionFailed(connection.lastError)
      }
    }
  }

  func execute() throws {
    guard sqlite3_step(statement) == SQLITE_DONE else {
      throw SQLiteError.executionFailed(connection.lastError)
    }
  }

  func next() throws -> Bool {
    let status = sqlite3_step(statement)
    if status == SQLITE_ROW {
      return true
    }
    if status == SQLITE_DONE {
      return false
    }
    throw SQLiteError.executionFailed(connection.lastError)
  }

  func text(at index: Int32) -> String {
    guard let value = sqlite3_column_text(statement, index) else {
      return ""
    }
    return String(cString: value)
  }

  func optionalText(at index: Int32) -> String? {
    guard sqlite3_column_type(statement, index) != SQLITE_NULL else {
      return nil
    }
    return text(at: index)
  }

  func data(at index: Int32) -> Data {
    let count = Int(sqlite3_column_bytes(statement, index))
    guard count > 0, let bytes = sqlite3_column_blob(statement, index) else {
      return Data()
    }
    return Data(bytes: bytes, count: count)
  }

  func integer(at index: Int32) -> Int64 {
    sqlite3_column_int64(statement, index)
  }

  func double(at index: Int32) -> Double {
    sqlite3_column_double(statement, index)
  }
}

enum SQLiteError: LocalizedError {
  case openFailed(String)
  case executionFailed(String)

  var errorDescription: String? {
    switch self {
    case .openFailed(let message):
      "Could not open the message database: \(message)"
    case .executionFailed(let message):
      "The message database operation failed: \(message)"
    }
  }
}
