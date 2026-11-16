import XCTest
import Security
@testable import ModelBridge

final class KeychainStoreTests: XCTestCase {
    private var store: KeychainStore!

    override func setUp() {
        store = KeychainStore(service: "ModelBridgeTests-\(UUID().uuidString)")
    }

    override func tearDown() {
        try? store.delete(relayID: "1")
    }

    func testDefaults_ServiceAndAccountNames() {
        XCTAssertEqual(KeychainStore.defaultService, "ModelBridge")
        XCTAssertEqual(KeychainStore.defaultAccount, "relay-api-key")
    }

    func testSaveThenLoad_ReturnsSameValue() throws {
        try store.save("sk-example-0000", relayID: "1")
        XCTAssertEqual(try store.load(relayID: "1"), "sk-example-0000")
    }

    func testSave_OverwritesExistingValue() throws {
        try store.save("sk-example-0000", relayID: "1")
        try store.save("sk-example-1111", relayID: "1")
        XCTAssertEqual(try store.load(relayID: "1"), "sk-example-1111")
    }

    func testLoad_MissingEntryReturnsNil() throws {
        XCTAssertNil(try store.load(relayID: "1"))
    }

    func testDelete_ThenLoadReturnsNil() throws {
        try store.save("sk-example-0000", relayID: "1")
        try store.delete(relayID: "1")
        XCTAssertNil(try store.load(relayID: "1"))
    }

    func testDelete_MissingEntryDoesNotThrow() {
        XCTAssertNoThrow(try store.delete(relayID: "1"))
    }

    func testSave_OverwriteKeepsAccessibleAfterFirstUnlockThisDeviceOnly() throws {
        let expected = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly as String
        try store.save("sk-example-0000", relayID: "1")
        try XCTSkipUnless(try store.accessibleAttribute(relayID: "1") == expected, "文件钥匙串不回报 kSecAttrAccessible，仅数据保护钥匙串可验证")
        try store.save("sk-example-1111", relayID: "1")
        XCTAssertEqual(try store.accessibleAttribute(relayID: "1"), kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly as String)
    }

    func testErrorStatus_AccessDeniedDistinguishedFromOtherFailures() {
        for status in [errSecAuthFailed, errSecInteractionNotAllowed, errSecUserCanceled, errSecNoAccessForItem] {
            XCTAssertEqual(KeychainError(status: status), .accessDenied(status))
        }
        XCTAssertEqual(KeychainError(status: errSecParam), .failed(errSecParam))
    }
}
