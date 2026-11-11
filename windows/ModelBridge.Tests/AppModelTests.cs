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
            "save_connection" => Json.Parse("""{"models_changed":false,"relay_model_count":6}"""),
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
            ["relay"] = new JsonObject
            {
                ["model_count"] = 6,
                ["fetched_at"] = "2026-11-11T00:12:00Z",
                ["stale"] = false,
                ["error"] = null,
            },
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
    public string? Stored { get; set; }
    public Exception? SaveException { get; set; }
    public Exception? ReadException { get; set; }
    public Exception? DeleteException { get; set; }
    public List<string> Saved { get; } = [];
    public int DeleteCount { get; private set; }

    public void Save(string key)
    {
        log.Add("vault:save");
        if (SaveException != null) throw SaveException;
        Stored = key;
        Saved.Add(key);
    }

    public string? Read() => ReadException != null ? throw ReadException : Stored;

    public void Delete()
    {
        DeleteCount++;
        if (DeleteException != null) throw DeleteException;
        Stored = null;
    }
}

sealed class FakeSettings(List<string> log) : IShellSettingsStore
{
    public string? BaseUrl { get; set; }
    public Exception? ReadException { get; set; }
    public Exception? SaveException { get; set; }
    public Exception? DeleteException { get; set; }
    public int DeleteCount { get; private set; }

    public string? ReadBaseUrl() => ReadException != null ? throw ReadException : BaseUrl;

    public void SaveBaseUrl(string baseUrl)
    {
        log.Add("settings:save");
        if (SaveException != null) throw SaveException;
        BaseUrl = baseUrl;
    }

