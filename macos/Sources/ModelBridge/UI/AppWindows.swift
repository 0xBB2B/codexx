import AppKit
import Combine
import SwiftUI

struct WindowDragArea: NSViewRepresentable {
    private final class DragView: NSView {
        override func mouseDown(with event: NSEvent) { window?.performDrag(with: event) }
    }

    func makeNSView(context: Context) -> NSView { DragView() }
    func updateNSView(_ nsView: NSView, context: Context) {}
}

@MainActor
enum AppWindows {
    static let titleBarHeight: CGFloat = 48

    static func makeMain(model: AppModel) -> NSWindow {
        let content = MainWindow(model: model).preferredColorScheme(.light).ignoresSafeArea(edges: .top)
        let host = NSHostingView(rootView: content)
        if #available(macOS 13.3, *) { host.safeAreaRegions = [] }
        let window = MainShellWindow(
            contentRect: NSRect(origin: .zero, size: host.fittingSize),
            styleMask: [.titled, .closable, .miniaturizable, .fullSizeContentView],
            backing: .buffered, defer: false
        )
        window.contentView = host
        window.title = "模型接入助手"
        window.titlebarAppearsTransparent = true
        window.titleVisibility = .hidden
        window.isReleasedWhenClosed = false
        window.center()
        window.centerTrafficLights()
        return window
    }
}

private final class MainShellWindow: NSWindow {
    override func update() {
        super.update()
        centerTrafficLights()
    }

    override func layoutIfNeeded() {
        super.layoutIfNeeded()
        centerTrafficLights()
    }

    // 系统刷新标题栏时会把按钮放回默认高度，所以每次更新都要重新摆；
    // 目标是顶栏中线（距窗口顶 24px），但按钮不能出系统标题栏的范围，否则点不到
    func centerTrafficLights() {
        for kind in [NSWindow.ButtonType.closeButton, .miniaturizeButton, .zoomButton] {
            guard let button = standardWindowButton(kind), let bar = button.superview else { continue }
            let centerY = bar.convert(NSPoint(x: 0, y: frame.height - AppWindows.titleBarHeight / 2), from: nil).y
            let y = min(max(0, centerY - button.frame.height / 2), bar.bounds.height - button.frame.height)
            if button.frame.origin.y != y { button.setFrameOrigin(NSPoint(x: button.frame.origin.x, y: y)) }
        }
    }
}

@MainActor
final class RelaySheetController {
    private enum Kind { case form, delete }

    private static var ownerKey = 0
    private let model: AppModel
    private weak var window: NSWindow?
    private var sheet: NSWindow?
    private var kind: Kind?
    private var subscriptions: Set<AnyCancellable> = []

    init(model: AppModel, window: NSWindow) {
        self.model = model
        self.window = window
        objc_setAssociatedObject(window, &Self.ownerKey, self, .OBJC_ASSOCIATION_RETAIN_NONATOMIC)
        model.$showRelayForm.sink { [weak self] visible in
            guard let self else { return }
            if visible { open(.form) } else { close(.form) }
        }.store(in: &subscriptions)
        model.$pendingConfirmation.map { $0?.kind.isDeleteRelay ?? false }.removeDuplicates().sink { [weak self] visible in
            guard let self else { return }
            if visible { open(.delete) } else { close(.delete) }
        }.store(in: &subscriptions)
    }

    private func open(_ newKind: Kind) {
        guard sheet == nil, let window else { return }
        let panel: NSWindow = switch newKind {
        case .form: DialogPanel.make(RelayFormView(model: model))
        case .delete: DialogPanel.make(DeleteRelayDialogView(model: model))
        }
        sheet = panel
        kind = newKind
        NSApp.activate(ignoringOtherApps: true)
        window.beginSheet(panel) { [weak self] _ in
            guard let self, sheet === panel else { return }
            sheet = nil
            kind = nil
            if newKind == .form { model.cancelRelayForm() } else { model.cancelPending() }
        }
    }

    private func close(_ closing: Kind) {
        guard kind == closing, let sheet, let window else { return }
        self.sheet = nil
        kind = nil
        window.endSheet(sheet)
        sheet.orderOut(nil)
    }
}
