import XCTest
@testable import ModelBridge

private let infoTitle = "接入方式没有变化"
private let warningTitle = "没能检测 ChatGPT 登录状态"
private let deleteFailedTitle = "没有删除「备用」"
private let configChangedText = "Codex 配置文件刚被其他程序修改，请重试"
private let internalErrorText = "内部错误，请重试"
private let relayStaysText = "仍然没有检测到 ChatGPT 登录，继续只用中转模型。"
private let relayOnlyStaysText = "仍然检测到 ChatGPT 登录。你手动选了只用中转，继续只用中转模型。"


private func relayEntry(_ id: String, _ name: String) -> String {
    #"{"id":"\#(id)","name":"\#(name)","model_count":6,"fetched_at":"2026-11-16T00:12:00Z","stale":false,"error":null}"#
}

private func statusJSON(mode: String = "relay", detect: String = "logged_in", relayOnly: Bool = false, loggedIn: Bool = false) -> String {
    """
    {"version":"1.3.0","forwarding":true,"proxy":{"running":true,"error":null},\
    "chatgpt":{"found":true,"error":null},"default_relay":"中转",\
    "relays":[\(relayEntry("1", "中转")),\(relayEntry("2", "备用"))],\
    "taken_over":[],"restore_blocked":[],"config_path":"/Users/example/.codex/config.toml",\
    "update":{"latest_version":null,"url":null},\
    "access":{"mode":"\(mode)","target":"\(mode)","detect":"\(detect)",\
    "relay_only":\(relayOnly),"logged_in":\(loggedIn)}}
    """
}

private func ok(_ text: String) -> Result<Data, CoreError> { .success(Data(text.utf8)) }

@MainActor
private func makeModel(core: FakeCore, log: CallLog) async -> AppModel {
    let model = AppModel(
        core: core,
        keychain: RelayFakeKeychain(log: log, entries: ["1": "sk-example-1", "2": "sk-example-2"]),
        settings: RelayFakeSettings(log: log, relays: [relayOne, relayTwo]),
        loginItem: FakeLoginItem(log: log),
        chatGPT: FakeChatGPT(running: false)
    )
    await model.start()
    let decoder = JSONDecoder()
    decoder.keyDecodingStrategy = .convertFromSnakeCase
    model.status = try! decoder.decode(CoreStatus.self, from: Data(statusJSON().utf8))
    return model
}

@MainActor
private func deleteBackup(_ model: AppModel) async {
    model.requestDeleteRelay(id: "2")
    await model.confirmPending()
}

@MainActor
final class NoticeReplaceTests: XCTestCase {
    private func make(
        queued: [String: [Result<Data, CoreError>]] = [:]
    ) async -> (AppModel, FakeCore) {
        let log = CallLog()
        let core = FakeCore(log: log)
        queued.forEach { core.enqueue($0.key, $0.value) }
        return (await makeModel(core: core, log: log), core)
    }

    func testDeleteFailsTwice_OneErrorNoticeWithNewDetail() async throws {
        let (model, _) = await make(queued: ["delete_relay": [
            .failure(.remote(code: "config_changed", keys: [])),
            .failure(.remote(code: "internal_error", keys: [])),
        ]])
        await deleteBackup(model)
        XCTAssertEqual(model.notices.map(\.detail), [configChangedText])
        await deleteBackup(model)

        XCTAssertEqual(model.notices.count, 1)
        let notice = try XCTUnwrap(model.notices.first)
        XCTAssertEqual(notice.title, deleteFailedTitle)
        XCTAssertEqual(notice.kind, .error)
        XCTAssertEqual(notice.detail, internalErrorText)
    }

    func testDeleteFailsTwice_ReplacedNoticeStaysAtIndexAndOthersUntouched() async throws {
        let (model, core) = await make(queued: ["delete_relay": [
            .failure(.remote(code: "config_changed", keys: [])),
            .failure(.remote(code: "internal_error", keys: [])),
        ]])
        core.enqueue("detect_access", [ok(statusJSON(detect: "failed"))])
        await deleteBackup(model)
        await model.recheckAccess()
        XCTAssertEqual(model.notices.map(\.title), [deleteFailedTitle, warningTitle])
        let warningBefore = model.notices[1]

        await deleteBackup(model)

        XCTAssertEqual(model.notices.map(\.title), [deleteFailedTitle, warningTitle])
        XCTAssertEqual(model.notices[0].detail, internalErrorText)
        XCTAssertEqual(model.notices[1], warningBefore)
    }

