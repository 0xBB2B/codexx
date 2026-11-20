import AppKit
import SwiftUI
import XCTest
@testable import ModelBridge

@MainActor
final class RelaySheetFrameTests: XCTestCase {
    private func openSheet(_ open: (AppModel) -> Void) async -> (NSWindow, RelaySheetController, NSWindow, ViewFixture)? {
        let f = await ViewFixture.make()
        let window = AppWindows.makeMain(model: f.model)
        hideOffscreen(window)
        let controller = RelaySheetController(model: f.model, window: window)
        open(f.model)
        await awaitUntil { window.attachedSheet != nil }
        guard let sheet = window.attachedSheet else { return nil }
        sheet.contentView?.layoutSubtreeIfNeeded()
        return (window, controller, sheet, f)
    }

    private func cardHeight<V: View>(_ view: V) -> CGFloat {
        let host = NSHostingView(rootView: view)
        host.layoutSubtreeIfNeeded()
        return host.fittingSize.height
    }

    private func cornersAreTransparent(_ sheet: NSWindow) -> Bool {
        guard let view = sheet.contentView,
              let rep = view.bitmapImageRepForCachingDisplay(in: view.bounds) else { return false }
        view.cacheDisplay(in: view.bounds, to: rep)
        let corners = [(0, 0), (rep.pixelsWide - 1, 0), (0, rep.pixelsHigh - 1), (rep.pixelsWide - 1, rep.pixelsHigh - 1)]
        return corners.allSatisfy { (rep.colorAt(x: $0.0, y: $0.1)?.alphaComponent ?? 1) == 0 }
    }

    func testAddSheet_SizeEqualsCard() async throws {
        let opened = await openSheet { $0.beginAddRelay() }
        let (window, controller, sheet, f) = try XCTUnwrap(opened)
        defer { window.orderOut(nil); withExtendedLifetime(controller) {} }
        XCTAssertEqual(sheet.frame.width, 520, accuracy: 0.5)
        XCTAssertEqual(sheet.frame.height, cardHeight(RelayFormView(model: f.model)), accuracy: 0.5)
    }

    func testEditSheet_SizeEqualsCard() async throws {
        let opened = await openSheet { $0.beginEditRelay(id: "2") }
        let (window, controller, sheet, f) = try XCTUnwrap(opened)
        defer { window.orderOut(nil); withExtendedLifetime(controller) {} }
        XCTAssertEqual(sheet.frame.width, 520, accuracy: 0.5)
        XCTAssertEqual(sheet.frame.height, cardHeight(RelayFormView(model: f.model)), accuracy: 0.5)
    }

    func testDeleteSheet_SizeEqualsCard() async throws {
        let opened = await openSheet { $0.requestDeleteRelay(id: "2") }
        let (window, controller, sheet, f) = try XCTUnwrap(opened)
        defer { window.orderOut(nil); withExtendedLifetime(controller) {} }
        XCTAssertEqual(sheet.frame.width, 520, accuracy: 0.5)
        XCTAssertEqual(sheet.frame.height, cardHeight(DeleteRelayDialogView(model: f.model)), accuracy: 0.5)
    }

    func testAddSheet_NoSelfDrawnShadow() async throws {
        let opened = await openSheet { $0.beginAddRelay() }
        let (window, controller, sheet, _) = try XCTUnwrap(opened)
        defer { window.orderOut(nil); withExtendedLifetime(controller) {} }
        XCTAssertTrue(cornersAreTransparent(sheet))
    }

    func testEditSheet_NoSelfDrawnShadow() async throws {
        let opened = await openSheet { $0.beginEditRelay(id: "2") }
        let (window, controller, sheet, _) = try XCTUnwrap(opened)
        defer { window.orderOut(nil); withExtendedLifetime(controller) {} }
        XCTAssertTrue(cornersAreTransparent(sheet))
    }

    func testDeleteSheet_NoSelfDrawnShadow() async throws {
        let opened = await openSheet { $0.requestDeleteRelay(id: "2") }
        let (window, controller, sheet, _) = try XCTUnwrap(opened)
        defer { window.orderOut(nil); withExtendedLifetime(controller) {} }
        XCTAssertTrue(cornersAreTransparent(sheet))
    }

    func testSheet_StillAttachedAndClosesOnCancel() async throws {
        let opened = await openSheet { $0.beginAddRelay() }
        let (window, controller, _, f) = try XCTUnwrap(opened)
        defer { window.orderOut(nil); withExtendedLifetime(controller) {} }
        XCTAssertEqual(window.sheets.count, 1)
        f.model.cancelRelayForm()
        await awaitUntil { window.attachedSheet == nil }
        XCTAssertNil(window.attachedSheet)
    }
}
