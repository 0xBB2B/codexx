import AppKit
import SwiftUI
import XCTest
@testable import ModelBridge

private final class KeyableWindow: NSWindow {
    override var canBecomeKey: Bool { true }
}

extension NSObject {
    func axObject(_ name: String) -> AnyObject? {
        let selector = NSSelectorFromString(name)
        guard responds(to: selector) else { return nil }
        return perform(selector)?.takeUnretainedValue()
    }

    func axString(_ name: String) -> String? {
        let value = axObject(name)
        return (value as? String) ?? (value as? NSString).map(String.init)
    }

    func axBool(_ name: String) -> Bool? {
        let selector = NSSelectorFromString(name)
        guard responds(to: selector) else { return nil }
        typealias Getter = @convention(c) (AnyObject, Selector) -> Bool
        return unsafeBitCast(method(for: selector), to: Getter.self)(self, selector)
    }

    func axFrame() -> NSRect? {
        let selector = NSSelectorFromString("accessibilityFrame")
        guard responds(to: selector) else { return nil }
        typealias Getter = @convention(c) (AnyObject, Selector) -> NSRect
        return unsafeBitCast(method(for: selector), to: Getter.self)(self, selector)
    }
}

@MainActor
final class RenderedView {
    private let window: NSWindow
    let host: NSView

    init(existing window: NSWindow) {
        NSApplication.shared.setAccessibilityEnabled(true)
        NSApplication.shared.setValue(true, forKey: "accessibilityEnhancedUserInterface")
        self.window = window
        host = window.contentView ?? NSView()
    }

    init<V: View>(_ view: V) {
        NSApplication.shared.setAccessibilityEnabled(true)
        NSApplication.shared.setValue(true, forKey: "accessibilityEnhancedUserInterface")
        host = NSHostingView(rootView: view)
        window = KeyableWindow(contentRect: .zero, styleMask: [.borderless], backing: .buffered, defer: false)
        window.contentView = host
        window.isReleasedWhenClosed = false
        host.layoutSubtreeIfNeeded()
    }

    func makeKeyOffscreen() {
        window.setFrame(NSRect(origin: CGPoint(x: -20000, y: -20000), size: host.fittingSize), display: false)
        window.makeKeyAndOrderFront(nil)
    }

    func close() { window.orderOut(nil) }

    func snapshot() -> PixelSnapshot? {
        window.setContentSize(host.fittingSize)
        host.layoutSubtreeIfNeeded()
        guard let rep = host.bitmapImageRepForCachingDisplay(in: host.bounds) else { return nil }
        host.cacheDisplay(in: host.bounds, to: rep)
        return PixelSnapshot(rep: rep, scale: CGFloat(rep.pixelsWide) / host.bounds.width)
    }

    func frame(ofText text: String) -> NSRect? {
        let match = elements.first { element in
            ["accessibilityLabel", "accessibilityValue"].contains { element.axString($0)?.contains(text) == true }
        }
        return match?.axFrame()
    }

    func frame(ofExactText text: String) -> NSRect? {
        let match = elements.first { element in
            ["accessibilityLabel", "accessibilityValue"].contains { element.axString($0) == text }
        }
        return match?.axFrame()
    }

    @discardableResult
    func press(label: String) -> Bool {
        guard let match = elements.first(where: { $0.axString("accessibilityLabel") == label }) else { return false }
        let selector = NSSelectorFromString("accessibilityPerformPress")
        guard match.responds(to: selector) else { return false }
        typealias Press = @convention(c) (AnyObject, Selector) -> Bool
        return unsafeBitCast(match.method(for: selector), to: Press.self)(match, selector)
    }

    func isFocused(label: String) -> Bool? {
        let match = elements.first { $0.axString("accessibilityLabel") == label }
        return match.flatMap { $0.axBool("isAccessibilityFocused") }
    }

    var size: CGSize {
        host.layoutSubtreeIfNeeded()
        return host.fittingSize
    }

    var elements: [NSObject] {
        host.layoutSubtreeIfNeeded()
        return Self.walk(host)
    }

    var texts: [String] {
        elements.flatMap { element in
            ["accessibilityLabel", "accessibilityTitle", "accessibilityValue"]
                .compactMap { element.axString($0) }
                .filter { !$0.isEmpty }
        }
    }

    func has(_ text: String) -> Bool {
        Self.squash(texts.joined()).contains(Self.squash(text))
    }

    func hasExactly(_ text: String) -> Bool { texts.contains(text) }

    func isEnabled(label: String) -> Bool? {
        let match = elements.first { $0.axString("accessibilityLabel") == label }
        return match.flatMap { $0.axBool("isAccessibilityEnabled") }
    }

    func order(ofExactly text: String) -> Int? {
        texts.firstIndex(of: text)
    }

    func firstTextFieldIsFocused() -> Bool? {
        let field = elements.first { $0.axString("accessibilityRole") == NSAccessibility.Role.textField.rawValue }
        return field.flatMap { $0.axBool("isAccessibilityFocused") }
    }

    private static func squash(_ text: String) -> String {
        text.filter { !$0.isWhitespace }
    }

    private static func walk(_ object: NSObject) -> [NSObject] {
        var result = [object]
        let children = (object.axObject("accessibilityChildren") as? [NSObject]) ?? []
        for child in children { result += walk(child) }
        return result
    }
}

