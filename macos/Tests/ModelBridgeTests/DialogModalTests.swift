import AppKit
import XCTest
@testable import ModelBridge

@MainActor
private final class ModalProbe {
    private(set) var pressed = false
    private(set) var timedOut = false
    private let start = Date()
    private var timer: Timer?
    private let deadline: TimeInterval

    init(deadline: TimeInterval = 2.5, onTick: @escaping @MainActor (ModalProbe) -> Bool) {
        self.deadline = deadline
        let timer = Timer(timeInterval: 0.05, repeats: true) { [self] _ in
            MainActor.assumeIsolated {
                if self.finished {
                    if NSApp.modalWindow != nil { NSApp.stopModal() }
                } else if onTick(self) {
                    self.finish()
                } else if Date().timeIntervalSince(self.start) > self.deadline {
                    self.timedOut = true
                    self.finish()
                }
            }
        }
        RunLoop.main.add(timer, forMode: .common)
        RunLoop.main.add(timer, forMode: .modalPanel)
        self.timer = timer
    }

    var elapsed: TimeInterval { Date().timeIntervalSince(start) }

    var panel: NSWindow? { NSApp.modalWindow }

    func pressPrimary() {
        guard !pressed, let panel else { return }
        pressed = true
        let point = NSPoint(x: panel.frame.width - 48 - 24 - 40, y: 78)
        for type in [NSEvent.EventType.leftMouseDown, .leftMouseUp] {
            if let event = NSEvent.mouseEvent(
                with: type, location: point, modifierFlags: [], timestamp: ProcessInfo.processInfo.systemUptime,
                windowNumber: panel.windowNumber, context: nil, eventNumber: 0, clickCount: 1, pressure: 1
            ) { panel.sendEvent(event) }
        }
    }

    func pressSpace() {
        guard let panel else { return }
        for type in [NSEvent.EventType.keyDown, .keyUp] {
            if let event = NSEvent.keyEvent(
                with: type, location: .zero, modifierFlags: [], timestamp: ProcessInfo.processInfo.systemUptime,
                windowNumber: panel.windowNumber, context: nil, characters: " ", charactersIgnoringModifiers: " ",
                isARepeat: false, keyCode: 49
            ) { panel.sendEvent(event) }
        }
    }

    func stop() {
        timer?.invalidate()
        timer = nil
    }

    private(set) var finished = false

    func finish() {
        finished = true
        if NSApp.modalWindow != nil { NSApp.stopModal() }
    }
}

private let blockedKeys: Result<Data, CoreError> = .failure(.remote(
    code: "restore_blocked", keys: ["model_provider", "chatgpt_base_url", "model_catalog_json", "openai_base_url"]
))

@MainActor
final class DialogModalTests: XCTestCase {
    private func makeModel(
        responses: [String: Result<Data, CoreError>] = [:]
    ) -> (AppModel, FakeCore, TerminationCounter) {
        let log = CallLog()
        let core = FakeCore(log: log, responses: responses)
        let model = AppModel(
            core: core, keychain: RelayFakeKeychain(log: log), settings: RelayFakeSettings(log: log, relays: [relayOne]),
            loginItem: FakeLoginItem(log: log), chatGPT: FakeChatGPT(running: false)
        )
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        model.status = try! decoder.decode(CoreStatus.self, from: Data(ViewFixture.statusJSON(forwarding: true, relayCount: 1).utf8))
        let terminations = TerminationCounter()
        model.onTerminate = { terminations.increment() }
        return (model, core, terminations)
    }

    private func waitUntilFinished(_ probe: ModalProbe) async {
        await awaitUntil(timeout: 10) { probe.finished && NSApp.modalWindow == nil }
        probe.stop()
    }

    func testQuitDialog_ConfirmWhileModalOpen_ShutdownStartsBeforeModalEnds() async {
        let (model, core, terminations) = makeModel()
        let presenter = DialogPresenter(model: model)
        var shutdownDuringModal = false
        var terminatedDuringModal = false
        let probe = ModalProbe { probe in
            probe.pressPrimary()
            if probe.pressed && !core.calls("shutdown").isEmpty {
                shutdownDuringModal = true
                terminatedDuringModal = terminations.value == 1
                return true
            }
            return false
        }
        await model.quit()
        await waitUntilFinished(probe)
        withExtendedLifetime(presenter) {}
        XCTAssertTrue(probe.pressed, "确认按钮没找到或没按下")
        XCTAssertTrue(shutdownDuringModal, "弹窗开着期间没有向后台发 shutdown")
        XCTAssertTrue(terminatedDuringModal, "还原完成后程序没有退出")
    }

