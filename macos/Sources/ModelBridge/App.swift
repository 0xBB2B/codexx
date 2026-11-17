import AppKit
import Combine
import SwiftUI

private let fallbackBundleID = "io.github.modelbridge.app"

private final class StartupConnection: @unchecked Sendable {
    private let lock = NSLock()
    private var value: StoredRelays?

    func set(_ connection: StoredRelays) {
        lock.lock()
        value = connection
        lock.unlock()
    }

    func take() -> StoredRelays? {
        lock.lock()
        defer { lock.unlock() }
        let taken = value
        value = nil
        return taken
    }
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    let model: AppModel
    private let core: CoreProcess
    private let settings = ShellSettings()
    private let keychain = KeychainStore()
    private let loginItem = LoginItem()
    private var dialogs: DialogPresenter?
    private var windows: AppWindowController?
    private var subscriptions: Set<AnyCancellable> = []
    private var activationObserver: NSObjectProtocol?
    private let startup = StartupConnection()

    override init() {
        let settings = settings
        let keychain = keychain
        let startup = startup
        core = CoreProcess(
            executableURL: CoreProcess.executableURL(appBundleURL: Bundle.main.bundleURL),
            arguments: CoreProcess.arguments(dataDir: CoreProcess.defaultDataDirectory),
            initParams: {
                if let stored = startup.take() {
                    return CoreInitParams(relays: stored.initRelays, chatgptDir: nil)
                }
                return CoreInitParams.load(settings: settings, keychain: keychain, chatgptDir: nil)
            }
        )
        model = AppModel(core: core, keychain: keychain, settings: settings, loginItem: loginItem, chatGPT: ChatGPTApp())
        super.init()
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        let instance = SingleInstance(bundleID: Bundle.main.bundleIdentifier ?? fallbackBundleID)
        if instance.shouldTerminate() {
            NSApp.terminate(nil)
            return
        }
        activationObserver = instance.observeActivation { [weak self] in
            Task { @MainActor in self?.showMainWindow() }
        }

        NSApp.appearance = NSAppearance(named: .aqua)
        model.onTerminate = { NSApp.terminate(nil) }
        model.loginStartEnabled = loginItem.isEnabled
        dialogs = DialogPresenter(model: model)
        windows = AppWindowController(model: model) { window in
            NSApp.activate(ignoringOtherApps: true)
            window.makeKeyAndOrderFront(nil)
        }
        observeModel()

        let launchedAtLogin = loginItem.wasLaunchedAtLogin
        let settings = settings
        let keychain = keychain
        Task {
            let stored = await Task.detached { StoredRelays.load(settings: settings, keychain: keychain) }.value
            startup.set(stored)
            let hasRelays = !stored.relays.isEmpty
            if !hasRelays { model.showSetup = true }
            do {
                try await core.start()
            } catch {
                model.coreDidFail()
            }
            await model.start(stored: stored)
            await model.recheckStatus()
            if hasRelays && !launchedAtLogin { showMainWindow() }
        }
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }

    func showMainWindow() { windows?.showMainWindow() }

    private func observeModel() {
        model.$mainWindowRequested.filter { $0 }.sink { [weak self] _ in
            self?.model.mainWindowRequested = false
            self?.showMainWindow()
        }.store(in: &subscriptions)
        model.$notices.map(\.count).scan((0, 0)) { ($0.1, $1) }.filter { $0.1 > $0.0 }.sink { [weak self] _ in
            guard let self, !model.showSetup else { return }
            showMainWindow()
        }.store(in: &subscriptions)
    }
}

func menuBarImage(in directory: URL) -> NSImage? {
    let size = NSSize(width: 18, height: 18)
    let image = NSImage(size: size)
    for name in ["MenuBarIcon.png", "MenuBarIcon@2x.png"] {
        guard let data = try? Data(contentsOf: directory.appendingPathComponent(name)),
              let rep = NSBitmapImageRep(data: data) else { return nil }
        rep.size = size
        image.addRepresentation(rep)
    }
    image.isTemplate = true
    return image
}

@main
struct ModelBridgeApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate

    var body: some Scene {
        MenuBarExtra {
            MenuPanel(model: delegate.model) { delegate.showMainWindow() }
        } label: {
            if let image = Bundle.main.resourceURL.flatMap(menuBarImage(in:)) {
                Image(nsImage: image).accessibilityLabel("模型接入助手")
            } else {
                // swift run 开发时没有打包好的资源，不退回系统符号菜单栏就没有入口
                Image(systemName: "arrow.left.arrow.right.circle").accessibilityLabel("模型接入助手")
            }
        }
        .menuBarExtraStyle(.window)
    }
}
