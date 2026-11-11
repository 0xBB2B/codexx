import XCTest
@testable import ModelBridge

private let allConfirmations: [Confirmation] = [
    Confirmation(kind: .toggleForwarding(enabled: true), title: "打开转发并重启 ChatGPT？", message: "正文一", confirmLabel: "打开并重启"),
    Confirmation(kind: .toggleForwarding(enabled: false), title: "关闭转发并重启 ChatGPT？", message: "正文二", confirmLabel: "关闭并重启"),
    Confirmation(kind: .restartAfterSave, title: "已保存，重启 ChatGPT 以更新模型列表？", message: "正文三", cancelLabel: "稍后", confirmLabel: "立即重启"),
    Confirmation(kind: .loginStartOff, title: "关闭「登录时启动」？", message: "正文四", confirmLabel: "仍然关闭"),
    Confirmation(kind: .quit, title: "退出模型接入助手？", message: "正文五", confirmLabel: "还原并退出"),
    Confirmation(kind: .uninstall, title: "卸载模型接入助手？", message: "正文六", confirmLabel: "卸载"),
]

final class DialogModelTests: XCTestCase {
    func testContent_MatchesConfirmation() {
        for confirmation in allConfirmations {
            let dialog = ConfirmDialogModel(confirmation: confirmation)
            XCTAssertEqual(dialog.title, confirmation.title)
            XCTAssertEqual(dialog.message, confirmation.message)
        }
    }

    func testButtons_CancelThenConfirm() {
        for confirmation in allConfirmations {
            let dialog = ConfirmDialogModel(confirmation: confirmation)
            XCTAssertEqual(dialog.buttons.map(\.label), [confirmation.cancelLabel, confirmation.confirmLabel])
            XCTAssertEqual(dialog.buttons.map(\.action), [.cancel, .confirm])
            XCTAssertEqual(dialog.buttons.map(\.style), [.secondary, .primary])
        }
    }

    func testDefaultCancelLabel_IsCancel() {
        let dialog = ConfirmDialogModel(confirmation: allConfirmations[0])
        XCTAssertEqual(dialog.buttons.first?.label, "取消")
    }

    func testDefaultFocus_IsCancelButton() {
        for confirmation in allConfirmations {
            let dialog = ConfirmDialogModel(confirmation: confirmation)
            XCTAssertEqual(dialog.buttons.map(\.isDefaultFocus), [true, false])
        }
    }

    func testEscape_IsCancel() {
        for confirmation in allConfirmations {
            XCTAssertEqual(ConfirmDialogModel(confirmation: confirmation).escapeAction, .cancel)
        }
    }

    func testUninstallPaused_ThreeButtonsInOrder() {
        let dialog = UninstallPausedDialogModel(keys: ["model_provider", "chatgpt_base_url"], configPath: "/Users/example/.codex/config.toml")
        XCTAssertEqual(dialog.buttons.map(\.label), ["取消卸载", "打开所在文件夹", "重新检查"])
        XCTAssertEqual(dialog.buttons.map(\.action), [.cancelUninstall, .revealFolder, .recheck])
    }

    func testUninstallPaused_MessageListsKeysAndConfigPath() {
        let dialog = UninstallPausedDialogModel(keys: ["model_provider", "chatgpt_base_url"], configPath: "/Users/example/.codex/config.toml")
        XCTAssertTrue(dialog.message.contains("model_provider"))
        XCTAssertTrue(dialog.message.contains("chatgpt_base_url"))
        XCTAssertTrue(dialog.message.contains("/Users/example/.codex/config.toml"))
    }

    func testUninstallPaused_AlertIsDangerWithTitle() {
        let dialog = UninstallPausedDialogModel(keys: ["model_provider"], configPath: "/Users/example/.codex/config.toml")
        XCTAssertEqual(dialog.alert?.severity, .danger)
        XCTAssertEqual(dialog.alert?.title, "没能自动还原 Codex 配置")
    }

