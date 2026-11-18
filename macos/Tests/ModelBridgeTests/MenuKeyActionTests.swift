import XCTest
@testable import ModelBridge

final class MenuKeyActionTests: XCTestCase {
    private func action(_ keyCode: UInt16, command: Bool = false, key: String? = nil) -> MenuKeyAction? {
        MenuKeyAction(keyCode: keyCode, command: command, key: key)
    }

    func testArrowDown_MovesDown() { XCTAssertEqual(action(125), .moveDown) }
    func testArrowUp_MovesUp() { XCTAssertEqual(action(126), .moveUp) }
    func testReturn_ActivatesFocused() { XCTAssertEqual(action(36), .activateFocused) }
    func testSpace_ActivatesFocused() { XCTAssertEqual(action(49, key: " "), .activateFocused) }
    func testEscape_Closes() { XCTAssertEqual(action(53), .close) }
    func testCommandComma_OpensConfig() { XCTAssertEqual(action(43, command: true, key: ","), .openConfig) }
    func testCommandQ_Quits() { XCTAssertEqual(action(12, command: true, key: "q"), .quit) }

    func testOtherKeys_AreIgnored() {
        XCTAssertNil(action(0, key: "a"))
        XCTAssertNil(action(43, key: ","))
        XCTAssertNil(action(12, key: "q"))
        XCTAssertNil(action(0, command: true, key: "a"))
        XCTAssertNil(action(125, command: true))
    }
}
