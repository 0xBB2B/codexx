import AppKit
import Combine
import SwiftUI

final class DialogPanel: NSPanel {
    override var canBecomeKey: Bool { true }

    @MainActor
    static func make<Content: View>(_ content: Content) -> DialogPanel {
        let panel = DialogPanel(contentRect: .zero, styleMask: [.borderless], backing: .buffered, defer: false)
        let host = NSHostingView(rootView: content.padding(48))
        panel.contentView = host
        panel.setContentSize(host.fittingSize)
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = false
        panel.isReleasedWhenClosed = false
        return panel
    }
}

struct DeleteRelayDialogView: View {
    @ObservedObject var model: AppModel
    @FocusState private var cancelFocused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                AppIconView(size: 24)
                Text(model.pendingConfirmation?.title ?? "").font(Fonts.sectionTitle).foregroundColor(Palette.text)
            }
            .padding(.top, 24).padding(.horizontal, 24)
            .accessibilityAddTraits(.isHeader)
            Text(model.pendingConfirmation?.message ?? "")
                .font(.system(size: 14))
                .foregroundColor(Palette.text)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.top, 12).padding(.horizontal, 24).padding(.bottom, 24)
            Rectangle().fill(Palette.border).frame(height: 1)
            HStack(spacing: 12) {
                Spacer(minLength: 0)
                Button("取消") { model.cancelPending() }
                    .secondaryAction()
                    .keyboardShortcut(.cancelAction)
                    .disabled(model.deleteCancelDisabled)
                    .focusable()
                    .focused($cancelFocused)
                Button { Task { await model.confirmPending() } } label: {
                    BusyLabel(text: model.deleteBusyLabel ?? "删除", busy: model.isDeletingRelay)
                }
                .primaryAction()
                .disabled(model.isDeletingRelay)
            }
            .padding(.vertical, 16).padding(.horizontal, 24)
        }
        .frame(width: 520)
        .background(RoundedRectangle(cornerRadius: 12).fill(Palette.surface))
        .shadow(color: Color(hex: 0x172b3a).opacity(0.16), radius: 24, x: 0, y: 16)
        .preferredColorScheme(.light)
        .onAppear { DispatchQueue.main.async { cancelFocused = true } }
        .accessibilityElement(children: .contain)
    }
}

extension Confirmation.Kind {
    var isDeleteRelay: Bool {
        if case .deleteRelay = self { return true }
        return false
    }
}

private final class DialogResult {
    var action: DialogAction?
    init(_ action: DialogAction?) { self.action = action }
}

private final class FreshFittingHostingView<Content: View>: NSHostingView<Content> {
    override var fittingSize: NSSize {
        layoutSubtreeIfNeeded()
        return super.fittingSize
    }
}

private struct UninstallDialogHost: View {
    @ObservedObject var model: AppModel
    let onAction: (DialogAction) -> Void
    let onClosed: () -> Void
    let onContentChanged: () -> Void

    private var contentKey: String {
        guard let content = model.uninstallDialogContent else { return "" }
        return content.title + content.message + content.buttons.map(\.label).joined()
    }

    var body: some View {
        Group {
            if let content = model.uninstallDialogContent {
                DialogView(model: content, onAction: onAction)
            }
        }
        .onChange(of: model.uninstallDialogContent == nil) { closed in
            if closed { onClosed() }
        }
        .onChange(of: contentKey) { _ in onContentChanged() }
    }
}

struct DialogView: View {
    let model: DialogContent
    let onAction: (DialogAction) -> Void
    @FocusState private var focusedIndex: Int?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                AppIconView(size: 24)
                Text(model.title).font(Fonts.sectionTitle).foregroundColor(Palette.text)
            }
            .padding(.top, 24).padding(.horizontal, 24)
            .accessibilityAddTraits(.isHeader)
            Group {
                if let alert = model.alert {
                    AlertBar(tone: alert.tone, title: alert.title, detail: alert.message)
                } else {
                    Text(model.message)
                        .font(.system(size: 14))
                        .foregroundColor(Palette.text)
                        .fixedSize(horizontal: false, vertical: true)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            .padding(.top, 12).padding(.horizontal, 24).padding(.bottom, 24)
            Rectangle().fill(Palette.border).frame(height: 1)
            HStack(spacing: 12) {
                Spacer(minLength: 0)
                ForEach(Array(model.buttons.enumerated()), id: \.offset) { index, button in
                    Button {
                        onAction(button.action)
                    } label: {
                        HStack(spacing: 8) {
                            if button.showsSpinner { Spinner() }
                            Text(button.label)
                        }
                    }
                    .buttonStyle(AppButtonStyle(kind: button.style == .primary ? .primary : .secondary))
                    .disabled(!button.isEnabled)
                        .focusable()
                        .focused($focusedIndex, equals: index)
                }
            }
            .padding(.vertical, 16).padding(.horizontal, 24)
        }
        .frame(width: 520)
        .background(RoundedRectangle(cornerRadius: 12).fill(Palette.surface))
        .shadow(color: Color(hex: 0x172b3a).opacity(0.16), radius: 24, x: 0, y: 16)
        .padding(48)
        .background {
            Button("") { if let escape = model.escapeAction { onAction(escape) } }
                .keyboardShortcut(.cancelAction)
                .disabled(model.escapeAction == nil)
                .opacity(0)
                .accessibilityHidden(true)
        }
        .preferredColorScheme(.light)
        .onAppear {
            let index = model.buttons.firstIndex { $0.isDefaultFocus }
            DispatchQueue.main.async { focusedIndex = index }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(model.title)
    }
}

