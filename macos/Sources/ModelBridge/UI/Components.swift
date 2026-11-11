import SwiftUI

extension Color {
    init(hex: UInt32) {
        self.init(
            .sRGB,
            red: Double((hex >> 16) & 0xff) / 255,
            green: Double((hex >> 8) & 0xff) / 255,
            blue: Double(hex & 0xff) / 255
        )
    }
}

enum Palette {
    static let primary = Color(hex: 0x0f766e)
    static let primaryHover = Color(hex: 0x115e59)
    static let background = Color(hex: 0xf5f7fa)
    static let surface = Color.white
    static let text = Color(hex: 0x172b3a)
    static let textMuted = Color(hex: 0x536575)
    static let textDisabled = Color(hex: 0x586878)
    static let disabledBg = Color(hex: 0xe9edf1)
    static let selectedBg = Color(hex: 0xe7f2f0)
    static let focus = Color(hex: 0x0f766e)
    static let border = Color(hex: 0xe2e7ec)
    static let borderStrong = Color(hex: 0x7b8998)
    static let success = Color(hex: 0x16734b)
    static let successBg = Color(hex: 0xf0faf4)
    static let warning = Color(hex: 0x8a530e)
    static let warningBg = Color(hex: 0xfff9ed)
    static let danger = Color(hex: 0xb42318)
    static let dangerBg = Color(hex: 0xfff3f1)
    static let info = Color(hex: 0x245d9a)
    static let infoBg = Color(hex: 0xf1f6fc)
}

enum Fonts {
    static let pageTitle = Font.system(size: 20, weight: .semibold)
    static let sectionTitle = Font.system(size: 16, weight: .semibold)
    static let control = Font.system(size: 13)
    static let label = Font.system(size: 13, weight: .semibold)
    static let caption = Font.system(size: 12)
    static let mono = Font.system(size: 12, design: .monospaced)
}

struct BusyLabel: View {
    let text: String
    var busy = false
    @State private var showSpinner = false

    var body: some View {
        HStack(spacing: 8) {
            if busy && showSpinner {
                Spinner()
            }
            Text(text)
        }
        .task(id: busy) {
            showSpinner = false
            guard busy else { return }
            try? await Task.sleep(nanoseconds: 200_000_000)
            if !Task.isCancelled { showSpinner = true }
        }
    }
}

struct SecretInput: View {
    let placeholder: String
    @Binding var text: String
    var invalid = false
    var errorText: String?
    @State private var revealed = false

    var body: some View {
        HStack(spacing: 8) {
            AppTextField(
                label: "API Key", placeholder: placeholder, text: $text,
                secure: !revealed, mono: true, invalid: invalid, errorText: errorText
            )
            Button { revealed.toggle() } label: {
                Image(systemName: revealed ? "eye.slash" : "eye")
            }
            .buttonStyle(AppButtonStyle(kind: .icon, toggled: revealed))
            .accessibilityLabel(revealed ? "隐藏 API Key" : "显示 API Key")
            .accessibilityAddTraits(revealed ? .isSelected : [])
        }
    }
}

struct FieldRow<Control: View>: View {
    let label: String
    var required = false
    var help: String?
    var error: String?
    @ViewBuilder let control: Control

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            HStack(spacing: 4) {
                Text(label).font(Fonts.label)
                if required { Text("必填").font(Fonts.caption).foregroundColor(Palette.textMuted) }
            }
            .foregroundColor(Palette.text)
            .frame(width: 112, height: 44, alignment: .leading)
            VStack(alignment: .leading, spacing: 8) {
                control
                if let error {
                    Label(error, systemImage: "exclamationmark.circle.fill")
                        .font(Fonts.caption)
                        .foregroundColor(Palette.danger)
                } else if let help {
                    Text(help).font(Fonts.caption).foregroundColor(Palette.textMuted)
                }
            }
        }
    }
}

struct Card<Content: View>: View {
    var title: String?
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            if let title { Text(title).font(Fonts.sectionTitle).foregroundColor(Palette.text) }
            content
        }
        .padding(24)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 12).fill(Palette.surface))
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Palette.border, lineWidth: 1))
    }
}

enum AlertTone { case info, warning, danger, success }

struct AlertBar<Actions: View>: View {
    let tone: AlertTone
    let title: String
    var detail: String?
    var keys: [String] = []
    var path: String?
    @ViewBuilder let actions: Actions

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: symbol).foregroundColor(color).font(.system(size: 14))
            VStack(alignment: .leading, spacing: 4) {
                Text(title).font(Fonts.label).foregroundColor(color)
                if let detail { Text(detail).font(Fonts.control).foregroundColor(Palette.text) }
                if !keys.isEmpty {
                    Text(keys.joined(separator: "、")).font(Fonts.mono).foregroundColor(Palette.text)
                }
                if let path { Text(path).font(Fonts.mono).foregroundColor(Palette.text).textSelection(.enabled) }
                HStack(spacing: 8) { actions }
            }
            Spacer(minLength: 0)
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 8).fill(background))
        .overlay(RoundedRectangle(cornerRadius: 8).stroke(color, lineWidth: 1))
        .accessibilityElement(children: .contain)
    }

    private var color: Color {
        switch tone {
        case .info: Palette.info
        case .warning: Palette.warning
        case .danger: Palette.danger
        case .success: Palette.success
        }
    }

    private var background: Color {
        switch tone {
        case .info: Palette.infoBg
        case .warning: Palette.warningBg
        case .danger: Palette.dangerBg
        case .success: Palette.successBg
        }
    }

    private var symbol: String {
        switch tone {
        case .info: "info.circle.fill"
        case .warning: "exclamationmark.triangle.fill"
        case .danger: "xmark.octagon.fill"
        case .success: "checkmark.circle.fill"
        }
    }
}

extension AlertBar where Actions == EmptyView {
    init(tone: AlertTone, title: String, detail: String? = nil) {
        self.init(tone: tone, title: title, detail: detail) { EmptyView() }
    }
}
