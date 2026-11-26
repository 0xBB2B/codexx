import SwiftUI
import XCTest
@testable import ModelBridge

@MainActor
final class MainWindowLayoutTests: XCTestCase {
    func testWidthIs760() async {
        let f = await ViewFixture.make()
        XCTAssertEqual(RenderedView(MainWindow(model: f.model)).size.width, 760, accuracy: 1)
    }

    func testSetupWindowWidthIs760() async {
        let f = await ViewFixture.make()
        f.model.beginSetup()
        XCTAssertEqual(RenderedView(SetupWindow(model: f.model)).size.width, 760, accuracy: 1)
    }

    func testNoRunStatusOrConnectionCard() async {
        let f = await ViewFixture.make()
        let view = RenderedView(MainWindow(model: f.model))
        XCTAssertFalse(view.has("运行状态"))
        XCTAssertFalse(view.has("中转连接"))
        XCTAssertTrue(view.has("中转配置"))
    }

    func testHeight_ThreeRelaysForwardingOn() async {
        let f = await ViewFixture.make()
        XCTAssertEqual(RenderedView(MainWindow(model: f.model)).size.height, 649, accuracy: 12)
    }

    func testHeight_ForwardingOffIsAbout56Shorter() async {
        let on = await ViewFixture.make()
        let off = await ViewFixture.make(forwarding: false)
        let onHeight = RenderedView(MainWindow(model: on.model)).size.height
        let offHeight = RenderedView(MainWindow(model: off.model)).size.height
        XCTAssertEqual(offHeight, 593, accuracy: 12)
        XCTAssertEqual(onHeight - offHeight, 56, accuracy: 8)
    }

    func testRelayList_StopsGrowingAfterThreeRows() async {
        let three = await ViewFixture.make(relayCount: 3)
        let six = await ViewFixture.make(relayCount: 6)
        let threeHeight = RenderedView(MainWindow(model: three.model)).size.height
        let sixHeight = RenderedView(MainWindow(model: six.model)).size.height
        XCTAssertEqual(sixHeight, threeHeight, accuracy: 1)
    }

    func testRelayList_FewerRowsAreShorter() async {
        let one = await ViewFixture.make(relayCount: 1)
        let three = await ViewFixture.make(relayCount: 3)
        let diff = RenderedView(MainWindow(model: three.model)).size.height - RenderedView(MainWindow(model: one.model)).size.height
        XCTAssertEqual(diff, 128, accuracy: 8)
    }

    func testNotice_FloatsWithoutChangingHeight() async {
        let f = await ViewFixture.make()
        let view = RenderedView(MainWindow(model: f.model))
        let before = view.size.height
        f.model.coreDidFail()
        XCTAssertTrue(view.has("后台服务没能启动，请退出后重新打开"))
        XCTAssertEqual(view.size.height, before, accuracy: 1)
    }
}

@MainActor
final class ForwardingOverviewTests: XCTestCase {
    func testOn_TitleAndAccessRow() async {
        let f = await ViewFixture.make(mode: "relay")
        let view = RenderedView(MainWindow(model: f.model))
        XCTAssertTrue(view.has("使用中转 · 已开启"))
        XCTAssertTrue(view.has("接入方式"))
        XCTAssertTrue(view.has("只用中转"))
        XCTAssertTrue(view.has("已检测到 ChatGPT 登录"))
        XCTAssertTrue(view.hasExactly("重新检测"))
    }

    func testOn_BothModeName() async {
        let f = await ViewFixture.make(mode: "both")
        XCTAssertTrue(RenderedView(MainWindow(model: f.model)).has("官方 + 中转"))
    }

    func testOff_TitleAndNoAccessRow() async {
        let f = await ViewFixture.make(forwarding: false)
        let view = RenderedView(MainWindow(model: f.model))
        XCTAssertTrue(view.has("使用中转 · 已关闭"))
        XCTAssertFalse(view.has("接入方式"))
        XCTAssertFalse(view.hasExactly("重新检测"))
    }

    func testDetecting_ShowsProgressTextAndNoRecheck() async {
        let f = await ViewFixture.make()
        f.model.isDetectingAccess = true
        let view = RenderedView(MainWindow(model: f.model))
        XCTAssertTrue(view.has("正在检测…"))
        XCTAssertFalse(view.hasExactly("重新检测"))
    }
}

