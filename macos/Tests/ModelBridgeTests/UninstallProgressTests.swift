import XCTest
@testable import ModelBridge

private final class StepCore: CoreClient, @unchecked Sendable {
    struct Step {
        var result: Result<Data, CoreError> = .success(Data("{}".utf8))
        var gate: Gate?
    }

    private let lock = NSLock()
    private var steps: [Step]
    private var count = 0
    private let stream: AsyncStream<CoreStatus>
    private let lifecycle: AsyncStream<CoreLifecycleEvent>

    init(_ steps: [Step]) {
        self.steps = steps
        (stream, _) = AsyncStream.makeStream()
        (lifecycle, _) = AsyncStream.makeStream()
    }

    var prepareCalls: Int { lock.lock(); defer { lock.unlock() }; return count }

    private func nextStep() -> Step {
        lock.lock()
        defer { lock.unlock() }
        count += 1
        return steps.isEmpty ? Step() : steps.removeFirst()
    }

    func statusEvents() -> AsyncStream<CoreStatus> { stream }
    func lifecycleEvents() -> AsyncStream<CoreLifecycleEvent> { lifecycle }

    func call<P: Encodable & Sendable>(_ method: String, params: P) async throws -> Data {
        guard method == "prepare_uninstall" else { return Data("{}".utf8) }
        let step = nextStep()
        if let gate = step.gate { await gate.wait() }
        return try step.result.get()
    }
}

private let blocked: Result<Data, CoreError> = .failure(.remote(code: "restore_blocked", keys: ["model_provider"]))
private let failed: Result<Data, CoreError> = .failure(.remote(code: "internal_error", keys: []))

@MainActor
final class UninstallProgressTests: XCTestCase {
    private func makeModel(_ steps: [StepCore.Step]) -> (AppModel, StepCore) {
        let log = CallLog()
        let core = StepCore(steps)
        let model = AppModel(
            core: core, keychain: RelayFakeKeychain(log: log), settings: RelayFakeSettings(log: log),
            loginItem: FakeLoginItem(log: log), chatGPT: FakeChatGPT(running: false)
        )
        return (model, core)
    }

    private func startConfirmed(_ model: AppModel, _ core: StepCore) async -> Task<Void, Never> {
        await model.uninstall()
        let task = Task { await model.confirmPending() }
        await awaitUntil { core.prepareCalls >= 1 }
        return task
    }

    func testUninstalling_ConfirmationStaysOpenUntilFinished() async {
        let gate = Gate()
        let (model, core) = makeModel([.init(gate: gate)])
        let task = await startConfirmed(model, core)
        XCTAssertEqual(model.pendingConfirmation?.kind, .uninstall)
        XCTAssertEqual(model.pendingConfirmation?.title, "卸载模型接入助手？")
        await gate.open()
        await task.value
        XCTAssertEqual(model.uninstallPhase, .finished)
        XCTAssertNil(model.pendingConfirmation)
    }

    func testUninstalling_CancelAndSecondConfirmAreIgnored() async {
        let gate = Gate()
        let (model, core) = makeModel([.init(gate: gate)])
        let task = await startConfirmed(model, core)
        model.cancelPending()
        XCTAssertEqual(model.pendingConfirmation?.kind, .uninstall)
        await model.confirmPending()
        XCTAssertEqual(core.prepareCalls, 1)
        await gate.open()
        await task.value
    }

    func testUninstallPaused_ConfirmationClosesAndPausedPhaseIsShown() async {
        let (model, core) = makeModel([.init(result: blocked)])
        let task = await startConfirmed(model, core)
        await task.value
        XCTAssertNil(model.pendingConfirmation)
        XCTAssertEqual(model.uninstallPhase, .paused(keys: ["model_provider"], configPath: nil))
    }

    func testUninstallFailed_ClosesDialogRequestsMainWindowAndShowsErrorNotice() async throws {
        let (model, core) = makeModel([.init(result: failed)])
        let task = await startConfirmed(model, core)
        await task.value
        XCTAssertNil(model.pendingConfirmation)
        XCTAssertEqual(model.uninstallPhase, .idle)
        XCTAssertTrue(model.mainWindowRequested)
        let notice = try XCTUnwrap(model.notices.first { $0.title == "卸载没有完成" })
        XCTAssertEqual(notice.kind, .error)
        XCTAssertFalse((notice.detail ?? "").isEmpty)
    }

    func testRecheckInProgress_PausedPhaseStaysAndCancelUninstallIsIgnored() async {
        let gate = Gate()
        let (model, core) = makeModel([.init(result: blocked), .init(gate: gate)])
        await model.uninstall()
        await model.confirmPending()
        let paused = model.uninstallPhase
        XCTAssertEqual(paused, .paused(keys: ["model_provider"], configPath: nil))
        let recheck = Task { await model.recheckUninstall() }
        await awaitUntil { core.prepareCalls == 2 }
        XCTAssertEqual(model.uninstallPhase, paused)
        model.cancelUninstall()
        XCTAssertEqual(model.uninstallPhase, paused)
        await gate.open()
        await recheck.value
        XCTAssertEqual(model.uninstallPhase, .finished)
    }

