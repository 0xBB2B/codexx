using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.Services;
using ModelBridge.UI;

namespace ModelBridge.Tests;

using Params = IReadOnlyDictionary<string, object?>;

sealed class FakeCore(List<string> log) : ICoreClient
{
    readonly Dictionary<string, Func<Params, Task<JsonElement>>> handlers = [];

    public List<(string Method, Params Params)> Calls { get; } = [];
    public Exception? StartException { get; set; }
    public CoreInitParams? StartedWith { get; private set; }

    public event Action<JsonElement>? StatusReceived;
    public event Action<string>? Failed;

    public Task StartAsync(CoreInitParams init)
    {
        StartedWith = init;
        return StartException is { } ex ? Task.FromException(ex) : Task.CompletedTask;
    }

    public int StopCount { get; private set; }

    public Task StopAsync()
    {
        StopCount++;
        return Task.CompletedTask;
    }

    public void On(string method, Func<Params, JsonElement> handler) =>
        handlers[method] = p => Task.FromResult(handler(p));

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
            "status" => Json.Status(),
            "save_relay" => Json.Parse(new JsonObject
            {
                ["id"] = p["id"] as string ?? "1",
                ["name"] = p["name"] as string,
                ["model_count"] = 6,
                ["models_changed"] = false,
            }.ToJsonString()),
            "set_forwarding" => Json.Status(s => s["forwarding"] = Convert.ToBoolean(p["enabled"])),
            "check_update" => Json.Parse("""{"latest_version":null,"url":null,"is_newer":false}"""),
            _ => Json.Parse("{}"),
        });
    }

    public int Count(string method) => Calls.Count(c => c.Method == method);
    public Params ParamsOf(string method) => Calls.Last(c => c.Method == method).Params;
    public void RaiseStatus(JsonElement status) => StatusReceived?.Invoke(status);
    public void RaiseFailed(string reason) => Failed?.Invoke(reason);
}

static class Json
{
    public static JsonElement Parse(string text) => JsonDocument.Parse(text).RootElement.Clone();

    public static JsonElement Status(Action<JsonObject>? tweak = null)
    {
        var status = new JsonObject
        {
            ["version"] = "1.2.0",
            ["forwarding"] = false,
            ["proxy"] = new JsonObject { ["running"] = true, ["error"] = null },
            ["chatgpt"] = new JsonObject { ["found"] = true, ["error"] = null },
            ["relays"] = new JsonArray(new JsonObject
            {
                ["id"] = "1",
                ["name"] = "中转",
                ["model_count"] = 6,
                ["fetched_at"] = "2026-11-11T00:12:00Z",
                ["stale"] = false,
                ["error"] = null,
            }),
            ["default_relay"] = "中转",
            ["taken_over"] = new JsonArray(),
            ["restore_blocked"] = new JsonArray(),
            ["config_path"] = "/home/example/.codex/config.toml",
            ["update"] = new JsonObject { ["latest_version"] = null, ["url"] = null },
        };
        tweak?.Invoke(status);
        return Parse(status.ToJsonString());
    }
}

sealed class FakeVault(List<string> log) : ICredentialVault
{
    public Dictionary<string, string> Keys { get; } = [];
    public Exception? SaveException { get; set; }
    public Exception? ReadException { get; set; }
    public Exception? DeleteException { get; set; }
    public List<string> Saved { get; } = [];
    public List<string> DeletedIds { get; } = [];
    public int DeleteCount => DeletedIds.Count;
    public int DeleteAllCount { get; private set; }

    public string? Stored
    {
        get => Keys.GetValueOrDefault("1");
        set
        {
            if (value == null) Keys.Remove("1");
            else Keys["1"] = value;
        }
    }

    public void Save(string id, string key)
    {
        log.Add("vault:save");
        if (SaveException != null) throw SaveException;
        Keys[id] = key;
        Saved.Add(key);
    }

    public string? Read(string id) => ReadException != null ? throw ReadException : Keys.GetValueOrDefault(id);

    public void Delete(string id)
    {
        DeletedIds.Add(id);
        if (DeleteException != null) throw DeleteException;
        Keys.Remove(id);
    }

    public void DeleteAll()
    {
        DeleteAllCount++;
        if (DeleteException != null) throw DeleteException;
        Keys.Clear();
    }

    public bool Any() => ReadException != null ? throw ReadException : Keys.Count > 0;
}

sealed class FakeSettings(List<string> log) : IShellSettingsStore
{
    public List<RelayConfig> Relays { get; set; } = [];
    public Exception? ReadException { get; set; }
    public Exception? SaveException { get; set; }
    public Exception? DeleteException { get; set; }
    public int DeleteCount { get; private set; }

    public string? BaseUrl
    {
        get => Relays.FirstOrDefault()?.BaseUrl;
        set => Relays = value == null ? [] : [new RelayConfig("1", "中转", value)];
    }

    public IReadOnlyList<RelayConfig> ReadRelays() => ReadException != null ? throw ReadException : Relays.ToList();

    public void SaveRelays(IReadOnlyList<RelayConfig> relays)
    {
        log.Add("settings:save");
        if (SaveException != null) throw SaveException;
        Relays = relays.ToList();
    }

    public void DeleteAll()
    {
        DeleteCount++;
        if (DeleteException != null) throw DeleteException;
        Relays = [];
    }
}

sealed class FakeLogin(List<string> log) : ILoginStart
{
    public bool Enabled { get; set; }
    public Exception? EnableException { get; set; }
    public Exception? DisableException { get; set; }
    public int EnableCount { get; private set; }
    public int DisableCount { get; private set; }

    public bool IsEnabled() => Enabled;

    public void Enable()
    {
        EnableCount++;
        log.Add("login:enable");
        if (EnableException != null) throw EnableException;
        Enabled = true;
    }

    public void Disable()
    {
        DisableCount++;
        log.Add("login:disable");
        if (DisableException != null) throw DisableException;
        Enabled = false;
    }
}

sealed class FakeChatGpt : IChatGptControl
{
    public bool Running { get; set; }
    public string? LocatedDir { get; set; } = @"C:\Apps\ChatGPT";

    public string? Locate() => LocatedDir;
    public bool IsRunning() => Running;
}

sealed class FakeConfirmer(List<string> log) : IConfirmer
{
    public List<ConfirmRequest> Requests { get; } = [];
    public Func<ConfirmRequest, bool> Answer { get; set; } = _ => true;
    public TaskCompletionSource<bool>? Gate { get; set; }

    public Task<bool> ConfirmAsync(ConfirmRequest request)
    {
        Requests.Add(request);
        log.Add("confirm");
        return Gate?.Task ?? Task.FromResult(Answer(request));
    }
}

sealed class Harness
{
    public List<string> Log { get; } = [];
    public FakeCore Core { get; }
    public FakeVault Vault { get; }
    public FakeSettings Settings { get; }
    public FakeLogin Login { get; }
    public FakeChatGpt ChatGpt { get; }
    public FakeConfirmer Confirmer { get; }
    public AppModel Model { get; }
    public int ShowMainCount { get; private set; }
    public int ExitCount { get; private set; }

    public Harness()
    {
        Core = new FakeCore(Log);
        Vault = new FakeVault(Log);
        Settings = new FakeSettings(Log);
        Login = new FakeLogin(Log);
        ChatGpt = new FakeChatGpt();
        Confirmer = new FakeConfirmer(Log);
        Model = new AppModel(Core, Vault, Settings, Login, ChatGpt, Confirmer);
        Model.ShowMainWindowRequested += () => ShowMainCount++;
        Model.ExitRequested += () => ExitCount++;
    }

    public void Seed(string id, string name, string url, string key)
    {
        Settings.Relays.Add(new RelayConfig(id, name, url));
        Vault.Keys[id] = key;
    }

    public void FillInputs(string baseUrl = "https://relay.example.com", string apiKey = "sk-example-0000")
    {
        Model.BaseUrlInput = baseUrl;
        Model.ApiKeyInput = apiKey;
    }

