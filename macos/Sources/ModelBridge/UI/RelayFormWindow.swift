import AppKit
import SwiftUI

enum RelayFormCopy {
    static let nameHelp = "1~16 个字，不能重名，不能含「·」。"
}

struct RelayFormView: View {
    @ObservedObject var model: AppModel
    @FocusState private var nameFocused: Bool

    var body: some View {
        let title = model.editingRelayID == nil ? "添加配置" : "编辑配置"
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                AppIconView(size: 24)
                Text(title).font(Fonts.sectionTitle).foregroundColor(Palette.text)
            }
            .padding(.top, 24).padding(.horizontal, 24)
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(.isHeader)
            VStack(alignment: .leading, spacing: 20) {
                FieldRow(label: "名字", required: true, help: RelayFormCopy.nameHelp, error: model.relayFieldErrors[.name]) {
                    AppTextField(
                        label: "名字", placeholder: "", text: $model.draftName,
                        invalid: model.relayFieldErrors[.name] != nil, errorText: model.relayFieldErrors[.name],
                        focus: $nameFocused
                    )
                }
                FieldRow(label: "Base URL", required: true, help: "以 https:// 开头的完整地址。", error: model.relayFieldErrors[.baseURL]) {
                    AppTextField(
                        label: "Base URL", placeholder: "https://relay.example.com/v1", text: $model.draftBaseURL,
                        mono: true, invalid: model.relayFieldErrors[.baseURL] != nil, errorText: model.relayFieldErrors[.baseURL]
                    )
                }
                FieldRow(label: "API Key", required: true, help: "保存在系统钥匙串中。", error: model.relayFieldErrors[.apiKey]) {
                    SecretInput(
                        placeholder: "", text: $model.draftAPIKey,
                        invalid: model.relayFieldErrors[.apiKey] != nil, errorText: model.relayFieldErrors[.apiKey]
                    )
                }
            }
            .disabled(model.relayFormLocked)
            .padding(.top, 12).padding(.horizontal, 24).padding(.bottom, 24)
            Rectangle().fill(Palette.border).frame(height: 1)
            HStack(spacing: 12) {
                Spacer(minLength: 0)
                Button("取消") { model.cancelRelayForm() }
                    .secondaryAction()
                    .keyboardShortcut(.cancelAction)
                    .disabled(model.relayFormLocked)
                Button { Task { await model.saveRelay() } } label: {
                    BusyLabel(text: model.relaySaveButtonTitle, busy: model.isSavingRelay)
                }
                .primaryAction()
                .disabled(model.isSavingRelay)
            }
            .padding(.vertical, 16).padding(.horizontal, 24)
        }
        .frame(width: 520)
        .background(RoundedRectangle(cornerRadius: 12).fill(Palette.surface))
        .overlay(alignment: .top) {
            if let alert = model.relaySaveAlert {
                AlertBar(tone: alert.tone, title: alert.title, detail: alert.message)
                    .shadow(color: Color(hex: 0x172b3a).opacity(0.16), radius: 24, x: 0, y: 16)
                    .padding(.top, 12).padding(.horizontal, 24)
            }
        }
        .shadow(color: Color(hex: 0x172b3a).opacity(0.16), radius: 24, x: 0, y: 16)
        .preferredColorScheme(.light)
        .onAppear { DispatchQueue.main.async { nameFocused = true } }
        .accessibilityElement(children: .contain)
    }
}
