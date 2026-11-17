import Foundation

enum RelayField: Hashable, Sendable {
    case name
    case baseURL
    case apiKey
}

enum SetupCopy {
    static let title = "连接中转服务"
    static let subtitle = "填写中转服务提供的 Base URL 和 API Key。设置完成后，Codex 的模型列表里会多出中转模型。"
    static let connecting = "正在连接中转…"
    static let failedTitle = "没能连上中转服务"
}

struct RelayRow: Equatable, Identifiable, Sendable {
    let id: String
    let name: String
    let text: String
    let canRetry: Bool
    let canEdit: Bool
    let canDelete: Bool
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
        let text = CoreProtocol.errorMessages[code] ?? CoreProtocol.errorMessages["internal_error"] ?? code
        return text.replacingOccurrences(of: "<keys[0]>", with: keys.first ?? "")
    }
}

struct Notice: Equatable, Identifiable, Sendable {
    enum Kind: Sendable { case success, error, warning, info }

    let id = UUID()
    let title: String
    var detail: String?
    var kind: Kind = .info
    var detailDialogTitle: String?
}

private struct DraftSnapshot {
    let name: String
    let baseURL: String
    let apiKey: String
}

struct Confirmation: Equatable, Sendable {
    enum Kind: Equatable, Sendable {
        case loginStartOff
        case quit
        case uninstall
        case deleteRelay(id: String)
    }

    let kind: Kind
    let title: String
    let message: String
    var cancelLabel = "取消"
    let confirmLabel: String
}

struct Banner: Hashable, Sendable {
    enum Kind: Sendable { case info, warning, error }
    enum Action: Sendable { case refreshModels, recheckStatus, restoreHelp, configInvalidHelp }

    let title: String
    var detail: String?
    let kind: Kind
    var keys: [String] = []
    var configPath: String?
    var action: Action?
}

private enum StorageFailure: Error {
    case keychain
    case settings

    var message: String {
        switch self {
        case .keychain: "无法写入钥匙串。"
        case .settings: "无法写入设置文件。"
        }
    }
}

struct UpdateInfo: Equatable, Sendable {
    let version: String
    let url: String
}

private struct Empty: Encodable, Sendable {}
private struct SaveRelayParams: Encodable, Sendable {
    let id: String?
    let name: String
    let baseURL: String
    let apiKey: String

    enum CodingKeys: String, CodingKey {
        case id, name
        case baseURL = "base_url"
        case apiKey = "api_key"
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(id, forKey: .id)
        try c.encode(name, forKey: .name)
        try c.encode(baseURL, forKey: .baseURL)
        try c.encode(apiKey, forKey: .apiKey)
    }
}
private struct RelayIDParams: Encodable, Sendable { let id: String }
private struct RefreshParams: Encodable, Sendable {
    let id: String?

    enum CodingKeys: String, CodingKey { case id }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(id, forKey: .id)
    }
}
private struct ForwardingParams: Encodable, Sendable { let enabled: Bool }
private struct ShutdownParams: Encodable, Sendable { let restore: Bool }
private struct UpdateParams: Encodable, Sendable { let manual: Bool }
private struct PersistError: Error {
    static let notRevokedLine = "\n已在本次运行中生效，但没能保存到本机，重启助手后会恢复成原来的样子。"

    let failure: StorageFailure
    let revoked: Bool

    var message: String { failure.message + (revoked ? "" : Self.notRevokedLine) }
}

