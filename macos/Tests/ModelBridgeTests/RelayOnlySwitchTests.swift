import XCTest
@testable import ModelBridge

private func quoted(_ value: String?) -> String { value.map { "\"\($0)\"" } ?? "null" }

private func statusJSON(
    forwarding: Bool = true,
    mode: String = "relay",
    target: String? = nil,
    detect: String = "logged_in",
    relayOnly: Bool,
    loggedIn: Bool,
    defaultRelay: String? = "配置1"
) -> String {
    """
    {"version":"1.3.0","forwarding":\(forwarding),"proxy":{"running":true,"error":null},\
    "chatgpt":{"found":true,"error":null},"default_relay":\(quoted(defaultRelay)),"relays":[],\
    "taken_over":[],"restore_blocked":[],"config_path":"/Users/example/.codex/config.toml",\
    "update":{"latest_version":null,"url":null},\
    "access":{"mode":"\(mode)","target":\(quoted(target ?? mode)),"detect":"\(detect)",\
    "relay_only":\(relayOnly),"logged_in":\(loggedIn)}}
    """
}

private func result(_ json: String) -> Result<Data, CoreError> { .success(Data(json.utf8)) }

private func decode(_ json: String) -> CoreStatus {
    let decoder = JSONDecoder()
    decoder.keyDecodingStrategy = .convertFromSnakeCase
    return try! decoder.decode(CoreStatus.self, from: Data(json.utf8))
}

@MainActor
private struct Harness {
    let core: FakeCore
    let model: AppModel

    init(
        status: String,
        responses: [String: Result<Data, CoreError>] = [:],
        gates: [String: Gate] = [:],
        chatGPTRunning: Bool = true
    ) {
        let log = CallLog()
        core = FakeCore(log: log, responses: responses, gates: gates)
        model = AppModel(
            core: core,
            keychain: RelayFakeKeychain(log: log, entries: [:]),
            settings: RelayFakeSettings(log: log, relays: []),
            loginItem: FakeLoginItem(log: log),
            chatGPT: FakeChatGPT(running: chatGPTRunning)
        )
        model.status = decode(status)
    }
}

@MainActor
final class RelayOnlySwitchTests: XCTestCase {
    func testSwitchUsable_LoggedIn_True() {
        let h = Harness(status: statusJSON(relayOnly: false, loggedIn: true))
        XCTAssertTrue(h.model.relayOnlySwitchUsable)
    }

    func testSwitchUsable_NotLoggedIn_FalseAndShowsRelayOnlyValue() {
        for relayOnly in [true, false] {
            let h = Harness(status: statusJSON(relayOnly: relayOnly, loggedIn: false))
            XCTAssertFalse(h.model.relayOnlySwitchUsable, "relayOnly=\(relayOnly)")
            XCTAssertEqual(h.model.relayOnlyEnabled, relayOnly)
        }
    }

    func testSwitchUsable_WhileDetecting_StillTrue() async {
        let gate = Gate()
        let h = Harness(
            status: statusJSON(mode: "both", relayOnly: false, loggedIn: true),
            responses: ["detect_access": result(statusJSON(mode: "both", relayOnly: false, loggedIn: true))],
            gates: ["detect_access": gate]
        )
        let detect = Task { await h.model.recheckAccess() }
        await awaitUntil { h.model.isDetectingAccess }
        XCTAssertTrue(h.model.isDetectingAccess)
        XCTAssertTrue(h.model.relayOnlySwitchUsable)
        await gate.open()
        await detect.value
    }

    func testHelpText_Both() {
        let h = Harness(status: statusJSON(mode: "both", relayOnly: false, loggedIn: true))
        XCTAssertEqual(h.model.accessHelpText, "官方模型在上，中转模型在下；选官方模型时仍访问官方服务。")
    }

    func testHelpText_RelayOnlyAndLoggedIn() {
        let h = Harness(status: statusJSON(relayOnly: true, loggedIn: true))
        XCTAssertEqual(h.model.accessHelpText, "已登录 ChatGPT，手动选了只用中转。只有中转模型，默认选中「配置1 · auto」。")
    }

    func testHelpText_RelayOnlyAndLoggedIn_NoDefaultRelay() {
        let h = Harness(status: statusJSON(relayOnly: true, loggedIn: true, defaultRelay: nil))
        XCTAssertEqual(h.model.accessHelpText, "已登录 ChatGPT，手动选了只用中转。只有中转模型，没有默认选中的模型。")
    }

