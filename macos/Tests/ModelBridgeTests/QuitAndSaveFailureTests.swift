import XCTest
@testable import ModelBridge

private func decodedStatus(_ text: String) -> CoreStatus {
    let decoder = JSONDecoder()
    decoder.keyDecodingStrategy = .convertFromSnakeCase
    return try! decoder.decode(CoreStatus.self, from: Data(text.utf8))
}

@MainActor
final class QuitProgressTests: XCTestCase {
    private func makeModel(
        shutdown: Result<Data, CoreError> = .success(Data("{}".utf8)), gate: Gate
    ) -> (AppModel, FakeCore) {
        let log = CallLog()
        let core = FakeCore(log: log, responses: ["shutdown": shutdown], gates: ["shutdown": gate])
        let model = AppModel(
            core: core, keychain: RelayFakeKeychain(log: log), settings: RelayFakeSettings(log: log, relays: [relayOne]),
            loginItem: FakeLoginItem(log: log), chatGPT: FakeChatGPT(running: false)
        )
        model.status = decodedStatus(ViewFixture.statusJSON(forwarding: true, relayCount: 1))
        return (model, core)
    }

    private func confirmQuit(_ model: AppModel, _ core: FakeCore) async -> Task<Void, Never> {
        await model.quit()
        let task = Task { await model.confirmPending() }
        await awaitUntil { !core.calls("shutdown").isEmpty }
        return task
    }

    func testQuitDialog_BeforeConfirm_ButtonsEnabledAndEscapeCancels() async throws {
        let (model, _) = makeModel(gate: Gate())
        await model.quit()
        let content = try XCTUnwrap(model.quitDialogContent)
        XCTAssertEqual(content.title, "退出模型接入助手？")
        XCTAssertEqual(content.buttons.map(\.label), ["取消", "还原并退出"])
        XCTAssertEqual(content.buttons.map(\.isEnabled), [true, true])
        XCTAssertEqual(content.buttons.map(\.showsSpinner), [false, false])
        XCTAssertEqual(content.escapeAction, .cancel)
    }

    func testRestoring_DialogStaysOpenButtonsDisabledConfirmShowsSpinnerAndEscapeIsInert() async throws {
        let gate = Gate()
        let (model, core) = makeModel(gate: gate)
        let task = await confirmQuit(model, core)
        XCTAssertEqual(model.pendingConfirmation?.kind, .quit)
        let content = try XCTUnwrap(model.quitDialogContent)
        XCTAssertEqual(content.title, "退出模型接入助手？")
        XCTAssertEqual(content.buttons.map(\.label), ["取消", "正在还原"])
        XCTAssertEqual(content.buttons.map(\.isEnabled), [false, false])
        XCTAssertEqual(content.buttons.map(\.showsSpinner), [false, true])
        XCTAssertNil(content.escapeAction)
        await gate.open()
        await task.value
    }

    func testRestoring_CancelAndSecondConfirmAreIgnored() async {
        let gate = Gate()
        let (model, core) = makeModel(gate: gate)
        let task = await confirmQuit(model, core)
        model.cancelPending()
        XCTAssertEqual(model.pendingConfirmation?.kind, .quit)
        await model.confirmPending()
        XCTAssertEqual(core.calls("shutdown").count, 1)
        await gate.open()
        await task.value
    }

    func testRestoreSucceeded_DialogClosesAndAppTerminates() async {
        let gate = Gate()
        let (model, core) = makeModel(gate: gate)
        let terminated = TerminationCounter()
        model.onTerminate = { terminated.increment() }
        let task = await confirmQuit(model, core)
        await gate.open()
        await task.value
        XCTAssertNil(model.pendingConfirmation)
        XCTAssertNil(model.quitDialogContent)
        XCTAssertEqual(terminated.value, 1)
    }

    func testRestoreFailed_DialogClosesAndMainWindowIsRequested() async {
        let gate = Gate()
        let (model, core) = makeModel(shutdown: .failure(.remote(code: "restore_blocked", keys: ["model_provider"])), gate: gate)
        let terminated = TerminationCounter()
        model.onTerminate = { terminated.increment() }
        let task = await confirmQuit(model, core)
        await gate.open()
        await task.value
        XCTAssertNil(model.pendingConfirmation)
        XCTAssertNil(model.quitDialogContent)
        XCTAssertTrue(model.mainWindowRequested)
        XCTAssertEqual(terminated.value, 0)
    }
}

@MainActor
final class SaveRelayFailureTextTests: XCTestCase {
    private let tail = "已填的内容都还在，改好后再保存。"