private struct SaveResult: Decodable { let id: String; let name: String; let modelCount: Int }
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
    @Published private(set) var relays: [RelayConfig] = []
    @Published var draftName = ""
    @Published var draftBaseURL = ""
    @Published var draftAPIKey = ""
    @Published var showRelayForm = false
    @Published private(set) var editingRelayID: String?
    @Published var relayFieldErrors: [RelayField: String] = [:]
    @Published private(set) var relaySaveAlert: DialogAlert?
    @Published private(set) var isSavingRelay = false
    @Published private(set) var isDeletingRelay = false
    @Published private var retryingRelayIDs: Set<String> = []
    @Published var status: CoreStatus?
    @Published var setupPhase: SetupPhase = .idle
    @Published var showSetup = false
    @Published var pendingConfirmation: Confirmation?
    @Published var forwardingFailure: CoreFailure? {
        didSet { pruneDismissedBanners() }
    }
    @Published private(set) var notices: [Notice] = []
    @Published private var dismissedBanners: Set<Banner> = []
    @Published var uninstallPhase: UninstallPhase = .idle
    @Published var availableUpdate: UpdateInfo?
    @Published var loginStartEnabled = false
    @Published var mainWindowRequested = false
    @Published var isSwitchingForwarding = false
    @Published var isDetectingAccess = false
    @Published var isRecheckingStatus = false

    @Published var coreRunning = true
    @Published var setupFollowUpIssues: [String] = []
    @Published var uninstallLeftovers: [String] = []
    @Published var trashFailurePath: String?

    var noticeAutoDismissDelay: TimeInterval = 4
    var onTerminate: @MainActor () -> Void = {}
    var onTrashFailure: @MainActor (String) -> Void = { _ in }

    private let core: any CoreClient
    private let keychain: any KeyStoring
    private let settings: any SettingsStoring
    private let loginItem: any LoginItemControlling
    private let chatGPT: any ChatGPTControlling
    private let trash: any AppTrashing
    private let appURL: URL
    private static let detectFailedTitle = "没能检测 ChatGPT 登录状态"
    private static let keychainDeniedTitle = "无法读取已保存的 API Key，请重新输入"
    private static let settingsBackedUpTitle = "原来的设置读不出来，已备份原文件"
    private static let settingsUnreadableTitle = "原来的设置读不出来"
    private static let updateFailedTitle = "暂时无法检查更新，请稍后再试"
    private var noticeTimers: [UUID: Task<Void, Never>] = [:]
    private var startupAligned = false
    private var forwardingSwitchCount = 0

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

    var forwardingEnabled: Bool { status?.forwardingUsable ?? false }

    static let maxRelays = 10

    var needsSetup: Bool { relays.isEmpty }

    var forwardingTitle: String { "使用中转 · " + (forwardingEnabled ? "已开启" : "已关闭") }

    var relayListCaption: String {
        relays.count >= Self.maxRelays ? "已有 \(Self.maxRelays) 个，达到上限。" : "已有 \(relays.count) 个，最多 \(Self.maxRelays) 个。"
    }

    var canAddRelay: Bool { relays.count < Self.maxRelays }

    var relayFormLocked: Bool { isSavingRelay }

    var relaySaveButtonTitle: String {
        isSavingRelay ? "正在保存并获取模型…" : "保存"
    }

    var deleteBlockedReason: String? { relays.count <= 1 ? CoreProtocol.errorMessages["relay_last"] : nil }

    var deleteBusyLabel: String? { isDeletingRelay ? "正在删除…" : nil }

    var deleteCancelDisabled: Bool { isDeletingRelay }

    var relayRows: [RelayRow] {
        relays.map { relay in
            let entry = status?.relays?.first { $0.id == relay.id }
            let count = entry?.modelCount ?? 0
            let text: String
            var canRetry = false
            if retryingRelayIDs.contains(relay.id) {
                text = "正在获取…"
            } else if !forwardingEnabled {
                text = "转发关闭时不获取"
            } else if entry?.fetchedAt == nil, count == 0 {
                text = "还没获取到模型"
                canRetry = true
            } else if entry?.stale == true {
                text = "没更新，沿用上次的 \(count) 个"
                canRetry = true
            } else {
                text = "\(count) 个模型"
            }
            let idle = !retryingRelayIDs.contains(relay.id)
            return RelayRow(
                id: relay.id, name: relay.name, text: text,
                canRetry: canRetry, canEdit: idle, canDelete: idle && canDeleteRelay(id: relay.id)
            )
        }
    }

    var isChatGPTRunning: Bool { chatGPT.isRunning() }

    var runtimeText: String { coreRunning ? "后台运行中" : "后台服务已停止" }

    var menuStatusLine: String {
        coreRunning ? (forwardingEnabled ? "转发已开启" : "转发已关闭") + " · 后台运行中" : "后台服务已停止"
    }

    var forwardingToggleDisabled: Bool {
        !coreRunning || status?.chatgpt?.found == false || chatGPTUnsupported || isSwitchingForwarding
    }

    var chatGPTUnsupported: Bool { status?.chatgpt?.error == "chatgpt_unsupported" }

    var modelCountText: String { "中转模型 " + modelCountValue + (modelCountNote ?? "") }

    var modelCountValue: String {
        guard forwardingEnabled else { return "转发关闭时不获取" }
        let total = (status?.relays ?? []).reduce(0) { $0 + ($1.modelCount ?? 0) }
        return "\(total) 个"
    }

    var modelCountNote: String? { forwardingEnabled ? "（\(relays.count) 个配置）" : nil }

    var accessModeName: String? {
        guard forwardingEnabled else { return nil }
        switch status?.access?.mode {
        case "relay": return "只用中转"
        case "both": return "官方 + 中转"
        default: return nil
        }
    }

    var accessReasonText: String? {
        switch status?.access?.detect {
        case "logged_in": "· 已检测到 ChatGPT 登录"
        case "not_logged_in": "· 没有检测到 ChatGPT 登录"
        case "failed": "· 沿用上次的方式"
        default: nil
        }
    }

    var accessHelpText: String? {
        switch status?.access?.mode {
        case "relay":
            status?.defaultRelay.map { "不用登录 ChatGPT。只有中转模型，默认选中「\($0) · auto」。" }
                ?? "不用登录 ChatGPT。只有中转模型，没有默认选中的模型。"
        case "both":
            "官方模型在上，中转模型在下；选官方模型时仍访问官方服务。"
        default: nil
        }
    }

    var forwardingHelpText: String {
        if status?.chatgpt?.found == false { return "找到 ChatGPT 后才能打开。" }
        if chatGPTUnsupported { return "当前 ChatGPT 版本暂不支持，暂时不能打开。" }
        return forwardingEnabled
            ? ""
            : "已关闭：Codex 配置已还原为开启前的样子。"
    }

    var setupSucceededText: String? {
        guard case .succeeded(_, let count) = setupPhase else { return nil }
        let head = "已获取 \(count) 个中转模型。"
        let tail = "\n请重启 ChatGPT 后使用。助手会在你登录电脑时自动启动，在菜单栏里常驻。"
        switch status?.access?.mode {
        case "relay":
            return head + "没有检测到 ChatGPT 登录，接入方式为「只用中转」：Codex 不需要登录，模型列表里只有中转模型。" + tail
        case "both":
            return head + "已检测到 ChatGPT 登录，接入方式为「官方 + 中转」。" + tail
        default:
            return head + tail
        }
    }

    var banners: [Banner] { allBanners.filter { !dismissedBanners.contains($0) } }

    private var allBanners: [Banner] {
        var result: [Banner] = []
        if let status {
            if !(status.takenOver ?? []).isEmpty {
                result.append(Banner(
                    title: "已接管 Codex 原有的接入设置",
                    detail: "开启转发前，Codex 配置里已有别的「官方服务地址」设置。原值已备份，关闭转发或卸载时会还原。",
                    kind: .info
                ))
            }
            if let blocked = status.restoreBlocked, !blocked.isEmpty {
                result.append(Banner(
                    title: "没能自动还原 Codex 配置",
                    detail: "有设置项被手动改过。",
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
            if chatGPTUnsupported {
                result.append(Banner(
                    title: "当前 ChatGPT 版本暂不支持",
                    detail: "读不到 ChatGPT 自带的官方模型清单，转发保持关闭。请把 ChatGPT 更新到最新版后点「重新检测」；还是不行的话，请等待助手新版本。",
                    kind: .error,
                    action: .recheckStatus
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
                action: failure.code == "restore_blocked" ? .restoreHelp : (failure.code == "config_invalid" && !failure.keys.isEmpty ? .configInvalidHelp : nil)
            ))
        }
        return result
    }

    func start(stored: StoredRelays? = nil) async {
        let stored = stored ?? StoredRelays.load(settings: settings, keychain: keychain)
        relays = stored.relays
        if let recovery = stored.recovery { addSettingsRecoveryNotice(recovery) }
        let denied = stored.keys.values.contains {
            if case .failure(KeychainError.accessDenied) = $0 { return true }
            return false
        }
        if denied {
            addNotice(Notice(title: Self.keychainDeniedTitle, kind: .warning))
            mainWindowRequested = true
        }

        let statuses = core.statusEvents()
        let lifecycle = core.lifecycleEvents()
        Task { [weak self] in
            for await value in statuses {
                self?.apply(value)
                await self?.alignOnStartup(value)
            }
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

    private func addSettingsRecoveryNotice(_ recovery: SettingsRecovery) {
        let cause = "保存中转名字和地址的设置文件读不出来，可能是文件坏了、正被别的程序占用，或者格式认不出来。"
        switch recovery {
        case .backedUp(let backup):
            addNotice(Notice(
                title: Self.settingsBackedUpTitle,
                detail: [cause, "原文件已经改名备份到：", backup.path, "这个文件里只有中转的名字和地址，没有 API Key。重新填写并完成设置后就能照常使用。"].joined(separator: "\n\n"),
                kind: .warning,
                detailDialogTitle: Self.settingsUnreadableTitle
            ))
        case .backupFailed(let original):
            addNotice(Notice(
                title: Self.settingsUnreadableTitle,
                detail: [cause, "没能备份这个文件，它还在原处：", original.path, "完成设置后，它会被新的设置覆盖。想留着原文件的话，请先自己复制一份。", "这个文件里只有中转的名字和地址，没有 API Key。"].joined(separator: "\n\n"),
                kind: .warning,
                detailDialogTitle: Self.settingsUnreadableTitle
            ))
        }
    }

    func coreDidFail() {
        coreRunning = false
        addNotice(Notice(title: "后台服务没能启动，请退出后重新打开", kind: .error))
    }

    func beginSetup() {
        resetDraft(name: ShellSettings.legacyRelayName)
    }

    func completeSetup() async {
        if setupPhase == .connecting { return }
        guard validateDraft() else { return }
        let draft = snapshotDraft()
        setupPhase = .connecting
        setupFollowUpIssues = []
        let saved: SaveResult
        do {
            saved = try await saveToCore(draft)
        } catch {
            let message = CoreFailure(error).message
            setupPhase = .failed(title: SetupCopy.failedTitle, detail: message.hasSuffix("。") ? message : message + "。")
            return
        }
        do {
            try await persistDraft(saved, draft)
        } catch {
            setupPhase = .failed(title: "没有保存", detail: (error as? PersistError)?.message ?? "")
            return
        }
        removeNotices(titled: Self.settingsBackedUpTitle, Self.settingsUnreadableTitle)
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
            modelCount: saved.modelCount
        )
    }

    func dismissSetupFailure() {
        if case .failed = setupPhase { setupPhase = .idle }
    }

    func finishSetup() {
        showSetup = false
        setupPhase = .idle
    }

    func toggleForwarding(enabled: Bool) async {
        guard await setForwarding(enabled), chatGPT.isRunning() else { return }
        addNotice(Notice(title: enabled ? "已打开转发" : "已关闭转发", detail: "重启 ChatGPT 后生效。", kind: .success))
    }

    func recheckAccess() async {
        removeNotices(titled: Self.detectFailedTitle)
        isDetectingAccess = true
        defer { isDetectingAccess = false }
        let switchCount = forwardingSwitchCount
        guard let data = try? await core.call("detect_access", params: Empty()),
              var value = try? JSONDecoder.snakeCase.decode(CoreStatus.self, from: data) else {
            addNotice(Notice(title: "没能重新检测", kind: .error))
            return
        }
        removeNotices(titled: "没能重新检测")
        if switchCount != forwardingSwitchCount {
            value.forwarding = status?.forwarding ?? value.forwarding
            apply(value)
            return
        }
        apply(value)
        guard let access = value.access else { return }
        if access.detect == "failed" {
            addDetectFailedWarning()
            return
        }
        if access.target == access.mode {
            switch access.mode {
            case "relay":
                addNotice(Notice(title: "接入方式没有变化", detail: "仍然没有检测到 ChatGPT 登录，继续只用中转模型。", kind: .info))
            case "both":
                addNotice(Notice(title: "接入方式没有变化", detail: "仍然检测到 ChatGPT 登录，继续同时使用官方模型和中转模型。", kind: .info))
            default: break
            }
            return
        }
        await reconcileAccess(value)
    }

    func beginAddRelay() {
        guard canAddRelay, !isSavingRelay, !showRelayForm else { return }
        resetDraft(name: "")
        showRelayForm = true
    }

    func beginEditRelay(id: String) {
        guard !isSavingRelay, let relay = relays.first(where: { $0.id == id }) else { return }
        resetDraft(name: relay.name)
        editingRelayID = id
        draftBaseURL = relay.baseURL
        draftAPIKey = ((try? keychain.load(relayID: id)) ?? nil) ?? ""
        showRelayForm = true
    }

    func clearRelaySaveAlert() { relaySaveAlert = nil }

    func cancelRelayForm() {
        guard !isSavingRelay else { return }
        showRelayForm = false
        relaySaveAlert = nil
    }

    func saveRelay() async {
        if isSavingRelay { return }
        guard validateDraft() else { return }
        let draft = snapshotDraft()
        isSavingRelay = true
        defer { isSavingRelay = false }
        relaySaveAlert = nil
        let saved: SaveResult
        do {
            saved = try await saveToCore(draft)
        } catch {
            let failure = CoreFailure(error)
            let suffix = failure.code == "relay_401" ? "。已填的内容都还在，改好后再保存。" : ""
            relaySaveAlert = DialogAlert(title: "没有保存", message: failure.message + suffix, severity: .danger)
            return
        }
        let adding = editingRelayID == nil
        do {
            try await persistDraft(saved, draft)
        } catch {
            relaySaveAlert = DialogAlert(title: "没有保存", message: (error as? PersistError)?.message ?? "", severity: .danger)
            return
        }
        removeNotices(titled: Self.keychainDeniedTitle)
        showRelayForm = false
        addNotice(adding
            ? Notice(title: "已添加「\(saved.name)」", detail: "已获取 \(saved.modelCount) 个模型。重启 ChatGPT 后生效。", kind: .success)
            : Notice(title: "已保存「\(saved.name)」", detail: "重启 ChatGPT 后生效。", kind: .success))
    }

    func canDeleteRelay(id: String) -> Bool { relays.count > 1 && relays.contains { $0.id == id } }

    func requestDeleteRelay(id: String) {
        guard !isDeletingRelay, canDeleteRelay(id: id), let relay = relays.first(where: { $0.id == id }) else { return }
        let count = status?.relays?.first { $0.id == id }?.modelCount ?? 0
        let models = count > 0 ? "的 \(count) 个模型" : "的模型"
        pendingConfirmation = Confirmation(
            kind: .deleteRelay(id: id),
            title: "删除配置「\(relay.name)」？",
            message: "删除后：\n· Codex 的模型列表里不再有「\(relay.name)」\(models)\n· 它的 API Key 会从系统钥匙串里删除\n\n重启 ChatGPT 后生效。",
            confirmLabel: "删除"
        )
    }

    func retryRelay(id: String) async {
        retryingRelayIDs.insert(id)
        defer { retryingRelayIDs.remove(id) }
        await refresh("refresh_models", params: RefreshParams(id: id))
    }

    func setLoginStart(_ enabled: Bool) async {
        if !enabled, forwardingEnabled {
            guard !isDeletingRelay else { mainWindowRequested = true; return }
            pendingConfirmation = Confirmation(
                kind: .loginStartOff,
                title: "关闭「登录时启动」？",
                message: "关闭后，下次登录电脑时助手不会自动运行。在你手动打开助手之前，Codex 里的官方模型和中转模型都无法使用，因为转发仍然开着。如果以后不打算常驻，建议先关闭转发。",
                confirmLabel: "仍然关闭"
            )
            return
        }
        applyLoginStart(enabled)
    }

    func quit() async {
        guard !isDeletingRelay, setupPhase != .connecting else { mainWindowRequested = true; return }
        if forwardingEnabled {
            pendingConfirmation = Confirmation(
                kind: .quit,
                title: "退出模型接入助手？",
                message: "退出前会关闭转发，并把 Codex 配置还原成开启前的样子。"
                    + (chatGPT.isRunning() ? "ChatGPT 正在运行，请在退出后重启 ChatGPT。" : ""),
                confirmLabel: "还原并退出"
            )
        } else {
            await shutdown(restore: status?.forwarding ?? false)
        }
    }

    func uninstall() async {
        guard !isDeletingRelay, setupPhase != .connecting else { mainWindowRequested = true; return }
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
            removeNotices(titled: Self.updateFailedTitle)
            if result.isNewer, let version = result.latestVersion, let url = result.url {
                availableUpdate = UpdateInfo(version: version, url: url)
            } else {
                availableUpdate = nil
                addNotice(Notice(title: "已是最新版本", kind: .success))
            }
        } catch {
            addNotice(Notice(title: Self.updateFailedTitle, kind: .warning))
        }
    }

    func refreshModels() async { await refresh("refresh_models", params: RefreshParams(id: nil)) }

    func recheckStatus() async {
        isRecheckingStatus = true
        defer { isRecheckingStatus = false }
        forwardingFailure = nil
        await refresh("status", params: Empty())
    }

    func cancelPending() {
        guard !isDeletingRelay else { return }
        pendingConfirmation = nil
    }

    func confirmPending() async {
        guard let pending = pendingConfirmation, !isDeletingRelay else { return }
        if case .deleteRelay(let id) = pending.kind {
            await deleteRelay(id: id)
            pendingConfirmation = nil
            return
        }
        pendingConfirmation = nil
        switch pending.kind {
        case .deleteRelay: break
        case .loginStartOff: applyLoginStart(false)
        case .quit: await shutdown(restore: true)
        case .uninstall: await runUninstall()
        }
    }

    func dismissNotice(_ id: UUID) { removeNotice(id) }

    func dismissBanner(_ banner: Banner) { dismissedBanners.insert(banner) }

    private func addNotice(_ notice: Notice) {
        guard !notices.contains(where: { $0.title == notice.title && $0.kind == notice.kind }) else { return }
        notices.append(notice)
        guard notice.kind == .success || notice.kind == .info else { return }
        let delay = noticeAutoDismissDelay
        noticeTimers[notice.id] = Task { [weak self] in
            try? await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))
            if !Task.isCancelled { self?.removeNotice(notice.id) }
        }
    }

    private func removeNotice(_ id: UUID) {
        noticeTimers.removeValue(forKey: id)?.cancel()
        notices.removeAll { $0.id == id }
    }

    private func removeNotices(titled titles: String...) {
        notices.filter { titles.contains($0.title) }.forEach { removeNotice($0.id) }
    }

    private func addDetectFailedWarning() {
        guard !notices.contains(where: { $0.title == Self.detectFailedTitle }) else { return }
        addNotice(Notice(title: Self.detectFailedTitle, detail: "沿用上次的方式。可以稍后点「重新检测」再试。", kind: .warning))
    }

    private func pruneDismissedBanners() {
        dismissedBanners.formIntersection(allBanners)
    }

    private func setForwarding(_ enabled: Bool) async -> Bool {
        isSwitchingForwarding = true
        forwardingSwitchCount += 1
        defer { isSwitchingForwarding = false }
        forwardingFailure = nil
        do {
            let data = try await core.call("set_forwarding", params: ForwardingParams(enabled: enabled))
            if let value = try? JSONDecoder.snakeCase.decode(CoreStatus.self, from: data) { apply(value) }
            return true
        } catch {
            recordFailure(CoreFailure(error))
            return false
        }
    }

    private func alignOnStartup(_ value: CoreStatus) async {
        guard !startupAligned, value.forwardingUsable, !isSwitchingForwarding else { return }
        startupAligned = true
        guard value.access?.target != nil else { return }
        if value.access?.detect == "failed" {
            addDetectFailedWarning()
            return
        }
        await reconcileAccess(value)
    }

    private func reconcileAccess(_ value: CoreStatus) async {
        guard let target = value.access?.target, target != value.access?.mode else { return }
        guard !isSwitchingForwarding, value.forwardingUsable else { return }
        guard await applyAccess(), chatGPT.isRunning() else { return }
        addNotice(target == "both"
            ? Notice(title: "已切换到「官方 + 中转」", detail: "已检测到 ChatGPT 登录。重启 ChatGPT 后生效。", kind: .success)
            : Notice(title: "已切换到「只用中转」", detail: "没有检测到 ChatGPT 登录。重启 ChatGPT 后生效。", kind: .success))
    }

    private func applyAccess() async -> Bool {
        isSwitchingForwarding = true
        defer { isSwitchingForwarding = false }
        forwardingFailure = nil
        do {
            let data = try await core.call("apply_access", params: Empty())
            if let value = try? JSONDecoder.snakeCase.decode(CoreStatus.self, from: data) { apply(value) }
            return true
        } catch {
            recordFailure(CoreFailure(error))
            return false
        }
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
            addNotice(Notice(title: "卸载没有完成", detail: CoreFailure(error).message, kind: .error))
            return
        }
        var leftovers: [String] = []
        do { try keychain.deleteAll() } catch { leftovers.append("钥匙串里的 API Key") }
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

    private func refresh<P: Encodable & Sendable>(_ method: String, params: P) async {
        guard let data = try? await core.call(method, params: params),
              let value = try? JSONDecoder.snakeCase.decode(CoreStatus.self, from: data) else { return }
        apply(value)
        await alignOnStartup(value)
    }

    private func applyLoginStart(_ enabled: Bool) {
        do {
            if enabled { try loginItem.enable() } else { try loginItem.disable() }
            loginStartEnabled = enabled
        } catch {
            addNotice(Notice(title: "没能修改「登录时启动」", kind: .error))
        }
    }

    private func apply(_ value: CoreStatus) {
        status = value
        pruneDismissedBanners()
        if let version = value.update?.latestVersion, let url = value.update?.url {
            availableUpdate = UpdateInfo(version: version, url: url)
        }
    }

    private func resetDraft(name: String) {
        editingRelayID = nil
        draftName = name
        draftBaseURL = ""
        draftAPIKey = ""
        relayFieldErrors = [:]
        relaySaveAlert = nil
    }

    private func validateDraft() -> Bool {
        var errors: [RelayField: String] = [:]
        let name = draftName.trimmingCharacters(in: .whitespaces)
        if name.isEmpty {
            errors[.name] = CoreProtocol.errorMessages["name_empty"]
        } else if name.count > 16 {
            errors[.name] = "名字最多 16 个字，现在是 \(name.count) 个。"
        } else if name.contains("·") {
            errors[.name] = CoreProtocol.errorMessages["name_has_dot"]
        } else if let same = relays.first(where: { $0.id != editingRelayID && $0.name.lowercased() == name.lowercased() }) {
            errors[.name] = CoreFailure(code: "name_duplicate", keys: [same.name]).message
        }
        let url = draftBaseURL.trimmingCharacters(in: .whitespaces)
        if url.isEmpty {
            errors[.baseURL] = "请填写 Base URL。"
        } else if !url.hasPrefix("https://") {
            errors[.baseURL] = CoreProtocol.errorMessages["invalid_base_url"]
        }
        if draftAPIKey.trimmingCharacters(in: .whitespaces).isEmpty { errors[.apiKey] = "请填写 API Key。" }
        relayFieldErrors = errors
        return errors.isEmpty
    }

    private func snapshotDraft() -> DraftSnapshot {
        DraftSnapshot(
            name: draftName.trimmingCharacters(in: .whitespaces),
            baseURL: draftBaseURL.trimmingCharacters(in: .whitespaces),
            apiKey: draftAPIKey
        )
    }

    private func saveToCore(_ draft: DraftSnapshot) async throws -> SaveResult {
        let data = try await core.call(
            "save_relay",
            params: SaveRelayParams(
                id: editingRelayID,
                name: draft.name,
                baseURL: draft.baseURL,
                apiKey: draft.apiKey
            )
        )
        return try JSONDecoder.snakeCase.decode(SaveResult.self, from: data)
    }

    private func persistDraft(_ saved: SaveResult, _ draft: DraftSnapshot) async throws {
        let previous = relays.first { $0.id == saved.id }
        let previousKey = (try? keychain.load(relayID: saved.id)) ?? nil
        do {
            try persistDraftLocally(saved, draft, previousKey: previousKey)
        } catch {
            let revoked = await revokeCore(saved, previous: previous, previousKey: previousKey)
            throw PersistError(failure: error as? StorageFailure ?? .settings, revoked: revoked)
        }
    }

    private func revokeCore(_ saved: SaveResult, previous: RelayConfig?, previousKey: String?) async -> Bool {
        do {
            if let previous {
                guard let previousKey else { return false }
                let restore = SaveRelayParams(id: previous.id, name: previous.name, baseURL: previous.baseURL, apiKey: previousKey)
                _ = try await core.call("save_relay", params: restore)
            } else if relays.isEmpty {
                _ = try await core.call("init", params: CoreInitParams(relays: [], chatgptDir: nil))
            } else {
                _ = try await core.call("delete_relay", params: RelayIDParams(id: saved.id))
            }
            return true
        } catch {
            return false
        }
    }

    private func persistDraftLocally(_ saved: SaveResult, _ draft: DraftSnapshot, previousKey: String?) throws {
        let config = RelayConfig(id: saved.id, name: saved.name, baseURL: draft.baseURL)
        do { try keychain.save(draft.apiKey, relayID: saved.id) } catch { throw StorageFailure.keychain }
        var list = relays
        if let index = list.firstIndex(where: { $0.id == saved.id }) { list[index] = config } else { list.append(config) }
        do {
            try settings.saveRelays(list)
        } catch {
            if let previousKey { try? keychain.save(previousKey, relayID: saved.id) } else { try? keychain.delete(relayID: saved.id) }
            throw StorageFailure.settings
        }
        relays = list
    }

    private func deleteRelay(id: String) async {
        guard let relay = relays.first(where: { $0.id == id }) else { return }
        isDeletingRelay = true
        defer { isDeletingRelay = false }
        let failedTitle = "没有删除「\(relay.name)」"
        let remaining = relays.filter { $0.id != id }
        do {
            try settings.saveRelays(remaining)
        } catch {
            addNotice(Notice(title: failedTitle, detail: StorageFailure.settings.message, kind: .error))
            return
        }
        do {
            let data = try await core.call("delete_relay", params: RelayIDParams(id: id))
            if let value = try? JSONDecoder.snakeCase.decode(CoreStatus.self, from: data) { apply(value) }
        } catch {
            var detail = CoreFailure(error).message
            do { try settings.saveRelays(relays) } catch { detail += "\n本机设置没能恢复，重启助手后这个配置会消失，需要重新添加。" }
            addNotice(Notice(title: failedTitle, detail: detail, kind: .error))
            return
        }
        relays = remaining
        do {
            try keychain.delete(relayID: id)
            addNotice(Notice(title: "已删除「\(relay.name)」", detail: "重启 ChatGPT 后生效。", kind: .success))
        } catch {
            addNotice(Notice(
                title: "已删除「\(relay.name)」",
                detail: "重启 ChatGPT 后生效。\n但没能从系统钥匙串里删除它的 API Key。",
                kind: .warning
            ))
        }
    }
}
