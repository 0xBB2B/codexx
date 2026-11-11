import XCTest
@testable import ModelBridge

final class CoreProtocolTests: XCTestCase {
    private func protocolErrorTable() throws -> [String: String] {
        let root = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent()
        let text = try String(contentsOf: root.appendingPathComponent("core/PROTOCOL.md"), encoding: .utf8)
        let section = try XCTUnwrap(text.components(separatedBy: "## 错误码").last)
        var table: [String: String] = [:]
        for line in section.split(separator: "\n") where line.hasPrefix("|") {
            let cells = line.split(separator: "|", omittingEmptySubsequences: false)
                .map { $0.trimmingCharacters(in: .whitespaces) }
            guard cells.count >= 3, cells[1] != "码", !cells[1].hasPrefix("---") else { continue }
            table[cells[1]] = cells[2]
        }
        return table
    }

    func testErrorMessages_CoverEveryCodeInProtocolWithIdenticalText() throws {
        let expected = try protocolErrorTable()
        XCTAssertEqual(expected.count, 18)
        for (code, text) in expected {
            XCTAssertEqual(CoreProtocol.errorMessages[code], text, "错误码 \(code)")
        }
    }

    func testErrorMessages_NoCodesBeyondProtocol() throws {
        let expected = try protocolErrorTable()
        XCTAssertEqual(Set(CoreProtocol.errorMessages.keys), Set(expected.keys))
    }
}
