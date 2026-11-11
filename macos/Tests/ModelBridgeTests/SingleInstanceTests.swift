import XCTest
@testable import ModelBridge

private struct FakeLister: RunningInstanceLister {
    let pids: [Int32]
    func processIDs(bundleID: String) -> [Int32] { pids }
}

private final class Counter: @unchecked Sendable {
    private let lock = NSLock()
    private var n = 0
    func increment() { lock.lock(); n += 1; lock.unlock() }
    var value: Int { lock.lock(); defer { lock.unlock() }; return n }
}

final class SingleInstanceTests: XCTestCase {
    private func makeGuard(pids: [Int32], center: NotificationCenter) -> SingleInstance {
        SingleInstance(bundleID: "com.example.modelbridge", currentPID: 100, lister: FakeLister(pids: pids), notificationCenter: center)
    }

    func testOtherInstanceRunning_ShouldTerminateAndPostsActivation() {
        let center = NotificationCenter()
        let fired = Counter()
        let token = center.addObserver(forName: SingleInstance.activateMainWindow, object: nil, queue: nil) { _ in fired.increment() }
        defer { center.removeObserver(token) }
        XCTAssertTrue(makeGuard(pids: [100, 200], center: center).shouldTerminate())
        XCTAssertEqual(fired.value, 1)
    }

    func testOnlySelfRunning_ContinuesWithoutNotification() {
        let center = NotificationCenter()
        let fired = Counter()
        let token = center.addObserver(forName: SingleInstance.activateMainWindow, object: nil, queue: nil) { _ in fired.increment() }
        defer { center.removeObserver(token) }
        XCTAssertFalse(makeGuard(pids: [100], center: center).shouldTerminate())
        XCTAssertEqual(fired.value, 0)
    }

    func testNoInstancesListed_Continues() {
        XCTAssertFalse(makeGuard(pids: [], center: NotificationCenter()).shouldTerminate())
    }

    func testNotificationName_IsBundleScoped() {
        XCTAssertEqual(SingleInstance.activateMainWindow.rawValue, "io.github.modelbridge.app.activateMainWindow")
    }

    func testDefaultNotificationCenter_IsDistributed() {
        XCTAssertTrue(SingleInstance.defaultNotificationCenter is DistributedNotificationCenter)
    }

    func testDistributedCenter_ObserverReceivesActivation() {
        let center = DistributedNotificationCenter.default()
        let guardian = makeGuard(pids: [100, 200], center: center)
        let received = expectation(description: "activation received")
        received.assertForOverFulfill = false
        let token = guardian.observeActivation { received.fulfill() }
        defer { center.removeObserver(token) }
        XCTAssertTrue(guardian.shouldTerminate())
        wait(for: [received], timeout: 5)
    }
}
