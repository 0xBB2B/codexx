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
    defaultRelay: String? = "配置1",
    restoreBlocked: String = ""
) -> String {
    """
    {"version":"1.3.0","forwarding":\(forwarding),"proxy":{"running":true,"error":null},\
    "chatgpt":{"found":true,"error":null},"default_relay":\(quoted(defaultRelay)),"relays":[],\
    "taken_over":[],"restore_blocked":[\(restoreBlocked)],"config_path":"/Users/example/.codex/config.toml",\
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
        let cases: [(relayOnly: Bool, loggedIn: Bool)] = [(false, false), (true, false)]
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

    func testSetRelayOnly_SuccessAfterFailure_ClearsErrorNotice() async {
        let h = Harness(
            status: statusJSON(mode: "both", relayOnly: false, loggedIn: true),
            responses: ["set_relay_only": .failure(.remote(code: "config_invalid", keys: []))]
        )
        await h.model.setRelayOnly(true)
        XCTAssertTrue(h.model.notices.contains { $0.title == "没能切换接入方式" })
        h.core.setResponse("set_relay_only", result(statusJSON(mode: "relay", relayOnly: true, loggedIn: true)))
        await h.model.setRelayOnly(true)
        XCTAssertFalse(h.model.notices.contains { $0.title == "没能切换接入方式" })
        XCTAssertTrue(h.model.notices.contains { $0.title == "已切换到「只用中转」" })
    }

    private func switchBodyColor(_ view: RenderedView, label: String) throws -> (r: Int, g: Int, b: Int) {
        let shot = try XCTUnwrap(view.snapshot())
        let host = try XCTUnwrap(view.elements.first?.axFrame())
        let button = view.elements.first {
            $0.axString("accessibilityLabel") == label && $0.axString("accessibilityRole") == NSAccessibility.Role.button.rawValue
        }
        let frame = try XCTUnwrap(button?.axFrame())
        let x = Int((frame.minX - host.minX + 6) * shot.scale)
        let y = Int((host.maxY - frame.maxY + 12) * shot.scale)
        let c = try XCTUnwrap(shot.rep.colorAt(x: x, y: y)?.usingColorSpace(.sRGB))
        return (Int(c.redComponent * 255), Int(c.greenComponent * 255), Int(c.blueComponent * 255))
    }

    private func assertNeutral(_ c: (r: Int, g: Int, b: Int), file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertLessThan(c.g - c.r, 20, "\(c)", file: file, line: line)
        XCTAssertLessThan(c.b - c.r, 25, "\(c)", file: file, line: line)
    }

    private func assertTeal(_ c: (r: Int, g: Int, b: Int), file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertGreaterThan(c.g - c.r, 60, "\(c)", file: file, line: line)
    }

    func testView_RelayOnlyDisabledAndOn_SwitchBodyIsNeutral() async throws {
        let h = Harness(status: statusJSON(relayOnly: true, loggedIn: false))
        assertNeutral(try switchBodyColor(RenderedView(MainWindow(model: h.model)), label: "只用中转"))
    }

    func testView_RelayOnlyEnabledAndOn_SwitchBodyIsPrimary() async throws {
        let h = Harness(status: statusJSON(relayOnly: true, loggedIn: true))
        assertTeal(try switchBodyColor(RenderedView(MainWindow(model: h.model)), label: "只用中转"))
    }

    func testView_ForwardingSwitchingWhileOn_ForwardingSwitchBodyIsNeutral() async throws {
        let gate = Gate()
        let h = Harness(
            status: statusJSON(relayOnly: false, loggedIn: true),
            responses: ["set_forwarding": result(statusJSON(forwarding: false, relayOnly: false, loggedIn: true))],
            gates: ["set_forwarding": gate]
        )
        let toggle = Task { await h.model.toggleForwarding(enabled: false) }
        await awaitUntil { h.model.isSwitchingForwarding }
        XCTAssertTrue(h.model.forwardingEnabled)
        assertNeutral(try switchBodyColor(RenderedView(MainWindow(model: h.model)), label: "使用中转"))
        await gate.open()
        await toggle.value
    }

    func testView_ForwardingEnabledAndOn_ForwardingSwitchBodyIsPrimary() async throws {
        let h = Harness(status: statusJSON(relayOnly: false, loggedIn: true))
        assertTeal(try switchBodyColor(RenderedView(MainWindow(model: h.model)), label: "使用中转"))
    }

    private func darkestRed(_ view: RenderedView, textContaining text: String) throws -> Int {
        let shot = try XCTUnwrap(view.snapshot())
        let host = try XCTUnwrap(view.elements.first?.axFrame())
        let label = view.elements.first { element in
            element.axString("accessibilityRole") == NSAccessibility.Role.staticText.rawValue
                && ["accessibilityLabel", "accessibilityValue"].contains { element.axString($0)?.contains(text) == true }
        }
        let frame = try XCTUnwrap(label?.axFrame())
        let region = CGRect(x: frame.minX - host.minX, y: host.maxY - frame.maxY, width: frame.width, height: frame.height)
        var darkest = 255
        for y in Int(region.minY * shot.scale)..<Int(region.maxY * shot.scale) {
            for x in Int(region.minX * shot.scale)..<Int(region.maxX * shot.scale) {
                if let c = shot.rep.colorAt(x: x, y: y)?.usingColorSpace(.sRGB) { darkest = min(darkest, Int(c.redComponent * 255)) }
            }
        }
        return darkest
    }

    func testView_RelayOnlyDisabledAndOn_KnobIsLightNeutralGrey() async throws {
        let h = Harness(status: statusJSON(relayOnly: true, loggedIn: false))
        let view = RenderedView(MainWindow(model: h.model))
        let shot = try XCTUnwrap(view.snapshot())
        let host = try XCTUnwrap(view.elements.first?.axFrame())
        let button = view.elements.first {
            $0.axString("accessibilityLabel") == "只用中转" && $0.axString("accessibilityRole") == NSAccessibility.Role.button.rawValue
        }
        let frame = try XCTUnwrap(button?.axFrame())
        let x = Int((frame.minX - host.minX + 27) * shot.scale)
        let y = Int((host.maxY - frame.maxY + 12) * shot.scale)
        let c = try XCTUnwrap(shot.rep.colorAt(x: x, y: y)?.usingColorSpace(.sRGB))
        let rgb = [c.redComponent, c.greenComponent, c.blueComponent].map { Int($0 * 255) }
        XCTAssertTrue(rgb.allSatisfy { $0 > 200 }, "\(rgb)")
        XCTAssertLessThan(rgb[1] - rgb[0], 20, "\(rgb)")
    }

    func testView_RelayOnlyDisabled_StateTextLighterThanEnabled() async throws {
        let disabled = Harness(status: statusJSON(relayOnly: true, loggedIn: false))
        let enabled = Harness(status: statusJSON(relayOnly: true, loggedIn: true))
        let light = try darkestRed(RenderedView(MainWindow(model: disabled.model)), textContaining: "只用中转 · ")
        let dark = try darkestRed(RenderedView(MainWindow(model: enabled.model)), textContaining: "只用中转 · ")
        XCTAssertLessThan(dark, 60)
        XCTAssertGreaterThan(light, 70)
    }

    func testSwitchUsable_AfterCoreDidFail_FalseAndKeepsValue() {
        for relayOnly in [true, false] {
            let h = Harness(status: statusJSON(mode: relayOnly ? "relay" : "both", relayOnly: relayOnly, loggedIn: true))
            h.model.coreDidFail()
            XCTAssertFalse(h.model.relayOnlySwitchUsable, "relayOnly=\(relayOnly)")
            XCTAssertEqual(h.model.relayOnlyEnabled, relayOnly)
            XCTAssertEqual(RenderedView(MainWindow(model: h.model)).isEnabled(label: "只用中转"), false)
        }
    }

    func testSwitchUsable_DuringSetRelayOnly_FalseAndKeepsValue() async {
        let gate = Gate()
        let h = Harness(
            status: statusJSON(mode: "both", relayOnly: false, loggedIn: true),
            responses: ["set_relay_only": result(statusJSON(mode: "relay", relayOnly: true, loggedIn: true))],
            gates: ["set_relay_only": gate]
        )
        let switching = Task { await h.model.setRelayOnly(true) }
        await awaitUntil { !h.core.calls("set_relay_only").isEmpty }
        XCTAssertFalse(h.model.relayOnlySwitchUsable)
        XCTAssertFalse(h.model.relayOnlyEnabled)
        XCTAssertEqual(RenderedView(MainWindow(model: h.model)).isEnabled(label: "只用中转"), false)
        await gate.open()
        await switching.value
        XCTAssertTrue(h.model.relayOnlySwitchUsable)
    }

    func testSwitchUsable_DuringSetForwarding_False() async {
        let gate = Gate()
        let h = Harness(
            status: statusJSON(mode: "both", relayOnly: false, loggedIn: true),
            responses: ["set_forwarding": result(statusJSON(mode: "both", relayOnly: false, loggedIn: true))],
            gates: ["set_forwarding": gate]
        )
        let toggle = Task { await h.model.toggleForwarding(enabled: false) }
        await awaitUntil { !h.core.calls("set_forwarding").isEmpty }
        XCTAssertFalse(h.model.relayOnlySwitchUsable)
        await gate.open()
        await toggle.value
    }

    func testSwitchUsable_DuringApplyAccess_False() async {
        let gate = Gate()
        let h = Harness(
            status: statusJSON(mode: "relay", target: "both", relayOnly: false, loggedIn: true),
            responses: [
                "detect_access": result(statusJSON(mode: "relay", target: "both", relayOnly: false, loggedIn: true)),
                "apply_access": result(statusJSON(mode: "both", relayOnly: false, loggedIn: true)),
            ],
            gates: ["apply_access": gate]
        )
        let detect = Task { await h.model.recheckAccess() }
        await awaitUntil { !h.core.calls("apply_access").isEmpty }
        XCTAssertFalse(h.model.relayOnlySwitchUsable)
        await gate.open()
        await detect.value
    }

    func testHelpText_RelayLoggedInNotRelayOnly_ExplainsFailedSwitch() {
        let h = Harness(status: statusJSON(relayOnly: false, loggedIn: true))
        XCTAssertEqual(h.model.accessHelpText, "已登录 ChatGPT，但没能切到「官方 + 中转」，暂时只有中转模型。点「重新检测」再试。")
    }

    func testSetRelayOnly_NoticeTitleFollowsActualMode() async {
        let h = Harness(
            status: statusJSON(mode: "both", relayOnly: false, loggedIn: true),
            responses: ["set_relay_only": result(statusJSON(mode: "relay", relayOnly: true, loggedIn: true))]
        )
        await h.model.setRelayOnly(false)
        XCTAssertEqual(h.model.notices.map(\.title), ["已切换到「只用中转」"])
    }

    func testSetRelayOnly_ModeUnchanged_ShowsNoSwitchNotice() async {
        let cases: [(before: String, after: String)] = [("both", "both"), ("relay", "relay")]
        for c in cases {
            let h = Harness(
                status: statusJSON(mode: c.before, relayOnly: false, loggedIn: true),
                responses: ["set_relay_only": result(statusJSON(mode: c.after, relayOnly: false, loggedIn: true))]
            )
            await h.model.setRelayOnly(true)
            XCTAssertFalse(h.model.notices.contains { $0.title.hasPrefix("已切换到") }, c.before)
        }
    }

    func testSetRelayOnly_OnThenOffWithinDismissDelay_SwitchNoticeReplacedInPlace() async {
        let h = Harness(
            status: statusJSON(mode: "both", relayOnly: false, loggedIn: true),
            responses: ["set_relay_only": result(statusJSON(mode: "relay", relayOnly: true, loggedIn: true))]
        )
        h.core.enqueue("detect_access", [result(statusJSON(mode: "relay", relayOnly: true, loggedIn: true))])
        await h.model.setRelayOnly(true)
        await h.model.recheckAccess()
        XCTAssertEqual(h.model.notices.map(\.title), ["已切换到「只用中转」", "接入方式没有变化"])
        h.core.setResponse("set_relay_only", result(statusJSON(mode: "both", relayOnly: false, loggedIn: true)))
        await h.model.setRelayOnly(false)
        XCTAssertEqual(h.model.notices.map(\.title), ["已切换到「官方 + 中转」", "接入方式没有变化"])
    }

    func testRecheckAutoSwitch_AfterManualSwitchNotice_LeavesOnlyLatestSwitchNotice() async {
        let h = Harness(
            status: statusJSON(mode: "both", relayOnly: false, loggedIn: true),
            responses: [
                "set_relay_only": result(statusJSON(mode: "relay", relayOnly: true, loggedIn: true)),
                "apply_access": result(statusJSON(mode: "both", relayOnly: false, loggedIn: true)),
            ]
        )
        h.core.enqueue("detect_access", [result(statusJSON(mode: "relay", target: "both", relayOnly: true, loggedIn: true))])
        await h.model.setRelayOnly(true)
        await h.model.recheckAccess()
        XCTAssertEqual(h.model.notices.map(\.title), ["已切换到「官方 + 中转」"])
    }

    func testRecheckAutoSwitch_ManualOnOffThenRecheck_SwitchNoticeReplacedInOriginalPosition() async {
        let h = Harness(
            status: statusJSON(mode: "both", relayOnly: false, loggedIn: true),
            responses: [
                "set_relay_only": result(statusJSON(mode: "relay", relayOnly: true, loggedIn: true)),
                "apply_access": result(statusJSON(mode: "both", relayOnly: false, loggedIn: true)),
            ]
        )
        h.core.enqueue("detect_access", [result(statusJSON(mode: "relay", target: "relay", relayOnly: true, loggedIn: true))])
        await h.model.setRelayOnly(true)
        await h.model.recheckAccess()
        XCTAssertEqual(h.model.notices.map(\.title), ["已切换到「只用中转」", "接入方式没有变化"])
        h.core.setResponse("set_relay_only", result(statusJSON(mode: "relay", relayOnly: false, loggedIn: true)))
        h.core.enqueue("detect_access", [result(statusJSON(mode: "relay", target: "both", relayOnly: false, loggedIn: true))])
        await h.model.setRelayOnly(false)
        await h.model.recheckAccess()
        XCTAssertEqual(h.core.calls("apply_access").count, 1)
        XCTAssertEqual(h.model.notices.map(\.title), ["已切换到「官方 + 中转」", "接入方式没有变化"])
    }

    func testStaleDetect_DoesNotOverrideOtherStatusFields() async {
        let gate = Gate()
        let before = statusJSON(mode: "both", relayOnly: false, loggedIn: true)
        let h = Harness(
            status: before,
            responses: [
                "detect_access": result(statusJSON(mode: "both", relayOnly: false, loggedIn: true, restoreBlocked: #""a""#)),
                "set_relay_only": result(statusJSON(mode: "relay", relayOnly: true, loggedIn: true)),
            ],
            gates: ["detect_access": gate]
        )
        let detect = Task { await h.model.recheckAccess() }
        await awaitUntil { !h.core.calls("detect_access").isEmpty }
        await h.model.setRelayOnly(true)
        await gate.open()
        await detect.value
        XCTAssertEqual(h.model.status?.restoreBlocked, [])
        XCTAssertTrue(h.model.relayOnlyEnabled)
        XCTAssertEqual(h.model.status?.access?.mode, "relay")
    }

    func testStaleDetect_FailedDetect_StillShowsDetectFailedWarning() async {
        let gate = Gate()
        let h = Harness(
            status: statusJSON(mode: "both", relayOnly: false, loggedIn: true),
            responses: [
                "detect_access": result(statusJSON(mode: "both", detect: "failed", relayOnly: false, loggedIn: true)),
                "set_relay_only": result(statusJSON(mode: "relay", relayOnly: true, loggedIn: true)),
            ],
            gates: ["detect_access": gate]
        )
        let detect = Task { await h.model.recheckAccess() }
        await awaitUntil { !h.core.calls("detect_access").isEmpty }
        await h.model.setRelayOnly(true)
        await gate.open()
        await detect.value
        XCTAssertTrue(h.model.notices.contains { $0.title == "没能检测 ChatGPT 登录状态" && $0.kind == .warning })
        XCTAssertTrue(h.model.relayOnlyEnabled)
    }

    func testView_LongRelayName_HelpTextIsOneLineWithFullTooltip() async {
        let name = String(repeating: "长", count: 16)
        let long = Harness(status: statusJSON(relayOnly: true, loggedIn: true, defaultRelay: name))
        let reference = Harness(status: statusJSON(mode: "both", relayOnly: false, loggedIn: true))
        func helpElement(_ h: Harness, _ text: String) -> NSObject? {
            RenderedView(MainWindow(model: h.model).frame(width: 520)).elements.first { element in
                element.axString("accessibilityRole") == NSAccessibility.Role.staticText.rawValue
                    && ["accessibilityLabel", "accessibilityValue"].contains { element.axString($0)?.contains(text) == true }
            }
        }
        let full = long.model.accessHelpText ?? ""
        XCTAssertTrue(full.contains(name))
        let element = helpElement(long, "手动选了只用中转")
        let ref = helpElement(reference, "官方模型在上")
        XCTAssertNotNil(element)
        XCTAssertNotNil(ref)
        let height = element?.axFrame()?.height ?? 0
        XCTAssertGreaterThan(height, 0)
        XCTAssertLessThan(height, (ref?.axFrame()?.height ?? 0) * 1.5)
        XCTAssertEqual(element?.axString("accessibilityHelp"), full)
    }
}