    func testRecheckTwice_InfoNoticeReplacedInPlaceWithNewDetail() async throws {
        let (model, core) = await make(queued: ["detect_access": [
            ok(statusJSON(relayOnly: false, loggedIn: false)),
            ok(statusJSON(relayOnly: true, loggedIn: true)),
        ]])
        await model.recheckAccess()
        XCTAssertEqual(model.notices.map(\.detail), [relayStaysText])
        await model.recheckAccess()

        XCTAssertEqual(core.calls("detect_access").count, 2)
        XCTAssertEqual(model.notices.count, 1)
        XCTAssertEqual(model.notices[0].title, infoTitle)
        XCTAssertEqual(model.notices[0].kind, .info)
        XCTAssertEqual(model.notices[0].detail, relayOnlyStaysText)
    }

    func testInfoReplacedBehindError_KeepsIndexOfBoth() async throws {
        let (model, _) = await make(queued: [
            "delete_relay": [.failure(.remote(code: "config_changed", keys: []))],
            "detect_access": [
                ok(statusJSON(relayOnly: false, loggedIn: false)),
                ok(statusJSON(relayOnly: true, loggedIn: true)),
            ],
        ])
        await deleteBackup(model)
        await model.recheckAccess()
        XCTAssertEqual(model.notices.map(\.title), [deleteFailedTitle, infoTitle])

        await model.recheckAccess()

        XCTAssertEqual(model.notices.map(\.title), [deleteFailedTitle, infoTitle])
        XCTAssertEqual(model.notices[0].detail, configChangedText)
        XCTAssertEqual(model.notices[1].detail, relayOnlyStaysText)
    }

    func testReplacedInfoNotice_OldTimerDoesNotRemoveNewOne_NewTimerDoes() async throws {
        let (model, _) = await make(queued: ["detect_access": [
            ok(statusJSON(relayOnly: false, loggedIn: false)),
            ok(statusJSON(relayOnly: true, loggedIn: true)),
        ]])
        model.noticeAutoDismissDelay = 0.6
        await model.recheckAccess()
        try await Task.sleep(nanoseconds: 350_000_000)
        await model.recheckAccess()
        XCTAssertEqual(model.notices.map(\.detail), [relayOnlyStaysText])

        try await Task.sleep(nanoseconds: 400_000_000)
        XCTAssertEqual(model.notices.map(\.detail), [relayOnlyStaysText], "旧计时到点后新提示仍应在")

        await awaitUntil { model.notices.isEmpty }
        XCTAssertTrue(model.notices.isEmpty, "新计时到点后新提示应消失")
    }

    func testReplacedErrorNotice_NeverAutoDismisses() async throws {
        let (model, _) = await make(queued: ["delete_relay": [
            .failure(.remote(code: "config_changed", keys: [])),
            .failure(.remote(code: "internal_error", keys: [])),
        ]])
        model.noticeAutoDismissDelay = 0.1
        await deleteBackup(model)
        await deleteBackup(model)
        try await Task.sleep(nanoseconds: 400_000_000)

        XCTAssertEqual(model.notices.map(\.detail), [internalErrorText])
    }

    func testReplacedWarningWithDetailDialog_TakesNewDetailAndKeepsAction() async throws {
        let (model, _) = await make()
        let first = URL(fileURLWithPath: "/Users/example/first.bak")
        let second = URL(fileURLWithPath: "/Users/example/second.bak")
        await model.start(stored: StoredRelays(relays: [], keys: [:], recovery: .backedUp(first)))
        await model.start(stored: StoredRelays(relays: [], keys: [:], recovery: .backedUp(second)))

        let notices = model.notices.filter { $0.title == "原来的设置读不出来，已备份原文件" }
        XCTAssertEqual(notices.count, 1)
        let notice = try XCTUnwrap(notices.first)
        XCTAssertEqual(notice.detailDialogTitle, "原来的设置读不出来")
        XCTAssertTrue(notice.detail?.contains(second.path) == true)
        XCTAssertFalse(notice.detail?.contains(first.path) == true)
    }
}
