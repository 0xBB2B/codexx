import Foundation

enum SetupField: Hashable, Sendable {
    case baseURL
    case apiKey
}

enum SetupPhase: Equatable, Sendable {
    case idle
    case connecting
    case failed(title: String, detail: String)
    case succeeded(title: String, modelCount: Int)
}

enum UninstallPhase: Equatable, Sendable {
    case idle
    case running
    case paused(keys: [String], configPath: String?)
    case finished
}

struct CoreFailure: Equatable, Sendable {
    let code: String
    let keys: [String]

    init(code: String, keys: [String] = []) {
        self.code = code
        self.keys = keys
    }

    init(_ error: Error) {
        if case CoreError.remote(let code, let keys) = error {
            self.init(code: code, keys: keys)
        } else {
            self.init(code: "internal_error")
        }
    }

    var message: String {
        CoreProtocol.errorMessages[code] ?? CoreProtocol.errorMessages["internal_error"] ?? code
    }
}

struct Notice: Equatable, Sendable {
    enum Kind: Sendable { case success, error, warning, info }

    let title: String
    var detail: String?
    var kind: Kind = .info
}

struct Confirmation: Equatable, Sendable {
    enum Kind: Equatable, Sendable {
        case toggleForwarding(enabled: Bool)
        case restartAfterSave
        case loginStartOff
        case quit
        case uninstall
    }

    let kind: Kind
    let title: String
    let message: String
    var cancelLabel = "取消"
    let confirmLabel: String
}

struct Banner: Equatable, Sendable {
    enum Kind: Sendable { case info, warning, error }
    enum Action: Sendable { case refreshModels, recheckStatus, restoreHelp }

    let title: String
    var detail: String?
    let kind: Kind
    var keys: [String] = []
    var configPath: String?
    var action: Action?
}

struct StoredConnection: Sendable {
    var baseURL: String?
    var apiKey: Result<String?, Error>

    static func load(settings: any SettingsStoring, keychain: any KeyStoring) -> StoredConnection {
        StoredConnection(
            baseURL: (try? settings.loadBaseURL()) ?? nil,
            apiKey: Result { try keychain.load() }
        )
    }
}

private enum StorageFailure: Error {
    case keychain
    case settings

    var message: String {
        switch self {
        case .keychain: "无法写入系统钥匙串。"
        case .settings: "无法写入设置文件。"
        }
    }
}

struct UpdateInfo: Equatable, Sendable {
    let version: String
    let url: String
}

private struct Empty: Encodable, Sendable {}
private struct SaveParams: Encodable, Sendable {
    let baseURL: String
    let apiKey: String

    enum CodingKeys: String, CodingKey {
        case baseURL = "base_url"
        case apiKey = "api_key"
    }
}
private struct ForwardingParams: Encodable, Sendable { let enabled: Bool }
private struct ShutdownParams: Encodable, Sendable { let restore: Bool }
private struct UpdateParams: Encodable, Sendable { let manual: Bool }
private struct SaveResult: Decodable { let modelsChanged: Bool; let relayModelCount: Int }
private struct UpdateResult: Decodable { let latestVersion: String?; let url: String?; let isNewer: Bool }

private extension JSONDecoder {
    static var snakeCase: JSONDecoder {
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        return decoder
    }
}

@MainActor
final class AppModel: ObservableObject {
    @Published var baseURL = ""
    @Published var apiKey = ""
    @Published var status: CoreStatus?
    @Published var setupFieldErrors: [SetupField: String] = [:]
    @Published var setupPhase: SetupPhase = .idle
    @Published var showSetup = false
    @Published var pendingConfirmation: Confirmation?
    @Published var forwardingFailure: CoreFailure?
    @Published var notice: Notice?
    @Published var uninstallPhase: UninstallPhase = .idle
    @Published var availableUpdate: UpdateInfo?
    @Published var loginStartEnabled = false
    @Published var mainWindowRequested = false
    @Published var isSaving = false
    @Published var isSwitchingForwarding = false

    @Published var coreRunning = true
    @Published var setupFollowUpIssues: [String] = []
    @Published var uninstallLeftovers: [String] = []
    @Published var trashFailurePath: String?