    private func makeModel(
        relays: [RelayConfig] = [relayOne], save: Result<Data, CoreError>? = nil,
        keychainFailing: Set<String> = [], settingsFailing: Set<String> = []
    ) async -> AppModel {
        let log = CallLog()
        var responses: [String: Result<Data, CoreError>] = [:]
        responses["save_relay"] = save ?? .success(Data(#"{"id":"3","name":"备用","model_count":3,"models_changed":true}"#.utf8))
        let model = AppModel(
            core: FakeCore(log: log, responses: responses),
            keychain: RelayFakeKeychain(log: log, entries: ["1": "sk-a"], failing: keychainFailing),
            settings: RelayFakeSettings(log: log, relays: relays, failing: settingsFailing),
            loginItem: FakeLoginItem(log: log), chatGPT: FakeChatGPT(running: false)
        )
        await model.start()
        return model
    }

    private func addMessage(_ model: AppModel) async -> String? {
        model.beginAddRelay()
        model.draftName = "备用"
        model.draftBaseURL = "https://b.example.com"
        model.draftAPIKey = "sk-bad"
        await model.saveRelay()
        guard let alert = model.relaySaveAlert, alert.title == "没有保存" else { return nil }
        return alert.message
    }

    func testAddFailure_MessagesWithTail() async {
        let cases: [(CoreError, String)] = [
            (.remote(code: "relay_401", keys: []), "中转服务返回 401：API Key 无效或已过期。"),
            (.remote(code: "relay_403", keys: []), "中转服务返回 403：API Key 没有权限。"),
            (.remote(code: "relay_404", keys: []), "中转服务返回 404：Base URL 不正确。"),
            (.remote(code: "relay_429", keys: []), "中转服务返回 429：请求太频繁，请稍后重试。"),
            (.remote(code: "relay_unreachable", keys: []), "连不上中转服务，请检查网络和 Base URL。"),
            (.remote(code: "internal_error", keys: []), "内部错误，请重试。"),
            (.remote(code: "config_changed", keys: []), "Codex 配置文件刚被其他程序修改，请重试。"),
        ]
        for (error, head) in cases {
            let model = await makeModel(save: .failure(error))
            let message = await addMessage(model)
            XCTAssertEqual(message, head + tail, "\(error)")
        }
    }

    func testAddFailure_MessagesWithoutTail() async {
        let cases: [(CoreError, String)] = [
            (.remote(code: "relay_5xx", keys: ["503"]), "中转服务返回 503：中转服务暂时不可用"),
            (.remote(code: "relay_4xx", keys: ["400"]), "中转服务返回 400：请求未被中转服务接受"),
            (.remote(code: "relay_bad_response", keys: ["302"]), "中转服务返回 302：没有返回模型列表，请检查 Base URL"),
            (.processExited, "后台服务已停止，请退出后重新打开。"),
        ]
        for (error, expected) in cases {
            let model = await makeModel(save: .failure(error))
            let message = await addMessage(model)
            XCTAssertEqual(message, expected, "\(error)")
        }
    }

    func testAddFailure_KeychainWriteFails_MessageWithTail() async {
        let model = await makeModel(keychainFailing: ["keychain.save"])
        let message = await addMessage(model)
        XCTAssertEqual(message, "无法写入钥匙串。" + tail)
    }

    func testAddFailure_SettingsWriteFails_MessageWithTail() async {
        let model = await makeModel(settingsFailing: ["settings.saveRelays"])
        let message = await addMessage(model)
        XCTAssertEqual(message, "无法写入设置文件。" + tail)
    }

    func testEditFailure_MessagesFollowSameRules() async {
        let cases: [(CoreError, String)] = [
            (.remote(code: "relay_401", keys: []), "中转服务返回 401：API Key 无效或已过期。" + tail),
            (.remote(code: "relay_unreachable", keys: []), "连不上中转服务，请检查网络和 Base URL。" + tail),
            (.remote(code: "relay_5xx", keys: ["503"]), "中转服务返回 503：中转服务暂时不可用"),
            (.processExited, "后台服务已停止，请退出后重新打开。"),
        ]
        for (error, expected) in cases {
            let model = await makeModel(save: .failure(error))
            model.beginEditRelay(id: "1")
            model.draftName = "改名"
            await model.saveRelay()
            XCTAssertEqual(model.relaySaveAlert?.title, "没有保存")
            XCTAssertEqual(model.relaySaveAlert?.message, expected, "\(error)")
        }
    }
}

@MainActor
final class ProxyStartTimeoutTextTests: XCTestCase {
    func testErrorMessages_HasProxyStartTimeout() {
        XCTAssertEqual(CoreProtocol.errorMessages["proxy_start_timeout"], "启动转发超过 30 秒还没完成，请重试")
    }

    func testProxyStartTimeout_ShowsStartFailedBannerWithRetry() async throws {
        let f = await ViewFixture.make()
        let text = ViewFixture.statusJSON(forwarding: false, relayCount: 1)
            .replacingOccurrences(of: #""proxy":{"running":true,"error":null}"#, with: #""proxy":{"running":false,"error":"proxy_start_timeout"}"#)
        f.model.status = decodedStatus(text)
        let banner = try XCTUnwrap(f.model.banners.first { $0.title == "本机转发服务没能启动" })
        XCTAssertEqual(banner.detail, "启动转发超过 30 秒还没完成，请重试")
        XCTAssertEqual(banner.kind, .error)
        XCTAssertNotNil(banner.action)
    }
}