@MainActor
final class RelayConfigCardTests: XCTestCase {
    func testRows_ShowNamesAndEditDeleteButtons() async {
        let f = await ViewFixture.make()
        let view = RenderedView(MainWindow(model: f.model))
        ["配置1", "配置2", "配置3"].forEach { XCTAssertTrue(view.has($0), $0) }
        XCTAssertEqual(view.texts.filter { $0 == "编辑" }.count, 3)
        XCTAssertEqual(view.texts.filter { $0 == "删除" }.count, 3)
        XCTAssertTrue(view.has("https://r1.example.com"))
    }

    func testRetry_OnlyOnFailedRow() async {
        let ok = await ViewFixture.make()
        XCTAssertFalse(RenderedView(MainWindow(model: ok.model)).hasExactly("重试"))
        let failed = await ViewFixture.make(failingIDs: ["2"])
        let view = RenderedView(MainWindow(model: failed.model))
        XCTAssertEqual(view.texts.filter { $0 == "重试" }.count, 1)
    }

    func testAddButtonAndCaption() async {
        let f = await ViewFixture.make()
        let view = RenderedView(MainWindow(model: f.model))
        XCTAssertTrue(view.hasExactly("添加配置"))
        XCTAssertTrue(view.has("已有 3 个，最多 10 个。"))
    }

    func testCaption_SingleRelayMustBeKept() async {
        let f = await ViewFixture.make(relayCount: 1)
        XCTAssertTrue(RenderedView(MainWindow(model: f.model)).has("至少保留一个配置。"))
    }

    func testCaption_AtLimit() async {
        let f = await ViewFixture.make(relayCount: 10)
        XCTAssertTrue(RenderedView(MainWindow(model: f.model)).has("已有 10 个，达到上限。"))
    }
}

@MainActor
final class MainStatusBarTests: XCTestCase {
    func testStatusBarItems() async {
        let f = await ViewFixture.make()
        let view = RenderedView(MainWindow(model: f.model))
        XCTAssertTrue(view.has("v1.3.0"))
        XCTAssertTrue(view.has("后台运行中"))
        XCTAssertTrue(view.has("ChatGPT 已检测到"))
        XCTAssertTrue(view.has("中转模型 18 个（3 个配置）"))
        XCTAssertTrue(view.hasExactly("检查更新"))
    }
}

@MainActor
final class RelayFormViewTests: XCTestCase {
    func testAddMode_WidthHeaderAndHelpTexts() async {
        let f = await ViewFixture.make()
        f.model.beginAddRelay()
        let view = RenderedView(RelayFormView(model: f.model))
        XCTAssertEqual(view.size.width, 520, accuracy: 1)
        XCTAssertTrue(view.hasExactly("添加配置"))
        XCTAssertEqual(view.texts.filter { $0 == "必填" }.count, 3)
        XCTAssertTrue(view.has("1~16 个字，不能重名，不能含「·」。"))
        XCTAssertTrue(view.has("以 https:// 开头的完整地址。"))
        XCTAssertTrue(view.has("保存在系统钥匙串中。"))
        XCTAssertTrue(view.hasExactly("取消"))
        XCTAssertTrue(view.hasExactly("保存"))
    }

    func testOpening_FocusesNameField() async {
        let f = await ViewFixture.make()
        f.model.beginAddRelay()
        let view = RenderedView(RelayFormView(model: f.model))
        view.makeKeyOffscreen()
        defer { view.close() }
        try? await Task.sleep(nanoseconds: 300_000_000)
        XCTAssertEqual(view.firstTextFieldIsFocused(), true)
    }

    func testEditMode_Title() async {
        let f = await ViewFixture.make()
        f.model.beginEditRelay(id: "2")
        let view = RenderedView(RelayFormView(model: f.model))
        XCTAssertTrue(view.hasExactly("编辑配置"))
        XCTAssertFalse(view.hasExactly("添加配置"))
    }

    func testFieldError_ReplacesHelpAndKeepsHeight() async {
        let f = await ViewFixture.make()
        f.model.beginAddRelay()
        let view = RenderedView(RelayFormView(model: f.model))
        let before = view.size.height
        f.model.relayFieldErrors = [.name: "请填写名字。", .baseURL: "请填写 Base URL。", .apiKey: "请填写 API Key。"]
        XCTAssertTrue(view.has("请填写名字。"))
        XCTAssertFalse(view.has("1~16 个字，不能重名，不能含「·」。"))
        XCTAssertEqual(view.size.height, before, accuracy: 1)
    }

