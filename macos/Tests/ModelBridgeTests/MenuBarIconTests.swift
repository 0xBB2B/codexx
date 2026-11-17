import AppKit
import XCTest
@testable import ModelBridge

final class MenuBarIconTests: XCTestCase {
    private let resources = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        .appendingPathComponent("Resources")

    func testMenuBarImage_LoadsFromResources() {
        XCTAssertNotNil(menuBarImage(in: resources))
    }

    func testMenuBarImage_IsTemplate() {
        XCTAssertEqual(menuBarImage(in: resources)?.isTemplate, true)
    }

    func testMenuBarImage_SizeIs18Points() {
        XCTAssertEqual(menuBarImage(in: resources)?.size, NSSize(width: 18, height: 18))
    }

    func testMenuBarImage_MissingDirectory_ReturnsNil() {
        let missing = resources.appendingPathComponent("does-not-exist")
        XCTAssertNil(menuBarImage(in: missing))
    }

    func testMenuBarImage_Has2xRepresentation36Pixels() {
        let reps = menuBarImage(in: resources)?.representations ?? []
        XCTAssertTrue(reps.contains { $0.pixelsWide == 36 && $0.pixelsHigh == 36 })
    }
}
