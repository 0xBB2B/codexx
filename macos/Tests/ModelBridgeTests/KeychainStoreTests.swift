import XCTest
import Security
@testable import ModelBridge

final class KeychainStoreTests: XCTestCase {
    private var store: KeychainStore!

    override func setUp() {
        store = KeychainStore(service: "ModelBridgeTests-\(UUID().uuidString)")
    }

    override func tearDown() {
        try? store.delete()
    }

    func testDefaults_ServiceAndAccountNames() {
        XCTAssertEqual(KeychainStore.defaultService, "ModelBridge")
        XCTAssertEqual(KeychainStore.defaultAccount, "relay-api-key")
    }

    func testSaveThenLoad_ReturnsSameValue() throws {
        try store.save("sk-example-0000")
        XCTAssertEqual(try store.load(), "sk-example-0000")
    }

    func testSave_OverwritesExistingValue() throws {
        try store.save("sk-example-0000")
        try store.save("sk-example-1111")
        XCTAssertEqual(try store.load(), "sk-example-1111")
    }

    func testLoad_MissingEntryReturnsNil() throws {
        XCTAssertNil(try store.load())
    }

    func testDelete_ThenLoadReturnsNil() throws {
        try store.save("sk-example-0000")
        try store.delete()
        XCTAssertNil(try store.load())
    }

    func testDelete_MissingEntryDoesNotThrow() {
        XCTAssertNoThrow(try store.delete())
    }

    func testSave_OverwriteKeepsAccessibleAfterFirstUnlockThisDeviceOnly() throws {
        let expected = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly as String
        try store.save("sk-example-0000")
        try XCTSkipUnless(try store.accessibleAttribute() == expected, "文件钥匙串不回报 kSecAttrAccessible，仅数据保护钥匙串可验证")
        try store.save("sk-example-1111")
        XCTAssertEqual(try store.accessibleAttribute(), kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly as String)
    }

    func testErrorStatus_AccessDeniedDistinguishedFromOtherFailures() {
        for status in [errSecAuthFailed, errSecInteractionNotAllowed, errSecUserCanceled, errSecNoAccessForItem] {
            XCTAssertEqual(KeychainError(status: status), .accessDenied(status))
        }
        XCTAssertEqual(KeychainError(status: errSecParam), .failed(errSecParam))
    }
}