    private static let longError = "已有同名配置「一二三四五六七八九十一二三四五六」。"

    func testLongFieldError_KeepsHeight() async {
        for field in [RelayField.name, .baseURL, .apiKey] {
            let f = await ViewFixture.make()
            f.model.beginAddRelay()
            let view = RenderedView(RelayFormView(model: f.model))
            let before = view.size.height
            f.model.relayFieldErrors = [field: Self.longError]
            XCTAssertEqual(view.size.height, before, accuracy: 1, "\(field)")
        }
    }

    func testDuplicateErrorFor16CharName_NotTruncated() async throws {
        let longName = "一二三四五六七八九十一二三四五六"
        let f = await ViewFixture.make(firstName: longName)
        f.model.beginAddRelay()
        f.model.draftName = longName
        f.model.draftBaseURL = "https://new.example.com"
        f.model.draftAPIKey = "sk-draft-0000"
        let view = RenderedView(RelayFormView(model: f.model))
        await f.model.saveRelay()
        let message = "已有同名配置「\(longName)」。"
        let frame = try XCTUnwrap(view.frame(ofExactText: message))
        let natural = (message as NSString).size(withAttributes: [.font: NSFont.systemFont(ofSize: 12)]).width
        XCTAssertGreaterThanOrEqual(frame.width, natural - 1)
    }

