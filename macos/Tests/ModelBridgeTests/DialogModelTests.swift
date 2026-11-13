import XCTest
@testable import ModelBridge

private let allConfirmations: [Confirmation] = [
    Confirmation(kind: .loginStartOff, title: "关闭「登录时启动」？", message: "正文一", confirmLabel: "仍然关闭"),
    Confirmation(kind: .quit, title: "退出模型接入助手？", message: "正文二", confirmLabel: "还原并退出"),
    Confirmation(kind: .uninstall, title: "卸载模型接入助手？", message: "正文三", confirmLabel: "卸载"),
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

final class RestoreHelpDialogModelTests: XCTestCase {
    func testMessage_ListsKeysAndConfigPath() {
        let dialog = RestoreHelpDialogModel(keys: ["model_provider", "chatgpt_base_url"], configPath: "/Users/example/.codex/config.toml")
        XCTAssertTrue(dialog.message.contains("model_provider、chatgpt_base_url"))
        XCTAssertTrue(dialog.message.contains("/Users/example/.codex/config.toml"))
    }

    func testMessage_ConfigInvalidKeys_ListsKeysAndConfigPath() {
        let dialog = RestoreHelpDialogModel(keys: ["model_providers.relay"], configPath: "/Users/example/.codex/config.toml")
        XCTAssertTrue(dialog.message.contains("model_providers.relay"))
        XCTAssertTrue(dialog.message.contains("/Users/example/.codex/config.toml"))
    }

    func testMessage_NoConfigPath_ShowsUnknown() {
        XCTAssertTrue(RestoreHelpDialogModel(keys: ["model_provider"], configPath: nil).message.contains("未知"))
    }

    func testButtons_CloseOpenFolderRecheck() {
        let dialog = RestoreHelpDialogModel(keys: [], configPath: nil)
        XCTAssertEqual(dialog.buttons.map(\.label), ["关闭", "打开所在文件夹", "重新检查"])
        XCTAssertEqual(dialog.buttons.map(\.action), [.dismiss, .revealFolder, .recheck])
        XCTAssertEqual(dialog.buttons.map(\.isDefaultFocus), [true, false, false])
        XCTAssertEqual(dialog.escapeAction, .dismiss)
    }
}

final class InfoDialogModelTests: XCTestCase {
    func testUninstallFinished_ChatGPTRunning_SingleOkButtonAndAsksToRestartManually() {
        let dialog = InfoDialogModel.uninstallFinished(leftovers: [], chatGPTRunning: true)
        XCTAssertEqual(dialog.title, "已卸载模型接入助手")
        XCTAssertEqual(dialog.buttons.map(\.label), ["好"])
        XCTAssertEqual(dialog.buttons.map(\.action), [.dismiss])
        XCTAssertEqual(dialog.buttons.map(\.style), [.primary])
        XCTAssertEqual(dialog.buttons.map(\.isDefaultFocus), [true])
        XCTAssertEqual(dialog.escapeAction, .dismiss)
        XCTAssertTrue(dialog.message.contains("请重启 ChatGPT，Codex 的模型列表才会恢复成只有官方模型。"))
    }

    func testUninstallFinished_ChatGPTNotRunning_SingleOkButton() {
        let dialog = InfoDialogModel.uninstallFinished(leftovers: [], chatGPTRunning: false)
        XCTAssertEqual(dialog.buttons.map(\.label), ["好"])
        XCTAssertEqual(dialog.buttons.map(\.action), [.dismiss])
        XCTAssertEqual(dialog.buttons.map(\.style), [.primary])
        XCTAssertEqual(dialog.buttons.map(\.isDefaultFocus), [true])
        XCTAssertEqual(dialog.escapeAction, .dismiss)
        XCTAssertFalse(dialog.message.contains("请重启 ChatGPT"))
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
