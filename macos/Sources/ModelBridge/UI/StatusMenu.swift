import AppKit
import SwiftUI

struct StatusMenu: View {
    @ObservedObject var model: AppModel
    let openMain: () -> Void

    var body: some View {
        Text(model.menuStatusLine)
        Divider()
        Toggle("转发到中台", isOn: Binding(
            get: { model.forwardingEnabled },
            set: { value in Task { await model.toggleForwarding(enabled: value) } }
        ))
        .disabled(model.forwardingToggleDisabled)
        Button("打开配置…", action: openMain).keyboardShortcut(",")
        Divider()
        Toggle("登录时启动", isOn: Binding(
            get: { model.loginStartEnabled },
            set: { value in Task { await model.setLoginStart(value) } }
        ))
        if let update = model.availableUpdate {
            Button("发现新版本 \(update.version) · 打开下载页") {
                if let url = URL(string: update.url) { NSWorkspace.shared.open(url) }
            }
        } else {
            Button("检查更新") { Task { await model.checkUpdate() } }
        }
        Divider()
        Button("卸载模型接入助手…") { Task { await model.uninstall() } }
        Button("退出模型接入助手") { Task { await model.quit() } }.keyboardShortcut("q")
    }
}
