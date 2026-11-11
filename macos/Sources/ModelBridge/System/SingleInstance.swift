import AppKit

protocol RunningInstanceLister {
    func processIDs(bundleID: String) -> [Int32]
}

struct RunningApplicationLister: RunningInstanceLister {
    func processIDs(bundleID: String) -> [Int32] {
        NSRunningApplication.runningApplications(withBundleIdentifier: bundleID).map(\.processIdentifier)
    }
}

struct SingleInstance {
    static let activateMainWindow = Notification.Name("io.github.modelbridge.app.activateMainWindow")

    static var defaultNotificationCenter: NotificationCenter { DistributedNotificationCenter.default() }

    func observeActivation(handler: @escaping @Sendable () -> Void) -> NSObjectProtocol {
        notificationCenter.addObserver(forName: Self.activateMainWindow, object: nil, queue: .main) { _ in handler() }
    }

    private let bundleID: String
    private let currentPID: Int32
    private let lister: RunningInstanceLister
    private let notificationCenter: NotificationCenter

    init(
        bundleID: String,
        currentPID: Int32 = ProcessInfo.processInfo.processIdentifier,
        lister: RunningInstanceLister = RunningApplicationLister(),
        notificationCenter: NotificationCenter = SingleInstance.defaultNotificationCenter
    ) {
        self.bundleID = bundleID
        self.currentPID = currentPID
        self.lister = lister
        self.notificationCenter = notificationCenter
    }

    func shouldTerminate() -> Bool {
        guard lister.processIDs(bundleID: bundleID).contains(where: { $0 != currentPID }) else { return false }
        if let distributed = notificationCenter as? DistributedNotificationCenter {
            distributed.postNotificationName(Self.activateMainWindow, object: nil, userInfo: nil, deliverImmediately: true)
        } else {
            notificationCenter.post(name: Self.activateMainWindow, object: nil)
        }
        return true
    }
}
