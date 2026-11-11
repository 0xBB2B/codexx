import XCTest
@testable import ModelBridge

final class CallLog: @unchecked Sendable {
    private let lock = NSLock()
    private var items: [String] = []

    func append(_ entry: String) {
        lock.lock()
        items.append(entry)
        lock.unlock()
    }

    var entries: [String] {
        lock.lock()
        defer { lock.unlock() }
        return items
    }
}

actor Gate {
    private var opened = false
    private var waiters: [CheckedContinuation<Void, Never>] = []

    func wait() async {
        if opened { return }
        await withCheckedContinuation { waiters.append($0) }
    }

    func open() {
        opened = true
        waiters.forEach { $0.resume() }
        waiters = []
    }
}

final class FakeCore: CoreClient, @unchecked Sendable {
    struct Call: Equatable, Sendable {
        let method: String
        let params: String
    }

    private let lock = NSLock()
    private var recorded: [Call] = []
    private let log: CallLog
    private let responses: [String: Result<Data, CoreError>]
    private let gates: [String: Gate]
    private let statusStream: AsyncStream<CoreStatus>
    private let lifecycleStream: AsyncStream<CoreLifecycleEvent>
    let statusContinuation: AsyncStream<CoreStatus>.Continuation
    let lifecycleContinuation: AsyncStream<CoreLifecycleEvent>.Continuation

    init(log: CallLog, responses: [String: Result<Data, CoreError>] = [:], gates: [String: Gate] = [:]) {
        self.log = log
        self.responses = responses
        self.gates = gates
        (statusStream, statusContinuation) = AsyncStream.makeStream()
        (lifecycleStream, lifecycleContinuation) = AsyncStream.makeStream()
    }

    var calls: [Call] {
        lock.lock()
        defer { lock.unlock() }
        return recorded
    }

    func calls(_ method: String) -> [Call] { calls.filter { $0.method == method } }

    private func record(_ call: Call) {
        lock.lock(); defer { lock.unlock() }
        recorded.append(call)
    }

    func statusEvents() -> AsyncStream<CoreStatus> { statusStream }
    func lifecycleEvents() -> AsyncStream<CoreLifecycleEvent> { lifecycleStream }

    func call<P: Encodable & Sendable>(_ method: String, params: P) async throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        let json = String(decoding: try encoder.encode(params), as: UTF8.self)
        record(Call(method: method, params: json))
        log.append("core:\(method)")
        if let gate = gates[method] { await gate.wait() }
        switch responses[method] {
        case .success(let data): return data
        case .failure(let error): throw error
        case nil: return Data("{}".utf8)
        }
    }
}

struct Boom: Error {}

final class TerminationCounter: @unchecked Sendable {
    private let lock = NSLock()
    private var n = 0
    func increment() { lock.lock(); n += 1; lock.unlock() }
    var value: Int { lock.lock(); defer { lock.unlock() }; return n }
}

final class FakeTrash: AppTrashing, @unchecked Sendable {
    private let lock = NSLock()
    private var urls: [URL] = []
    private let error: Error?
    init(error: Error? = nil) { self.error = error }

    var trashed: [URL] { lock.lock(); defer { lock.unlock() }; return urls }

    private func record(_ url: URL) {
        lock.lock()
        urls.append(url)
        lock.unlock()
    }

    func moveToTrash(_ url: URL) async -> Error? {
        record(url)
        return error
    }
}

final class FakeKeychain: KeyStoring, @unchecked Sendable {
    private let log: CallLog
    private let lock = NSLock()
    private var stored: String?
    private let loadError: KeychainError?
    private let failing: Set<String>
    private var loads = 0

    init(log: CallLog, stored: String? = nil, loadError: KeychainError? = nil, failing: Set<String> = []) {
        self.log = log
        self.stored = stored
        self.loadError = loadError
        self.failing = failing
    }

    var loadCount: Int {
        lock.lock()
        defer { lock.unlock() }
        return loads
    }

    func save(_ value: String) throws {
        log.append("keychain.save:\(value)")
        if failing.contains("keychain.save") { throw Boom() }
        lock.lock()
        stored = value
        lock.unlock()
    }

    func load() throws -> String? {
        lock.lock()
        loads += 1
        lock.unlock()
        if let loadError { throw loadError }
        lock.lock()
        defer { lock.unlock() }
        return stored
    }

    func delete() throws {
        log.append("keychain.delete")
        if failing.contains("keychain.delete") { throw Boom() }
    }
}

final class FakeSettings: SettingsStoring, @unchecked Sendable {
    private let log: CallLog
    private let failing: Set<String>
    init(log: CallLog, failing: Set<String> = []) {
        self.log = log
        self.failing = failing
    }

    func loadBaseURL() throws -> String? { nil }

    func saveBaseURL(_ baseURL: String) throws {
        log.append("settings.saveBaseURL:\(baseURL)")
        if failing.contains("settings.saveBaseURL") { throw Boom() }
    }

    func deleteAll() throws {
        log.append("settings.deleteAll")
        if failing.contains("settings.deleteAll") { throw Boom() }
    }
}

final class FakeLoginItem: LoginItemControlling, @unchecked Sendable {
    private let log: CallLog
    private let failing: Set<String>
    init(log: CallLog, failing: Set<String> = []) {
        self.log = log
        self.failing = failing
    }

    func enable() throws {
        log.append("login.enable")
        if failing.contains("login.enable") { throw Boom() }
    }

    func disable() throws {
        log.append("login.disable")
        if failing.contains("login.disable") { throw Boom() }
    }
}

final class FakeChatGPT: ChatGPTControlling, @unchecked Sendable {
    private let log: CallLog
    private let running: Bool
    private let result: ChatGPTRestartResult

