using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.Services;
using ModelBridge.UI;

namespace ModelBridge.Tests;

using Params = IReadOnlyDictionary<string, object?>;

sealed class MultiCore(List<string> log) : ICoreClient
{
    readonly Dictionary<string, Func<Params, Task<JsonElement>>> handlers = [];

    public List<(string Method, Params Params)> Calls { get; } = [];
    public CoreInitParams? StartedWith { get; private set; }
    public bool Forwarding { get; set; } = true;
    public string NextId { get; set; } = "2";
    public int NewModelCount { get; set; } = 3;
    public List<RelayJson.Entry> Entries { get; } = [];

    public event Action<JsonElement>? StatusReceived;
    public event Action<string>? Failed;

    public Task StartAsync(CoreInitParams init)
    {
        StartedWith = init;
        return Task.CompletedTask;
    }

    public Task StopAsync() => Task.CompletedTask;

    public void OnAsync(string method, Func<Params, Task<JsonElement>> handler) => handlers[method] = handler;

    public void Fail(string method, string code, params string[] keys) =>
        handlers[method] = _ => Task.FromException<JsonElement>(new CoreCallException(code, keys));

    public Task<JsonElement> CallAsync(string method, Params? parameters = null)
    {
        var p = parameters ?? new Dictionary<string, object?>();
        Calls.Add((method, p));
        log.Add("call:" + method);
        if (handlers.TryGetValue(method, out var handler)) return handler(p);
        return Task.FromResult(method switch
        {
            "save_relay" => RelayJson.Parse(new JsonObject
            {
                ["id"] = p["id"] as string ?? NextId,
                ["name"] = p["name"] as string,
                ["model_count"] = NewModelCount,
                ["models_changed"] = true,
            }.ToJsonString()),
            "set_forwarding" => RelayJson.Status(Convert.ToBoolean(p["enabled"]), Entries.ToArray()),
            _ => RelayJson.Status(Forwarding, Entries.ToArray()),
        });
    }

    public int Count(string method) => Calls.Count(c => c.Method == method);
    public Params ParamsOf(string method) => Calls.Last(c => c.Method == method).Params;
    public void RaiseStatus(JsonElement status) => StatusReceived?.Invoke(status);
    public void RaiseFailed(string reason) => Failed?.Invoke(reason);
}

static class RelayJson
{
    public sealed record Entry(
        string Id,
        string Name,
        int ModelCount = 3,
        string? FetchedAt = "2026-11-11T00:12:00Z",
        bool Stale = false,
        string? Error = null);

    public static JsonElement Parse(string text) => JsonDocument.Parse(text).RootElement.Clone();

    public static JsonElement Status(
        bool forwarding,
        Entry[] relays,
        string? defaultRelay = null,
        string? mode = "both",
        string detect = "logged_in")
    {
        var list = new JsonArray();
        foreach (var r in relays)
            list.Add(new JsonObject
            {
                ["id"] = r.Id,
                ["name"] = r.Name,
                ["model_count"] = r.ModelCount,
                ["fetched_at"] = r.FetchedAt,
                ["stale"] = r.Stale,
                ["error"] = r.Error,
            });
        var status = new JsonObject
        {
            ["version"] = "1.2.0",
            ["forwarding"] = forwarding,
            ["proxy"] = new JsonObject { ["running"] = true, ["error"] = null },
            ["chatgpt"] = new JsonObject { ["found"] = true, ["error"] = null },
            ["relays"] = list,
            ["default_relay"] = defaultRelay,
            ["access"] = new JsonObject { ["mode"] = mode, ["detect"] = detect, ["target"] = mode },
            ["taken_over"] = new JsonArray(),
            ["restore_blocked"] = new JsonArray(),
            ["config_path"] = "/home/example/.codex/config.toml",
            ["update"] = new JsonObject { ["latest_version"] = null, ["url"] = null },
        };
        return Parse(status.ToJsonString());
    }
}

sealed class MultiVault(List<string> log) : ICredentialVault
{
    public Dictionary<string, string> Keys { get; } = [];
    public Exception? SaveException { get; set; }
    public Exception? DeleteException { get; set; }
    public int DeleteAllCount { get; private set; }

    public void Save(string id, string key)
    {
        log.Add("vault:save");
        if (SaveException != null) throw SaveException;
        Keys[id] = key;
    }

    public string? Read(string id) => Keys.GetValueOrDefault(id);

    public void Delete(string id)
    {
        log.Add("vault:delete");
        if (DeleteException != null) throw DeleteException;
        Keys.Remove(id);
    }

    public void DeleteAll()
    {
        DeleteAllCount++;
        Keys.Clear();
    }

    public bool Any() => Keys.Count > 0;
}

sealed class MultiSettings(List<string> log) : IShellSettingsStore
{
    public List<RelayConfig> Relays { get; set; } = [];
    public Exception? SaveException { get; set; }
    public int FailFromCall { get; set; }
    public int SaveCalls { get; private set; }

    public IReadOnlyList<RelayConfig> ReadRelays() => Relays.ToList();

    public void SaveRelays(IReadOnlyList<RelayConfig> relays)
    {
        log.Add("settings:save");
        SaveCalls++;
        if (SaveException != null) throw SaveException;
        if (FailFromCall > 0 && SaveCalls >= FailFromCall) throw new IOException("disk full");
        Relays = relays.ToList();
    }

    public void DeleteAll() => Relays = [];
}

sealed class MultiHarness
{
    public List<string> Log { get; } = [];
    public MultiCore Core { get; }
    public MultiVault Vault { get; }
    public MultiSettings Settings { get; }
    public FakeLogin Login { get; }
    public FakeChatGpt ChatGpt { get; }
    public FakeConfirmer Confirmer { get; }
    public AppModel Model { get; }

    public MultiHarness()
    {
        Core = new MultiCore(Log);
        Vault = new MultiVault(Log);
        Settings = new MultiSettings(Log);
        Login = new FakeLogin(Log);
        ChatGpt = new FakeChatGpt();
        Confirmer = new FakeConfirmer(Log);
        Model = new AppModel(Core, Vault, Settings, Login, ChatGpt, Confirmer);
    }

