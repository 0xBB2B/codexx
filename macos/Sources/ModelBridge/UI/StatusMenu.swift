import AppKit
import SwiftUI

struct MenuPanel: View {
    @ObservedObject var model: AppModel
    let openMain: () -> Void
    @State private var hovered: MenuPanelItem.ID?
    @State private var focused: MenuPanelItem.ID?
    @StateObject private var keys = MenuKeyMonitor()

    var body: some View {
        let panel = MenuPanelModel(model: model)
        VStack(spacing: 0) {
            header(panel)
            ForEach(Array(panel.groups.enumerated()), id: \.offset) { _, group in
                Rectangle().fill(Palette.border).frame(height: 1).padding(.vertical, 4).padding(.horizontal, 8)
                ForEach(group) { row($0) }
            }
        }
        .padding(4)
        .frame(width: 286)
        .background(RoundedRectangle(cornerRadius: 12).fill(Palette.surface))
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Palette.border, lineWidth: 1))
        .preferredColorScheme(.light)
        .background(WindowReader { keys.attach($0) })
        .onAppear { keys.handler = handleKey }
        .onDisappear { keys.stop(); focused = nil }
    }

    private func header(_ panel: MenuPanelModel) -> some View {
        HStack(spacing: 8) {
            Image(systemName: "arrow.left.arrow.right.circle").font(.system(size: 16)).foregroundColor(Palette.primary)
            VStack(alignment: .leading, spacing: 0) {
                Text(panel.title).font(Fonts.label).foregroundColor(Palette.text)
                Text(panel.subtitle).font(Fonts.caption).foregroundColor(Palette.textMuted)
            }
            Spacer(minLength: 0)
        }
        .padding(12)
        .accessibilityElement(children: .combine)
    }

    private func row(_ item: MenuPanelItem) -> some View {
        let highlighted = !item.isDisabled && hovered == item.id
        return HStack(spacing: 8) {
            Group {
                if item.checked == true {
                    Image(systemName: "checkmark").font(.system(size: 12, weight: .semibold)).foregroundColor(Palette.primary)
                }
            }
            .frame(width: 16)
            VStack(alignment: .leading, spacing: 0) {
                Text(item.title).font(Fonts.control)
                if let subtitle = item.subtitle {
                    Text(subtitle).font(Fonts.caption).foregroundColor(item.isDisabled ? Palette.textDisabled : Palette.textMuted)
                }
            }
            Spacer(minLength: 0)
            if let shortcut = item.shortcut {
                Text(shortcut).font(Fonts.caption).foregroundColor(item.isDisabled ? Palette.textDisabled : Palette.textMuted)
            }
        }
        .foregroundColor(item.isDisabled ? Palette.textDisabled : Palette.text)
        .padding(.leading, 8)
        .padding(.trailing, 12)
        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 8).fill(highlighted ? Palette.selectedBg : Color.clear))
        .overlay {
            if focused == item.id {
                RoundedRectangle(cornerRadius: 8).strokeBorder(Palette.focus, lineWidth: 2)
            }
        }
        .contentShape(RoundedRectangle(cornerRadius: 8))
        .onHover { inside in
            if inside { hovered = item.id } else if hovered == item.id { hovered = nil }
        }
        .onTapGesture { activate(item) }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isButton)
        .accessibilityValue(item.checked.map { $0 ? "已勾选" : "未勾选" } ?? "")
    }

    private func activate(_ item: MenuPanelItem) {
        guard !item.isDisabled else { return }
        NSApp.keyWindow?.close()
        switch item.id {
        case .forwarding:
            let enable = !model.forwardingEnabled
            Task { await model.toggleForwarding(enabled: enable) }
        case .openConfig:
            openMain()
        case .loginStart:
            let enable = !model.loginStartEnabled
            Task { await model.setLoginStart(enable) }
        case .update:
            if let update = model.availableUpdate {
                if let url = URL(string: update.url) { NSWorkspace.shared.open(url) }
            } else {
                Task { await model.checkUpdate() }
            }
        case .uninstall:
            Task { await model.uninstall() }
        case .quit:
            Task { await model.quit() }
        }
    }

    private func move(by step: Int) {
        let items = MenuPanelModel(model: model).groups.flatMap { $0 }.filter { !$0.isDisabled }
        guard !items.isEmpty else { return }
        guard let current = items.firstIndex(where: { $0.id == focused }) else {
            focused = (step > 0 ? items.first : items.last)?.id
            return
        }
        focused = items[(current + step + items.count) % items.count].id
    }

    private func handleKey(_ action: MenuKeyAction) -> Bool {
        let items = MenuPanelModel(model: model).groups.flatMap { $0 }
        switch action {
        case .moveDown: move(by: 1)
        case .moveUp: move(by: -1)
        case .close: NSApp.keyWindow?.close()
        case .openConfig: items.first { $0.id == .openConfig }.map(activate)
        case .quit: items.first { $0.id == .quit }.map(activate)
        case .activateFocused:
            guard let item = items.first(where: { $0.id == focused }) else { return false }
            activate(item)
        }
        return true
    }
}

private struct WindowReader: NSViewRepresentable {
    let onWindow: (NSWindow?) -> Void

    func makeNSView(context: Context) -> NSView { NSView() }

    func updateNSView(_ view: NSView, context: Context) {
        DispatchQueue.main.async { [weak view] in onWindow(view?.window) }
    }
}

@MainActor
final class MenuKeyMonitor: ObservableObject {
    var handler: (MenuKeyAction) -> Bool = { _ in false }
    private weak var window: NSWindow?
    private var monitor: Any?
    private var observers: [NSObjectProtocol] = []

    func attach(_ window: NSWindow?) {
        guard let window, window !== self.window else { return }
        stop()
        observers.forEach(NotificationCenter.default.removeObserver)
        self.window = window
        observers = [
            NotificationCenter.default.addObserver(forName: NSWindow.didBecomeKeyNotification, object: window, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.start() }
            },
            NotificationCenter.default.addObserver(forName: NSWindow.didResignKeyNotification, object: window, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.stop() }
            },
        ]
        if window.isKeyWindow { start() }
    }

    func stop() {
        if let monitor { NSEvent.removeMonitor(monitor) }
        monitor = nil
    }

    private func start() {
        guard monitor == nil, let window else { return }
        let panelID = ObjectIdentifier(window)
        monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
            guard let eventWindow = event.window, ObjectIdentifier(eventWindow) == panelID,
                  let action = MenuKeyAction(
                      keyCode: event.keyCode,
                      command: event.modifierFlags.contains(.command),
                      key: event.charactersIgnoringModifiers
                  )
            else { return event }
            let handled = MainActor.assumeIsolated { self?.handler(action) ?? false }
            return handled ? nil : event
        }
    }
}
