enum DialogAction: Equatable, Sendable {
    case cancel, confirm
    case cancelUninstall, revealFolder, recheck
    case dismiss
}

enum DialogButtonStyle: Equatable, Sendable { case primary, secondary }

struct DialogButton: Equatable, Sendable {
    let label: String
    let action: DialogAction
    let style: DialogButtonStyle
    var isDefaultFocus = false
    var isEnabled = true
    var showsSpinner = false
}

enum DialogAlertSeverity: Equatable, Sendable { case danger }

struct DialogAlert: Equatable, Sendable {
    let title: String
    let message: String
    let severity: DialogAlertSeverity

    var tone: AlertTone {
        switch severity {
        case .danger: .danger
        }
    }
}

protocol DialogContent {
    var title: String { get }
    var message: String { get }
    var alert: DialogAlert? { get }
    var buttons: [DialogButton] { get }
    var escapeAction: DialogAction? { get }
}

struct ConfirmDialogModel: DialogContent {
    let title: String
    let message: String
    let buttons: [DialogButton]
    let alert: DialogAlert? = nil
    let escapeAction: DialogAction?

    init(confirmation: Confirmation, busyLabel: String? = nil) {
        title = confirmation.title
        message = confirmation.message
        let busy = busyLabel != nil
        escapeAction = busy ? nil : .cancel
        buttons = [
            DialogButton(label: confirmation.cancelLabel, action: .cancel, style: .secondary, isDefaultFocus: true, isEnabled: !busy),
            DialogButton(label: busyLabel ?? confirmation.confirmLabel, action: .confirm, style: .primary, isEnabled: !busy, showsSpinner: busy),
        ]
    }
}

struct UninstallPausedDialogModel: DialogContent {
    let title = "卸载暂停：有设置被手动改过"
    let message: String
    let alert: DialogAlert?
    let buttons: [DialogButton]
    let escapeAction: DialogAction?

    init(keys: [String], configPath: String?, busyLabel: String? = nil) {
        let busy = busyLabel != nil
        escapeAction = busy ? nil : .cancelUninstall
        buttons = [
            DialogButton(label: "取消卸载", action: .cancelUninstall, style: .secondary, isDefaultFocus: true, isEnabled: !busy),
            DialogButton(label: "打开所在文件夹", action: .revealFolder, style: .secondary, isEnabled: !busy),
            DialogButton(label: busyLabel ?? "重新检查", action: .recheck, style: .primary, isEnabled: !busy, showsSpinner: busy),
        ]
        message = "这些设置项被手动改过，为了不覆盖你的修改，助手没有还原它们：\n\(keys.joined(separator: "、"))\n\n配置文件：\(configPath ?? "未知")\n\n请手动处理后点「重新检查」。"
        alert = DialogAlert(title: "没能自动还原 Codex 配置", message: message, severity: .danger)
    }
}

struct RestoreHelpDialogModel: DialogContent {
    enum Purpose: Sendable { case restoreBlocked, configInvalid }

    let title: String
    let message: String
    let alert: DialogAlert? = nil
    let buttons = [
        DialogButton(label: "关闭", action: .dismiss, style: .secondary, isDefaultFocus: true),
        DialogButton(label: "打开所在文件夹", action: .revealFolder, style: .secondary),
        DialogButton(label: "重新检查", action: .recheck, style: .primary),
    ]
    let escapeAction: DialogAction? = .dismiss

    init(keys: [String], configPath: String?, purpose: Purpose = .restoreBlocked) {
        let list = keys.joined(separator: "、")
        let path = configPath ?? "未知"
        switch purpose {
        case .restoreBlocked:
            title = "没能自动还原 Codex 配置"
            let changed = keys.isEmpty ? "" : "这些设置项被手动改过：\n\(list)\n\n"
            message = "为了不覆盖你的修改，助手没有还原本助手写入的设置。\n\n\(changed)配置文件：\(path)\n\n请手动处理后点「重新检查」。"
        case .configInvalid:
            title = "Codex 配置文件格式有误"
            let wrong = keys.isEmpty ? "" : "这些设置项的格式不对：\n\(list)\n\n"
            message = "助手没有改动配置文件。\n\n\(wrong)配置文件：\(path)\n\n请手动修正后点「重新检查」。"
        }
    }
}

struct InfoDialogModel: DialogContent {
    let title: String
    let message: String
    let buttons: [DialogButton]
    let alert: DialogAlert? = nil
    let escapeAction: DialogAction? = .dismiss

    static func trashFailure(path: String) -> InfoDialogModel {
        InfoDialogModel(
            title: "没能把模型接入助手移到废纸篓",
            message: "请手动把它拖到废纸篓：\n\(path)",
            buttons: [DialogButton(label: "好", action: .dismiss, style: .primary, isDefaultFocus: true)]
        )
    }

    static func uninstallFinished(leftovers: [String], chatGPTRunning: Bool) -> InfoDialogModel {
        var message = "已卸载模型接入助手。"
        if !leftovers.isEmpty {
            message += "\n\n以下内容没有删除干净，请手动处理：\(leftovers.joined(separator: "、"))"
        }
        if chatGPTRunning { message += "\n\n请重启 ChatGPT，Codex 的模型列表才会恢复成只有官方模型。" }
        let buttons = [DialogButton(label: "好", action: .dismiss, style: .primary, isDefaultFocus: true)]
        return InfoDialogModel(title: "已卸载模型接入助手", message: message, buttons: buttons)
    }
}
