import Foundation

struct RelayConfig: Codable, Sendable, Equatable, Identifiable {
    var id: String
    var name: String
    var baseURL: String

    enum CodingKeys: String, CodingKey {
        case id, name
        case baseURL = "base_url"
    }
}

enum SettingsRecovery: Sendable, Equatable {
    case backedUp(URL)
    case backupFailed(URL)
}

struct ShellSettings: Sendable {
    private struct Stored: Codable {
        var relays: [RelayConfig]
    }

    private struct LegacyStored: Decodable {
        var baseURL: String

        enum CodingKeys: String, CodingKey { case baseURL = "base_url" }
    }

    static let legacyRelayID = "1"
    static let legacyRelayName = "中转"

    static var defaultDirectory: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Application Support/ModelBridge")
    }

    let directory: URL

    init(directory: URL = ShellSettings.defaultDirectory) {
        self.directory = directory
    }

    var fileURL: URL {
        directory.appendingPathComponent("shell-settings.json")
    }

    func loadRelays() throws -> [RelayConfig] {
        guard FileManager.default.fileExists(atPath: fileURL.path) else { return [] }
        let data = try Data(contentsOf: fileURL)
        if let stored = try? JSONDecoder().decode(Stored.self, from: data) { return stored.relays }
        let legacy = try JSONDecoder().decode(LegacyStored.self, from: data)
        let migrated = [RelayConfig(id: Self.legacyRelayID, name: Self.legacyRelayName, baseURL: legacy.baseURL)]
        try? saveRelays(migrated)
        return migrated
    }

    var backupURL: URL {
        directory.appendingPathComponent("shell-settings.json.bak")
    }

    func recoverUnreadable() -> SettingsRecovery? {
        do {
            try? FileManager.default.removeItem(at: backupURL)
            try FileManager.default.moveItem(at: fileURL, to: backupURL)
            return .backedUp(backupURL)
        } catch {
            return .backupFailed(fileURL)
        }
    }

    func saveRelays(_ relays: [RelayConfig]) throws {
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        try JSONEncoder().encode(Stored(relays: relays)).write(to: fileURL, options: .atomic)
    }

    func deleteAll() throws {
        var failure: Error?
        for url in [fileURL, backupURL] where FileManager.default.fileExists(atPath: url.path) {
            do { try FileManager.default.removeItem(at: url) } catch { failure = error }
        }
        if let failure { throw failure }
        if let rest = try? FileManager.default.contentsOfDirectory(atPath: directory.path), rest.isEmpty {
            try FileManager.default.removeItem(at: directory)
        }
    }
}

protocol SettingsStoring: Sendable {
    func loadRelays() throws -> [RelayConfig]
    func saveRelays(_ relays: [RelayConfig]) throws
    func deleteAll() throws
    func recoverUnreadable() -> SettingsRecovery?
}

extension SettingsStoring {
    func recoverUnreadable() -> SettingsRecovery? { nil }
}

extension ShellSettings: SettingsStoring {}