    public void Push(Action<JsonObject>? tweak = null) => Core.RaiseStatus(Json.Status(tweak));

    public void ForwardingOn() => Push(s => s["forwarding"] = true);
}

[TestClass]
public class AppModelTests
{
    const string OldUrl = "https://old.example.com";
    const string OldKey = "sk-example-old0";
    const string NoSave = "没有保存";
    const string CredentialWriteFailed = "无法写入系统凭据管理器";

    static string Text(AppNotice? n) => n == null ? "" : n.Title + n.Detail;

    static string AllText(Harness h) => string.Concat(h.Model.Notices.Select(Text));

    static AppNotice? Find(Harness h, string title) => h.Model.Notices.FirstOrDefault(n => n.Title == title);

    static string[] Titles(Harness h) => h.Model.Notices.Select(n => n.Title).ToArray();

    static async Task WaitUntilAsync(Func<bool> condition)
    {
        var deadline = DateTime.UtcNow.AddSeconds(5);
        while (!condition() && DateTime.UtcNow < deadline) await Task.Delay(20);
    }

    const string RestartHint = "重启 ChatGPT 后生效。";
    const string UpToDate = "已是最新版本";
    const string UpdateUnavailable = "暂时无法检查更新，请稍后再试";
    const string CoreDown = "后台服务没能启动，请退出后重新打开";

    static string Msg(string code) => CoreErrorMessages.Messages[code];

    #region 首次设置

    [TestMethod]
    public async Task Setup_EmptyFields_FieldErrorsAndCoreNotCalled()
    {
        var h = new Harness();
        await h.Model.CompleteSetupAsync();

        Assert.IsFalse(string.IsNullOrEmpty(h.Model.BaseUrlError));
        Assert.IsFalse(string.IsNullOrEmpty(h.Model.ApiKeyError));
        Assert.AreEqual(0, h.Core.Count("save_relay"));
    }

    [TestMethod]
    public async Task Setup_OnlyKeyMissing_OnlyKeyError()
    {
        var h = new Harness();
        h.FillInputs(apiKey: "");
        await h.Model.CompleteSetupAsync();

        Assert.IsNull(h.Model.BaseUrlError);
        Assert.IsFalse(string.IsNullOrEmpty(h.Model.ApiKeyError));
        Assert.AreEqual(0, h.Core.Count("save_relay"));
    }

    [TestMethod]
    public async Task Setup_OnlyUrlMissing_OnlyUrlError()
    {
        var h = new Harness();
        h.FillInputs(baseUrl: "");
        await h.Model.CompleteSetupAsync();

        Assert.IsFalse(string.IsNullOrEmpty(h.Model.BaseUrlError));
        Assert.IsNull(h.Model.ApiKeyError);
        Assert.AreEqual(0, h.Core.Count("save_relay"));
    }

    [TestMethod]
    public async Task Setup_NonHttpsUrl_FieldErrorAndCoreNotCalled()
    {
        var h = new Harness();
        h.FillInputs(baseUrl: "http://relay.example.com");
        await h.Model.CompleteSetupAsync();

        Assert.AreEqual("Base URL 要以 https:// 开头。", h.Model.BaseUrlError);
        Assert.AreEqual(0, h.Core.Count("save_relay"));
    }

    [TestMethod]
    public async Task Setup_MarksConnectingWhileCallingCore()
    {
        var h = new Harness();
        h.FillInputs();
        var connectingDuringCall = false;
        h.Core.On("save_relay", _ =>
        {
            connectingDuringCall = h.Model.IsConnecting;
            return Json.Parse("""{"id":"1","name":"中转","model_count":6,"models_changed":false}""");
        });

        await h.Model.CompleteSetupAsync();

        Assert.IsTrue(connectingDuringCall);
        Assert.IsFalse(h.Model.IsConnecting);
    }

    [TestMethod]
    public async Task Setup_DuplicateSubmit_SendsSaveConnectionOnce()
    {
        var h = new Harness();
        h.FillInputs();
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("save_relay", _ => gate.Task);

        var first = h.Model.CompleteSetupAsync();
        var second = h.Model.CompleteSetupAsync();
        gate.SetResult(Json.Parse("""{"id":"1","name":"中转","model_count":6,"models_changed":false}"""));
        await Task.WhenAll(first, second);

        Assert.AreEqual(1, h.Core.Count("save_relay"));
    }

    [TestMethod]
    public async Task Setup_SendsEnteredUrlAndKeyToCore()
    {
        var h = new Harness();
        h.FillInputs();
        await h.Model.CompleteSetupAsync();

        var p = h.Core.ParamsOf("save_relay");
        Assert.AreEqual("https://relay.example.com", p["base_url"]);
        Assert.AreEqual("sk-example-0000", p["api_key"]);
    }

    [TestMethod]
    public async Task Setup_CoreFails_TaskErrorWithCodeMessage_NoCredentialWrite_InputKept()
    {
        var h = new Harness();
        h.FillInputs();
        h.Core.Fail("save_relay", "relay_401");

        await h.Model.CompleteSetupAsync();

        Assert.AreEqual("没能连上中转服务", h.Model.TaskError?.Title);
        Assert.AreEqual(Msg("relay_401"), h.Model.TaskError?.Detail);
        Assert.AreEqual(0, h.Vault.Saved.Count);
        Assert.AreEqual(AppPage.Setup, h.Model.Page);
        Assert.AreEqual("https://relay.example.com", h.Model.BaseUrlInput);
        Assert.AreEqual("sk-example-0000", h.Model.ApiKeyInput);
    }

    [TestMethod]
    public async Task Setup_CoreFails_ConnectingFlagCleared()
    {
        var h = new Harness();
        h.FillInputs();
        h.Core.Fail("save_relay", "relay_unreachable");

        await h.Model.CompleteSetupAsync();

        Assert.IsFalse(h.Model.IsConnecting);
    }

    [TestMethod]
    public async Task Setup_Success_WritesCredentialAndSettings()
    {
        var h = new Harness();
        h.FillInputs();
        await h.Model.CompleteSetupAsync();

        CollectionAssert.AreEqual(new[] { "sk-example-0000" }, h.Vault.Saved);
        Assert.AreEqual("https://relay.example.com", h.Settings.BaseUrl);
    }

    [TestMethod]
    public async Task Setup_WriteFails_NoSaveNoticeAndStaysOnSetup()
    {
        var h = new Harness();
        h.FillInputs();
        h.Vault.SaveException = new IOException();

        await h.Model.CompleteSetupAsync();

        Assert.AreEqual(NoSave, Find(h, NoSave)?.Title);
        Assert.AreEqual(CredentialWriteFailed, Find(h, NoSave)?.Detail);
        Assert.AreEqual(AppPage.Setup, h.Model.Page);
        Assert.AreEqual(0, h.Core.Count("set_forwarding"));
        Assert.AreEqual(0, h.Login.EnableCount);
    }

    [TestMethod]
    public async Task Setup_Success_EnablesForwardingThenLoginStart_ShowsSuccessPage()
    {
        var h = new Harness();
        h.FillInputs();
        await h.Model.CompleteSetupAsync();

        CollectionAssert.AreEqual(
            new[] { "call:save_relay", "vault:save", "settings:save", "call:set_forwarding", "login:enable" },
            h.Log);
        Assert.AreEqual(true, h.Core.ParamsOf("set_forwarding")["enabled"]);
        Assert.AreEqual(AppPage.Success, h.Model.Page);
        Assert.AreEqual("设置完成，转发已开启", h.Model.SuccessTitle);
        Assert.AreEqual("中转模型 6 个（1 个配置）", h.Model.RelayModelSummary);
        Assert.AreEqual(0, h.Model.Notices.Count);
    }