    func testHelpText_RelayOtherCases() {
        let cases: [(relayOnly: Bool, loggedIn: Bool)] = [(false, false), (true, false), (false, true)]
        for c in cases {
            let h = Harness(status: statusJSON(relayOnly: c.relayOnly, loggedIn: c.loggedIn))
            XCTAssertEqual(
                h.model.accessHelpText,
                "没有登录 ChatGPT，只能用中转模型，默认选中「配置1 · auto」。登录后才能改接入方式。",
                "\(c)"
            )
        }
    }

    func testHelpText_RelayOtherCases_NoDefaultRelay() {
        let h = Harness(status: statusJSON(relayOnly: false, loggedIn: false, defaultRelay: nil))
        XCTAssertEqual(h.model.accessHelpText, "没有登录 ChatGPT，只能用中转模型，没有默认选中的模型。登录后才能改接入方式。")
    }

    func testSetRelayOnly_On_ChatGPTRunning_SendsParamsAndShowsSuccessNotice() async {
        let h = Harness(
            status: statusJSON(mode: "both", relayOnly: false, loggedIn: true),
            responses: ["set_relay_only": result(statusJSON(mode: "relay", relayOnly: true, loggedIn: true))]
        )
        await h.model.setRelayOnly(true)
        XCTAssertEqual(h.core.calls("set_relay_only").map(\.params), [#"{"enabled":true}"#])
        XCTAssertTrue(h.model.relayOnlyEnabled)
        let notice = h.model.notices.last
        XCTAssertEqual(notice?.title, "已切换到「只用中转」")
        XCTAssertEqual(notice?.detail, "重启 ChatGPT 后生效。重启后请新建对话：用「官方 + 中转」聊过的旧对话接不上。")
        XCTAssertEqual(notice?.kind, .success)
    }

    func testSetRelayOnly_Off_ChatGPTRunning_SendsParamsAndShowsSuccessNotice() async {
        let h = Harness(
            status: statusJSON(relayOnly: true, loggedIn: true),
            responses: ["set_relay_only": result(statusJSON(mode: "both", relayOnly: false, loggedIn: true))]
        )
        await h.model.setRelayOnly(false)
        XCTAssertEqual(h.core.calls("set_relay_only").map(\.params), [#"{"enabled":false}"#])
        XCTAssertFalse(h.model.relayOnlyEnabled)
        let notice = h.model.notices.last
        XCTAssertEqual(notice?.title, "已切换到「官方 + 中转」")
        XCTAssertEqual(notice?.detail, "重启 ChatGPT 后生效。重启后请新建对话：用「只用中转」聊过的旧对话接不上。")
        XCTAssertEqual(notice?.kind, .success)
    }

    func testSetRelayOnly_ChatGPTNotRunning_ShowsNoNotice() async {
        for enabled in [true, false] {
            let h = Harness(
                status: statusJSON(mode: enabled ? "both" : "relay", relayOnly: !enabled, loggedIn: true),
                responses: ["set_relay_only": result(statusJSON(mode: enabled ? "relay" : "both", relayOnly: enabled, loggedIn: true))],
                chatGPTRunning: false
            )
            await h.model.setRelayOnly(enabled)
            XCTAssertEqual(h.core.calls("set_relay_only").count, 1, "enabled=\(enabled)")
            XCTAssertTrue(h.model.notices.isEmpty, "enabled=\(enabled)")
        }
    }

    func testSetRelayOnly_SuccessNoticeAutoDismisses() async {
        let h = Harness(
            status: statusJSON(mode: "both", relayOnly: false, loggedIn: true),
            responses: ["set_relay_only": result(statusJSON(mode: "relay", relayOnly: true, loggedIn: true))]
        )
        h.model.noticeAutoDismissDelay = 0.05
        await h.model.setRelayOnly(true)
        XCTAssertEqual(h.model.notices.count, 1)
        await awaitUntil { h.model.notices.isEmpty }
        XCTAssertTrue(h.model.notices.isEmpty)
    }

    func testSetRelayOnly_Failure_ShowsPersistentErrorAndKeepsState() async {
        let h = Harness(
            status: statusJSON(mode: "both", relayOnly: false, loggedIn: true),
            responses: ["set_relay_only": .failure(.remote(code: "config_invalid", keys: []))]
        )
        h.model.noticeAutoDismissDelay = 0.05
        await h.model.setRelayOnly(true)
        let notice = h.model.notices.last
        XCTAssertEqual(notice?.title, "没能切换接入方式")
        XCTAssertEqual(notice?.detail, "没能修改 Codex 配置，接入方式保持原样。请稍后再试。")
        XCTAssertEqual(notice?.kind, .error)
        XCTAssertFalse(h.model.relayOnlyEnabled)
        XCTAssertEqual(h.model.status?.access?.mode, "both")
        try? await Task.sleep(nanoseconds: 300_000_000)
        XCTAssertEqual(h.model.notices.last?.title, "没能切换接入方式")
    }

    func testSetRelayOnly_Failure_Off_KeepsRelayOnlyOn() async {
        let h = Harness(
            status: statusJSON(relayOnly: true, loggedIn: true),
            responses: ["set_relay_only": .failure(.remote(code: "config_invalid", keys: []))]
        )
        await h.model.setRelayOnly(false)
        XCTAssertTrue(h.model.relayOnlyEnabled)
        XCTAssertEqual(h.model.status?.access?.mode, "relay")
    }

    func testSetRelayOnly_WhileSwitchingForwarding_DoesNotCallCore() async {
        let gate = Gate()
        let h = Harness(
            status: statusJSON(forwarding: false, mode: "both", relayOnly: false, loggedIn: true),
            responses: ["set_forwarding": result(statusJSON(forwarding: true, mode: "both", relayOnly: false, loggedIn: true))],
            gates: ["set_forwarding": gate]
        )
        let toggle = Task { await h.model.toggleForwarding(enabled: true) }
        await awaitUntil { !h.core.calls("set_forwarding").isEmpty }
        XCTAssertTrue(h.model.isSwitchingForwarding)
        await h.model.setRelayOnly(true)
        XCTAssertEqual(h.core.calls("set_relay_only").count, 0)
        await gate.open()
        await toggle.value
    }

    func testRecheck_UnchangedRelayOnlyLoggedIn_ShowsManualChoiceNotice() async {
        let json = statusJSON(mode: "relay", target: "relay", detect: "logged_in", relayOnly: true, loggedIn: true)
        let h = Harness(status: json, responses: ["detect_access": result(json)])
        await h.model.recheckAccess()
        let notice = h.model.notices.last
        XCTAssertEqual(notice?.title, "接入方式没有变化")
        XCTAssertEqual(notice?.detail, "仍然检测到 ChatGPT 登录。你手动选了只用中转，继续只用中转模型。")
        XCTAssertEqual(notice?.kind, .info)
    }

    func testRecheck_UnchangedRelayNotRelayOnly_KeepsExistingSentence() async {
        let json = statusJSON(mode: "relay", target: "relay", detect: "not_logged_in", relayOnly: false, loggedIn: false)
        let h = Harness(status: json, responses: ["detect_access": result(json)])
        await h.model.recheckAccess()
        XCTAssertEqual(h.model.notices.last?.detail, "仍然没有检测到 ChatGPT 登录，继续只用中转模型。")
    }

    func testRecheck_UnchangedBoth_KeepsExistingSentence() async {
        let json = statusJSON(mode: "both", target: "both", detect: "logged_in", relayOnly: false, loggedIn: true)
        let h = Harness(status: json, responses: ["detect_access": result(json)])
        await h.model.recheckAccess()
        XCTAssertEqual(h.model.notices.last?.detail, "仍然检测到 ChatGPT 登录，继续同时使用官方模型和中转模型。")
    }

    func testView_ForwardingOn_AccessRowShowsSwitchStateText() async {
        let h = Harness(status: statusJSON(mode: "both", relayOnly: false, loggedIn: true))
        let view = RenderedView(MainWindow(model: h.model))
        XCTAssertTrue(view.has("只用中转 · 已关闭"))
        XCTAssertTrue(view.has("重新检测"))
    }

    func testView_RelayOnlyOn_ShowsEnabledText() async {
        let h = Harness(status: statusJSON(relayOnly: true, loggedIn: true))
        let view = RenderedView(MainWindow(model: h.model))
        XCTAssertTrue(view.has("只用中转 · 已开启"))
    }

    func testView_ForwardingOff_NoRelayOnlyRow() async {
        let h = Harness(status: statusJSON(forwarding: false, mode: "both", relayOnly: false, loggedIn: true))
        let view = RenderedView(MainWindow(model: h.model))
        XCTAssertFalse(view.has("只用中转 · 已关闭"))
        XCTAssertFalse(view.has("重新检测"))
    }

    func testView_NotLoggedIn_SwitchDisabledLoggedIn_Enabled() async {
        let off = Harness(status: statusJSON(relayOnly: true, loggedIn: false))
        XCTAssertEqual(RenderedView(MainWindow(model: off.model)).isEnabled(label: "只用中转"), false)
        let on = Harness(status: statusJSON(mode: "both", relayOnly: false, loggedIn: true))
        XCTAssertEqual(RenderedView(MainWindow(model: on.model)).isEnabled(label: "只用中转"), true)
    }

    func testView_AccessRowIsOneLine_CardKeepsTwoRows() async {
        let h = Harness(status: statusJSON(mode: "both", relayOnly: false, loggedIn: true))
        let view = RenderedView(MainWindow(model: h.model))
        let ys = ["接入方式", "只用中转 · 已关闭", "重新检测"].compactMap { view.frame(ofText: $0)?.midY }
        XCTAssertEqual(ys.count, 3)
        guard let first = ys.first else { return }
        XCTAssertTrue(ys.allSatisfy { abs($0 - first) < 8 }, "\(ys)")
    }

    func testDuringSetRelayOnly_ForwardingToggleDisabledAndOtherSwitchesSkipped() async {
        let gate = Gate()
        let h = Harness(
            status: statusJSON(mode: "both", relayOnly: false, loggedIn: true),
            responses: [
                "set_relay_only": result(statusJSON(mode: "relay", relayOnly: true, loggedIn: true)),
                "detect_access": result(statusJSON(mode: "both", target: "relay", relayOnly: false, loggedIn: true)),
            ],
            gates: ["set_relay_only": gate]
        )
        let switching = Task { await h.model.setRelayOnly(true) }
        await awaitUntil { !h.core.calls("set_relay_only").isEmpty }
        XCTAssertTrue(h.model.forwardingToggleDisabled)
        await h.model.toggleForwarding(enabled: false)
        XCTAssertEqual(h.core.calls("set_forwarding").count, 0)
        await h.model.recheckAccess()
        XCTAssertEqual(h.core.calls("apply_access").count, 0)
        await gate.open()
        await switching.value
        XCTAssertFalse(h.model.forwardingToggleDisabled)
    }

    func testDuringSetRelayOnly_SecondSetRelayOnlyDoesNotCallCoreAgain() async {
        let gate = Gate()
        let h = Harness(
            status: statusJSON(mode: "both", relayOnly: false, loggedIn: true),
            responses: ["set_relay_only": result(statusJSON(mode: "relay", relayOnly: true, loggedIn: true))],
            gates: ["set_relay_only": gate]
        )
        let first = Task { await h.model.setRelayOnly(true) }
        await awaitUntil { !h.core.calls("set_relay_only").isEmpty }
        await h.model.setRelayOnly(false)
        XCTAssertEqual(h.core.calls("set_relay_only").count, 1)
        await gate.open()
        await first.value
    }

    func testView_SwitchAccessibilityNameAndHelpMatchSubtitle() async {
        let h = Harness(status: statusJSON(relayOnly: true, loggedIn: true))
        let view = RenderedView(MainWindow(model: h.model))
        let toggle = view.elements.first { $0.axString("accessibilityLabel") == "只用中转" }
        XCTAssertNotNil(toggle)
        XCTAssertEqual(toggle?.axString("accessibilityHelp"), h.model.accessHelpText)
    }

    func testView_TabOrder_ForwardingThenRelayOnlyThenRecheck() async {
        let h = Harness(status: statusJSON(mode: "both", relayOnly: false, loggedIn: true))
        let labels = RenderedView(MainWindow(model: h.model)).elements.compactMap { $0.axString("accessibilityLabel") }
        let order = ["使用中转", "只用中转", "重新检测"].map { labels.firstIndex(of: $0) }
        XCTAssertFalse(order.contains(nil), "\(labels)")
        XCTAssertEqual(order.compactMap { $0 }, order.compactMap { $0 }.sorted())
    }

    func testView_DisabledRelayOnlySwitch_IsNotFocusable() async {
        let h = Harness(status: statusJSON(relayOnly: true, loggedIn: false))
        let view = RenderedView(MainWindow(model: h.model))
        let toggle = view.elements.first { $0.axString("accessibilityLabel") == "只用中转" }
        XCTAssertEqual(toggle?.axBool("isAccessibilityEnabled"), false)
    }

    func testSetRelayOnlyDuringDetection_StaleDetectResultDoesNotOverrideOrNotify() async {
        let gate = Gate()
        let before = statusJSON(mode: "both", target: "both", detect: "logged_in", relayOnly: false, loggedIn: true)
        let h = Harness(
            status: before,
            responses: [
                "detect_access": result(before),
                "set_relay_only": result(statusJSON(mode: "relay", relayOnly: true, loggedIn: true)),
            ],
            gates: ["detect_access": gate]
        )
        let detect = Task { await h.model.recheckAccess() }
        await awaitUntil { !h.core.calls("detect_access").isEmpty }
        XCTAssertTrue(h.model.relayOnlySwitchUsable)
        await h.model.setRelayOnly(true)
        XCTAssertTrue(h.model.relayOnlyEnabled)
        await gate.open()
        await detect.value
        XCTAssertFalse(h.model.notices.contains { $0.title == "接入方式没有变化" })
        XCTAssertTrue(h.model.relayOnlyEnabled)
        XCTAssertEqual(h.model.accessModeName, "只用中转")
        XCTAssertEqual(h.core.calls("apply_access").count, 0)
    }
}