    public void Seed(string id, string name, string url, string key, int models = 3)
    {
        Settings.Relays.Add(new RelayConfig(id, name, url));
        Vault.Keys[id] = key;
        Core.Entries.Add(new RelayJson.Entry(id, name, models));
    }

    public async Task StartWithTwoAsync()
    {
        Seed("1", "中转", "https://relay.example.com/v1", "sk-example-0001");
        Seed("2", "A家", "https://relay2.example.com/v1", "sk-example-0002");
        await Model.StartAsync();
        Push();
    }

    public void Push(bool forwarding = true, string? defaultRelay = null, string? mode = "both", params RelayJson.Entry[] entries) =>
        Core.RaiseStatus(RelayJson.Status(forwarding, entries.Length > 0 ? entries : Core.Entries.ToArray(), defaultRelay, mode));
}

[TestClass]
public class MultiRelayTests
{
    static string NoteText(AppNotice? n) => n == null ? "" : n.Title + n.Detail;

    static AppNotice? Find(MultiHarness h, string title) => h.Model.Notices.FirstOrDefault(n => n.Title == title);

    static void Fill(RelayEditorModel e, string name = "B家", string url = "https://relay3.example.com/v1", string key = "sk-example-0003")
    {
        e.NameInput = name;
        e.BaseUrlInput = url;
        e.ApiKeyInput = key;
    }

    static string LegacyZhong = "中" + "台";

    // ---------- 启动 ----------

