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

    func testLoad_NoFileReturnsNil() throws {
        XCTAssertNil(try settings.loadBaseURL())
    }

    func testSaveThenLoad_CreatesMissingDirectoryAndRoundTrips() throws {
        try settings.saveBaseURL("https://relay.example.com")
        XCTAssertEqual(try settings.loadBaseURL(), "https://relay.example.com")
    }

    func testSave_OverwritesPreviousValue() throws {
        try settings.saveBaseURL("https://relay.example.com")
        try settings.saveBaseURL("https://relay2.example.com")
        XCTAssertEqual(try settings.loadBaseURL(), "https://relay2.example.com")
    }

    func testFile_ContainsOnlyBaseURLField() throws {
        try settings.saveBaseURL("https://relay.example.com")
        let data = try Data(contentsOf: settings.fileURL)
        let obj = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        XCTAssertEqual(Array(obj.keys), ["base_url"])
        XCTAssertEqual(obj["base_url"] as? String, "https://relay.example.com")
    }

    func testDeleteAll_RemovesFileAndLoadReturnsNil() throws {
        try settings.saveBaseURL("https://relay.example.com")
        try settings.deleteAll()
        XCTAssertFalse(FileManager.default.fileExists(atPath: settings.fileURL.path))
        XCTAssertNil(try settings.loadBaseURL())
    }

    func testDeleteAll_NothingToDeleteDoesNotThrow() {
        XCTAssertNoThrow(try settings.deleteAll())
    }
}
