struct MenuPanelItem: Equatable, Identifiable {
    enum ID: Equatable, Hashable {
        case forwarding, openConfig, loginStart, update, uninstall, quit
    }

    let id: ID
    let title: String
    var subtitle: String?
    var shortcut: String?
    var checked: Bool?
    var isDisabled = false
}

@MainActor
struct MenuPanelModel {
    let title = "模型接入助手"
    let subtitle: String
    let groups: [[MenuPanelItem]]

    init(model: AppModel) {
        subtitle = model.menuStatusLine
        let update: MenuPanelItem
        if let available = model.availableUpdate {
            update = MenuPanelItem(id: .update, title: "发现新版本 \(available.version)", subtitle: "打开下载页")
        } else {
            update = MenuPanelItem(id: .update, title: "检查更新")
        }
        groups = [
            [
                MenuPanelItem(
                    id: .forwarding, title: "转发到中台",
                    checked: model.forwardingEnabled, isDisabled: model.forwardingToggleDisabled
                ),
                MenuPanelItem(id: .openConfig, title: "打开配置…", shortcut: "⌘,"),
            ],
            [
                MenuPanelItem(id: .loginStart, title: "登录时启动", checked: model.loginStartEnabled),
                update,
            ],
            [
                MenuPanelItem(id: .uninstall, title: "卸载模型接入助手…"),
                MenuPanelItem(id: .quit, title: "退出模型接入助手", shortcut: "⌘Q"),
            ],
        ]
    }
}

enum MenuKeyAction: Equatable, Sendable {
    case moveDown, moveUp, activateFocused, close, openConfig, quit

    init?(keyCode: UInt16, command: Bool, key: String?) {
        if command {
            switch key {
            case ",": self = .openConfig
            case "q": self = .quit
            default: return nil
            }
            return
        }
        switch keyCode {
        case 125: self = .moveDown
        case 126: self = .moveUp
        case 36, 49: self = .activateFocused
        case 53: self = .close
        default: return nil
        }
    }
}