    [TestMethod]
    public async Task Setup_ForwardingFails_StillSuccessPageWithReasonNotice()
    {
        var h = new Harness();
        h.FillInputs();
        h.Core.Fail("set_forwarding", "config_invalid");

        await h.Model.CompleteSetupAsync();

        Assert.AreEqual(AppPage.Success, h.Model.Page);
        StringAssert.Contains(AllText(h), Msg("config_invalid"));
    }

    [TestMethod]
    public async Task Setup_LoginStartFails_StillSuccessPageWithReasonNotice()
    {
        var h = new Harness();
        h.FillInputs();
        h.Login.EnableException = new UnauthorizedAccessException();

        await h.Model.CompleteSetupAsync();

        Assert.AreEqual(AppPage.Success, h.Model.Page);
        StringAssert.Contains(AllText(h), "没能开启登录时启动");
    }

    [TestMethod]
    public async Task Setup_Success_TextEndsWithRestartChatGptLine()
    {
        var h = new Harness();
        h.FillInputs();
        await h.Model.CompleteSetupAsync();

        StringAssert.Contains(h.Model.SuccessText, "已获取 6 个中转模型");
        Assert.IsTrue(
            h.Model.SuccessText.EndsWith(
                "\n请重启 ChatGPT 后使用。助手会在你登录电脑时自动启动，在任务栏通知区域里常驻。",
                StringComparison.Ordinal),
            h.Model.SuccessText);
    }

    [TestMethod]
    public async Task Setup_LoginStartFails_TextStillHasAutoStartLine()
    {
        var h = new Harness();
        h.FillInputs();
        h.Login.EnableException = new UnauthorizedAccessException();

        await h.Model.CompleteSetupAsync();

        StringAssert.Contains(h.Model.SuccessText, "助手会在你登录电脑时自动启动，在任务栏通知区域里常驻。");
    }

    [TestMethod]
    public async Task FinishSetup_GoesToMainPage_WithoutAskingOrTouchingChatGpt()
    {
        var h = new Harness();
        h.FillInputs();
        await h.Model.CompleteSetupAsync();

        h.Model.FinishSetup();

        Assert.AreEqual(AppPage.Main, h.Model.Page);
        Assert.AreEqual(0, h.Confirmer.Requests.Count);
    }

    #endregion

    #region 转发开关

    [TestMethod]
    public async Task Toggle_ChatGptRunning_TurnOn_CallsCoreDirectlyWithoutConfirm()
    {
        var h = new Harness();
        h.ChatGpt.Running = true;

        await h.Model.ToggleForwardingAsync();

        Assert.AreEqual(0, h.Confirmer.Requests.Count);
        CollectionAssert.AreEqual(new[] { "call:set_forwarding" }, h.Log);
        Assert.AreEqual(true, h.Core.ParamsOf("set_forwarding")["enabled"]);
        Assert.IsTrue(h.Model.ForwardingEnabled);
    }

    [TestMethod]
    public async Task Toggle_ChatGptRunning_TurnOff_CallsCoreDirectlyWithoutConfirm()
    {
        var h = new Harness();
        h.ForwardingOn();
        h.ChatGpt.Running = true;

        await h.Model.ToggleForwardingAsync();

        Assert.AreEqual(0, h.Confirmer.Requests.Count);
        CollectionAssert.AreEqual(new[] { "call:set_forwarding" }, h.Log);
        Assert.AreEqual(false, h.Core.ParamsOf("set_forwarding")["enabled"]);
        Assert.IsFalse(h.Model.ForwardingEnabled);
    }

    [TestMethod]
    public async Task Toggle_ChatGptRunning_TurnOn_SuccessNoticeRestartToTakeEffect()
    {
        var h = new Harness();
        h.ChatGpt.Running = true;

        await h.Model.ToggleForwardingAsync();

        var notice = h.Model.Notices.Last();
        Assert.AreEqual("已打开转发", notice.Title);
        Assert.AreEqual(RestartHint, notice.Detail);
        Assert.AreEqual(AppSeverity.Success, notice.Severity);
    }

    [TestMethod]
    public async Task Toggle_ChatGptRunning_TurnOff_SuccessNoticeRestartToTakeEffect()
    {
        var h = new Harness();
        h.ForwardingOn();
        h.ChatGpt.Running = true;

        await h.Model.ToggleForwardingAsync();

        var notice = h.Model.Notices.Last();
        Assert.AreEqual("已关闭转发", notice.Title);
        Assert.AreEqual(RestartHint, notice.Detail);
        Assert.AreEqual(AppSeverity.Success, notice.Severity);
    }

    [TestMethod]
    public async Task Toggle_ChatGptNotRunning_SwitchesDirectlyWithoutNotice()
    {
        foreach (var startOn in new[] { false, true })
        {
            var h = new Harness();
            if (startOn) h.ForwardingOn();
            h.ChatGpt.Running = false;

            await h.Model.ToggleForwardingAsync();

            Assert.AreEqual(0, h.Confirmer.Requests.Count, "startOn=" + startOn);
            Assert.AreEqual(1, h.Core.Count("set_forwarding"), "startOn=" + startOn);
            Assert.AreEqual(!startOn, h.Model.ForwardingEnabled, "startOn=" + startOn);
            Assert.AreEqual(0, h.Model.Notices.Count, "startOn=" + startOn);
        }
    }

    [TestMethod]
    public async Task Toggle_ChatGptRunning_CoreFails_NoRestartHintNotice()
    {
        var h = new Harness();
        h.ChatGpt.Running = true;
        h.Core.Fail("set_forwarding", "config_invalid");

        await h.Model.ToggleForwardingAsync();

        Assert.IsFalse(h.Model.Notices.Any(n => n.Detail == RestartHint));
        Assert.IsFalse(h.Model.ForwardingEnabled);
    }

    [TestMethod]
    public async Task Toggle_WhileInFlight_SecondCallIgnored()
    {
        var h = new Harness();
        h.ChatGpt.Running = true;
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("set_forwarding", _ => gate.Task);

        var first = h.Model.ToggleForwardingAsync();
        await h.Model.ToggleForwardingAsync();

        Assert.AreEqual(1, h.Core.Count("set_forwarding"));
        Assert.IsTrue(h.Model.IsBusy);
        gate.SetResult(Json.Status(s => s["forwarding"] = true));
        await first;
        Assert.IsFalse(h.Model.IsBusy);
    }

    [TestMethod]
    public async Task Toggle_RestoreBlocked_RecordsCodeAndKeys_AsksToShowMainWindow()
    {
        var h = new Harness();
        h.ForwardingOn();
        h.Core.Fail("set_forwarding", "restore_blocked", "model_provider", "openai_base_url");

        await h.Model.ToggleForwardingAsync();

        Assert.AreEqual("restore_blocked", h.Model.LastError?.Code);
        CollectionAssert.AreEqual(new[] { "model_provider", "openai_base_url" }, h.Model.LastError?.Keys.ToArray());
        Assert.AreEqual(1, h.ShowMainCount);
    }

    [TestMethod]
    public async Task ToggleEnabled_Default_True()
    {
        var h = new Harness();
        await h.Model.StartAsync();
        h.Push();
        Assert.IsTrue(h.Model.ToggleEnabled);
    }

    [TestMethod]
    public void ToggleEnabled_ChatGptNotFound_False()
    {
        var h = new Harness();
        h.Push(s => s["chatgpt"] = new JsonObject { ["found"] = false, ["error"] = null });
        Assert.IsFalse(h.Model.ToggleEnabled);
    }

    [TestMethod]
    public async Task ToggleEnabled_WhileToggling_FalseThenTrueAgain()
    {
        var h = new Harness();
        await h.Model.StartAsync();
        h.Push();
        var duringCall = true;
        h.Core.On("set_forwarding", _ =>
        {
            duringCall = h.Model.ToggleEnabled;
            return Json.Status(s => s["forwarding"] = true);
        });

        await h.Model.ToggleForwardingAsync();

        Assert.IsFalse(duringCall);
        Assert.IsTrue(h.Model.ToggleEnabled);
    }

