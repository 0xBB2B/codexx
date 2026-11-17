import XCTest
@testable import ModelBridge

@MainActor
final class AccessDetectionRaceTests: XCTestCase {
    private func json(_ forwarding: Bool, mode: String = "both", target: String? = nil) -> Result<Data, CoreError> {
        .success(Data(ViewFixture.statusJSON(forwarding: forwarding, relayCount: 3, mode: mode, target: target).utf8))
    }

    func testDetectionDuringPendingSwitch_DoesNotApplyAccessOrUnlockToggleEarly() async {
        let gate = Gate()
        let f = await ViewFixture.make(
            forwarding: false,
            responses: [
                "set_forwarding": json(true),
                "detect_access": json(false, mode: "both", target: "relay"),
                "apply_access": json(true, mode: "relay"),
            ],
            gates: ["set_forwarding": gate]
        )
        let toggle = Task { await f.model.toggleForwarding(enabled: true) }
        await awaitUntil { !f.core.calls("set_forwarding").isEmpty }
        let detect = Task { await f.model.recheckAccess() }
        try? await Task.sleep(nanoseconds: 300_000_000)
        XCTAssertEqual(f.core.calls("apply_access").count, 0)
        XCTAssertTrue(f.model.forwardingToggleDisabled)
        XCTAssertTrue(f.model.isSwitchingForwarding)
        await gate.open()
        await toggle.value
        await detect.value
        XCTAssertTrue(f.model.forwardingEnabled)
        XCTAssertFalse(f.model.isSwitchingForwarding)
        XCTAssertFalse(f.model.isDetectingAccess)
        XCTAssertFalse(f.model.forwardingToggleDisabled)
    }

    func testSwitchDuringDetection_SkipsApplyAccess() async {
        let gate = Gate()
        let f = await ViewFixture.make(
            forwarding: true,
            responses: [
                "detect_access": json(true, mode: "both", target: "relay"),
                "set_forwarding": json(false),
                "apply_access": json(false, mode: "relay"),
            ],
            gates: ["detect_access": gate]
        )
        let detect = Task { await f.model.recheckAccess() }
        await awaitUntil { !f.core.calls("detect_access").isEmpty }
        await f.model.toggleForwarding(enabled: false)
        await gate.open()
        await detect.value
        XCTAssertEqual(f.core.calls("apply_access").count, 0)
        XCTAssertEqual(f.core.calls("set_forwarding").count, 1)
        XCTAssertFalse(f.model.forwardingEnabled)
    }

    func testApplyAccessInProgress_DisablesToggleThenRestoresIt() async {
        let gate = Gate()
        let f = await ViewFixture.make(
            forwarding: true,
            responses: [
                "detect_access": json(true, mode: "both", target: "relay"),
                "apply_access": json(true, mode: "relay"),
            ],
            gates: ["apply_access": gate]
        )
        let detect = Task { await f.model.recheckAccess() }
        await awaitUntil { !f.core.calls("apply_access").isEmpty }
        XCTAssertTrue(f.model.forwardingToggleDisabled)
        XCTAssertEqual(RenderedView(MainWindow(model: f.model)).isEnabled(label: "使用中转"), false)
        await gate.open()
        await detect.value
        XCTAssertFalse(f.model.forwardingToggleDisabled)
        XCTAssertEqual(RenderedView(MainWindow(model: f.model)).isEnabled(label: "使用中转"), true)
    }

    func testStartupAlignmentSkippedByPendingSwitchIsRetriedOnNextStatus() async throws {
        let gate = Gate()
        let f = await ViewFixture.make(
            forwarding: false,
            responses: [
                "set_forwarding": .failure(.remote(code: "internal_error", keys: [])),
                "apply_access": json(true, mode: "relay"),
            ],
            gates: ["set_forwarding": gate]
        )
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        let pushed = try decoder.decode(
            CoreStatus.self,
            from: Data(ViewFixture.statusJSON(forwarding: true, relayCount: 3, mode: "both", target: "relay").utf8)
        )
        let toggle = Task { await f.model.toggleForwarding(enabled: true) }
        await awaitUntil { !f.core.calls("set_forwarding").isEmpty }
        f.core.statusContinuation.yield(pushed)
        await awaitUntil { f.model.forwardingEnabled }
        try? await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertEqual(f.core.calls("apply_access").count, 0)
        await gate.open()
        await toggle.value
        f.core.statusContinuation.yield(pushed)
        await awaitUntil { !f.core.calls("apply_access").isEmpty }
        try? await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertEqual(f.core.calls("apply_access").count, 1)
    }
}
