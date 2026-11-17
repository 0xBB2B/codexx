import SwiftUI
import XCTest
@testable import ModelBridge

@MainActor
final class MenuPanelHeaderTests: XCTestCase {
    private let iconRegion = CGRect(x: 0, y: 0, width: 40, height: 80)

    private func panelSnapshot() async -> PixelSnapshot? {
        let f = await ViewFixture.make()
        return RenderedView(MenuPanel(model: f.model, openMain: {})).snapshot()
    }

    private func solidPlate(_ shot: PixelSnapshot) throws -> CGRect {
        XCTAssertGreaterThan(shot.fillRatio(.primary, inPoints: iconRegion), 0.5)
        return try XCTUnwrap(shot.bounds(.primary, inPoints: iconRegion))
    }

    func testHeaderHasSolidAppIconPlateInsteadOfSystemSymbol() async throws {
        let snapshot = await panelSnapshot()
        let shot = try XCTUnwrap(snapshot)
        let plate = try solidPlate(shot)
        XCTAssertEqual(plate.width, 14, accuracy: 1)
        XCTAssertEqual(plate.height, 14, accuracy: 1)
    }

    func testHeaderIconSitsAtPanelPaddingPlusHeaderPadding() async throws {
        let snapshot = await panelSnapshot()
        let shot = try XCTUnwrap(snapshot)
        let plate = try solidPlate(shot)
        XCTAssertEqual(plate.minX, 4 + 12 + 1, accuracy: 0.5)
    }

    func testIconToTitleGapIs8() async throws {
        let snapshot = await panelSnapshot()
        let shot = try XCTUnwrap(snapshot)
        let plate = try solidPlate(shot)
        let titleRegion = CGRect(x: plate.maxX + 1, y: 0, width: 120, height: 80)
        let title = try XCTUnwrap(shot.bounds(.darkText, inPoints: titleRegion))

        let reference = RenderedView(
            HStack(spacing: 8) {
                AppIconView(size: 16)
                Text("模型接入助手").font(Fonts.label).foregroundColor(Palette.text)
            }
            .padding(16)
            .background(Color.white)
        )
        let refShot = try XCTUnwrap(reference.snapshot())
        let refRegion = CGRect(x: 0, y: 0, width: 40, height: 80)
        let refPlate = try XCTUnwrap(refShot.bounds(.primary, inPoints: refRegion))
        let refTitle = try XCTUnwrap(refShot.bounds(.darkText, inPoints: CGRect(x: refPlate.maxX + 1, y: 0, width: 120, height: 80)))
        XCTAssertEqual(title.minX - plate.maxX, refTitle.minX - refPlate.maxX, accuracy: 0.5)
    }

    func testHeaderTextBlockMatchesReferenceTopPaddingAndFonts() async throws {
        let f = await ViewFixture.make()
        let panel = MenuPanelModel(model: f.model)
        let actualShot = RenderedView(MenuPanel(model: f.model, openMain: {})).snapshot()
        let actual = try XCTUnwrap(actualShot)
        let reference = RenderedView(
            HStack(spacing: 8) {
                Color.clear.frame(width: 16, height: 16)
                VStack(alignment: .leading, spacing: 0) {
                    Text(panel.title).font(.system(size: 13, weight: .semibold)).foregroundColor(Color(hex: 0x172b3a))
                    Text(panel.subtitle).font(.system(size: 12)).foregroundColor(Color(hex: 0x536575))
                }
                Spacer(minLength: 0)
            }
            .padding(12)
            .padding(4)
            .frame(width: 286, alignment: .topLeading)
            .background(Color.white)
        )
        let expectedShot = reference.snapshot()
        let expected = try XCTUnwrap(expectedShot)
        let region = CGRect(x: 36, y: 0, width: 240, height: 56)
        XCTAssertEqual(actual.scale, expected.scale)
        var mismatches = 0
        for y in Int(region.minY * actual.scale)..<Int(region.maxY * actual.scale) {
            for x in Int(region.minX * actual.scale)..<Int(region.maxX * actual.scale) {
                guard let a = actual.rep.colorAt(x: x, y: y)?.usingColorSpace(.sRGB),
                      let b = expected.rep.colorAt(x: x, y: y)?.usingColorSpace(.sRGB) else { continue }
                let delta = max(abs(a.redComponent - b.redComponent), abs(a.greenComponent - b.greenComponent), abs(a.blueComponent - b.blueComponent))
                if delta > 0.15 { mismatches += 1 }
            }
        }
        XCTAssertLessThanOrEqual(mismatches, 3)
    }
}
