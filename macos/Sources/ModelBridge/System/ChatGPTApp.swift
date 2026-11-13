import AppKit

protocol AppControlling: Sendable {
    func runningBundleIDs() -> Set<String>
}

struct WorkspaceApps: AppControlling {
    func runningBundleIDs() -> Set<String> {
        Set(NSWorkspace.shared.runningApplications.compactMap(\.bundleIdentifier))
    }
}

struct ChatGPTApp: Sendable {
    static let bundleID = "com.openai.codex"

    private let apps: AppControlling

    init(apps: AppControlling = WorkspaceApps()) {
        self.apps = apps
    }

    func isRunning() -> Bool {
        apps.runningBundleIDs().contains(Self.bundleID)
    }

}

protocol ChatGPTControlling: Sendable {
    func isRunning() -> Bool
}

extension ChatGPTApp: ChatGPTControlling {}
