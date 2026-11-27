import AppKit
import XCTest
@testable import ModelBridge

private let revisionTitle = "官方模型已按当前账号更新"
private let revisionDetail = "重启 ChatGPT 后生效。"
private let savedTitle = "已保存「中转」"
private let backendTitle = "后台服务没能启动，请退出后重新打开"

private func statusJSON(revision: Int? = nil, forwarding: Bool = true, mode: String = "both", marker: Int = 0) -> String {
    let field = revision.map { #""official_models_revision":\#($0),"# } ?? ""
    let entry = #"{"id":"1","name":"中转","model_count":6,"fetched_at":"2026-11-16T00:12:00Z","stale":false,"error":null}"#
    return """
    {"version":"1.3.\(marker)",\(field)"forwarding":\(forwarding),"proxy":{"running":true,"error":null},\
    "chatgpt":{"found":true,"error":null},"default_relay":"中转","relays":[\(entry)],\
    "taken_over":[],"restore_blocked":[],"config_path":"/Users/example/.codex/config.toml",\
    "update":{"latest_version":null,"url":null},\
    "access":{"mode":"\(mode)","target":"both","detect":"logged_in","relay_only":false,"logged_in":true}}
    """
}

private func decodeStatus(_ json: String) -> CoreStatus {
    let decoder = JSONDecoder()
    decoder.keyDecodingStrategy = .convertFromSnakeCase
    return try! decoder.decode(CoreStatus.self, from: Data(json.utf8))
}

private struct Bar {
    let frame: NSRect
    let own: Set<String>
}

private func signature(_ element: NSObject) -> String {
    "\(element.axString("accessibilityRole") ?? "")|\(element.axString("accessibilityLabel") ?? "")|\(NSStringFromRect(element.axFrame() ?? .zero))"
}

private func descendants(_ object: NSObject) -> [NSObject] {
    let children = (object.axObject("accessibilityChildren") as? [NSObject]) ?? []
    return children.flatMap { [$0] + descendants($0) }
}

private func axTexts(of element: NSObject) -> [String] {
    ["accessibilityLabel", "accessibilityValue"].compactMap { element.axString($0) }
}

extension RenderedView {
    fileprivate func bar(titled title: String) -> Bar? {
        let found = elements.dropFirst()
            .filter { $0.axString("accessibilityRole") == NSAccessibility.Role.group.rawValue }
            .compactMap { group -> (NSObject, [NSObject])? in
                let members = descendants(group)
                return members.contains { axTexts(of: $0).contains(title) } ? (group, members) : nil
            }
        let best = found.min { lhs, rhs in
            let a = lhs.0.axFrame() ?? .zero, b = rhs.0.axFrame() ?? .zero
            return a.width * a.height < b.width * b.height
        }
        guard let (group, members) = best, let frame = group.axFrame() else { return nil }
        return Bar(frame: frame, own: Set(members.map(signature)))
    }

    fileprivate func buttons(excluding bars: [Bar]) -> [(label: String, frame: NSRect)] {
        elements.compactMap { element in
            guard element.axString("accessibilityRole") == NSAccessibility.Role.button.rawValue,
                  let label = element.axString("accessibilityLabel"), !label.isEmpty,
                  !bars.contains(where: { $0.own.contains(signature(element)) }),
                  let frame = element.axFrame() else { return nil }
            return (label, frame)
        }
    }
}

@MainActor
private func assertStack(_ bars: [Bar], in window: NSWindow, file: StaticString = #filePath, line: UInt = #line) {
    var expectedTop = window.frame.maxY - AppWindows.titleBarHeight - 12
    for (index, bar) in bars.enumerated() {
        XCTAssertEqual(bar.frame.minX - window.frame.minX, 24, accuracy: 1.5, "bar \(index) 距左边", file: file, line: line)
        XCTAssertEqual(bar.frame.width, 376, accuracy: 1.5, "bar \(index) 宽度", file: file, line: line)
        XCTAssertEqual(bar.frame.maxY, expectedTop, accuracy: 1.5, "bar \(index) 距上方", file: file, line: line)
        expectedTop = bar.frame.minY - 8
    }
}

@MainActor
private struct Rig {
    let model: AppModel
    let core: FakeCore

    static func make() async -> Rig {
        let log = CallLog()
        let core = FakeCore(log: log, responses: [
            "save_relay": .success(Data(#"{"id":"1","name":"中转","model_count":6,"models_changed":true}"#.utf8)),
        ])
        let relay = RelayConfig(id: "1", name: "中转", baseURL: "https://a.example.com")
        let model = AppModel(
            core: core,
            keychain: RelayFakeKeychain(log: log, entries: ["1": "sk-example-1"]),
            settings: RelayFakeSettings(log: log, relays: [relay]),
            loginItem: FakeLoginItem(log: log),
            chatGPT: FakeChatGPT(running: false)
        )
        await model.start()
        model.noticeAutoDismissDelay = 60
        return Rig(model: model, core: core)
    }

    private static var counter = 0

    func push(_ revision: Int?, forwarding: Bool = true, mode: String = "both") async {
        Self.counter += 1
        let marker = Self.counter
        core.statusContinuation.yield(decodeStatus(statusJSON(revision: revision, forwarding: forwarding, mode: mode, marker: marker)))
        await awaitUntil { model.status?.version == "1.3.\(marker)" }
        try? await Task.sleep(nanoseconds: 60_000_000)
    }

    func saveRelay() async {
        model.beginEditRelay(id: "1")
        await model.saveRelay()
    }

    func restartCore() async {
        model.coreDidFail()
        core.lifecycleContinuation.yield(.restarted)
        await awaitUntil { model.coreRunning }
    }

    var revisionNotices: [Notice] { model.notices.filter { $0.title == revisionTitle } }
}

@MainActor
final class AccountModelNoticeTests: XCTestCase {
    func testFirstStatusWithRevisionZero_NoNotice() async {
        let rig = await Rig.make()
        await rig.push(0)
        XCTAssertTrue(rig.model.notices.isEmpty)
    }

    func testFirstStatusWithNonZeroRevision_NoNotice() async {
        let rig = await Rig.make()
        await rig.push(5)
        XCTAssertTrue(rig.model.notices.isEmpty)
    }

    func testRevisionIncreases_SuccessNoticeWithTitleDetailAndNoAction() async throws {
        let rig = await Rig.make()
        await rig.push(0)
        await rig.push(1)

        XCTAssertEqual(rig.model.notices.count, 1)
        let notice = try XCTUnwrap(rig.model.notices.first)
        XCTAssertEqual(notice.title, revisionTitle)
        XCTAssertEqual(notice.detail, revisionDetail)
        XCTAssertEqual(notice.kind, .success)
        XCTAssertNil(notice.detailDialogTitle)
    }

    func testSameRevisionPushedAgain_NoSecondNotice() async throws {
        let rig = await Rig.make()
        await rig.push(0)
        await rig.push(1)
        let first = try XCTUnwrap(rig.model.notices.first)
        await rig.push(1)

        XCTAssertEqual(rig.model.notices, [first])
    }

    func testRevisionDecreases_RecordKeepsMax_OnlyHigherThanMaxNotifies() async {
        let rig = await Rig.make()
        await rig.push(3)
        await rig.push(1)
        XCTAssertTrue(rig.model.notices.isEmpty)

        await rig.push(2)
        XCTAssertTrue(rig.model.notices.isEmpty, "没超过历史最大值 3")

        await rig.push(4)
        XCTAssertEqual(rig.revisionNotices.count, 1)
    }

    func testMissingField_TreatedAsZero() async {
        let rig = await Rig.make()
        await rig.push(nil)
        XCTAssertTrue(rig.model.notices.isEmpty)
        await rig.push(1)
        XCTAssertEqual(rig.revisionNotices.count, 1)
    }

    func testMissingFieldAfterPositiveRevision_CountsAsZeroAndKeepsMax() async {
        let rig = await Rig.make()
        await rig.push(2)
        await rig.push(nil)
        XCTAssertTrue(rig.model.notices.isEmpty)
        await rig.push(1)
        XCTAssertTrue(rig.model.notices.isEmpty, "没超过历史最大值 2")
        await rig.push(3)
        XCTAssertEqual(rig.revisionNotices.count, 1)
    }

    func testNoticeSurvivesAutoDismissDelay_OtherSuccessNoticeDoesNot() async throws {
        let rig = await Rig.make()
        rig.model.noticeAutoDismissDelay = 0.1
        await rig.push(0)
        await rig.push(1)
        await rig.saveRelay()
        XCTAssertEqual(rig.model.notices.map(\.title), [revisionTitle, savedTitle])

        try await Task.sleep(nanoseconds: 500_000_000)

        XCTAssertEqual(rig.model.notices.map(\.title), [revisionTitle])
    }

    func testDismissNotice_Removes() async throws {
        let rig = await Rig.make()
        await rig.push(0)
        await rig.push(1)
        let notice = try XCTUnwrap(rig.model.notices.first)

        rig.model.dismissNotice(notice.id)

        XCTAssertTrue(rig.model.notices.isEmpty)
    }

    func testRevisionIncreasesAgain_ReplacesInPlaceWithOthersUntouched() async throws {
        let rig = await Rig.make()
        await rig.push(0)
        await rig.push(1)
        rig.model.coreDidFail()
        XCTAssertEqual(rig.model.notices.map(\.title), [revisionTitle, backendTitle])
        let errorBefore = try XCTUnwrap(rig.model.notices.first { $0.title == backendTitle })
        let firstID = try XCTUnwrap(rig.revisionNotices.first).id

        await rig.push(2)

        XCTAssertEqual(rig.model.notices.map(\.title), [revisionTitle, backendTitle])
        XCTAssertNotEqual(rig.revisionNotices.first?.id, firstID, "应是新提示")
        XCTAssertEqual(rig.model.notices.last, errorBefore)
    }

    func testSequence0_1_1_2_ThenForwardingOff_OneNoticeThenRemoved() async {
        let rig = await Rig.make()
        await rig.push(0)
        await rig.push(1)
        await rig.push(1)
        XCTAssertEqual(rig.revisionNotices.count, 1)
        await rig.push(2)
        XCTAssertEqual(rig.revisionNotices.count, 1)

        await rig.push(2, forwarding: false)

        XCTAssertTrue(rig.revisionNotices.isEmpty)
    }

    func testForwardingOff_RemovesOnlyThisNotice() async {
        let rig = await Rig.make()
        await rig.push(0)
        await rig.push(1)
        rig.model.coreDidFail()
        XCTAssertEqual(rig.revisionNotices.count, 1)

        await rig.push(1, forwarding: false)

        XCTAssertEqual(rig.model.notices.map(\.title), [backendTitle])
    }

    func testAccessModeBecomesRelayOnly_RemovesNotice() async {
        let rig = await Rig.make()
        await rig.push(0)
        await rig.push(1)
        XCTAssertEqual(rig.revisionNotices.count, 1)

        await rig.push(1, mode: "relay")

        XCTAssertTrue(rig.revisionNotices.isEmpty)
    }

    func testLateLowerRevisionAfterDismiss_SameRunDoesNotReshowNotice() async throws {
        let rig = await Rig.make()
        await rig.push(0)
        await rig.push(1)
        rig.model.dismissNotice(try XCTUnwrap(rig.revisionNotices.first).id)

        await rig.push(0)
        await rig.push(1)

        XCTAssertTrue(rig.revisionNotices.isEmpty)
    }

    func testCoreRestart_FirstStatusOnlyRecordsEvenIfHigher() async throws {
        let rig = await Rig.make()
        await rig.push(0)
        await rig.push(1)
        rig.model.dismissNotice(try XCTUnwrap(rig.revisionNotices.first).id)

        await rig.restartCore()
        await rig.push(2)

        XCTAssertTrue(rig.revisionNotices.isEmpty)
    }

    func testCoreRestart_RevisionRestartsFromZeroAndStillNotifies() async throws {
        let rig = await Rig.make()
        await rig.push(0)
        await rig.push(3)
        rig.model.dismissNotice(try XCTUnwrap(rig.revisionNotices.first).id)

        await rig.restartCore()
        await rig.push(0)
        XCTAssertTrue(rig.revisionNotices.isEmpty)
        await rig.push(1)

        XCTAssertEqual(rig.revisionNotices.count, 1)
    }
}

@MainActor
final class AccountModelNoticeTestsMainWindow: XCTestCase {
    private struct Window {
        let rig: Rig
        let window: NSWindow
        let view: RenderedView
    }

    private func openMain() async -> Window {
        let rig = await Rig.make()
        await rig.push(0)
        let window = AppWindows.makeMain(model: rig.model)
        hideOffscreen(window)
        try? await Task.sleep(nanoseconds: 200_000_000)
        window.contentView?.layoutSubtreeIfNeeded()
        return Window(rig: rig, window: window, view: RenderedView(existing: window))
    }

    private func settle(_ w: Window) async {
        try? await Task.sleep(nanoseconds: 150_000_000)
        w.window.contentView?.layoutSubtreeIfNeeded()
    }

    func testNotice_TopLeftAt376Wide_DoesNotCoverControls() async throws {
        let w = await openMain()
        defer { w.window.orderOut(nil) }
        await w.rig.push(1)
        await settle(w)

        let bar = try XCTUnwrap(w.view.bar(titled: revisionTitle))
        assertStack([bar], in: w.window)
        let controls = w.view.buttons(excluding: [bar])
        XCTAssertFalse(controls.isEmpty)
        for control in controls {
            XCTAssertTrue(NSIntersectionRect(bar.frame, control.frame).isEmpty, "提示条盖住了「\(control.label)」")
        }
    }

    func testNoticeThenSavedNotice_StackedInOrderWithGap8_AndStaysAfterSavedDismisses() async throws {
        let w = await openMain()
        defer { w.window.orderOut(nil) }
        await w.rig.push(1)
        w.rig.model.noticeAutoDismissDelay = 0.4
        await w.rig.saveRelay()
        await settle(w)

        let both = [try XCTUnwrap(w.view.bar(titled: revisionTitle)), try XCTUnwrap(w.view.bar(titled: savedTitle))]
        assertStack(both, in: w.window)

        try await Task.sleep(nanoseconds: 700_000_000)
        await settle(w)

        XCTAssertNil(w.view.bar(titled: savedTitle))
        assertStack([try XCTUnwrap(w.view.bar(titled: revisionTitle))], in: w.window)
    }

    func testReplacedNotice_StaysAtSamePosition() async throws {
        let w = await openMain()
        defer { w.window.orderOut(nil) }
        await w.rig.push(1)
        await w.rig.saveRelay()
        await w.rig.push(2)
        await settle(w)

        let both = [try XCTUnwrap(w.view.bar(titled: revisionTitle)), try XCTUnwrap(w.view.bar(titled: savedTitle))]
        assertStack(both, in: w.window)
    }
}