    init(log: CallLog, running: Bool, result: ChatGPTRestartResult = .restarted) {
        self.log = log
        self.running = running
        self.result = result
    }

    func isRunning() -> Bool { running }

    func restart() async -> ChatGPTRestartResult {
        log.append("chatgpt.restart")
        return result
    }
}

private let exampleURL = "https://relay.example.com"
private let exampleKey = "sk-example-0000"

private func resultJSON(_ text: String) -> Result<Data, CoreError> { .success(Data(text.utf8)) }

private func saveResult(changed: Bool, count: Int = 6) -> Result<Data, CoreError> {
    resultJSON(#"{"models_changed":\#(changed),"relay_model_count":\#(count)}"#)
}

private func statusResult(forwarding: Bool) -> Result<Data, CoreError> {
    resultJSON("""
    {"version":"1.2.0","forwarding":\(forwarding),"proxy":{"running":true,"error":null},\
    "chatgpt":{"found":true,"error":null},\
    "relay":{"model_count":6,"fetched_at":"2026-11-11T00:12:00Z","stale":false,"error":null},\
    "taken_over":[],"restore_blocked":[],"config_path":"/Users/example/.codex/config.toml",\
    "update":{"latest_version":null,"url":null}}
    """)
}

private func remote(_ code: String, keys: [String] = []) -> Result<Data, CoreError> {
    .failure(.remote(code: code, keys: keys))
}

private func makeStatus(
    forwarding: Bool = true,
    proxyError: String? = nil,
    chatgptFound: Bool = true,
    relay: CoreStatus.Relay = .init(modelCount: 6, fetchedAt: "2026-11-11T00:12:00Z", stale: false, error: nil),
    takenOver: [String] = [],
    restoreBlocked: [String] = [],
    configPath: String? = "/Users/example/.codex/config.toml"
) -> CoreStatus {
    CoreStatus(
        version: "1.2.0",
        forwarding: forwarding,
        proxy: .init(running: proxyError == nil, error: proxyError),
        chatgpt: .init(found: chatgptFound, error: nil),
        relay: relay,
        takenOver: takenOver,
        restoreBlocked: restoreBlocked,
        configPath: configPath,
        update: .init(latestVersion: nil, url: nil)
    )
}

@MainActor
private struct Harness {
    let log: CallLog
    let core: FakeCore
    let keychain: FakeKeychain
    let trash: FakeTrash
    let terminations = TerminationCounter()
    let model: AppModel

    init(
        responses: [String: Result<Data, CoreError>] = [:],
        gates: [String: Gate] = [:],
        chatGPTRunning: Bool = false,
        restartResult: ChatGPTRestartResult = .restarted,
        keychainLoadError: KeychainError? = nil,
        status: CoreStatus? = nil,
        failing: Set<String> = [],
        trashError: Error? = nil
    ) {
        log = CallLog()
        core = FakeCore(log: log, responses: responses, gates: gates)
        keychain = FakeKeychain(log: log, loadError: keychainLoadError, failing: failing)
        trash = FakeTrash(error: trashError)
        model = AppModel(
            core: core,
            keychain: keychain,
            settings: FakeSettings(log: log, failing: failing),
            loginItem: FakeLoginItem(log: log, failing: failing),
            chatGPT: FakeChatGPT(log: log, running: chatGPTRunning, result: restartResult),
            trash: trash,
            appURL: URL(fileURLWithPath: "/Applications/ModelBridge.app")
        )
        model.status = status
        let terminations = terminations
        model.onTerminate = { terminations.increment() }
    }

    func fillInputs() {
        model.baseURL = exampleURL
        model.apiKey = exampleKey
    }
}

@MainActor
private func waitUntil(timeout: TimeInterval = 2, _ condition: () -> Bool) async {
    let deadline = Date().addingTimeInterval(timeout)
    while !condition(), Date() < deadline {
        try? await Task.sleep(nanoseconds: 10_000_000)
    }
}

@MainActor
final class AppModelTests: XCTestCase {
    // MARK: completeSetup

    func testCompleteSetup_EmptyBaseURL_FieldErrorAndNoCoreCall() async {
        let h = Harness()
        h.model.apiKey = exampleKey
        await h.model.completeSetup()
        XCTAssertNotNil(h.model.setupFieldErrors[.baseURL])
        XCTAssertNil(h.model.setupFieldErrors[.apiKey])
        XCTAssertEqual(h.log.entries, [])
    }

    func testCompleteSetup_EmptyAPIKey_FieldErrorAndNoCoreCall() async {
        let h = Harness()
        h.model.baseURL = exampleURL
        await h.model.completeSetup()
        XCTAssertNotNil(h.model.setupFieldErrors[.apiKey])
        XCTAssertNil(h.model.setupFieldErrors[.baseURL])
        XCTAssertEqual(h.log.entries, [])
    }

    func testCompleteSetup_BothEmpty_BothFieldErrors() async {
        let h = Harness()
        await h.model.completeSetup()
        XCTAssertNotNil(h.model.setupFieldErrors[.baseURL])
        XCTAssertNotNil(h.model.setupFieldErrors[.apiKey])
        XCTAssertEqual(h.log.entries, [])
    }

    func testCompleteSetup_NonHTTPSBaseURL_FieldErrorAndNoCoreCall() async {
        let h = Harness()
        h.model.baseURL = "http://relay.example.com"
        h.model.apiKey = exampleKey
        await h.model.completeSetup()
        XCTAssertEqual(h.model.setupFieldErrors[.baseURL], "Base URL 要以 https:// 开头。")
        XCTAssertEqual(h.log.entries, [])
    }

    func testCompleteSetup_Relay401_TaskErrorAndNothingPersisted() async {
        let h = Harness(responses: ["save_connection": remote("relay_401")])
        h.fillInputs()
        await h.model.completeSetup()
        XCTAssertEqual(
            h.model.setupPhase,
            .failed(title: "没能连上中台", detail: CoreProtocol.errorMessages["relay_401"]!)
        )
        XCTAssertEqual(h.log.entries, ["core:save_connection"])
    }

    func testCompleteSetup_Success_RunsStepsInOrder() async {
        let h = Harness(responses: [
            "save_connection": saveResult(changed: true, count: 6),
            "set_forwarding": statusResult(forwarding: true),
        ])
        h.fillInputs()
        await h.model.completeSetup()
        XCTAssertEqual(h.log.entries, [
            "core:save_connection",
            "keychain.save:\(exampleKey)",
            "settings.saveBaseURL:\(exampleURL)",
            "core:set_forwarding",
            "login.enable",
        ])
        XCTAssertEqual(h.core.calls("save_connection").first?.params, #"{"api_key":"sk-example-0000","base_url":"https://relay.example.com"}"#)
        XCTAssertEqual(h.core.calls("set_forwarding").first?.params, #"{"enabled":true}"#)
    }

    func testCompleteSetup_Success_ShowsCompletionWithModelCount() async {
        let h = Harness(responses: [
            "save_connection": saveResult(changed: true, count: 12),
            "set_forwarding": statusResult(forwarding: true),
        ])
        h.fillInputs()
        await h.model.completeSetup()
        XCTAssertEqual(h.model.setupPhase, .succeeded(title: "设置完成，转发已开启", modelCount: 12))
    }

    func testCompleteSetup_WhileInFlight_ShowsConnectingAndIgnoresSecondSubmit() async {
        let gate = Gate()
        let h = Harness(
            responses: [
                "save_connection": saveResult(changed: true),
                "set_forwarding": statusResult(forwarding: true),
            ],
            gates: ["save_connection": gate]
        )
        h.fillInputs()
        let first = Task { await h.model.completeSetup() }
        await waitUntil { !h.core.calls("save_connection").isEmpty }
        XCTAssertEqual(h.model.setupPhase, .connecting)

        await h.model.completeSetup()
        await gate.open()
        await first.value

        XCTAssertEqual(h.core.calls("save_connection").count, 1)
    }

    // MARK: toggleForwarding

    func testToggleForwarding_ChatGPTRunning_TurnOnAsksConfirmationFirst() async {
        let h = Harness(chatGPTRunning: true, status: makeStatus(forwarding: false))
        await h.model.toggleForwarding(enabled: true)
        XCTAssertEqual(h.model.pendingConfirmation?.title, "打开转发并重启 ChatGPT？")
        XCTAssertEqual(h.log.entries, [])
    }

    func testToggleForwarding_ChatGPTRunning_TurnOffAsksConfirmationFirst() async {
        let h = Harness(chatGPTRunning: true, status: makeStatus(forwarding: true))
        await h.model.toggleForwarding(enabled: false)
        XCTAssertEqual(h.model.pendingConfirmation?.title, "关闭转发并重启 ChatGPT？")
        XCTAssertEqual(h.log.entries, [])
    }

    func testToggleForwarding_ChatGPTRunning_CancelDoesNotCallCore() async {
        let h = Harness(chatGPTRunning: true, status: makeStatus(forwarding: false))
        await h.model.toggleForwarding(enabled: true)
        h.model.cancelPending()
        XCTAssertNil(h.model.pendingConfirmation)
        XCTAssertEqual(h.log.entries, [])
    }

    func testToggleForwarding_ChatGPTRunning_ConfirmSetsForwardingThenRestarts() async {
        let h = Harness(
            responses: ["set_forwarding": statusResult(forwarding: true)],
            chatGPTRunning: true,
            status: makeStatus(forwarding: false)
        )
        await h.model.toggleForwarding(enabled: true)
        await h.model.confirmPending()
        XCTAssertEqual(h.log.entries, ["core:set_forwarding", "chatgpt.restart"])
        XCTAssertEqual(h.core.calls("set_forwarding").first?.params, #"{"enabled":true}"#)
        XCTAssertNil(h.model.pendingConfirmation)
    }

    func testToggleForwarding_ChatGPTNotRunning_CallsCoreDirectlyWithoutRestart() async {
        let h = Harness(
            responses: ["set_forwarding": statusResult(forwarding: false)],
            chatGPTRunning: false,
            status: makeStatus(forwarding: true)
        )
        await h.model.toggleForwarding(enabled: false)
        XCTAssertNil(h.model.pendingConfirmation)
        XCTAssertEqual(h.log.entries, ["core:set_forwarding"])
        XCTAssertEqual(h.core.calls("set_forwarding").first?.params, #"{"enabled":false}"#)
    }

    func testToggleForwarding_RestoreBlocked_RecordsCodeAndKeys() async {
        let h = Harness(
            responses: ["set_forwarding": remote("restore_blocked", keys: ["openai_base_url", "model_provider"])],
            chatGPTRunning: false,
            status: makeStatus(forwarding: true)
        )
        await h.model.toggleForwarding(enabled: false)
        XCTAssertEqual(
            h.model.forwardingFailure,
            CoreFailure(code: "restore_blocked", keys: ["openai_base_url", "model_provider"])
        )
    }

    // MARK: ChatGPT 重启结果

    func testRestartResult_ShowsNoticeForEachFailure() async {
        let cases: [(ChatGPTRestartResult, String)] = [
            (.timedOut, "ChatGPT 没有退出"),
            (.openFailed, "没能打开 ChatGPT，请手动打开"),
            (.notFound, "没有找到 ChatGPT"),
        ]
        for (result, title) in cases {
            let h = Harness(
                responses: ["set_forwarding": statusResult(forwarding: true)],
                chatGPTRunning: true,
                restartResult: result,
                status: makeStatus(forwarding: false)
            )
            await h.model.toggleForwarding(enabled: true)
            await h.model.confirmPending()
            XCTAssertEqual(h.model.notice?.title, title, "\(result)")
        }
    }

    // MARK: saveConnection

    func testSaveConnection_Failure_ShowsNotSavedKeepsInputsAndSkipsKeychain() async {
        let h = Harness(responses: ["save_connection": remote("relay_401")], chatGPTRunning: true)
        h.fillInputs()
        await h.model.saveConnection()
        XCTAssertEqual(h.model.notice?.title, "没有保存")
        XCTAssertEqual(h.model.notice?.detail, CoreProtocol.errorMessages["relay_401"])
        XCTAssertEqual(h.model.baseURL, exampleURL)
        XCTAssertEqual(h.model.apiKey, exampleKey)
        XCTAssertEqual(h.log.entries, ["core:save_connection"])
    }

    func testSaveConnection_ModelsChangedAndChatGPTRunning_AsksWhetherToRestart() async {
        let h = Harness(responses: ["save_connection": saveResult(changed: true)], chatGPTRunning: true)
        h.fillInputs()
        await h.model.saveConnection()
        XCTAssertEqual(h.log.entries, [
            "core:save_connection",
            "keychain.save:\(exampleKey)",
            "settings.saveBaseURL:\(exampleURL)",
        ])
        let pending = h.model.pendingConfirmation
        XCTAssertEqual(pending?.title, "已保存，重启 ChatGPT 以更新模型列表？")
        XCTAssertEqual(pending?.cancelLabel, "稍后")
        XCTAssertEqual(pending?.confirmLabel, "立即重启")
    }

    func testSaveConnection_RestartPrompt_ConfirmRestartsAndLaterDoesNot() async {
        let now = Harness(responses: ["save_connection": saveResult(changed: true)], chatGPTRunning: true)
        now.fillInputs()
        await now.model.saveConnection()
        await now.model.confirmPending()
        XCTAssertEqual(now.log.entries.last, "chatgpt.restart")

        let later = Harness(responses: ["save_connection": saveResult(changed: true)], chatGPTRunning: true)
        later.fillInputs()
        await later.model.saveConnection()
        later.model.cancelPending()
        XCTAssertFalse(later.log.entries.contains("chatgpt.restart"))
    }

    func testSaveConnection_ModelsUnchanged_ShowsNoRestartNeeded() async {
        let h = Harness(responses: ["save_connection": saveResult(changed: false)], chatGPTRunning: true)
        h.fillInputs()
        await h.model.saveConnection()
        XCTAssertEqual(h.model.notice?.title, "已保存，不需要重启 ChatGPT")
        XCTAssertNil(h.model.pendingConfirmation)
        XCTAssertTrue(h.log.entries.contains("keychain.save:\(exampleKey)"))
    }

    func testSaveConnection_ModelsChangedButChatGPTNotRunning_DoesNotAskRestart() async {
        let h = Harness(responses: ["save_connection": saveResult(changed: true)], chatGPTRunning: false)
        h.fillInputs()
        await h.model.saveConnection()
        XCTAssertNil(h.model.pendingConfirmation)
    }

    // MARK: setLoginStart

    func testSetLoginStart_UncheckWhileForwardingOn_AsksThenDisablesOnConfirm() async {
        let h = Harness(status: makeStatus(forwarding: true))
        await h.model.setLoginStart(false)
        XCTAssertEqual(h.model.pendingConfirmation?.title, "关闭「登录时启动」？")
        XCTAssertEqual(h.log.entries, [])
        await h.model.confirmPending()
        XCTAssertEqual(h.log.entries, ["login.disable"])
    }

    func testSetLoginStart_UncheckWhileForwardingOn_CancelDoesNotDisable() async {
        let h = Harness(status: makeStatus(forwarding: true))
        await h.model.setLoginStart(false)
        h.model.cancelPending()
        XCTAssertEqual(h.log.entries, [])
    }

    func testSetLoginStart_UncheckWhileForwardingOff_DisablesDirectly() async {
        let h = Harness(status: makeStatus(forwarding: false))
        await h.model.setLoginStart(false)
        XCTAssertNil(h.model.pendingConfirmation)
        XCTAssertEqual(h.log.entries, ["login.disable"])
    }

    func testSetLoginStart_Check_EnablesDirectly() async {
        let h = Harness(status: makeStatus(forwarding: true))
        await h.model.setLoginStart(true)
        XCTAssertNil(h.model.pendingConfirmation)
        XCTAssertEqual(h.log.entries, ["login.enable"])
    }

    // MARK: quit

    func testQuit_ForwardingOn_AsksThenShutsDownWithRestore() async {
        let h = Harness(status: makeStatus(forwarding: true))
        await h.model.quit()
        XCTAssertEqual(h.model.pendingConfirmation?.title, "退出模型接入助手？")
        XCTAssertEqual(h.log.entries, [])
        await h.model.confirmPending()
        XCTAssertEqual(h.core.calls("shutdown").map(\.params), [#"{"restore":true}"#])
    }

    func testQuit_ForwardingOff_ShutsDownWithoutRestoreDirectly() async {
        let h = Harness(status: makeStatus(forwarding: false))
        await h.model.quit()
        XCTAssertNil(h.model.pendingConfirmation)
        XCTAssertEqual(h.core.calls("shutdown").map(\.params), [#"{"restore":false}"#])
    }

    // MARK: uninstall

    func testUninstall_AsksConfirmationBeforeCallingCore() async {
        let h = Harness()
        await h.model.uninstall()
        XCTAssertEqual(h.model.pendingConfirmation?.kind, .uninstall)
        XCTAssertEqual(h.log.entries, [])
    }

    func testUninstall_RestoreBlocked_PausesAndCleansNothing() async {
        let h = Harness(
            responses: ["prepare_uninstall": remote("restore_blocked", keys: ["openai_base_url"])],
            status: makeStatus(configPath: "/Users/example/.codex/config.toml")
        )
        await h.model.uninstall()
        await h.model.confirmPending()
        XCTAssertEqual(
            h.model.uninstallPhase,
            .paused(keys: ["openai_base_url"], configPath: "/Users/example/.codex/config.toml")
        )
        XCTAssertEqual(h.log.entries, ["core:prepare_uninstall"])
    }

    func testUninstall_Success_CleansKeychainSettingsAndLoginItem() async {
        let h = Harness(responses: ["prepare_uninstall": resultJSON("{}")])
        await h.model.uninstall()
        await h.model.confirmPending()
        XCTAssertEqual(h.model.uninstallPhase, .finished)
        XCTAssertEqual(h.log.entries.first, "core:prepare_uninstall")
        XCTAssertEqual(Set(h.log.entries.dropFirst()), ["keychain.delete", "settings.deleteAll", "login.disable"])
        XCTAssertEqual(h.log.entries.count, 4)
    }

    // MARK: checkUpdate

    func testCheckUpdate_Newer_SetsAvailableUpdate() async {
        let h = Harness(responses: [
            "check_update": resultJSON(#"{"latest_version":"1.3.0","url":"https://relay.example.com/releases/1.3.0","is_newer":true}"#),
        ])
        await h.model.checkUpdate()
        XCTAssertEqual(h.core.calls("check_update").first?.params, #"{"manual":true}"#)
        XCTAssertEqual(h.model.availableUpdate, UpdateInfo(version: "1.3.0", url: "https://relay.example.com/releases/1.3.0"))
    }

    func testCheckUpdate_NotNewer_ShowsAlreadyLatest() async {
        let h = Harness(responses: [
            "check_update": resultJSON(#"{"latest_version":"1.2.0","url":null,"is_newer":false}"#),
        ])
        await h.model.checkUpdate()
        XCTAssertEqual(h.model.notice?.title, "已是最新版本")
        XCTAssertNil(h.model.availableUpdate)
    }

    func testCheckUpdate_Unavailable_ShowsRetryLaterNotice() async {
        let h = Harness(responses: ["check_update": remote("update_unavailable")])
        await h.model.checkUpdate()
        XCTAssertEqual(h.model.notice?.title, "暂时无法检查更新，请稍后再试")
    }

    // MARK: banners

    func testBanners_HealthyStatus_Empty() {
        let h = Harness(status: makeStatus())
        XCTAssertEqual(h.model.banners, [])
    }

    func testBanners_NoStatus_Empty() {
        let h = Harness()
        XCTAssertEqual(h.model.banners, [])
    }

    func testBanners_TakenOver_Info() {
        let h = Harness(status: makeStatus(takenOver: ["openai_base_url"]))
        XCTAssertEqual(h.model.banners.map(\.title), ["已接管 Codex 原有的接入设置"])
        XCTAssertEqual(h.model.banners.first?.kind, .info)
    }

    func testBanners_RelayStale_Warning() {
        let relay = CoreStatus.Relay(modelCount: 6, fetchedAt: "2026-11-11T00:12:00Z", stale: true, error: nil)
        let h = Harness(status: makeStatus(relay: relay))
        XCTAssertEqual(h.model.banners.map(\.title), ["中台模型列表本次没有更新"])
        XCTAssertEqual(h.model.banners.first?.kind, .warning)
    }

    func testBanners_RelayNeverSucceeded_Error() {
        let relay = CoreStatus.Relay(modelCount: 0, fetchedAt: nil, stale: false, error: "relay_unreachable")
        let h = Harness(status: makeStatus(relay: relay))
        XCTAssertEqual(h.model.banners.map(\.title), ["还没有获取到中台模型"])
        XCTAssertEqual(h.model.banners.first?.kind, .error)
    }

    func testBanners_RelayErrorWithKnownModels_NotNeverSucceeded() {
        let relay = CoreStatus.Relay(modelCount: 6, fetchedAt: "2026-11-11T00:12:00Z", stale: false, error: "relay_unreachable")
        let h = Harness(status: makeStatus(relay: relay))
        XCTAssertFalse(h.model.banners.map(\.title).contains("还没有获取到中台模型"))
    }

    func testBanners_RestoreBlocked_ErrorWithKeysAndConfigPath() {
        let h = Harness(status: makeStatus(restoreBlocked: ["openai_base_url", "model_provider"], configPath: "/Users/example/.codex/config.toml"))
        XCTAssertEqual(h.model.banners.map(\.title), ["没能自动还原 Codex 配置"])
        let banner = h.model.banners.first
        XCTAssertEqual(banner?.kind, .error)
        XCTAssertEqual(banner?.keys, ["openai_base_url", "model_provider"])
        XCTAssertEqual(banner?.configPath, "/Users/example/.codex/config.toml")
    }

    func testBanners_ProxyError_Error() {
        let h = Harness(status: makeStatus(proxyError: "proxy_port_in_use"))
        XCTAssertEqual(h.model.banners.map(\.title), ["本机转发服务没能启动"])
        XCTAssertEqual(h.model.banners.first?.kind, .error)
    }

    func testBanners_ChatGPTNotFound_Error() {
        let h = Harness(status: makeStatus(chatgptFound: false))
        XCTAssertEqual(h.model.banners.map(\.title), ["没有找到 ChatGPT"])
        XCTAssertEqual(h.model.banners.first?.kind, .error)
    }

    func testBanners_MultipleConditions_AllShown() {
        let h = Harness(status: makeStatus(proxyError: "proxy_port_in_use", chatgptFound: false, takenOver: ["openai_base_url"]))
        XCTAssertEqual(
            Set(h.model.banners.map(\.title)),
            ["已接管 Codex 原有的接入设置", "本机转发服务没能启动", "没有找到 ChatGPT"]
        )
    }

    func testBanners_FollowStatusEvents() async {
        let h = Harness()
        await h.model.start()
        h.core.statusContinuation.yield(makeStatus(chatgptFound: false))
        await waitUntil { !h.model.banners.isEmpty }
        XCTAssertEqual(h.model.banners.map(\.title), ["没有找到 ChatGPT"])
    }

    // MARK: 钥匙串与核心进程

    func testStart_KeychainAccessDenied_AsksToReenterKeyAndOpensMainWindow() async {
        let h = Harness(keychainLoadError: .accessDenied(-25293))
        await h.model.start()
        XCTAssertEqual(h.model.notice?.title, "无法读取已保存的 API Key，请重新输入")
        XCTAssertTrue(h.model.mainWindowRequested)
    }

    func testLifecycleFailed_ShowsBackendNotStartedNotice() async {
        let h = Harness()
        await h.model.start()
        h.core.lifecycleContinuation.yield(.failed)
        await waitUntil { h.model.notice != nil }
        XCTAssertEqual(h.model.notice?.title, "后台服务没能启动，请退出后重新打开")
    }

    // MARK: 不显示模型名

    func testNoModelNamesAnywhereInUI() async {
        let relay = CoreStatus.Relay(modelCount: 2, fetchedAt: "2026-11-11T00:12:00Z", stale: true, error: "example-model-alpha not found")
        let h = Harness(
            responses: [
                "save_connection": resultJSON(#"{"models_changed":true,"relay_model_count":2,"relay_models":["example-model-alpha","example-model-beta"]}"#),
                "set_forwarding": statusResult(forwarding: true),
            ],
            status: makeStatus(relay: relay, takenOver: ["example-model-alpha"])
        )
        h.fillInputs()
        await h.model.completeSetup()
        let rendered = [
            String(describing: h.model.setupPhase),
            String(describing: h.model.notice),
            h.model.modelCountText,
            h.model.banners.map { [$0.title, $0.detail ?? "", $0.configPath ?? ""].joined() }.joined(),
        ].joined()
        XCTAssertFalse(rendered.contains("example-model"))
        XCTAssertFalse(h.model.modelCountText.isEmpty)
    }

    // MARK: 退出时还原失败

    func testQuit_RestoreBlocked_DoesNotTerminateAndShowsRestoreBanner() async {
        let h = Harness(
            responses: ["shutdown": remote("restore_blocked", keys: ["openai_base_url"])],
            status: makeStatus(forwarding: true)
        )
        await h.model.quit()
        await h.model.confirmPending()
        XCTAssertEqual(h.terminations.value, 0)
        XCTAssertEqual(h.model.forwardingFailure, CoreFailure(code: "restore_blocked", keys: ["openai_base_url"]))
        XCTAssertTrue(h.model.mainWindowRequested)
        XCTAssertFalse(h.model.coreRunning)
        let banner = h.model.banners.first { $0.title == "没能自动还原 Codex 配置" }
        XCTAssertEqual(banner?.keys, ["openai_base_url"])
        XCTAssertEqual(banner?.configPath, "/Users/example/.codex/config.toml")
    }

    func testQuit_ShutdownSucceeds_TerminatesAndMarksCoreStopped() async {
        let h = Harness(status: makeStatus(forwarding: false))
        await h.model.quit()
        XCTAssertEqual(h.terminations.value, 1)
        XCTAssertFalse(h.model.coreRunning)
    }

    func testQuit_CoreAlreadyGone_StillTerminates() async {
        let h = Harness(responses: ["shutdown": .failure(.processExited)], status: makeStatus(forwarding: false))
        await h.model.quit()
        XCTAssertEqual(h.terminations.value, 1)
    }

    // MARK: 卸载后移到废纸篓

    func testFinishUninstall_MovesAppToTrashThenTerminates() async {
        let h = Harness()
        await h.model.finishUninstall()
        XCTAssertEqual(h.trash.trashed, [URL(fileURLWithPath: "/Applications/ModelBridge.app")])
        XCTAssertNil(h.model.trashFailurePath)
        XCTAssertEqual(h.terminations.value, 1)
    }

    func testFinishUninstall_TrashFails_RecordsPathAndStillTerminates() async {
        let h = Harness(trashError: Boom())
        await h.model.finishUninstall()
        XCTAssertEqual(h.model.trashFailurePath, "/Applications/ModelBridge.app")
        XCTAssertEqual(h.terminations.value, 1)
    }

    // MARK: 运行状态

    func testRuntimeText_CoreRunning() {
        let h = Harness(status: makeStatus(forwarding: true))
        XCTAssertEqual(h.model.runtimeText, "后台运行中")
        XCTAssertEqual(h.model.menuStatusLine, "转发已开启 · 后台运行中")
        let off = Harness(status: makeStatus(forwarding: false))
        XCTAssertEqual(off.model.menuStatusLine, "转发已关闭 · 后台运行中")
    }

    func testLifecycleFailed_MarksCoreStopped() async {
        let h = Harness(status: makeStatus(forwarding: true))
        await h.model.start()
        h.core.lifecycleContinuation.yield(.failed)
        await waitUntil { !h.model.coreRunning }
        XCTAssertFalse(h.model.coreRunning)
        XCTAssertEqual(h.model.runtimeText, "后台服务已停止")
        XCTAssertEqual(h.model.menuStatusLine, "后台服务已停止")
    }

    func testLifecycleRestarted_MarksCoreRunningAgain() async {
        let h = Harness()
        await h.model.start()
        h.model.coreDidFail()
        h.core.lifecycleContinuation.yield(.restarted)
        await waitUntil { h.model.coreRunning }
        XCTAssertTrue(h.model.coreRunning)
    }

    func testCoreDidFail_StopsCoreAndShowsNotice() {
        let h = Harness()
        h.model.coreDidFail()
        XCTAssertFalse(h.model.coreRunning)
        XCTAssertEqual(h.model.notice?.title, "后台服务没能启动，请退出后重新打开")
    }

    // MARK: 转发开关禁用条件

    func testForwardingToggleDisabled_Conditions() {
        XCTAssertFalse(Harness(status: makeStatus()).model.forwardingToggleDisabled)
        XCTAssertFalse(Harness().model.forwardingToggleDisabled)
        XCTAssertTrue(Harness(status: makeStatus(chatgptFound: false)).model.forwardingToggleDisabled)
        let switching = Harness(status: makeStatus())
        switching.model.isSwitchingForwarding = true
        XCTAssertTrue(switching.model.forwardingToggleDisabled)
        let stopped = Harness(status: makeStatus())
        stopped.model.coreRunning = false
        XCTAssertTrue(stopped.model.forwardingToggleDisabled)
    }

    func testToggleForwarding_Failure_RequestsMainWindow() async {
        let h = Harness(responses: ["set_forwarding": remote("config_invalid", keys: ["model_provider"])], status: makeStatus(forwarding: false))
        await h.model.toggleForwarding(enabled: true)
        XCTAssertTrue(h.model.mainWindowRequested)
    }

    // MARK: 首次设置的后续步骤

    func testCompleteSetup_SetForwardingFails_StillSucceedsAndExplains() async {
        let h = Harness(responses: [
            "save_connection": saveResult(changed: true, count: 6),
            "set_forwarding": remote("config_invalid"),
        ])
        h.fillInputs()
        await h.model.completeSetup()
        XCTAssertEqual(h.model.setupPhase, .succeeded(title: "设置完成，但转发没能开启", modelCount: 6))
        XCTAssertEqual(h.model.setupFollowUpIssues, [CoreProtocol.errorMessages["config_invalid"]!])
        XCTAssertTrue(h.log.entries.contains("login.enable"))
    }

    func testCompleteSetup_LoginEnableFails_StillSucceedsAndExplains() async {
        let h = Harness(
            responses: ["save_connection": saveResult(changed: true), "set_forwarding": statusResult(forwarding: true)],
            failing: ["login.enable"]
        )
        h.fillInputs()
        await h.model.completeSetup()
        XCTAssertEqual(h.model.setupPhase, .succeeded(title: "设置完成，转发已开启", modelCount: 6))
        XCTAssertEqual(h.model.setupFollowUpIssues, ["没能开启登录时启动"])
    }

    func testCompleteSetup_Failure_KeepsInputs() async {
        let h = Harness(responses: ["save_connection": remote("relay_401")])
        h.fillInputs()
        await h.model.completeSetup()
        XCTAssertEqual(h.model.baseURL, exampleURL)
        XCTAssertEqual(h.model.apiKey, exampleKey)
    }

    // MARK: 钥匙串写入失败时回滚

    func testSaveConnection_KeychainWriteFails_RollsBackCoreToOldConnection() async {
        let h = Harness(responses: ["save_connection": saveResult(changed: true)], failing: ["keychain.save"])
        await h.model.start(stored: StoredConnection(baseURL: "https://old.example.com", apiKey: .success("sk-old-0000")))
        h.fillInputs()
        await h.model.saveConnection()
        let saves = h.core.calls("save_connection")
        XCTAssertEqual(saves.count, 2)
        XCTAssertEqual(saves.last?.params, #"{"api_key":"sk-old-0000","base_url":"https://old.example.com"}"#)
        XCTAssertEqual(h.model.notice?.title, "没有保存")
        XCTAssertEqual(h.model.notice?.detail, "无法写入系统钥匙串。")
        XCTAssertFalse(h.log.entries.contains { $0.hasPrefix("settings.saveBaseURL") })
        XCTAssertNil(h.model.pendingConfirmation)
    }

    func testSaveConnection_SettingsWriteFails_RestoresOldKeyInKeychain() async {
        let h = Harness(responses: ["save_connection": saveResult(changed: true)], failing: ["settings.saveBaseURL"])
        await h.model.start(stored: StoredConnection(baseURL: "https://old.example.com", apiKey: .success("sk-old-0000")))
        h.fillInputs()
        await h.model.saveConnection()
        XCTAssertEqual(h.log.entries.filter { $0.hasPrefix("keychain.") }, ["keychain.save:\(exampleKey)", "keychain.save:sk-old-0000"])
        XCTAssertEqual(h.core.calls("save_connection").count, 2)
        XCTAssertEqual(h.model.notice?.detail, "无法写入设置文件。")
    }

    func testCompleteSetup_SettingsWriteFails_DeletesNewKeyFromKeychain() async {
        let h = Harness(responses: ["save_connection": saveResult(changed: true)], failing: ["settings.saveBaseURL"])
        h.fillInputs()
        await h.model.completeSetup()
        XCTAssertEqual(h.log.entries.filter { $0.hasPrefix("keychain.") }, ["keychain.save:\(exampleKey)", "keychain.delete"])
        XCTAssertEqual(h.model.setupPhase, .failed(title: "没有保存", detail: "无法写入设置文件。"))
    }

    func testCompleteSetup_KeychainWriteFails_StaysOnSetupWithoutRollback() async {
        let h = Harness(responses: ["save_connection": saveResult(changed: true)], failing: ["keychain.save"])
        h.fillInputs()
        await h.model.completeSetup()
        XCTAssertEqual(h.model.setupPhase, .failed(title: "没有保存", detail: "无法写入系统钥匙串。"))
        XCTAssertEqual(h.core.calls("save_connection").count, 1)
        XCTAssertTrue(h.core.calls("set_forwarding").isEmpty)
        XCTAssertEqual(h.model.apiKey, exampleKey)
    }

    // MARK: 卸载清理失败

    func testUninstall_CleanupFails_ListsEachLeftover() async {
        let h = Harness(
            responses: ["prepare_uninstall": resultJSON("{}")],
            failing: ["keychain.delete", "login.disable"]
        )
        await h.model.uninstall()
        await h.model.confirmPending()
        XCTAssertEqual(h.model.uninstallPhase, .finished)
        XCTAssertEqual(h.model.uninstallLeftovers, ["钥匙串里的 API Key", "登录时启动"])
    }

    func testUninstall_CleanupSucceeds_NoLeftovers() async {
        let h = Harness(responses: ["prepare_uninstall": resultJSON("{}")])
        await h.model.uninstall()
        await h.model.confirmPending()
        XCTAssertEqual(h.model.uninstallLeftovers, [])
    }

    // MARK: 启动只读一次钥匙串

    func testStart_PreloadedConnection_DoesNotReadKeychainAgain() async {
        let h = Harness()
        await h.model.start(stored: StoredConnection(baseURL: "https://old.example.com", apiKey: .success("sk-old-0000")))
        XCTAssertEqual(h.keychain.loadCount, 0)
        XCTAssertEqual(h.model.baseURL, "https://old.example.com")
        XCTAssertEqual(h.model.apiKey, "sk-old-0000")
    }

    func testStart_PreloadedAccessDenied_AsksToReenterKey() async {
        let h = Harness()
        await h.model.start(stored: StoredConnection(baseURL: nil, apiKey: .failure(KeychainError.accessDenied(-25293))))
        XCTAssertEqual(h.model.notice?.title, "无法读取已保存的 API Key，请重新输入")
        XCTAssertEqual(h.keychain.loadCount, 0)
    }

    // MARK: 设置窗口

    func testNeedsSetup_UntilBaseURLSaved() async {
        let h = Harness(responses: ["save_connection": saveResult(changed: true), "set_forwarding": statusResult(forwarding: true)])
        XCTAssertTrue(h.model.needsSetup)
        h.fillInputs()
        await h.model.completeSetup()
        XCTAssertFalse(h.model.needsSetup)

        let stored = Harness()
        await stored.model.start(stored: StoredConnection(baseURL: "https://old.example.com", apiKey: .success(nil)))
        XCTAssertFalse(stored.model.needsSetup)
    }

    func testFinishSetup_HidesSetupAndResetsPhase() async {
        let h = Harness(responses: ["save_connection": saveResult(changed: true), "set_forwarding": statusResult(forwarding: true)])
        h.model.showSetup = true
        h.fillInputs()
        await h.model.completeSetup()
        h.model.finishSetup()
        XCTAssertFalse(h.model.showSetup)
        XCTAssertEqual(h.model.setupPhase, .idle)
    }

    // MARK: 文案

    func testLoginStartOffConfirmation_Message() async {
        let h = Harness(status: makeStatus(forwarding: true))
        await h.model.setLoginStart(false)
        XCTAssertEqual(
            h.model.pendingConfirmation?.message,
            "关闭后，下次登录电脑时助手不会自动运行。在你手动打开助手之前，Codex 里的官方模型和中台模型都无法使用，因为转发仍然开着。如果以后不打算常驻，建议先关闭转发。"
        )
    }

    func testQuitConfirmation_MessageDependsOnChatGPTRunning() async {
        let running = Harness(chatGPTRunning: true, status: makeStatus(forwarding: true))
        await running.model.quit()
        XCTAssertEqual(
            running.model.pendingConfirmation?.message,
            "退出前会关闭转发，并把 Codex 配置还原成开启前的样子。ChatGPT 正在运行，请在退出后重启 ChatGPT。"
        )
        let idle = Harness(chatGPTRunning: false, status: makeStatus(forwarding: true))
        await idle.model.quit()
        XCTAssertEqual(idle.model.pendingConfirmation?.message, "退出前会关闭转发，并把 Codex 配置还原成开启前的样子。")
    }

    func testSwitchingMessage_DiffersForOnAndOff() async {
        for (enabled, expected) in [(true, "正在打开转发并重启 ChatGPT"), (false, "正在还原 Codex 配置并重启 ChatGPT")] {
            let gate = Gate()
            let h = Harness(
                responses: ["set_forwarding": statusResult(forwarding: enabled)],
                gates: ["set_forwarding": gate],
                chatGPTRunning: true,
                status: makeStatus(forwarding: !enabled)
            )
            await h.model.toggleForwarding(enabled: enabled)
            let task = Task { await h.model.confirmPending() }
            await waitUntil { !h.core.calls("set_forwarding").isEmpty }
            XCTAssertEqual(h.model.switchingMessage, expected)
            await gate.open()
            await task.value
            XCTAssertNil(h.model.switchingMessage)
        }
    }

    func testIsChatGPTRunning_ReflectsSystem() {
        XCTAssertTrue(Harness(chatGPTRunning: true).model.isChatGPTRunning)
        XCTAssertFalse(Harness(chatGPTRunning: false).model.isChatGPTRunning)
    }
}
