import XCTest
@testable import ModelBridge

final class RelayFakeKeychain: KeyStoring, @unchecked Sendable {
    private let log: CallLog
    private let lock = NSLock()
    private var entries: [String: String]
    private let failing: Set<String>
    private let loadError: KeychainError?

    init(log: CallLog, entries: [String: String] = [:], loadError: KeychainError? = nil, failing: Set<String> = []) {
        self.log = log
        self.entries = entries
        self.loadError = loadError
        self.failing = failing
    }

    var stored: [String: String] { lock.lock(); defer { lock.unlock() }; return entries }

    func save(_ value: String, relayID: String) throws {
        log.append("keychain.save:\(relayID)")
        if failing.contains("keychain.save") { throw Boom() }
        lock.lock(); entries[relayID] = value; lock.unlock()
    }

    func load(relayID: String) throws -> String? {
        if let loadError { throw loadError }
        lock.lock(); defer { lock.unlock() }
        return entries[relayID]
    }

    func deleteAll() throws {
        log.append("keychain.deleteAll")
        if failing.contains("keychain.deleteAll") { throw Boom() }
        lock.lock(); entries = [:]; lock.unlock()
    }

    func delete(relayID: String) throws {
        log.append("keychain.delete:\(relayID)")
        if failing.contains("keychain.delete") { throw Boom() }
        lock.lock(); entries[relayID] = nil; lock.unlock()
    }
}

final class RelayFakeSettings: SettingsStoring, @unchecked Sendable {
    private let log: CallLog
    private let lock = NSLock()
    private var relays: [RelayConfig]
    private let failing: Set<String>
    private let failingSaveCalls: Set<Int>
    private var saveCalls = 0

    init(log: CallLog, relays: [RelayConfig] = [], failing: Set<String> = [], failingSaveCalls: Set<Int> = []) {
        self.log = log
        self.relays = relays
        self.failing = failing
        self.failingSaveCalls = failingSaveCalls
    }

    var stored: [RelayConfig] { lock.lock(); defer { lock.unlock() }; return relays }

    func loadRelays() throws -> [RelayConfig] {
        if failing.contains("settings.loadRelays") { throw Boom() }
        return stored
    }

    func saveRelays(_ value: [RelayConfig]) throws {
        log.append("settings.saveRelays")
        lock.lock(); saveCalls += 1; let call = saveCalls; lock.unlock()
        if failing.contains("settings.saveRelays") || failingSaveCalls.contains(call) { throw Boom() }
        lock.lock(); relays = value; lock.unlock()
    }

    func deleteAll() throws {
        log.append("settings.deleteAll")
        if failing.contains("settings.deleteAll") { throw Boom() }
        lock.lock(); relays = []; lock.unlock()
    }
}

@MainActor
private func awaitCondition(timeout: TimeInterval = 2, _ condition: () -> Bool) async {
    let deadline = Date().addingTimeInterval(timeout)
    while !condition(), Date() < deadline { try? await Task.sleep(nanoseconds: 5_000_000) }
}

private func json(_ text: String) -> Result<Data, CoreError> { .success(Data(text.utf8)) }

private func entry(
    _ id: String, _ name: String, count: Int = 6, fetchedAt: String? = "2026-11-16T00:12:00Z",
    stale: Bool = false, error: String? = nil
) -> String {
    let fetched = fetchedAt.map { "\"\($0)\"" } ?? "null"
    let err = error.map { "\"\($0)\"" } ?? "null"
    return #"{"id":"\#(id)","name":"\#(name)","model_count":\#(count),"fetched_at":\#(fetched),"stale":\#(stale),"error":\#(err)}"#
}

private func statusJSON(
    forwarding: Bool = true, mode: String = "both", defaultRelay: String? = "中转", relays: [String]
) -> String {
    let def = defaultRelay.map { "\"\($0)\"" } ?? "null"
    return """
    {"version":"1.3.0","forwarding":\(forwarding),"proxy":{"running":true,"error":null},\
    "chatgpt":{"found":true,"error":null},"default_relay":\(def),"relays":[\(relays.joined(separator: ","))],\
    "taken_over":[],"restore_blocked":[],"config_path":"/Users/example/.codex/config.toml",\
    "update":{"latest_version":null,"url":null},"access":{"mode":"\(mode)","target":"\(mode)","detect":"logged_in"}}
    """
}

private func decodeStatus(_ text: String) -> CoreStatus {
    let decoder = JSONDecoder()
    decoder.keyDecodingStrategy = .convertFromSnakeCase
    return try! decoder.decode(CoreStatus.self, from: Data(text.utf8))
}

let relayOne = RelayConfig(id: "1", name: "中转", baseURL: "https://a.example.com")
let relayTwo = RelayConfig(id: "2", name: "备用", baseURL: "https://b.example.com")

@MainActor
private struct Harness {
    let model: AppModel
    let core: FakeCore
    let keychain: RelayFakeKeychain
    let settings: RelayFakeSettings
    let log: CallLog

