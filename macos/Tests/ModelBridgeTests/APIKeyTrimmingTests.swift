import XCTest
@testable import ModelBridge

@MainActor
final class APIKeyTrimmingTests: XCTestCase {
    private let padded = "  sk-example-0000\n"
    private let clean = "sk-example-0000"

    private struct Rig {
        let model: AppModel
        let core: FakeCore
        let keychain: RelayFakeKeychain
    }

    private func makeRig(relays: [RelayConfig] = []) async -> Rig {
        let log = CallLog()
        let core = FakeCore(log: log, responses: [
            "save_relay": .success(Data(#"{"id":"1","name":"中转","model_count":3,"models_changed":true}"#.utf8))
        ])
        let keychain = RelayFakeKeychain(log: log, entries: Dictionary(uniqueKeysWithValues: relays.map { ($0.id, "sk-old-0000") }))
        let model = AppModel(
            core: core, keychain: keychain, settings: RelayFakeSettings(log: log, relays: relays),
            loginItem: FakeLoginItem(log: log), chatGPT: FakeChatGPT(running: false)
        )
        await model.start()
        return Rig(model: model, core: core, keychain: keychain)
    }

    private func sentAPIKey(_ rig: Rig) throws -> String? {
        let params = try XCTUnwrap(rig.core.calls("save_relay").last?.params)
        let object = try JSONSerialization.jsonObject(with: Data(params.utf8)) as? [String: Any]
        return object?["api_key"] as? String
    }

    func testSetup_APIKeyWithSurroundingWhitespace_IsTrimmedForCoreAndKeychain() async throws {
        let rig = await makeRig()
        rig.model.beginSetup()
        rig.model.draftBaseURL = "https://new.example.com"
        rig.model.draftAPIKey = padded
        await rig.model.completeSetup()
        XCTAssertEqual(try sentAPIKey(rig), clean)
        XCTAssertEqual(rig.keychain.stored["1"], clean)
    }

    func testAddRelay_APIKeyWithSurroundingWhitespace_IsTrimmedForCoreAndKeychain() async throws {
        let rig = await makeRig(relays: [relayOne])
        rig.model.beginAddRelay()
        rig.model.draftName = "备用"
        rig.model.draftBaseURL = "https://new.example.com"
        rig.model.draftAPIKey = padded
        await rig.model.saveRelay()
        XCTAssertEqual(try sentAPIKey(rig), clean)
        XCTAssertEqual(rig.keychain.stored["1"], clean)
    }

    func testEditRelay_APIKeyWithSurroundingWhitespace_IsTrimmedForCoreAndKeychain() async throws {
        let rig = await makeRig(relays: [relayOne])
        rig.model.beginEditRelay(id: "1")
        rig.model.draftAPIKey = padded
        await rig.model.saveRelay()
        XCTAssertEqual(try sentAPIKey(rig), clean)
        XCTAssertEqual(rig.keychain.stored["1"], clean)
    }

    func testSetup_WhitespaceOnlyAPIKey_ShowsRequiredError() async {
        let rig = await makeRig()
        rig.model.beginSetup()
        rig.model.draftBaseURL = "https://new.example.com"
        rig.model.draftAPIKey = " \n\t "
        await rig.model.completeSetup()
        XCTAssertEqual(rig.model.relayFieldErrors[.apiKey], "请填写 API Key。")
        XCTAssertTrue(rig.core.calls("save_relay").isEmpty)
    }

    func testAddRelay_WhitespaceOnlyAPIKey_ShowsRequiredError() async {
        let rig = await makeRig(relays: [relayOne])
        rig.model.beginAddRelay()
        rig.model.draftName = "备用"
        rig.model.draftBaseURL = "https://new.example.com"
        rig.model.draftAPIKey = " \n\t "
        await rig.model.saveRelay()
        XCTAssertEqual(rig.model.relayFieldErrors[.apiKey], "请填写 API Key。")
        XCTAssertTrue(rig.core.calls("save_relay").isEmpty)
    }
}
