import XCTest
@testable import ModelBridge

@MainActor
private func makeAppModel(chatGPTFound: Bool = true) -> AppModel {
    let log = CallLog()
    let model = AppModel(
        core: FakeCore(log: log),
        keychain: FakeKeychain(log: log),
        settings: FakeSettings(log: log),
        loginItem: FakeLoginItem(log: log),
        chatGPT: FakeChatGPT(log: log, running: false)
    )
    model.status = CoreStatus(
        version: "1.2.0",
        forwarding: false,
        proxy: .init(running: true, error: nil),
        chatgpt: .init(found: chatGPTFound, error: nil),
        relay: nil,
        takenOver: [],
        restoreBlocked: [],
        configPath: nil,
        update: nil
    )
    return model
}

@MainActor
private func item(_ id: MenuPanelItem.ID, in panel: MenuPanelModel) -> MenuPanelItem? {
    panel.groups.flatMap { $0 }.first { $0.id == id }
}

@MainActor
final class MenuPanelModelTests: XCTestCase {
    func testTitle_IsAppName() {
        XCTAssertEqual(MenuPanelModel(model: makeAppModel()).title, "模型接入助手")
    }

    func testSubtitle_ForwardingOn() {
        let model = makeAppModel()
        model.status?.forwarding = true
        XCTAssertEqual(MenuPanelModel(model: model).subtitle, "转发已开启 · 后台运行中")
    }

    func testSubtitle_ForwardingOff() {
        XCTAssertEqual(MenuPanelModel(model: makeAppModel()).subtitle, "转发已关闭 · 后台运行中")
    }

    func testSubtitle_CoreStopped() {
        let model = makeAppModel()
        model.coreRunning = false
        XCTAssertEqual(MenuPanelModel(model: model).subtitle, "后台服务已停止")
    }

    func testGroups_ThreeGroupsInOrder() {
        let panel = MenuPanelModel(model: makeAppModel())
        XCTAssertEqual(panel.groups.map { $0.map(\.id) }, [
            [.forwarding, .openConfig],
            [.loginStart, .update],
            [.uninstall, .quit],
        ])
    }

    func testItemTitlesAndShortcuts() {
        let panel = MenuPanelModel(model: makeAppModel())
        XCTAssertEqual(panel.groups.map { $0.map(\.title) }, [
            ["转发到中台", "打开配置…"],
            ["登录时启动", "检查更新"],
            ["卸载模型接入助手…", "退出模型接入助手"],
        ])
        XCTAssertEqual(item(.openConfig, in: panel)?.shortcut, "⌘,")
        XCTAssertEqual(item(.quit, in: panel)?.shortcut, "⌘Q")
        XCTAssertNil(item(.forwarding, in: panel)?.shortcut)
        XCTAssertNil(item(.uninstall, in: panel)?.shortcut)
    }

    func testForwardingItem_CheckedFollowsForwarding() {
        let model = makeAppModel()
        XCTAssertEqual(item(.forwarding, in: MenuPanelModel(model: model))?.checked, false)
        model.status?.forwarding = true
        XCTAssertEqual(item(.forwarding, in: MenuPanelModel(model: model))?.checked, true)
    }

    func testForwardingItem_DisabledMatchesAppModel() {
        let enabledModel = makeAppModel()
        XCTAssertEqual(item(.forwarding, in: MenuPanelModel(model: enabledModel))?.isDisabled, false)

        let noChatGPT = makeAppModel(chatGPTFound: false)
        XCTAssertTrue(noChatGPT.forwardingToggleDisabled)
        XCTAssertEqual(item(.forwarding, in: MenuPanelModel(model: noChatGPT))?.isDisabled, true)

        let stopped = makeAppModel()
        stopped.coreRunning = false
        XCTAssertEqual(item(.forwarding, in: MenuPanelModel(model: stopped))?.isDisabled, true)

        let switching = makeAppModel()
        switching.isSwitchingForwarding = true
        XCTAssertEqual(item(.forwarding, in: MenuPanelModel(model: switching))?.isDisabled, true)
    }

    func testLoginStartItem_CheckedFollowsModel() {
        let model = makeAppModel()
        XCTAssertEqual(item(.loginStart, in: MenuPanelModel(model: model))?.checked, false)
        model.loginStartEnabled = true
        XCTAssertEqual(item(.loginStart, in: MenuPanelModel(model: model))?.checked, true)
    }

    func testPlainActionItems_AreNotCheckable() {
        let panel = MenuPanelModel(model: makeAppModel())
        for id in [MenuPanelItem.ID.openConfig, .update, .uninstall, .quit] {
            XCTAssertNil(item(id, in: panel)?.checked, "\(id)")
        }
    }

    func testUpdateItem_NoUpdate_IsCheckUpdateWithoutSubtitle() {
        let update = item(.update, in: MenuPanelModel(model: makeAppModel()))
        XCTAssertEqual(update?.title, "检查更新")
        XCTAssertNil(update?.subtitle)
    }

    func testUpdateItem_NewVersion_ShowsVersionAndDownloadHint() {
        let model = makeAppModel()
        model.availableUpdate = UpdateInfo(version: "1.3.0", url: "https://example.com/dl")
        let update = item(.update, in: MenuPanelModel(model: model))
        XCTAssertEqual(update?.title, "发现新版本 1.3.0")
        XCTAssertEqual(update?.subtitle, "打开下载页")
    }
}
