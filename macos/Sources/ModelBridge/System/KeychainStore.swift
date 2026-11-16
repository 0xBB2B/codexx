import Foundation
import Security

enum KeychainError: Error, Equatable {
    case accessDenied(OSStatus)
    case failed(OSStatus)

    init(status: OSStatus) {
        switch status {
        case errSecAuthFailed, errSecInteractionNotAllowed, errSecUserCanceled, errSecNoAccessForItem:
            self = .accessDenied(status)
        default:
            self = .failed(status)
        }
    }
}

struct KeychainStore: Sendable {
    static let defaultService = "ModelBridge"
    static let defaultAccount = "relay-api-key"

    private let service: String

    init(service: String = KeychainStore.defaultService) {
        self.service = service
    }

    static func account(forRelayID id: String) -> String {
        id == "1" ? defaultAccount : "\(defaultAccount)-\(id)"
    }

    private func identity(_ relayID: String) -> [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: Self.account(forRelayID: relayID),
        ]
    }

    func save(_ value: String, relayID: String) throws {
        let identity = identity(relayID)
        let data = Data(value.utf8)
        let updated = SecItemUpdate(identity as CFDictionary, [
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
        ] as CFDictionary)
        if updated == errSecSuccess { return }
        guard updated == errSecItemNotFound else { throw KeychainError(status: updated) }
        var attributes = identity
        attributes[kSecValueData as String] = data
        attributes[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        let added = SecItemAdd(attributes as CFDictionary, nil)
        guard added == errSecSuccess else { throw KeychainError(status: added) }
    }

    func load(relayID: String) throws -> String? {
        var query = identity(relayID)
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess, let data = result as? Data else { throw KeychainError(status: status) }
        return String(data: data, encoding: .utf8)
    }

    func accessibleAttribute(relayID: String) throws -> String? {
        var query = identity(relayID)
        query[kSecReturnAttributes as String] = true
        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        guard status == errSecSuccess else { throw KeychainError(status: status) }
        return (result as? [String: Any])?[kSecAttrAccessible as String] as? String
    }

    func delete(relayID: String) throws {
        let status = SecItemDelete(identity(relayID) as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else { throw KeychainError(status: status) }
    }

    func deleteAll() throws {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
        ]
        var status = SecItemDelete(query as CFDictionary)
        while status == errSecSuccess { status = SecItemDelete(query as CFDictionary) }
        guard status == errSecItemNotFound else { throw KeychainError(status: status) }
    }
}

protocol KeyStoring: Sendable {
    func save(_ value: String, relayID: String) throws
    func load(relayID: String) throws -> String?
    func delete(relayID: String) throws
    func deleteAll() throws
}

extension KeychainStore: KeyStoring {}
