import AppKit
import SwiftUI

struct MainWindow: View {
    @ObservedObject var model: AppModel

    var body: some View {
        VStack(spacing: 0) {
            WindowTitleBar(title: "模型接入助手")
            VStack(spacing: 0) {
                VStack(alignment: .leading, spacing: 20) {
                    ForwardingOverview(model: model)
                    RelayConfigCard(model: model)
                }
                .padding(24)
                StatusFooter(model: model)
            }
            .overlay(alignment: .top) {
                AlertOverlay {
                    NoticeStack(model: model)
                    ForEach(Array(model.banners.enumerated()), id: \.offset) { _, banner in
                        bannerView(banner)
                    }
                }
            }
        }
        .frame(width: 760)
        .background(Palette.background)
    }

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
                Button { Task { await model.recheckStatus() } } label: {
                    BusyLabel(text: model.isRecheckingStatus ? "正在检测…" : "重新检测", busy: model.isRecheckingStatus)
                }
                .secondaryAction(compact: true)
                .disabled(model.isRecheckingStatus)
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
}

struct ForwardingOverview: View {
    @ObservedObject var model: AppModel

    private var subtitle: String {
        if model.forwardingEnabled, let help = model.accessHelpText { return help }
        return model.forwardingHelpText
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 16) {
                AppIconView(size: 48)
                VStack(alignment: .leading, spacing: 4) {
                    Text(model.forwardingTitle).font(Fonts.sectionTitle).foregroundColor(Palette.text)
                    Text(subtitle).font(Fonts.caption).foregroundColor(Palette.textMuted)
                }
                Spacer(minLength: 0)
                SwitchRow(label: "使用中转", isOn: Binding(
                    get: { model.forwardingEnabled },
                    set: { value in Task { await model.toggleForwarding(enabled: value) } }
                ), showsState: false)
                .disabled(model.forwardingToggleDisabled)
            }
            if model.forwardingEnabled, let name = model.accessModeName {
                HStack(spacing: 12) {
                    LabelValue(label: "接入方式", value: name, note: model.accessReasonText)
                    Spacer(minLength: 0)
                    Button { Task { await model.recheckAccess() } } label: {
                        BusyLabel(text: model.isDetectingAccess ? "正在检测…" : "重新检测", busy: model.isDetectingAccess)
                    }
                    .secondaryAction(compact: true)
                    .disabled(model.isDetectingAccess)
                }
                .frame(minHeight: 44)
            }
        }
        .padding(.vertical, 16)
        .padding(.horizontal, 24)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 12).fill(model.forwardingEnabled ? Palette.selectedBg : Palette.surface))
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(model.forwardingEnabled ? Palette.primary : Palette.border, lineWidth: 1))
    }
}

struct RelayConfigCard: View {
    @ObservedObject var model: AppModel
    private static let rowHeight: CGFloat = 64
    private static let visibleRows = 3
    @FocusState private var focus: MainFocusTarget?

    var body: some View {
        Card(title: "中转配置") {
            let rows = model.relayRows
            ScrollView {
                VStack(spacing: 0) {
                    ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
                        RelayRowView(model: model, row: row, focus: $focus)
                            .frame(height: Self.rowHeight)
                            .overlay(alignment: .top) {
                                if index > 0 { Rectangle().fill(Palette.border).frame(height: 1) }
                            }
                    }
                }
            }
            .frame(height: Self.rowHeight * CGFloat(min(rows.count, Self.visibleRows)))
            HStack(spacing: 12) {
                Button("添加配置") { model.beginAddRelay() }.secondaryAction().disabled(!model.canAddRelay)
                    .focusable(model.canAddRelay)
                    .focused($focus, equals: .addButton)
                Text(model.deleteBlockedReason ?? model.relayListCaption).font(Fonts.caption).foregroundColor(Palette.textMuted)
                Spacer(minLength: 0)
            }
        }
        .disabled(model.isSwitchingForwarding)
        .onChange(of: model.mainFocusTarget) { target in
            guard let target else { return }
            DispatchQueue.main.async { focus = target; model.mainFocusTarget = nil }
        }
    }
}

private struct RelayRowView: View {
    @ObservedObject var model: AppModel
    let row: RelayRow
    var focus: FocusState<MainFocusTarget?>.Binding

