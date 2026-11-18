import AppKit

protocol AppTrashing: Sendable {
    func moveToTrash(_ url: URL) async -> Error?
}

struct WorkspaceTrash: AppTrashing {
    func moveToTrash(_ url: URL) async -> Error? {
        await withCheckedContinuation { continuation in
            NSWorkspace.shared.recycle([url]) { _, error in continuation.resume(returning: error) }
        }
    }
}
