import AppKit
import SwiftUI

private let hoverAnimation = Animation.easeOut(duration: 0.2)

private struct FocusRing: View {
    var radius: CGFloat = 8

    var body: some View {
        RoundedRectangle(cornerRadius: radius + 3)
            .stroke(Palette.focus, lineWidth: 2)
            .padding(-4)
            .allowsHitTesting(false)
    }
}

enum AppButtonKind { case primary, secondary, link, icon }

struct AppButtonStyle: ButtonStyle {
    let kind: AppButtonKind
    var compact = false
    var toggled = false

    func makeBody(configuration: Configuration) -> some View {
        AppButtonBody(configuration: configuration, kind: kind, compact: compact, toggled: toggled)
    }
}

private struct AppButtonBody: View {
    let configuration: ButtonStyleConfiguration
    let kind: AppButtonKind
    let compact: Bool
    let toggled: Bool
    @Environment(\.isEnabled) private var isEnabled
    @Environment(\.isFocused) private var isFocused
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var hovering = false

    var body: some View {
        let pressed = configuration.isPressed
        switch kind {
        case .link:
            configuration.label
                .font(Fonts.label)
                .foregroundColor(!isEnabled ? Palette.textDisabled : (hovering || pressed ? Palette.primaryHover : Palette.primary))
                .padding(.horizontal, 4)
                .frame(minHeight: 44)
                .contentShape(Rectangle())
                .overlay { if isFocused { FocusRing() } }
                .onHover { hovering = $0 }
        default:
            configuration.label
                .font(Fonts.label)
                .foregroundColor(foreground)
                .padding(.horizontal, kind == .icon ? 0 : (compact ? 16 : 20))
                .frame(minWidth: kind == .icon ? 44 : nil, minHeight: 44, maxHeight: 44)
                .background(RoundedRectangle(cornerRadius: 8).fill(fill(pressed: pressed)))
                .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(stroke(pressed: pressed), lineWidth: 1))
                .overlay { if isFocused { FocusRing() } }
                .offset(y: pressed && isEnabled && kind == .primary ? 1 : 0)
                .contentShape(RoundedRectangle(cornerRadius: 8))
                .animation(reduceMotion ? nil : hoverAnimation, value: hovering)
                .onHover { hovering = $0 }
        }
    }

    private var foreground: Color {
        if !isEnabled { return Palette.textDisabled }
        switch kind {
        case .primary: return .white
        case .icon: return toggled ? Palette.primary : Palette.text
        default: return Palette.text
        }
    }

    private func fill(pressed: Bool) -> Color {
        if !isEnabled { return Palette.disabledBg }
        switch kind {
        case .primary: return hovering || pressed ? Palette.primaryHover : Palette.primary
        case .icon: return toggled ? Palette.selectedBg : (pressed ? Palette.disabledBg : (hovering ? Palette.background : Palette.surface))
        default: return pressed ? Palette.disabledBg : (hovering ? Palette.background : Palette.surface)
        }
    }

    private func stroke(pressed: Bool) -> Color {
        if !isEnabled { return Palette.disabledBg }
        switch kind {
        case .primary: return hovering || pressed ? Palette.primaryHover : Palette.primary
        case .icon: return toggled ? Palette.primary : Palette.borderStrong
        default: return Palette.borderStrong
        }
    }
}

extension View {
    func primaryAction() -> some View { buttonStyle(AppButtonStyle(kind: .primary)) }
    func secondaryAction(compact: Bool = false) -> some View { buttonStyle(AppButtonStyle(kind: .secondary, compact: compact)) }
    func linkAction() -> some View { buttonStyle(AppButtonStyle(kind: .link)) }
}

struct Spinner: View {
    @State private var angle = 0.0
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var color = Palette.textDisabled

    var body: some View {
        Circle()
            .trim(from: 0, to: 0.75)
            .stroke(color, style: StrokeStyle(lineWidth: 2, lineCap: .butt))
            .frame(width: 16, height: 16)
            .rotationEffect(.degrees(angle))
            .onAppear {
                guard !reduceMotion else { return }
                withAnimation(.linear(duration: 0.8).repeatForever(autoreverses: false)) { angle = 360 }
            }
            .accessibilityHidden(true)
    }
}

struct AppTextField: View {
    let label: String
    let placeholder: String
    @Binding var text: String
    var secure = false
    var mono = false
    var invalid = false
    var errorText: String?
    @Environment(\.isEnabled) private var isEnabled
    @FocusState private var focused: Bool
    @State private var hovering = false

    var body: some View {
        Group {
            if secure {
                SecureField(placeholder, text: $text)
            } else {
                TextField(placeholder, text: $text)
            }
        }
        .textFieldStyle(.plain)
        .font(mono ? Fonts.mono : Fonts.control)
        .foregroundColor(isEnabled ? Palette.text : Palette.textDisabled)
        .focused($focused)
        .padding(.horizontal, 12)
        .frame(height: 44)
        .background(RoundedRectangle(cornerRadius: 8).fill(isEnabled ? Palette.surface : Palette.disabledBg))
        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(borderColor, lineWidth: 1))
        .overlay { if focused { FocusRing() } }
        .onHover { hovering = $0 }
        .accessibilityLabel(label)
        .accessibilityHint(invalid ? (errorText ?? "") : "")
    }

    private var borderColor: Color {
        if !isEnabled { return Palette.disabledBg }
        if invalid { return Palette.danger }
        return hovering ? Palette.textMuted : Palette.borderStrong
    }
}

struct SwitchRow: View {
    let label: String
    @Binding var isOn: Bool
    @Environment(\.isEnabled) private var isEnabled

    var body: some View {
        HStack(spacing: 12) {
            Button { isOn.toggle() } label: { SwitchBody(isOn: isOn) }
                .buttonStyle(.plain)
                .accessibilityLabel(label)
                .accessibilityValue(isOn ? "已开启" : "已关闭")
            Text(isOn ? "已开启" : "已关闭")
                .font(Fonts.label)
                .foregroundColor(isEnabled ? Palette.text : Palette.textDisabled)
        }
        .frame(minHeight: 44)
    }
}

private struct SwitchBody: View {
    let isOn: Bool
    @Environment(\.isEnabled) private var isEnabled
    @Environment(\.isFocused) private var isFocused
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var cursorPushed = false

    var body: some View {
        ZStack(alignment: .topLeading) {
            Capsule().fill(isOn ? Palette.primary : Palette.disabledBg)
            Capsule().strokeBorder(border, lineWidth: 1)
            Circle()
                .fill(knob)
                .frame(width: 16, height: 16)
                .offset(x: isOn ? 19 : 3, y: 3)
        }
        .frame(width: 40, height: 24)
        .animation(reduceMotion ? nil : hoverAnimation, value: isOn)
        .overlay {
            if isFocused {
                Capsule().stroke(Palette.focus, lineWidth: 2).padding(-4)
            }
        }
        .onHover { inside in
            if inside, isEnabled, !cursorPushed {
                NSCursor.pointingHand.push()
                cursorPushed = true
            } else if !inside, cursorPushed {
                NSCursor.pop()
                cursorPushed = false
            }
        }
    }

    private var border: Color {
        if !isEnabled { return Palette.border }
        return isOn ? Palette.primary : Palette.borderStrong
    }

    private var knob: Color {
        if !isEnabled { return Palette.border }
        return isOn ? .white : Palette.borderStrong
    }
}
