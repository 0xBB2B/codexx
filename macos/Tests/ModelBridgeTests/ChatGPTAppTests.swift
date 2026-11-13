import XCTest
@testable import ModelBridge

private final class FakeApps: AppControlling, @unchecked Sendable {
    private let running: Set<String>

    init(running: Set<String>) { self.running = running }

    func runningBundleIDs() -> Set<String> { running }
}

final class ChatGPTAppTests: XCTestCase {
    func testBundleID_IsComOpenaiCodex() {
        XCTAssertEqual(ChatGPTApp.bundleID, "com.openai.codex")
    }

    func testIsRunning_TrueWhenBundleIDListed() {
        let on = ChatGPTApp(apps: FakeApps(running: ["com.openai.codex"]))
        let off = ChatGPTApp(apps: FakeApps(running: ["com.apple.finder"]))
        XCTAssertTrue(on.isRunning())
        XCTAssertFalse(off.isRunning())
    }
}