    func testSaving_ButtonTextAndCancelDisabled() async {
        let gate = Gate()
        let f = await ViewFixture.make(responses: ["save_relay": .success(Data(#"{"id":"9","name":"新","model_count":1,"models_changed":true}"#.utf8))], gates: ["save_relay": gate])
        f.model.beginAddRelay()
        f.model.draftName = "新"
        f.model.draftBaseURL = "https://new.example.com"
        f.model.draftAPIKey = "sk-draft-0000"
        let view = RenderedView(RelayFormView(model: f.model))
        let before = view.size.height
        let task = Task { await f.model.saveRelay() }
        await awaitUntil { f.model.isSavingRelay }
        XCTAssertTrue(view.has("正在保存并获取模型…"))
        XCTAssertEqual(view.isEnabled(label: "取消"), false)
        XCTAssertEqual(view.size.height, before, accuracy: 1)
        await gate.open()
        await task.value
    }

    func testCoreFailure_InsertsRedAlertAboveFieldsAndGrowsHeight() async throws {
        let f = await ViewFixture.make(responses: ["save_relay": .failure(.remote(code: "relay_401", keys: []))])
        f.model.beginAddRelay()
        f.model.draftName = "新"
        f.model.draftBaseURL = "https://new.example.com"
        f.model.draftAPIKey = "sk-draft-0000"
        let view = RenderedView(RelayFormView(model: f.model))
        let before = view.size.height
        await f.model.saveRelay()
        view.makeKeyOffscreen()
        defer { view.close() }
        XCTAssertTrue(view.has("中转服务返回 401"))
        XCTAssertGreaterThan(view.size.height, before + 1)
        let alert = try XCTUnwrap(view.frame(ofExactText: "没有保存"))
        let heading = try XCTUnwrap(view.frame(ofExactText: "添加配置"))
        let name = try XCTUnwrap(view.frame(ofExactText: "名字"))
        XCTAssertLessThanOrEqual(alert.maxY, heading.minY)
        XCTAssertGreaterThanOrEqual(alert.minY, name.maxY)
        XCTAssertTrue(f.model.showRelayForm)
    }
}

@MainActor
final class DeleteRelayDialogViewTests: XCTestCase {
    func testConfirmState_Width520AndButtons() async {
        let f = await ViewFixture.make()
        f.model.requestDeleteRelay(id: "2")
        let view = RenderedView(DeleteRelayDialogView(model: f.model))
        XCTAssertEqual(view.size.width, 520, accuracy: 1)
        XCTAssertTrue(view.has("删除配置「配置2」？"))
        XCTAssertTrue(view.hasExactly("取消"))
        XCTAssertTrue(view.hasExactly("删除"))
        XCTAssertFalse(view.has("正在删除…"))
    }

    func testDeleting_ShowsBusyLabelAndDisablesCancelWhileDialogStaysOpen() async {
        let gate = Gate()
        let f = await ViewFixture.make(
            responses: ["delete_relay": .success(Data(#"{"version":"1.3.0","forwarding":true,"relays":[]}"#.utf8))],
            gates: ["delete_relay": gate]
        )
        f.model.requestDeleteRelay(id: "2")
        let view = RenderedView(DeleteRelayDialogView(model: f.model))
        let task = Task { await f.model.confirmPending() }
        await awaitUntil { f.model.isDeletingRelay }
        XCTAssertNotNil(f.model.pendingConfirmation)
        XCTAssertTrue(view.has("正在删除…"))
        XCTAssertEqual(view.isEnabled(label: "取消"), false)
        await gate.open()
        await task.value
    }
}

@MainActor
final class SetupWindowNameFieldTests: XCTestCase {
    func testLongFieldError_KeepsHeight() async {
        for field in [RelayField.name, .baseURL, .apiKey] {
            let f = await ViewFixture.make(relayCount: 0)
            f.model.beginSetup()
            let view = RenderedView(SetupWindow(model: f.model))
            let before = view.size.height
            f.model.relayFieldErrors = [field: "已有同名配置「一二三四五六七八九十一二三四五六」。"]
            XCTAssertEqual(view.size.height, before, accuracy: 1, "\(field)")
        }
    }

    func testTitleAndNameFieldFirstWithDefault() async {
        let f = await ViewFixture.make(relayCount: 0)
        f.model.beginSetup()
        let view = RenderedView(SetupWindow(model: f.model))
        XCTAssertTrue(view.has("连接中转服务"))
        XCTAssertTrue(view.has("1~16 个字，不能重名，不能含「·」。"))
        XCTAssertTrue(view.texts.contains("中转"))
        let name = view.order(ofExactly: "名字")
        let url = view.order(ofExactly: "Base URL")
        XCTAssertNotNil(name)
        XCTAssertNotNil(url)
        if let name, let url { XCTAssertLessThan(name, url) }
    }
}

@MainActor
final class ChatGPTUnsupportedViewTests: XCTestCase {
    private let overview = "当前 ChatGPT 版本暂不支持，暂时不能打开。"
    private let bannerTitle = "当前 ChatGPT 版本暂不支持"
    private let bannerDetail = "读不到 ChatGPT 自带的官方模型清单，转发保持关闭。请把 ChatGPT 更新到最新版后点「重新检测」；还是不行的话，请等待助手新版本。"

    private func unsupported(
        responses: [String: Result<Data, CoreError>] = [:], gates: [String: Gate] = [:]
    ) async -> ViewFixture {
        let f = await ViewFixture.make(forwarding: false, responses: responses, gates: gates)
        f.model.status = Self.decode(unsupportedJSON())
        return f
    }

    private func unsupportedJSON() -> String {
        ViewFixture.statusJSON(forwarding: false, relayCount: 1)
            .replacingOccurrences(of: #""chatgpt":{"found":true,"error":null}"#, with: #""chatgpt":{"found":true,"error":"chatgpt_unsupported"}"#)
    }

    private static func decode(_ text: String) -> CoreStatus {
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        return try! decoder.decode(CoreStatus.self, from: Data(text.utf8))
    }

    private func forwardingReportedOn(chatgpt: String) async -> ViewFixture {
        let f = await ViewFixture.make(forwarding: true)
        let text = ViewFixture.statusJSON(forwarding: true, relayCount: 1)
            .replacingOccurrences(of: #""chatgpt":{"found":true,"error":null}"#, with: chatgpt)
        f.model.status = Self.decode(text)
        return f
    }

    func testForwardingReportedOn_ChatGPTUnsupported_ShownAsClosedAndDisabled() async {
        let f = await forwardingReportedOn(chatgpt: #""chatgpt":{"found":true,"error":"chatgpt_unsupported"}"#)
        XCTAssertFalse(f.model.forwardingEnabled)
        XCTAssertEqual(f.model.forwardingTitle, "使用中转 · 已关闭")
        XCTAssertTrue(f.model.forwardingToggleDisabled)
        XCTAssertEqual(f.model.forwardingHelpText, overview)
        XCTAssertEqual(f.model.banners.map(\.title), [bannerTitle])
        XCTAssertEqual(f.model.banners.first?.detail, bannerDetail)
        let view = RenderedView(MainWindow(model: f.model))
        XCTAssertTrue(view.hasExactly("使用中转 · 已关闭"))
        XCTAssertTrue(view.hasExactly(overview))
        XCTAssertFalse(view.has("官方模型在上，中转模型在下"))
        XCTAssertTrue(view.has("中转模型 转发关闭时不获取"))
    }

    func testForwardingReportedOn_ChatGPTNotFound_ShownAsClosedAndDisabled() async {
        let f = await forwardingReportedOn(chatgpt: #""chatgpt":{"found":false,"error":"chatgpt_not_found"}"#)
        XCTAssertFalse(f.model.forwardingEnabled)
        XCTAssertEqual(f.model.forwardingTitle, "使用中转 · 已关闭")
        XCTAssertTrue(f.model.forwardingToggleDisabled)
        XCTAssertEqual(f.model.forwardingHelpText, "找到 ChatGPT 后才能打开。")
        XCTAssertEqual(f.model.banners.map(\.title), ["没有找到 ChatGPT"])
        let view = RenderedView(MainWindow(model: f.model))
        XCTAssertTrue(view.hasExactly("使用中转 · 已关闭"))
        XCTAssertTrue(view.hasExactly("找到 ChatGPT 后才能打开。"))
        XCTAssertFalse(view.has("官方模型在上，中转模型在下"))
    }

    func testToggleDisabledAndOverviewText() async {
        let f = await unsupported()
        XCTAssertTrue(f.model.forwardingToggleDisabled)
        XCTAssertFalse(f.model.forwardingEnabled)
        XCTAssertEqual(f.model.forwardingHelpText, overview)
        XCTAssertTrue(RenderedView(MainWindow(model: f.model)).hasExactly(overview))
    }

    func testBanner_TitleDetailAndRecheckAction() async {
        let f = await unsupported()
        let banner = f.model.banners.first { $0.title == bannerTitle }
        XCTAssertEqual(banner?.detail, bannerDetail)
        XCTAssertEqual(banner?.kind, .error)
        XCTAssertEqual(banner?.action, .recheckStatus)
        XCTAssertEqual(f.model.banners.count, 1)
        let view = RenderedView(MainWindow(model: f.model))
        XCTAssertTrue(view.hasExactly(bannerTitle))
        XCTAssertTrue(view.hasExactly("重新检测"))
    }

    func testStatusFooter_ChatGPTUnsupported_EveryItemStaysOnOneLine() async throws {
        let f = await unsupported()
        let view = RenderedView(StatusFooter(model: f.model).frame(width: 760))
        let reference = try XCTUnwrap(view.frame(ofExactText: "后台运行中")).height
        for text in ["版本暂不支持", "转发关闭时不获取"] {
            let frame = try XCTUnwrap(view.frame(ofExactText: text), text)
            XCTAssertEqual(frame.height, reference, accuracy: 2, text)
        }
    }

    func testStatusRow_ChatGPTVersionUnsupported() async {
        let f = await unsupported()
        let view = RenderedView(MainWindow(model: f.model))
        XCTAssertTrue(view.hasExactly("版本暂不支持"))
        XCTAssertFalse(view.has("ChatGPT 已检测到"))
        XCTAssertFalse(view.has("ChatGPT 未找到"))
    }

    func testDismissBanner_KeepsToggleDisabledAndTextsUnchanged() async throws {
        let f = await unsupported()
        let banner = try XCTUnwrap(f.model.banners.first)
        f.model.dismissBanner(banner)
        XCTAssertTrue(f.model.banners.isEmpty)
        XCTAssertTrue(f.model.forwardingToggleDisabled)
        XCTAssertEqual(f.model.forwardingHelpText, overview)
        let view = RenderedView(MainWindow(model: f.model))
        XCTAssertTrue(view.hasExactly(overview))
        XCTAssertTrue(view.hasExactly("版本暂不支持"))
    }

    func testRecheck_CallsStatusAndButtonShowsBusyDisabled() async {
        let gate = Gate()
        let f = await unsupported(
            responses: ["status": .success(Data(unsupportedJSON().utf8))], gates: ["status": gate]
        )
        let view = RenderedView(MainWindow(model: f.model))
        XCTAssertEqual(view.isEnabled(label: "重新检测"), true)
        let task = Task { await f.model.recheckStatus() }
        await awaitUntil { !f.core.calls("status").isEmpty }
        XCTAssertEqual(f.core.calls("status").count, 1)
        XCTAssertTrue(view.has("正在检测…"))
        XCTAssertEqual(view.isEnabled(label: "正在检测…"), false)
        await gate.open()
        await task.value
        XCTAssertTrue(view.hasExactly("重新检测"))
    }
}

@MainActor
final class StatusFooterUpdateTests: XCTestCase {
    private static func decode(_ text: String) -> CoreStatus {
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        return try! decoder.decode(CoreStatus.self, from: Data(text.utf8))
    }

    private func footerHeight(_ model: AppModel) -> CGFloat {
        RenderedView(StatusFooter(model: model).frame(width: 760)).size.height
    }

    private func longestFixture() async -> ViewFixture {
        let f = await ViewFixture.make(forwarding: false)
        let json = ViewFixture.statusJSON(forwarding: false, relayCount: 1)
            .replacingOccurrences(of: #""version":"1.3.0""#, with: #""version":"0.10.0-local""#)
            .replacingOccurrences(of: #""chatgpt":{"found":true,"error":null}"#, with: #""chatgpt":{"found":true,"error":"chatgpt_unsupported"}"#)
        f.model.status = Self.decode(json)
        f.model.coreRunning = false
        f.model.availableUpdate = UpdateInfo(version: "0.11.0", url: "https://example.com/dl")
        return f
    }

    func testFooter_NewVersion_SingleLineSameHeightAsNormal() async {
        let normal = await ViewFixture.make()
        let updated = await ViewFixture.make()
        updated.model.availableUpdate = UpdateInfo(version: "0.2.0", url: "https://example.com/dl")
        XCTAssertEqual(footerHeight(updated.model), footerHeight(normal.model), accuracy: 1)
        XCTAssertEqual(footerHeight(normal.model), 48, accuracy: 2)
    }

    func testFooter_LongestTexts_SingleLineSameHeightAsNormal() async {
        let normal = await ViewFixture.make()
        let longest = await longestFixture()
        XCTAssertEqual(footerHeight(longest.model), footerHeight(normal.model), accuracy: 1)
    }

    func testFooter_LongestTexts_EveryItemStaysOnOneLine() async throws {
        let f = await longestFixture()
        let view = RenderedView(StatusFooter(model: f.model).frame(width: 760))
        let reference = try XCTUnwrap(view.frame(ofExactText: "后台服务已停止")).height
        for text in ["v0.10.0-local", "版本暂不支持", "转发关闭时不获取"] {
            let frame = try XCTUnwrap(view.frame(ofText: text), text)
            XCTAssertEqual(frame.height, reference, accuracy: 2, text)
        }
        // 按钮最小高 44，量不出折行；按钮宽度等于单行文字宽加左右 4px 内边距，折行或截断时会变窄
        let button = try XCTUnwrap(view.frame(ofExactText: "发现新版本 0.11.0，打开下载页"))
        let singleLine = RenderedView(Text("新版本 0.11.0").font(Fonts.label).fixedSize()).size.width
        XCTAssertEqual(button.width, singleLine + 8, accuracy: 1)
        XCTAssertLessThanOrEqual(button.maxX, 760 - 24 + 1)
    }

    func testFooter_NewVersion_ShowsShortButtonWithFullAccessibilityLabel() async {
        let f = await ViewFixture.make()
        f.model.availableUpdate = UpdateInfo(version: "0.2.0", url: "https://example.com/dl")
        let view = RenderedView(StatusFooter(model: f.model).frame(width: 760))
        XCTAssertTrue(view.hasExactly("发现新版本 0.2.0，打开下载页"))
        XCTAssertNotNil(view.elements.first {
            $0.axString("accessibilityLabel") == "发现新版本 0.2.0，打开下载页"
                && $0.axString("accessibilityRole") == NSAccessibility.Role.button.rawValue
        })
        XCTAssertTrue(view.has("新版本 0.2.0"))
        XCTAssertFalse(view.hasExactly("发现新版本 0.2.0"))
        XCTAssertFalse(view.hasExactly("打开下载页"))
    }

    private func footerAfterCheckUpdate(url: String, installer: String) async -> RenderedView {
        let f = await ViewFixture.make(responses: [
            "check_update": .success(Data(#"{"latest_version":"0.1.2","url":"\#(url)",\#(installer)"is_newer":true}"#.utf8)),
        ])
        await f.model.checkUpdate()
        return RenderedView(StatusFooter(model: f.model).frame(width: 760))
    }

    func testFooter_NewVersionButtonPress_RechecksAndOpensFreshURL() async {
        let freshURL = "https://github.com/o/r/releases/tag/v0.1.2"
        let f = await ViewFixture.make(responses: [
            "check_update": .success(Data(#"{"latest_version":"0.1.2","url":"\#(freshURL)","is_newer":true}"#.utf8)),
        ])
        f.model.availableUpdate = UpdateInfo(version: "0.1.0", url: "https://github.com/o/r/releases/tag/v0.1.0")
        var opened: [URL] = []
        f.model.openURL = { opened.append($0) }
        let view = RenderedView(StatusFooter(model: f.model).frame(width: 760))
        XCTAssertTrue(view.press(label: "发现新版本 0.1.0，打开下载页"))
        await awaitUntil { !opened.isEmpty }
        XCTAssertEqual(f.core.calls("check_update").first?.params, #"{"manual":true}"#)
        XCTAssertEqual(opened, [URL(string: freshURL)!])
    }

    func testFooter_InstallerTrue_LabelSaysDownloadInstaller() async {
        let view = await footerAfterCheckUpdate(
            url: "https://github.com/o/r/releases/download/v0.1.2/model-bridge-0.1.2-macos.dmg", installer: #""installer":true,"#
        )
        XCTAssertTrue(view.hasExactly("发现新版本 0.1.2，下载安装包"))
        XCTAssertTrue(view.has("新版本 0.1.2"))
        XCTAssertFalse(view.hasExactly("发现新版本 0.1.2，打开下载页"))
    }

    func testFooter_InstallerFalse_LabelSaysOpenDownloadPage() async {
        let view = await footerAfterCheckUpdate(url: "https://github.com/o/r/releases/tag/v0.1.2", installer: #""installer":false,"#)
        XCTAssertTrue(view.hasExactly("发现新版本 0.1.2，打开下载页"))
        XCTAssertTrue(view.has("新版本 0.1.2"))
    }

    func testFooter_InstallerMissing_LabelSaysOpenDownloadPage() async {
        let view = await footerAfterCheckUpdate(url: "https://github.com/o/r/releases/tag/v0.1.2", installer: "")
        XCTAssertTrue(view.hasExactly("发现新版本 0.1.2，打开下载页"))
    }

    func testFooter_StatusPushInstallerTrue_LabelSaysDownloadInstaller() async {
        let f = await ViewFixture.make()
        let json = ViewFixture.statusJSON(forwarding: true, relayCount: 1)
            .replacingOccurrences(
                of: #""update":{"latest_version":null,"url":null}"#,
                with: #""update":{"latest_version":"0.1.2","url":"https://github.com/o/r/releases/download/v0.1.2/model-bridge-0.1.2-macos.dmg","installer":true}"#
            )
        f.core.statusContinuation.yield(Self.decode(json))
        await awaitUntil { f.model.availableUpdate != nil }
        let view = RenderedView(StatusFooter(model: f.model).frame(width: 760))
        XCTAssertTrue(view.hasExactly("发现新版本 0.1.2，下载安装包"))
        XCTAssertTrue(view.has("新版本 0.1.2"))
    }

    func testFooter_NoUpdate_RightmostIsCheckUpdate() async throws {
        let f = await ViewFixture.make()
        let view = RenderedView(StatusFooter(model: f.model).frame(width: 760))
        let check = try XCTUnwrap(view.frame(ofExactText: "检查更新"))
        for text in ["v1.3.0", "后台运行中", "已检测到", "转发关闭时不获取"] {
            if let other = view.frame(ofText: text) { XCTAssertLessThan(other.maxX, check.minX + 0.5, text) }
        }
        XCTAssertFalse(view.has("新版本"))
    }
}
