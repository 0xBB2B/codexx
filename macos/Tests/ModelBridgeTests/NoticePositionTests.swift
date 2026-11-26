import AppKit
import XCTest
@testable import ModelBridge

private struct Bar {
    let frame: NSRect
    let own: Set<String>
}

private func signature(_ element: NSObject) -> String {
    "\(element.axString("accessibilityRole") ?? "")|\(element.axString("accessibilityLabel") ?? "")|\(NSStringFromRect(element.axFrame() ?? .zero))"
}

private func descendants(_ object: NSObject) -> [NSObject] {
    let children = (object.axObject("accessibilityChildren") as? [NSObject]) ?? []
    return children.flatMap { [$0] + descendants($0) }
}

private func axTexts(of element: NSObject) -> [String] {
    ["accessibilityLabel", "accessibilityValue"].compactMap { element.axString($0) }
}

extension RenderedView {
    fileprivate func bar(titled title: String) -> Bar? {
        let found = elements.dropFirst()
            .filter { $0.axString("accessibilityRole") == NSAccessibility.Role.group.rawValue }
            .compactMap { group -> (NSObject, [NSObject])? in
                let members = descendants(group)
                return members.contains { axTexts(of: $0).contains(title) } ? (group, members) : nil
            }
        let best = found.min { lhs, rhs in
            let a = lhs.0.axFrame() ?? .zero, b = rhs.0.axFrame() ?? .zero
            return a.width * a.height < b.width * b.height
        }
        guard let (group, members) = best, let frame = group.axFrame() else { return nil }
        return Bar(frame: frame, own: Set(members.map(signature)))
    }

    fileprivate func controls(roles: Set<String>, labels: Set<String>? = nil, excluding bars: [Bar]) -> [(label: String, frame: NSRect)] {
        elements.compactMap { element in
            guard let role = element.axString("accessibilityRole"), roles.contains(role),
                  let label = element.axString("accessibilityLabel"), !label.isEmpty,
                  labels.map({ $0.contains(label) }) ?? true,
                  !bars.contains(where: { $0.own.contains(signature(element)) }),
                  let frame = element.axFrame() else { return nil }
            return (label, frame)
        }
    }
}

private let mainButtonLabels: Set<String> = ["使用中转", "只用中转", "重新检测", "编辑", "删除", "添加配置", "检查更新"]
private let button = NSAccessibility.Role.button.rawValue
private let textField = NSAccessibility.Role.textField.rawValue

@MainActor private func assertStack(
    _ bars: [Bar], in window: NSWindow, width: CGFloat,
    file: StaticString = #filePath, line: UInt = #line
) {
    var expectedTop = window.frame.maxY - AppWindows.titleBarHeight - 12
    for (index, bar) in bars.enumerated() {
        XCTAssertEqual(bar.frame.minX - window.frame.minX, 24, accuracy: 1.5, "bar \(index) 距左边", file: file, line: line)
        XCTAssertEqual(bar.frame.width, width, accuracy: 1.5, "bar \(index) 宽度", file: file, line: line)
        XCTAssertEqual(bar.frame.maxY, expectedTop, accuracy: 1.5, "bar \(index) 距上方", file: file, line: line)
        expectedTop = bar.frame.minY - 8
    }
}

private func assertNoOverlap(
    _ bars: [Bar], _ controls: [(label: String, frame: NSRect)],
    file: StaticString = #filePath, line: UInt = #line
) {
    for bar in bars {
        for control in controls {
            XCTAssertTrue(NSIntersectionRect(bar.frame, control.frame).isEmpty, "提示条盖住了「\(control.label)」", file: file, line: line)
        }
    }
}

private func decodeStatus(_ json: String) -> CoreStatus {
    let decoder = JSONDecoder()
    decoder.keyDecodingStrategy = .convertFromSnakeCase
    return try! decoder.decode(CoreStatus.self, from: Data(json.utf8))
}