    static func make(
        relays: [RelayConfig] = [relayOne],
        keys: [String: String]? = nil,
        responses: [String: Result<Data, CoreError>] = [:],
        gates: [String: Gate] = [:],
        settingsFailing: Set<String> = [],
        settingsFailingSaveCalls: Set<Int> = [],
        keychainFailing: Set<String> = [],
        queued: [String: [Result<Data, CoreError>]] = [:],
        status: String? = nil,
        chatGPTRunning: Bool = false
    ) async -> Harness {
        let log = CallLog()
        let core = FakeCore(log: log, responses: responses, gates: gates)
        let keyMap = keys ?? Dictionary(uniqueKeysWithValues: relays.map { ($0.id, "sk-example-\($0.id)") })
        let keychain = RelayFakeKeychain(log: log, entries: keyMap, failing: keychainFailing)
        let settings = RelayFakeSettings(log: log, relays: relays, failing: settingsFailing, failingSaveCalls: settingsFailingSaveCalls)
        queued.forEach { core.enqueue($0.key, $0.value) }
        let model = AppModel(
            core: core, keychain: keychain, settings: settings,
            loginItem: FakeLoginItem(log: log), chatGPT: FakeChatGPT(running: chatGPTRunning)
        )
        await model.start()
        let text = status ?? statusJSON(relays: relays.map { entry($0.id, $0.name) })
        model.status = decodeStatus(text)
        return Harness(model: model, core: core, keychain: keychain, settings: settings, log: log)
    }
}

