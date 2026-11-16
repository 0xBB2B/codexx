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
                label: "使用中转",
                help: model.forwardingHelpText
            ) {
                SwitchRow(label: "使用中转", isOn: Binding(
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
        Card(title: "中转配置") {
            ForEach(model.relayRows) { row in
                HStack(spacing: 12) {
                    Text(row.name).font(Fonts.label).foregroundColor(Palette.text)
                    Text(row.text).font(Fonts.control).foregroundColor(Palette.textMuted)
                    Spacer()
                    if row.canRetry {
                        Button("重试") { Task { await model.retryRelay(id: row.id) } }.secondaryAction(compact: true)
                    }
                    Button("编辑") { model.beginEditRelay(id: row.id) }.secondaryAction(compact: true).disabled(!row.canEdit)
                    Button("删除") { model.requestDeleteRelay(id: row.id) }.secondaryAction(compact: true).disabled(!row.canDelete)
                }
            }
            HStack {
                Text(model.relayListCaption).font(Fonts.control).foregroundColor(Palette.textMuted)
                Spacer()
                Button("添加") { model.beginAddRelay() }.secondaryAction().disabled(!model.canAddRelay)
            }
        }
        .disabled(model.isSwitchingForwarding)
    }

    private var statusCard: some View {
        Card(title: "运行状态") {
            VStack(alignment: .leading, spacing: 12) {
                kv("ChatGPT", chatGPTFound ? "已检测到" : "未找到", good: chatGPTFound)
                kv("中转模型", model.modelCountText, good: model.relayRows.contains { $0.canRetry == false && $0.text.hasSuffix("个模型") })
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