    #endregion

    #region 登录时启动

    [TestMethod]
    public async Task LoginStart_UncheckWhileForwardingOn_AsksWithExactText()
    {
        var h = new Harness();
        h.ForwardingOn();
        h.Confirmer.Answer = _ => false;

        await h.Model.SetLoginStartAsync(false);

        var ask = h.Confirmer.Requests.Single();
        Assert.AreEqual("关闭「登录时启动」？", ask.Title);
        Assert.AreEqual(
            "关闭后，下次登录电脑时助手不会自动运行。在你手动打开助手之前，Codex 里的官方模型和中转模型都无法使用，因为转发仍然开着。如果以后不打算常驻，建议先关闭转发。",
            ask.Message);
    }

    [TestMethod]
    public async Task LoginStart_UncheckWhileForwardingOn_CancelKeepsEnabled()
    {
        var h = new Harness();
        h.Login.Enabled = true;
        await h.Model.StartAsync();
        h.ForwardingOn();
        h.Confirmer.Answer = _ => false;

        await h.Model.SetLoginStartAsync(false);

        Assert.AreEqual(0, h.Login.DisableCount);
        Assert.IsTrue(h.Model.LoginStartEnabled);
    }

    [TestMethod]
    public async Task LoginStart_UncheckWhileForwardingOn_ConfirmDisables()
    {
        var h = new Harness();
        h.Login.Enabled = true;
        await h.Model.StartAsync();
        h.ForwardingOn();

        await h.Model.SetLoginStartAsync(false);

        Assert.AreEqual(1, h.Login.DisableCount);
        Assert.IsFalse(h.Model.LoginStartEnabled);
    }

    [TestMethod]
    public async Task LoginStart_UncheckWhileForwardingOff_DisablesWithoutAsking()
    {
        var h = new Harness();
        await h.Model.SetLoginStartAsync(false);

        Assert.AreEqual(0, h.Confirmer.Requests.Count);
        Assert.AreEqual(1, h.Login.DisableCount);
    }

    [TestMethod]
    public async Task LoginStart_CheckWhileForwardingOn_EnablesWithoutAsking()
    {
        var h = new Harness();
        h.ForwardingOn();

        await h.Model.SetLoginStartAsync(true);

        Assert.AreEqual(0, h.Confirmer.Requests.Count);
        Assert.AreEqual(1, h.Login.EnableCount);
    }

    #endregion

    #region 退出

    const string QuitTitle = "退出模型接入助手？";
    const string QuitBase = "退出前会关闭转发，并把 Codex 配置还原成开启前的样子。";
    const string QuitRunningTail = "ChatGPT 正在运行，请在退出后重启 ChatGPT。";

    [TestMethod]
    public async Task Quit_ForwardingOnAndChatGptRunning_AsksWithFullMessage()
    {
        var h = new Harness();
        h.ForwardingOn();
        h.ChatGpt.Running = true;
        h.Confirmer.Answer = _ => false;

        await h.Model.QuitAsync();

        var ask = h.Confirmer.Requests.Single();
        Assert.AreEqual(QuitTitle, ask.Title);
        Assert.AreEqual(QuitBase + QuitRunningTail, ask.Message);
    }

    [TestMethod]
    public async Task Quit_ForwardingOnAndChatGptNotRunning_MessageWithoutSecondHalf()
    {
        var h = new Harness();
        h.ForwardingOn();
        h.ChatGpt.Running = false;
        h.Confirmer.Answer = _ => false;

        await h.Model.QuitAsync();

        Assert.AreEqual(QuitBase, h.Confirmer.Requests.Single().Message);
    }

    [TestMethod]
    public async Task Quit_Cancel_NoShutdownNoExit()
    {
        var h = new Harness();
        h.ForwardingOn();
        h.Confirmer.Answer = _ => false;

        await h.Model.QuitAsync();

        Assert.AreEqual(0, h.Core.Count("shutdown"));
        Assert.AreEqual(0, h.ExitCount);
    }

    [TestMethod]
    public async Task Quit_Confirm_ShutdownWithRestoreThenExit()
    {
        var h = new Harness();
        h.ForwardingOn();

        await h.Model.QuitAsync();

        Assert.AreEqual(true, h.Core.ParamsOf("shutdown")["restore"]);
        Assert.AreEqual(1, h.ExitCount);
        Assert.IsFalse(h.Model.CoreRunning);
    }

    [TestMethod]
    public async Task Quit_RestoreBlocked_NoExitRecordsKeysShowsWindowCoreStopped()
    {
        var h = new Harness();
        await h.Model.StartAsync();
        h.ForwardingOn();
        h.Core.Fail("shutdown", "restore_blocked", "model_provider");

        await h.Model.QuitAsync();

        Assert.AreEqual(0, h.ExitCount);
        CollectionAssert.AreEqual(new[] { "model_provider" }, h.Model.LastError?.Keys.ToArray());
        Assert.AreEqual(1, h.ShowMainCount);
        Assert.IsFalse(h.Model.CoreRunning);
    }

    [TestMethod]
    public async Task Quit_ForwardingOff_ShutdownWithoutRestoreAndNoAsk()
    {
        var h = new Harness();
        await h.Model.StartAsync();

        await h.Model.QuitAsync();

        Assert.AreEqual(0, h.Confirmer.Requests.Count);
        Assert.AreEqual(false, h.Core.ParamsOf("shutdown")["restore"]);
        Assert.AreEqual(1, h.ExitCount);
        Assert.IsFalse(h.Model.CoreRunning);
    }

    #endregion

    #region 卸载

    [TestMethod]
    public async Task Uninstall_Cancel_NothingHappens()
    {
        var h = new Harness();
        h.Confirmer.Answer = _ => false;

        await h.Model.UninstallAsync();

        Assert.AreEqual(0, h.Core.Count("prepare_uninstall"));
        Assert.AreEqual(0, h.Vault.DeleteAllCount);
        Assert.AreEqual(0, h.Settings.DeleteCount);
        Assert.AreEqual(0, h.Login.DisableCount);
    }

    [TestMethod]
    public async Task Uninstall_RestoreBlocked_PausesWithKeysAndConfigPath_ExitCode2_NothingCleaned()
    {
        var h = new Harness();
        h.Push(s => s["config_path"] = "/home/example/.codex/config.toml");
        h.Core.Fail("prepare_uninstall", "restore_blocked", "model_provider", "openai_base_url");

        var code = await h.Model.UninstallAsync();

        Assert.AreEqual(2, code);
        CollectionAssert.AreEqual(
            new[] { "model_provider", "openai_base_url" }, h.Model.UninstallPause?.Keys.ToArray());
        Assert.AreEqual("/home/example/.codex/config.toml", h.Model.UninstallPause?.ConfigPath);
        Assert.AreEqual(0, h.Vault.DeleteAllCount);
        Assert.AreEqual(0, h.Settings.DeleteCount);
        Assert.AreEqual(0, h.Login.DisableCount);
        Assert.IsNull(h.Model.Uninstalled);
    }

    [TestMethod]
    public async Task Uninstall_Success_CleansEverythingInOrder_ExitCode0()
    {
        var h = new Harness();
        h.Seed("1", "中转", "https://relay.example.com", "sk-example-0001");
        h.Seed("2", "A家", "https://relay2.example.com", "sk-example-0002");

        var code = await h.Model.UninstallAsync();

        Assert.AreEqual(0, code);
        Assert.AreEqual(1, h.Vault.DeleteAllCount);
        Assert.AreEqual(0, h.Vault.Keys.Count);
        Assert.AreEqual(1, h.Settings.DeleteCount);
        Assert.AreEqual(1, h.Login.DisableCount);
        Assert.IsNotNull(h.Model.Uninstalled);
        Assert.AreEqual(0, h.Model.Uninstalled.Leftovers.Count);
        Assert.IsNull(h.Model.UninstallPause);
    }

