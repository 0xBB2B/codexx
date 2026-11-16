import XCTest
@testable import ModelBridge

final class ShellSettingsTests: XCTestCase {
    private var dir: URL!
    private var settings: ShellSettings!

    override func setUp() {
        dir = FileManager.default.temporaryDirectory
            .appendingPathComponent("settings-\(UUID().uuidString)")
            .appendingPathComponent("ModelBridge")
        settings = ShellSettings(directory: dir)
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: dir.deletingLastPathComponent())
    }

    func testDefaultDirectory_IsApplicationSupportModelBridge() {
        XCTAssertTrue(ShellSettings.defaultDirectory.path.hasSuffix("Library/Application Support/ModelBridge"))
    }

    func testFileName_IsShellSettingsJson() {
        XCTAssertEqual(settings.fileURL.lastPathComponent, "shell-settings.json")
        XCTAssertEqual(settings.fileURL.deletingLastPathComponent().path, dir.path)
    }
}
