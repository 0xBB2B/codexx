import AppKit
import Combine
import SwiftUI

private let fallbackBundleID = "io.github.modelbridge.app"

private final class StartupConnection: @unchecked Sendable {
    private let lock = NSLock()
    private var value: StoredConnection?

    func set(_ connection: StoredConnection) {
        lock.lock()
        value = connection
        lock.unlock()
    }

    func take() -> StoredConnection? {
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
    private var mainWindow: NSWindow?
    private var setupWindow: NSWindow?
    private var subscriptions: Set<AnyCancellable> = []
    private var activationObserver: NSObjectProtocol?
    private var setupCloseObserver: NSObjectProtocol?
    private let startup = StartupConnection()

    override init() {
        let settings = settings
        let keychain = keychain
        let startup = startup
        core = CoreProcess(
            executableURL: CoreProcess.executableURL(appBundleURL: Bundle.main.bundleURL),
            arguments: CoreProcess.arguments(dataDir: CoreProcess.defaultDataDirectory),
            initParams: {
                let stored = startup.take() ?? StoredConnection.load(settings: settings, keychain: keychain)
                return CoreInitParams(baseURL: stored.baseURL, apiKey: (try? stored.apiKey.get()) ?? nil, chatgptDir: nil)
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
        observeModel()

        let launchedAtLogin = loginItem.wasLaunchedAtLogin
        let settings = settings
        let keychain = keychain
        Task {
            let stored = await Task.detached { StoredConnection.load(settings: settings, keychain: keychain) }.value
            startup.set(stored)
            let hasBaseURL = stored.baseURL != nil
            if !hasBaseURL { model.showSetup = true }
            do {
                try await core.start()
            } catch {
                model.coreDidFail()
            }
            await model.start(stored: stored)
            await model.recheckStatus()
            if hasBaseURL && !launchedAtLogin { showMainWindow() }
        }
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }

    func showMainWindow() {
        if model.needsSetup {
            if model.showSetup, let setupWindow { present(setupWindow) } else { model.showSetup = true }
            return
        }
        let window = mainWindow ?? makeWindow(title: "模型接入助手", content: MainWindow(model: model))
        mainWindow = window
        present(window)
    }

    private func observeModel() {
        model.$showSetup.removeDuplicates().sink { [weak self] visible in
            guard let self else { return }
            if visible {
                if setupWindow == nil {
                    let window = makeWindow(title: "设置模型接入助手", content: SetupWindow(model: model))
                    setupWindow = window
                    setupCloseObserver = NotificationCenter.default.addObserver(
                        forName: NSWindow.willCloseNotification, object: window, queue: .main
                    ) { [weak self] _ in
                        Task { @MainActor in self?.model.finishSetup() }
                    }
                }
                if let setupWindow { present(setupWindow) }
            } else {
                setupWindow?.orderOut(nil)
            }
        }.store(in: &subscriptions)
        model.$mainWindowRequested.filter { $0 }.sink { [weak self] _ in
            self?.model.mainWindowRequested = false
            self?.showMainWindow()
        }.store(in: &subscriptions)
        model.$notice.compactMap { $0 }.sink { [weak self] _ in
            guard let self, !model.showSetup else { return }
            showMainWindow()
        }.store(in: &subscriptions)
    }

    private func makeWindow<Content: View>(title: String, content: Content) -> NSWindow {
        let window = NSWindow(contentViewController: NSHostingController(rootView: content.preferredColorScheme(.light)))
        window.title = title
        window.styleMask = [.titled, .closable, .miniaturizable]
        window.isReleasedWhenClosed = false
        window.center()
        return window
    }

    private func present(_ window: NSWindow) {
        NSApp.activate(ignoringOtherApps: true)
        window.makeKeyAndOrderFront(nil)
    }
}

@main
struct ModelBridgeApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate

    var body: some Scene {
        MenuBarExtra("模型接入助手", systemImage: "arrow.left.arrow.right.circle") {
            MenuPanel(model: delegate.model) { delegate.showMainWindow() }
        }
        .menuBarExtraStyle(.window)
    }
}