    [TestMethod]
    public async Task Uninstall_CredentialDeleteFails_RecordedAndOthersStillCleaned_ExitCode0()
    {
        var h = new Harness();
        h.Seed("1", "中转", "https://relay.example.com", "sk-example-0001");
        h.Vault.DeleteException = new IOException();

        var code = await h.Model.UninstallAsync();

        Assert.AreEqual(0, code);
        Assert.AreEqual(1, h.Model.Uninstalled?.Leftovers.Count);
        StringAssert.Contains(h.Model.Uninstalled!.Leftovers[0], "凭据");
        Assert.AreEqual(1, h.Settings.DeleteCount);
        Assert.AreEqual(1, h.Login.DisableCount);
    }

    [TestMethod]
    public async Task Uninstall_SettingsDeleteFails_RecordedAndOthersStillCleaned()
    {
        var h = new Harness();
        h.Settings.DeleteException = new IOException();

        await h.Model.UninstallAsync();

        Assert.AreEqual(1, h.Model.Uninstalled?.Leftovers.Count);
        StringAssert.Contains(h.Model.Uninstalled!.Leftovers[0], "设置");
        Assert.AreEqual(1, h.Login.DisableCount);
    }

    [TestMethod]
    public async Task Uninstall_LoginDisableFails_Recorded()
    {
        var h = new Harness();
        h.Login.DisableException = new UnauthorizedAccessException();

        await h.Model.UninstallAsync();

        Assert.AreEqual(1, h.Model.Uninstalled?.Leftovers.Count);
        StringAssert.Contains(h.Model.Uninstalled!.Leftovers[0], "登录");
    }

    [TestMethod]
    public async Task Uninstall_ChatGptRunning_OnlyTheUninstallAsk_RecordsRunningInOutcome()
    {
        var h = new Harness();
        h.ChatGpt.Running = true;

        await h.Model.UninstallAsync();

        Assert.AreEqual(1, h.Confirmer.Requests.Count);
        Assert.IsTrue(h.Model.Uninstalled!.ChatGptRunning);
    }

    [TestMethod]
    public async Task Uninstall_ChatGptNotRunning_OnlyOneAsk_OutcomeNotRunning()
    {
        var h = new Harness();
        h.ChatGpt.Running = false;

        await h.Model.UninstallAsync();

        Assert.AreEqual(1, h.Confirmer.Requests.Count);
        Assert.IsFalse(h.Model.Uninstalled!.ChatGptRunning);
    }

    #endregion

    #region 检查更新

    [TestMethod]
    public async Task CheckUpdate_Newer_RecordsVersionAndUrl()
    {
        var h = new Harness();
        h.Core.On("check_update", _ => Json.Parse(
            """{"latest_version":"1.3.0","url":"https://relay.example.com/download","is_newer":true}"""));

        await h.Model.CheckUpdateAsync();

        Assert.AreEqual(true, h.Core.ParamsOf("check_update")["manual"]);
        Assert.AreEqual("1.3.0", h.Model.UpdateVersion);
        Assert.AreEqual("https://relay.example.com/download", h.Model.UpdateUrl);
    }

    [TestMethod]
    public async Task CheckUpdate_AlreadyLatest_NoticeAndNoUpdateRecorded()
    {
        var h = new Harness();

        await h.Model.CheckUpdateAsync();

        Assert.AreEqual(AppSeverity.Success, Find(h, UpToDate)?.Severity);
        Assert.IsNull(h.Model.UpdateVersion);
        Assert.IsNull(h.Model.UpdateUrl);
    }

    [TestMethod]
    public async Task CheckUpdate_Unavailable_Notice()
    {
        var h = new Harness();
        h.Core.Fail("check_update", "update_unavailable");

        await h.Model.CheckUpdateAsync();

        Assert.AreEqual(AppSeverity.Warning, Find(h, UpdateUnavailable)?.Severity);
    }

    #endregion

    #region 顶部提示条

    static bool Has(Harness h, string title) => h.Model.Banners.Any(b => b.Title == title);

    [TestMethod]
    public void Banners_HealthyStatus_Empty()
    {
        var h = new Harness();
        h.Push();
        Assert.AreEqual(0, h.Model.Banners.Count);
    }

    [TestMethod]
    public void Banners_TakenOver()
    {
        var h = new Harness();
        h.Push(s => s["taken_over"] = new JsonArray("openai_base_url"));
        Assert.IsTrue(Has(h, "已接管 Codex 原有的接入设置"));
    }

    [TestMethod]
    public void Banners_RestoreBlocked_ShortDetailAndOnlyShowDetailAction()
    {
        var h = new Harness();
        h.Push(s =>
        {
            s["restore_blocked"] = new JsonArray("model_provider", "openai_base_url");
            s["config_path"] = "/home/example/.codex/config.toml";
        });

        var banner = h.Model.Banners.Single(b => b.Title == "没能自动还原 Codex 配置");
        Assert.AreEqual("有设置项被手动改过。", banner.Detail);
        CollectionAssert.AreEqual(new[] { BannerAction.ShowDetail }, banner.Actions.ToArray());
        CollectionAssert.AreEqual(new[] { "model_provider", "openai_base_url" }, banner.Keys.ToArray());
        Assert.AreEqual("/home/example/.codex/config.toml", banner.ConfigPath);
        Assert.AreEqual("restore_blocked", banner.Code);
    }

    [TestMethod]
    public async Task Banners_ConfigInvalidWithKeys_OffersShowDetail()
    {
        var h = new Harness();
        h.Push(s => s["config_path"] = "/home/example/.codex/config.toml");
        h.Core.Fail("set_forwarding", "config_invalid", "model_provider");

        await h.Model.ToggleForwardingAsync();

        var banner = h.Model.Banners.Single(b => b.Keys.Count > 0);
        CollectionAssert.AreEqual(new[] { "model_provider" }, banner.Keys.ToArray());
        CollectionAssert.Contains(banner.Actions.ToList(), BannerAction.ShowDetail);
        Assert.AreEqual("/home/example/.codex/config.toml", banner.ConfigPath);
        Assert.AreEqual("config_invalid", banner.Code);
    }

    [TestMethod]
    public void Banners_ProxyError()
    {
        var h = new Harness();
        h.Push(s => s["proxy"] = new JsonObject { ["running"] = false, ["error"] = "proxy_port_in_use" });
        Assert.IsTrue(Has(h, "本机转发服务没能启动"));
    }

    [TestMethod]
    public void Banners_ChatGptNotFound()
    {
        var h = new Harness();
        h.Push(s => s["chatgpt"] = new JsonObject { ["found"] = false, ["error"] = null });
        Assert.IsTrue(Has(h, "没有找到 ChatGPT"));
    }

    [TestMethod]
    public void Banners_FollowLatestStatus_ClearedWhenProblemGone()
    {
        var h = new Harness();
        h.Push(s => s["taken_over"] = new JsonArray("openai_base_url"));
        Assert.AreEqual(1, h.Model.Banners.Count);
        h.Push();
        Assert.AreEqual(0, h.Model.Banners.Count);
    }

    #endregion

    #region 核心运行状态

    [TestMethod]
    public async Task CoreRunning_StartSucceeds_TrueWithRunningText()
    {
        var h = new Harness();
        await h.Model.StartAsync();

        Assert.IsTrue(h.Model.CoreRunning);
        Assert.AreEqual("后台运行中", h.Model.CoreStatusText);
    }

    [TestMethod]
    public async Task CoreRunning_StartFails_FalseWithStoppedText()
    {
        var h = new Harness();
        h.Core.StartException = new CoreProcessExitedException();

        await h.Model.StartAsync();

        Assert.IsFalse(h.Model.CoreRunning);
        Assert.AreEqual("后台服务已停止", h.Model.CoreStatusText);
    }

    [TestMethod]
    public async Task TrayStatusText_CoreStopped_IsStoppedOnly()
    {
        var h = new Harness();
        await h.Model.StartAsync();

        h.Core.RaiseFailed("核心进程反复崩溃");

        Assert.AreEqual("后台服务已停止", h.Model.TrayStatusText);
    }

