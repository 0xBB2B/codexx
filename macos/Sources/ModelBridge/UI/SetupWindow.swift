import SwiftUI

struct SetupWindow: View {
    @ObservedObject var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            VStack(alignment: .leading, spacing: 8) {
                Text("连接到中台").font(Fonts.pageTitle).foregroundColor(Palette.text)
                Text("填写中台提供的 Base URL 和 API Key。设置完成后，Codex 的模型列表里会多出中台模型。")
                    .font(Fonts.control)
                    .foregroundColor(Palette.textMuted)
            }
            if case .succeeded(let title, let count) = model.setupPhase {
                succeeded(title: title, count: count)
            } else {
                form
            }
        }
        .padding(24)
        .frame(width: 620)
        .background(Palette.background)
    }

    private var connecting: Bool { model.setupPhase == .connecting }

    @ViewBuilder
    private var form: some View {
        if case .failed(let title, let detail) = model.setupPhase {
            AlertBar(tone: .danger, title: title, detail: detail)
        }
        Card {
            FieldRow(
                label: "Base URL",
                required: true,
                help: "以 https:// 开头的完整地址。",
                error: model.setupFieldErrors[.baseURL]
            ) {
                TextField("https://relay.example.com/v1", text: $model.baseURL)
                    .textFieldStyle(.roundedBorder)
                    .font(Fonts.mono)
            }
            FieldRow(
                label: "API Key",
                required: true,
                help: "保存在系统钥匙串中。",
                error: model.setupFieldErrors[.apiKey]
            ) {
                SecretInput(placeholder: "", text: $model.apiKey)
            }
        }
        .disabled(connecting)
        HStack {
            Spacer()
            Button { Task { await model.completeSetup() } } label: {
                BusyLabel(text: connecting ? "正在连接中台…" : "完成设置", busy: connecting)
            }
            .primaryAction()
            .disabled(connecting)
        }
    }

    @ViewBuilder
    private func succeeded(title: String, count: Int) -> some View {
        AlertBar(
            tone: .success,
            title: title,
            detail: "已获取 \(count) 个中台模型。重启 ChatGPT 后，Codex 的模型列表里就会出现它们。助手会在你登录电脑时自动启动，在菜单栏里常驻。"
        )
        ForEach(model.setupFollowUpIssues, id: \.self) { issue in
            AlertBar(tone: .warning, title: issue)
        }
        HStack(spacing: 12) {
            Spacer()
            Button("稍后自己重启") { model.finishSetup() }.secondaryAction()
            Button("立即重启 ChatGPT") {
                Task {
                    await model.restartChatGPT()
                    model.finishSetup()
                }
            }
            .primaryAction()
        }
    }
}
