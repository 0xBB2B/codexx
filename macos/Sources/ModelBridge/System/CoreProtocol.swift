import Foundation

struct CoreRelayInit: Codable, Sendable, Equatable {
    var id: String
    var name: String
    var baseURL: String
    var apiKey: String?

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

struct CoreInitParams: Codable, Sendable, Equatable {
    var relays: [CoreRelayInit]
    var chatgptDir: String?

    enum CodingKeys: String, CodingKey {
        case relays
        case chatgptDir = "chatgpt_dir"
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(relays, forKey: .relays)
        try c.encode(chatgptDir, forKey: .chatgptDir)
    }
}

struct StoredRelays: Sendable {
    var relays: [RelayConfig]
    var keys: [String: Result<String?, Error>]
    var recovery: SettingsRecovery?
    var readFailed = false

    static func load(settings: any SettingsStoring, keychain: any KeyStoring) -> StoredRelays {
        var stored = read(settings: settings, keychain: keychain)
        if stored.readFailed { stored.recovery = settings.recoverUnreadable() }
        return stored
    }

    static func read(settings: any SettingsStoring, keychain: any KeyStoring) -> StoredRelays {
        let relays: [RelayConfig]
        var readFailed = false
        do {
            relays = try settings.loadRelays()
        } catch {
            relays = []
            readFailed = true
        }
        var keys: [String: Result<String?, Error>] = [:]
        for relay in relays { keys[relay.id] = Result { try keychain.load(relayID: relay.id) } }
        return StoredRelays(relays: relays, keys: keys, readFailed: readFailed)
    }

    var initRelays: [CoreRelayInit] {
        relays.map { relay in
            CoreRelayInit(
                id: relay.id, name: relay.name, baseURL: relay.baseURL,
                apiKey: (try? keys[relay.id]?.get()) ?? nil
            )
        }
    }
}

struct CoreStatus: Codable, Sendable, Equatable {
    struct Component: Codable, Sendable, Equatable {
        var running: Bool?
        var found: Bool?
        var error: String?
    }

    struct Relay: Codable, Sendable, Equatable {
        var id: String
        var name: String
        var modelCount: Int?
        var fetchedAt: String?
        var stale: Bool?
        var error: String?
    }

    struct Update: Codable, Sendable, Equatable {
        var latestVersion: String?
        var url: String?
    }

    struct Access: Codable, Sendable, Equatable {
        var mode: String?
        var target: String?
        var detect: String?
    }

    var version: String
    var forwarding: Bool
    var proxy: Component?
    var chatgpt: Component?
    var defaultRelay: String?
    var relays: [Relay]?
    var takenOver: [String]?
    var restoreBlocked: [String]?
    var configPath: String?
    var update: Update?
    var access: Access?

    var forwardingUsable: Bool {
        forwarding && chatgpt?.found != false && chatgpt?.error != "chatgpt_unsupported"
    }
}

enum CoreProtocol {
    static let errorMessages: [String: String] = [
        "invalid_base_url": "Base URL 要以 https:// 开头。",
        "name_empty": "请填写名字。",
        "name_too_long": "名字最多 16 个字。",
        "name_has_dot": "名字里不能有「·」。",
        "name_duplicate": "已有同名配置「<keys[0]>」。",
        "relay_limit": "已有 10 个，达到上限。",
        "relay_last": "至少保留一个配置。",
        "relay_not_found": "这个配置不存在，请刷新后重试。",
        "relay_401": "中转服务返回 401：API Key 无效或已过期",
        "relay_403": "中转服务返回 403：API Key 没有权限",
        "relay_404": "中转服务返回 404：Base URL 不正确",
        "relay_429": "中转服务返回 429：请求太频繁，请稍后重试",
        "relay_5xx": "中转服务返回 <keys[0]>：中转服务暂时不可用",
        "relay_4xx": "中转服务返回 <keys[0]>：请求未被中转服务接受",
        "relay_bad_response": "中转服务返回 <keys[0]>：没有返回模型列表，请检查 Base URL",
        "relay_unreachable": "连不上中转服务，请检查网络和 Base URL",
        "chatgpt_not_found": "没有找到 ChatGPT",
        "chatgpt_unsupported": "当前 ChatGPT 版本暂不支持",
        "proxy_port_in_use": "本机转发服务没能启动：端口被占用",
        "proxy_start_timeout": "启动转发超过 30 秒还没完成，请重试",
        "restore_blocked": "没能自动还原 Codex 配置",
        "config_invalid": "Codex 配置文件格式有误，未做修改",
        "config_changed": "Codex 配置文件刚被其他程序修改，请重试",
        "update_unavailable": "暂时无法检查更新，请稍后再试",
        "internal_error": "内部错误，请重试",
        "unknown_method": "未知请求",
        "bad_params": "请求参数有误",
    ]
}
