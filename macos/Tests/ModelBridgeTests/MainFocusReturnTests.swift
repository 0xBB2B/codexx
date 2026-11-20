import AppKit
import XCTest
@testable import ModelBridge

@MainActor
final class MainFocusReturnTests: XCTestCase {
    private static let saved = Result<Data, CoreError>.success(Data(#"{"id":"1","name":"配置1","model_count":5,"models_changed":true}"#.utf8))
    private static let afterDelete = Result<Data, CoreError>.success(Data(ViewFixture.statusJSON(forwarding: true, relayCount: 2).utf8))

    private func fillAddDraft(_ model: AppModel) {
        model.draftName = "新配置"
        model.draftBaseURL = "https://relay.example.com"
        model.draftAPIKey = "sk-example-0000"
    }

    func testAdd_Cancel_FocusesAddButton() async {
        let f = await ViewFixture.make()
        f.model.beginAddRelay()
        f.model.cancelRelayForm()
        XCTAssertEqual(f.model.mainFocusTarget, .addButton)
    }

    func testAdd_SaveSuccess_FocusesAddButton() async {
        let f = await ViewFixture.make(responses: ["save_relay": Self.saved])
        f.model.beginAddRelay()
        fillAddDraft(f.model)
        await f.model.saveRelay()
        XCTAssertFalse(f.model.showRelayForm)
        XCTAssertEqual(f.model.mainFocusTarget, .addButton)
    }

    func testEdit_Cancel_FocusesThatRowEditButton() async {
        let f = await ViewFixture.make()
        f.model.beginEditRelay(id: "2")
        f.model.cancelRelayForm()
        XCTAssertEqual(f.model.mainFocusTarget, .edit("2"))
    }

    func testEdit_SaveSuccess_FocusesThatRowEditButton() async {
        let f = await ViewFixture.make(responses: ["save_relay": Self.saved])
        f.model.beginEditRelay(id: "1")
        await f.model.saveRelay()
        XCTAssertFalse(f.model.showRelayForm)
        XCTAssertEqual(f.model.mainFocusTarget, .edit("1"))
    }

    func testDelete_Cancel_FocusesThatRowDeleteButton() async {
        let f = await ViewFixture.make()
        f.model.requestDeleteRelay(id: "2")
        f.model.cancelPending()
        XCTAssertEqual(f.model.mainFocusTarget, .delete("2"))
    }

    func testDelete_Success_FocusesAddButton() async {
        let f = await ViewFixture.make(responses: ["delete_relay": Self.afterDelete])
        f.model.requestDeleteRelay(id: "3")
        await f.model.confirmPending()
        XCTAssertNil(f.model.pendingConfirmation)
        XCTAssertEqual(f.model.mainFocusTarget, .addButton)
    }

    func testDelete_Failure_RowRemains_FocusesThatRowDeleteButton() async {
        let f = await ViewFixture.make(responses: ["delete_relay": .failure(.remote(code: "relay_not_found", keys: []))])
        f.model.requestDeleteRelay(id: "3")
        await f.model.confirmPending()
        XCTAssertNil(f.model.pendingConfirmation)
        XCTAssertTrue(f.model.relays.contains { $0.id == "3" })
        XCTAssertEqual(f.model.mainFocusTarget, .delete("3"))
    }

    func testSaveFailure_FormStaysOpen_FocusTargetUnchanged() async {
        let f = await ViewFixture.make(responses: ["save_relay": .failure(.remote(code: "relay_unreachable", keys: []))])
        f.model.beginEditRelay(id: "2")
        let before = f.model.mainFocusTarget
        await f.model.saveRelay()
        XCTAssertTrue(f.model.showRelayForm)
        XCTAssertNotNil(f.model.relaySaveAlert)
        XCTAssertEqual(f.model.mainFocusTarget, before)
        XCTAssertNotEqual(f.model.mainFocusTarget, .edit("2"))
    }

    func testFormSheetDismissedBySystem_FocusesTriggerButton() async throws {
        let f = await ViewFixture.make()
        let window = AppWindows.makeMain(model: f.model)
        hideOffscreen(window)
        let controller = RelaySheetController(model: f.model, window: window)
        defer { window.orderOut(nil); _ = controller }
        f.model.beginEditRelay(id: "3")
        await awaitUntil { window.attachedSheet != nil }
        let sheet = try XCTUnwrap(window.attachedSheet)
        window.endSheet(sheet)
        await awaitUntil { !f.model.showRelayForm }
        XCTAssertEqual(f.model.mainFocusTarget, .edit("3"))
    }

    func testDeleteSheetDismissedBySystem_FocusesTriggerButton() async throws {
        let f = await ViewFixture.make()
        let window = AppWindows.makeMain(model: f.model)
        hideOffscreen(window)
        let controller = RelaySheetController(model: f.model, window: window)
        defer { window.orderOut(nil); _ = controller }
        f.model.requestDeleteRelay(id: "2")
        await awaitUntil { window.attachedSheet != nil }
        let sheet = try XCTUnwrap(window.attachedSheet)
        window.endSheet(sheet)
        await awaitUntil { f.model.pendingConfirmation == nil }
        XCTAssertEqual(f.model.mainFocusTarget, .delete("2"))
    }
}