    [TestMethod]
    public async Task TrayStatusText_CoreRunning_ForwardingOffAndOn()
    {
        var h = new Harness();
        await h.Model.StartAsync();
        Assert.AreEqual("转发已关闭 · 后台运行中", h.Model.TrayStatusText);

        h.Push(s => s["forwarding"] = true);

        Assert.AreEqual("转发已开启 · 后台运行中", h.Model.TrayStatusText);
    }

    [TestMethod]
    public async Task CoreRunning_FailedEvent_FalseWithNotice()
    {
        var h = new Harness();
        await h.Model.StartAsync();

        h.Core.RaiseFailed("核心进程反复崩溃");

        Assert.IsFalse(h.Model.CoreRunning);
        Assert.AreEqual("后台服务已停止", h.Model.CoreStatusText);
        StringAssert.Contains(AllText(h), CoreDown);
    }

    [TestMethod]
    public async Task Start_PassesStoredUrlKeyAndChatGptDirToCore()
    {
        var h = new Harness();
        h.Settings.BaseUrl = OldUrl;
        h.Vault.Stored = OldKey;
        h.ChatGpt.LocatedDir = @"C:\Apps\ChatGPT";

        await h.Model.StartAsync();

        CollectionAssert.AreEqual(new[] { new RelayInit("1", "中转", OldUrl, OldKey) }, h.Core.StartedWith!.Relays.ToArray());
        Assert.AreEqual(@"C:\Apps\ChatGPT", h.Core.StartedWith.ChatGptDir);
    }

    #endregion

    #region 凭据读取失败与单实例

    [TestMethod]
    public async Task Start_CredentialReadFails_NoticeAndAsksToShowMainWindow()
    {
        var h = new Harness();
        h.Settings.BaseUrl = OldUrl;
        h.Vault.ReadException = new System.ComponentModel.Win32Exception(5);

        await h.Model.StartAsync();

        StringAssert.Contains(AllText(h), "无法读取已保存的 API Key，请重新输入");
        Assert.AreEqual(1, h.ShowMainCount);
    }

    [TestMethod]
    public async Task Start_CredentialReadOk_NoWindowRequested()
    {
        var h = new Harness();
        h.Vault.Stored = OldKey;

        await h.Model.StartAsync();

        Assert.AreEqual(0, h.ShowMainCount);
    }

    [TestMethod]
    public void ActivationFailedMessage_Text()
    {
        Assert.AreEqual(
            "模型接入助手已在运行，但无法切换到它，请从任务栏通知区域打开",
            AppModel.ActivationFailedMessage);
    }

    #endregion

    #region 复核补充

    [TestMethod]
    public async Task Setup_SettingsWriteFails_DeletesKey()
    {
        var h = new Harness();
        h.FillInputs();
        h.Settings.SaveException = new IOException();

        await h.Model.CompleteSetupAsync();

        Assert.AreEqual(1, h.Vault.DeleteCount);
        Assert.IsNull(h.Vault.Stored);
        Assert.IsNotNull(Find(h, NoSave));
        Assert.AreEqual(AppPage.Setup, h.Model.Page);
    }

    [TestMethod]
    public async Task Setup_CredentialWriteFails_SettingsNotWritten()
    {
        var h = new Harness();
        h.FillInputs();
        h.Vault.SaveException = new IOException();

        await h.Model.CompleteSetupAsync();

        Assert.AreEqual(0, h.Settings.Relays.Count);

    }

    [TestMethod]
    public async Task ToggleEnabled_CoreNotRunning_False()
    {
        var h = new Harness();
        h.Core.StartException = new CoreProcessExitedException();
        await h.Model.StartAsync();

        Assert.IsFalse(h.Model.ToggleEnabled);
    }

    [TestMethod]
    public async Task ToggleEnabled_CoreFailsLater_FalseAndNotifies()
    {
        var h = new Harness();
        await h.Model.StartAsync();
        Assert.IsTrue(h.Model.ToggleEnabled);
        var changed = new List<string?>();
        h.Model.PropertyChanged += (_, e) => changed.Add(e.PropertyName);

        h.Core.RaiseFailed("核心进程反复崩溃");

        Assert.IsFalse(h.Model.ToggleEnabled);
        CollectionAssert.Contains(changed, "ToggleEnabled");
    }

    [TestMethod]
    public async Task Quit_RestoreBlocked_ShowsBannerWithKeysAndConfigPath()
    {
        var h = new Harness();
        h.Push(s =>
        {
            s["forwarding"] = true;
            s["config_path"] = "/home/example/.codex/config.toml";
        });
        h.Core.Fail("shutdown", "restore_blocked", "model_provider", "openai_base_url");

        await h.Model.QuitAsync();

        var banner = h.Model.Banners.Single(b => b.Title == "没能自动还原 Codex 配置");
        CollectionAssert.AreEqual(new[] { "model_provider", "openai_base_url" }, banner.Keys.ToArray());
        Assert.AreEqual("/home/example/.codex/config.toml", banner.ConfigPath);
        Assert.AreEqual("restore_blocked", banner.Code);
        CollectionAssert.AreEqual(new[] { BannerAction.ShowDetail }, banner.Actions.ToArray());
    }

    [TestMethod]
    public async Task Quit_OtherError_NoticeWithReason_NoExit()
    {
        var h = new Harness();
        h.ForwardingOn();
        h.Core.Fail("shutdown", "internal_error");

        await h.Model.QuitAsync();

        Assert.AreEqual("没能退出", Find(h, "没能退出")?.Title);
        Assert.AreEqual(Msg("internal_error"), Find(h, "没能退出")?.Detail);
        Assert.AreEqual(0, h.ExitCount);
        Assert.AreEqual(1, h.ShowMainCount);
    }

    [TestMethod]
    public async Task Uninstall_ShowsProgressWhilePreparing_ThenClears()
    {
        var h = new Harness();
        string? progress = null;
        h.Core.On("prepare_uninstall", _ =>
        {
            progress = h.Model.ProgressText;
            return Json.Parse("{}");
        });

        await h.Model.UninstallAsync();

        Assert.AreEqual("正在卸载", progress);
        Assert.IsNull(h.Model.ProgressText);
    }

    [TestMethod]
    public async Task StopCore_StopsCoreAndMarksNotRunning()
    {
        var h = new Harness();
        await h.Model.StartAsync();

        await h.Model.StopCoreAsync();

        Assert.AreEqual(1, h.Core.StopCount);
        Assert.IsFalse(h.Model.CoreRunning);
    }

    [TestMethod]
    public async Task Quit_ConfirmPending_SecondCallReturnsWithoutAsking()
    {
        var h = new Harness();
        h.ForwardingOn();
        h.Confirmer.Gate = new TaskCompletionSource<bool>();

        var first = h.Model.QuitAsync();
        await h.Model.QuitAsync();

        Assert.AreEqual(1, h.Confirmer.Requests.Count);
        h.Confirmer.Gate.SetResult(false);
        await first;
    }

    [TestMethod]
    public async Task Uninstall_ConfirmPending_SecondCallReturns1WithoutAsking()
    {
        var h = new Harness();
        h.Confirmer.Gate = new TaskCompletionSource<bool>();

        var first = h.Model.UninstallAsync();
        var second = await h.Model.UninstallAsync();

        Assert.AreEqual(1, second);
        Assert.AreEqual(1, h.Confirmer.Requests.Count);
        h.Confirmer.Gate.SetResult(false);
        await first;
    }

    [TestMethod]
    public void IsAlreadyCleaned_NothingLeft_True()
    {
        var h = new Harness();
        Assert.IsTrue(h.Model.IsAlreadyCleaned());
    }

