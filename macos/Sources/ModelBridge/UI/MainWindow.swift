import AppKit
import SwiftUI

struct MainWindow: View {
    @ObservedObject var model: AppModel

    var body: some View {
        VStack(spacing: 0) {
            VStack(alignment: .leading, spacing: 20) {
                forwardingCard
                connectionCard
                statusCard
            }
            .padding(24)
            statusBar
        }
        .frame(width: 620)
        .background(Palette.background)
        .overlay(alignment: .top) {
            AlertOverlay {
                NoticeStack(model: model)
                ForEach(Array(model.banners.enumerated()), id: \.offset) { _, banner in
                    bannerView(banner)
                }
            }
        }
    }

    private var chatGPTFound: Bool { model.status?.chatgpt?.found != false }

    @ViewBuilder
    private func bannerView(_ banner: Banner) -> some View {
        let tone: AlertTone = switch banner.kind {
        case .info: .info
        case .warning: .warning
        case .error: .danger
        }
        AlertBar(tone: tone, title: banner.title, detail: banner.detail, onClose: { model.dismissBanner(banner) }) {
            switch banner.action {
            case .refreshModels:
                Button("重试") { Task { await model.refreshModels() } }.secondaryAction(compact: true)
            case .recheckStatus:
                Button("重新检测") { Task { await model.recheckStatus() } }.secondaryAction(compact: true)
            case .restoreHelp, .configInvalidHelp:
                Button("查看详情") {
                    Task {
                        await DialogPresenter.showRestoreHelp(
                            keys: banner.keys,
                            configPath: banner.configPath,
                            purpose: banner.action == .configInvalidHelp ? .configInvalid : .restoreBlocked
                        ) {
                            await model.recheckStatus()
                        }
                    }
                }
                .secondaryAction(compact: true)
            case nil:
                EmptyView()
            }
        }
    }

    static func reveal(_ path: String?) {
        guard let path else { return }
        NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: path)])
    }

    private var forwardingCard: some View {
        Card(title: "转发") {
            FieldRow(
                label: "转发到中台",
                help: model.forwardingHelpText
            ) {
                SwitchRow(label: "转发到中台", isOn: Binding(
                    get: { model.forwardingEnabled },
                    set: { value in Task { await model.toggleForwarding(enabled: value) } }
                ))
                .disabled(model.forwardingToggleDisabled)
            }
            accessRow
        }
    }

    @ViewBuilder
    private var accessRow: some View {
        if let name = model.accessModeName {
            FieldRow(label: "接入方式", help: model.accessHelpText) {
                VStack(alignment: .leading, spacing: 8) {
                    HStack(spacing: 12) {
                        HStack(spacing: 4) {
                            Text(name).font(Fonts.label).foregroundColor(Palette.text)
                            if let reason = model.accessReasonText {
                                Text(reason).font(Fonts.control).foregroundColor(Palette.textMuted)
                            }
                        }
                        Spacer()
                        Button { Task { await model.recheckAccess() } } label: {
                            BusyLabel(text: model.isDetectingAccess ? "正在检测…" : "重新检测", busy: model.isDetectingAccess)
                        }
                        .secondaryAction()
                        .disabled(model.isDetectingAccess)
                    }
                    .frame(minHeight: 44, alignment: .center)
                }
            }
        }
    }

    private var connectionCard: some View {
        Card(title: "中台连接") {
            FieldRow(label: "Base URL", help: "以 https:// 开头的完整地址。", error: model.setupFieldErrors[.baseURL]) {
                AppTextField(
                    label: "Base URL", placeholder: "https://relay.example.com/v1", text: $model.baseURL,
                    mono: true, invalid: model.setupFieldErrors[.baseURL] != nil, errorText: model.setupFieldErrors[.baseURL]
                )
            }
            FieldRow(label: "API Key", help: "保存在系统钥匙串中。", error: model.setupFieldErrors[.apiKey]) {
                SecretInput(
                    placeholder: "", text: $model.apiKey,
                    invalid: model.setupFieldErrors[.apiKey] != nil, errorText: model.setupFieldErrors[.apiKey]
                )
            }
            .disabled(model.isSaving)
            HStack {
                Button { Task { await model.saveConnection() } } label: {
                    BusyLabel(text: model.isSaving ? "正在保存并重新获取模型…" : "保存修改", busy: model.isSaving)
                }
                .secondaryAction()
                .disabled(model.isSaving)
            }
        }
        .disabled(model.isSwitchingForwarding)
    }

    private var statusCard: some View {
        Card(title: "运行状态") {
            VStack(alignment: .leading, spacing: 12) {
                kv("ChatGPT", chatGPTFound ? "已检测到" : "未找到", good: chatGPTFound)
                kv("中台模型", model.modelCountText, good: (model.status?.relay?.modelCount ?? 0) > 0)
            }
        }
    }

    private func kv(_ key: String, _ value: String, good: Bool) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Text(key).font(Fonts.label).frame(width: 112, alignment: .leading)
            Label(value, systemImage: good ? "checkmark.circle.fill" : "minus.circle")
                .font(Fonts.control)
                .foregroundColor(good ? Palette.success : Palette.textMuted)
        }
        .foregroundColor(Palette.text)
    }

    private var statusBar: some View {
        HStack(spacing: 8) {
            Text("v\(model.status?.version ?? "—")").font(Fonts.mono).foregroundColor(Palette.textMuted)
            Circle().fill(model.coreRunning ? Palette.success : Palette.borderStrong).frame(width: 8, height: 8)
            Text(model.runtimeText + (model.coreRunning && !model.forwardingEnabled ? " · 转发已关闭" : ""))
                .font(Fonts.caption)
                .foregroundColor(Palette.textMuted)
            Spacer()
            if let update = model.availableUpdate {
                Text("发现新版本 \(update.version)").font(Fonts.caption).foregroundColor(Palette.info)
                Button("打开下载页") {
                    if let url = URL(string: update.url) { NSWorkspace.shared.open(url) }
                }
                .linkAction()
            } else {
                Button("检查更新") { Task { await model.checkUpdate() } }.linkAction()
            }
        }
        .padding(.horizontal, 24)
        .frame(minHeight: 48)
        .background(Palette.surface)
        .overlay(alignment: .top) { Rectangle().fill(Palette.border).frame(height: 1) }
    }
}