private func entryJSON(_ id: String, _ name: String, count: Int, fetched: Bool, stale: Bool) -> String {
    let fetchedAt = fetched ? "\"2026-11-16T00:12:00Z\"" : "null"
    return #"{"id":"\#(id)","name":"\#(name)","model_count":\#(count),"fetched_at":\#(fetchedAt),"stale":\#(stale),"error":null}"#
}

@MainActor
struct ViewFixture {
    static func statusJSON(forwarding: Bool, relayCount: Int, mode: String = "both", target: String? = nil) -> String {
        let entries = (0..<relayCount).map { entryJSON("\($0 + 1)", "配置\($0 + 1)", count: 6, fetched: true, stale: false) }
        return """
        {"version":"1.3.0","forwarding":\(forwarding),"proxy":{"running":true,"error":null},\
        "chatgpt":{"found":true,"error":null},"default_relay":"配置1","relays":[\(entries.joined(separator: ","))],\
        "taken_over":[],"restore_blocked":[],"config_path":"/Users/example/.codex/config.toml",\
        "update":{"latest_version":null,"url":null},"access":{"mode":"\(mode)","target":"\(target ?? mode)","detect":"logged_in"}}
        """
    }

    let model: AppModel
    let core: FakeCore

    static func make(
        relayCount: Int = 3, forwarding: Bool = true, mode: String = "both",
        failingIDs: Set<String> = [], firstName: String? = nil, responses: [String: Result<Data, CoreError>] = [:],
        gates: [String: Gate] = [:]
    ) async -> ViewFixture {
        let relays = (0..<relayCount).map { $0 + 1 }.map {
            RelayConfig(id: "\($0)", name: $0 == 1 ? (firstName ?? "配置1") : "配置\($0)", baseURL: "https://r\($0).example.com")
        }
        let log = CallLog()
        let core = FakeCore(log: log, responses: responses, gates: gates)
        let keys = Dictionary(uniqueKeysWithValues: relays.map { ($0.id, "sk-example-\($0.id)") })
        let model = AppModel(
            core: core, keychain: RelayFakeKeychain(log: log, entries: keys),
            settings: RelayFakeSettings(log: log, relays: relays),
            loginItem: FakeLoginItem(log: log), chatGPT: FakeChatGPT(running: false)
        )
        await model.start()
        let entries = relays.map { entryJSON($0.id, $0.name, count: failingIDs.contains($0.id) ? 0 : 6, fetched: !failingIDs.contains($0.id), stale: false) }
        let text = """
        {"version":"1.3.0","forwarding":\(forwarding),"proxy":{"running":true,"error":null},\
        "chatgpt":{"found":true,"error":null},"default_relay":"配置1","relays":[\(entries.joined(separator: ","))],\
        "taken_over":[],"restore_blocked":[],"config_path":"/Users/example/.codex/config.toml",\
        "update":{"latest_version":null,"url":null},"access":{"mode":"\(mode)","target":"\(mode)","detect":"logged_in"}}
        """
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        model.status = try! decoder.decode(CoreStatus.self, from: Data(text.utf8))
        return ViewFixture(model: model, core: core)
    }
}

@MainActor
func awaitUntil(timeout: TimeInterval = 2, _ condition: () -> Bool) async {
    let deadline = Date().addingTimeInterval(timeout)
    while !condition(), Date() < deadline { try? await Task.sleep(nanoseconds: 5_000_000) }
}

@MainActor
func hideOffscreen(_ window: NSWindow) {
    window.alphaValue = 0
    window.ignoresMouseEvents = true
    window.makeKeyAndOrderFront(nil)
    window.setFrameOrigin(NSPoint(x: -20000, y: -20000))
}

struct PixelSnapshot {
    let rep: NSBitmapImageRep
    let scale: CGFloat

    enum Kind { case primary, darkText }

    func matches(_ kind: Kind, x: Int, y: Int) -> Bool {
        guard x >= 0, y >= 0, x < rep.pixelsWide, y < rep.pixelsHigh,
              let c = rep.colorAt(x: x, y: y)?.usingColorSpace(.sRGB), c.alphaComponent > 0.9 else { return false }
        let r = c.redComponent * 255, g = c.greenComponent * 255, b = c.blueComponent * 255
        switch kind {
        case .primary: return r < 90 && g < 200 && g - r > 60 && b - r > 55
        case .darkText: return max(r, g, b) < 130 && g - r < 45
        }
    }

    func pixels(_ kind: Kind, inPoints rect: CGRect) -> [(x: Int, y: Int)] {
        var result: [(x: Int, y: Int)] = []
        for y in Int(rect.minY * scale)..<Int(rect.maxY * scale) {
            for x in Int(rect.minX * scale)..<Int(rect.maxX * scale) where matches(kind, x: x, y: y) {
                result.append((x, y))
            }
        }
        return result
    }

    func bounds(_ kind: Kind, inPoints rect: CGRect) -> CGRect? {
        let hits = pixels(kind, inPoints: rect)
        guard let minX = hits.map(\.x).min(), let maxX = hits.map(\.x).max(),
              let minY = hits.map(\.y).min(), let maxY = hits.map(\.y).max() else { return nil }
        return CGRect(x: CGFloat(minX) / scale, y: CGFloat(minY) / scale,
                      width: CGFloat(maxX - minX + 1) / scale, height: CGFloat(maxY - minY + 1) / scale)
    }

    func fillRatio(_ kind: Kind, inPoints rect: CGRect) -> Double {
        guard let box = bounds(kind, inPoints: rect) else { return 0 }
        return Double(pixels(kind, inPoints: rect).count) / Double(box.width * box.height * scale * scale)
    }
}