    var onTerminate: @MainActor () -> Void = {}
    var onTrashFailure: @MainActor (String) -> Void = { _ in }

    private let core: any CoreClient
    private let keychain: any KeyStoring
    private let settings: any SettingsStoring
    private let loginItem: any LoginItemControlling
    private let chatGPT: any ChatGPTControlling
    private let trash: any AppTrashing
    private let appURL: URL
    private var savedBaseURL: String?
    private var savedKey: String?
    private var switchTarget = false

    init(
        core: any CoreClient,
        keychain: any KeyStoring,
        settings: any SettingsStoring,
        loginItem: any LoginItemControlling,
        chatGPT: any ChatGPTControlling,
        trash: any AppTrashing = WorkspaceTrash(),
        appURL: URL = Bundle.main.bundleURL
    ) {
        self.core = core
        self.keychain = keychain
        self.settings = settings
        self.loginItem = loginItem
        self.chatGPT = chatGPT
        self.trash = trash
        self.appURL = appURL
    }

    var forwardingEnabled: Bool { status?.forwarding ?? false }

    var needsSetup: Bool { savedBaseURL == nil }

    var isChatGPTRunning: Bool { chatGPT.isRunning() }

    var runtimeText: String { coreRunning ? "后台运行中" : "后台服务已停止" }

    var menuStatusLine: String {
        coreRunning ? (forwardingEnabled ? "转发已开启" : "转发已关闭") + " · 后台运行中" : "后台服务已停止"
    }

    var forwardingToggleDisabled: Bool {
        !coreRunning || status?.chatgpt?.found == false || isSwitchingForwarding
    }

    var switchingMessage: String? {
        guard isSwitchingForwarding else { return nil }
        return switchTarget ? "正在打开转发并重启 ChatGPT" : "正在还原 Codex 配置并重启 ChatGPT"
    }

    var modelCountText: String {
        guard forwardingEnabled else { return "转发关闭时不获取" }
        let count = status?.relay?.modelCount ?? 0
        return count > 0 ? "已获取 \(count) 个" : "暂无，Codex 里只显示官方模型"
    }

    var banners: [Banner] {
        var result: [Banner] = []
        if let status {
            if !(status.takenOver ?? []).isEmpty {
                result.append(Banner(
                    title: "已接管 Codex 原有的接入设置",
                    detail: "开启转发前，Codex 配置里已有别的「官方服务地址」设置。原值已备份，关闭转发或卸载时会还原。",
                    kind: .info
                ))
            }
            let relay = status.relay
            if relay?.stale == true {
                result.append(Banner(
                    title: "中台模型列表本次没有更新",
                    detail: relay?.error.flatMap { CoreProtocol.errorMessages[$0] } ?? "Codex 里继续使用上次获取的列表。",
                    kind: .warning,
                    action: .refreshModels
                ))
            } else if relay?.error != nil, (relay?.modelCount ?? 0) == 0 {
                result.append(Banner(
                    title: "还没有获取到中台模型",
                    detail: relay?.error.flatMap { CoreProtocol.errorMessages[$0] },
                    kind: .error,
                    action: .refreshModels
                ))
            }
            if let blocked = status.restoreBlocked, !blocked.isEmpty {
                result.append(Banner(
                    title: "没能自动还原 Codex 配置",
                    detail: "有人手动改过本助手写入的设置项。为了不覆盖这些修改，助手没有改动它。请手动检查配置文件，处理后点「重新检查」。",
                    kind: .error,
                    keys: blocked,
                    configPath: status.configPath,
                    action: .restoreHelp
                ))
            }
            if let error = status.proxy?.error {
                result.append(Banner(
                    title: "本机转发服务没能启动",
                    detail: CoreProtocol.errorMessages[error],
                    kind: .error,
                    action: .refreshModels
                ))
            }
            if status.chatgpt?.found == false {
                result.append(Banner(
                    title: "没有找到 ChatGPT",
                    detail: "请先安装官方 ChatGPT 桌面版，再回来打开转发。如果已经安装，可能是当前 ChatGPT 版本暂不支持，请等待助手新版本。",
                    kind: .error,
                    action: .recheckStatus
                ))
            }
        }
        if let failure = forwardingFailure {
            result.append(Banner(
                title: failure.message,
                kind: .error,
                keys: failure.keys,
                configPath: status?.configPath,
                action: failure.code == "restore_blocked" ? .restoreHelp : nil
            ))
        }
        return result
    }

