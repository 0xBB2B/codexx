import AppKit
import ServiceManagement

enum LoginItemStatus: Sendable {
    case enabled
    case notRegistered
    case requiresApproval
    case notFound
}

protocol LoginItemService: Sendable {
    var status: LoginItemStatus { get }
    func register() throws
    func unregister() throws
}

struct MainAppLoginService: LoginItemService {
    var status: LoginItemStatus {
        switch SMAppService.mainApp.status {
        case .enabled: .enabled
        case .requiresApproval: .requiresApproval
        case .notFound: .notFound
        default: .notRegistered
        }
    }

    func register() throws { try SMAppService.mainApp.register() }
    func unregister() throws { try SMAppService.mainApp.unregister() }
}

struct LoginItem: Sendable {
    private let service: LoginItemService
    private let launchedAtLogin: @Sendable () -> Bool

    init(
        service: LoginItemService = MainAppLoginService(),
        launchedAtLogin: @escaping @Sendable () -> Bool = LoginItem.systemLaunchedAtLogin
    ) {
        self.service = service
        self.launchedAtLogin = launchedAtLogin
    }

    var isEnabled: Bool { service.status == .enabled }
    var wasLaunchedAtLogin: Bool { launchedAtLogin() }

    func enable() throws { try service.register() }
    func disable() throws { try service.unregister() }

    static let systemLaunchedAtLogin: @Sendable () -> Bool = {
        let event = NSAppleEventManager.shared().currentAppleEvent
        let reason = event?.paramDescriptor(forKeyword: AEKeyword(keyAEPropData))?.enumCodeValue
        return reason == OSType(keyAELaunchedAsLogInItem)
    }
}

protocol LoginItemControlling: Sendable {
    func enable() throws
    func disable() throws
}

extension LoginItem: LoginItemControlling {}
