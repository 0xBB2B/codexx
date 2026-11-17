import XCTest
@testable import ModelBridge

@MainActor
final class MainWindowLayoutTests: XCTestCase {
    func testWidthIs620() async {
        let f = await ViewFixture.make()
        XCTAssertEqual(RenderedView(MainWindow(model: f.model)).size.width, 620, accuracy: 1)
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

    func testCoreFailure_FloatsRedAlertWithoutChangingHeight() async {
        let f = await ViewFixture.make(responses: ["save_relay": .failure(.remote(code: "relay_401", keys: []))])
        f.model.beginAddRelay()
        f.model.draftName = "新"
        f.model.draftBaseURL = "https://new.example.com"
        f.model.draftAPIKey = "sk-draft-0000"
        let view = RenderedView(RelayFormView(model: f.model))
        let before = view.size.height
        await f.model.saveRelay()
        XCTAssertTrue(view.has("没有保存"))
        XCTAssertTrue(view.has("中转服务返回 401"))
        XCTAssertEqual(view.size.height, before, accuracy: 1)
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