    func start(stored: StoredConnection? = nil) async {
        let stored = stored ?? StoredConnection.load(settings: settings, keychain: keychain)
        if let url = stored.baseURL {
            baseURL = url
            savedBaseURL = url
        }
        switch stored.apiKey {
        case .success(let key?):
            apiKey = key
            savedKey = key
        case .success(nil):
            break
        case .failure(KeychainError.accessDenied):
            notice = Notice(title: "无法读取已保存的 API Key，请重新输入", kind: .warning)
            mainWindowRequested = true
        case .failure:
            break
        }

        let statuses = core.statusEvents()
        let lifecycle = core.lifecycleEvents()
        Task { [weak self] in
            for await value in statuses { self?.apply(value) }
        }
        Task { [weak self] in
            for await event in lifecycle {
                switch event {
                case .failed: self?.coreDidFail()
                case .restarted: self?.coreRunning = true
                }
            }
        }
    }

    func coreDidFail() {
        coreRunning = false
        notice = Notice(title: "后台服务没能启动，请退出后重新打开", kind: .error)
    }

    func completeSetup() async {
        if setupPhase == .connecting { return }
        guard validateInputs() else { return }
        setupPhase = .connecting
        setupFollowUpIssues = []
        let saved: SaveResult
        do {
            saved = try await saveToCore()
        } catch {
            setupPhase = .failed(title: "没能连上中台", detail: CoreFailure(error).message)
            return
        }
        do {
            try persistConnection()
        } catch {
            await rollbackCore()
            setupPhase = .failed(title: "没有保存", detail: (error as? StorageFailure)?.message ?? "")
            return
        }
        var issues: [String] = []
        var forwardingOn = true
        do {
            let data = try await core.call("set_forwarding", params: ForwardingParams(enabled: true))
            if let value = try? JSONDecoder.snakeCase.decode(CoreStatus.self, from: data) { apply(value) }
        } catch {
            forwardingOn = false
            issues.append(CoreFailure(error).message)
        }
        do {
            try loginItem.enable()
            loginStartEnabled = true
        } catch {
            issues.append("没能开启登录时启动")
        }
        setupFollowUpIssues = issues
        setupPhase = .succeeded(
            title: forwardingOn ? "设置完成，转发已开启" : "设置完成，但转发没能开启",
            modelCount: saved.relayModelCount
        )
    }

    func finishSetup() {
        showSetup = false
        setupPhase = .idle
    }

    func toggleForwarding(enabled: Bool) async {
        if chatGPT.isRunning() {
            pendingConfirmation = Confirmation(
                kind: .toggleForwarding(enabled: enabled),
                title: enabled ? "打开转发并重启 ChatGPT？" : "关闭转发并重启 ChatGPT？",
                message: (enabled
                    ? "打开后，Codex 的模型列表里会多出中台模型。"
                    : "关闭后会把 Codex 配置还原成开启前的样子，Codex 里只剩官方模型。")
                    + "\n\nChatGPT 正在运行，需要重启才能生效。重启会中断 Codex 里正在进行的任务。",
                confirmLabel: enabled ? "打开并重启" : "关闭并重启"
            )
        } else {
            await setForwarding(enabled, thenRestart: false)
        }
    }

    func saveConnection() async {
        if isSaving { return }
        guard validateInputs() else { return }
        isSaving = true
        defer { isSaving = false }
        let saved: SaveResult
        do {
            saved = try await saveToCore()
        } catch {
            notice = Notice(title: "没有保存", detail: CoreFailure(error).message, kind: .error)
            return
        }
        do {
            try persistConnection()
        } catch {
            await rollbackCore()
            notice = Notice(title: "没有保存", detail: (error as? StorageFailure)?.message, kind: .error)
            return
        }
        if saved.modelsChanged, chatGPT.isRunning() {
            pendingConfirmation = Confirmation(
                kind: .restartAfterSave,
                title: "已保存，重启 ChatGPT 以更新模型列表？",
                message: "已重新获取中台模型，现在可用 \(saved.relayModelCount) 个。\n\nChatGPT 正在运行，需要重启后 Codex 里才会显示新的列表。重启会中断 Codex 里正在进行的任务。",
                cancelLabel: "稍后",
                confirmLabel: "立即重启"
            )
        } else {
            notice = Notice(
                title: "已保存，不需要重启 ChatGPT",
                detail: "已重新获取中台模型，不需要重启 ChatGPT。",
                kind: .success
            )
        }
    }

