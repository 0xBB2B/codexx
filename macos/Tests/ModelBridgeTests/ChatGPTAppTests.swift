import XCTest
@testable import ModelBridge

private final class FakeApps: AppControlling, @unchecked Sendable {
    private let lock = NSLock()
    private var running: Set<String>
    private var calls: [String] = []
    private let quitsOnRequest: Bool
    private let openError: Error?

    init(running: Set<String>, quitsOnRequest: Bool, openError: Error? = nil) {
        self.running = running
        self.quitsOnRequest = quitsOnRequest
        self.openError = openError
    }

    var recordedCalls: [String] { lock.lock(); defer { lock.unlock() }; return calls }

    func runningBundleIDs() -> Set<String> {
        lock.lock(); defer { lock.unlock() }
        return running
    }

    func requestQuit(bundleID: String) {
        lock.lock(); defer { lock.unlock() }
        calls.append("quit:\(bundleID)")
        if quitsOnRequest { running.remove(bundleID) }
    }

    func openApplication(at url: URL) async throws {
        record("open:\(url.path)")
        if let openError { throw openError }
    }

    private func record(_ call: String) {
        lock.lock(); defer { lock.unlock() }
        calls.append(call)
    }
}

final class ChatGPTAppTests: XCTestCase {
    private let home = URL(fileURLWithPath: "/Users/example")
    private let systemApp = "/Applications/ChatGPT.app"

    private func makeApp(_ apps: FakeApps, existing: Set<String>, timeout: TimeInterval = 0.3) -> ChatGPTApp {
        ChatGPTApp(
            apps: apps,
            homeDirectory: home,
            fileExists: { existing.contains($0.path) },
            quitTimeout: timeout,
            pollInterval: 0.02
        )
    }

    func testBundleID_IsComOpenaiCodex() {
        XCTAssertEqual(ChatGPTApp.bundleID, "com.openai.codex")
    }

    func testIsRunning_TrueWhenBundleIDListed() {
        let on = makeApp(FakeApps(running: ["com.openai.codex"], quitsOnRequest: true), existing: [])
        let off = makeApp(FakeApps(running: ["com.apple.finder"], quitsOnRequest: true), existing: [])
        XCTAssertTrue(on.isRunning())
        XCTAssertFalse(off.isRunning())
    }

    func testLocate_PrefersSystemApplications() {
        let app = makeApp(FakeApps(running: [], quitsOnRequest: true), existing: [systemApp, "/Users/example/Applications/ChatGPT.app"])
        XCTAssertEqual(app.locate()?.path, systemApp)
    }

    func testLocate_FallsBackToUserApplications() {
        let app = makeApp(FakeApps(running: [], quitsOnRequest: true), existing: ["/Users/example/Applications/ChatGPT.app"])
        XCTAssertEqual(app.locate()?.path, "/Users/example/Applications/ChatGPT.app")
    }

    func testLocate_NotInstalledReturnsNil() {
        let app = makeApp(FakeApps(running: [], quitsOnRequest: true), existing: [])
        XCTAssertNil(app.locate())
    }

    func testRestart_QuitsGracefullyThenOpensApp() async {
        let apps = FakeApps(running: ["com.openai.codex"], quitsOnRequest: true)
        let result = await makeApp(apps, existing: [systemApp]).restart()
        XCTAssertEqual(result, .restarted)
        XCTAssertEqual(apps.recordedCalls, ["quit:com.openai.codex", "open:\(systemApp)"])
    }

    func testRestart_NotQuitWithinTimeoutReportsTimedOutWithoutForceOrOpen() async {
        let apps = FakeApps(running: ["com.openai.codex"], quitsOnRequest: false)
        let started = Date()
        let result = await makeApp(apps, existing: [systemApp], timeout: 0.3).restart()
        XCTAssertEqual(result, .timedOut)
        XCTAssertGreaterThanOrEqual(Date().timeIntervalSince(started), 0.25)
        XCTAssertFalse(apps.recordedCalls.contains { $0.hasPrefix("open:") })
    }

    func testDefaultQuitTimeout_Is30Seconds() {
        XCTAssertEqual(ChatGPTApp.defaultQuitTimeout, 30)
    }

    func testRestart_NotRunningOpensDirectly() async {
        let apps = FakeApps(running: [], quitsOnRequest: true)
        let result = await makeApp(apps, existing: [systemApp]).restart()
        XCTAssertEqual(result, .restarted)
        XCTAssertEqual(apps.recordedCalls, ["open:\(systemApp)"])
    }

    func testRestart_NotInstalledReturnsNotFoundAndCallsNothing() async {
        let apps = FakeApps(running: ["com.openai.codex"], quitsOnRequest: true)
        let result = await makeApp(apps, existing: []).restart()
        XCTAssertEqual(result, .notFound)
        XCTAssertEqual(apps.recordedCalls, [])
    }

    func testRestart_OpenFailureReportsOpenFailed() async {
        struct OpenError: Error {}
        let apps = FakeApps(running: [], quitsOnRequest: true, openError: OpenError())
        let result = await makeApp(apps, existing: [systemApp]).restart()
        XCTAssertEqual(result, .openFailed)
    }

    func testRestart_CancelledWhileWaitingReturnsCancelledPromptly() async {
        let apps = FakeApps(running: ["com.openai.codex"], quitsOnRequest: false)
        let app = makeApp(apps, existing: [systemApp], timeout: 5)
        let started = Date()
        let task = Task { await app.restart() }
        try? await Task.sleep(nanoseconds: 100_000_000)
        task.cancel()
        let result = await task.value
        XCTAssertEqual(result, .cancelled)
        XCTAssertLessThan(Date().timeIntervalSince(started), 2)
    }
}