    [TestMethod]
    public void IsAlreadyCleaned_AnythingLeft_False()
    {
        var withUrl = new Harness();
        withUrl.Settings.BaseUrl = OldUrl;
        var withLogin = new Harness();
        withLogin.Login.Enabled = true;
        var withKey = new Harness();
        withKey.Vault.Stored = OldKey;
        var unreadable = new Harness();
        unreadable.Settings.ReadException = new IOException();

        Assert.IsFalse(withUrl.Model.IsAlreadyCleaned());
        Assert.IsFalse(withKey.Model.IsAlreadyCleaned());
        Assert.IsFalse(withLogin.Model.IsAlreadyCleaned());
        Assert.IsFalse(unreadable.Model.IsAlreadyCleaned());
    }

    [TestMethod]
    public async Task Start_SettingsReadFails_TreatedAsNoSettings()
    {
        foreach (Exception ex in new Exception[] { new IOException(), new UnauthorizedAccessException() })
        {
            var h = new Harness();
            h.Settings.ReadException = ex;

            await h.Model.StartAsync();

            Assert.AreEqual(AppPage.Setup, h.Model.Page, ex.GetType().Name);
            Assert.AreEqual(0, h.Core.StartedWith!.Relays.Count, ex.GetType().Name);
        }
    }

    [TestMethod]
    public async Task Start_CredentialReadFails_PageIsSetBeforeWindowRequested()
    {
        var h = new Harness();
        h.Settings.BaseUrl = OldUrl;
        h.Vault.ReadException = new System.ComponentModel.Win32Exception(5);
        AppPage? pageAtRequest = null;
        h.Model.ShowMainWindowRequested += () => pageAtRequest = h.Model.Page;

        await h.Model.StartAsync();

        Assert.AreEqual(AppPage.Main, pageAtRequest);
    }

    #endregion

    #region 并存的提示

    [TestMethod]
    public void NoticeAutoDismissDelay_DefaultsToFourSeconds()
    {
        Assert.AreEqual(TimeSpan.FromSeconds(4), new Harness().Model.NoticeAutoDismissDelay);
    }

    [TestMethod]
    public async Task Notices_NewOnesAppendedAtEnd_EachWithOwnId()
    {
        var h = new Harness();
        h.Core.RaiseFailed("核心进程反复崩溃");
        await h.Model.CheckUpdateAsync();

        CollectionAssert.AreEqual(new[] { CoreDown, UpToDate }, Titles(h));
        Assert.AreNotEqual(h.Model.Notices[0].Id, h.Model.Notices[1].Id);
    }

    [TestMethod]
    public async Task Notices_SameTitleAndSeverityAgain_NotAppendedTwice()
    {
        var h = new Harness();
        h.Core.Fail("check_update", "update_unavailable");

        await h.Model.CheckUpdateAsync();
        await h.Model.CheckUpdateAsync();

        CollectionAssert.AreEqual(new[] { UpdateUnavailable }, Titles(h));
    }

    [TestMethod]
    public async Task Notices_CheckUpdateFailureWarning_RemovedByLaterSuccess()
    {
        var h = new Harness();
        h.Core.Fail("check_update", "update_unavailable");
        await h.Model.CheckUpdateAsync();
        Assert.IsNotNull(Find(h, UpdateUnavailable));

        h.Core.On("check_update", _ => Json.Parse("""{"latest_version":null,"url":null,"is_newer":false}"""));
        await h.Model.CheckUpdateAsync();

        CollectionAssert.AreEqual(new[] { UpToDate }, Titles(h));
    }

    [TestMethod]
    public async Task Notices_CheckUpdateFailureWarning_RemovedByLaterNewVersionFound()
    {
        var h = new Harness();
        h.Core.Fail("check_update", "update_unavailable");
        await h.Model.CheckUpdateAsync();

        h.Core.On("check_update", _ => Json.Parse(
            """{"latest_version":"1.3.0","url":"https://relay.example.com/download","is_newer":true}"""));
        await h.Model.CheckUpdateAsync();

        Assert.IsNull(Find(h, UpdateUnavailable));
    }

    [TestMethod]
    public async Task DismissNotice_RemovesOnlyThatNotice()
    {
        var h = new Harness();
        h.Core.RaiseFailed("核心进程反复崩溃");
        await h.Model.CheckUpdateAsync();
        var success = Find(h, UpToDate)!;

        h.Model.DismissNotice(success.Id);

        CollectionAssert.AreEqual(new[] { CoreDown }, Titles(h));
    }

    [TestMethod]
    public async Task Notices_AddAndDismiss_RaisePropertyChanged()
    {
        var h = new Harness();
        var changed = new List<string?>();
        h.Model.PropertyChanged += (_, e) => changed.Add(e.PropertyName);

        await h.Model.CheckUpdateAsync();
        var afterAdd = changed.Count(n => n == "Notices");
        Assert.IsTrue(afterAdd >= 1);

        h.Model.DismissNotice(h.Model.Notices[0].Id);
        Assert.IsTrue(changed.Count(n => n == "Notices") > afterAdd);
    }

    [TestMethod]
    public async Task Notice_Success_AutoDismissesAfterDelay()
    {
        var h = new Harness();
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMilliseconds(100);

        await h.Model.CheckUpdateAsync();
        Assert.AreEqual(AppSeverity.Success, h.Model.Notices.Last().Severity);

        await WaitUntilAsync(() => h.Model.Notices.Count == 0);
        Assert.AreEqual(0, h.Model.Notices.Count);
    }

    [TestMethod]
    public async Task Notice_Success_StaysUntilDelayElapses()
    {
        var h = new Harness();
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMilliseconds(800);

        await h.Model.CheckUpdateAsync();
        await Task.Delay(200);

        CollectionAssert.AreEqual(new[] { UpToDate }, Titles(h));
    }

    [TestMethod]
    public async Task Notice_ErrorAndWarning_NeverAutoDismiss()
    {
        var h = new Harness();
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMilliseconds(50);
        h.Core.Fail("check_update", "update_unavailable");

        h.Core.RaiseFailed("核心进程反复崩溃");
        await h.Model.CheckUpdateAsync();
        Assert.AreEqual(AppSeverity.Danger, Find(h, CoreDown)!.Severity);
        Assert.AreEqual(AppSeverity.Warning, Find(h, UpdateUnavailable)!.Severity);
        await Task.Delay(500);

        CollectionAssert.AreEqual(new[] { CoreDown, UpdateUnavailable }, Titles(h));
    }

    [TestMethod]
    public async Task Notice_SuccessAfterError_BothShownThenOnlyErrorRemains()
    {
        var h = new Harness();
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMilliseconds(200);
        h.Core.RaiseFailed("核心进程反复崩溃");

        await h.Model.CheckUpdateAsync();
        CollectionAssert.AreEqual(new[] { CoreDown, UpToDate }, Titles(h));

        await WaitUntilAsync(() => h.Model.Notices.Count == 1);
        await Task.Delay(300);
        CollectionAssert.AreEqual(new[] { CoreDown }, Titles(h));
    }

    [TestMethod]
    public async Task Notice_EachSuccessDismissesOnItsOwnTimer()
    {
        var h = new Harness();
        h.Seed("1", "中转", "https://relay.example.com", "sk-example-0001");
        h.Seed("2", "A家", "https://relay2.example.com", "sk-example-0002");
        await h.Model.StartAsync();
        h.Core.On("delete_relay", _ => Json.Status());
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMilliseconds(600);

        await h.Model.CheckUpdateAsync();
        await Task.Delay(400);
        await h.Model.PerformDeleteRelayAsync("2");
        CollectionAssert.AreEqual(new[] { UpToDate, "已删除「A家」" }, Titles(h));

        await Task.Delay(400);
        CollectionAssert.AreEqual(new[] { "已删除「A家」" }, Titles(h));
        await WaitUntilAsync(() => h.Model.Notices.Count == 0);
        Assert.AreEqual(0, h.Model.Notices.Count);
    }