    func setLoginStart(_ enabled: Bool) async {
        if !enabled, forwardingEnabled {
            pendingConfirmation = Confirmation(
                kind: .loginStartOff,
                title: "关闭「登录时启动」？",
                message: "关闭后，下次登录电脑时助手不会自动运行。在你手动打开助手之前，Codex 里的官方模型和中台模型都无法使用，因为转发仍然开着。如果以后不打算常驻，建议先关闭转发。",
                confirmLabel: "仍然关闭"
            )
            return
        }
        applyLoginStart(enabled)
    }

    func quit() async {
        if forwardingEnabled {
            pendingConfirmation = Confirmation(
                kind: .quit,
                title: "退出模型接入助手？",
                message: "退出前会关闭转发，并把 Codex 配置还原成开启前的样子。"
                    + (chatGPT.isRunning() ? "ChatGPT 正在运行，请在退出后重启 ChatGPT。" : ""),
                confirmLabel: "还原并退出"
            )
        } else {
            await shutdown(restore: false)
        }
    }

    func uninstall() async {
        pendingConfirmation = Confirmation(
            kind: .uninstall,
            title: "卸载模型接入助手？",
            message: "会还原 Codex 配置，删除钥匙串里保存的 API Key、助手的设置文件，并取消登录时启动。不会删除 ChatGPT 和 Codex 自己的文件。",
            confirmLabel: "卸载"
        )
    }

    func finishUninstall() async {
        if await trash.moveToTrash(appURL) != nil {
            trashFailurePath = appURL.path
            onTrashFailure(appURL.path)
        }
        onTerminate()
    }

    func recheckUninstall() async { await runUninstall() }

    func cancelUninstall() { uninstallPhase = .idle }

    func checkUpdate() async {
        do {
            let data = try await core.call("check_update", params: UpdateParams(manual: true))
            let result = try JSONDecoder.snakeCase.decode(UpdateResult.self, from: data)
            if result.isNewer, let version = result.latestVersion, let url = result.url {
                availableUpdate = UpdateInfo(version: version, url: url)
            } else {
                availableUpdate = nil
                notice = Notice(title: "已是最新版本", kind: .success)
            }
        } catch {
            notice = Notice(title: "暂时无法检查更新，请稍后再试", kind: .warning)
        }
    }

    func refreshModels() async { await refresh("refresh_models") }

    func recheckStatus() async {
        forwardingFailure = nil
        await refresh("status")
    }

    func restartChatGPT() async {
        let result = await chatGPT.restart()
        switch result {
        case .timedOut: notice = Notice(title: "ChatGPT 没有退出", detail: "请手动退出 ChatGPT 后重新打开。", kind: .warning)
        case .openFailed: notice = Notice(title: "没能打开 ChatGPT，请手动打开", kind: .warning)
        case .notFound: notice = Notice(title: "没有找到 ChatGPT", kind: .error)
        case .restarted, .cancelled: break
        }
    }

    func cancelPending() { pendingConfirmation = nil }

    func confirmPending() async {
        guard let pending = pendingConfirmation else { return }
        pendingConfirmation = nil
        switch pending.kind {
        case .toggleForwarding(let enabled): await setForwarding(enabled, thenRestart: true)
        case .restartAfterSave: await restartChatGPT()
        case .loginStartOff: applyLoginStart(false)
        case .quit: await shutdown(restore: true)
        case .uninstall: await runUninstall()
        }
    }

    func dismissNotice() { notice = nil }