    func testQuitDialog_ConfirmFails_DialogClosesAndFailureShown() async {
        let (model, core, terminations) = makeModel(responses: ["shutdown": .failure(.remote(code: "internal_error", keys: []))])
        let presenter = DialogPresenter(model: model)
        var failedDuringModal = false
        let probe = ModalProbe { probe in
            probe.pressPrimary()
            if probe.pressed && !core.calls("shutdown").isEmpty && model.quitDialogContent == nil {
                failedDuringModal = true
                return true
            }
            return false
        }
        await model.quit()
        await waitUntilFinished(probe)
        withExtendedLifetime(presenter) {}
        XCTAssertTrue(failedDuringModal, "失败后弹窗没有在模态期间关闭")
        XCTAssertEqual(terminations.value, 0)
        XCTAssertFalse(probe.timedOut)
    }

    func testUninstallDialog_ConfirmWhileModalOpen_PrepareUninstallStartsBeforeModalEnds() async {
        let (model, core, _) = makeModel(responses: ["prepare_uninstall": blockedKeys])
        let presenter = DialogPresenter(model: model)
        var startedDuringModal = false
        let probe = ModalProbe { probe in
            probe.pressPrimary()
            if probe.pressed && !core.calls("prepare_uninstall").isEmpty {
                startedDuringModal = true
                return true
            }
            return false
        }
        await model.uninstall()
        await waitUntilFinished(probe)
        withExtendedLifetime(presenter) {}
        XCTAssertTrue(probe.pressed, "卸载按钮没找到或没按下")
        XCTAssertTrue(startedDuringModal, "弹窗开着期间没有开始卸载")
    }

    func testUninstallPaused_RecheckWhileModalOpen_StartsRecheckBeforeModalEnds() async {
        let (model, core, _) = makeModel(responses: ["prepare_uninstall": blockedKeys])
        await model.uninstall()
        await model.confirmPending()
        guard case .paused = model.uninstallPhase else { return XCTFail("uninstall did not pause") }
        let callsBefore = core.calls("prepare_uninstall").count
        var recheckedDuringModal = false
        let probe = ModalProbe { probe in
            probe.pressPrimary()
            if probe.pressed && core.calls("prepare_uninstall").count > callsBefore {
                recheckedDuringModal = true
                return true
            }
            return false
        }
        let presenter = DialogPresenter(model: model)
        await waitUntilFinished(probe)
        withExtendedLifetime(presenter) {}
        XCTAssertTrue(probe.pressed, "重新检查按钮没找到或没按下")
        XCTAssertTrue(recheckedDuringModal, "弹窗开着期间没有开始重新检测")
    }

    func testQuitDialog_WhileOpen_NewMainQueueWorkRuns() async {
        let (model, _, _) = makeModel()
        let presenter = DialogPresenter(model: model)
        var scheduledAt: TimeInterval?
        var taskRan = false
        var dispatchRan = false
        let probe = ModalProbe(deadline: 2) { probe in
            guard probe.panel != nil else { return false }
            guard let scheduled = scheduledAt else {
                scheduledAt = probe.elapsed
                Task { @MainActor in taskRan = true }
                DispatchQueue.main.async { dispatchRan = true }
                return false
            }
            return taskRan && dispatchRan || probe.elapsed - scheduled > 0.5
        }
        await model.quit()
        await waitUntilFinished(probe)
        withExtendedLifetime(presenter) {}
        XCTAssertTrue(taskRan, "弹窗开着期间 Task 没有执行")
        XCTAssertTrue(dispatchRan, "弹窗开着期间 DispatchQueue.main.async 没有执行（默认焦点和弹窗调整大小都靠它）")
    }

    func testUninstallDialog_ContentGrowsWhileModalOpen_PanelResizesToFit() async {
        let (model, core, _) = makeModel(responses: ["prepare_uninstall": blockedKeys])
        let presenter = DialogPresenter(model: model)
        var confirmHeight: CGFloat = 0
        var resized = false
        let probe = ModalProbe { probe in
            guard let panel = probe.panel, let content = panel.contentView else { return false }
            if !probe.pressed {
                confirmHeight = panel.frame.height
                probe.pressPrimary()
                return false
            }
            guard !core.calls("prepare_uninstall").isEmpty, probe.elapsed > 1 else { return false }
            resized = content.fittingSize.height > confirmHeight && abs(panel.frame.height - content.fittingSize.height) < 1
            return true
        }
        await model.uninstall()
        await waitUntilFinished(probe)
        withExtendedLifetime(presenter) {}
        XCTAssertTrue(resized, "内容变高后弹窗没有跟着调整大小")
    }
}