    [TestMethod]
    public async Task Notice_DismissedByUserBeforeTimer_TimerDoesNotRemoveLaterNotice()
    {
        var h = new Harness();
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMilliseconds(400);
        await h.Model.CheckUpdateAsync();
        h.Model.DismissNotice(h.Model.Notices[0].Id);
        h.Core.RaiseFailed("核心进程反复崩溃");

        await Task.Delay(700);

        CollectionAssert.AreEqual(new[] { CoreDown }, Titles(h));
    }

    #endregion

    #region 提示条可关闭

    static readonly (string Name, Action<JsonObject> Tweak)[] StatusProblems =
    [
        ("taken_over", s => s["taken_over"] = new JsonArray("openai_base_url")),
        ("restore_blocked", s => s["restore_blocked"] = new JsonArray("openai_base_url")),
        ("proxy_error", s => s["proxy"] = new JsonObject { ["running"] = false, ["error"] = "proxy_port_in_use" }),
        ("chatgpt_missing", s => s["chatgpt"] = new JsonObject { ["found"] = false, ["error"] = null }),
    ];

    [TestMethod]
    public void DismissBanner_EveryStatusBanner_CanBeHidden()
    {
        foreach (var (name, tweak) in StatusProblems)
        {
            var h = new Harness();
            h.Push(tweak);
            var banner = h.Model.Banners.First();

            h.Model.DismissBanner(banner);

            Assert.AreEqual(0, h.Model.Banners.Count, name);
        }
    }

    [TestMethod]
    public async Task DismissBanner_ForwardingFailureBanner_CanBeHidden()
    {
        var h = new Harness();
        h.Core.Fail("set_forwarding", "config_invalid", "model_provider");
        await h.Model.ToggleForwardingAsync();
        var banner = h.Model.Banners.Single(b => b.Keys.Count > 0);

        h.Model.DismissBanner(banner);

        Assert.AreEqual(0, h.Model.Banners.Count);
    }

    [TestMethod]
    public void DismissBanner_DismissesOnlyThatBanner()
    {
        var h = new Harness();
        h.Push(s =>
        {
            s["taken_over"] = new JsonArray("openai_base_url");
            s["proxy"] = new JsonObject { ["running"] = false, ["error"] = "proxy_port_in_use" };
        });

        h.Model.DismissBanner(h.Model.Banners.Single(b => b.Title == "已接管 Codex 原有的接入设置"));

        CollectionAssert.AreEqual(new[] { "本机转发服务没能启动" }, h.Model.Banners.Select(b => b.Title).ToArray());
    }

    [TestMethod]
    public void DismissBanner_SameStateAgain_StaysHidden_GoneThenBackShowsAgain()
    {
        var h = new Harness();
        Action<JsonObject> taken = s => s["taken_over"] = new JsonArray("openai_base_url");
        h.Push(taken);
        h.Model.DismissBanner(h.Model.Banners.Single());

        h.Push(taken);
        Assert.AreEqual(0, h.Model.Banners.Count);

        h.Push();
        Assert.AreEqual(0, h.Model.Banners.Count);

        h.Push(taken);
        CollectionAssert.AreEqual(
            new[] { "已接管 Codex 原有的接入设置" }, h.Model.Banners.Select(b => b.Title).ToArray());
    }

    [TestMethod]
    public void DismissBanner_RaisesBannersPropertyChanged()
    {
        var h = new Harness();
        h.Push(s => s["taken_over"] = new JsonArray("openai_base_url"));
        var changed = new List<string?>();
        h.Model.PropertyChanged += (_, e) => changed.Add(e.PropertyName);

        h.Model.DismissBanner(h.Model.Banners[0]);

        CollectionAssert.Contains(changed, "Banners");
    }

    #endregion

    #region 同类操作成功后移除旧失败提示

    [TestMethod]
    public async Task Notices_ToggleFailure_RemovedByLaterToggleSuccess()
    {
        var h = new Harness();
        h.Core.Fail("set_forwarding", "internal_error");
        await h.Model.ToggleForwardingAsync();
        Assert.IsNotNull(Find(h, "没能切换转发"));

        h.Core.On("set_forwarding", _ => Json.Status(s => s["forwarding"] = true));
        await h.Model.ToggleForwardingAsync();

        Assert.IsNull(Find(h, "没能切换转发"));
    }

    [TestMethod]
    public async Task Notices_RefreshFailure_RemovedByLaterRefreshSuccess()
    {
        foreach (var (method, run) in new (string, Func<AppModel, Task>)[]
                 {
                     ("refresh_models", m => m.RetryAsync()),
                     ("status", m => m.RecheckAsync()),
                 })
        {
            var h = new Harness();
            h.Core.Fail(method, "internal_error");
            await run(h.Model);
            Assert.IsNotNull(Find(h, "没能刷新"), method);

            h.Core.On(method, _ => Json.Status());
            await run(h.Model);

            Assert.IsNull(Find(h, "没能刷新"), method);
        }
    }

    [TestMethod]
    public async Task Notices_LoginStartFailure_RemovedByLaterSuccessInSameDirection()
    {
        var h = new Harness();
        h.Login.EnableException = new UnauthorizedAccessException();
        await h.Model.SetLoginStartAsync(true);
        Assert.IsNotNull(Find(h, "没能开启登录时启动"));
        h.Login.EnableException = null;
        await h.Model.SetLoginStartAsync(true);
        Assert.IsNull(Find(h, "没能开启登录时启动"));

        h.Login.DisableException = new UnauthorizedAccessException();
        await h.Model.SetLoginStartAsync(false);
        Assert.IsNotNull(Find(h, "没能关闭登录时启动"));
        h.Login.DisableException = null;
        await h.Model.SetLoginStartAsync(false);
        Assert.IsNull(Find(h, "没能关闭登录时启动"));
    }

    #endregion

    #region 托盘依据的新提示判定

    static HashSet<object> Ids(Harness h) => h.Model.Notices.Select(n => (object)n.Id).ToHashSet();

    static bool HasNewNotice(Harness h, HashSet<object> before) => h.Model.Notices.Any(n => !before.Contains(n.Id));

    [TestMethod]
    public async Task NewNotice_LoginStartFailsBeforeFirstAwait_ShowsAsNewId()
    {
        var h = new Harness();
        h.Login.EnableException = new UnauthorizedAccessException();
        var before = Ids(h);

        await h.Model.SetLoginStartAsync(true);

        Assert.IsTrue(HasNewNotice(h, before));
    }

    [TestMethod]
    public async Task NewNotice_CheckUpdateCoreGoneSynchronously_ShowsAsNewId()
    {
        var h = new Harness();
        h.Core.On("check_update", _ => throw new CoreProcessExitedException());
        var before = Ids(h);

        await h.Model.CheckUpdateAsync();

        Assert.IsTrue(HasNewNotice(h, before));
    }

    [TestMethod]
    public async Task NewNotice_UninstallCancelled_KeepsOldNoticeAndAddsNone()
    {
        var h = new Harness();
        h.Core.RaiseFailed("核心进程反复崩溃");
        var oldId = h.Model.Notices.Single().Id;
        var before = Ids(h);
        h.Confirmer.Answer = _ => false;

        var code = await h.Model.UninstallAsync();

        Assert.AreEqual(1, code);
        Assert.IsFalse(HasNewNotice(h, before));
        Assert.AreEqual(oldId, h.Model.Notices.Single().Id);
    }

    [TestMethod]
    public async Task NewNotice_UninstallFails_AddsNoticeWithNewIdAndKeepsOldOne()
    {
        var h = new Harness();
        h.Core.RaiseFailed("核心进程反复崩溃");
        var before = Ids(h);
        h.Core.Fail("prepare_uninstall", "internal_error");

        var code = await h.Model.UninstallAsync();

        Assert.AreEqual(1, code);
        Assert.IsTrue(HasNewNotice(h, before));
        Assert.IsNotNull(Find(h, "没能卸载"));
        Assert.IsNotNull(Find(h, CoreDown));
    }

    #endregion
}
