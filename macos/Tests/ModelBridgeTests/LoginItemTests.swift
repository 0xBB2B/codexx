import XCTest
@testable import ModelBridge

private final class FakeLoginService: LoginItemService, @unchecked Sendable {
    var status: LoginItemStatus
    var registerCalls = 0
    var unregisterCalls = 0
    var failure: Error?

    init(status: LoginItemStatus) { self.status = status }

    func register() throws {
        registerCalls += 1
        if let failure { throw failure }
        status = .enabled
    }

    func unregister() throws {
        unregisterCalls += 1
        if let failure { throw failure }
        status = .notRegistered
    }
}

private struct SampleError: Error, Equatable {}

final class LoginItemTests: XCTestCase {
    func testIsEnabled_OnlyWhenStatusEnabled() {
        for (status, expected) in [(LoginItemStatus.enabled, true), (.notRegistered, false), (.requiresApproval, false), (.notFound, false)] {
            let item = LoginItem(service: FakeLoginService(status: status), launchedAtLogin: { false })
            XCTAssertEqual(item.isEnabled, expected, "\(status)")
        }
    }

    func testEnable_RegistersService() throws {
        let service = FakeLoginService(status: .notRegistered)
        let item = LoginItem(service: service, launchedAtLogin: { false })
        try item.enable()
        XCTAssertEqual(service.registerCalls, 1)
        XCTAssertTrue(item.isEnabled)
    }

    func testDisable_UnregistersService() throws {
        let service = FakeLoginService(status: .enabled)
        let item = LoginItem(service: service, launchedAtLogin: { false })
        try item.disable()
        XCTAssertEqual(service.unregisterCalls, 1)
        XCTAssertFalse(item.isEnabled)
    }

    func testEnable_ServiceFailurePropagates() {
        let service = FakeLoginService(status: .notRegistered)
        service.failure = SampleError()
        let item = LoginItem(service: service, launchedAtLogin: { false })
        XCTAssertThrowsError(try item.enable()) { XCTAssertEqual($0 as? SampleError, SampleError()) }
    }

    func testWasLaunchedAtLogin_ReflectsInjectedDetector() {
        let yes = LoginItem(service: FakeLoginService(status: .enabled), launchedAtLogin: { true })
        let no = LoginItem(service: FakeLoginService(status: .enabled), launchedAtLogin: { false })
        XCTAssertTrue(yes.wasLaunchedAtLogin)
        XCTAssertFalse(no.wasLaunchedAtLogin)
    }
}