    private var baseURL: String { model.relays.first { $0.id == row.id }?.baseURL ?? "" }
    private var busy: Bool { row.text.hasPrefix("正在获取") }

    var body: some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text(row.name).font(Fonts.label).foregroundColor(Palette.text).lineLimit(1)
                HStack(spacing: 8) {
                    status.fixedSize()
                    Text(baseURL).font(Fonts.mono).foregroundColor(Palette.textMuted).lineLimit(1).truncationMode(.tail)
                }
            }
            Spacer(minLength: 0)
            HStack(spacing: 8) {
                if row.canRetry {
                    Button("重试") { Task { await model.retryRelay(id: row.id) } }.secondaryAction(compact: true)
                }
                Button("编辑") { model.beginEditRelay(id: row.id) }.secondaryAction(compact: true).disabled(!row.canEdit)
                    .focusable(row.canEdit)
                    .focused(focus, equals: .edit(row.id))
                Button("删除") { model.requestDeleteRelay(id: row.id) }.secondaryAction(compact: true).disabled(!row.canDelete)
                    .focusable(row.canDelete)
                    .focused(focus, equals: .delete(row.id))
            }
        }
        .padding(.vertical, 8)
    }

    @ViewBuilder
    private var status: some View {
        HStack(spacing: 4) {
            if busy {
                Spinner()
            } else if row.canRetry {
                Image(systemName: "exclamationmark.triangle.fill").foregroundColor(Palette.warning)
            } else if model.forwardingEnabled {
                Image(systemName: "checkmark.circle.fill").foregroundColor(Palette.success)
            }
            Text(row.text).foregroundColor(row.canRetry ? Palette.warning : (busy || !model.forwardingEnabled ? Palette.textMuted : Palette.success))
        }
        .font(Fonts.caption)
    }
}

struct StatusFooter: View {
    @ObservedObject var model: AppModel

    private var chatGPTFound: Bool { model.status?.chatgpt?.found != false }
    private var chatGPTValue: String {
        if model.chatGPTUnsupported { return "版本暂不支持" }
        return chatGPTFound ? "已检测到" : "未找到"
    }
    private var modelsFetched: Bool { model.forwardingEnabled && model.relayRows.contains { !$0.canRetry && $0.text.hasSuffix("个模型") } }

    var body: some View {
        HStack(spacing: 16) {
            Text("v\(model.status?.version ?? "—")").font(Fonts.mono).foregroundColor(Palette.textMuted).lineLimit(1).fixedSize(horizontal: true, vertical: false)
            HStack(spacing: 8) {
                Circle().fill(model.coreRunning ? Palette.success : Palette.borderStrong).frame(width: 8, height: 8)
                Text(model.runtimeText).font(Fonts.caption).foregroundColor(Palette.textMuted).lineLimit(1).fixedSize(horizontal: true, vertical: false)
            }
            item("ChatGPT", chatGPTValue, note: nil, good: chatGPTFound && !model.chatGPTUnsupported)
            item("中转模型", model.modelCountValue, note: model.modelCountNote, good: modelsFetched)
            Spacer(minLength: 0)
            if let update = model.availableUpdate {
                Button {
                    Task { await model.openUpdate() }
                } label: {
                    Text("新版本 \(update.version)").lineLimit(1).fixedSize(horizontal: true, vertical: false)
                }
                .linkAction()
                .accessibilityLabel("发现新版本 \(update.version)，\(update.installer ? "下载安装包" : "打开下载页")")
            } else {
                Button("检查更新") { Task { await model.checkUpdate() } }.linkAction()
            }
        }
        .padding(.horizontal, 24)
        .frame(minHeight: 48)
        .background(Palette.surface)
        .overlay(alignment: .top) { Rectangle().fill(Palette.border).frame(height: 1) }
    }

    private func item(_ label: String, _ value: String, note: String?, good: Bool) -> some View {
        HStack(spacing: 4) {
            Image(systemName: good ? "checkmark.circle.fill" : "minus.circle")
                .font(.system(size: 16))
                .foregroundColor(good ? Palette.success : Palette.textMuted)
            LabelValue(label: label, value: value, note: note, font: Fonts.caption)
        }
        .lineLimit(1)
        .fixedSize(horizontal: true, vertical: false)
    }
}