    func testRecheckFailed_ClosesPausedDialogRequestsMainWindowAndShowsErrorNotice() async throws {
        let (model, _) = makeModel([.init(result: blocked), .init(result: failed)])
        await model.uninstall()
        await model.confirmPending()
        await model.recheckUninstall()
        XCTAssertEqual(model.uninstallPhase, .idle)
        XCTAssertTrue(model.mainWindowRequested)
        XCTAssertEqual(model.notices.first { $0.title == "卸载没有完成" }?.kind, .error)
    }

    func testRecheckPausedAgain_StaysPaused() async {
        let (model, _) = makeModel([.init(result: blocked), .init(result: blocked)])
        await model.uninstall()
        await model.confirmPending()
        await model.recheckUninstall()
        XCTAssertEqual(model.uninstallPhase, .paused(keys: ["model_provider"], configPath: nil))
    }

    func testUninstalling_ConfirmDialogButtonsDisabledConfirmShowsSpinnerAndEscapeIsInert() async throws {
        let gate = Gate()
        let (model, core) = makeModel([.init(gate: gate)])
        let task = await startConfirmed(model, core)
        let content = try XCTUnwrap(model.uninstallDialogContent)
        XCTAssertEqual(content.title, "卸载模型接入助手？")
        XCTAssertEqual(content.buttons.map(\.label), ["取消", "正在卸载"])
        XCTAssertEqual(content.buttons.map(\.isEnabled), [false, false])
        XCTAssertEqual(content.buttons.map(\.showsSpinner), [false, true])
        XCTAssertNil(content.escapeAction)
        await gate.open()
        await task.value
    }

    func testRecheckInProgress_PausedDialogButtonsDisabledRecheckShowsSpinnerAndEscapeIsInert() async throws {
        let gate = Gate()
        let (model, core) = makeModel([.init(result: blocked), .init(gate: gate)])
        await model.uninstall()
        await model.confirmPending()
        let idle = try XCTUnwrap(model.uninstallDialogContent)
        XCTAssertEqual(idle.buttons.map(\.label), ["取消卸载", "打开所在文件夹", "重新检查"])
        XCTAssertEqual(idle.buttons.map(\.isEnabled), [true, true, true])
        XCTAssertEqual(idle.buttons.map(\.showsSpinner), [false, false, false])
        XCTAssertEqual(idle.escapeAction, .cancelUninstall)
        let recheck = Task { await model.recheckUninstall() }
        await awaitUntil { core.prepareCalls == 2 }
        let busy = try XCTUnwrap(model.uninstallDialogContent)
        XCTAssertEqual(busy.title, idle.title)
        XCTAssertEqual(busy.message, idle.message)
        XCTAssertEqual(busy.alert, idle.alert)
        XCTAssertEqual(busy.buttons.map(\.label), ["取消卸载", "打开所在文件夹", "正在卸载"])
        XCTAssertEqual(busy.buttons.map(\.isEnabled), [false, false, false])
        XCTAssertEqual(busy.buttons.map(\.showsSpinner), [false, false, true])
        XCTAssertNil(busy.escapeAction)
        await gate.open()
        await recheck.value
    }

    func testDialogButton_DefaultsToEnabledWithoutSpinner() {
        let button = DialogButton(label: "好", action: .dismiss, style: .primary)
        XCTAssertTrue(button.isEnabled)
        XCTAssertFalse(button.showsSpinner)
    }
}

@MainActor
final class UninstallPanelSizeTests: XCTestCase {
    func testPanelHeightFollowsContentWhenConfirmSwitchesToPaused() async throws {
        let manyKeys: Result<Data, CoreError> = .failure(.remote(
            code: "restore_blocked", keys: ["model_provider", "chatgpt_base_url", "model_catalog_json", "openai_base_url"]
        ))
        let log = CallLog()
        let model = AppModel(
            core: StepCore([.init(result: manyKeys)]), keychain: RelayFakeKeychain(log: log),
            settings: RelayFakeSettings(log: log), loginItem: FakeLoginItem(log: log), chatGPT: FakeChatGPT(running: false)
        )
        await model.uninstall()
        let panel = DialogPresenter.makeUninstallPanel(model: model, onAction: { _ in }, onClosed: {})
        defer { panel.orderOut(nil) }
        let confirmHeight = panel.frame.height

        await model.confirmPending()
        guard case .paused = model.uninstallPhase else { return XCTFail("uninstall did not pause") }
        let content = try XCTUnwrap(panel.contentView)
        await awaitUntil { panel.frame.height >= content.fittingSize.height }

        XCTAssertGreaterThan(content.fittingSize.height, confirmHeight)
        XCTAssertGreaterThanOrEqual(panel.frame.height, content.fittingSize.height)
    }
}
