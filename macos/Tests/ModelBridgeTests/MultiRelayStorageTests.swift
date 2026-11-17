import XCTest
@testable import ModelBridge

final class MultiRelayStorageTests: XCTestCase {
    private var dir: URL!
    private var settings: ShellSettings!

    override func setUp() {
        dir = FileManager.default.temporaryDirectory
            .appendingPathComponent("relay-settings-\(UUID().uuidString)")
            .appendingPathComponent("ModelBridge")
        settings = ShellSettings(directory: dir)
    }

    override func tearDown() {
        try? FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: dir.path)
        try? FileManager.default.removeItem(at: dir.deletingLastPathComponent())
    }

    private func writeRaw(_ json: String) throws {
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        try Data(json.utf8).write(to: settings.fileURL)
    }

    private func rawObject() throws -> [String: Any] {
        let data = try Data(contentsOf: settings.fileURL)
        return try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    func testLoadRelays_NoFileReturnsEmpty() throws {
        XCTAssertEqual(try settings.loadRelays(), [])
    }

    func testSaveThenLoad_KeepsOrderAndFields() throws {
        let relays = [
            RelayConfig(id: "1", name: "中转", baseURL: "https://a.example.com"),
            RelayConfig(id: "3", name: "备用", baseURL: "https://b.example.com"),
            RelayConfig(id: "2", name: "测试", baseURL: "https://c.example.com"),
        ]
        try settings.saveRelays(relays)
        XCTAssertEqual(try settings.loadRelays(), relays)
    }

    func testFile_StoresRelaysListWithIdNameBaseURLOnly() throws {
        try settings.saveRelays([RelayConfig(id: "1", name: "中转", baseURL: "https://a.example.com")])
        let list = try XCTUnwrap(try rawObject()["relays"] as? [[String: Any]])
        XCTAssertEqual(list.count, 1)
        XCTAssertEqual(Set(list[0].keys), ["id", "name", "base_url"])
        XCTAssertEqual(list[0]["id"] as? String, "1")
        XCTAssertEqual(list[0]["name"] as? String, "中转")
        XCTAssertEqual(list[0]["base_url"] as? String, "https://a.example.com")
    }

    func testFile_NeverContainsApiKey() throws {
        try settings.saveRelays([RelayConfig(id: "1", name: "中转", baseURL: "https://a.example.com")])
        let text = try String(contentsOf: settings.fileURL, encoding: .utf8)
        XCTAssertFalse(text.contains("api_key"))
    }

    func testMigration_LegacySingleBaseURLBecomesRelayOne() throws {
        try writeRaw(#"{"base_url":"https://old.example.com"}"#)
        XCTAssertEqual(
            try settings.loadRelays(),
            [RelayConfig(id: "1", name: "中转", baseURL: "https://old.example.com")]
        )
    }

    func testMigration_WritesBackNewFormat() throws {
        try writeRaw(#"{"base_url":"https://old.example.com"}"#)
        _ = try settings.loadRelays()
        let object = try rawObject()
        XCTAssertNil(object["base_url"])
        let list = try XCTUnwrap(object["relays"] as? [[String: Any]])
        XCTAssertEqual(list.count, 1)
        XCTAssertEqual(list[0]["id"] as? String, "1")
        XCTAssertEqual(list[0]["name"] as? String, "中转")
        XCTAssertEqual(list[0]["base_url"] as? String, "https://old.example.com")
        XCTAssertEqual(try settings.loadRelays().count, 1)
    }

    func testMigration_WriteBackFails_StillReturnsMigratedList() throws {
        try writeRaw(#"{"base_url":"https://old.example.com"}"#)
        try FileManager.default.setAttributes([.posixPermissions: 0o555], ofItemAtPath: dir.path)
        defer { try? FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: dir.path) }
        XCTAssertEqual(
            try settings.loadRelays(),
            [RelayConfig(id: "1", name: "中转", baseURL: "https://old.example.com")]
        )
    }

    @MainActor
    func testMigration_WriteBackFails_InitCarriesOriginalKeyAndNoSetupNeeded() async throws {
        try writeRaw(#"{"base_url":"https://old.example.com"}"#)
        try FileManager.default.setAttributes([.posixPermissions: 0o555], ofItemAtPath: dir.path)
        defer { try? FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: dir.path) }
        let log = CallLog()
        let keychain = RelayFakeKeychain(log: log, entries: ["1": "sk-old-0000"])
        let params = CoreInitParams.load(settings: settings, keychain: keychain, chatgptDir: nil)
        XCTAssertEqual(params.relays, [CoreRelayInit(id: "1", name: "中转", baseURL: "https://old.example.com", apiKey: "sk-old-0000")])
        let model = AppModel(
            core: FakeCore(log: log), keychain: keychain, settings: settings,
            loginItem: FakeLoginItem(log: log), chatGPT: FakeChatGPT(running: false)
        )
        await model.start()
        XCTAssertFalse(model.needsSetup)
    }

    func testMigration_InitCarriesOriginalKeyUnderAccountRelayApiKey() throws {
        try writeRaw(#"{"base_url":"https://old.example.com"}"#)
        let log = CallLog()
        let params = CoreInitParams.load(
            settings: settings, keychain: RelayFakeKeychain(log: log, entries: ["1": "sk-old-0000"]), chatgptDir: nil
        )
        XCTAssertEqual(params.relays, [CoreRelayInit(id: "1", name: "中转", baseURL: "https://old.example.com", apiKey: "sk-old-0000")])
    }

    func testDeleteAll_RemovesFile() throws {
        try settings.saveRelays([RelayConfig(id: "1", name: "中转", baseURL: "https://a.example.com")])
        try settings.deleteAll()
        XCTAssertFalse(FileManager.default.fileExists(atPath: settings.fileURL.path))
    }

    func testKeychainAccount_IdOneUsesOriginalEntry() {
        XCTAssertEqual(KeychainStore.account(forRelayID: "1"), "relay-api-key")
        XCTAssertEqual(KeychainStore.account(forRelayID: "1"), KeychainStore.defaultAccount)
    }

    func testKeychainAccount_OtherIdsAppendSuffix() {
        XCTAssertEqual(KeychainStore.account(forRelayID: "2"), "relay-api-key-2")
        XCTAssertEqual(KeychainStore.account(forRelayID: "10"), "relay-api-key-10")
    }

    func testKeychain_RelayEntriesAreIndependent() throws {
        let store = KeychainStore(service: "ModelBridgeTests-\(UUID().uuidString)")
        defer { try? store.delete(relayID: "1"); try? store.delete(relayID: "2") }
        try store.save("sk-example-0001", relayID: "1")
        try store.save("sk-example-0002", relayID: "2")
        XCTAssertEqual(try store.load(relayID: "1"), "sk-example-0001")
        XCTAssertEqual(try store.load(relayID: "2"), "sk-example-0002")
        try store.delete(relayID: "2")
        XCTAssertNil(try store.load(relayID: "2"))
        XCTAssertEqual(try store.load(relayID: "1"), "sk-example-0001")
    }

    func testKeychain_DeleteAllRemovesEveryEntryOfServiceOnly() throws {
        let store = KeychainStore(service: "ModelBridgeTests-\(UUID().uuidString)")
        let other = KeychainStore(service: "ModelBridgeTests-\(UUID().uuidString)")
        defer { try? store.deleteAll(); try? other.deleteAll() }
        for id in ["1", "2", "9"] { try store.save("sk-example-\(id)", relayID: id) }
        try other.save("sk-other", relayID: "1")
        try store.deleteAll()
        for id in ["1", "2", "9"] { XCTAssertNil(try store.load(relayID: id)) }
        XCTAssertEqual(try other.load(relayID: "1"), "sk-other")
    }

    func testKeychain_DeleteAllWithNothingStoredDoesNotThrow() {
        XCTAssertNoThrow(try KeychainStore(service: "ModelBridgeTests-\(UUID().uuidString)").deleteAll())
    }

    func testKeychain_DeleteMissingRelayEntryDoesNotThrow() {
        let store = KeychainStore(service: "ModelBridgeTests-\(UUID().uuidString)")
        XCTAssertNoThrow(try store.delete(relayID: "7"))
    }

    private func decodeStatus(_ json: String) throws -> CoreStatus {
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        return try decoder.decode(CoreStatus.self, from: Data(json.utf8))
    }

    func testCoreStatus_DecodesRelaysAndDefaultRelay() throws {
        let status = try decodeStatus("""
        {"version":"1.3.0","forwarding":true,"default_relay":"中转","relays":[
        {"id":"1","name":"中转","model_count":6,"fetched_at":"2026-11-16T00:12:00Z","stale":false,"error":null},
        {"id":"2","name":"备用","model_count":0,"fetched_at":null,"stale":false,"error":"relay_401"}]}
        """)
        XCTAssertEqual(status.defaultRelay, "中转")
        XCTAssertEqual(status.relays?.map(\.id), ["1", "2"])
        XCTAssertEqual(status.relays?[0].name, "中转")
        XCTAssertEqual(status.relays?[0].modelCount, 6)
        XCTAssertEqual(status.relays?[0].fetchedAt, "2026-11-16T00:12:00Z")
        XCTAssertEqual(status.relays?[0].stale, false)
        XCTAssertNil(status.relays?[1].fetchedAt)
        XCTAssertEqual(status.relays?[1].error, "relay_401")
    }

    func testCoreStatus_DefaultRelayNullDecodesToNil() throws {
        let status = try decodeStatus(#"{"version":"1.3.0","forwarding":true,"default_relay":null,"relays":[]}"#)
        XCTAssertNil(status.defaultRelay)
        XCTAssertEqual(status.relays, [])
    }

    func testInitParams_EncodesRelaysArrayWithKeysAndChatgptDir() throws {
        let params = CoreInitParams(
            relays: [
                CoreRelayInit(id: "1", name: "中转", baseURL: "https://a.example.com", apiKey: "sk-example-0001"),
                CoreRelayInit(id: "2", name: "备用", baseURL: "https://b.example.com", apiKey: "sk-example-0002"),
            ],
            chatgptDir: "/Applications/ChatGPT.app"
        )
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(params)) as? [String: Any])
        XCTAssertEqual(object["chatgpt_dir"] as? String, "/Applications/ChatGPT.app")
        XCTAssertNil(object["base_url"])
        XCTAssertNil(object["api_key"])
        let relays = try XCTUnwrap(object["relays"] as? [[String: Any]])
        XCTAssertEqual(relays.count, 2)
        XCTAssertEqual(Set(relays[0].keys), ["id", "name", "base_url", "api_key"])
        XCTAssertEqual(relays[1]["id"] as? String, "2")
        XCTAssertEqual(relays[1]["api_key"] as? String, "sk-example-0002")
    }

    func testErrorMessage_NameDuplicateTemplateHasNoAnnotation() {
        XCTAssertEqual(CoreProtocol.errorMessages["name_duplicate"], "已有同名配置「<keys[0]>」。")
    }

    func testCoreFailure_NameDuplicateFillsExistingName() {
        XCTAssertEqual(
            CoreFailure(code: "name_duplicate", keys: ["Alpha"]).message,
            "已有同名配置「Alpha」。"
        )
    }

    func testErrorMessage_Relay401IsRelayWording() {
        XCTAssertTrue(CoreProtocol.errorMessages["relay_401"]?.hasPrefix("中转服务返回 401：API Key 无效或已过期") == true)
    }

    func testSources_ContainNoLegacyWordingForRelay() throws {
        let forbidden = "\u{4E2D}\u{53F0}"
        let macosRoot = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        var offenders: [String] = []
        for sub in ["Sources", "Tests"] {
            let enumerator = FileManager.default.enumerator(at: macosRoot.appendingPathComponent(sub), includingPropertiesForKeys: nil)
            while let url = enumerator?.nextObject() as? URL, url.pathExtension == "swift" {
                if try String(contentsOf: url, encoding: .utf8).contains(forbidden) { offenders.append(url.lastPathComponent) }
            }
        }
        XCTAssertEqual(offenders, [])
    }
}
