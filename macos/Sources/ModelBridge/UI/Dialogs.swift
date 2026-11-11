import AppKit
import Combine

@MainActor
final class DialogPresenter {
    private let model: AppModel
    private var subscriptions: Set<AnyCancellable> = []
    private var presenting = false

    init(model: AppModel) {
        self.model = model
        model.$pendingConfirmation.compactMap { $0 }.sink { [weak self] value in
            Task { @MainActor in await self?.present(value) }
        }.store(in: &subscriptions)
        model.onTrashFailure = { [weak self] path in
            let failure = self?.makeAlert(
                title: "没能把模型接入助手移到废纸篓",
                message: "请手动把它拖到废纸篓：\n\(path)"
            )
            failure?.addButton(withTitle: "好")
            if let failure { _ = self?.run(failure) }
        }
        model.$uninstallPhase.sink { [weak self] phase in
            Task { @MainActor in await self?.present(phase) }
        }.store(in: &subscriptions)
    }

    private func present(_ confirmation: Confirmation) async {
        guard !presenting else { return }
        presenting = true
        let alert = makeAlert(title: confirmation.title, message: confirmation.message)
        alert.addButton(withTitle: confirmation.confirmLabel)
        alert.addButton(withTitle: confirmation.cancelLabel)
        focusCancel(alert, index: 1)
        let confirmed = run(alert) == .alertFirstButtonReturn
        presenting = false
        if confirmed { await model.confirmPending() } else { model.cancelPending() }
    }

    private func present(_ phase: UninstallPhase) async {
        switch phase {
        case .paused(let keys, let configPath):
            guard !presenting else { return }
            presenting = true
            while true {
                let alert = makeAlert(
                    title: "卸载暂停：有设置被手动改过",
                    message: "这些设置项被手动改过，为了不覆盖你的修改，助手没有还原它们：\n\(keys.joined(separator: "、"))\n\n配置文件：\(configPath ?? "未知")\n\n请手动处理后点「重新检查」。"
                )
                alert.addButton(withTitle: "重新检查")
                alert.addButton(withTitle: "打开所在文件夹")
                alert.addButton(withTitle: "取消卸载")
                focusCancel(alert, index: 2)
                let choice = run(alert)
                if choice == .alertSecondButtonReturn {
                    MainWindow.reveal(configPath)
                    continue
                }
                presenting = false
                if choice == .alertFirstButtonReturn { await model.recheckUninstall() } else { model.cancelUninstall() }
                return
            }
        case .finished:
            guard !presenting else { return }
            presenting = true
            var message = "已卸载模型接入助手。"
            if !model.uninstallLeftovers.isEmpty {
                message += "\n\n以下内容没有删除干净，请手动处理：\(model.uninstallLeftovers.joined(separator: "、"))"
            }
            let running = model.isChatGPTRunning
            if running { message += "\n\nChatGPT 需要重启一次，Codex 的模型列表才会恢复成只有官方模型。" }
            let alert = makeAlert(title: "已卸载模型接入助手", message: message)
            if running {
                alert.addButton(withTitle: "立即重启 ChatGPT")
                alert.addButton(withTitle: "稍后自己重启")
            } else {
                alert.addButton(withTitle: "好")
            }
            focusCancel(alert, index: alert.buttons.count - 1)
            let response = run(alert)
            presenting = false
            if running, response == .alertFirstButtonReturn { await model.restartChatGPT() }
            await model.finishUninstall()
        case .idle, .running:
            break
        }
    }

    private func makeAlert(title: String, message: String) -> NSAlert {
        let alert = NSAlert()
        alert.messageText = title
        alert.informativeText = message
        return alert
    }

    private func focusCancel(_ alert: NSAlert, index: Int) {
        alert.buttons.forEach { $0.keyEquivalent = "" }
        alert.buttons[index].keyEquivalent = "\u{1b}"
        alert.window.initialFirstResponder = alert.buttons[index]
    }

    private func run(_ alert: NSAlert) -> NSApplication.ModalResponse {
        NSApp.activate(ignoringOtherApps: true)
        return alert.runModal()
    }
}