private func saveRelayResult(id: String, name: String, count: Int = 5) -> Result<Data, CoreError> {
    json(#"{"id":"\#(id)","name":"\#(name)","model_count":\#(count),"models_changed":true}"#)
}

@MainActor
private func fillDraft(_ model: AppModel, name: String, url: String = "https://c.example.com", key: String = "sk-draft-0000") {
    model.draftName = name
    model.draftBaseURL = url
    model.draftAPIKey = key
}

@MainActor
final class MultiRelayAppModelTests: XCTestCase {
    // MARK: 启动与迁移

    func testStart_LoadsRelaysFromSettingsInOrder() async {
        let h = await Harness.make(relays: [relayTwo, relayOne])
        XCTAssertEqual(h.model.relays, [relayTwo, relayOne])
        XCTAssertFalse(h.model.needsSetup)
    }

    func testStart_NoRelaysNeedsSetup() async {
        let h = await Harness.make(relays: [])
        XCTAssertTrue(h.model.needsSetup)
    }

    func testInitParams_AllRelaysWithKeysInOrder() async throws {
        let log = CallLog()
        let settings = RelayFakeSettings(log: log, relays: [relayOne, relayTwo])
        let keychain = RelayFakeKeychain(log: log, entries: ["1": "sk-a", "2": "sk-b"])
        let params = CoreInitParams.load(settings: settings, keychain: keychain, chatgptDir: "/Applications/ChatGPT.app")
        XCTAssertEqual(params.relays, [
            CoreRelayInit(id: "1", name: "中转", baseURL: "https://a.example.com", apiKey: "sk-a"),
            CoreRelayInit(id: "2", name: "备用", baseURL: "https://b.example.com", apiKey: "sk-b"),
        ])
        XCTAssertEqual(params.chatgptDir, "/Applications/ChatGPT.app")
    }

    func testInitParams_NoRelaysIsEmptyArray() {
        let log = CallLog()
        let params = CoreInitParams.load(
            settings: RelayFakeSettings(log: log), keychain: RelayFakeKeychain(log: log), chatgptDir: nil
        )
        XCTAssertEqual(params.relays, [])
    }

    // MARK: 本地校验

    private func validationError(_ configure: (AppModel) -> Void, field: RelayField) async -> String? {
        let h = await Harness.make(relays: [relayOne, RelayConfig(id: "2", name: "Alpha", baseURL: "https://b.example.com")])
        h.model.beginAddRelay()
        fillDraft(h.model, name: "新配置")
        configure(h.model)
        await h.model.saveRelay()
        XCTAssertTrue(h.core.calls("save_relay").isEmpty, "校验没通过不能调核心")
        XCTAssertTrue(h.model.showRelayForm)
        return h.model.relayFieldErrors[field]
    }

    func testValidation_NameTooLongAllowsTypingButFailsOnSave() async {
        let h = await Harness.make()
        h.model.beginAddRelay()
        let long = String(repeating: "字", count: 20)
        fillDraft(h.model, name: long)
        XCTAssertEqual(h.model.draftName, long)
        await h.model.saveRelay()
        XCTAssertEqual(h.model.relayFieldErrors[.name], "名字最多 16 个字，现在是 20 个。")
    }

    func testValidation_NameIsTrimmedBeforeCounting() async {
        let h = await Harness.make(responses: ["save_relay": saveRelayResult(id: "2", name: String(repeating: "字", count: 16))])
        h.model.beginAddRelay()
        fillDraft(h.model, name: "  " + String(repeating: "字", count: 16) + "  ")
        await h.model.saveRelay()
        XCTAssertNil(h.model.relayFieldErrors[.name])
        XCTAssertEqual(h.core.calls("save_relay").count, 1)
    }

    func testValidation_NameWithDot() async {
        let error = await validationError({ $0.draftName = "甲·乙" }, field: .name)
        XCTAssertEqual(error, "名字里不能有「·」。")
    }

    func testValidation_NameEmpty() async {
        let error = await validationError({ $0.draftName = "   " }, field: .name)
        XCTAssertEqual(error, "请填写名字。")
    }

    func testValidation_NameDuplicateIgnoringCase() async {
        let error = await validationError({ $0.draftName = "alpha" }, field: .name)
        XCTAssertEqual(error, "已有同名配置「Alpha」。")
    }

    func testValidation_BaseURLEmpty() async {
        let error = await validationError({ $0.draftBaseURL = " " }, field: .baseURL)
        XCTAssertEqual(error, "请填写 Base URL。")
    }

    func testValidation_BaseURLNotHTTPS() async {
        let error = await validationError({ $0.draftBaseURL = "http://c.example.com" }, field: .baseURL)
        XCTAssertEqual(error, "Base URL 要以 https:// 开头。")
    }

    func testValidation_APIKeyEmpty() async {
        let error = await validationError({ $0.draftAPIKey = "" }, field: .apiKey)
        XCTAssertEqual(error, "请填写 API Key。")
    }

    func testValidation_EditingKeepsOwnNameWithoutDuplicateError() async {
        let h = await Harness.make(responses: ["save_relay": saveRelayResult(id: "1", name: "中转")])
        h.model.beginEditRelay(id: "1")
        await h.model.saveRelay()
        XCTAssertNil(h.model.relayFieldErrors[.name])
        XCTAssertEqual(h.core.calls("save_relay").count, 1)
    }

    // MARK: 添加与编辑

    func testBeginEdit_FillsDraftFromRelayAndKeychain() async {
        let h = await Harness.make(relays: [relayOne, relayTwo])
        h.model.beginEditRelay(id: "2")
        XCTAssertTrue(h.model.showRelayForm)
        XCTAssertEqual(h.model.editingRelayID, "2")
        XCTAssertEqual(h.model.draftName, "备用")
        XCTAssertEqual(h.model.draftBaseURL, "https://b.example.com")
        XCTAssertEqual(h.model.draftAPIKey, "sk-example-2")
    }

    func testAdd_Success_StoresKeyThenSettingsAndShowsNotice() async throws {
        let h = await Harness.make(responses: ["save_relay": saveRelayResult(id: "2", name: "备用", count: 5)])
        h.model.beginAddRelay()
        XCTAssertNil(h.model.editingRelayID)
        fillDraft(h.model, name: " 备用 ", url: "https://b.example.com", key: "sk-new-2")
        await h.model.saveRelay()

        let params = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(try XCTUnwrap(h.core.calls("save_relay").first).params.utf8)) as? [String: Any])
        XCTAssertTrue((params["id"] ?? NSNull()) is NSNull)
        XCTAssertEqual(params["name"] as? String, "备用")
        XCTAssertEqual(params["base_url"] as? String, "https://b.example.com")
        XCTAssertEqual(params["api_key"] as? String, "sk-new-2")

        XCTAssertEqual(h.keychain.stored["2"], "sk-new-2")
        XCTAssertEqual(h.keychain.stored["1"], "sk-example-1")
        XCTAssertEqual(h.settings.stored, [relayOne, relayTwo])
        XCTAssertEqual(h.model.relays, [relayOne, relayTwo])
        let order = h.log.entries.filter { $0 == "core:save_relay" || $0 == "keychain.save:2" || $0 == "settings.saveRelays" }
        XCTAssertEqual(order, ["core:save_relay", "keychain.save:2", "settings.saveRelays"])

        XCTAssertFalse(h.model.showRelayForm)
        XCTAssertNil(h.model.relaySaveAlert)
        let notice = try XCTUnwrap(h.model.notices.first { $0.title == "已添加「备用」" })
        XCTAssertEqual(notice.detail, "已获取 5 个模型。重启 ChatGPT 后生效。")
        XCTAssertEqual(notice.kind, .success)
    }

    func testEdit_Success_UpdatesInPlaceAndShowsNotice() async throws {
        let h = await Harness.make(relays: [relayOne, relayTwo], responses: ["save_relay": saveRelayResult(id: "2", name: "备用二")])
        h.model.beginEditRelay(id: "2")
        h.model.draftName = "备用二"
        await h.model.saveRelay()

        let params = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(try XCTUnwrap(h.core.calls("save_relay").first).params.utf8)) as? [String: Any])
        XCTAssertEqual(params["id"] as? String, "2")
        XCTAssertEqual(h.settings.stored.map(\.id), ["1", "2"])
        XCTAssertEqual(h.settings.stored[1].name, "备用二")
        XCTAssertEqual(h.keychain.stored["2"], "sk-example-2")
        XCTAssertFalse(h.model.showRelayForm)
        let notice = try XCTUnwrap(h.model.notices.first { $0.title == "已保存「备用二」" })
        XCTAssertEqual(notice.detail, "重启 ChatGPT 后生效。")
        XCTAssertEqual(notice.kind, .success)
    }

    func testSave_Relay401_KeepsDialogAndStorageUntouched() async {
        let h = await Harness.make(responses: ["save_relay": .failure(.remote(code: "relay_401", keys: []))])
        h.model.beginAddRelay()
        fillDraft(h.model, name: "备用", url: "https://b.example.com", key: "sk-bad")
        await h.model.saveRelay()

        XCTAssertTrue(h.model.showRelayForm)
        XCTAssertEqual(h.model.draftName, "备用")
        XCTAssertEqual(h.model.draftAPIKey, "sk-bad")
        XCTAssertEqual(h.model.relaySaveAlert, DialogAlert(
            title: "没有保存",
            message: "中转服务返回 401：API Key 无效或已过期。已填的内容都还在，改好后再保存。",
            severity: .danger
        ))
        XCTAssertFalse(h.log.entries.contains { $0.hasPrefix("keychain.save") || $0 == "settings.saveRelays" })
        XCTAssertEqual(h.settings.stored, [relayOne])
        XCTAssertEqual(h.keychain.stored, ["1": "sk-example-1"])
        XCTAssertTrue(h.model.notices.isEmpty)
    }

    func testSave_SettingsWriteFailure_RollsBackKeyAndShowsAlertInDialog() async {
        let h = await Harness.make(
            responses: ["save_relay": saveRelayResult(id: "2", name: "备用")],
            settingsFailing: ["settings.saveRelays"]
        )
        h.model.beginAddRelay()
        fillDraft(h.model, name: "备用", url: "https://b.example.com", key: "sk-new-2")
        await h.model.saveRelay()

        XCTAssertNil(h.keychain.stored["2"])
        XCTAssertEqual(h.settings.stored, [relayOne])
        XCTAssertEqual(h.model.relays, [relayOne])
        XCTAssertEqual(h.model.relaySaveAlert, DialogAlert(title: "没有保存", message: "无法写入设置文件。", severity: .danger))
        XCTAssertTrue(h.model.showRelayForm)
        XCTAssertFalse(h.model.notices.contains { $0.title.hasPrefix("已添加") })
    }

    func testSave_KeychainWriteFailure_DoesNotWriteSettings() async {
        let h = await Harness.make(
            responses: ["save_relay": saveRelayResult(id: "2", name: "备用"), "delete_relay": json(statusJSON(relays: [entry("1", "中转")]))],
            keychainFailing: ["keychain.save"]
        )
        h.model.beginAddRelay()
        fillDraft(h.model, name: "备用", url: "https://b.example.com", key: "sk-new-2")
        await h.model.saveRelay()

        XCTAssertEqual(h.core.calls("delete_relay").map(\.params), [#"{"id":"2"}"#])
        XCTAssertEqual(h.settings.stored, [relayOne])
        XCTAssertEqual(h.model.relays, [relayOne])
        XCTAssertEqual(h.model.relaySaveAlert, DialogAlert(title: "没有保存", message: "无法写入钥匙串。", severity: .danger))
        XCTAssertTrue(h.model.showRelayForm)
    }

    func testSaving_DisablesControlsAndShowsBusyLabel() async {
        let gate = Gate()
        let h = await Harness.make(responses: ["save_relay": saveRelayResult(id: "2", name: "备用")], gates: ["save_relay": gate])
        h.model.beginAddRelay()
        fillDraft(h.model, name: "备用", url: "https://b.example.com")
        XCTAssertFalse(h.model.isSavingRelay)
        XCTAssertFalse(h.model.relayFormLocked)
        let task = Task { await h.model.saveRelay() }
        await awaitCondition { !h.core.calls("save_relay").isEmpty }
        XCTAssertTrue(h.model.isSavingRelay)
        XCTAssertTrue(h.model.relayFormLocked)
        XCTAssertEqual(h.model.relaySaveButtonTitle, "正在保存并获取模型…")
        await gate.open()
        await task.value
        XCTAssertFalse(h.model.isSavingRelay)
        XCTAssertFalse(h.model.relayFormLocked)
    }

    func testCancelRelayForm_ClosesWithoutSaving() async {
        let h = await Harness.make()
        h.model.beginAddRelay()
        h.model.cancelRelayForm()
        XCTAssertFalse(h.model.showRelayForm)
        XCTAssertTrue(h.core.calls("save_relay").isEmpty)
    }

    // MARK: 删除

    func testDelete_LastRelayCannotBeDeleted() async {
        let h = await Harness.make()
        XCTAssertFalse(h.model.canDeleteRelay(id: "1"))
        XCTAssertEqual(h.model.deleteBlockedReason, "至少保留一个配置。")
        h.model.requestDeleteRelay(id: "1")
        XCTAssertNil(h.model.pendingConfirmation)
    }

    func testDelete_ConfirmationTexts() async throws {
        let status = statusJSON(relays: [entry("1", "中转"), entry("2", "备用", count: 5)])
        let h = await Harness.make(relays: [relayOne, relayTwo], status: status)
        h.model.requestDeleteRelay(id: "2")
        let confirmation = try XCTUnwrap(h.model.pendingConfirmation)
        XCTAssertEqual(confirmation.title, "删除配置「备用」？")
        XCTAssertEqual(confirmation.cancelLabel, "取消")
        XCTAssertEqual(confirmation.confirmLabel, "删除")
        XCTAssertTrue(confirmation.message.contains("删除后："))
        XCTAssertTrue(confirmation.message.contains("· Codex 的模型列表里不再有「备用」的 5 个模型"))
        XCTAssertTrue(confirmation.message.contains("· 它的 API Key 会从系统钥匙串里删除"))
        XCTAssertTrue(confirmation.message.hasSuffix("\n重启 ChatGPT 后生效。"))
        let dialog = ConfirmDialogModel(confirmation: confirmation)
        XCTAssertEqual(dialog.buttons.map(\.label), ["取消", "删除"])
    }

    func testDelete_Success_WritesSettingsThenCoreThenKeychain() async throws {
        let status = statusJSON(relays: [entry("1", "中转")])
        let h = await Harness.make(relays: [relayOne, relayTwo], responses: ["delete_relay": json(status)])
        h.model.requestDeleteRelay(id: "2")
        await h.model.confirmPending()

        let params = try XCTUnwrap(h.core.calls("delete_relay").first).params
        XCTAssertEqual(params, #"{"id":"2"}"#)
        XCTAssertEqual(h.settings.stored, [relayOne])
        XCTAssertEqual(h.model.relays, [relayOne])
        XCTAssertNil(h.keychain.stored["2"])
        XCTAssertEqual(h.keychain.stored["1"], "sk-example-1")
        let order = h.log.entries.filter { ["core:delete_relay", "settings.saveRelays", "keychain.delete:2"].contains($0) }
        XCTAssertEqual(order, ["settings.saveRelays", "core:delete_relay", "keychain.delete:2"])
        XCTAssertNil(h.model.pendingConfirmation)
        let notice = try XCTUnwrap(h.model.notices.first { $0.title == "已删除「备用」" })
        XCTAssertEqual(notice.detail, "重启 ChatGPT 后生效。")
        XCTAssertEqual(notice.kind, .success)
    }

    func testDelete_SettingsWriteFailure_ShowsRedNoticeAndKeepsList() async throws {
        let status = statusJSON(relays: [entry("1", "中转")])
        let h = await Harness.make(
            relays: [relayOne, relayTwo], responses: ["delete_relay": json(status)],
            settingsFailing: ["settings.saveRelays"]
        )
        h.model.requestDeleteRelay(id: "2")
        await h.model.confirmPending()

        let notice = try XCTUnwrap(h.model.notices.first { $0.title == "没有删除「备用」" })
        XCTAssertEqual(notice.detail, "无法写入设置文件。")
        XCTAssertEqual(notice.kind, .error)
        XCTAssertEqual(h.model.relays, [relayOne, relayTwo])
        XCTAssertEqual(h.settings.stored, [relayOne, relayTwo])
        XCTAssertEqual(h.keychain.stored["2"], "sk-example-2")
        XCTAssertTrue(h.core.calls("delete_relay").isEmpty)
    }

    func testDelete_CoreFailure_RestoresSettingsKeepsKeychainAndShowsReason() async throws {
        let h = await Harness.make(
            relays: [relayOne, relayTwo],
            responses: ["delete_relay": .failure(.remote(code: "relay_not_found", keys: []))]
        )
        h.model.requestDeleteRelay(id: "2")
        await h.model.confirmPending()

        XCTAssertEqual(h.settings.stored, [relayOne, relayTwo])
        XCTAssertEqual(h.model.relays, [relayOne, relayTwo])
        XCTAssertEqual(h.keychain.stored["2"], "sk-example-2")
        XCTAssertFalse(h.log.entries.contains("keychain.delete:2"))
        let notice = try XCTUnwrap(h.model.notices.first { $0.title == "没有删除「备用」" })
        XCTAssertEqual(notice.detail, "这个配置不存在，请刷新后重试。")
        XCTAssertEqual(notice.kind, .error)
        XCTAssertNil(h.model.pendingConfirmation)
    }

    func testAdd_SettingsWriteFailure_RevokesNewRelayInCore() async throws {
        let h = await Harness.make(
            responses: ["save_relay": saveRelayResult(id: "2", name: "备用"), "delete_relay": json(statusJSON(relays: [entry("1", "中转")]))],
            settingsFailing: ["settings.saveRelays"]
        )
        h.model.beginAddRelay()
        fillDraft(h.model, name: "备用", url: "https://b.example.com", key: "sk-new-2")
        await h.model.saveRelay()

        XCTAssertEqual(h.core.calls("delete_relay").map(\.params), [#"{"id":"2"}"#])
        XCTAssertNil(h.keychain.stored["2"])
        XCTAssertEqual(h.settings.stored, [relayOne])
        XCTAssertEqual(h.model.relays, [relayOne])
    }

    func testEdit_SettingsWriteFailure_RestoresOldValuesInCoreAndKeychain() async throws {
        let h = await Harness.make(
            relays: [relayOne, relayTwo],
            responses: ["save_relay": saveRelayResult(id: "2", name: "备用二")],
            settingsFailing: ["settings.saveRelays"]
        )
        h.model.beginEditRelay(id: "2")
        fillDraft(h.model, name: "备用二", url: "https://new.example.com", key: "sk-new-2")
        await h.model.saveRelay()

        let calls = h.core.calls("save_relay")
        XCTAssertEqual(calls.count, 2)
        let restore = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(try XCTUnwrap(calls.last).params.utf8)) as? [String: Any])
        XCTAssertEqual(restore["id"] as? String, "2")
        XCTAssertEqual(restore["name"] as? String, "备用")
        XCTAssertEqual(restore["base_url"] as? String, "https://b.example.com")
        XCTAssertEqual(restore["api_key"] as? String, "sk-example-2")
        XCTAssertEqual(h.keychain.stored["2"], "sk-example-2")
        XCTAssertEqual(h.model.relays, [relayOne, relayTwo])
    }

    // MARK: 撤回做不到与删除的补充提示

    private let notRevokedLine = "\n已在本次运行中生效，但没能保存到本机，重启助手后会恢复成原来的样子。"

    func testSetup_KeychainWriteFailure_ResetsCoreWithEmptyInit() async {
        let h = await Harness.make(relays: [], responses: ["save_relay": saveRelayResult(id: "1", name: "中转")], keychainFailing: ["keychain.save"])
        h.model.beginSetup()
        h.model.draftBaseURL = "https://a.example.com"
        h.model.draftAPIKey = "sk-first"
        await h.model.completeSetup()
        assertCoreResetToEmpty(h)
        XCTAssertTrue(h.settings.stored.isEmpty)
    }

    func testSetup_SettingsWriteFailure_ResetsCoreWithEmptyInit() async {
        let h = await Harness.make(relays: [], responses: ["save_relay": saveRelayResult(id: "1", name: "中转")], settingsFailing: ["settings.saveRelays"])
        h.model.beginSetup()
        h.model.draftBaseURL = "https://a.example.com"
        h.model.draftAPIKey = "sk-first"
        await h.model.completeSetup()
        assertCoreResetToEmpty(h)
        XCTAssertTrue(h.keychain.stored.isEmpty)
    }

    private func assertCoreResetToEmpty(_ h: Harness, file: StaticString = #filePath, line: UInt = #line) {
        let inits = h.core.calls("init")
        XCTAssertEqual(inits.count, 1, file: file, line: line)
        let object = inits.first.flatMap { try? JSONSerialization.jsonObject(with: Data($0.params.utf8)) as? [String: Any] }
        XCTAssertEqual((object?["relays"] as? [Any])?.count, 0, file: file, line: line)
        XCTAssertTrue(h.core.calls("delete_relay").isEmpty, file: file, line: line)
        guard case .failed = h.model.setupPhase else { return XCTFail("应停在失败", file: file, line: line) }
    }

    func testAdd_SettingsWriteFailureAndRevokeFails_AddsNotRevokedLine() async {
        let h = await Harness.make(
            responses: ["save_relay": saveRelayResult(id: "2", name: "备用"), "delete_relay": .failure(.remote(code: "relay_unreachable", keys: []))],
            settingsFailing: ["settings.saveRelays"]
        )
        h.model.beginAddRelay()
        fillDraft(h.model, name: "备用", url: "https://b.example.com", key: "sk-new-2")
        await h.model.saveRelay()
        XCTAssertEqual(h.model.relaySaveAlert, DialogAlert(
            title: "没有保存", message: "无法写入设置文件。" + notRevokedLine, severity: .danger
        ))
    }

    func testEdit_OldKeyUnreadable_CannotRevokeAndAddsNotRevokedLine() async {
        let h = await Harness.make(
            relays: [relayOne, relayTwo], keys: ["1": "sk-example-1"],
            responses: ["save_relay": saveRelayResult(id: "2", name: "备用二")],
            settingsFailing: ["settings.saveRelays"]
        )
        h.model.beginEditRelay(id: "2")
        fillDraft(h.model, name: "备用二", url: "https://new.example.com", key: "sk-new-2")
        await h.model.saveRelay()
        XCTAssertEqual(h.core.calls("save_relay").count, 1)
        XCTAssertEqual(h.model.relaySaveAlert, DialogAlert(
            title: "没有保存", message: "无法写入设置文件。" + notRevokedLine, severity: .danger
        ))
    }

    func testEdit_RestoreCallFails_AddsNotRevokedLine() async {
        let h = await Harness.make(
            relays: [relayOne, relayTwo],
            settingsFailing: ["settings.saveRelays"],
            queued: ["save_relay": [saveRelayResult(id: "2", name: "备用二"), .failure(.remote(code: "relay_unreachable", keys: []))]]
        )
        h.model.beginEditRelay(id: "2")
        fillDraft(h.model, name: "备用二", url: "https://new.example.com", key: "sk-new-2")
        await h.model.saveRelay()
        XCTAssertEqual(h.core.calls("save_relay").count, 2)
        XCTAssertEqual(h.model.relaySaveAlert?.message, "无法写入设置文件。" + notRevokedLine)
    }

    func testDelete_KeychainDeleteFails_ShowsWarningWithKeychainLine() async throws {
        let h = await Harness.make(
            relays: [relayOne, relayTwo], responses: ["delete_relay": json(statusJSON(relays: [entry("1", "中转")]))],
            keychainFailing: ["keychain.delete"]
        )
        h.model.requestDeleteRelay(id: "2")
        await h.model.confirmPending()
        let notice = try XCTUnwrap(h.model.notices.first { $0.title == "已删除「备用」" })
        XCTAssertEqual(notice.kind, .warning)
        XCTAssertEqual(notice.detail, "重启 ChatGPT 后生效。\n但没能从系统钥匙串里删除它的 API Key。")
        XCTAssertEqual(h.model.relays, [relayOne])
    }

    func testDelete_CoreFailsAndSettingsRestoreFails_ShowsRestoreFailedLine() async throws {
        let h = await Harness.make(
            relays: [relayOne, relayTwo],
            responses: ["delete_relay": .failure(.remote(code: "relay_not_found", keys: []))],
            settingsFailingSaveCalls: [2]
        )
        h.model.requestDeleteRelay(id: "2")
        await h.model.confirmPending()
        let notice = try XCTUnwrap(h.model.notices.first { $0.title == "没有删除「备用」" })
        XCTAssertEqual(notice.kind, .error)
        XCTAssertEqual(notice.detail, "这个配置不存在，请刷新后重试。\n本机设置没能恢复，重启助手后这个配置会消失，需要重新添加。")
        XCTAssertEqual(h.keychain.stored["2"], "sk-example-2")
    }

    func testDeleting_DisablesCancelAndShowsBusyLabel() async {
        let gate = Gate()
        let status = statusJSON(relays: [entry("1", "中转")])
        let h = await Harness.make(relays: [relayOne, relayTwo], responses: ["delete_relay": json(status)], gates: ["delete_relay": gate])
        h.model.requestDeleteRelay(id: "2")
        XCTAssertFalse(h.model.isDeletingRelay)
        XCTAssertNil(h.model.deleteBusyLabel)
        let task = Task { await h.model.confirmPending() }
        await awaitCondition { !h.core.calls("delete_relay").isEmpty }
        XCTAssertTrue(h.model.isDeletingRelay)
        XCTAssertNotNil(h.model.pendingConfirmation)
        XCTAssertEqual(h.model.deleteBusyLabel, "正在删除…")
        XCTAssertTrue(h.model.deleteCancelDisabled)
        await gate.open()
        await task.value
        XCTAssertFalse(h.model.isDeletingRelay)
        XCTAssertNil(h.model.pendingConfirmation)
    }

    // MARK: 列表与行状态

    private func rows(_ entries: [String], forwarding: Bool = true) async -> (Harness, [RelayRow]) {
        let relays = (1...entries.count).map { RelayConfig(id: "\($0)", name: "R\($0)", baseURL: "https://r\($0).example.com") }
        let h = await Harness.make(relays: relays, status: statusJSON(forwarding: forwarding, relays: entries))
        return (h, h.model.relayRows)
    }

    func testRow_Normal() async {
        let (_, rows) = await rows([entry("1", "R1", count: 6)])
        XCTAssertEqual(rows[0].text, "6 个模型")
        XCTAssertFalse(rows[0].canRetry)
        XCTAssertTrue(rows[0].canEdit)
    }

    func testRow_Stale() async {
        let (_, rows) = await rows([entry("1", "R1", count: 4, stale: true, error: "relay_5xx")])
        XCTAssertEqual(rows[0].text, "没更新，沿用上次的 4 个")
        XCTAssertTrue(rows[0].canRetry)
    }

    func testRow_NeverFetchedWithError() async {
        let (_, rows) = await rows([entry("1", "R1", count: 0, fetchedAt: nil, error: "relay_401")])
        XCTAssertEqual(rows[0].text, "还没获取到模型")
        XCTAssertTrue(rows[0].canRetry)
    }

    func testRow_NeverFetchedWithZeroModelsAndNoError() async {
        let (_, rows) = await rows([entry("1", "R1", count: 0, fetchedAt: nil)])
        XCTAssertEqual(rows[0].text, "还没获取到模型")
        XCTAssertTrue(rows[0].canRetry)
    }

    func testRow_ForwardingOff() async {
        let (_, rows) = await rows([entry("1", "R1")], forwarding: false)
        XCTAssertEqual(rows[0].text, "转发关闭时不获取")
    }

    func testRow_RetryShowsRefreshingAndLocksEditDelete() async throws {
        let gate = Gate()
        let relays = [relayOne, relayTwo]
        let status = statusJSON(relays: [entry("1", "中转", count: 0, fetchedAt: nil, error: "relay_401"), entry("2", "备用")])
        let h = await Harness.make(relays: relays, responses: ["refresh_models": json(status)], gates: ["refresh_models": gate], status: status)
        let task = Task { await h.model.retryRelay(id: "1") }
        await awaitCondition { !h.core.calls("refresh_models").isEmpty }
        XCTAssertEqual(h.core.calls("refresh_models").first?.params, #"{"id":"1"}"#)
        let refreshing = try XCTUnwrap(h.model.relayRows.first { $0.id == "1" })
        XCTAssertEqual(refreshing.text, "正在获取…")
        XCTAssertFalse(refreshing.canEdit)
        XCTAssertFalse(refreshing.canDelete)
        let other = try XCTUnwrap(h.model.relayRows.first { $0.id == "2" })
        XCTAssertEqual(other.text, "6 个模型")
        XCTAssertTrue(other.canEdit)
        await gate.open()
        await task.value
        XCTAssertNotEqual(h.model.relayRows.first { $0.id == "1" }?.text, "正在获取…")
    }

    func testRows_FollowConfigurationOrderAndNames() async {
        let (_, rows) = await rows([entry("1", "R1"), entry("2", "R2"), entry("3", "R3")])
        XCTAssertEqual(rows.map(\.id), ["1", "2", "3"])
        XCTAssertEqual(rows.map(\.name), ["R1", "R2", "R3"])
    }

    func testNoGlobalBannersForStaleOrNeverFetched() async {
        let status = statusJSON(relays: [
            entry("1", "中转", count: 3, stale: true, error: "relay_5xx"),
            entry("2", "备用", count: 0, fetchedAt: nil, error: "relay_401"),
        ])
        let h = await Harness.make(relays: [relayOne, relayTwo], status: status)
        XCTAssertTrue(h.model.banners.isEmpty)
    }

    func testListCaption_BelowLimit() async {
        let h = await Harness.make(relays: [relayOne, relayTwo])
        XCTAssertEqual(h.model.relayListCaption, "已有 2 个，最多 10 个。")
        XCTAssertTrue(h.model.canAddRelay)
    }

    func testListCaption_AtLimitBlocksAdd() async {
        let relays = (1...10).map { RelayConfig(id: "\($0)", name: "R\($0)", baseURL: "https://r.example.com") }
        let h = await Harness.make(relays: relays)
        XCTAssertEqual(h.model.relayListCaption, "已有 10 个，达到上限。")
        XCTAssertFalse(h.model.canAddRelay)
        h.model.beginAddRelay()
        XCTAssertFalse(h.model.showRelayForm)
    }

    // MARK: 总览

    func testOverview_TitleOnAndOff() async {
        let on = await Harness.make()
        XCTAssertEqual(on.model.forwardingTitle, "使用中转 · 已开启")
        let off = await Harness.make(status: statusJSON(forwarding: false, relays: [entry("1", "中转")]))
        XCTAssertEqual(off.model.forwardingTitle, "使用中转 · 已关闭")
    }

    func testOverview_BothMode() async {
        let h = await Harness.make()
        XCTAssertEqual(h.model.accessModeName, "官方 + 中转")
        XCTAssertEqual(h.model.accessHelpText, "官方模型在上，中转模型在下；选官方模型时仍访问官方服务。")
    }

    func testOverview_RelayOnlyWithDefault() async {
        let h = await Harness.make(status: statusJSON(mode: "relay", defaultRelay: "中转", relays: [entry("1", "中转")]))
        XCTAssertEqual(h.model.accessModeName, "只用中转")
        XCTAssertEqual(h.model.accessHelpText, "不用登录 ChatGPT。只有中转模型，默认选中「中转 · auto」。")
    }

    func testOverview_RelayOnlyWithoutDefault() async {
        let h = await Harness.make(status: statusJSON(mode: "relay", defaultRelay: nil, relays: [entry("1", "中转")]))
        XCTAssertEqual(h.model.accessHelpText, "不用登录 ChatGPT。只有中转模型，没有默认选中的模型。")
    }

    func testOverview_ForwardingOffHelp() async {
        let h = await Harness.make(status: statusJSON(forwarding: false, relays: [entry("1", "中转")]))
        XCTAssertEqual(h.model.forwardingHelpText, "已关闭：Codex 配置已还原为开启前的样子。")
        XCTAssertNil(h.model.accessModeName)
    }

    func testFooter_SumsModelCountsAndConfigCount() async {
        let status = statusJSON(relays: [entry("1", "中转", count: 6), entry("2", "备用", count: 5)])
        let h = await Harness.make(relays: [relayOne, relayTwo], status: status)
        XCTAssertEqual(h.model.modelCountText, "中转模型 11 个（2 个配置）")
    }

    func testFooter_ForwardingOff() async {
        let h = await Harness.make(status: statusJSON(forwarding: false, relays: [entry("1", "中转")]))
        XCTAssertEqual(h.model.modelCountText, "中转模型 转发关闭时不获取")
    }

    // MARK: 首次设置

    func testSetup_DefaultsAndCopy() async {
        let h = await Harness.make(relays: [])
        h.model.beginSetup()
        XCTAssertEqual(h.model.draftName, "中转")
        XCTAssertEqual(SetupCopy.title, "连接中转服务")
        XCTAssertEqual(SetupCopy.subtitle, "填写中转服务提供的 Base URL 和 API Key。设置完成后，Codex 的模型列表里会多出中转模型。")
        XCTAssertEqual(SetupCopy.connecting, "正在连接中转…")
        XCTAssertEqual(SetupCopy.failedTitle, "没能连上中转服务")
    }

    func testSetup_Success_CreatesFirstRelayWithIdOneAndKey() async {
        let h = await Harness.make(
            relays: [],
            responses: ["save_relay": saveRelayResult(id: "1", name: "中转", count: 6), "set_forwarding": json(statusJSON(relays: [entry("1", "中转")]))]
        )
        h.model.beginSetup()
        h.model.draftBaseURL = "https://a.example.com"
        h.model.draftAPIKey = "sk-first"
        await h.model.completeSetup()
        XCTAssertEqual(h.settings.stored, [relayOne])
        XCTAssertEqual(h.keychain.stored["1"], "sk-first")
        guard case .succeeded(_, let count) = h.model.setupPhase else { return XCTFail("应成功：\(h.model.setupPhase)") }
        XCTAssertEqual(count, 6)
        let text = h.model.setupSucceededText ?? ""
        XCTAssertTrue(text.contains("中转模型"))
    }

    func testSetup_Relay401_FailsWithRelayWording() async {
        let h = await Harness.make(relays: [], responses: ["save_relay": .failure(.remote(code: "relay_401", keys: []))])
        h.model.beginSetup()
        h.model.draftBaseURL = "https://a.example.com"
        h.model.draftAPIKey = "sk-bad"
        await h.model.completeSetup()
        guard case .failed(let title, _) = h.model.setupPhase else { return XCTFail("应失败") }
        XCTAssertEqual(title, "没能连上中转服务")
        XCTAssertTrue(h.settings.stored.isEmpty)
        XCTAssertTrue(h.keychain.stored.isEmpty)
    }

    // MARK: 菜单文案

    func testMenu_ForwardingItemIsUseRelay() async {
        let h = await Harness.make()
        let panel = MenuPanelModel(model: h.model)
        XCTAssertEqual(panel.groups.flatMap { $0 }.first { $0.id == .forwarding }?.title, "使用中转")
    }
}
