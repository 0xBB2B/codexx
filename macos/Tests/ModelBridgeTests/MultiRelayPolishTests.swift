import AppKit
import SwiftUI
import XCTest
@testable import ModelBridge

private let saveFailure: Result<Data, CoreError> = .failure(.remote(code: "relay_401", keys: []))

@MainActor
final class FormAlertCloseTests: XCTestCase {
    func testFormAlert_HasCloseButtonThatDismissesAndKeepsDraftAndHeight() async throws {
        let f = await ViewFixture.make(responses: ["save_relay": saveFailure])
        f.model.beginAddRelay()
        f.model.draftName = "新"
        f.model.draftBaseURL = "https://new.example.com"
        f.model.draftAPIKey = "sk-draft-0000"
        let view = RenderedView(RelayFormView(model: f.model))
        let before = view.size.height
        await f.model.saveRelay()
        XCTAssertTrue(view.has("没有保存"))
        let frame = try XCTUnwrap(view.frame(ofExactText: "关闭提示"))
        XCTAssertEqual(frame.width, 44, accuracy: 1)
        XCTAssertEqual(frame.height, 44, accuracy: 1)
        XCTAssertTrue(view.press(label: "关闭提示"))
        try? await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertFalse(view.has("没有保存"))
        XCTAssertEqual(f.model.draftName, "新")
        XCTAssertEqual(f.model.draftBaseURL, "https://new.example.com")
        XCTAssertEqual(f.model.draftAPIKey, "sk-draft-0000")
        XCTAssertTrue(f.model.showRelayForm)
        XCTAssertEqual(view.size.height, before, accuracy: 1)
    }

    func testFormAlert_401HasSingleFullStopBeforeKeepNotice() async {
        let f = await ViewFixture.make(responses: ["save_relay": saveFailure])
        f.model.beginAddRelay()
        f.model.draftName = "新"
        f.model.draftBaseURL = "https://new.example.com"
        f.model.draftAPIKey = "sk-draft-0000"
        await f.model.saveRelay()
        XCTAssertEqual(f.model.relaySaveAlert?.message, "中转服务返回 401：API Key 无效或已过期。已填的内容都还在，改好后再保存。")
    }
}

@MainActor
final class ConfirmDialogIconTests: XCTestCase {
    private let region = CGRect(x: 0, y: 0, width: 140, height: 140)

    private func assertAppIcon(_ view: RenderedView, _ label: String, file: StaticString = #filePath, line: UInt = #line) throws {
        let shot = try XCTUnwrap(view.snapshot(), label, file: file, line: line)
        let plate = try XCTUnwrap(shot.bounds(.primary, inPoints: region), label, file: file, line: line)
        XCTAssertEqual(plate.width, 20, accuracy: 1, label, file: file, line: line)
        XCTAssertEqual(plate.height, 20, accuracy: 1, label, file: file, line: line)
        XCTAssertGreaterThan(shot.fillRatio(.primary, inPoints: region), 0.5, label, file: file, line: line)
    }

    func testDeleteDialog_HeaderUses24pxAppIcon() async throws {
        let f = await ViewFixture.make()
        f.model.requestDeleteRelay(id: "2")
        try assertAppIcon(RenderedView(DeleteRelayDialogView(model: f.model)), "delete")
    }

    func testQuitUninstallAndLoginStartDialogs_HeaderUses24pxAppIcon() async throws {
        let f = await ViewFixture.make()
        await f.model.quit()
        try assertAppIcon(RenderedView(DialogView(model: ConfirmDialogModel(confirmation: try XCTUnwrap(f.model.pendingConfirmation)), onAction: { _ in })), "quit")
        f.model.cancelPending()
        await f.model.uninstall()
        try assertAppIcon(RenderedView(DialogView(model: ConfirmDialogModel(confirmation: try XCTUnwrap(f.model.pendingConfirmation)), onAction: { _ in })), "uninstall")
        f.model.cancelPending()
        await f.model.setLoginStart(false)
        try assertAppIcon(RenderedView(DialogView(model: ConfirmDialogModel(confirmation: try XCTUnwrap(f.model.pendingConfirmation)), onAction: { _ in })), "loginStartOff")
    }
}