    func testUninstallPaused_AlertMessageListsKeysAndConfigPath() {
        let dialog = UninstallPausedDialogModel(keys: ["model_provider", "chatgpt_base_url"], configPath: "/Users/example/.codex/config.toml")
        let message = dialog.alert?.message ?? ""
        XCTAssertTrue(message.contains("model_provider"))
        XCTAssertTrue(message.contains("chatgpt_base_url"))
        XCTAssertTrue(message.contains("/Users/example/.codex/config.toml"))
    }

    func testUninstallPaused_StylesAndDefaultFocus() {
        let dialog = UninstallPausedDialogModel(keys: ["model_provider"], configPath: nil)
        XCTAssertEqual(dialog.buttons.map(\.style), [.secondary, .secondary, .primary])
        XCTAssertEqual(dialog.buttons.map(\.isDefaultFocus), [true, false, false])
    }

    func testConfirm_HasNoAlert() {
        XCTAssertNil(ConfirmDialogModel(confirmation: allConfirmations[0]).alert)
    }

    func testUninstallPaused_EscapeIsCancelUninstall() {
        let dialog = UninstallPausedDialogModel(keys: [], configPath: nil)
        XCTAssertEqual(dialog.escapeAction, .cancelUninstall)
    }
}

final class InfoDialogModelTests: XCTestCase {
    func testUninstallFinished_ChatGPTRunning_OffersRestart() {
        let dialog = InfoDialogModel.uninstallFinished(leftovers: [], chatGPTRunning: true)
        XCTAssertEqual(dialog.title, "已卸载模型接入助手")
        XCTAssertEqual(dialog.buttons.map(\.label), ["稍后自己重启", "立即重启 ChatGPT"])
        XCTAssertEqual(dialog.buttons.map(\.action), [.dismiss, .restartChatGPT])
        XCTAssertEqual(dialog.buttons.map(\.style), [.secondary, .primary])
        XCTAssertEqual(dialog.buttons.map(\.isDefaultFocus), [true, false])
        XCTAssertEqual(dialog.escapeAction, .dismiss)
        XCTAssertTrue(dialog.message.contains("ChatGPT 需要重启一次"))
    }

    func testUninstallFinished_ChatGPTNotRunning_SingleOkButton() {
        let dialog = InfoDialogModel.uninstallFinished(leftovers: [], chatGPTRunning: false)
        XCTAssertEqual(dialog.buttons.map(\.label), ["好"])
        XCTAssertEqual(dialog.buttons.map(\.action), [.dismiss])
        XCTAssertEqual(dialog.buttons.map(\.style), [.primary])
        XCTAssertEqual(dialog.buttons.map(\.isDefaultFocus), [true])
        XCTAssertEqual(dialog.escapeAction, .dismiss)
        XCTAssertFalse(dialog.message.contains("ChatGPT 需要重启一次"))
    }

    func testUninstallFinished_ListsLeftovers() {
        let dialog = InfoDialogModel.uninstallFinished(leftovers: ["钥匙串", "设置文件"], chatGPTRunning: false)
        XCTAssertTrue(dialog.message.contains("钥匙串、设置文件"))
        XCTAssertFalse(InfoDialogModel.uninstallFinished(leftovers: [], chatGPTRunning: false).message.contains("没有删除干净"))
    }

    func testTrashFailure_ShowsPathWithSingleOkButton() {
        let dialog = InfoDialogModel.trashFailure(path: "/Applications/模型接入助手.app")
        XCTAssertEqual(dialog.title, "没能把模型接入助手移到废纸篓")
        XCTAssertTrue(dialog.message.contains("/Applications/模型接入助手.app"))
        XCTAssertEqual(dialog.buttons.map(\.label), ["好"])
        XCTAssertEqual(dialog.buttons.map(\.isDefaultFocus), [true])
        XCTAssertEqual(dialog.escapeAction, .dismiss)
    }
}
