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
        for code in ["name_empty", "name_too_long", "name_has_dot", "name_duplicate", "relay_limit", "relay_last", "relay_not_found", "relay_401"] {
            XCTAssertNotNil(expected[code], "PROTOCOL.md 缺少 \(code)")
        }
        for (code, text) in expected {
            XCTAssertEqual(CoreProtocol.errorMessages[code], text, "错误码 \(code)")
        }
    }

    func testErrorMessages_NoCodesBeyondProtocol() throws {
        let expected = try protocolErrorTable()
        XCTAssertEqual(Set(CoreProtocol.errorMessages.keys), Set(expected.keys))
    }

    func testErrorMessages_NoLegacyWording() {
        let forbidden = "\u{4E2D}\u{53F0}"
        for (code, text) in CoreProtocol.errorMessages {
            XCTAssertFalse(text.contains(forbidden), "错误码 \(code)")
        }
    }

    private func decodeStatus(_ access: String?) throws -> CoreStatus {
        let accessField = access.map { #","access":\#($0)"# } ?? ""
        let json = #"{"version":"1.2.0","forwarding":true\#(accessField)}"#
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        return try decoder.decode(CoreStatus.self, from: Data(json.utf8))
    }

    func testCoreStatus_DecodesAccess() throws {
        let status = try decodeStatus(#"{"mode":"relay","target":"both","detect":"logged_in"}"#)
        XCTAssertEqual(status.access?.mode, "relay")
        XCTAssertEqual(status.access?.target, "both")
        XCTAssertEqual(status.access?.detect, "logged_in")
    }

    func testCoreStatus_AccessNullFields_DecodeToNil() throws {
        let status = try decodeStatus(#"{"mode":null,"target":null,"detect":null}"#)
        XCTAssertNotNil(status.access)
        XCTAssertNil(status.access?.mode)
        XCTAssertNil(status.access?.target)
        XCTAssertNil(status.access?.detect)
    }

    func testCoreStatus_MissingAccess_IsNil() throws {
        XCTAssertNil(try decodeStatus(nil).access)
    }
}