@MainActor
final class DialogPresenter {
    private let model: AppModel
    private var subscriptions: Set<AnyCancellable> = []
    private var presenting = false

    init(model: AppModel) {
        self.model = model
        model.$pendingConfirmation.compactMap { $0 }.filter { !$0.kind.isDeleteRelay && $0.kind != .uninstall }.sink { [weak self] value in
            Task { @MainActor in await self?.present(value) }
        }.store(in: &subscriptions)
        model.$pendingConfirmation.compactMap { $0 }.filter { $0.kind == .uninstall }.sink { [weak self] _ in
            Task { @MainActor in await self?.presentUninstall() }
        }.store(in: &subscriptions)
        model.onTrashFailure = { path in
            _ = Self.run(InfoDialogModel.trashFailure(path: path))
        }
        model.$uninstallPhase.sink { [weak self] phase in
            Task { @MainActor in await self?.present(phase) }
        }.store(in: &subscriptions)
    }

    static func showRestoreHelp(
        keys: [String], configPath: String?, purpose: RestoreHelpDialogModel.Purpose, onRecheck: () async -> Void
    ) async {
        while true {
            let choice = run(RestoreHelpDialogModel(keys: keys, configPath: configPath, purpose: purpose))
            if choice == .revealFolder {
                MainWindow.reveal(configPath)
                continue
            }
            if choice == .recheck { await onRecheck() }
            return
        }
    }

    static func showNoticeDetail(title: String, message: String) {
        _ = run(InfoDialogModel(
            title: title,
            message: message,
            buttons: [DialogButton(label: "知道了", action: .dismiss, style: .primary, isDefaultFocus: true)]
        ))
    }

    private func present(_ confirmation: Confirmation) async {
        guard !presenting else { return }
        presenting = true
        let confirmed = Self.run(ConfirmDialogModel(confirmation: confirmation)) == .confirm
        presenting = false
        if confirmed { await model.confirmPending() } else { model.cancelPending() }
    }

    private func present(_ phase: UninstallPhase) async {
        switch phase {
        case .paused:
            await presentUninstall()
        case .finished:
            guard !presenting else { return }
            presenting = true
            let running = model.isChatGPTRunning
            _ = Self.run(InfoDialogModel.uninstallFinished(leftovers: model.uninstallLeftovers, chatGPTRunning: running))
            presenting = false
            await model.finishUninstall()
        case .idle, .running:
            break
        }
    }

    private func presentUninstall() async {
        guard !presenting, model.uninstallDialogContent != nil else { return }
        presenting = true
        let panel = Self.makeUninstallPanel(
            model: model,
            onAction: { [model] action in
                switch action {
                case .cancel: model.cancelPending()
                case .confirm: Task { await model.confirmPending() }
                case .cancelUninstall: model.cancelUninstall()
                case .revealFolder:
                    if case .paused(_, let path) = model.uninstallPhase { MainWindow.reveal(path) }
                case .recheck: Task { await model.recheckUninstall() }
                case .dismiss: break
                }
            },
            onClosed: { NSApp.stopModal() }
        )
        NSApp.activate(ignoringOtherApps: true)
        NSApp.runModal(for: panel)
        panel.orderOut(nil)
        presenting = false
        if model.uninstallPhase == .finished { await present(.finished) }
    }

    static func makeUninstallPanel(
        model: AppModel, onAction: @escaping (DialogAction) -> Void, onClosed: @escaping () -> Void
    ) -> DialogPanel {
        let panel = DialogPanel(contentRect: .zero, styleMask: [.borderless], backing: .buffered, defer: false)
        let host = FreshFittingHostingView(rootView: UninstallDialogHost(
            model: model, onAction: onAction, onClosed: onClosed,
            onContentChanged: { [weak panel] in
                DispatchQueue.main.async {
                    guard let panel, let content = panel.contentView else { return }
                    let size = content.fittingSize
                    let center = NSPoint(x: panel.frame.midX, y: panel.frame.midY)
                    panel.setFrame(
                        NSRect(x: center.x - size.width / 2, y: center.y - size.height / 2, width: size.width, height: size.height),
                        display: true
                    )
                }
            }
        ))
        configure(panel, host: host)
        return panel
    }

    private static func configure(_ panel: DialogPanel, host: NSView) {
        panel.contentView = host
        panel.setContentSize(host.fittingSize)
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = false
        panel.isReleasedWhenClosed = false
        panel.center()
    }

    fileprivate static func run(_ content: DialogContent) -> DialogAction {
        let result = DialogResult(content.escapeAction)
        let panel = DialogPanel(contentRect: .zero, styleMask: [.borderless], backing: .buffered, defer: false)
        let host = NSHostingView(rootView: DialogView(model: content) { action in
            result.action = action
            NSApp.stopModal()
        })
        configure(panel, host: host)
        NSApp.activate(ignoringOtherApps: true)
        NSApp.runModal(for: panel)
        panel.orderOut(nil)
        return result.action ?? content.escapeAction ?? .dismiss
    }
}
