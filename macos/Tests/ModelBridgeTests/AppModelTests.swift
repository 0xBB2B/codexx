import Combine
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
    private var responses: [String: Result<Data, CoreError>]
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

    func setResponse(_ method: String, _ result: Result<Data, CoreError>) {
        lock.lock(); defer { lock.unlock() }
        responses[method] = result
    }

    private func response(_ method: String) -> Result<Data, CoreError>? {
        lock.lock(); defer { lock.unlock() }
        return responses[method]
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
        switch response(method) {
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
    private let running: Bool

    init(running: Bool) { self.running = running }

    func isRunning() -> Bool { running }
}

private struct NoticeText: Equatable {
    let title: String
    var detail: String?
    var kind: Notice.Kind
}

@MainActor
private extension AppModel {
    var noticeTexts: [NoticeText] { notices.map { NoticeText(title: $0.title, detail: $0.detail, kind: $0.kind) } }
    var noticeTitles: [String] { notices.map(\.title) }
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

private func quoted(_ value: String?) -> String { value.map { "\"\($0)\"" } ?? "null" }

private func accessStatusJSON(
    forwarding: Bool = true,
    chatgptFound: Bool = true,
    mode: String? = nil,
    target: String? = nil,
    detect: String? = nil
) -> String {
    """
    {"version":"1.2.0","forwarding":\(forwarding),"proxy":{"running":true,"error":null},\
    "chatgpt":{"found":\(chatgptFound),"error":null},\
    "relay":{"model_count":6,"fetched_at":"2026-11-11T00:12:00Z","stale":false,"error":null},\
    "taken_over":[],"restore_blocked":[],"config_path":"/Users/example/.codex/config.toml",\
    "update":{"latest_version":null,"url":null},\
    "access":{"mode":\(quoted(mode)),"target":\(quoted(target)),"detect":\(quoted(detect))}}
    """
}

private func accessResult(
    forwarding: Bool = true,
    mode: String? = nil,
    target: String? = nil,
    detect: String? = nil
) -> Result<Data, CoreError> {
    resultJSON(accessStatusJSON(forwarding: forwarding, mode: mode, target: target, detect: detect))
}

private func accessStatus(
    forwarding: Bool = true,
    chatgptFound: Bool = true,
    mode: String? = nil,
    target: String? = nil,
    detect: String? = nil
) throws -> CoreStatus {
    let json = accessStatusJSON(forwarding: forwarding, chatgptFound: chatgptFound, mode: mode, target: target, detect: detect)
    let decoder = JSONDecoder()
    decoder.keyDecodingStrategy = .convertFromSnakeCase
    return try decoder.decode(CoreStatus.self, from: Data(json.utf8))
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
            chatGPT: FakeChatGPT(running: chatGPTRunning),
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

    func testDismissSetupFailure_ReturnsToIdleAndKeepsInputs() async {
        let h = Harness(responses: ["save_connection": remote("relay_401")])
        h.fillInputs()
        await h.model.completeSetup()
        h.model.dismissSetupFailure()
        XCTAssertEqual(h.model.setupPhase, .idle)
        XCTAssertEqual(h.model.baseURL, exampleURL)
        XCTAssertEqual(h.model.apiKey, exampleKey)
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

    func testToggleForwarding_ChatGPTRunning_TurnOnCallsCoreDirectlyWithoutConfirmOrRestart() async {
        let h = Harness(
            responses: ["set_forwarding": statusResult(forwarding: true)],
            chatGPTRunning: true,
            status: makeStatus(forwarding: false)
        )
        await h.model.toggleForwarding(enabled: true)
        XCTAssertNil(h.model.pendingConfirmation)
        XCTAssertEqual(h.log.entries, ["core:set_forwarding"])
        XCTAssertEqual(h.core.calls("set_forwarding").first?.params, #"{"enabled":true}"#)
    }

    func testToggleForwarding_ChatGPTRunning_TurnOffCallsCoreDirectlyWithoutConfirmOrRestart() async {
        let h = Harness(
            responses: ["set_forwarding": statusResult(forwarding: false)],
            chatGPTRunning: true,
            status: makeStatus(forwarding: true)
        )
        await h.model.toggleForwarding(enabled: false)
        XCTAssertNil(h.model.pendingConfirmation)
        XCTAssertEqual(h.log.entries, ["core:set_forwarding"])
        XCTAssertEqual(h.core.calls("set_forwarding").first?.params, #"{"enabled":false}"#)
    }

    func testToggleForwarding_ChatGPTRunning_TurnOnSuccessShowsRestartToTakeEffectNotice() async {
        let h = Harness(
            responses: ["set_forwarding": statusResult(forwarding: true)],
            chatGPTRunning: true,
            status: makeStatus(forwarding: false)
        )
        await h.model.toggleForwarding(enabled: true)
        XCTAssertEqual(h.model.noticeTexts.last, NoticeText(title: "已打开转发", detail: "重启 ChatGPT 后生效。", kind: .success))
    }

    func testToggleForwarding_ChatGPTRunning_TurnOffSuccessShowsRestartToTakeEffectNotice() async {
        let h = Harness(
            responses: ["set_forwarding": statusResult(forwarding: false)],
            chatGPTRunning: true,
            status: makeStatus(forwarding: true)
        )
        await h.model.toggleForwarding(enabled: false)
        XCTAssertEqual(h.model.noticeTexts.last, NoticeText(title: "已关闭转发", detail: "重启 ChatGPT 后生效。", kind: .success))
    }

    func testToggleForwarding_ChatGPTNotRunning_SuccessShowsNoNotice() async {
        for enabled in [true, false] {
            let h = Harness(
                responses: ["set_forwarding": statusResult(forwarding: enabled)],
                chatGPTRunning: false,
                status: makeStatus(forwarding: !enabled)
            )
            await h.model.toggleForwarding(enabled: enabled)
            XCTAssertTrue(h.model.notices.isEmpty, "enabled=\(enabled)")
        }
    }

    func testToggleForwarding_ChatGPTRunning_FailureShowsNoSuccessNotice() async {
        let h = Harness(
            responses: ["set_forwarding": remote("config_invalid", keys: ["model_provider"])],
            chatGPTRunning: true,
            status: makeStatus(forwarding: false)
        )
        await h.model.toggleForwarding(enabled: true)
        XCTAssertTrue(h.model.notices.isEmpty)
        XCTAssertEqual(h.model.forwardingFailure?.code, "config_invalid")
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

    // MARK: saveConnection

    func testSaveConnection_Failure_ShowsNotSavedKeepsInputsAndSkipsKeychain() async {
        let h = Harness(responses: ["save_connection": remote("relay_401")], chatGPTRunning: true)
        h.fillInputs()
        await h.model.saveConnection()
        XCTAssertEqual(h.model.notices.last?.title, "没有保存")
        XCTAssertEqual(h.model.notices.last?.detail, CoreProtocol.errorMessages["relay_401"])
        XCTAssertEqual(h.model.baseURL, exampleURL)
        XCTAssertEqual(h.model.apiKey, exampleKey)
        XCTAssertEqual(h.log.entries, ["core:save_connection"])
    }

    func testSaveConnection_ModelsChangedAndChatGPTRunning_ShowsRestartNoticeWithoutConfirmOrRestart() async {
        let h = Harness(responses: ["save_connection": saveResult(changed: true, count: 9)], chatGPTRunning: true)
        h.fillInputs()
        await h.model.saveConnection()
        XCTAssertEqual(h.log.entries, [
            "core:save_connection",
            "keychain.save:\(exampleKey)",
            "settings.saveBaseURL:\(exampleURL)",
        ])
        XCTAssertNil(h.model.pendingConfirmation)
        XCTAssertEqual(
            h.model.noticeTexts.last,
            NoticeText(title: "已保存", detail: "现在可用 9 个中台模型。重启 ChatGPT 后，Codex 里会显示新的列表。", kind: .success)
        )
    }

    func testSaveConnection_ModelsUnchanged_ShowsNoRestartNeeded() async {
        let h = Harness(responses: ["save_connection": saveResult(changed: false)], chatGPTRunning: true)
        h.fillInputs()
        await h.model.saveConnection()
        XCTAssertEqual(
            h.model.noticeTexts.last,
            NoticeText(title: "已保存，不需要重启 ChatGPT", detail: "已重新获取中台模型，不需要重启 ChatGPT。", kind: .success)
        )
        XCTAssertNil(h.model.pendingConfirmation)
        XCTAssertTrue(h.log.entries.contains("keychain.save:\(exampleKey)"))
    }

    func testSaveConnection_ModelsChangedButChatGPTNotRunning_ShowsNoRestartNeeded() async {
        let h = Harness(responses: ["save_connection": saveResult(changed: true)], chatGPTRunning: false)
        h.fillInputs()
        await h.model.saveConnection()
        XCTAssertNil(h.model.pendingConfirmation)
        XCTAssertEqual(h.model.notices.last?.title, "已保存，不需要重启 ChatGPT")
        XCTAssertEqual(h.model.notices.last?.kind, .success)
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
        XCTAssertEqual(h.model.notices.last?.title, "已是最新版本")
        XCTAssertNil(h.model.availableUpdate)
    }

    func testCheckUpdate_Unavailable_ShowsRetryLaterNotice() async {
        let h = Harness(responses: ["check_update": remote("update_unavailable")])
        await h.model.checkUpdate()
        XCTAssertEqual(h.model.notices.last?.title, "暂时无法检查更新，请稍后再试")
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

    func testBanners_RestoreBlocked_ShortDetail() {
        let h = Harness(status: makeStatus(restoreBlocked: ["openai_base_url"]))
        XCTAssertEqual(h.model.banners.first?.detail, "有设置项被手动改过。")
    }

    func testBanners_ConfigInvalidWithKeys_OffersDetailAction() async {
        let h = Harness(
            responses: ["set_forwarding": remote("config_invalid", keys: ["model_provider"])],
            status: makeStatus(forwarding: false)
        )
        await h.model.toggleForwarding(enabled: true)
        let banner = h.model.banners.first { $0.keys == ["model_provider"] }
        XCTAssertNotNil(banner)
        XCTAssertNotNil(banner?.action)
        XCTAssertEqual(banner?.configPath, "/Users/example/.codex/config.toml")
    }

    func testDismissBanner_StatusBanners_EachCanBeHidden() async {
        let statuses: [CoreStatus] = [
            makeStatus(takenOver: ["openai_base_url"]),
            makeStatus(relay: .init(modelCount: 6, fetchedAt: "2026-11-11T00:12:00Z", stale: true, error: nil)),
            makeStatus(relay: .init(modelCount: 0, fetchedAt: nil, stale: false, error: "relay_unreachable")),
            makeStatus(restoreBlocked: ["openai_base_url"]),
            makeStatus(proxyError: "proxy_port_in_use"),
            makeStatus(chatgptFound: false),
        ]
        for status in statuses {
            let h = Harness()
            await h.model.start()
            h.core.statusContinuation.yield(status)
            await waitUntil { !h.model.banners.isEmpty }
            let banner = h.model.banners.first
            XCTAssertNotNil(banner)
            h.model.dismissBanner(banner!)
            XCTAssertEqual(h.model.banners, [], "\(banner!.title)")
        }
    }

    func testDismissBanner_ForwardingFailureBanner_CanBeHidden() async {
        let h = Harness(
            responses: ["set_forwarding": remote("config_invalid", keys: ["model_provider"])],
            status: makeStatus(forwarding: false)
        )
        await h.model.toggleForwarding(enabled: true)
        let banner = h.model.banners.first
        XCTAssertNotNil(banner)
        h.model.dismissBanner(banner!)
        XCTAssertEqual(h.model.banners, [])
    }

    func testDismissBanner_SameStateAgain_StaysHidden_ChangedStateShowsAgain() async {
        let taken = makeStatus(takenOver: ["openai_base_url"])
        let h = Harness()
        await h.model.start()
        h.core.statusContinuation.yield(taken)
        await waitUntil { !h.model.banners.isEmpty }
        h.model.dismissBanner(h.model.banners[0])

        h.core.statusContinuation.yield(taken)
        try? await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertEqual(h.model.banners, [])

        h.core.statusContinuation.yield(makeStatus(takenOver: []))
        try? await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertEqual(h.model.banners, [])

        h.core.statusContinuation.yield(taken)
        await waitUntil { !h.model.banners.isEmpty }
        XCTAssertEqual(h.model.banners.map(\.title), ["已接管 Codex 原有的接入设置"])
    }

    func testDismissBanner_RelayStale_StaysHiddenUntilItBecomesStaleAgain() async {
        func relay(stale: Bool) -> CoreStatus {
            makeStatus(relay: .init(modelCount: 6, fetchedAt: "2026-11-11T00:12:00Z", stale: stale, error: nil))
        }
        let h = Harness()
        await h.model.start()
        h.core.statusContinuation.yield(relay(stale: true))
        await waitUntil { !h.model.banners.isEmpty }
        h.model.dismissBanner(h.model.banners[0])

        h.core.statusContinuation.yield(relay(stale: true))
        try? await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertEqual(h.model.banners, [])

        h.core.statusContinuation.yield(relay(stale: false))
        try? await Task.sleep(nanoseconds: 100_000_000)
        h.core.statusContinuation.yield(relay(stale: true))
        await waitUntil { !h.model.banners.isEmpty }
        XCTAssertEqual(h.model.banners.map(\.title), ["中台模型列表本次没有更新"])
    }

    func testDismissBanner_PublishesChange() async {
        let h = Harness()
        await h.model.start()
        h.core.statusContinuation.yield(makeStatus(takenOver: ["openai_base_url"]))
        await waitUntil { !h.model.banners.isEmpty }
        var changes = 0
        let subscription = h.model.objectWillChange.sink { changes += 1 }
        h.model.dismissBanner(h.model.banners[0])
        XCTAssertGreaterThan(changes, 0)
        subscription.cancel()
    }

    // MARK: 钥匙串与核心进程

    func testStart_KeychainAccessDenied_AsksToReenterKeyAndOpensMainWindow() async {
        let h = Harness(keychainLoadError: .accessDenied(-25293))
        await h.model.start()
        XCTAssertEqual(h.model.notices.last?.title, "无法读取已保存的 API Key，请重新输入")
        XCTAssertTrue(h.model.mainWindowRequested)
    }

    func testLifecycleFailed_ShowsBackendNotStartedNotice() async {
        let h = Harness()
        await h.model.start()
        h.core.lifecycleContinuation.yield(.failed)
        await waitUntil { !h.model.notices.isEmpty }
        XCTAssertEqual(h.model.notices.last?.title, "后台服务没能启动，请退出后重新打开")
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
            String(describing: h.model.notices),
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
        XCTAssertEqual(h.model.notices.last?.title, "后台服务没能启动，请退出后重新打开")
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
        XCTAssertEqual(h.model.notices.last?.title, "没有保存")
        XCTAssertEqual(h.model.notices.last?.detail, "无法写入系统钥匙串。")
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
        XCTAssertEqual(h.model.notices.last?.detail, "无法写入设置文件。")
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
        XCTAssertEqual(h.model.notices.last?.title, "无法读取已保存的 API Key，请重新输入")
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

    func testIsChatGPTRunning_ReflectsSystem() {
        XCTAssertTrue(Harness(chatGPTRunning: true).model.isChatGPTRunning)
        XCTAssertFalse(Harness(chatGPTRunning: false).model.isChatGPTRunning)
    }

    // MARK: 接入方式展示

    func testAccessTexts_RelayNotLoggedIn() throws {
        let h = Harness(status: try accessStatus(mode: "relay", target: "relay", detect: "not_logged_in"))
        XCTAssertEqual(h.model.accessModeName, "只用中台")
        XCTAssertEqual(h.model.accessReasonText, "· 没有检测到 ChatGPT 登录")
        XCTAssertEqual(
            h.model.accessHelpText,
            "不需要登录 ChatGPT。模型列表里只有中台模型，默认选中「中台 · auto」。登录 ChatGPT 后点「重新检测」，就能同时用官方模型。"
        )
    }

    func testAccessTexts_BothLoggedIn() throws {
        let h = Harness(status: try accessStatus(mode: "both", target: "both", detect: "logged_in"))
        XCTAssertEqual(h.model.accessModeName, "官方 + 中台")
        XCTAssertEqual(h.model.accessReasonText, "· 已检测到 ChatGPT 登录")
        XCTAssertEqual(h.model.accessHelpText, "模型列表里官方模型在上，中台模型在下；选官方模型时仍然访问官方服务。")
    }

    func testAccessTexts_DetectFailed_KeepsLastWay() throws {
        let h = Harness(status: try accessStatus(mode: "relay", target: "relay", detect: "failed"))
        XCTAssertEqual(h.model.accessReasonText, "· 沿用上次的方式")
    }

    func testBanners_AccessDetectFailed_NoSeparateWarningRow() throws {
        let h = Harness(status: try accessStatus(mode: "relay", target: "relay", detect: "failed"))
        XCTAssertEqual(h.model.banners, [])
    }

    func testAccessModeName_ForwardingOffOrModeNil_IsNil() throws {
        let off = Harness(status: try accessStatus(forwarding: false, mode: "both", target: "both", detect: "logged_in"))
        XCTAssertNil(off.model.accessModeName)
        let noMode = Harness(status: try accessStatus(mode: nil, target: nil, detect: nil))
        XCTAssertNil(noMode.model.accessModeName)
    }

    func testForwardingHelpText_ThreeStates() throws {
        let on = Harness(status: makeStatus(forwarding: true))
        XCTAssertEqual(on.model.forwardingHelpText, "开启时，本助手按下方的接入方式修改 Codex 配置。")
        let off = Harness(status: makeStatus(forwarding: false))
        XCTAssertEqual(off.model.forwardingHelpText, "已关闭：Codex 配置已还原为开启前的样子。")
        let notFound = Harness(status: makeStatus(forwarding: false, chatgptFound: false))
        XCTAssertEqual(notFound.model.forwardingHelpText, "找到 ChatGPT 后才能打开。")
    }

    // MARK: 重新检测接入方式

    func testRecheckAccess_IsDetectingWhileInFlight() async throws {
        let gate = Gate()
        let h = Harness(
            responses: ["detect_access": accessResult(mode: "both", target: "both", detect: "logged_in")],
            gates: ["detect_access": gate],
            status: try accessStatus(mode: "both", target: "both", detect: "logged_in")
        )
        XCTAssertFalse(h.model.isDetectingAccess)
        let task = Task { await h.model.recheckAccess() }
        await waitUntil { !h.core.calls("detect_access").isEmpty }
        XCTAssertTrue(h.model.isDetectingAccess)
        await gate.open()
        await task.value
        XCTAssertFalse(h.model.isDetectingAccess)
    }

    func testRecheckAccess_UnchangedRelay_ShowsNoChangeNotice() async throws {
        let h = Harness(
            responses: ["detect_access": accessResult(mode: "relay", target: "relay", detect: "not_logged_in")],
            status: try accessStatus(mode: "relay", target: "relay", detect: "not_logged_in")
        )
        await h.model.recheckAccess()
        XCTAssertEqual(h.model.notices.last?.title, "接入方式没有变化")
        XCTAssertEqual(h.model.notices.last?.detail, "仍然没有检测到 ChatGPT 登录，继续只用中台模型。")
        XCTAssertEqual(h.model.notices.last?.kind, .info)
        XCTAssertEqual(h.core.calls("apply_access").count, 0)
    }

    func testRecheckAccess_UnchangedBoth_ShowsNoChangeNotice() async throws {
        let h = Harness(
            responses: ["detect_access": accessResult(mode: "both", target: "both", detect: "logged_in")],
            status: try accessStatus(mode: "both", target: "both", detect: "logged_in")
        )
        await h.model.recheckAccess()
        XCTAssertEqual(h.model.notices.last?.title, "接入方式没有变化")
        XCTAssertEqual(h.model.notices.last?.detail, "仍然检测到 ChatGPT 登录，继续同时使用官方模型和中台模型。")
        XCTAssertEqual(h.model.notices.last?.kind, .info)
        XCTAssertEqual(h.core.calls("apply_access").count, 0)
    }

    func testRecheckAccess_DetectFailed_ShowsWarningNoticeAndNoApply() async throws {
        let h = Harness(
            responses: ["detect_access": accessResult(mode: "relay", target: "relay", detect: "failed")],
            status: try accessStatus(mode: "relay", target: "relay", detect: "failed")
        )
        await h.model.recheckAccess()
        XCTAssertEqual(
            h.model.noticeTexts.last,
            NoticeText(title: "没能检测 ChatGPT 登录状态", detail: "沿用上次的方式。可以稍后点「重新检测」再试。", kind: .warning)
        )
        XCTAssertEqual(h.core.calls("apply_access").count, 0)
    }

    func testRecheckAccess_ChangedAndChatGPTNotRunning_AppliesDirectlyWithoutConfirmation() async throws {
        let h = Harness(
            responses: [
                "detect_access": accessResult(mode: "relay", target: "both", detect: "logged_in"),
                "apply_access": accessResult(mode: "both", target: "both", detect: "logged_in"),
            ],
            chatGPTRunning: false,
            status: try accessStatus(mode: "relay", target: "relay", detect: "not_logged_in")
        )
        await h.model.recheckAccess()
        XCTAssertNil(h.model.pendingConfirmation)
        XCTAssertEqual(h.log.entries, ["core:detect_access", "core:apply_access"])
        XCTAssertEqual(h.model.status?.access?.mode, "both")
    }

    func testRecheckAccess_ChangedAndChatGPTNotRunning_ShowsNoNotice() async throws {
        let h = Harness(
            responses: [
                "detect_access": accessResult(mode: "relay", target: "both", detect: "logged_in"),
                "apply_access": accessResult(mode: "both", target: "both", detect: "logged_in"),
            ],
            chatGPTRunning: false,
            status: try accessStatus(mode: "relay", target: "relay", detect: "not_logged_in")
        )
        await h.model.recheckAccess()
        XCTAssertTrue(h.model.notices.isEmpty)
    }

    func testRecheckAccess_ChangedToBothAndChatGPTRunning_AppliesDirectlyAndShowsNotice() async throws {
        let h = Harness(
            responses: [
                "detect_access": accessResult(mode: "relay", target: "both", detect: "logged_in"),
                "apply_access": accessResult(mode: "both", target: "both", detect: "logged_in"),
            ],
            chatGPTRunning: true,
            status: try accessStatus(mode: "relay", target: "relay", detect: "not_logged_in")
        )
        await h.model.recheckAccess()
        XCTAssertNil(h.model.pendingConfirmation)
        XCTAssertEqual(h.log.entries, ["core:detect_access", "core:apply_access"])
        XCTAssertEqual(
            h.model.noticeTexts.last,
            NoticeText(title: "已切换到「官方 + 中台」", detail: "已检测到 ChatGPT 登录。重启 ChatGPT 后生效。", kind: .success)
        )
    }

    func testRecheckAccess_ChangedToRelayAndChatGPTRunning_AppliesDirectlyAndShowsNotice() async throws {
        let h = Harness(
            responses: [
                "detect_access": accessResult(mode: "both", target: "relay", detect: "not_logged_in"),
                "apply_access": accessResult(mode: "relay", target: "relay", detect: "not_logged_in"),
            ],
            chatGPTRunning: true,
            status: try accessStatus(mode: "both", target: "both", detect: "logged_in")
        )
        await h.model.recheckAccess()
        XCTAssertNil(h.model.pendingConfirmation)
        XCTAssertEqual(h.log.entries, ["core:detect_access", "core:apply_access"])
        XCTAssertEqual(
            h.model.noticeTexts.last,
            NoticeText(title: "已切换到「只用中台」", detail: "没有检测到 ChatGPT 登录。重启 ChatGPT 后生效。", kind: .success)
        )
    }

    func testRecheckAccess_ApplyRestoreBlocked_RecordsFailure() async throws {
        let h = Harness(
            responses: [
                "detect_access": accessResult(mode: "relay", target: "both", detect: "logged_in"),
                "apply_access": remote("restore_blocked", keys: ["openai_base_url"]),
            ],
            chatGPTRunning: false,
            status: try accessStatus(mode: "relay", target: "relay", detect: "not_logged_in")
        )
        await h.model.recheckAccess()
        XCTAssertEqual(h.model.forwardingFailure, CoreFailure(code: "restore_blocked", keys: ["openai_base_url"]))
        XCTAssertTrue(h.model.banners.contains { $0.title == "没能自动还原 Codex 配置" })
    }

    func testRecheckAccess_ApplyFailsWhileChatGPTRunning_NoSuccessNoticeAndNoRestart() async throws {
        let h = Harness(
            responses: [
                "detect_access": accessResult(mode: "relay", target: "both", detect: "logged_in"),
                "apply_access": remote("restore_blocked", keys: ["openai_base_url"]),
            ],
            chatGPTRunning: true,
            status: try accessStatus(mode: "relay", target: "relay", detect: "not_logged_in")
        )
        await h.model.recheckAccess()
        XCTAssertEqual(h.model.forwardingFailure?.code, "restore_blocked")
        XCTAssertTrue(h.model.notices.isEmpty)
        XCTAssertNil(h.model.pendingConfirmation)
    }

    func testRecheckAccess_DetectCallFails_ShowsErrorNoticeAndStopsDetecting() async throws {
        let h = Harness(
            responses: ["detect_access": remote("internal_error")],
            status: try accessStatus(mode: "relay", target: "relay", detect: "not_logged_in")
        )
        await h.model.recheckAccess()
        XCTAssertEqual(h.model.notices.last?.title, "没能重新检测")
        XCTAssertEqual(h.model.notices.last?.kind, .error)
        XCTAssertFalse(h.model.isDetectingAccess)
        XCTAssertEqual(h.core.calls("apply_access").count, 0)
    }

    func testStartupAlign_DetectFailed_DoesNotApplyOrAsk() async throws {
        for running in [false, true] {
            let h = Harness(chatGPTRunning: running)
            await h.model.start()
            h.core.statusContinuation.yield(try accessStatus(mode: "relay", target: "both", detect: "failed"))
            h.core.statusContinuation.yield(try accessStatus(chatgptFound: false, mode: "relay", target: "relay", detect: "not_logged_in"))
            await waitUntil { !h.model.banners.isEmpty }
            try? await Task.sleep(nanoseconds: 100_000_000)
            XCTAssertEqual(h.core.calls("apply_access").count, 0, "running=\(running)")
            XCTAssertNil(h.model.pendingConfirmation, "running=\(running)")
        }
    }

    func testStartupAlign_DetectFailedWithDifferentTarget_WarnsOnceAndDoesNotApply() async throws {
        for running in [false, true] {
            let h = Harness(chatGPTRunning: running)
            await h.model.start()
            h.core.statusContinuation.yield(try accessStatus(mode: "relay", target: "both", detect: "failed"))
            await waitUntil { !h.model.notices.isEmpty }
            try? await Task.sleep(nanoseconds: 100_000_000)
            XCTAssertEqual(
                h.model.noticeTexts,
                [NoticeText(title: "没能检测 ChatGPT 登录状态", detail: "沿用上次的方式。可以稍后点「重新检测」再试。", kind: .warning)],
                "running=\(running)"
            )
            XCTAssertEqual(h.core.calls("apply_access").count, 0, "running=\(running)")
        }
    }

    func testStartupAlign_DetectFailedWithSameTarget_AlsoWarns() async throws {
        let h = Harness()
        await h.model.start()
        h.core.statusContinuation.yield(try accessStatus(mode: "relay", target: "relay", detect: "failed"))
        await waitUntil { !h.model.notices.isEmpty }
        XCTAssertEqual(h.model.noticeTitles, ["没能检测 ChatGPT 登录状态"])
        XCTAssertEqual(h.model.notices.last?.kind, .warning)
        XCTAssertEqual(h.core.calls("apply_access").count, 0)
    }

    func testRecheckAccess_RemovesOldDetectFailedWarningBeforeSucceeding() async throws {
        let h = Harness(
            responses: [
                "detect_access": accessResult(mode: "relay", target: "both", detect: "logged_in"),
                "apply_access": accessResult(mode: "both", target: "both", detect: "logged_in"),
            ],
            chatGPTRunning: false
        )
        await h.model.start()
        h.core.statusContinuation.yield(try accessStatus(mode: "relay", target: "both", detect: "failed"))
        await waitUntil { !h.model.notices.isEmpty }
        XCTAssertEqual(h.model.noticeTitles, ["没能检测 ChatGPT 登录状态"])
        await h.model.recheckAccess()
        XCTAssertFalse(h.model.noticeTitles.contains("没能检测 ChatGPT 登录状态"))
        XCTAssertEqual(h.core.calls("apply_access").count, 1)
    }

    func testRecheckAccess_RemovesOldDetectFailedWarningEvenWhenDetectCallFails() async throws {
        let h = Harness(responses: ["detect_access": remote("internal_error")])
        await h.model.start()
        h.core.statusContinuation.yield(try accessStatus(mode: "relay", target: "relay", detect: "failed"))
        await waitUntil { !h.model.notices.isEmpty }
        await h.model.recheckAccess()
        XCTAssertEqual(h.model.noticeTitles, ["没能重新检测"])
    }

    func testRecheckAccess_DetectFailedWithDifferentTarget_WarnsAndDoesNotApplyOrAsk() async throws {
        for running in [false, true] {
            let h = Harness(
                responses: ["detect_access": accessResult(mode: "relay", target: "both", detect: "failed")],
                chatGPTRunning: running,
                status: try accessStatus(mode: "relay", target: "relay", detect: "not_logged_in")
            )
            await h.model.recheckAccess()
            XCTAssertEqual(h.core.calls("apply_access").count, 0, "running=\(running)")
            XCTAssertNil(h.model.pendingConfirmation, "running=\(running)")
            XCTAssertEqual(h.model.notices.last?.title, "没能检测 ChatGPT 登录状态", "running=\(running)")
            XCTAssertEqual(h.model.notices.last?.kind, .warning, "running=\(running)")
        }
    }

    // MARK: 启动后首次对齐接入方式

    func testStartupAlign_FirstStatusMismatch_AppliesOnceAndNotRepeated() async throws {
        let h = Harness(responses: ["apply_access": accessResult(mode: "relay", target: "relay", detect: "not_logged_in")])
        await h.model.start()
        let mismatch = try accessStatus(mode: "both", target: "relay", detect: "not_logged_in")
        h.core.statusContinuation.yield(mismatch)
        await waitUntil { h.core.calls("apply_access").count == 1 }
        XCTAssertEqual(h.core.calls("apply_access").count, 1)
        XCTAssertNil(h.model.pendingConfirmation)

        h.core.statusContinuation.yield(mismatch)
        h.core.statusContinuation.yield(mismatch)
        h.core.statusContinuation.yield(try accessStatus(chatgptFound: false, mode: "both", target: "both", detect: "logged_in"))
        await waitUntil { !h.model.banners.isEmpty }
        try? await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertEqual(h.core.calls("apply_access").count, 1)
    }

    func testStartupAlign_ChatGPTRunning_AppliesWithoutConfirmAndShowsNotice() async throws {
        let h = Harness(
            responses: ["apply_access": accessResult(mode: "relay", target: "relay", detect: "not_logged_in")],
            chatGPTRunning: true
        )
        await h.model.start()
        h.core.statusContinuation.yield(try accessStatus(mode: "both", target: "relay", detect: "not_logged_in"))
        await waitUntil { !h.model.notices.isEmpty }
        XCTAssertNil(h.model.pendingConfirmation)
        XCTAssertEqual(h.core.calls("apply_access").count, 1)
        XCTAssertEqual(
            h.model.noticeTexts.last,
            NoticeText(title: "已切换到「只用中台」", detail: "没有检测到 ChatGPT 登录。重启 ChatGPT 后生效。", kind: .success)
        )
    }

    func testStartupAlign_ChatGPTNotRunning_AppliesWithoutNotice() async throws {
        let h = Harness(
            responses: ["apply_access": accessResult(mode: "both", target: "both", detect: "logged_in")],
            chatGPTRunning: false
        )
        await h.model.start()
        h.core.statusContinuation.yield(try accessStatus(mode: "relay", target: "both", detect: "logged_in"))
        await waitUntil { h.core.calls("apply_access").count == 1 }
        try? await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertTrue(h.model.notices.isEmpty)
    }

    func testStartupAlign_ForwardingOffOrTargetNil_DoesNothing() async throws {
        let h = Harness()
        await h.model.start()
        h.core.statusContinuation.yield(try accessStatus(forwarding: false, mode: "both", target: "relay", detect: "not_logged_in"))
        h.core.statusContinuation.yield(try accessStatus(mode: "both", target: nil, detect: nil))
        h.core.statusContinuation.yield(try accessStatus(chatgptFound: false, mode: "both", target: "both", detect: "logged_in"))
        await waitUntil { !h.model.banners.isEmpty }
        try? await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertEqual(h.core.calls("apply_access").count, 0)
        XCTAssertNil(h.model.pendingConfirmation)
    }

    // MARK: 提示去重与问题解决后移除

    func testSaveConnection_RepeatedFailures_KeepSingleNotice_ThenSuccessRemovesIt() async {
        let h = Harness(responses: ["save_connection": remote("relay_401")])
        h.fillInputs()
        for _ in 0..<3 { await h.model.saveConnection() }
        XCTAssertEqual(h.model.noticeTitles, ["没有保存"])
        h.core.setResponse("save_connection", saveResult(changed: false))
        await h.model.saveConnection()
        XCTAssertEqual(h.model.noticeTitles, ["已保存，不需要重启 ChatGPT"])
    }

    func testRecheckAccess_DetectCallFailure_RemovedByLaterSuccess() async throws {
        let h = Harness(
            responses: ["detect_access": remote("internal_error")],
            status: try accessStatus(mode: "relay", target: "relay", detect: "not_logged_in")
        )
        await h.model.recheckAccess()
        await h.model.recheckAccess()
        XCTAssertEqual(h.model.noticeTitles, ["没能重新检测"])
        h.core.setResponse("detect_access", accessResult(mode: "relay", target: "relay", detect: "not_logged_in"))
        await h.model.recheckAccess()
        XCTAssertEqual(h.model.noticeTitles, ["接入方式没有变化"])
    }

    func testCheckUpdate_FailureWarning_NotStackedAndRemovedByLaterSuccess() async {
        let h = Harness(responses: ["check_update": remote("update_unavailable")])
        await h.model.checkUpdate()
        await h.model.checkUpdate()
        XCTAssertEqual(h.model.noticeTitles, ["暂时无法检查更新，请稍后再试"])
        h.core.setResponse("check_update", resultJSON(#"{"latest_version":"1.2.0","url":null,"is_newer":false}"#))
        await h.model.checkUpdate()
        XCTAssertEqual(h.model.noticeTitles, ["已是最新版本"])
    }

    func testKeychainWarning_RemovedBySuccessfulSave() async {
        let h = Harness(responses: ["save_connection": saveResult(changed: false)])
        await h.model.start(stored: StoredConnection(baseURL: nil, apiKey: .failure(KeychainError.accessDenied(-25293))))
        XCTAssertEqual(h.model.noticeTitles, ["无法读取已保存的 API Key，请重新输入"])
        h.fillInputs()
        await h.model.saveConnection()
        XCTAssertEqual(h.model.noticeTitles, ["已保存，不需要重启 ChatGPT"])
    }

    // MARK: 启动检测失败只针对启动后第一条转发开着的状态

    func testStartupAlign_DetectFailedWarningDismissedByUser_NotAddedBackByLaterStatus() async throws {
        let h = Harness()
        await h.model.start()
        h.core.statusContinuation.yield(try accessStatus(mode: "relay", target: "both", detect: "failed"))
        await waitUntil { !h.model.notices.isEmpty }
        h.model.dismissNotice(h.model.notices[0].id)
        h.core.statusContinuation.yield(try accessStatus(mode: "relay", target: "both", detect: "failed"))
        h.core.statusContinuation.yield(try accessStatus(mode: "relay", target: "relay", detect: "failed"))
        try? await Task.sleep(nanoseconds: 150_000_000)
        XCTAssertTrue(h.model.notices.isEmpty)
    }

    func testStartupAlign_ForwardingOffStatusFirst_ThenForwardingOnFailed_Warns() async throws {
        let h = Harness()
        await h.model.start()
        h.core.statusContinuation.yield(try accessStatus(forwarding: false, mode: "relay", target: "both", detect: "failed"))
        try? await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertTrue(h.model.notices.isEmpty)
        h.core.statusContinuation.yield(try accessStatus(mode: "relay", target: "both", detect: "failed"))
        await waitUntil { !h.model.notices.isEmpty }
        XCTAssertEqual(h.model.noticeTitles, ["没能检测 ChatGPT 登录状态"])
    }

    func testStartupAlign_FirstForwardingOnStatusOK_LaterDetectFailedStatus_NoWarning() async throws {
        let h = Harness()
        await h.model.start()
        h.core.statusContinuation.yield(try accessStatus(mode: "both", target: "both", detect: "logged_in"))
        h.core.statusContinuation.yield(try accessStatus(mode: "both", target: "both", detect: "failed"))
        h.core.statusContinuation.yield(try accessStatus(mode: "relay", target: "both", detect: "failed"))
        try? await Task.sleep(nanoseconds: 150_000_000)
        XCTAssertTrue(h.model.notices.isEmpty)
        XCTAssertEqual(h.core.calls("apply_access").count, 0)
    }

    // MARK: 提示自动消失

    func testNoticeAutoDismissDelay_DefaultsToFourSeconds() {
        XCTAssertEqual(Harness().model.noticeAutoDismissDelay, 4)
    }

    func testNotice_Success_AutoDismissesAfterDelay() async {
        let h = Harness(responses: ["check_update": resultJSON(#"{"latest_version":"1.2.0","url":null,"is_newer":false}"#)])
        h.model.noticeAutoDismissDelay = 0.1
        await h.model.checkUpdate()
        XCTAssertEqual(h.model.notices.last?.kind, .success)
        await waitUntil { h.model.notices.isEmpty }
        XCTAssertTrue(h.model.notices.isEmpty)
    }

    func testNotice_Info_AutoDismissesAfterDelay() async throws {
        let h = Harness(
            responses: ["detect_access": accessResult(mode: "relay", target: "relay", detect: "not_logged_in")],
            status: try accessStatus(mode: "relay", target: "relay", detect: "not_logged_in")
        )
        h.model.noticeAutoDismissDelay = 0.1
        await h.model.recheckAccess()
        XCTAssertEqual(h.model.notices.last?.kind, .info)
        await waitUntil { h.model.notices.isEmpty }
        XCTAssertTrue(h.model.notices.isEmpty)
    }

    func testNotice_Success_StaysUntilDelayElapses() async {
        let h = Harness(responses: ["check_update": resultJSON(#"{"latest_version":"1.2.0","url":null,"is_newer":false}"#)])
        h.model.noticeAutoDismissDelay = 0.6
        await h.model.checkUpdate()
        try? await Task.sleep(nanoseconds: 200_000_000)
        XCTAssertEqual(h.model.notices.last?.title, "已是最新版本")
    }

    func testNotice_Error_NeverAutoDismisses() async {
        let h = Harness()
        h.model.noticeAutoDismissDelay = 0.05
        h.model.coreDidFail()
        try? await Task.sleep(nanoseconds: 400_000_000)
        XCTAssertEqual(h.model.noticeTitles, ["后台服务没能启动，请退出后重新打开"])
    }

    func testNotice_Warning_NeverAutoDismisses() async {
        let h = Harness(responses: ["check_update": remote("update_unavailable")])
        h.model.noticeAutoDismissDelay = 0.05
        await h.model.checkUpdate()
        XCTAssertEqual(h.model.notices.last?.kind, .warning)
        try? await Task.sleep(nanoseconds: 400_000_000)
        XCTAssertEqual(h.model.noticeTitles, ["暂时无法检查更新，请稍后再试"])
    }

    func testNotice_SuccessAfterError_BothShownThenOnlyErrorRemains() async {
        let h = Harness(responses: ["check_update": resultJSON(#"{"latest_version":"1.2.0","url":null,"is_newer":false}"#)])
        h.model.noticeAutoDismissDelay = 0.2
        h.model.coreDidFail()
        await h.model.checkUpdate()
        XCTAssertEqual(h.model.noticeTitles, ["后台服务没能启动，请退出后重新打开", "已是最新版本"])
        await waitUntil { h.model.notices.count == 1 }
        try? await Task.sleep(nanoseconds: 300_000_000)
        XCTAssertEqual(h.model.noticeTitles, ["后台服务没能启动，请退出后重新打开"])
    }

    func testNotice_EachSuccessDismissesOnItsOwnTimer() async throws {
        let h = Harness(
            responses: [
                "check_update": resultJSON(#"{"latest_version":"1.2.0","url":null,"is_newer":false}"#),
                "detect_access": accessResult(mode: "relay", target: "relay", detect: "not_logged_in"),
            ],
            status: try accessStatus(mode: "relay", target: "relay", detect: "not_logged_in")
        )
        h.model.noticeAutoDismissDelay = 0.4
        await h.model.checkUpdate()
        try? await Task.sleep(nanoseconds: 250_000_000)
        await h.model.recheckAccess()
        XCTAssertEqual(h.model.noticeTitles, ["已是最新版本", "接入方式没有变化"])
        try? await Task.sleep(nanoseconds: 250_000_000)
        XCTAssertEqual(h.model.noticeTitles, ["接入方式没有变化"])
        await waitUntil { h.model.notices.isEmpty }
        XCTAssertTrue(h.model.notices.isEmpty)
    }

    func testDismissNotice_RemovesOnlyThatNotice() async {
        let h = Harness(responses: ["check_update": resultJSON(#"{"latest_version":"1.2.0","url":null,"is_newer":false}"#)])
        h.model.coreDidFail()
        await h.model.checkUpdate()
        let success = h.model.notices.last
        XCTAssertEqual(success?.title, "已是最新版本")
        h.model.dismissNotice(success!.id)
        XCTAssertEqual(h.model.noticeTitles, ["后台服务没能启动，请退出后重新打开"])
    }

    func testNotice_KeychainWarningSurvivesStartupAlignSwitch() async throws {
        let h = Harness(
            responses: ["apply_access": accessResult(mode: "relay", target: "relay", detect: "not_logged_in")],
            chatGPTRunning: true
        )
        await h.model.start(stored: StoredConnection(baseURL: nil, apiKey: .failure(KeychainError.accessDenied(-25293))))
        h.core.statusContinuation.yield(try accessStatus(mode: "both", target: "relay", detect: "not_logged_in"))
        await waitUntil { h.model.notices.count == 2 }
        XCTAssertEqual(h.model.noticeTitles, ["无法读取已保存的 API Key，请重新输入", "已切换到「只用中台」"])
        XCTAssertEqual(h.model.notices.first?.kind, .warning)
    }

    // MARK: 首次设置成功文案

    func testSetupSucceededText_NilBeforeSetup() {
        XCTAssertNil(Harness().model.setupSucceededText)
    }

    func testSetupSucceededText_Relay() async {
        let h = Harness(responses: [
            "save_connection": saveResult(changed: true, count: 12),
            "set_forwarding": accessResult(mode: "relay", target: "relay", detect: "not_logged_in"),
        ])
        h.fillInputs()
        await h.model.completeSetup()
        XCTAssertEqual(
            h.model.setupSucceededText,
            "已获取 12 个中台模型。没有检测到 ChatGPT 登录，接入方式为「只用中台」：Codex 不需要登录，模型列表里只有中台模型。\n请重启 ChatGPT 后使用。助手会在你登录电脑时自动启动，在菜单栏里常驻。"
        )
    }

    func testSetupSucceededText_Both() async {
        let h = Harness(responses: [
            "save_connection": saveResult(changed: true, count: 7),
            "set_forwarding": accessResult(mode: "both", target: "both", detect: "logged_in"),
        ])
        h.fillInputs()
        await h.model.completeSetup()
        XCTAssertEqual(
            h.model.setupSucceededText,
            "已获取 7 个中台模型。已检测到 ChatGPT 登录，接入方式为「官方 + 中台」。\n请重启 ChatGPT 后使用。助手会在你登录电脑时自动启动，在菜单栏里常驻。"
        )
    }
}
