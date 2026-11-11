import AppKit

enum ChatGPTRestartResult: Sendable, Equatable {
    case restarted
    case timedOut
    case notFound
    case openFailed
    case cancelled
}

protocol AppControlling: Sendable {
    func runningBundleIDs() -> Set<String>
    func requestQuit(bundleID: String)
    func openApplication(at url: URL) async throws
}

struct WorkspaceApps: AppControlling {
    func runningBundleIDs() -> Set<String> {
        Set(NSWorkspace.shared.runningApplications.compactMap(\.bundleIdentifier))
    }

    func requestQuit(bundleID: String) {
        NSRunningApplication.runningApplications(withBundleIdentifier: bundleID).forEach { _ = $0.terminate() }
    }

    func openApplication(at url: URL) async throws {
        _ = try await NSWorkspace.shared.openApplication(at: url, configuration: NSWorkspace.OpenConfiguration())
    }
}

struct ChatGPTApp: Sendable {
    static let bundleID = "com.openai.codex"
    static let defaultQuitTimeout: TimeInterval = 30

    private let apps: AppControlling
    private let homeDirectory: URL
    private let fileExists: @Sendable (URL) -> Bool
    private let quitTimeout: TimeInterval
    private let pollInterval: TimeInterval

    init(
        apps: AppControlling = WorkspaceApps(),
        homeDirectory: URL = FileManager.default.homeDirectoryForCurrentUser,
        fileExists: @escaping @Sendable (URL) -> Bool = { FileManager.default.fileExists(atPath: $0.path) },
        quitTimeout: TimeInterval = ChatGPTApp.defaultQuitTimeout,
        pollInterval: TimeInterval = 0.2
    ) {
        self.apps = apps
        self.homeDirectory = homeDirectory
        self.fileExists = fileExists
        self.quitTimeout = quitTimeout
        self.pollInterval = pollInterval
    }

    func isRunning() -> Bool {
        apps.runningBundleIDs().contains(Self.bundleID)
    }

    func locate() -> URL? {
        [
            URL(fileURLWithPath: "/Applications/ChatGPT.app"),
            homeDirectory.appendingPathComponent("Applications/ChatGPT.app"),
        ].first(where: fileExists)
    }

    func restart() async -> ChatGPTRestartResult {
        guard let appURL = locate() else { return .notFound }
        if isRunning() {
            apps.requestQuit(bundleID: Self.bundleID)
            let deadline = Date().addingTimeInterval(quitTimeout)
            while isRunning(), Date() < deadline {
                try? await Task.sleep(nanoseconds: UInt64(pollInterval * 1_000_000_000))
                if Task.isCancelled { return .cancelled }
            }
            if isRunning() { return .timedOut }
        }
        do {
            try await apps.openApplication(at: appURL)
            return .restarted
        } catch {
            return .openFailed
        }
    }
}

protocol ChatGPTControlling: Sendable {
    func isRunning() -> Bool
    func restart() async -> ChatGPTRestartResult
}

extension ChatGPTApp: ChatGPTControlling {}