    [TestMethod]
    public async Task Start_PassesAllRelaysWithKeysToCoreInit()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        var relays = h.Core.StartedWith!.Relays;
        Assert.AreEqual(2, relays.Count);
        Assert.AreEqual(new RelayInit("1", "中转", "https://relay.example.com/v1", "sk-example-0001"), relays[0]);
        Assert.AreEqual(new RelayInit("2", "A家", "https://relay2.example.com/v1", "sk-example-0002"), relays[1]);
        Assert.AreEqual(@"C:\Apps\ChatGPT", h.Core.StartedWith.ChatGptDir);
    }

    [TestMethod]
    public async Task Start_WithRelays_OpensMainPage()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        Assert.AreEqual(AppPage.Main, h.Model.Page);
    }

    [TestMethod]
    public async Task Start_NoRelays_OpensSetupAndInitsWithEmptyList()
    {
        var h = new MultiHarness();

        await h.Model.StartAsync();

        Assert.AreEqual(AppPage.Setup, h.Model.Page);
        Assert.AreEqual(0, h.Core.StartedWith!.Relays.Count);
    }

    // ---------- 本地校验（添加弹窗） ----------

    [TestMethod]
    public async Task Editor_Add_TitleAndApiKeyHelp()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        var editor = Editors.Add(h.Model);

        Assert.AreEqual("添加配置", editor.Title);
        Assert.AreEqual("保存在系统凭据管理器中。", editor.ApiKeyHelp);
        Assert.AreEqual("保存", editor.SaveButtonText);
    }

    [TestMethod]
    public async Task Validate_EmptyName_ShowsFillName_CoreNotCalled()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var editor = Editors.Add(h.Model);
        Fill(editor, name: "   ");

        Assert.IsFalse(await editor.SaveAsync());

        Assert.AreEqual("请填写名字。", editor.NameError);
        Assert.AreEqual(0, h.Core.Count("save_relay"));
    }

    [TestMethod]
    public async Task Validate_Name17Chars_ReportsCountAndKeepsInput()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var editor = Editors.Add(h.Model);
        var name = new string('名', 17);
        Fill(editor, name: name);

        Assert.AreEqual(name, editor.NameInput);
        Assert.IsNull(editor.NameError);
        await editor.SaveAsync();

        Assert.AreEqual("名字最多 16 个字，现在是 17 个。", editor.NameError);
        Assert.AreEqual(0, h.Core.Count("save_relay"));
    }

    [TestMethod]
    public async Task Validate_Name16Chars_Allowed()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var editor = Editors.Add(h.Model);
        Fill(editor, name: new string('名', 16));

        Assert.IsTrue(await editor.SaveAsync());

        Assert.AreEqual(1, h.Core.Count("save_relay"));
    }

    [TestMethod]
    public async Task Validate_NameWith17CharsOnlyAfterTrim_Is16AndAllowed()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var editor = Editors.Add(h.Model);
        Fill(editor, name: "  " + new string('名', 16) + "  ");

        Assert.IsTrue(await editor.SaveAsync());
    }

    [TestMethod]
    public async Task Validate_NameWithMiddleDot_Rejected()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var editor = Editors.Add(h.Model);
        Fill(editor, name: "中转·主力");

        await editor.SaveAsync();

        Assert.AreEqual("名字里不能有「·」。", editor.NameError);
        Assert.AreEqual(0, h.Core.Count("save_relay"));
    }

    [TestMethod]
    public async Task Validate_DuplicateNameIgnoringCase_Rejected()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var editor = Editors.Add(h.Model);
        Fill(editor, name: "a家");

        await editor.SaveAsync();

        Assert.AreEqual("已有同名配置「A家」。", editor.NameError);
        Assert.AreEqual(0, h.Core.Count("save_relay"));
    }

    [TestMethod]
    public async Task Validate_EmptyBaseUrl_ShowsFillBaseUrl()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var editor = Editors.Add(h.Model);
        Fill(editor, url: "");

        await editor.SaveAsync();

        Assert.AreEqual("请填写 Base URL。", editor.BaseUrlError);
        Assert.IsNull(editor.NameError);
        Assert.IsNull(editor.ApiKeyError);
    }

    [TestMethod]
    public async Task Validate_NonHttpsBaseUrl_Rejected()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var editor = Editors.Add(h.Model);
        Fill(editor, url: "http://relay3.example.com");

        await editor.SaveAsync();

        Assert.AreEqual("Base URL 要以 https:// 开头。", editor.BaseUrlError);
        Assert.AreEqual(0, h.Core.Count("save_relay"));
    }

    [TestMethod]
    public async Task Validate_EmptyApiKey_ShowsFillApiKey()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var editor = Editors.Add(h.Model);
        Fill(editor, key: " ");

        await editor.SaveAsync();

        Assert.AreEqual("请填写 API Key。", editor.ApiKeyError);
        Assert.AreEqual(0, h.Core.Count("save_relay"));
    }

    [TestMethod]
    public async Task Validate_AllEmpty_ShowsThreeErrorsAtOnce()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var editor = Editors.Add(h.Model);

        await editor.SaveAsync();

        Assert.AreEqual("请填写名字。", editor.NameError);
        Assert.AreEqual("请填写 Base URL。", editor.BaseUrlError);
        Assert.AreEqual("请填写 API Key。", editor.ApiKeyError);
    }

    [TestMethod]
    public async Task Validate_TypingClearsThatFieldError()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var editor = Editors.Add(h.Model);
        await editor.SaveAsync();

        editor.NameInput = "B家";

        Assert.IsNull(editor.NameError);
        Assert.IsNotNull(editor.BaseUrlError);
    }

    // ---------- 添加 ----------

    [TestMethod]
    public async Task Add_Success_CallsSaveRelayWithTrimmedNameAndNullId()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var editor = Editors.Add(h.Model);
        Fill(editor, name: "  B家  ");

        Assert.IsTrue(await editor.SaveAsync());

        var p = h.Core.ParamsOf("save_relay");
        Assert.IsNull(p["id"]);
        Assert.AreEqual("B家", p["name"]);
        Assert.AreEqual("https://relay3.example.com/v1", p["base_url"]);
        Assert.AreEqual("sk-example-0003", p["api_key"]);
    }

    [TestMethod]
    public async Task Add_Success_SavesKeyBeforeSettings_UnderCoreReturnedId()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Core.NextId = "3";
        h.Log.Clear();
        var editor = Editors.Add(h.Model);
        Fill(editor);

        await editor.SaveAsync();

        CollectionAssert.AreEqual(new[] { "call:save_relay", "vault:save", "settings:save" }, h.Log.Where(x => x != "confirm").ToArray());
        Assert.AreEqual("sk-example-0003", h.Vault.Keys["3"]);
        CollectionAssert.AreEqual(
            new[] { new RelayConfig("1", "中转", "https://relay.example.com/v1"), new RelayConfig("2", "A家", "https://relay2.example.com/v1"), new RelayConfig("3", "B家", "https://relay3.example.com/v1") },
            h.Settings.Relays);
    }

    [TestMethod]
    public async Task Add_Success_NoticeNamesRelayAndModelCount()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Core.NextId = "3";
        var editor = Editors.Add(h.Model);
        Fill(editor);

        await editor.SaveAsync();

        var notice = Find(h, "已添加「B家」");
        Assert.IsNotNull(notice);
        Assert.AreEqual("已获取 3 个模型。重启 ChatGPT 后生效。", notice.Detail);
        Assert.AreEqual(AppSeverity.Success, notice.Severity);
    }

    [TestMethod]
    public async Task Add_CoreFails401_NothingSaved_AlertKeepsInputs()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Core.Fail("save_relay", "relay_401");
        var editor = Editors.Add(h.Model);
        Fill(editor);

        Assert.IsFalse(await editor.SaveAsync());

        Assert.AreEqual("没有保存", editor.SaveAlert!.Title);
        Assert.AreEqual("中转服务返回 401：API Key 无效或已过期。已填的内容都还在，改好后再保存。", editor.SaveAlert.Message);
        Assert.AreEqual(DialogAlertSeverity.Danger, editor.SaveAlert.Severity);
        Assert.AreEqual("B家", editor.NameInput);
        Assert.AreEqual("sk-example-0003", editor.ApiKeyInput);
        Assert.AreEqual(2, h.Settings.Relays.Count);
        Assert.AreEqual(2, h.Vault.Keys.Count);
        Assert.AreEqual(2, h.Model.Relays.Count);
        Assert.IsFalse(editor.IsSaving);
    }

    [TestMethod]
    public async Task Add_CoreFails_KeyNeverAppearsInNoticesOrAlert()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Core.Fail("save_relay", "relay_401");
        var editor = Editors.Add(h.Model);
        Fill(editor);

        await editor.SaveAsync();

        var all = editor.SaveAlert!.Title + editor.SaveAlert.Message + string.Concat(h.Model.Notices.Select(NoteText));
        Assert.IsFalse(all.Contains("sk-example-0003"));
    }

    [TestMethod]
    public async Task Add_CoreReportsDuplicateName_ShownUnderNameWithExistingName()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Core.Fail("save_relay", "name_duplicate", "A家");
        var editor = Editors.Add(h.Model);
        Fill(editor, name: "B家");

        await editor.SaveAsync();

        Assert.AreEqual("已有同名配置「A家」。", editor.NameError);
        Assert.AreEqual(2, h.Settings.Relays.Count);
    }

    [TestMethod]
    public async Task Add_WhileSaving_FieldsAndCancelDisabled_ButtonShowsProgress()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("save_relay", _ => gate.Task);
        var editor = Editors.Add(h.Model);
        Fill(editor);

        var saving = editor.SaveAsync();

        Assert.IsTrue(editor.IsSaving);
        Assert.IsFalse(editor.FieldsEnabled);
        Assert.IsFalse(editor.CancelEnabled);
        Assert.AreEqual("正在保存并获取模型…", editor.SaveButtonText);

        gate.SetResult(RelayJson.Parse("""{"id":"3","name":"B家","model_count":3,"models_changed":true}"""));
        await saving;

        Assert.IsFalse(editor.IsSaving);
        Assert.IsTrue(editor.FieldsEnabled);
        Assert.IsTrue(editor.CancelEnabled);
        Assert.AreEqual("保存", editor.SaveButtonText);
    }

    [TestMethod]
    public async Task Add_SettingsWriteFails_RollsBackKeyAndSettings_AlertShown()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Core.NextId = "3";
        h.Settings.SaveException = new IOException("disk full");
        var editor = Editors.Add(h.Model);
        Fill(editor);

        Assert.IsFalse(await editor.SaveAsync());

        Assert.IsFalse(h.Vault.Keys.ContainsKey("3"));
        Assert.AreEqual(2, h.Settings.Relays.Count);
        Assert.AreEqual("没有保存", editor.SaveAlert!.Title);
        Assert.IsNull(Find(h, "已添加「B家」"));
    }

    [TestMethod]
    public async Task Add_KeyWriteFails_SettingsNotWritten_AlertShown()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Core.NextId = "3";
        h.Vault.SaveException = new IOException("denied");
        var editor = Editors.Add(h.Model);
        Fill(editor);

        Assert.IsFalse(await editor.SaveAsync());

        Assert.AreEqual(0, h.Log.Count(x => x == "settings:save"));
        Assert.AreEqual(2, h.Settings.Relays.Count);
        Assert.AreEqual("没有保存", editor.SaveAlert!.Title);
    }

    // ---------- 编辑 ----------

    [TestMethod]
    public async Task Edit_PrefillsFieldsFromSettingsAndVault()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        var editor = Editors.Edit(h.Model, "2");

        Assert.AreEqual("编辑配置", editor.Title);
        Assert.AreEqual("A家", editor.NameInput);
        Assert.AreEqual("https://relay2.example.com/v1", editor.BaseUrlInput);
        Assert.AreEqual("sk-example-0002", editor.ApiKeyInput);
    }

    [TestMethod]
    public async Task Edit_SameNameAsItself_NotDuplicate()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var editor = Editors.Edit(h.Model, "2");
        editor.NameInput = "a家";

        Assert.IsTrue(await editor.SaveAsync());
        Assert.IsNull(editor.NameError);
    }

    [TestMethod]
    public async Task Edit_Success_CallsSaveRelayWithId_KeepsOrderAndId_NoticeText()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var editor = Editors.Edit(h.Model, "1");
        editor.NameInput = "主线路";

        Assert.IsTrue(await editor.SaveAsync());

        Assert.AreEqual("1", h.Core.ParamsOf("save_relay")["id"]);
        Assert.AreEqual(new RelayConfig("1", "主线路", "https://relay.example.com/v1"), h.Settings.Relays[0]);
        Assert.AreEqual("A家", h.Settings.Relays[1].Name);
        var notice = Find(h, "已保存「主线路」");
        Assert.IsNotNull(notice);
        Assert.AreEqual("重启 ChatGPT 后生效。", notice.Detail);
        Assert.AreEqual(AppSeverity.Success, notice.Severity);
    }

    [TestMethod]
    public async Task Edit_SettingsWriteFails_RestoresOldKey()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Settings.SaveException = new IOException("disk full");
        var editor = Editors.Edit(h.Model, "2");
        editor.ApiKeyInput = "sk-example-9999";

        Assert.IsFalse(await editor.SaveAsync());

        Assert.AreEqual("sk-example-0002", h.Vault.Keys["2"]);
        Assert.AreEqual("A家", h.Settings.Relays[1].Name);
        Assert.AreEqual("没有保存", editor.SaveAlert!.Title);
    }

    // ---------- 删除 ----------

    [TestMethod]
    public async Task Delete_OnlyOneRelay_CannotDelete_ShowsMinimumHint()
    {
        var h = new MultiHarness();
        h.Seed("1", "中转", "https://relay.example.com/v1", "sk-example-0001");
        await h.Model.StartAsync();
        h.Push();

        Assert.IsFalse(h.Model.Relays[0].CanDelete);
        Assert.AreEqual("至少保留一个配置。", h.Model.RelayMinimumText);

        await h.Model.PerformDeleteRelayAsync("1");

        Assert.AreEqual(0, h.Core.Count("delete_relay"));
        Assert.AreEqual(0, h.Confirmer.Requests.Count);
    }

    [TestMethod]
    public async Task Delete_TwoRelays_NoMinimumHint_CanDelete()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        Assert.IsNull(h.Model.RelayMinimumText);
        Assert.IsTrue(h.Model.Relays.All(r => r.CanDelete));
    }

    [TestMethod]
    public void DeleteDialog_ExactTitleBodyAndButtons()
    {
        var dialog = DialogModel.ForDeleteRelay("A家", 3, isDeleting: false);

        Assert.AreEqual("删除配置「A家」？", dialog.Title);
        Assert.IsTrue(dialog.Message.StartsWith("删除后："));
        Assert.IsTrue(dialog.Message.Contains("· Codex 的模型列表里不再有「A家」的 3 个模型"));
        Assert.IsTrue(dialog.Message.Contains("· 它的 API Key 会从系统凭据管理器里删除"));
        Assert.IsTrue(dialog.Message.EndsWith("\n重启 ChatGPT 后生效。"));
        Assert.AreEqual("取消", dialog.Buttons[0].Text);
        Assert.AreEqual("删除", dialog.Buttons[1].Text);
    }

    [TestMethod]
    public async Task Delete_Confirm_SettingsThenCoreThenKey_AndSuccessNotice()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Log.Clear();
        h.Core.Entries.RemoveAt(1);

        await h.Model.PerformDeleteRelayAsync("2");

        CollectionAssert.AreEqual(
            new[] { "settings:save", "call:delete_relay", "vault:delete" },
            h.Log.ToArray());
        Assert.AreEqual("2", h.Core.ParamsOf("delete_relay")["id"]);
        CollectionAssert.AreEqual(new[] { "1" }, h.Settings.Relays.Select(r => r.Id).ToArray());
        Assert.IsFalse(h.Vault.Keys.ContainsKey("2"));
        Assert.AreEqual(1, h.Model.Relays.Count);
        var notice = Find(h, "已删除「A家」");
        Assert.IsNotNull(notice);
        Assert.AreEqual("重启 ChatGPT 后生效。", notice.Detail);
        Assert.AreEqual(AppSeverity.Success, notice.Severity);
    }

    [TestMethod]
    public async Task Delete_SettingsWriteFails_DangerNotice_ListAndKeyUnchanged()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Settings.SaveException = new IOException("disk full");

        await h.Model.PerformDeleteRelayAsync("2");

        var notice = Find(h, "没有删除「A家」");
        Assert.IsNotNull(notice);
        Assert.AreEqual("无法写入设置文件。", notice.Detail);
        Assert.AreEqual(AppSeverity.Danger, notice.Severity);
        Assert.AreEqual(2, h.Model.Relays.Count);
        Assert.AreEqual(2, h.Settings.Relays.Count);
        Assert.IsTrue(h.Vault.Keys.ContainsKey("2"));
    }

    [TestMethod]
    public async Task Delete_CoreFails_SettingsWrittenBack_KeyKept_DangerNoticeWithReason()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var before = h.Settings.Relays.ToArray();
        h.Core.Fail("delete_relay", "relay_not_found");

        await h.Model.PerformDeleteRelayAsync("2");

        CollectionAssert.AreEqual(before, h.Settings.Relays);
        Assert.IsTrue(h.Vault.Keys.ContainsKey("2"));
        Assert.AreEqual(0, h.Log.Count(x => x == "vault:delete"));
        Assert.AreEqual(2, h.Model.Relays.Count);
        var notice = Find(h, "没有删除「A家」");
        Assert.IsNotNull(notice);
        Assert.AreEqual("这个配置不存在，请刷新后重试。", notice.Detail);
        Assert.AreEqual(AppSeverity.Danger, notice.Severity);
    }

    // ---------- 回滚与失败说明 ----------

    const string AppliedButNotSaved = "已在本次运行中生效，但没能保存到本机，重启助手后会恢复成原来的样子。";

    [TestMethod]
    public async Task Add_SettingsWriteFails_CoreToldToDeleteNewRelay()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Core.NextId = "3";
        h.Settings.SaveException = new IOException("disk full");
        var editor = Editors.Add(h.Model);
        Fill(editor);

        await editor.SaveAsync();

        Assert.AreEqual("3", h.Core.ParamsOf("delete_relay")["id"]);
        Assert.IsFalse(editor.SaveAlert!.Message.Contains(AppliedButNotSaved));
    }

    [TestMethod]
    public async Task Add_SettingsWriteFails_RevertCallFails_AlertAddsAppliedButNotSavedLine()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Core.NextId = "3";
        h.Settings.SaveException = new IOException("disk full");
        h.Core.Fail("delete_relay", "internal_error");
        var editor = Editors.Add(h.Model);
        Fill(editor);

        Assert.IsFalse(await editor.SaveAsync());

        Assert.AreEqual("没有保存", editor.SaveAlert!.Title);
        var lines = editor.SaveAlert.Message.Split('\n');
        Assert.AreEqual(2, lines.Length);
        Assert.IsTrue(lines[0].Length > 0);
        Assert.AreEqual(AppliedButNotSaved, lines[1]);
    }

    [TestMethod]
    public async Task Edit_SettingsWriteFails_CoreRestoredWithOldValues_NoExtraLine()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Settings.SaveException = new IOException("disk full");
        var editor = Editors.Edit(h.Model, "2");
        editor.NameInput = "新名字";
        editor.BaseUrlInput = "https://new.example.com/v1";
        editor.ApiKeyInput = "sk-example-9999";

        await editor.SaveAsync();

        Assert.AreEqual(2, h.Core.Count("save_relay"));
        var revert = h.Core.ParamsOf("save_relay");
        Assert.AreEqual("2", revert["id"]);
        Assert.AreEqual("A家", revert["name"]);
        Assert.AreEqual("https://relay2.example.com/v1", revert["base_url"]);
        Assert.AreEqual("sk-example-0002", revert["api_key"]);
        Assert.IsFalse(editor.SaveAlert!.Message.Contains(AppliedButNotSaved));
    }

    [TestMethod]
    public async Task Edit_SettingsWriteFails_RevertCallFails_AlertAddsAppliedButNotSavedLine()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Settings.SaveException = new IOException("disk full");
        var calls = 0;
        h.Core.OnAsync("save_relay", _ => ++calls == 1
            ? Task.FromResult(RelayJson.Parse("""{"id":"2","name":"新名字","model_count":3,"models_changed":true}"""))
            : Task.FromException<JsonElement>(new CoreCallException("internal_error", [])));
        var editor = Editors.Edit(h.Model, "2");
        editor.NameInput = "新名字";

        await editor.SaveAsync();

        Assert.AreEqual("没有保存", editor.SaveAlert!.Title);
        Assert.AreEqual(AppliedButNotSaved, editor.SaveAlert.Message.Split('\n').Last());
    }

    [TestMethod]
    public async Task Edit_OldKeyUnreadable_SettingsWriteFails_RevertImpossible_AlertAddsLine()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Vault.Keys.Remove("2");
        h.Settings.SaveException = new IOException("disk full");
        var editor = Editors.Edit(h.Model, "2");
        editor.ApiKeyInput = "sk-example-9999";

        await editor.SaveAsync();

        Assert.AreEqual(1, h.Core.Count("save_relay"));
        Assert.AreEqual("没有保存", editor.SaveAlert!.Title);
        Assert.AreEqual(AppliedButNotSaved, editor.SaveAlert.Message.Split('\n').Last());
    }

    [TestMethod]
    public async Task Setup_KeyWriteFails_ResetsCoreWithEmptyRelays()
    {
        var h = new MultiHarness();
        await h.Model.StartAsync();
        h.Vault.SaveException = new IOException("denied");
        h.Model.BaseUrlInput = "https://relay.example.com/v1";
        h.Model.ApiKeyInput = "sk-example-0001";

        await h.Model.CompleteSetupAsync();

        AssertCoreReset(h);
    }

    [TestMethod]
    public async Task Setup_SettingsWriteFails_ResetsCoreWithEmptyRelays()
    {
        var h = new MultiHarness();
        await h.Model.StartAsync();
        h.Settings.SaveException = new IOException("disk full");
        h.Model.BaseUrlInput = "https://relay.example.com/v1";
        h.Model.ApiKeyInput = "sk-example-0001";

        await h.Model.CompleteSetupAsync();

        AssertCoreReset(h);
    }

    [TestMethod]
    public async Task Setup_LocalWriteFails_ResetInitAlsoFails_NoticeAddsAppliedButNotSavedLine()
    {
        var h = new MultiHarness();
        await h.Model.StartAsync();
        h.Settings.SaveException = new IOException("disk full");
        h.Core.Fail("init", "internal_error");
        h.Model.BaseUrlInput = "https://relay.example.com/v1";
        h.Model.ApiKeyInput = "sk-example-0001";

        await h.Model.CompleteSetupAsync();

        var notice = Find(h, "没有保存");
        Assert.IsNotNull(notice);
        var lines = notice.Detail.Split('\n');
        Assert.AreEqual(2, lines.Length);
        Assert.IsTrue(lines[0].Length > 0);
        Assert.AreEqual(AppliedButNotSaved, lines[1]);
        Assert.AreEqual(AppPage.Setup, h.Model.Page);
    }

    [TestMethod]
    public async Task Setup_LocalWriteFails_ResetInitSucceeds_NoticeHasNoAppliedLine()
    {
        var h = new MultiHarness();
        await h.Model.StartAsync();
        h.Settings.SaveException = new IOException("disk full");
        h.Model.BaseUrlInput = "https://relay.example.com/v1";
        h.Model.ApiKeyInput = "sk-example-0001";

        await h.Model.CompleteSetupAsync();

        var notice = Find(h, "没有保存");
        Assert.IsNotNull(notice);
        Assert.IsFalse(notice.Detail.Contains(AppliedButNotSaved));
    }

    static void AssertCoreReset(MultiHarness h)
    {
        var init = h.Core.ParamsOf("init");
        Assert.AreEqual(0, ((System.Collections.IEnumerable)init["relays"]!).Cast<object>().Count());
        Assert.AreEqual(@"C:\Apps\ChatGPT", init["chatgpt_dir"]);
    }

    [TestMethod]
    public async Task Delete_Succeeds_KeyDeleteFails_WarningNoticeWithExtraLine()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Vault.DeleteException = new IOException("denied");

        await h.Model.PerformDeleteRelayAsync("2");

        var notice = Find(h, "已删除「A家」");
        Assert.IsNotNull(notice);
        Assert.AreEqual("重启 ChatGPT 后生效。\n但没能从系统凭据管理器里删除它的 API Key。", notice.Detail);
        Assert.AreEqual(AppSeverity.Warning, notice.Severity);
        Assert.AreEqual(1, h.Settings.Relays.Count);
    }

    [TestMethod]
    public async Task Delete_CoreFails_SettingsRestoreAlsoFails_DangerNoticeWithRelayWillVanishLine()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Settings.FailFromCall = 2;
        h.Core.Fail("delete_relay", "relay_not_found");

        await h.Model.PerformDeleteRelayAsync("2");

        var notice = Find(h, "没有删除「A家」");
        Assert.IsNotNull(notice);
        Assert.AreEqual(
            "这个配置不存在，请刷新后重试。\n本机设置没能恢复，重启助手后这个配置会消失，需要重新添加。",
            notice.Detail);
        Assert.AreEqual(AppSeverity.Danger, notice.Severity);
        Assert.IsTrue(h.Vault.Keys.ContainsKey("2"));
    }

    [TestMethod]
    public async Task Delete_InProgress_ExposesDeletingRelayId_ThenResets()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("delete_relay", _ => gate.Task);

        var deleting = h.Model.PerformDeleteRelayAsync("2");

        Assert.AreEqual("2", h.Model.DeletingRelayId);
        Assert.AreEqual(0, h.Confirmer.Requests.Count);

        gate.SetResult(RelayJson.Status(true, [new("1", "中转")]));
        await deleting;

        Assert.IsNull(h.Model.DeletingRelayId);
        Assert.IsNotNull(Find(h, "已删除「A家」"));
    }

    [TestMethod]
    public async Task Delete_Failed_ResetsDeletingRelayId()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Core.Fail("delete_relay", "relay_not_found");

        await h.Model.PerformDeleteRelayAsync("2");

        Assert.IsNull(h.Model.DeletingRelayId);
    }

    [TestMethod]
    public async Task Uninstall_DeletesAllKeysByPrefix_EvenWhenSettingsUnreadable()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Vault.Keys["7"] = "sk-example-0007";
        h.Settings.Relays.Clear();

        await h.Model.UninstallAsync();

        Assert.AreEqual(1, h.Vault.DeleteAllCount);
        Assert.AreEqual(0, h.Vault.Keys.Count);
    }

    [TestMethod]
    public async Task IsAlreadyCleaned_NoRelaysButKeyLeft_False()
    {
        var h = new MultiHarness();
        h.Vault.Keys["2"] = "sk-example-0002";

        Assert.IsFalse(h.Model.IsAlreadyCleaned());
    }

    [TestMethod]
    public void IsAlreadyCleaned_NothingLeft_True()
    {
        Assert.IsTrue(new MultiHarness().Model.IsAlreadyCleaned());
    }

    // ---------- 删除弹窗 ----------

    [TestMethod]
    public void DeleteDialog_Idle_ButtonsAndMessage()
    {
        var dialog = DialogModel.ForDeleteRelay("A家", 3, isDeleting: false);

        Assert.AreEqual("删除配置「A家」？", dialog.Title);
        Assert.IsTrue(dialog.Message.Contains("「A家」的 3 个模型"));
        CollectionAssert.AreEqual(new[] { "取消", "删除" }, dialog.Buttons.Select(b => b.Text).ToArray());
        Assert.IsTrue(dialog.Buttons.All(b => b.IsEnabled));
    }

    [TestMethod]
    public void DeleteDialog_Deleting_CancelDisabledAndButtonShowsProgress()
    {
        var dialog = DialogModel.ForDeleteRelay("A家", 3, isDeleting: true);

        Assert.IsFalse(dialog.Buttons[0].IsEnabled);
        Assert.AreEqual("正在删除…", dialog.Buttons[1].Text);
    }

    // ---------- 列表说明 ----------

    [TestMethod]
    public async Task Count_ThreeRelays_ShowsMaxTenHint_AddEnabled()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Seed("3", "B家", "https://relay3.example.com/v1", "sk-example-0003");
        await h.Model.StartAsync();
        h.Push();

        Assert.AreEqual("已有 3 个，最多 10 个。", h.Model.RelayCountText);
        Assert.IsTrue(h.Model.CanAddRelay);
    }

    [TestMethod]
    public async Task Count_TenRelays_ReachedLimit_AddDisabled()
    {
        var h = new MultiHarness();
        for (var i = 1; i <= 10; i++) h.Seed(i.ToString(), "配置" + i, $"https://r{i}.example.com", "sk-example-" + i);
        await h.Model.StartAsync();
        h.Push();

        Assert.AreEqual("已有 10 个，达到上限。", h.Model.RelayCountText);
        Assert.IsFalse(h.Model.CanAddRelay);
    }

    [TestMethod]
    public async Task Count_NineRelays_StillCanAdd()
    {
        var h = new MultiHarness();
        for (var i = 1; i <= 9; i++) h.Seed(i.ToString(), "配置" + i, $"https://r{i}.example.com", "sk-example-" + i);
        await h.Model.StartAsync();
        h.Push();

        Assert.AreEqual("已有 9 个，最多 10 个。", h.Model.RelayCountText);
        Assert.IsTrue(h.Model.CanAddRelay);
    }

    // ---------- 每行状态 ----------

    [TestMethod]
    public async Task Row_Healthy_ShowsModelCount()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        var row = h.Model.Relays[1];
        Assert.AreEqual(RelayRowState.Normal, row.State);
        Assert.AreEqual("3 个模型", row.StatusText);
        Assert.IsFalse(row.CanRetry);
        Assert.IsTrue(row.CanEdit);
        Assert.AreEqual("https://relay2.example.com/v1", row.BaseUrl);
        Assert.AreEqual("A家", row.Name);
    }

    [TestMethod]
    public async Task Row_Stale_ShowsKeptCountAndRetry_OthersUnaffected_NoGlobalBanner()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        h.Push(entries: [new("1", "中转", 6), new("2", "A家", 3, Stale: true, Error: "relay_unreachable")]);

        Assert.AreEqual(RelayRowState.Normal, h.Model.Relays[0].State);
        var row = h.Model.Relays[1];
        Assert.AreEqual(RelayRowState.Stale, row.State);
        Assert.AreEqual("没更新，沿用上次的 3 个", row.StatusText);
        Assert.IsTrue(row.CanRetry);
        Assert.AreEqual(0, h.Model.Banners.Count);
    }

    [TestMethod]
    public async Task Row_NeverFetched_ShowsNotFetchedAndRetry_NoGlobalBanner()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        h.Push(entries: [new("1", "中转"), new("2", "A家", 0, FetchedAt: null, Error: "relay_401")]);

        var row = h.Model.Relays[1];
        Assert.AreEqual(RelayRowState.NeverFetched, row.State);
        Assert.AreEqual("还没获取到模型", row.StatusText);
        Assert.IsTrue(row.CanRetry);
        Assert.AreEqual(0, h.Model.Banners.Count);
    }

    [TestMethod]
    public async Task Row_ForwardingOff_ShowsNotFetchedWhenOff()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        h.Push(forwarding: false);

        Assert.IsTrue(h.Model.Relays.All(r => r.State == RelayRowState.ForwardingOff));
        Assert.IsTrue(h.Model.Relays.All(r => r.StatusText == "转发关闭时不获取"));
        Assert.IsTrue(h.Model.Relays.All(r => !r.CanRetry));
    }

    [TestMethod]
    public async Task Row_Retry_FetchingOnlyThatRow_CallsRefreshWithId()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        h.Push(entries: [new("1", "中转"), new("2", "A家", 3, Stale: true, Error: "relay_unreachable")]);
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("refresh_models", _ => gate.Task);

        var retry = h.Model.RetryRelayAsync("2");

        var row = h.Model.Relays[1];
        Assert.AreEqual(RelayRowState.Fetching, row.State);
        Assert.AreEqual("正在获取…", row.StatusText);
        Assert.IsFalse(row.CanEdit);
        Assert.IsFalse(row.CanDelete);
        Assert.AreEqual(RelayRowState.Normal, h.Model.Relays[0].State);
        Assert.IsTrue(h.Model.Relays[0].CanEdit);

        gate.SetResult(RelayJson.Status(true, [new("1", "中转"), new("2", "A家", 4)]));
        await retry;

        Assert.AreEqual("2", h.Core.ParamsOf("refresh_models")["id"]);
        Assert.AreEqual(RelayRowState.Normal, h.Model.Relays[1].State);
        Assert.AreEqual("4 个模型", h.Model.Relays[1].StatusText);
        Assert.IsTrue(h.Model.Relays[1].CanEdit);
    }

    // ---------- 转发总览与底部 ----------

    [TestMethod]
    public async Task Overview_BothMode_TitleAndText()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        Assert.AreEqual("使用中转 · 已开启", h.Model.OverviewTitle);
        Assert.AreEqual("官方模型在上，中转模型在下；选官方模型时仍访问官方服务。", h.Model.OverviewText);
        Assert.AreEqual("官方 + 中转", h.Model.AccessModeName);
    }

    [TestMethod]
    public async Task Overview_RelayOnly_NamesDefaultRelayAuto()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        h.Push(defaultRelay: "A家", mode: "relay");

        Assert.AreEqual("不用登录 ChatGPT。只有中转模型，默认选中「A家 · auto」。", h.Model.OverviewText);
        Assert.AreEqual("只用中转", h.Model.AccessModeName);
    }

    [TestMethod]
    public async Task Overview_RelayOnly_NoDefaultRelay_SaysNoDefaultModel()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        h.Push(defaultRelay: null, mode: "relay");

        Assert.AreEqual("不用登录 ChatGPT。只有中转模型，没有默认选中的模型。", h.Model.OverviewText);
    }

    [TestMethod]
    public async Task Overview_ForwardingOff_TitleTextAndNoAccessMode()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        h.Push(forwarding: false);

        Assert.AreEqual("使用中转 · 已关闭", h.Model.OverviewTitle);
        Assert.AreEqual("已关闭：Codex 配置已还原为开启前的样子。", h.Model.OverviewText);
        Assert.IsNull(h.Model.AccessModeName);
    }

    [TestMethod]
    public async Task Footer_SumsModelsAndCountsRelays()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        h.Push(entries: [new("1", "中转", 6), new("2", "A家", 3)]);

        Assert.AreEqual("中转模型 9 个（2 个配置）", h.Model.RelayModelSummary);
    }

    [TestMethod]
    public async Task Footer_ForwardingOff_NotFetchedText()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        h.Push(forwarding: false);

        Assert.AreEqual("中转模型 转发关闭时不获取", h.Model.RelayModelSummary);
    }

    // ---------- 首次设置 ----------

    [TestMethod]
    public void Setup_Texts()
    {
        Assert.AreEqual("连接中转服务", AppModel.SetupHeading);
        Assert.AreEqual("填写中转服务提供的 Base URL 和 API Key。设置完成后，Codex 的模型列表里会多出中转模型。", AppModel.SetupDescription);
        Assert.AreEqual("正在连接中转…", AppModel.ConnectingText);
    }

    [TestMethod]
    public void Setup_NameDefaultsToRelay()
    {
        Assert.AreEqual("中转", new MultiHarness().Model.NameInput);
    }

    [TestMethod]
    public async Task Setup_NameRulesApply()
    {
        var h = new MultiHarness();
        await h.Model.StartAsync();
        h.Model.NameInput = "a·b";
        h.Model.BaseUrlInput = "https://relay.example.com/v1";
        h.Model.ApiKeyInput = "sk-example-0001";

        await h.Model.CompleteSetupAsync();

        Assert.AreEqual("名字里不能有「·」。", h.Model.NameError);
        Assert.AreEqual(0, h.Core.Count("save_relay"));
    }

    [TestMethod]
    public async Task Setup_Success_SavesFirstRelay_AndShowsSuccessPageWithTaskbarWording()
    {
        var h = new MultiHarness();
        await h.Model.StartAsync();
        h.Core.NextId = "1";
        h.Model.BaseUrlInput = "https://relay.example.com/v1";
        h.Model.ApiKeyInput = "sk-example-0001";

        await h.Model.CompleteSetupAsync();

        Assert.IsNull(h.Core.ParamsOf("save_relay")["id"]);
        Assert.AreEqual("中转", h.Core.ParamsOf("save_relay")["name"]);
        CollectionAssert.AreEqual(new[] { new RelayConfig("1", "中转", "https://relay.example.com/v1") }, h.Settings.Relays);
        Assert.AreEqual("sk-example-0001", h.Vault.Keys["1"]);
        Assert.AreEqual(AppPage.Success, h.Model.Page);
        Assert.AreEqual("设置完成，转发已开启", h.Model.SuccessTitle);
        Assert.IsTrue(h.Model.SuccessText.Contains("任务栏通知区域"));
        Assert.IsTrue(h.Model.SuccessText.Contains("中转模型"));
        Assert.IsFalse(h.Model.SuccessText.Contains(LegacyZhong));
    }

    [TestMethod]
    public async Task Setup_CoreFails_TaskErrorTitleIsCouldNotConnect_NothingSaved()
    {
        var h = new MultiHarness();
        await h.Model.StartAsync();
        h.Core.Fail("save_relay", "relay_401");
        h.Model.BaseUrlInput = "https://relay.example.com/v1";
        h.Model.ApiKeyInput = "sk-example-0001";

        await h.Model.CompleteSetupAsync();

        Assert.AreEqual("没能连上中转服务", h.Model.TaskError!.Title);
        Assert.AreEqual(0, h.Settings.Relays.Count);
        Assert.AreEqual(0, h.Vault.Keys.Count);
        Assert.AreEqual(AppPage.Setup, h.Model.Page);
    }

    // ---------- 托盘 / 文案 ----------

    [TestMethod]
    public async Task Tray_ForwardingLabel_IsUseRelay()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        var item = TrayPanelModel.From(h.Model).Groups.SelectMany(g => g).Single(i => i.Command == TrayMenuCommand.ToggleForwarding);

        Assert.AreEqual("使用中转", item.Label);
    }

    [TestMethod]
    public void NoLegacyWordingInWindowsSourcesOrTests()
    {
        var windows = FindWindowsDir();
        var hits = Directory.EnumerateFiles(windows, "*.*", SearchOption.AllDirectories)
            .Where(f => f.EndsWith(".cs") || f.EndsWith(".xaml"))
            .Where(f => !f.Contains(Path.DirectorySeparatorChar + "obj" + Path.DirectorySeparatorChar)
                     && !f.Contains(Path.DirectorySeparatorChar + "bin" + Path.DirectorySeparatorChar))
            .Where(f => File.ReadAllText(f).Contains(LegacyZhong))
            .Select(f => Path.GetRelativePath(windows, f))
            .ToArray();

        CollectionAssert.AreEqual(Array.Empty<string>(), hits);
    }

    static string FindWindowsDir()
    {
        for (var dir = new DirectoryInfo(AppContext.BaseDirectory); dir != null; dir = dir.Parent)
            if (Directory.Exists(Path.Combine(dir.FullName, "ModelBridge.App")) && Directory.Exists(Path.Combine(dir.FullName, "ModelBridge.Tests")))
                return dir.FullName;
        throw new DirectoryNotFoundException("从测试程序集目录向上找不到 windows 目录");
    }
}