private func statusJSON(proxyError: String? = nil, chatGPTFound: Bool = true, relayOnly: Bool = false) -> String {
    let entries = (1...3).map {
        #"{"id":"\#($0)","name":"配置\#($0)","model_count":6,"fetched_at":"2026-11-16T00:12:00Z","stale":false,"error":null}"#
    }
    let proxy = proxyError.map { #"{"running":false,"error":"\#($0)"}"# } ?? #"{"running":true,"error":null}"#
    let chatgpt = chatGPTFound ? #"{"found":true,"error":null}"# : #"{"found":false,"error":"chatgpt_not_found"}"#
    let mode = relayOnly ? "relay" : "both"
    return """
    {"version":"1.3.0","forwarding":true,"proxy":\(proxy),"chatgpt":\(chatgpt),\
    "default_relay":"配置1","relays":[\(entries.joined(separator: ","))],\
    "taken_over":[],"restore_blocked":[],"config_path":"/Users/example/.codex/config.toml",\
    "update":{"latest_version":null,"url":null},\
    "access":{"mode":"\(mode)","target":"\(mode)","detect":"logged_in","relay_only":\(relayOnly),"logged_in":true}}
    """
}

@MainActor
private func makeModel(
    relayCount: Int, responses: [String: Result<Data, CoreError>] = [:],
    settingsFailing: Set<String> = [], chatGPTRunning: Bool = true
) async -> AppModel {
    let relays = (0..<relayCount).map { RelayConfig(id: "\($0 + 1)", name: "配置\($0 + 1)", baseURL: "https://r\($0 + 1).example.com") }
    let log = CallLog()
    let model = AppModel(
        core: FakeCore(log: log, responses: responses),
        keychain: RelayFakeKeychain(log: log, entries: Dictionary(uniqueKeysWithValues: relays.map { ($0.id, "sk-example-\($0.id)") })),
        settings: RelayFakeSettings(log: log, relays: relays, failing: settingsFailing),
        loginItem: FakeLoginItem(log: log), chatGPT: FakeChatGPT(running: chatGPTRunning)
    )
    await model.start()
    model.noticeAutoDismissDelay = 60
    return model
}

@MainActor
private func open(_ window: NSWindow) async -> RenderedView {
    hideOffscreen(window)
    try? await Task.sleep(nanoseconds: 200_000_000)
    window.contentView?.layoutSubtreeIfNeeded()
    return RenderedView(existing: window)
}

private func ok(_ json: String) -> Result<Data, CoreError> { .success(Data(json.utf8)) }

private let switchTitle = "已切换到「只用中转」"
private let proxyTitle = "本机转发服务没能启动"
private let notFoundTitle = "没有找到 ChatGPT"
private let backendTitle = "后台服务没能启动，请退出后重新打开"

@MainActor
final class MainWindowNoticePositionTests: XCTestCase {
    private struct Rig {
        let model: AppModel
        let window: NSWindow
        let view: RenderedView
    }

    private func makeRig(initial: String = statusJSON(), responses: [String: Result<Data, CoreError>] = [:]) async -> Rig {
        let model = await makeModel(relayCount: 3, responses: responses)
        model.status = decodeStatus(initial)
        let window = AppWindows.makeMain(model: model)
        return Rig(model: model, window: window, view: await open(window))
    }

    private func switchedRig() async -> Rig {
        await makeRig(responses: ["set_relay_only": ok(statusJSON(relayOnly: true))])
    }

    private func errorAndSuccessRig() async -> Rig {
        await makeRig(responses: ["set_relay_only": ok(statusJSON(proxyError: "proxy_port_in_use", relayOnly: true))])
    }

    private func notFoundRig() async -> Rig {
        await makeRig(initial: statusJSON(chatGPTFound: false))
    }

    private func bars(_ rig: Rig, _ titles: [String]) throws -> [Bar] {
        try titles.map { try XCTUnwrap(rig.view.bar(titled: $0), $0) }
    }

    private func controls(_ rig: Rig, _ bars: [Bar]) -> [(label: String, frame: NSRect)] {
        rig.view.controls(roles: [button], labels: mainButtonLabels, excluding: bars)
    }

    func testSwitchSuccess_TopLeftAt376Wide() async throws {
        let rig = await switchedRig()
        defer { rig.window.orderOut(nil) }
        await rig.model.setRelayOnly(true)
        assertStack(try bars(rig, [switchTitle]), in: rig.window, width: 376)
    }

    func testSwitchSuccess_DoesNotCoverControls() async throws {
        let rig = await switchedRig()
        defer { rig.window.orderOut(nil) }
        await rig.model.setRelayOnly(true)
        let found = try bars(rig, [switchTitle])
        let all = controls(rig, found)
        XCTAssertTrue(all.contains { $0.label == "只用中转" })
        XCTAssertTrue(all.contains { $0.label == "使用中转" })
        assertNoOverlap(found, all)
    }

    func testErrorWithRetryAndSuccess_StackedTopLeftWithGap8() async throws {
        let rig = await errorAndSuccessRig()
        defer { rig.window.orderOut(nil) }
        await rig.model.setRelayOnly(true)
        let found = try bars(rig, [switchTitle, proxyTitle])
        XCTAssertTrue(found[1].own.contains { $0.contains("|重试|") })
        assertStack(found, in: rig.window, width: 376)
    }

    func testErrorWithRetryAndSuccess_DoNotCoverControls() async throws {
        let rig = await errorAndSuccessRig()
        defer { rig.window.orderOut(nil) }
        await rig.model.setRelayOnly(true)
        let found = try bars(rig, [switchTitle, proxyTitle])
        let all = controls(rig, found)
        XCTAssertTrue(all.contains { $0.label == "只用中转" })
        XCTAssertTrue(all.contains { $0.label == "重新检测" })
        assertNoOverlap(found, all)
    }

    func testChatGPTNotFound_TopLeftAt376Wide() async throws {
        let rig = await notFoundRig()
        defer { rig.window.orderOut(nil) }
        assertStack(try bars(rig, [notFoundTitle]), in: rig.window, width: 376)
    }

    func testChatGPTNotFound_DoesNotCoverControls() async throws {
        let rig = await notFoundRig()
        defer { rig.window.orderOut(nil) }
        let found = try bars(rig, [notFoundTitle])
        let all = controls(rig, found)
        XCTAssertTrue(all.contains { $0.label == "使用中转" })
        XCTAssertTrue(all.contains { $0.label == "添加配置" })
        assertNoOverlap(found, all)
    }

    func testChatGPTNotFoundAndNotice_StackedWithGap8AndDoNotCoverControls() async throws {
        let rig = await notFoundRig()
        defer { rig.window.orderOut(nil) }
        rig.model.coreDidFail()
        try? await Task.sleep(nanoseconds: 100_000_000)
        let found = try bars(rig, [backendTitle, notFoundTitle])
        assertStack(found, in: rig.window, width: 376)
        assertNoOverlap(found, controls(rig, found))
    }

    func testBannerAppearsAndDisappears_WindowHeightUnchanged() async throws {
        let rig = await notFoundRig()
        defer { rig.window.orderOut(nil) }
        let withBanner = rig.view.size.height
        let banner = try XCTUnwrap(rig.model.banners.first)
        rig.model.dismissBanner(banner)
        try? await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertNil(rig.view.bar(titled: notFoundTitle))
        XCTAssertEqual(rig.view.size.height, withBanner, accuracy: 1)
    }

    func testNoticeAppearsAndDisappears_WindowHeightUnchanged() async throws {
        let rig = await makeRig()
        defer { rig.window.orderOut(nil) }
        let before = rig.view.size.height
        rig.model.coreDidFail()
        try? await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertNotNil(rig.view.bar(titled: backendTitle))
        XCTAssertEqual(rig.view.size.height, before, accuracy: 1)
        let notice = try XCTUnwrap(rig.model.notices.first)
        rig.model.dismissNotice(notice.id)
        try? await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertEqual(rig.view.size.height, before, accuracy: 1)
    }

    func testSuccessNoticeAutoDismisses_ErrorNoticeStays() async throws {
        let rig = await switchedRig()
        defer { rig.window.orderOut(nil) }
        rig.model.noticeAutoDismissDelay = 0.2
        await rig.model.setRelayOnly(true)
        rig.model.coreDidFail()
        try? await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertNotNil(rig.view.bar(titled: switchTitle))
        try? await Task.sleep(nanoseconds: 500_000_000)
        XCTAssertNil(rig.view.bar(titled: switchTitle))
        XCTAssertNotNil(rig.view.bar(titled: backendTitle))
    }
}

@MainActor
final class SetupWindowNoticePositionTests: XCTestCase {
    private struct Rig {
        let model: AppModel
        let window: NSWindow
        let view: RenderedView
    }

    private func makeRig(save: Result<Data, CoreError>, settingsFailing: Set<String> = []) async -> Rig {
        let model = await makeModel(relayCount: 0, responses: ["save_relay": save], settingsFailing: settingsFailing, chatGPTRunning: false)
        let window = AppWindows.makeSetup(model: model)
        let view = await open(window)
        model.draftName = "中转"
        model.draftBaseURL = "https://relay.example.com/v1"
        model.draftAPIKey = "sk-example-0000"
        return Rig(model: model, window: window, view: view)
    }

    private func connectFailedRig() async -> Rig {
        await makeRig(save: .failure(.remote(code: "relay_401", keys: [])))
    }

    private func writeFailedRig() async -> Rig {
        await makeRig(
            save: ok(#"{"id":"1","name":"中转","model_count":3,"models_changed":true}"#),
            settingsFailing: ["settings.saveRelays"]
        )
    }

    private func settle() async { try? await Task.sleep(nanoseconds: 150_000_000) }

    private func controls(_ rig: Rig, _ bars: [Bar]) -> [(label: String, frame: NSRect)] {
        let fields = rig.view.controls(roles: [textField], excluding: bars)
        let buttons = rig.view.controls(roles: [button], labels: ["显示 API Key", "隐藏 API Key", "完成设置"], excluding: bars)
        return fields + buttons
    }

    private func assertAllControlsPresent(_ all: [(label: String, frame: NSRect)]) {
        let labels = Set(all.map(\.label))
        XCTAssertTrue(labels.isSuperset(of: ["名字", "Base URL", "API Key", "显示 API Key", "完成设置"]), "\(labels)")
    }

    func testConnectFailed_TopLeftAt608Wide() async throws {
        let rig = await connectFailedRig()
        defer { rig.window.orderOut(nil) }
        await rig.model.completeSetup()
        await settle()
        assertStack([try XCTUnwrap(rig.view.bar(titled: SetupCopy.failedTitle))], in: rig.window, width: 608)
    }

    func testConnectFailed_DoesNotCoverInputsOrButtons() async throws {
        let rig = await connectFailedRig()
        defer { rig.window.orderOut(nil) }
        await rig.model.completeSetup()
        await settle()
        let found = [try XCTUnwrap(rig.view.bar(titled: SetupCopy.failedTitle))]
        let all = controls(rig, found)
        assertAllControlsPresent(all)
        assertNoOverlap(found, all)
    }

    func testSettingsWriteFailed_TopLeftAt608Wide() async throws {
        let rig = await writeFailedRig()
        defer { rig.window.orderOut(nil) }
        await rig.model.completeSetup()
        await settle()
        assertStack([try XCTUnwrap(rig.view.bar(titled: "没有保存"))], in: rig.window, width: 608)
    }

    func testSettingsWriteFailed_DoesNotCoverInputsOrButtons() async throws {
        let rig = await writeFailedRig()
        defer { rig.window.orderOut(nil) }
        await rig.model.completeSetup()
        await settle()
        let found = [try XCTUnwrap(rig.view.bar(titled: "没有保存"))]
        let all = controls(rig, found)
        assertAllControlsPresent(all)
        assertNoOverlap(found, all)
    }

    func testFailureAndNotice_StackedWithGap8() async throws {
        let rig = await connectFailedRig()
        defer { rig.window.orderOut(nil) }
        await rig.model.completeSetup()
        rig.model.coreDidFail()
        await settle()
        let found = [try XCTUnwrap(rig.view.bar(titled: backendTitle)), try XCTUnwrap(rig.view.bar(titled: SetupCopy.failedTitle))]
        assertStack(found, in: rig.window, width: 608)
    }

    func testFailureAppearsAndDismisses_WindowHeightUnchanged() async throws {
        let rig = await writeFailedRig()
        defer { rig.window.orderOut(nil) }
        let before = rig.view.size.height
        await rig.model.completeSetup()
        await settle()
        XCTAssertNotNil(rig.view.bar(titled: "没有保存"))
        XCTAssertEqual(rig.view.size.height, before, accuracy: 1)
        rig.model.dismissSetupFailure()
        await settle()
        XCTAssertEqual(rig.view.size.height, before, accuracy: 1)
    }
}

@MainActor
final class RelayFormNoticePositionTests: XCTestCase {
    private struct Rig {
        let model: AppModel
        let view: RenderedView
        var window: NSWindow { view.host.window! }
    }

    private func makeRig(save: Result<Data, CoreError>, settingsFailing: Set<String> = []) async -> Rig {
        let model = await makeModel(relayCount: 3, responses: ["save_relay": save], settingsFailing: settingsFailing, chatGPTRunning: false)
        model.beginEditRelay(id: "2")
        let view = RenderedView(RelayFormView(model: model))
        view.makeKeyOffscreen()
        return Rig(model: model, view: view)
    }

    private func status401Rig() async -> Rig {
        await makeRig(save: .failure(.remote(code: "relay_401", keys: [])))
    }

    private func writeFailedRig() async -> Rig {
        await makeRig(
            save: ok(#"{"id":"2","name":"配置2","model_count":3,"models_changed":true}"#),
            settingsFailing: ["settings.saveRelays"]
        )
    }

    private func headingBottom(_ rig: Rig) throws -> CGFloat {
        try XCTUnwrap(rig.view.frame(ofExactText: "编辑配置")).midY - 12
    }

    private func belowHeading(_ rig: Rig, _ text: String) throws -> CGFloat {
        try XCTUnwrap(rig.view.frame(ofExactText: text)).maxY - headingBottom(rig)
    }

    private func fail(_ rig: Rig) async {
        await rig.model.saveRelay()
        _ = rig.view.size
        rig.view.makeKeyOffscreen()
    }

    private func assertInsertedAboveFirstField(_ rig: Rig) async throws {
        let beforeHeight = rig.view.size.height
        let labelBefore = try belowHeading(rig, "名字"), buttonsBefore = try belowHeading(rig, "取消")
        await fail(rig)
        let bar = try XCTUnwrap(rig.view.bar(titled: "没有保存"))
        let window = rig.window
        XCTAssertEqual(bar.frame.maxY - (try headingBottom(rig)), -12, accuracy: 1.5, "标题下面 12")
        XCTAssertEqual(bar.frame.minX - window.frame.minX, 24, accuracy: 1.5)
        XCTAssertEqual(window.frame.maxX - bar.frame.maxX, 24, accuracy: 1.5)
        let shift = bar.frame.height + 20
        XCTAssertEqual(rig.view.size.height - beforeHeight, shift, accuracy: 1.5, "弹窗变高")
        XCTAssertEqual(labelBefore - (try belowHeading(rig, "名字")), shift, accuracy: 1.5, "第一个字段上面 20")
        XCTAssertEqual(buttonsBefore - (try belowHeading(rig, "取消")), shift, accuracy: 1.5, "按钮整体下移")
    }

    func testEdit401_ErrorInsertedAboveFirstField() async throws {
        try await assertInsertedAboveFirstField(await status401Rig())
    }

    func testEditSettingsWriteFailed_ErrorInsertedAboveFirstField() async throws {
        try await assertInsertedAboveFirstField(await writeFailedRig())
    }

    private func assertDoesNotCoverControls(_ rig: Rig) async throws {
        await fail(rig)
        let bar = try XCTUnwrap(rig.view.bar(titled: "没有保存"))
        let all = rig.view.controls(roles: [button, textField], excluding: [bar])
        XCTAssertTrue(all.contains { $0.label == "取消" })
        XCTAssertTrue(all.contains { $0.label == "名字" })
        for control in all {
            XCTAssertTrue(NSIntersectionRect(bar.frame, control.frame).isEmpty, "提示条盖住了「\(control.label)」")
        }
        let heading = try XCTUnwrap(rig.view.frame(ofExactText: "编辑配置"))
        XCTAssertTrue(NSIntersectionRect(bar.frame, heading).isEmpty, "提示条盖住了标题")
    }

    func testEdit401_DoesNotCoverControls() async throws {
        try await assertDoesNotCoverControls(await status401Rig())
    }

    func testEditSettingsWriteFailed_DoesNotCoverControls() async throws {
        try await assertDoesNotCoverControls(await writeFailedRig())
    }

    func testEdit401_ErrorHasNoShadow() async throws {
        let rig = await status401Rig()
        await fail(rig)
        let bar = try XCTUnwrap(rig.view.bar(titled: "没有保存"))
        let window = rig.window
        let snapshot = try XCTUnwrap(rig.view.snapshot())
        let x = bar.frame.midX - window.frame.minX
        let below = window.frame.maxY - bar.frame.minY + 6
        let above = window.frame.maxY - bar.frame.maxY - 6
        for y in [below, above] {
            let color = try XCTUnwrap(snapshot.rep.colorAt(x: Int(x * snapshot.scale), y: Int(y * snapshot.scale))?.usingColorSpace(.sRGB))
            XCTAssertGreaterThan(color.redComponent, 0.99, "y=\(y)")
            XCTAssertGreaterThan(color.greenComponent, 0.99, "y=\(y)")
            XCTAssertGreaterThan(color.blueComponent, 0.99, "y=\(y)")
        }
    }

    func testEdit401_CloseButtonRestoresHeightAndLayout() async throws {
        let rig = await status401Rig()
        let beforeHeight = rig.view.size.height
        let labelBefore = try belowHeading(rig, "名字")
        await fail(rig)
        XCTAssertGreaterThan(rig.view.size.height, beforeHeight + 1)
        XCTAssertTrue(rig.view.press(label: "关闭提示"))
        try? await Task.sleep(nanoseconds: 100_000_000)
        rig.view.makeKeyOffscreen()
        XCTAssertNil(rig.view.bar(titled: "没有保存"))
        XCTAssertEqual(rig.view.size.height, beforeHeight, accuracy: 1)
        XCTAssertEqual(try belowHeading(rig, "名字"), labelBefore, accuracy: 1)
        XCTAssertTrue(rig.model.showRelayForm)
    }

    func testEdit401_RealPanelGrowsDownwardAndShrinksBackOnClose() async throws {
        let model = await makeModel(relayCount: 3, responses: ["save_relay": .failure(.remote(code: "relay_401", keys: []))], chatGPTRunning: false)
        model.beginEditRelay(id: "2")
        let panel = DialogPanel.make(RelayFormView(model: model))
        hideOffscreen(panel)
        defer { panel.orderOut(nil) }
        let view = RenderedView(existing: panel)
        try? await Task.sleep(nanoseconds: 200_000_000)
        let before = panel.frame
        await model.saveRelay()
        try? await Task.sleep(nanoseconds: 300_000_000)
        let bar = try XCTUnwrap(view.bar(titled: "没有保存"))
        XCTAssertEqual(panel.frame.height - before.height, bar.frame.height + 20, accuracy: 2, "弹窗变高")
        XCTAssertEqual(panel.frame.maxY, before.maxY, accuracy: 1, "顶边不动")
        XCTAssertEqual(panel.frame.minX, before.minX, accuracy: 1)
        XCTAssertGreaterThanOrEqual(bar.frame.minY, panel.frame.minY)
        XCTAssertLessThanOrEqual(bar.frame.maxY, panel.frame.maxY)
        for control in view.controls(roles: [button, textField], excluding: [bar]) {
            XCTAssertTrue(NSIntersectionRect(bar.frame, control.frame).isEmpty, "提示条盖住了「\(control.label)」")
        }
        XCTAssertTrue(view.press(label: "关闭提示"))
        try? await Task.sleep(nanoseconds: 300_000_000)
        XCTAssertNil(view.bar(titled: "没有保存"))
        XCTAssertEqual(panel.frame.height, before.height, accuracy: 1)
        XCTAssertEqual(panel.frame.maxY, before.maxY, accuracy: 1)
    }
}