    public void DeleteAll()
    {
        DeleteCount++;
        if (DeleteException != null) throw DeleteException;
        BaseUrl = null;
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

sealed class FakeChatGpt(List<string> log) : IChatGptControl
{
    public bool Running { get; set; }
    public string? LocatedDir { get; set; } = @"C:\Apps\ChatGPT";
    public ChatGptRestartResult Result { get; set; } = ChatGptRestartResult.Restarted;
    public Action? OnRestart { get; set; }
    public int RestartCount { get; private set; }

    public string? Locate() => LocatedDir;
    public bool IsRunning() => Running;

    public Task<ChatGptRestartResult> RestartAsync(CancellationToken ct)
    {
        RestartCount++;
        log.Add("restart");
        OnRestart?.Invoke();
        return Task.FromResult(Result);
    }
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
        ChatGpt = new FakeChatGpt(Log);
        Confirmer = new FakeConfirmer(Log);
        Model = new AppModel(Core, Vault, Settings, Login, ChatGpt, Confirmer);
        Model.ShowMainWindowRequested += () => ShowMainCount++;
        Model.ExitRequested += () => ExitCount++;
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

    static string Msg(string code) => CoreErrorMessages.Messages[code];

    #region 首次设置

    [TestMethod]
    public async Task Setup_EmptyFields_FieldErrorsAndCoreNotCalled()
    {
        var h = new Harness();
        await h.Model.CompleteSetupAsync();

        Assert.IsFalse(string.IsNullOrEmpty(h.Model.BaseUrlError));
        Assert.IsFalse(string.IsNullOrEmpty(h.Model.ApiKeyError));
        Assert.AreEqual(0, h.Core.Count("save_connection"));
    }

    [TestMethod]
    public async Task Setup_OnlyKeyMissing_OnlyKeyError()
    {
        var h = new Harness();
        h.FillInputs(apiKey: "");
        await h.Model.CompleteSetupAsync();

        Assert.IsNull(h.Model.BaseUrlError);
        Assert.IsFalse(string.IsNullOrEmpty(h.Model.ApiKeyError));
        Assert.AreEqual(0, h.Core.Count("save_connection"));
    }

    [TestMethod]
    public async Task Setup_OnlyUrlMissing_OnlyUrlError()
    {
        var h = new Harness();
        h.FillInputs(baseUrl: "");
        await h.Model.CompleteSetupAsync();

        Assert.IsFalse(string.IsNullOrEmpty(h.Model.BaseUrlError));
        Assert.IsNull(h.Model.ApiKeyError);
        Assert.AreEqual(0, h.Core.Count("save_connection"));
    }

    [TestMethod]
    public async Task Setup_NonHttpsUrl_FieldErrorAndCoreNotCalled()
    {
        var h = new Harness();
        h.FillInputs(baseUrl: "http://relay.example.com");
        await h.Model.CompleteSetupAsync();

        Assert.AreEqual("Base URL 要以 https:// 开头。", h.Model.BaseUrlError);
        Assert.AreEqual(0, h.Core.Count("save_connection"));
    }

    [TestMethod]
    public async Task Setup_MarksConnectingWhileCallingCore()
    {
        var h = new Harness();
        h.FillInputs();
        var connectingDuringCall = false;
        h.Core.On("save_connection", _ =>
        {
            connectingDuringCall = h.Model.IsConnecting;
            return Json.Parse("""{"models_changed":false,"relay_model_count":6}""");
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
        h.Core.OnAsync("save_connection", _ => gate.Task);

        var first = h.Model.CompleteSetupAsync();
        var second = h.Model.CompleteSetupAsync();
        gate.SetResult(Json.Parse("""{"models_changed":false,"relay_model_count":6}"""));
        await Task.WhenAll(first, second);

        Assert.AreEqual(1, h.Core.Count("save_connection"));
    }

    [TestMethod]
    public async Task Setup_SendsEnteredUrlAndKeyToCore()
    {
        var h = new Harness();
        h.FillInputs();
        await h.Model.CompleteSetupAsync();

        var p = h.Core.ParamsOf("save_connection");
        Assert.AreEqual("https://relay.example.com", p["base_url"]);
        Assert.AreEqual("sk-example-0000", p["api_key"]);
    }

    [TestMethod]
    public async Task Setup_CoreFails_TaskErrorWithCodeMessage_NoCredentialWrite_InputKept()
    {
        var h = new Harness();
        h.FillInputs();
        h.Core.Fail("save_connection", "relay_401");

        await h.Model.CompleteSetupAsync();

        Assert.AreEqual("没能连上中台", h.Model.TaskError?.Title);
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
        h.Core.Fail("save_connection", "relay_unreachable");

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

        Assert.AreEqual(NoSave, h.Model.Notice?.Title);
        Assert.AreEqual(CredentialWriteFailed, h.Model.Notice?.Detail);
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
            new[] { "call:save_connection", "vault:save", "settings:save", "call:set_forwarding", "login:enable" },
            h.Log);
        Assert.AreEqual(true, h.Core.ParamsOf("set_forwarding")["enabled"]);
        Assert.AreEqual(AppPage.Success, h.Model.Page);
        Assert.AreEqual("设置完成，转发已开启", h.Model.SuccessTitle);
        Assert.AreEqual("已获取 6 个", h.Model.ModelCountText);
        Assert.IsNull(h.Model.Notice);
    }

    [TestMethod]
    public async Task Setup_ForwardingFails_StillSuccessPageWithReasonNotice()
    {
        var h = new Harness();
        h.FillInputs();
        h.Core.Fail("set_forwarding", "config_invalid");

        await h.Model.CompleteSetupAsync();

        Assert.AreEqual(AppPage.Success, h.Model.Page);
        StringAssert.Contains(Text(h.Model.Notice), Msg("config_invalid"));
    }

    [TestMethod]
    public async Task Setup_LoginStartFails_StillSuccessPageWithReasonNotice()
    {
        var h = new Harness();
        h.FillInputs();
        h.Login.EnableException = new UnauthorizedAccessException();

        await h.Model.CompleteSetupAsync();

        Assert.AreEqual(AppPage.Success, h.Model.Page);
        StringAssert.Contains(Text(h.Model.Notice), "没能开启登录时启动");
    }

    #endregion

    #region 转发开关

    [TestMethod]
    public async Task Toggle_ChatGptRunning_TurnOn_AsksWithTitle()
    {
        var h = new Harness();
        h.ChatGpt.Running = true;
        h.Confirmer.Answer = _ => false;

        await h.Model.ToggleForwardingAsync();

        Assert.AreEqual("打开转发并重启 ChatGPT？", h.Confirmer.Requests.Single().Title);
    }

    [TestMethod]
    public async Task Toggle_ChatGptRunning_TurnOff_AsksWithTitle()
    {
        var h = new Harness();
        h.ForwardingOn();
        h.ChatGpt.Running = true;
        h.Confirmer.Answer = _ => false;

        await h.Model.ToggleForwardingAsync();

        Assert.AreEqual("关闭转发并重启 ChatGPT？", h.Confirmer.Requests.Single().Title);
    }

    [TestMethod]
    public async Task Toggle_ChatGptRunning_Cancel_CoreNotCalled()
    {
        var h = new Harness();
        h.ChatGpt.Running = true;
        h.Confirmer.Answer = _ => false;

        await h.Model.ToggleForwardingAsync();

        Assert.AreEqual(0, h.Core.Count("set_forwarding"));
        Assert.AreEqual(0, h.ChatGpt.RestartCount);
        Assert.IsFalse(h.Model.ForwardingEnabled);
    }

    [TestMethod]
    public async Task Toggle_ChatGptRunning_Confirm_SetsForwardingThenRestarts()
    {
        var h = new Harness();
        h.ChatGpt.Running = true;

        await h.Model.ToggleForwardingAsync();

        CollectionAssert.AreEqual(new[] { "confirm", "call:set_forwarding", "restart" }, h.Log);
        Assert.AreEqual(true, h.Core.ParamsOf("set_forwarding")["enabled"]);
        Assert.IsTrue(h.Model.ForwardingEnabled);
    }

    [TestMethod]
    public async Task Toggle_ChatGptRunning_TurnOn_ProgressText()
    {
        var h = new Harness();
        h.ChatGpt.Running = true;
        string? progress = null;
        h.ChatGpt.OnRestart = () => progress = h.Model.ProgressText;

        await h.Model.ToggleForwardingAsync();

        Assert.AreEqual("正在打开转发并重启 ChatGPT", progress);
    }

    [TestMethod]
    public async Task Toggle_ChatGptRunning_TurnOff_ProgressText()
    {
        var h = new Harness();
        h.ForwardingOn();
        h.ChatGpt.Running = true;
        string? progress = null;
        h.ChatGpt.OnRestart = () => progress = h.Model.ProgressText;

        await h.Model.ToggleForwardingAsync();

        Assert.AreEqual("正在还原 Codex 配置并重启 ChatGPT", progress);
    }

    [TestMethod]
    public async Task Toggle_ChatGptNotRunning_SwitchesDirectlyWithoutRestart()
    {
        var h = new Harness();
        h.ChatGpt.Running = false;

        await h.Model.ToggleForwardingAsync();

        Assert.AreEqual(0, h.Confirmer.Requests.Count);
        Assert.AreEqual(1, h.Core.Count("set_forwarding"));
        Assert.AreEqual(0, h.ChatGpt.RestartCount);
        Assert.IsTrue(h.Model.ForwardingEnabled);
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
    public async Task Toggle_RestoreBlocked_ChatGptNotRestarted()
    {
        var h = new Harness();
        h.ForwardingOn();
        h.ChatGpt.Running = true;
        h.Core.Fail("set_forwarding", "restore_blocked", "model_provider");

        await h.Model.ToggleForwardingAsync();

        Assert.AreEqual(0, h.ChatGpt.RestartCount);
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

    [TestMethod]
    public async Task Toggle_RestartResults_ShowMatchingNotice()
    {
        var cases = new[]
        {
            (ChatGptRestartResult.CloseTimedOut, "ChatGPT 没有退出"),
            (ChatGptRestartResult.OpenFailed, "没能打开 ChatGPT，请手动打开"),
            (ChatGptRestartResult.NotFound, "没有找到 ChatGPT"),
        };
        foreach (var (result, text) in cases)
        {
            var h = new Harness();
            h.ChatGpt.Running = true;
            h.ChatGpt.Result = result;

            await h.Model.ToggleForwardingAsync();

            StringAssert.Contains(Text(h.Model.Notice), text, result.ToString());
        }
    }

    [TestMethod]
    public async Task Toggle_RestartSucceeded_NoNotice()
    {
        var h = new Harness();
        h.ChatGpt.Running = true;

        await h.Model.ToggleForwardingAsync();

        Assert.IsNull(h.Model.Notice);
    }

    #endregion

    #region 保存修改

    [TestMethod]
    public async Task Save_CoreFails_NoSaveNoticeWithReason_InputKept()
    {
        var h = new Harness();
        h.FillInputs();
        h.Core.Fail("save_connection", "relay_403");

        await h.Model.SaveConnectionAsync();

        Assert.AreEqual(NoSave, h.Model.Notice?.Title);
        Assert.AreEqual(Msg("relay_403"), h.Model.Notice?.Detail);
        Assert.AreEqual(0, h.Vault.Saved.Count);
        Assert.AreEqual("https://relay.example.com", h.Model.BaseUrlInput);
        Assert.AreEqual("sk-example-0000", h.Model.ApiKeyInput);
    }

    [TestMethod]
    public async Task Save_Success_WritesCredentialAndSettings()
    {
        var h = new Harness();
        h.FillInputs();
        await h.Model.SaveConnectionAsync();

        CollectionAssert.AreEqual(new[] { "sk-example-0000" }, h.Vault.Saved);
        Assert.AreEqual("https://relay.example.com", h.Settings.BaseUrl);
    }

    [TestMethod]
    public async Task Save_CredentialWriteFails_RollsBackCoreWithOldValues()
    {
        var h = new Harness();
        h.Settings.BaseUrl = OldUrl;
        h.Vault.Stored = OldKey;
        h.FillInputs();
        h.Vault.SaveException = new IOException();

        await h.Model.SaveConnectionAsync();

        Assert.AreEqual(2, h.Core.Count("save_connection"));
        var rollback = h.Core.Calls.Last(c => c.Method == "save_connection").Params;
        Assert.AreEqual(OldUrl, rollback["base_url"]);
        Assert.AreEqual(OldKey, rollback["api_key"]);
        Assert.AreEqual(NoSave, h.Model.Notice?.Title);
        Assert.AreEqual(CredentialWriteFailed, h.Model.Notice?.Detail);
    }

    [TestMethod]
    public async Task Save_SettingsWriteFails_RollsBackCoreWithOldValues()
    {
        var h = new Harness();
        h.Settings.BaseUrl = OldUrl;
        h.Vault.Stored = OldKey;
        h.FillInputs();
        h.Settings.SaveException = new IOException();

        await h.Model.SaveConnectionAsync();

        var rollback = h.Core.Calls.Last(c => c.Method == "save_connection").Params;
        Assert.AreEqual(2, h.Core.Count("save_connection"));
        Assert.AreEqual(OldUrl, rollback["base_url"]);
        Assert.AreEqual(OldKey, rollback["api_key"]);
        Assert.AreEqual(OldKey, h.Vault.Stored);
        Assert.AreEqual(NoSave, h.Model.Notice?.Title);
    }

    [TestMethod]
    public async Task Save_ModelsChangedAndChatGptRunning_AsksToRestart()
    {
        var h = new Harness();
        h.FillInputs();
        h.ChatGpt.Running = true;
        h.Core.On("save_connection", _ => Json.Parse("""{"models_changed":true,"relay_model_count":7}"""));
        h.Confirmer.Answer = _ => false;

        await h.Model.SaveConnectionAsync();

        var ask = h.Confirmer.Requests.Single();
        Assert.AreEqual("已保存，重启 ChatGPT 以更新模型列表？", ask.Title);
        Assert.AreEqual("稍后", ask.CancelText);
        Assert.AreEqual("立即重启", ask.ConfirmText);
        Assert.AreEqual(0, h.ChatGpt.RestartCount);
    }

    [TestMethod]
    public async Task Save_ModelsChangedAndRestartConfirmed_RestartsChatGpt()
    {
        var h = new Harness();
        h.FillInputs();
        h.ChatGpt.Running = true;
        h.Core.On("save_connection", _ => Json.Parse("""{"models_changed":true,"relay_model_count":7}"""));

        await h.Model.SaveConnectionAsync();

        Assert.AreEqual(1, h.ChatGpt.RestartCount);
    }

    [TestMethod]
    public async Task Save_ModelsChangedButChatGptNotRunning_DoesNotAsk()
    {
        var h = new Harness();
        h.FillInputs();
        h.ChatGpt.Running = false;
        h.Core.On("save_connection", _ => Json.Parse("""{"models_changed":true,"relay_model_count":7}"""));

        await h.Model.SaveConnectionAsync();

        Assert.AreEqual(0, h.Confirmer.Requests.Count);
        Assert.AreEqual(0, h.ChatGpt.RestartCount);
    }

    [TestMethod]
    public async Task Save_ModelsUnchanged_NoticeNoRestartNeeded()
    {
        var h = new Harness();
        h.FillInputs();
        h.ChatGpt.Running = true;

        await h.Model.SaveConnectionAsync();

        Assert.AreEqual("已保存，不需要重启 ChatGPT", h.Model.Notice?.Title);
        Assert.AreEqual(0, h.Confirmer.Requests.Count);
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
            "关闭后，下次登录电脑时助手不会自动运行。在你手动打开助手之前，Codex 里的官方模型和中台模型都无法使用，因为转发仍然开着。如果以后不打算常驻，建议先关闭转发。",
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
        Assert.AreEqual(0, h.Vault.DeleteCount);
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
        Assert.AreEqual(0, h.Vault.DeleteCount);
        Assert.AreEqual(0, h.Settings.DeleteCount);
        Assert.AreEqual(0, h.Login.DisableCount);
        Assert.IsNull(h.Model.Uninstalled);
    }

    [TestMethod]
    public async Task Uninstall_Success_CleansEverythingInOrder_ExitCode0()
    {
        var h = new Harness();

        var code = await h.Model.UninstallAsync();

        Assert.AreEqual(0, code);
        Assert.AreEqual(1, h.Vault.DeleteCount);
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
    public async Task Uninstall_ChatGptRunning_AsksToRestartAfterCleanup()
    {
        var h = new Harness();
        h.ChatGpt.Running = true;

        await h.Model.UninstallAsync();

        Assert.AreEqual(2, h.Confirmer.Requests.Count);
        Assert.AreEqual(1, h.ChatGpt.RestartCount);
    }

    [TestMethod]
    public async Task Uninstall_ChatGptRunning_DeclineRestart_NotRestarted()
    {
        var h = new Harness();
        h.ChatGpt.Running = true;
        h.Confirmer.Answer = _ => h.Confirmer.Requests.Count == 1;

        await h.Model.UninstallAsync();

        Assert.AreEqual(2, h.Confirmer.Requests.Count);
        Assert.AreEqual(0, h.ChatGpt.RestartCount);
    }

    [TestMethod]
    public async Task Uninstall_ChatGptNotRunning_OnlyOneAsk()
    {
        var h = new Harness();
        h.ChatGpt.Running = false;

        await h.Model.UninstallAsync();

        Assert.AreEqual(1, h.Confirmer.Requests.Count);
        Assert.AreEqual(0, h.ChatGpt.RestartCount);
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

        Assert.AreEqual("已是最新版本", h.Model.Notice?.Title);
        Assert.IsNull(h.Model.UpdateVersion);
        Assert.IsNull(h.Model.UpdateUrl);
    }

    [TestMethod]
    public async Task CheckUpdate_Unavailable_Notice()
    {
        var h = new Harness();
        h.Core.Fail("check_update", "update_unavailable");

        await h.Model.CheckUpdateAsync();

        Assert.AreEqual("暂时无法检查更新，请稍后再试", h.Model.Notice?.Title);
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
    public void Banners_RelayStale()
    {
        var h = new Harness();
        h.Push(s => s["relay"]!["stale"] = true);
        Assert.IsTrue(Has(h, "中台模型列表本次没有更新"));
    }

    [TestMethod]
    public void Banners_NeverSucceeded_ZeroModelsAndError()
    {
        var h = new Harness();
        h.Push(s =>
        {
            s["relay"]!["model_count"] = 0;
            s["relay"]!["error"] = "relay_unreachable";
        });
        Assert.IsTrue(Has(h, "还没有获取到中台模型"));
    }

    [TestMethod]
    public void Banners_ZeroModelsWithoutError_NoNeverSucceeded()
    {
        var h = new Harness();
        h.Push(s => s["relay"]!["model_count"] = 0);
        Assert.IsFalse(Has(h, "还没有获取到中台模型"));
    }

    [TestMethod]
    public void Banners_ModelsPresentWithError_NoNeverSucceeded()
    {
        var h = new Harness();
        h.Push(s => s["relay"]!["error"] = "relay_unreachable");
        Assert.IsFalse(Has(h, "还没有获取到中台模型"));
    }

    [TestMethod]
    public void Banners_RestoreBlocked_ShowsKeysAndConfigPath()
    {
        var h = new Harness();
        h.Push(s =>
        {
            s["restore_blocked"] = new JsonArray("model_provider", "openai_base_url");
            s["config_path"] = "/home/example/.codex/config.toml";
        });

        var banner = h.Model.Banners.Single(b => b.Title == "没能自动还原 Codex 配置");
        StringAssert.Contains(banner.Detail, "model_provider");
        StringAssert.Contains(banner.Detail, "openai_base_url");
        StringAssert.Contains(banner.Detail, "/home/example/.codex/config.toml");
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
        h.Push(s => s["relay"]!["stale"] = true);
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
        StringAssert.Contains(Text(h.Model.Notice), "后台服务没能启动，请退出后重新打开");
    }

    [TestMethod]
    public async Task Start_PassesStoredUrlKeyAndChatGptDirToCore()
    {
        var h = new Harness();
        h.Settings.BaseUrl = OldUrl;
        h.Vault.Stored = OldKey;
        h.ChatGpt.LocatedDir = @"C:\Apps\ChatGPT";

        await h.Model.StartAsync();

        Assert.AreEqual(new CoreInitParams(OldUrl, OldKey, @"C:\Apps\ChatGPT"), h.Core.StartedWith);
    }

    #endregion

    #region 凭据读取失败与单实例

    [TestMethod]
    public async Task Start_CredentialReadFails_NoticeAndAsksToShowMainWindow()
    {
        var h = new Harness();
        h.Vault.ReadException = new System.ComponentModel.Win32Exception(5);

        await h.Model.StartAsync();

        StringAssert.Contains(Text(h.Model.Notice), "无法读取已保存的 API Key，请重新输入");
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

    #region 模型数量

    [TestMethod]
    public void ModelCountText_FromStatus()
    {
        var h = new Harness();
        h.Push(s => s["relay"]!["model_count"] = 12);
        Assert.AreEqual("已获取 12 个", h.Model.ModelCountText);
    }

    [TestMethod]
    public void ModelCountText_DoesNotExposeModelNames()
    {
        var h = new Harness();
        h.Push(s => s["relay"]!["models"] = new JsonArray("gpt-example-1"));

        var shown = h.Model.ModelCountText + string.Join("", h.Model.Banners.Select(b => b.Title + b.Detail));
        Assert.IsFalse(shown.Contains("gpt-example-1"));
    }

    #endregion

    #region 复核补充

    static void AssertCoreReset(Harness h)
    {
        var init = h.Core.ParamsOf("init");
        Assert.IsNull(init["base_url"]);
        Assert.IsNull(init["api_key"]);
    }

    [TestMethod]
    public async Task Setup_SettingsWriteFails_DeletesKeyAndResetsCoreToNoConnection()
    {
        var h = new Harness();
        h.FillInputs();
        h.Settings.SaveException = new IOException();

        await h.Model.CompleteSetupAsync();

        Assert.AreEqual(1, h.Vault.DeleteCount);
        Assert.IsNull(h.Vault.Stored);
        AssertCoreReset(h);
        Assert.AreEqual(NoSave, h.Model.Notice?.Title);
        Assert.AreEqual(AppPage.Setup, h.Model.Page);
    }

    [TestMethod]
    public async Task Setup_CredentialWriteFails_ResetsCoreToNoConnection()
    {
        var h = new Harness();
        h.FillInputs();
        h.Vault.SaveException = new IOException();

        await h.Model.CompleteSetupAsync();

        AssertCoreReset(h);
    }

    [TestMethod]
    public async Task Save_SettingsWriteFails_NoOldKey_DeletesNewKeyAndResetsCore()
    {
        var h = new Harness();
        h.Settings.BaseUrl = OldUrl;
        h.FillInputs();
        h.Settings.SaveException = new IOException();

        await h.Model.SaveConnectionAsync();

        Assert.AreEqual(1, h.Core.Count("save_connection"));
        Assert.AreEqual(1, h.Vault.DeleteCount);
        Assert.IsNull(h.Vault.Stored);
        AssertCoreReset(h);
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
        StringAssert.Contains(banner.Detail, "model_provider");
        StringAssert.Contains(banner.Detail, "openai_base_url");
        StringAssert.Contains(banner.Detail, "/home/example/.codex/config.toml");
        CollectionAssert.Contains(banner.Actions.ToList(), BannerAction.OpenConfigFolder);
    }

    [TestMethod]
    public async Task Quit_OtherError_NoticeWithReason_NoExit()
    {
        var h = new Harness();
        h.ForwardingOn();
        h.Core.Fail("shutdown", "internal_error");

        await h.Model.QuitAsync();

        Assert.AreEqual("没能退出", h.Model.Notice?.Title);
        Assert.AreEqual(Msg("internal_error"), h.Model.Notice?.Detail);
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
    public async Task Toggle_ConfirmPending_SecondCallReturnsWithoutAsking()
    {
        var h = new Harness();
        h.ChatGpt.Running = true;
        h.Confirmer.Gate = new TaskCompletionSource<bool>();

        var first = h.Model.ToggleForwardingAsync();
        await h.Model.ToggleForwardingAsync();

        Assert.AreEqual(1, h.Confirmer.Requests.Count);
        Assert.IsTrue(h.Model.IsBusy);
        h.Confirmer.Gate.SetResult(false);
        await first;
        Assert.IsFalse(h.Model.IsBusy);
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
        var withKey = new Harness();
        withKey.Vault.Stored = OldKey;
        var withLogin = new Harness();
        withLogin.Login.Enabled = true;
        var unreadable = new Harness();
        unreadable.Vault.ReadException = new IOException();

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
            Assert.IsNull(h.Core.StartedWith?.BaseUrl, ex.GetType().Name);
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
}
