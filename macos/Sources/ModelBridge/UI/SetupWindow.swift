import SwiftUI

struct SetupWindow: View {
    @ObservedObject var model: AppModel

    var body: some View {
        VStack(spacing: 0) {
            WindowTitleBar(title: "设置模型接入助手")
            VStack(alignment: .leading, spacing: 20) {
                VStack(alignment: .leading, spacing: 8) {
                    Text(SetupCopy.title).font(Fonts.pageTitle).foregroundColor(Palette.text)
                    Text(SetupCopy.subtitle)
                        .font(Fonts.control)
                        .foregroundColor(Palette.textMuted)
                }
                if case .succeeded(let title, _) = model.setupPhase {
                    succeeded(title: title)
                } else {
                    form
                }
            }
            .padding(24)
            .overlay(alignment: .top) {
                AlertOverlay {
                    NoticeStack(model: model)
                    if case .failed(let title, let detail) = model.setupPhase {
                        AlertBar(tone: .danger, title: title, detail: detail, onClose: { model.dismissSetupFailure() })
                    }
                }
            }
        }
        .onAppear { model.beginSetup() }
        .frame(width: 620)
        .background(Palette.background)
    }

    private var connecting: Bool { model.setupPhase == .connecting }

    @ViewBuilder
    private var form: some View {
        Card {
            FieldRow(
                label: "名字",
                required: true,
                help: RelayFormCopy.nameHelp,
                error: model.relayFieldErrors[.name]
            ) {
                AppTextField(
                    label: "名字", placeholder: "", text: $model.draftName,
                    invalid: model.relayFieldErrors[.name] != nil, errorText: model.relayFieldErrors[.name]
                )
            }
            FieldRow(
                label: "Base URL",
                required: true,
                help: "以 https:// 开头的完整地址。",
                error: model.relayFieldErrors[.baseURL]
            ) {
                AppTextField(
                    label: "Base URL", placeholder: "https://relay.example.com/v1", text: $model.draftBaseURL,
                    mono: true, invalid: model.relayFieldErrors[.baseURL] != nil, errorText: model.relayFieldErrors[.baseURL]
                )
            }
            FieldRow(
                label: "API Key",
                required: true,
                help: "保存在系统钥匙串中。",
                error: model.relayFieldErrors[.apiKey]
            ) {
                SecretInput(
                    placeholder: "", text: $model.draftAPIKey,
                    invalid: model.relayFieldErrors[.apiKey] != nil, errorText: model.relayFieldErrors[.apiKey]
                )
            }
        }
        .disabled(connecting)
        HStack {
            Spacer()
            Button { Task { await model.completeSetup() } } label: {
                BusyLabel(text: connecting ? SetupCopy.connecting : "完成设置", busy: connecting)
            }
            .primaryAction()
            .disabled(connecting)
        }
    }

    @ViewBuilder
    private func succeeded(title: String) -> some View {
        AlertBar(
            tone: .success,
            title: title,
            detail: model.setupSucceededText
        )
        ForEach(model.setupFollowUpIssues, id: \.self) { issue in
            AlertBar(tone: .warning, title: issue)
        }
        HStack {
            Spacer()
            Button("完成") { model.finishSetup() }.primaryAction()
        }
    }
}
