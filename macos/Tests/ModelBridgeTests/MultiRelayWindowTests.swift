import AppKit
import XCTest
@testable import ModelBridge

@MainActor
final class MainWindowShellTests: XCTestCase {
    private func makeWindow(_ f: ViewFixture) async -> (NSWindow, RenderedView) {
        let window = AppWindows.makeMain(model: f.model)
        hideOffscreen(window)
        try? await Task.sleep(nanoseconds: 200_000_000)
        window.contentView?.layoutSubtreeIfNeeded()
        return (window, RenderedView(existing: window))
    }

    func testWindowHeight_ThreeRelaysForwardingOn() async {
        let f = await ViewFixture.make()
        let (window, _) = await makeWindow(f)
        defer { window.orderOut(nil) }
        XCTAssertGreaterThanOrEqual(window.frame.height, 637)
        XCTAssertLessThanOrEqual(window.frame.height, 661)
    }

    func testTrafficLightsCenteredOnCustomTitle() async throws {
        let f = await ViewFixture.make()
        let (window, view) = await makeWindow(f)
        defer { window.orderOut(nil) }
        let title = try XCTUnwrap(view.frame(ofExactText: "模型接入助手"))
        for kind in [NSWindow.ButtonType.closeButton, .miniaturizeButton, .zoomButton] {
            let button = try XCTUnwrap(window.standardWindowButton(kind))
            let screenRect = window.convertToScreen(button.convert(button.bounds, to: nil))
            XCTAssertEqual(screenRect.midY, title.midY, accuracy: 2, "\(kind)")
        }
    }

    func testCustomTitleBarStartsAtWindowTop() async throws {
        let f = await ViewFixture.make()
        let (window, view) = await makeWindow(f)
        defer { window.orderOut(nil) }
        let title = try XCTUnwrap(view.frame(ofExactText: "模型接入助手"))
        XCTAssertLessThanOrEqual(window.frame.maxY - title.maxY, 24)
    }
}

@MainActor
final class RelaySheetTests: XCTestCase {
    private func openWindow(_ f: ViewFixture) -> (NSWindow, RelaySheetController) {
        let window = AppWindows.makeMain(model: f.model)
        hideOffscreen(window)
        return (window, RelaySheetController(model: f.model, window: window))
    }

    func testAddForm_OpensOneSheetAndClosesOnCancel() async {
        let f = await ViewFixture.make()
        let (window, _) = openWindow(f)
        defer { window.orderOut(nil) }
        f.model.beginAddRelay()
        await awaitUntil { window.attachedSheet != nil }
        XCTAssertNotNil(window.attachedSheet)
        f.model.beginAddRelay()
        try? await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertEqual(window.sheets.count, 1)
        f.model.cancelRelayForm()
        await awaitUntil { window.attachedSheet == nil }
        XCTAssertNil(window.attachedSheet)
        XCTAssertFalse(f.model.showRelayForm)
    }

    func testEditForm_OpensSheet() async {
        let f = await ViewFixture.make()
        let (window, _) = openWindow(f)
        defer { window.orderOut(nil) }
        f.model.beginEditRelay(id: "2")
        await awaitUntil { window.attachedSheet != nil }
        XCTAssertNotNil(window.attachedSheet)
    }

    func testDeleteConfirm_OpensOneSheetAndClosesOnCancel() async {
        let f = await ViewFixture.make()
        let (window, _) = openWindow(f)
        defer { window.orderOut(nil) }
        f.model.requestDeleteRelay(id: "2")
        await awaitUntil { window.attachedSheet != nil }
        XCTAssertNotNil(window.attachedSheet)
        f.model.requestDeleteRelay(id: "3")
        try? await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertEqual(window.sheets.count, 1)
        f.model.cancelPending()
        await awaitUntil { window.attachedSheet == nil }
        XCTAssertNil(window.attachedSheet)
        XCTAssertNil(f.model.pendingConfirmation)
    }

    func testDeleteConfirm_FocusesCancelButton() async {
        let f = await ViewFixture.make()
        f.model.requestDeleteRelay(id: "2")
        let view = RenderedView(DeleteRelayDialogView(model: f.model))
        view.makeKeyOffscreen()
        defer { view.close() }
        try? await Task.sleep(nanoseconds: 300_000_000)
        XCTAssertEqual(view.isFocused(label: "取消"), true)
    }
}

@MainActor
final class RelayMutualExclusionTests: XCTestCase {
    func testEditIgnoredWhileSaving() async {
        let gate = Gate()
        let f = await ViewFixture.make(
            responses: ["save_relay": .success(Data(#"{"id":"1","name":"配置1","model_count":1,"models_changed":true}"#.utf8))],
            gates: ["save_relay": gate]
        )
        f.model.beginEditRelay(id: "1")
        f.model.draftName = "改名"
        let task = Task { await f.model.saveRelay() }
        await awaitUntil { f.model.isSavingRelay }
        f.model.beginEditRelay(id: "2")
        XCTAssertEqual(f.model.editingRelayID, "1")
        XCTAssertEqual(f.model.draftName, "改名")
        XCTAssertEqual(f.model.draftBaseURL, "https://r1.example.com")
        await gate.open()
        await task.value
    }

    func testAddIgnoredWhileFormOpen() async {
        let f = await ViewFixture.make()
        f.model.beginEditRelay(id: "1")
        f.model.draftName = "改了一半"
        f.model.beginAddRelay()
        XCTAssertEqual(f.model.editingRelayID, "1")
        XCTAssertEqual(f.model.draftName, "改了一半")
        XCTAssertEqual(f.model.draftBaseURL, "https://r1.example.com")
    }

    func testOtherConfirmationDoesNotReplaceDeleteInProgress() async {
        let gate = Gate()
        let f = await ViewFixture.make(
            responses: ["delete_relay": .success(Data(#"{"version":"1.3.0","forwarding":true,"relays":[]}"#.utf8))],
            gates: ["delete_relay": gate]
        )
        f.model.requestDeleteRelay(id: "2")
        let pending = f.model.pendingConfirmation
        let task = Task { await f.model.confirmPending() }
        await awaitUntil { f.model.isDeletingRelay }
        await f.model.quit()
        XCTAssertEqual(f.model.pendingConfirmation, pending)
        await f.model.setLoginStart(false)
        XCTAssertEqual(f.model.pendingConfirmation, pending)
        await gate.open()
        await task.value
    }
}

@MainActor
final class MainWindowTextLayoutTests: XCTestCase {
    func testRowNameAndStatusSpacingIs4() async throws {
        let f = await ViewFixture.make()
        let view = RenderedView(MainWindow(model: f.model))
        let name = try XCTUnwrap(view.frame(ofExactText: "配置1"))
        let status = try XCTUnwrap(view.frame(ofText: "6 个模型"))
        let gap = min(abs(name.minY - status.maxY), abs(status.minY - name.maxY))
        XCTAssertEqual(gap, 4, accuracy: 0.5)
    }

    func testStatusBarConfigCountIsSeparatePlainText() async {
        let f = await ViewFixture.make()
        let view = RenderedView(MainWindow(model: f.model))
        XCTAssertNotNil(view.frame(ofExactText: "（3 个配置）"))
    }
}
