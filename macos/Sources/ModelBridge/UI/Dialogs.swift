import AppKit
import Combine
import SwiftUI

private final class DialogPanel: NSPanel {
    override var canBecomeKey: Bool { true }
}

private final class DialogResult {
    var action: DialogAction
    init(_ action: DialogAction) { self.action = action }
}

private struct DialogView: View {
    let model: DialogContent
    let onAction: (DialogAction) -> Void
    @FocusState private var focusedIndex: Int?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                Image(systemName: "exclamationmark.triangle").font(.system(size: 16)).foregroundColor(Palette.primary)
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
                    Button(button.label) { onAction(button.action) }
                        .buttonStyle(AppButtonStyle(kind: button.style == .primary ? .primary : .secondary))
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
            Button("") { onAction(model.escapeAction) }
                .keyboardShortcut(.cancelAction)
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
        model.$pendingConfirmation.compactMap { $0 }.sink { [weak self] value in
            Task { @MainActor in await self?.present(value) }
        }.store(in: &subscriptions)
        model.onTrashFailure = { [weak self] path in
            _ = self?.run(InfoDialogModel.trashFailure(path: path))
        }
        model.$uninstallPhase.sink { [weak self] phase in
            Task { @MainActor in await self?.present(phase) }
        }.store(in: &subscriptions)
    }

    private func present(_ confirmation: Confirmation) async {
        guard !presenting else { return }
        presenting = true
        let confirmed = run(ConfirmDialogModel(confirmation: confirmation)) == .confirm
        presenting = false
        if confirmed { await model.confirmPending() } else { model.cancelPending() }
    }

    private func present(_ phase: UninstallPhase) async {
        switch phase {
        case .paused(let keys, let configPath):
            guard !presenting else { return }
            presenting = true
            while true {
                let choice = run(UninstallPausedDialogModel(keys: keys, configPath: configPath))
                if choice == .revealFolder {
                    MainWindow.reveal(configPath)
                    continue
                }
                presenting = false
                if choice == .recheck { await model.recheckUninstall() } else { model.cancelUninstall() }
                return
            }
        case .finished:
            guard !presenting else { return }
            presenting = true
            let running = model.isChatGPTRunning
            let response = run(InfoDialogModel.uninstallFinished(leftovers: model.uninstallLeftovers, chatGPTRunning: running))
            presenting = false
            if running, response == .restartChatGPT { await model.restartChatGPT() }
            await model.finishUninstall()
        case .idle, .running:
            break
        }
    }

    private func run(_ content: DialogContent) -> DialogAction {
        let result = DialogResult(content.escapeAction)
        let panel = DialogPanel(contentRect: .zero, styleMask: [.borderless], backing: .buffered, defer: false)
        let host = NSHostingView(rootView: DialogView(model: content) { action in
            result.action = action
            NSApp.stopModal()
        })
        panel.contentView = host
        panel.setContentSize(host.fittingSize)
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = false
        panel.isReleasedWhenClosed = false
        panel.center()
        NSApp.activate(ignoringOtherApps: true)
        NSApp.runModal(for: panel)
        panel.orderOut(nil)
        return result.action
    }
}