@MainActor
final class SetupFailureTextTests: XCTestCase {
    private func failureDetail(code: String) async -> String? {
        let f = await ViewFixture.make(relayCount: 0, responses: ["save_relay": .failure(.remote(code: code, keys: ["x"]))])
        f.model.beginSetup()
        f.model.draftBaseURL = "https://new.example.com"
        f.model.draftAPIKey = "sk-draft-0000"
        await f.model.completeSetup()
        if case .failed(_, let detail) = f.model.setupPhase { return detail }
        return nil
    }

    func testSetupFailure_401EndsWithFullStop() async {
        let detail = await failureDetail(code: "relay_401")
        XCTAssertEqual(detail, "中转服务返回 401：API Key 无效或已过期。")
    }

    func testSetupFailure_CommonCoreErrorsEndWithSingleFullStop() async {
        for code in ["relay_401", "relay_unreachable", "relay_5xx", "config_invalid", "config_changed", "internal_error"] {
            let detail = await failureDetail(code: code)
            XCTAssertEqual(detail?.hasSuffix("。"), true, code)
            XCTAssertEqual(detail?.hasSuffix("。。"), false, code)
        }
    }

    func testSetupFailure_LocalStorageFailureEndsWithFullStop() async {
        let log = CallLog()
        let core = FakeCore(log: log, responses: ["save_relay": .success(Data(#"{"id":"1","name":"中转","model_count":1,"models_changed":true}"#.utf8))])
        let model = AppModel(
            core: core, keychain: RelayFakeKeychain(log: log),
            settings: RelayFakeSettings(log: log, failing: ["settings.saveRelays"]),
            loginItem: FakeLoginItem(log: log), chatGPT: FakeChatGPT(running: false)
        )
        model.beginSetup()
        model.draftBaseURL = "https://new.example.com"
        model.draftAPIKey = "sk-draft-0000"
        await model.completeSetup()
        guard case .failed(_, let detail) = model.setupPhase else { return XCTFail("setup did not fail") }
        XCTAssertTrue(detail.hasSuffix("。"))
        XCTAssertFalse(detail.hasSuffix("。。"))
    }
}

@MainActor
final class DetectingAccessForwardingTests: XCTestCase {
    func testToggleStaysEnabledWhileDetecting() async {
        let f = await ViewFixture.make()
        f.model.isDetectingAccess = true
        XCTAssertFalse(f.model.forwardingToggleDisabled)
        let view = RenderedView(MainWindow(model: f.model))
        XCTAssertEqual(view.isEnabled(label: "使用中转"), true)
        XCTAssertTrue(view.has("正在检测…"))
    }

    func testTurningOffDuringDetectionSticksAfterDetectionFinishes() async {
        let gate = Gate()
        let f = await ViewFixture.make(
            responses: [
                "detect_access": .success(Data(ViewFixture.statusJSON(forwarding: true, relayCount: 3).utf8)),
                "set_forwarding": .success(Data(ViewFixture.statusJSON(forwarding: false, relayCount: 3).utf8)),
            ],
            gates: ["detect_access": gate]
        )
        let detect = Task { await f.model.recheckAccess() }
        await awaitUntil { !f.core.calls("detect_access").isEmpty }
        let toggle = Task { await f.model.toggleForwarding(enabled: false) }
        try? await Task.sleep(nanoseconds: 100_000_000)
        await gate.open()
        await detect.value
        await toggle.value
        XCTAssertFalse(f.model.forwardingEnabled)
        XCTAssertEqual(f.core.calls("set_forwarding").count, 1)
        XCTAssertFalse(f.model.isDetectingAccess)
    }
}

@MainActor
final class SetupWindowShellTests: XCTestCase {
    func testTitleBarIs48pxWithCenteredTitleAndTrafficLights() async throws {
        let f = await ViewFixture.make(relayCount: 0)
        let window = AppWindows.makeSetup(model: f.model)
        hideOffscreen(window)
        defer { window.orderOut(nil) }
        try? await Task.sleep(nanoseconds: 200_000_000)
        window.contentView?.layoutSubtreeIfNeeded()
        let view = RenderedView(existing: window)
        let title = try XCTUnwrap(view.frame(ofExactText: "设置模型接入助手"))
        XCTAssertEqual(window.frame.maxY - title.midY, 24, accuracy: 2)
        XCTAssertEqual(title.midX, window.frame.midX, accuracy: 2)
        for kind in [NSWindow.ButtonType.closeButton, .miniaturizeButton, .zoomButton] {
            let button = try XCTUnwrap(window.standardWindowButton(kind))
            let rect = window.convertToScreen(button.convert(button.bounds, to: nil))
            XCTAssertEqual(rect.midY, title.midY, accuracy: 2, "\(kind)")
        }
    }
}

@MainActor
final class SetupCompletionWindowTests: XCTestCase {
    func testFinishingSetupClosesSetupWindowAndOpensMainWindowWithOneRelay() async throws {
        let f = await ViewFixture.make(
            relayCount: 0,
            responses: ["save_relay": .success(Data(#"{"id":"1","name":"中转","model_count":3,"models_changed":true}"#.utf8))]
        )
        let windows = AppWindowController(model: f.model, present: { hideOffscreen($0) })
        defer {
            windows.setupWindow?.orderOut(nil)
            windows.mainWindow?.orderOut(nil)
        }
        f.model.showSetup = true
        await awaitUntil { windows.setupWindow?.isVisible == true }
        XCTAssertEqual(windows.setupWindow?.isVisible, true)
        f.model.beginSetup()
        f.model.draftBaseURL = "https://new.example.com"
        f.model.draftAPIKey = "sk-draft-0000"
        await f.model.completeSetup()
        XCTAssertEqual(f.model.relays.count, 1)
        f.model.finishSetup()
        await awaitUntil { windows.mainWindow?.isVisible == true }
        XCTAssertEqual(windows.setupWindow?.isVisible, false)
        let main = try XCTUnwrap(windows.mainWindow)
        XCTAssertTrue(main.isVisible)
        try? await Task.sleep(nanoseconds: 200_000_000)
        let view = RenderedView(existing: main)
        XCTAssertEqual(view.texts.filter { $0 == "编辑" }.count, 1)
    }
}

@MainActor
final class DisabledTextFieldBorderTests: XCTestCase {
    private func color(_ shot: PixelSnapshot, xPoints: CGFloat, yPoints: CGFloat) -> NSColor? {
        shot.rep.colorAt(x: Int(xPoints * shot.scale), y: Int(yPoints * shot.scale))?.usingColorSpace(.sRGB)
    }

    private func swatch(_ c: Color) throws -> NSColor {
        let shot = try XCTUnwrap(RenderedView(c.frame(width: 20, height: 20)).snapshot())
        return try XCTUnwrap(color(shot, xPoints: 10, yPoints: 10))
    }

    private func close(_ a: NSColor, _ b: NSColor) -> Bool {
        abs(a.redComponent - b.redComponent) < 0.01 && abs(a.greenComponent - b.greenComponent) < 0.01 && abs(a.blueComponent - b.blueComponent) < 0.01
    }

    func testDisabledFieldHasGreyFillAndVisibleBorderColor() throws {
        let field = AppTextField(label: "名字", placeholder: "", text: .constant("")).disabled(true)
            .frame(width: 200, height: 44)
            .background(Color.white)
        let shot = try XCTUnwrap(RenderedView(field).snapshot())
        let border = try XCTUnwrap(color(shot, xPoints: 0.25, yPoints: 22))
        let fill = try XCTUnwrap(color(shot, xPoints: 20, yPoints: 4))
        XCTAssertTrue(close(fill, try swatch(Palette.disabledBg)), "fill")
        XCTAssertTrue(close(border, try swatch(Palette.borderStrong)), "border")
        XCTAssertFalse(close(border, fill))
    }
}
