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

struct LabelValue: View {
    let label: String
    let value: String
    var note: String?
    var font: Font = Fonts.control
    var weight: Font.Weight = .semibold

    var body: some View {
        HStack(spacing: 8) {
            Text(label).foregroundColor(Palette.textMuted)
            HStack(spacing: 4) {
                Text(value).fontWeight(weight).foregroundColor(Palette.text)
                if let note { Text(note).foregroundColor(Palette.textMuted) }
            }
        }
        .font(font)
    }
}

struct AppIconView: View {
    let size: CGFloat

    private var inset: CGFloat {
        if size <= 16 { return 1 }
        if size <= 32 { return 2 }
        return size * 100 / 1024
    }

    private var lineRatio: CGFloat { size >= 64 ? 0.06 : (size >= 24 ? 0.09 : 0.12) }

    var body: some View {
        let plate = size - inset * 2
        let unit = plate / 100
        ZStack {
            RoundedRectangle(cornerRadius: size * 184 / 1024).fill(Palette.primary)
                .frame(width: plate, height: plate)
            Canvas { context, _ in
                var path = Path()
                if size >= 24 {
                    path.addEllipse(in: CGRect(x: 16 * unit, y: 16 * unit, width: 68 * unit, height: 68 * unit))
                    path.move(to: CGPoint(x: 31.5 * unit, y: 42.6 * unit))
                    path.addLine(to: CGPoint(x: 68.5 * unit, y: 42.6 * unit))
                    path.addLine(to: CGPoint(x: 58.9 * unit, y: 33 * unit))
                    path.move(to: CGPoint(x: 68.5 * unit, y: 57.4 * unit))
                    path.addLine(to: CGPoint(x: 31.5 * unit, y: 57.4 * unit))
                    path.addLine(to: CGPoint(x: 41.1 * unit, y: 67 * unit))
                } else {
                    path.move(to: CGPoint(x: 22 * unit, y: 38 * unit))
                    path.addLine(to: CGPoint(x: 78 * unit, y: 38 * unit))
                    path.addLine(to: CGPoint(x: 65 * unit, y: 25 * unit))
                    path.move(to: CGPoint(x: 78 * unit, y: 62 * unit))
                    path.addLine(to: CGPoint(x: 22 * unit, y: 62 * unit))
                    path.addLine(to: CGPoint(x: 35 * unit, y: 75 * unit))
                }
                context.stroke(
                    path, with: .color(.white),
                    style: StrokeStyle(lineWidth: plate * lineRatio, lineCap: .round, lineJoin: .round)
                )
            }
            .frame(width: plate, height: plate)
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
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
                        .lineLimit(1)
                        .truncationMode(.tail)
                        .help(error)
                } else if let help {
                    Text(help).font(Fonts.caption).foregroundColor(Palette.textMuted)
                        .lineLimit(1)
                        .truncationMode(.tail)
                        .help(help)
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

private struct AlertCloseStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        AlertCloseBody(configuration: configuration)
    }
}

private struct AlertCloseBody: View {
    let configuration: ButtonStyleConfiguration
    @Environment(\.isFocused) private var isFocused
    @State private var hovering = false

    var body: some View {
        configuration.label
            .foregroundColor(Palette.textMuted)
            .frame(width: 44, height: 44)
            .background(RoundedRectangle(cornerRadius: 8).fill(configuration.isPressed ? Palette.disabledBg : (hovering ? Palette.background : Color.clear)))
            .overlay { if isFocused { FocusRing() } }
            .contentShape(RoundedRectangle(cornerRadius: 8))
            .onHover { hovering = $0 }
    }
}

enum AlertTone { case info, warning, danger, success }

struct AlertBar<Actions: View>: View {
    let tone: AlertTone
    let title: String
    var detail: String?
    var onClose: (() -> Void)?
    @ViewBuilder let actions: Actions

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: symbol).foregroundColor(color).font(.system(size: 14))
            VStack(alignment: .leading, spacing: 4) {
                Text(title).font(Fonts.label).foregroundColor(color)
                if let detail { Text(detail).font(Fonts.control).foregroundColor(Palette.text) }
                HStack(spacing: 8) { actions }
            }
            Spacer(minLength: 0)
            if let onClose {
                Button(action: onClose) {
                    Image(systemName: "xmark").font(.system(size: 12, weight: .semibold))
                }
                .buttonStyle(AlertCloseStyle())
                .padding(.vertical, -12)
                .padding(.trailing, -8)
                .accessibilityLabel("关闭提示")
            }
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
    init(tone: AlertTone, title: String, detail: String? = nil, onClose: (() -> Void)? = nil) {
        self.init(tone: tone, title: title, detail: detail, onClose: onClose) { EmptyView() }
    }
}

struct AlertOverlay<Content: View>: View {
    @ViewBuilder let content: Content

    var body: some View {
        VStack(spacing: 8) { content }
            .shadow(color: Color(hex: 0x172b3a).opacity(0.16), radius: 24, x: 0, y: 16)
            .padding(.top, 12)
            .padding(.horizontal, 24)
    }
}

struct NoticeStack: View {
    @ObservedObject var model: AppModel

    var body: some View {
        ForEach(model.notices) { notice in
            if let dialogTitle = notice.detailDialogTitle {
                AlertBar(tone: tone(notice.kind), title: notice.title, onClose: { model.dismissNotice(notice.id) }) {
                    Button("查看详情") { DialogPresenter.showNoticeDetail(title: dialogTitle, message: notice.detail ?? "") }
                        .secondaryAction(compact: true)
                }
            } else {
                AlertBar(tone: tone(notice.kind), title: notice.title, detail: notice.detail, onClose: { model.dismissNotice(notice.id) })
            }
        }
    }

    private func tone(_ kind: Notice.Kind) -> AlertTone {
        switch kind {
        case .success: .success
        case .error: .danger
        case .warning: .warning
        case .info: .info
        }
    }
}
