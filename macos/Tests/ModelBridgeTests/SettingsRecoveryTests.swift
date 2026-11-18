import XCTest
@testable import ModelBridge

private let backupTitle = "原来的设置读不出来，已备份原文件"
private let failedBackupTitle = "原来的设置读不出来"
private let corrupt = Data("{ not json".utf8)

@MainActor
final class SettingsRecoveryTests: XCTestCase {
    private var root: URL!
    private var dir: URL!
    private var settings: ShellSettings!

    override func setUp() {
        root = FileManager.default.temporaryDirectory.appendingPathComponent("recovery-\(UUID().uuidString)")
        dir = root.appendingPathComponent("ModelBridge")
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        settings = ShellSettings(directory: dir)
    }

    override func tearDown() {
        try? FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: dir.path)
        try? FileManager.default.removeItem(at: root)
    }

    private var backupURL: URL { dir.appendingPathComponent("shell-settings.json.bak") }

    private func makeModel(responses: [String: Result<Data, CoreError>] = [:]) -> AppModel {
        let log = CallLog()
        return AppModel(
            core: FakeCore(log: log, responses: responses),
            keychain: RelayFakeKeychain(log: log),
            settings: settings,
            loginItem: FakeLoginItem(log: log),
            chatGPT: FakeChatGPT(running: false)
        )
    }

    private func notice(_ model: AppModel, titled title: String) -> Notice? {
        model.notices.first { $0.title == title }
    }

    private func lockDirectory() throws {
        try FileManager.default.setAttributes([.posixPermissions: 0o555], ofItemAtPath: dir.path)
    }

    private func unlockDirectory() throws {
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: dir.path)
    }

    func testCorruptSettingsFile_IsRenamedToBakInSameDirectory() async throws {
        try corrupt.write(to: settings.fileURL)
        let model = makeModel()
        await model.start()
        XCTAssertFalse(FileManager.default.fileExists(atPath: settings.fileURL.path))
        XCTAssertEqual(try Data(contentsOf: backupURL), corrupt)
    }

    func testCorruptSettingsFile_UndecodableJSONShapeIsAlsoBackedUp() async throws {
        try Data(#"{"relays":"oops"}"#.utf8).write(to: settings.fileURL)
        let model = makeModel()
        await model.start()
        XCTAssertTrue(FileManager.default.fileExists(atPath: backupURL.path))
    }

    func testCorruptSettingsFile_EntersSetupWithEmptyRelaysAndForm() async throws {
        try corrupt.write(to: settings.fileURL)
        let model = makeModel()
        await model.start()
        model.beginSetup()
        XCTAssertTrue(model.needsSetup)
        XCTAssertEqual(model.relays, [])
        XCTAssertEqual(model.draftBaseURL, "")
        XCTAssertEqual(model.draftAPIKey, "")
    }

    func testCorruptSettingsFile_ShowsWarningNoticeWithBackupPathAndCopy() async throws {
        try corrupt.write(to: settings.fileURL)
        let model = makeModel()
        await model.start()
        let shown = try XCTUnwrap(notice(model, titled: backupTitle))
        XCTAssertEqual(shown.kind, .warning)
        let detail = try XCTUnwrap(shown.detail)
        XCTAssertTrue(detail.contains("保存中转名字和地址的设置文件读不出来，可能是文件坏了、正被别的程序占用，或者格式认不出来。"))
        XCTAssertTrue(detail.contains("原文件已经改名备份到："))
        XCTAssertTrue(detail.contains(backupURL.path))
        XCTAssertTrue(detail.contains("这个文件里只有中转的名字和地址，没有 API Key。重新填写并完成设置后就能照常使用。"))
    }

    func testCorruptSettingsFile_NoticeStaysPastAutoDismissDelay() async throws {
        try corrupt.write(to: settings.fileURL)
        let model = makeModel()
        model.noticeAutoDismissDelay = 0.05
        await model.start()
        try? await Task.sleep(nanoseconds: 300_000_000)
        XCTAssertNotNil(notice(model, titled: backupTitle))
    }

    func testCorruptSettingsFile_NoticeIsRenderedInSetupWindow() async throws {
        try corrupt.write(to: settings.fileURL)
        let model = makeModel()
        await model.start()
        XCTAssertTrue(RenderedView(SetupWindow(model: model)).has(backupTitle))
    }

    func testBackupFails_ShortTitleAndOriginalPathWithCopy() async throws {
        try corrupt.write(to: settings.fileURL)
        try lockDirectory()
        let model = makeModel()
        await model.start()
        XCTAssertNil(notice(model, titled: backupTitle))
        let shown = try XCTUnwrap(notice(model, titled: failedBackupTitle))
        XCTAssertEqual(shown.kind, .warning)
        let detail = try XCTUnwrap(shown.detail)
        XCTAssertTrue(detail.contains("保存中转名字和地址的设置文件读不出来，可能是文件坏了、正被别的程序占用，或者格式认不出来。"))
        XCTAssertTrue(detail.contains("没能备份这个文件，它还在原处："))
        XCTAssertTrue(detail.contains(settings.fileURL.path))
        XCTAssertTrue(detail.contains("完成设置后，它会被新的设置覆盖。想留着原文件的话，请先自己复制一份。"))
        XCTAssertTrue(detail.contains("这个文件里只有中转的名字和地址，没有 API Key。"))
        XCTAssertTrue(model.needsSetup)
    }

    func testBackupFails_OriginalFileUntouchedUntilSetupCompletes() async throws {
        try corrupt.write(to: settings.fileURL)
        try lockDirectory()
        let model = makeModel(responses: [
            "save_relay": .success(Data(#"{"id":"1","name":"中转","model_count":2,"models_changed":true}"#.utf8))
        ])
        await model.start()
        model.beginSetup()
        XCTAssertEqual(try Data(contentsOf: settings.fileURL), corrupt)
        XCTAssertFalse(FileManager.default.fileExists(atPath: backupURL.path))

        try unlockDirectory()
        model.draftBaseURL = "https://new.example.com"
        model.draftAPIKey = "sk-draft-0000"
        await model.completeSetup()
        let saved = try ShellSettings(directory: dir).loadRelays()
        XCTAssertEqual(saved.map(\.baseURL), ["https://new.example.com"])
    }

    func testMissingSettingsFile_EntersSetupWithoutNoticeOrBackup() async {
        let model = makeModel()
        await model.start()
        XCTAssertTrue(model.needsSetup)
        XCTAssertTrue(model.notices.isEmpty)
        XCTAssertFalse(FileManager.default.fileExists(atPath: backupURL.path))
    }

    func testValidSettingsFile_IsNotBackedUp() async throws {
        try settings.saveRelays([RelayConfig(id: "1", name: "中转", baseURL: "https://a.example.com")])
        let model = makeModel()
        await model.start()
        XCTAssertEqual(model.relays.count, 1)
        XCTAssertTrue(model.notices.isEmpty)
        XCTAssertFalse(FileManager.default.fileExists(atPath: backupURL.path))
    }

    func testSuccessfulSetup_RemovesBackupNotice() async throws {
        try corrupt.write(to: settings.fileURL)
        let model = makeModel(responses: [
            "save_relay": .success(Data(#"{"id":"1","name":"中转","model_count":2,"models_changed":true}"#.utf8))
        ])
        await model.start()
        model.beginSetup()
        XCTAssertNotNil(notice(model, titled: backupTitle))
        model.draftBaseURL = "https://new.example.com"
        model.draftAPIKey = "sk-draft-0000"
        await model.completeSetup()
        XCTAssertNil(notice(model, titled: backupTitle))
    }

    func testSuccessfulSetup_RemovesFailedBackupNotice() async throws {
        try corrupt.write(to: settings.fileURL)
        try lockDirectory()
        let model = makeModel(responses: [
            "save_relay": .success(Data(#"{"id":"1","name":"中转","model_count":2,"models_changed":true}"#.utf8))
        ])
        await model.start()
        try unlockDirectory()
        model.beginSetup()
        model.draftBaseURL = "https://new.example.com"
        model.draftAPIKey = "sk-draft-0000"
        await model.completeSetup()
        XCTAssertNil(notice(model, titled: failedBackupTitle))
    }

    func testFailedSetup_KeepsBackupNotice() async throws {
        try corrupt.write(to: settings.fileURL)
        let model = makeModel(responses: ["save_relay": .failure(.remote(code: "relay_401", keys: []))])
        await model.start()
        model.beginSetup()
        model.draftBaseURL = "https://new.example.com"
        model.draftAPIKey = "sk-draft-0000"
        await model.completeSetup()
        XCTAssertNotNil(notice(model, titled: backupTitle))
    }
}

@MainActor
final class SettingsBackupUninstallTests: XCTestCase {
    private var root: URL!
    private var dir: URL!
    private var settings: ShellSettings!

    override func setUp() {
        root = FileManager.default.temporaryDirectory.appendingPathComponent("uninstall-\(UUID().uuidString)")
        dir = root.appendingPathComponent("ModelBridge")
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        settings = ShellSettings(directory: dir)
    }

    override func tearDown() {
        try? FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: dir.path)
        try? FileManager.default.removeItem(at: root)
    }

    private var backupURL: URL { dir.appendingPathComponent("shell-settings.json.bak") }

    private func uninstall() async -> AppModel {
        let log = CallLog()
        let model = AppModel(
            core: FakeCore(log: log), keychain: RelayFakeKeychain(log: log), settings: settings,
            loginItem: FakeLoginItem(log: log), chatGPT: FakeChatGPT(running: false)
        )
        await model.uninstall()
        await model.confirmPending()
        return model
    }

    func testDeleteAll_RemovesBackupFileAlongWithSettings() throws {
        try settings.saveRelays([RelayConfig(id: "1", name: "中转", baseURL: "https://a.example.com")])
        try corrupt.write(to: backupURL)
        try settings.deleteAll()
        XCTAssertFalse(FileManager.default.fileExists(atPath: settings.fileURL.path))
        XCTAssertFalse(FileManager.default.fileExists(atPath: backupURL.path))
    }

    func testDeleteAll_RemovesBackupWhenSettingsFileIsAlreadyGone() throws {
        try corrupt.write(to: backupURL)
        try settings.deleteAll()
        XCTAssertFalse(FileManager.default.fileExists(atPath: backupURL.path))
    }

    func testDeleteAll_DirectoryBecomesEmpty_RemovesDirectory() throws {
        try settings.saveRelays([RelayConfig(id: "1", name: "中转", baseURL: "https://a.example.com")])
        try corrupt.write(to: backupURL)
        try settings.deleteAll()
        XCTAssertFalse(FileManager.default.fileExists(atPath: dir.path))
    }

    func testDeleteAll_DirectoryHasOtherFiles_KeepsDirectoryAndFilesWithoutError() throws {
        let other = dir.appendingPathComponent("other.log")
        try Data("keep".utf8).write(to: other)
        try settings.saveRelays([RelayConfig(id: "1", name: "中转", baseURL: "https://a.example.com")])
        XCTAssertNoThrow(try settings.deleteAll())
        XCTAssertFalse(FileManager.default.fileExists(atPath: settings.fileURL.path))
        XCTAssertEqual(try Data(contentsOf: other), Data("keep".utf8))
    }

    func testUninstall_RemovesEmptiedDataDirectory() async throws {
        try settings.saveRelays([RelayConfig(id: "1", name: "中转", baseURL: "https://a.example.com")])
        let model = await uninstall()
        XCTAssertFalse(FileManager.default.fileExists(atPath: dir.path))
        XCTAssertEqual(model.uninstallLeftovers, [])
    }

    func testUninstall_RemovesBackupFile() async throws {
        try corrupt.write(to: backupURL)
        let model = await uninstall()
        XCTAssertFalse(FileManager.default.fileExists(atPath: backupURL.path))
        XCTAssertEqual(model.uninstallLeftovers, [])
    }

    func testUninstall_UndeletableBackupIsReportedAsLeftover() async throws {
        try corrupt.write(to: backupURL)
        try FileManager.default.setAttributes([.posixPermissions: 0o555], ofItemAtPath: dir.path)
        let model = await uninstall()
        XCTAssertTrue(FileManager.default.fileExists(atPath: backupURL.path))
        XCTAssertFalse(model.uninstallLeftovers.isEmpty)
    }
}

final class CoreRestartSettingsTests: XCTestCase {
    private var root: URL!
    private var settings: ShellSettings!

    override func setUp() {
        root = FileManager.default.temporaryDirectory.appendingPathComponent("restart-\(UUID().uuidString)")
        let dir = root.appendingPathComponent("ModelBridge")
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        settings = ShellSettings(directory: dir)
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: root)
    }

    private var backupURL: URL { settings.directory.appendingPathComponent("shell-settings.json.bak") }

    func testRestartParams_ReadSuccess_ReturnsLatestRelaysIncludingNewlyAdded() throws {
        let startup = StartupConnection()
        let log = CallLog()
        let keychain = RelayFakeKeychain(log: log, entries: ["1": "sk-a", "2": "sk-b"])
        try settings.saveRelays([relayOne])
        XCTAssertEqual(startup.restartParams(settings: settings, keychain: keychain).relays.map(\.id), ["1"])
        try settings.saveRelays([relayOne, relayTwo])
        let params = startup.restartParams(settings: settings, keychain: keychain)
        XCTAssertEqual(params.relays, [
            CoreRelayInit(id: "1", name: "中转", baseURL: "https://a.example.com", apiKey: "sk-a"),
            CoreRelayInit(id: "2", name: "备用", baseURL: "https://b.example.com", apiKey: "sk-b"),
        ])
    }

    func testRestartParams_ReadFailure_ReturnsLastSuccessfulParamsWithoutRenamingFile() throws {
        let startup = StartupConnection()
        let log = CallLog()
        let keychain = RelayFakeKeychain(log: log, entries: ["1": "sk-a"])
        try settings.saveRelays([relayOne])
        let good = startup.restartParams(settings: settings, keychain: keychain)
        XCTAssertEqual(good.relays.count, 1)
        try corrupt.write(to: settings.fileURL)
        let params = startup.restartParams(settings: settings, keychain: keychain)
        XCTAssertEqual(params, good)
        XCTAssertEqual(try Data(contentsOf: settings.fileURL), corrupt)
        XCTAssertFalse(FileManager.default.fileExists(atPath: backupURL.path))
    }

    func testRestartParams_ReadFailureAfterStartupParams_KeepsStartupRelays() throws {
        let startup = StartupConnection()
        let log = CallLog()
        let keychain = RelayFakeKeychain(log: log, entries: ["1": "sk-a"])
        let initial = CoreInitParams(relays: [CoreRelayInit(id: "1", name: "中转", baseURL: "https://a.example.com", apiKey: "sk-a")], chatgptDir: nil)
        startup.remember(initial)
        try corrupt.write(to: settings.fileURL)
        XCTAssertEqual(startup.restartParams(settings: settings, keychain: keychain), initial)
        XCTAssertFalse(FileManager.default.fileExists(atPath: backupURL.path))
    }

    func testRestartParams_ValidSettingsFileLoadsRelaysWithKeys() throws {
        let startup = StartupConnection()
        try settings.saveRelays([RelayConfig(id: "1", name: "中转", baseURL: "https://a.example.com")])
        let params = startup.restartParams(settings: settings, keychain: RelayFakeKeychain(log: CallLog(), entries: ["1": "sk-a"]))
        XCTAssertEqual(params.relays, [CoreRelayInit(id: "1", name: "中转", baseURL: "https://a.example.com", apiKey: "sk-a")])
    }
}

@MainActor
final class UninstallConfirmationTextTests: XCTestCase {
    private let body = "卸载会：\n· 关闭转发，把 Codex 配置还原成安装前的样子\n· 从系统钥匙串删除中转 API Key，删除本助手保存的 Base URL\n· 取消「登录时启动」，删除本助手\n\n不会删除 ChatGPT、聊天记录，以及你自己的其他 Codex 设置。"

    private func confirmation(chatGPTRunning: Bool) async -> Confirmation? {
        let log = CallLog()
        let model = AppModel(
            core: FakeCore(log: log), keychain: RelayFakeKeychain(log: log), settings: RelayFakeSettings(log: log),
            loginItem: FakeLoginItem(log: log), chatGPT: FakeChatGPT(running: chatGPTRunning)
        )
        await model.uninstall()
        return model.pendingConfirmation
    }

    func testUninstallConfirmation_ChatGPTRunning_BodyEndsWithRestartLine() async throws {
        let pending = await confirmation(chatGPTRunning: true)
        let shown = try XCTUnwrap(pending)
        XCTAssertEqual(shown.kind, .uninstall)
        XCTAssertEqual(shown.title, "卸载模型接入助手？")
        XCTAssertEqual(shown.message, body + "\nChatGPT 正在运行，卸载后请重启 ChatGPT。")
        XCTAssertEqual(shown.cancelLabel, "取消")
        XCTAssertEqual(shown.confirmLabel, "卸载")
    }

    func testUninstallConfirmation_ChatGPTNotRunning_BodyHasNoRestartLine() async throws {
        let pending = await confirmation(chatGPTRunning: false)
        let shown = try XCTUnwrap(pending)
        XCTAssertEqual(shown.title, "卸载模型接入助手？")
        XCTAssertEqual(shown.message, body)
        XCTAssertEqual(shown.cancelLabel, "取消")
        XCTAssertEqual(shown.confirmLabel, "卸载")
    }
}
