import Foundation

struct ShellSettings: Sendable {
    private struct Stored: Codable {
        var baseURL: String

        enum CodingKeys: String, CodingKey { case baseURL = "base_url" }
    }

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

    func loadBaseURL() throws -> String? {
        guard FileManager.default.fileExists(atPath: fileURL.path) else { return nil }
        return try JSONDecoder().decode(Stored.self, from: Data(contentsOf: fileURL)).baseURL
    }

    func saveBaseURL(_ baseURL: String) throws {
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        try JSONEncoder().encode(Stored(baseURL: baseURL)).write(to: fileURL, options: .atomic)
    }

    func deleteAll() throws {
        guard FileManager.default.fileExists(atPath: fileURL.path) else { return }
        try FileManager.default.removeItem(at: fileURL)
    }
}

protocol SettingsStoring: Sendable {
    func loadBaseURL() throws -> String?
    func saveBaseURL(_ baseURL: String) throws
    func deleteAll() throws
}

extension ShellSettings: SettingsStoring {}
