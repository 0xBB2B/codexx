import AppKit
import XCTest
@testable import ModelBridge

private let saveResponse: Result<Data, CoreError> =
    .success(Data(#"{"id":"1","name":"中转","model_count":3,"models_changed":true}"#.utf8))

@MainActor
private final class Presenter {
    var presented: [NSWindow] = []
    var ordersFront = true

    func present(_ window: NSWindow) {
        presented.append(window)
        if ordersFront { hideOffscreen(window) }
    }
}

@MainActor
final class SetupConnectingTests: XCTestCase {
    @MainActor private struct Rig {
        let model: AppModel
        let core: FakeCore
        let keychain: RelayFakeKeychain
        let settings: RelayFakeSettings
        let windows: AppWindowController
        let gate: Gate
        let presenter: Presenter

        var closeButton: NSButton? { windows.setupWindow?.standardWindowButton(.closeButton) }
    }

    private func makeRig(saveResult: Result<Data, CoreError> = saveResponse) async -> Rig {
        let log = CallLog()
        let gate = Gate()
        let presenter = Presenter()
        let core = FakeCore(log: log, responses: ["save_relay": saveResult], gates: ["save_relay": gate])
        let keychain = RelayFakeKeychain(log: log)
        let settings = RelayFakeSettings(log: log)
        let model = AppModel(
            core: core, keychain: keychain, settings: settings,
            loginItem: FakeLoginItem(log: log), chatGPT: FakeChatGPT(running: false)
        )
        await model.start()
        let windows = AppWindowController(model: model, present: { [presenter] in presenter.present($0) })
        model.showSetup = true
        await awaitUntil { windows.setupWindow?.isVisible == true }
        try? await Task.sleep(nanoseconds: 300_000_000)
        model.beginSetup()
        model.draftName = "原名"
        model.draftBaseURL = "https://original.example.com"
        model.draftAPIKey = "sk-original-0000"
        return Rig(model: model, core: core, keychain: keychain, settings: settings, windows: windows, gate: gate, presenter: presenter)
    }

    private func beginConnecting(_ rig: Rig) async -> Task<Void, Never> {
        let task = Task { await rig.model.completeSetup() }
        await awaitUntil { rig.model.setupPhase == .connecting && !rig.core.calls("save_relay").isEmpty }
        return task
    }

    private func tearDown(_ rig: Rig) {
        rig.windows.setupWindow?.orderOut(nil)
        rig.windows.mainWindow?.orderOut(nil)
    }

    func testConnecting_CloseButtonIsDisabledAndWindowCannotClose() async {
        let rig = await makeRig()
        defer { tearDown(rig) }
        let task = await beginConnecting(rig)
        await awaitUntil { rig.closeButton?.isEnabled == false }
        XCTAssertEqual(rig.closeButton?.isEnabled, false)
        rig.windows.setupWindow?.performClose(nil)
        XCTAssertEqual(rig.windows.setupWindow?.isVisible, true)
        await rig.gate.open()
        await task.value
    }

    func testConnectionFailed_CloseButtonIsEnabledAgain() async {
        let rig = await makeRig(saveResult: .failure(.remote(code: "relay_401", keys: [])))
        defer { tearDown(rig) }
        let task = await beginConnecting(rig)
        await awaitUntil { rig.closeButton?.isEnabled == false }
        await rig.gate.open()
        await task.value
        await awaitUntil { rig.closeButton?.isEnabled == true }
        XCTAssertEqual(rig.closeButton?.isEnabled, true)
    }

    func testConnectionSucceeded_CloseButtonIsEnabledAgain() async {
        let rig = await makeRig()
        defer { tearDown(rig) }
        let task = await beginConnecting(rig)
        await awaitUntil { rig.closeButton?.isEnabled == false }
        await rig.gate.open()
        await task.value
        await awaitUntil { rig.closeButton?.isEnabled == true }
        XCTAssertEqual(rig.closeButton?.isEnabled, true)
    }

    func testConnecting_QuitDoesNothingAndRequestsSetupWindowToFront() async {
        let rig = await makeRig()
        defer { tearDown(rig) }
        let terminated = TerminationCounter()
        rig.model.onTerminate = { terminated.increment() }
        let task = await beginConnecting(rig)
        await rig.model.quit()
        XCTAssertNil(rig.model.pendingConfirmation)
        XCTAssertTrue(rig.core.calls("shutdown").isEmpty)
        XCTAssertEqual(terminated.value, 0)
        XCTAssertTrue(rig.model.mainWindowRequested)
        await rig.gate.open()
        await task.value
    }

    func testConnecting_UninstallDoesNothingAndRequestsSetupWindowToFront() async {
        let rig = await makeRig()
        defer { tearDown(rig) }
        let task = await beginConnecting(rig)
        await rig.model.uninstall()
        XCTAssertNil(rig.model.pendingConfirmation)
        XCTAssertEqual(rig.model.uninstallPhase, .idle)
        XCTAssertTrue(rig.model.mainWindowRequested)
        await rig.gate.open()
        await task.value
    }

    func testConnecting_QuitWhileSetupWindowMiniaturized_RestoresAndShowsSetupWindow() async {
        let rig = await makeRig()
        defer { tearDown(rig) }
        let task = await beginConnecting(rig)
        let window = rig.windows.setupWindow
        window?.miniaturize(nil)
        await awaitUntil { window?.isMiniaturized == true }
        XCTAssertEqual(window?.isMiniaturized, true)
        rig.presenter.ordersFront = false
        rig.presenter.presented.removeAll()
        await rig.model.quit()
        XCTAssertTrue(rig.model.mainWindowRequested)
        rig.windows.showMainWindow()
        XCTAssertTrue(rig.presenter.presented.contains { $0 === window })
        await awaitUntil { window?.isMiniaturized == false }
        XCTAssertEqual(window?.isMiniaturized, false)
        await rig.gate.open()
        await task.value
    }

    func testNotConnecting_QuitStillWorksNormally() async {
        let rig = await makeRig()
        defer { tearDown(rig) }
        await rig.model.uninstall()
        XCTAssertEqual(rig.model.pendingConfirmation?.kind, .uninstall)
    }

    func testDraftEditedWhileConnecting_SavedRelayUsesValuesFromClickTime() async throws {
        let rig = await makeRig()
        defer { tearDown(rig) }
        let task = await beginConnecting(rig)
        rig.model.draftName = "改后名字"
        rig.model.draftBaseURL = "https://changed.example.com"
        rig.model.draftAPIKey = "sk-changed-9999"
        await rig.gate.open()
        await task.value
        XCTAssertEqual(rig.settings.stored.map(\.baseURL), ["https://original.example.com"])
        XCTAssertEqual(rig.model.relays.map(\.baseURL), ["https://original.example.com"])
        XCTAssertEqual(rig.keychain.stored["1"], "sk-original-0000")
    }
}

@MainActor
final class ReopenWindowTests: XCTestCase {
    func testAppDelegate_HandlesDockReopen() {
        let delegate = AppDelegate()
        XCTAssertTrue(delegate.responds(to: #selector(NSApplicationDelegate.applicationShouldHandleReopen(_:hasVisibleWindows:))))
    }

    func testReopenAfterMainWindowClosed_ShowsMainWindowWhenSetupIsDone() async {
        let f = await ViewFixture.make()
        var presented: [NSWindow] = []
        let windows = AppWindowController(model: f.model, present: { presented.append($0); hideOffscreen($0) })
        defer { windows.mainWindow?.orderOut(nil) }
        windows.showMainWindow()
        let main = windows.mainWindow
        main?.orderOut(nil)
        presented.removeAll()
        windows.showMainWindow()
        XCTAssertTrue(presented.first === main)
        XCTAssertEqual(main?.isVisible, true)
    }

    func testReopenDuringSetup_ShowsSetupWindow() async {
        let f = await ViewFixture.make(relayCount: 0)
        var presented: [NSWindow] = []
        let windows = AppWindowController(model: f.model, present: { presented.append($0); hideOffscreen($0) })
        defer { windows.setupWindow?.orderOut(nil) }
        f.model.showSetup = true
        await awaitUntil { windows.setupWindow?.isVisible == true }
        windows.setupWindow?.orderOut(nil)
        presented.removeAll()
        windows.showMainWindow()
        XCTAssertTrue(presented.first === windows.setupWindow)
        XCTAssertNil(windows.mainWindow)
    }
}