    private func setForwarding(_ enabled: Bool, thenRestart: Bool) async {
        isSwitchingForwarding = thenRestart
        switchTarget = enabled
        defer { isSwitchingForwarding = false }
        forwardingFailure = nil
        do {
            let data = try await core.call("set_forwarding", params: ForwardingParams(enabled: enabled))
            if let value = try? JSONDecoder.snakeCase.decode(CoreStatus.self, from: data) { apply(value) }
        } catch {
            recordFailure(CoreFailure(error))
            return
        }
        if thenRestart { await restartChatGPT() }
    }

    private func runUninstall() async {
        uninstallPhase = .running
        uninstallLeftovers = []
        do {
            _ = try await core.call("prepare_uninstall", params: Empty())
        } catch CoreError.remote(let code, let keys) where code == "restore_blocked" {
            uninstallPhase = .paused(keys: keys, configPath: status?.configPath)
            return
        } catch {
            uninstallPhase = .idle
            notice = Notice(title: "卸载没有完成", detail: CoreFailure(error).message, kind: .error)
            return
        }
        var leftovers: [String] = []
        do { try keychain.delete() } catch { leftovers.append("钥匙串里的 API Key") }
        do { try settings.deleteAll() } catch { leftovers.append("助手的设置文件") }
        do { try loginItem.disable() } catch { leftovers.append("登录时启动") }
        uninstallLeftovers = leftovers
        uninstallPhase = .finished
    }

    private func shutdown(restore: Bool) async {
        do {
            _ = try await core.call("shutdown", params: ShutdownParams(restore: restore))
        } catch CoreError.remote(let code, let keys) {
            if code == "restore_blocked" { coreRunning = false }
            recordFailure(CoreFailure(code: code, keys: keys))
            return
        } catch {}
        coreRunning = false
        onTerminate()
    }

    private func recordFailure(_ failure: CoreFailure) {
        forwardingFailure = failure
        mainWindowRequested = true
    }

    private func rollbackCore() async {
        guard let url = savedBaseURL, let key = savedKey else { return }
        _ = try? await core.call("save_connection", params: SaveParams(baseURL: url, apiKey: key))
    }

    private func refresh(_ method: String) async {
        guard let data = try? await core.call(method, params: Empty()),
              let value = try? JSONDecoder.snakeCase.decode(CoreStatus.self, from: data) else { return }
        apply(value)
    }

    private func applyLoginStart(_ enabled: Bool) {
        do {
            if enabled { try loginItem.enable() } else { try loginItem.disable() }
            loginStartEnabled = enabled
        } catch {
            notice = Notice(title: "没能修改「登录时启动」", kind: .error)
        }
    }

    private func apply(_ value: CoreStatus) {
        status = value
        if let version = value.update?.latestVersion, let url = value.update?.url {
            availableUpdate = UpdateInfo(version: version, url: url)
        }
    }

    private func validateInputs() -> Bool {
        var errors: [SetupField: String] = [:]
        let url = baseURL.trimmingCharacters(in: .whitespaces)
        if url.isEmpty {
            errors[.baseURL] = "请填写 Base URL。"
        } else if !url.hasPrefix("https://") {
            errors[.baseURL] = CoreProtocol.errorMessages["invalid_base_url"]
        }
        if apiKey.trimmingCharacters(in: .whitespaces).isEmpty { errors[.apiKey] = "请填写 API Key。" }
        setupFieldErrors = errors
        return errors.isEmpty
    }

    private func saveToCore() async throws -> SaveResult {
        let data = try await core.call(
            "save_connection",
            params: SaveParams(baseURL: baseURL.trimmingCharacters(in: .whitespaces), apiKey: apiKey)
        )
        return try JSONDecoder.snakeCase.decode(SaveResult.self, from: data)
    }

    private func persistConnection() throws {
        let url = baseURL.trimmingCharacters(in: .whitespaces)
        do { try keychain.save(apiKey) } catch { throw StorageFailure.keychain }
        do {
            try settings.saveBaseURL(url)
        } catch {
            if let savedKey { try? keychain.save(savedKey) } else { try? keychain.delete() }
            throw StorageFailure.settings
        }
        savedBaseURL = url
        savedKey = apiKey
    }
}
