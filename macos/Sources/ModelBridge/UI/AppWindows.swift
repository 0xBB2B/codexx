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

struct WindowTitleBar: View {
    let title: String

    var body: some View {
        Text(title)
            .font(Fonts.control)
            .foregroundColor(Palette.text)
            .frame(maxWidth: .infinity, minHeight: AppWindows.titleBarHeight)
            .background(Palette.surface)
            .overlay(alignment: .bottom) { Rectangle().fill(Palette.border).frame(height: 1) }
            .overlay(WindowDragArea().accessibilityHidden(true))
    }
}

@MainActor
enum AppWindows {
    static let titleBarHeight: CGFloat = 48

    static func makeMain(model: AppModel) -> NSWindow {
        makeShell(title: "模型接入助手", content: MainWindow(model: model))
    }

    static func makeSetup(model: AppModel) -> NSWindow {
        makeShell(title: "设置模型接入助手", content: SetupWindow(model: model))
    }

    private static func makeShell<Content: View>(title: String, content: Content) -> NSWindow {
        let host = NSHostingView(rootView: content.preferredColorScheme(.light).ignoresSafeArea(edges: .top))
        if #available(macOS 13.3, *) { host.safeAreaRegions = [] }
        let window = MainShellWindow(
            contentRect: NSRect(origin: .zero, size: host.fittingSize),
            styleMask: [.titled, .closable, .miniaturizable, .fullSizeContentView],
            backing: .buffered, defer: false
        )
        window.contentView = host
        window.title = title
        window.titlebarAppearsTransparent = true
        window.titleVisibility = .hidden
        window.isReleasedWhenClosed = false
        window.center()
        window.centerTrafficLights()
        return window
    }
}

@MainActor
final class AppWindowController {
    private let model: AppModel
    private let present: (NSWindow) -> Void
    private(set) var mainWindow: NSWindow?
    private(set) var setupWindow: NSWindow?
    private var setupCloseObserver: NSObjectProtocol?
    private var subscription: AnyCancellable?
    private var connectingSubscription: AnyCancellable?

    init(model: AppModel, present: @escaping (NSWindow) -> Void) {
        self.model = model
        self.present = present
        subscription = model.$showSetup.removeDuplicates().sink { [weak self] visible in
            guard let self else { return }
            if visible {
                showSetupWindow()
            } else {
                setupWindow?.orderOut(nil)
                if !model.needsSetup { showMainWindow() }
            }
        }
    }

    func showMainWindow() {
        if model.needsSetup {
            if model.showSetup, let setupWindow { presentRestored(setupWindow) } else { model.showSetup = true }
            return
        }
        let window = mainWindow ?? AppWindows.makeMain(model: model)
        if mainWindow == nil { _ = RelaySheetController(model: model, window: window) }
        mainWindow = window
        presentRestored(window)
    }

    private func presentRestored(_ window: NSWindow) {
        if window.isMiniaturized { window.deminiaturize(nil) }
        present(window)
    }

    private func showSetupWindow() {
        if setupWindow == nil {
            let window = AppWindows.makeSetup(model: model)
            setupWindow = window
            connectingSubscription = model.$setupPhase.map { $0 == .connecting }.removeDuplicates().sink { [weak window] connecting in
                window?.standardWindowButton(.closeButton)?.isEnabled = !connecting
            }
            setupCloseObserver = NotificationCenter.default.addObserver(
                forName: NSWindow.willCloseNotification, object: window, queue: .main
            ) { [weak self] _ in
                Task { @MainActor in self?.model.finishSetup() }
            }
        }
        if let setupWindow { present(setupWindow) }
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
