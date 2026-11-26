using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.UI;

namespace ModelBridge.Tests;

[TestClass]
public class RelayOnlySwitchTests
{
    const string ToRelayTitle = "已切换到「只用中转」";
    const string ToRelayDetail = "重启 ChatGPT 后生效。重启后请新建对话：用「官方 + 中转」聊过的旧对话接不上。";
    const string ToBothTitle = "已切换到「官方 + 中转」";
    const string ToBothDetail = "重启 ChatGPT 后生效。重启后请新建对话：用「只用中转」聊过的旧对话接不上。";
    const string FailedTitle = "没能切换接入方式";
    const string FailedDetail = "没能修改 Codex 配置，接入方式保持原样。请稍后再试。";
    const string NoChangeTitle = "接入方式没有变化";
    const string NoChangeRelayOnlyDetail = "仍然检测到 ChatGPT 登录。你手动选了只用中转，继续只用中转模型。";

    static Action<JsonObject> Access(string mode, string detect, bool relayOnly, bool loggedIn, string? defaultRelay = "中转") => s =>
    {
        s["forwarding"] = true;
        s["default_relay"] = defaultRelay;
        s["access"] = new JsonObject
        {
            ["mode"] = mode,
            ["target"] = mode,
            ["detect"] = detect,
            ["relay_only"] = relayOnly,
            ["logged_in"] = loggedIn,
        };
    };

    static JsonElement Status(string mode, string detect, bool relayOnly, bool loggedIn) =>
        Json.Status(Access(mode, detect, relayOnly, loggedIn));

    static Harness Pushed(string mode, string detect, bool relayOnly, bool loggedIn, string? defaultRelay = "中转")
    {
        var h = new Harness();
        h.Push(Access(mode, detect, relayOnly, loggedIn, defaultRelay));
        return h;
    }

    static Harness LoggedInBoth(bool chatGptRunning = false)
    {
        var h = Pushed("both", "logged_in", false, true);
        h.ChatGpt.Running = chatGptRunning;
        return h;
    }

    static Harness LoggedInRelayOnly(bool chatGptRunning = false)
    {
        var h = Pushed("relay", "logged_in", true, true);
        h.ChatGpt.Running = chatGptRunning;
        return h;
    }

    static async Task WaitUntilAsync(Func<bool> condition)
    {
        var deadline = DateTime.UtcNow.AddSeconds(5);
        while (!condition() && DateTime.UtcNow < deadline) await Task.Delay(20);
    }

    #region 开关能不能点

    [TestMethod]
    public async Task Usable_LoggedIn_True()
    {
        Assert.IsTrue((await StartedAsync("both", false, true)).Model.RelayOnlySwitchUsable);
    }

    [TestMethod]
    public async Task Usable_NotLoggedIn_False()
    {
        Assert.IsFalse((await StartedAsync("relay", false, false)).Model.RelayOnlySwitchUsable);
    }

    [TestMethod]
    public async Task Disabled_ShowsRelayOnlyValueAsIs()
    {
        foreach (var relayOnly in new[] { true, false })
        {
            var model = (await StartedAsync("relay", relayOnly, false)).Model;

            Assert.IsFalse(model.RelayOnlySwitchUsable, $"relayOnly={relayOnly}");
            Assert.AreEqual(relayOnly, model.RelayOnlyEnabled, $"relayOnly={relayOnly}");
        }
    }

    [TestMethod]
    public async Task Usable_WhileDetecting_StillTrue()
    {
        var h = await StartedAsync("both", false, true);
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("detect_access", _ => gate.Task);

        var task = h.Model.RecheckAccessAsync();

        Assert.IsTrue(h.Model.IsDetectingAccess);
        Assert.IsTrue(h.Model.RelayOnlySwitchUsable);
        gate.SetResult(Status("both", "logged_in", false, true));
        await task;
    }

    [TestMethod]
    public void RelayOnlyEnabled_FollowsStatus()
    {
        Assert.IsTrue(Pushed("relay", "logged_in", true, true).Model.RelayOnlyEnabled);
        Assert.IsFalse(Pushed("both", "logged_in", false, true).Model.RelayOnlyEnabled);
    }

    #endregion

    #region 标题下的说明

    [TestMethod]
    public void Overview_Both()
    {
        Assert.AreEqual(
            "官方模型在上，中转模型在下；选官方模型时仍访问官方服务。",
            Pushed("both", "logged_in", false, true).Model.OverviewText);
    }

    [TestMethod]
    public void Overview_RelayOnlyAndLoggedIn()
    {
        Assert.AreEqual(
            "已登录 ChatGPT，手动选了只用中转。只有中转模型，默认选中「中转 · auto」。",
            Pushed("relay", "logged_in", true, true).Model.OverviewText);
    }

    [TestMethod]
    public void Overview_RelayNotLoggedIn()
    {
        Assert.AreEqual(
            "没有登录 ChatGPT，只能用中转模型，默认选中「中转 · auto」。登录后才能改接入方式。",
            Pushed("relay", "not_logged_in", false, false).Model.OverviewText);
    }

    [TestMethod]
    public void Overview_RelayOnlyButNotLoggedIn_UsesNotLoggedInText()
    {
        Assert.AreEqual(
            "没有登录 ChatGPT，只能用中转模型，默认选中「中转 · auto」。登录后才能改接入方式。",
            Pushed("relay", "not_logged_in", true, false).Model.OverviewText);
    }

    [TestMethod]
    public void Overview_RelayOnlyAndLoggedIn_NoDefaultRelay()
    {
        Assert.AreEqual(
            "已登录 ChatGPT，手动选了只用中转。只有中转模型，没有默认选中的模型。",
            Pushed("relay", "logged_in", true, true, defaultRelay: null).Model.OverviewText);
    }

    [TestMethod]
    public void Overview_RelayNotLoggedIn_NoDefaultRelay()
    {
        Assert.AreEqual(
            "没有登录 ChatGPT，只能用中转模型，没有默认选中的模型。登录后才能改接入方式。",
            Pushed("relay", "not_logged_in", false, false, defaultRelay: null).Model.OverviewText);
    }

    #endregion

    #region 点开关

    [TestMethod]
    public async Task SetRelayOnly_On_CallsCoreWithEnabledTrue()
    {
        var h = LoggedInBoth();
        h.Core.On("set_relay_only", _ => Status("relay", "logged_in", true, true));

        await h.Model.SetRelayOnlyAsync(true);

        Assert.AreEqual(1, h.Core.Count("set_relay_only"));
        Assert.AreEqual(true, h.Core.ParamsOf("set_relay_only")["enabled"]);
        Assert.IsTrue(h.Model.RelayOnlyEnabled);
    }

    [TestMethod]
    public async Task SetRelayOnly_Off_CallsCoreWithEnabledFalse()
    {
        var h = LoggedInRelayOnly();
        h.Core.On("set_relay_only", _ => Status("both", "logged_in", false, true));

        await h.Model.SetRelayOnlyAsync(false);

        Assert.AreEqual(false, h.Core.ParamsOf("set_relay_only")["enabled"]);
        Assert.IsFalse(h.Model.RelayOnlyEnabled);
    }

    [TestMethod]
    public async Task SetRelayOnly_On_ChatGptRunning_SuccessNotice()
    {
        var h = LoggedInBoth(chatGptRunning: true);
        h.Core.On("set_relay_only", _ => Status("relay", "logged_in", true, true));

        await h.Model.SetRelayOnlyAsync(true);

        var notice = h.Model.Notices.Single();
        Assert.AreEqual(ToRelayTitle, notice.Title);
        Assert.AreEqual(ToRelayDetail, notice.Detail);
        Assert.AreEqual(AppSeverity.Success, notice.Severity);
    }

    [TestMethod]
    public async Task SetRelayOnly_Off_ChatGptRunning_SuccessNotice()
    {
        var h = LoggedInRelayOnly(chatGptRunning: true);
        h.Core.On("set_relay_only", _ => Status("both", "logged_in", false, true));

        await h.Model.SetRelayOnlyAsync(false);

        var notice = h.Model.Notices.Single();
        Assert.AreEqual(ToBothTitle, notice.Title);
        Assert.AreEqual(ToBothDetail, notice.Detail);
        Assert.AreEqual(AppSeverity.Success, notice.Severity);
    }

    [TestMethod]
    public async Task SetRelayOnly_ChatGptNotRunning_NoNotice()
    {
        var h = LoggedInBoth(chatGptRunning: false);
        h.Core.On("set_relay_only", _ => Status("relay", "logged_in", true, true));
        await h.Model.SetRelayOnlyAsync(true);
        Assert.AreEqual(0, h.Model.Notices.Count);

        h.Core.On("set_relay_only", _ => Status("both", "logged_in", false, true));
        await h.Model.SetRelayOnlyAsync(false);
        Assert.AreEqual(0, h.Model.Notices.Count);
    }

    [TestMethod]
    public async Task SetRelayOnly_SuccessNotice_AutoDismissesAfterFourSeconds()
    {
        var h = LoggedInBoth(chatGptRunning: true);
        h.Core.On("set_relay_only", _ => Status("relay", "logged_in", true, true));

        await h.Model.SetRelayOnlyAsync(true);

        Assert.AreEqual(TimeSpan.FromSeconds(4), h.Model.NoticeAutoDismissDelay);
    }

    [TestMethod]
    public async Task SetRelayOnly_SuccessNotice_DisappearsAfterDelay()
    {
        var h = LoggedInBoth(chatGptRunning: true);
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMilliseconds(100);
        h.Core.On("set_relay_only", _ => Status("relay", "logged_in", true, true));

        await h.Model.SetRelayOnlyAsync(true);
        await WaitUntilAsync(() => h.Model.Notices.Count == 0);

        Assert.AreEqual(0, h.Model.Notices.Count);
    }

    [TestMethod]
    public async Task SetRelayOnly_Fails_ErrorNoticeAndStateUnchanged()
    {
        foreach (var running in new[] { false, true })
        {
            var h = LoggedInBoth(running);
            h.Core.Fail("set_relay_only", "config_changed");

            await h.Model.SetRelayOnlyAsync(true);

            var notice = h.Model.Notices.Single();
            Assert.AreEqual(FailedTitle, notice.Title, $"running={running}");
            Assert.AreEqual(FailedDetail, notice.Detail, $"running={running}");
            Assert.AreEqual(AppSeverity.Danger, notice.Severity, $"running={running}");
            Assert.IsFalse(h.Model.RelayOnlyEnabled, $"running={running}");
            Assert.AreEqual("官方 + 中转", h.Model.AccessModeName, $"running={running}");
            Assert.IsFalse(h.Model.IsBusy, $"running={running}");
        }
    }

    [TestMethod]
    public async Task SetRelayOnly_FailureNotice_DoesNotAutoDismiss()
    {
        var h = LoggedInBoth();
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMilliseconds(50);
        h.Core.Fail("set_relay_only", "config_changed");

        await h.Model.SetRelayOnlyAsync(true);
        await Task.Delay(300);

        Assert.AreEqual(1, h.Model.Notices.Count);
    }

    [TestMethod]
    public async Task SetRelayOnly_FailureThenSuccess_RemovesErrorNotice()
    {
        var h = LoggedInBoth();
        h.Core.Fail("set_relay_only", "config_changed");
        await h.Model.SetRelayOnlyAsync(true);
        Assert.AreEqual(FailedTitle, h.Model.Notices.Single().Title);

        h.Core.On("set_relay_only", _ => Status("relay", "logged_in", true, true));
        await h.Model.SetRelayOnlyAsync(true);

        Assert.IsFalse(h.Model.Notices.Any(n => n.Title == FailedTitle));
    }

    [TestMethod]
    public async Task SetRelayOnly_WhileInFlight_IsBusyAndSecondCallIgnored()
    {
        var h = LoggedInBoth();
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("set_relay_only", _ => gate.Task);

        var first = h.Model.SetRelayOnlyAsync(true);
        Assert.IsTrue(h.Model.IsBusy);
        await h.Model.SetRelayOnlyAsync(false);

        Assert.AreEqual(1, h.Core.Count("set_relay_only"));
        gate.SetResult(Status("relay", "logged_in", true, true));
        await first;
        Assert.IsFalse(h.Model.IsBusy);
    }

    [TestMethod]
    public async Task SetRelayOnly_DuringForwardingToggle_IgnoredWithoutCoreCall()
    {
        var h = LoggedInBoth();
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("set_forwarding", _ => gate.Task);

        var toggling = h.Model.ToggleForwardingAsync();
        await h.Model.SetRelayOnlyAsync(true);

        Assert.AreEqual(0, h.Core.Count("set_relay_only"));
        gate.SetResult(Json.Status(s => s["forwarding"] = false));
        await toggling;
    }

    [TestMethod]
    public async Task ToggleForwarding_DuringSetRelayOnly_IgnoredWithoutCoreCall()
    {
        var h = LoggedInBoth();
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("set_relay_only", _ => gate.Task);

        var switching = h.Model.SetRelayOnlyAsync(true);
        await h.Model.ToggleForwardingAsync();

        Assert.AreEqual(0, h.Core.Count("set_forwarding"));
        gate.SetResult(Status("relay", "logged_in", true, true));
        await switching;
    }

    #endregion

    #region 重新检测

    [TestMethod]
    public async Task Recheck_SwitchToggledMeanwhile_StaleDetectResultDoesNotOverride()
    {
        var h = LoggedInBoth();
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("detect_access", _ => gate.Task);
        h.Core.On("set_relay_only", _ => Status("relay", "logged_in", true, true));

        var rechecking = h.Model.RecheckAccessAsync();
        await h.Model.SetRelayOnlyAsync(true);
        gate.SetResult(Status("both", "logged_in", false, true));
        await rechecking;

        Assert.IsFalse(h.Model.Notices.Any(n => n.Title == NoChangeTitle));
        Assert.IsTrue(h.Model.RelayOnlyEnabled);
        Assert.AreEqual("只用中转", h.Model.AccessModeName);
    }

    [TestMethod]
    public async Task Recheck_UnchangedRelayOnlyLoggedIn_NoChangeNoticeWithRelayOnlyText()
    {
        var h = LoggedInRelayOnly();
        h.Core.On("detect_access", _ => Status("relay", "logged_in", true, true));

        await h.Model.RecheckAccessAsync();

        var notice = h.Model.Notices.Single();
        Assert.AreEqual(NoChangeTitle, notice.Title);
        Assert.AreEqual(NoChangeRelayOnlyDetail, notice.Detail);
        Assert.AreEqual(AppSeverity.Info, notice.Severity);
        Assert.AreEqual(0, h.Core.Count("apply_access"));
    }

    [TestMethod]
    public async Task Recheck_UnchangedBoth_KeepsExistingText()
    {
        var h = LoggedInBoth();
        h.Core.On("detect_access", _ => Status("both", "logged_in", false, true));

        await h.Model.RecheckAccessAsync();

        Assert.AreEqual("仍然检测到 ChatGPT 登录，继续同时使用官方模型和中转模型。", h.Model.Notices.Single().Detail);
    }

    [TestMethod]
    public async Task Recheck_UnchangedRelayNotLoggedIn_KeepsExistingText()
    {
        var h = Pushed("relay", "not_logged_in", false, false);
        h.Core.On("detect_access", _ => Status("relay", "not_logged_in", false, false));

        await h.Model.RecheckAccessAsync();

        Assert.AreEqual("仍然没有检测到 ChatGPT 登录，继续只用中转模型。", h.Model.Notices.Single().Detail);
    }

    #endregion

    #region 开关能不能点：服务失败与进行中

    static async Task<Harness> StartedAsync(string mode, bool relayOnly, bool loggedIn)
    {
        var h = new Harness();
        await h.Model.StartAsync();
        h.Push(Access(mode, "logged_in", relayOnly, loggedIn));
        return h;
    }

    [TestMethod]
    public async Task Usable_AfterCoreFailed_FalseAndSwitchKeepsValue()
    {
        foreach (var relayOnly in new[] { true, false })
        {
            var h = await StartedAsync(relayOnly ? "relay" : "both", relayOnly, true);

            h.Core.RaiseFailed("core exited");

            Assert.IsFalse(h.Model.RelayOnlySwitchUsable, $"relayOnly={relayOnly}");
            Assert.AreEqual(relayOnly, h.Model.RelayOnlyEnabled, $"relayOnly={relayOnly}");
        }
    }

    [TestMethod]
    public async Task Usable_WhileSetRelayOnlyInFlight_False()
    {
        var h = await StartedAsync("both", false, true);
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("set_relay_only", _ => gate.Task);

        var task = h.Model.SetRelayOnlyAsync(true);

        Assert.IsFalse(h.Model.RelayOnlySwitchUsable);
        Assert.IsFalse(h.Model.RelayOnlyEnabled);
        gate.SetResult(Status("relay", "logged_in", true, true));
        await task;
        Assert.IsTrue(h.Model.RelayOnlySwitchUsable);
    }

    [TestMethod]
    public async Task Usable_WhileForwardingToggleInFlight_False()
    {
        var h = await StartedAsync("both", false, true);
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("set_forwarding", _ => gate.Task);

        var task = h.Model.ToggleForwardingAsync();

        Assert.IsFalse(h.Model.RelayOnlySwitchUsable);
        gate.SetResult(Status("both", "logged_in", false, true));
        await task;
    }

    [TestMethod]
    public async Task Usable_WhileApplyAccessInFlight_False()
    {
        var h = await StartedAsync("both", false, true);
        var apply = new TaskCompletionSource<JsonElement>();
        h.Core.On("detect_access", _ => Json.Status(s =>
        {
            Access("relay", "logged_in", false, true)(s);
            s["access"]!["target"] = "both";
        }));
        h.Core.OnAsync("apply_access", _ => apply.Task);

        var rechecking = h.Model.RecheckAccessAsync();
        await WaitUntilAsync(() => h.Core.Count("apply_access") == 1);

        Assert.IsFalse(h.Model.RelayOnlySwitchUsable);
        apply.SetResult(Status("both", "logged_in", false, true));
        await rechecking;
        Assert.IsTrue(h.Model.RelayOnlySwitchUsable);
    }

    #endregion

    #region 第四种说明

    [TestMethod]
    public void Overview_RelayLoggedInButNotRelayOnly_SaysCouldNotSwitchToBoth()
    {
        var model = Pushed("relay", "logged_in", false, true).Model;

        Assert.AreEqual(
            "已登录 ChatGPT，但没能切到「官方 + 中转」，暂时只有中转模型。点「重新检测」再试。",
            model.OverviewText);
        Assert.IsFalse(model.RelayOnlyEnabled);
    }

    #endregion

    #region 切换成功提示

    const string NoticePrefix = "已切换到";

    [TestMethod]
    public async Task SetRelayOnly_ReturnedModeSameAsBefore_NoSwitchedNotice()
    {
        var h = LoggedInBoth(chatGptRunning: true);
        h.Core.On("set_relay_only", _ => Status("both", "logged_in", false, true));

        await h.Model.SetRelayOnlyAsync(true);

        Assert.IsFalse(h.Model.Notices.Any(n => n.Title.StartsWith(NoticePrefix)));
    }

    [TestMethod]
    public async Task SetRelayOnly_FromFourthState_ModeStaysRelay_NoSwitchedNotice()
    {
        var h = Pushed("relay", "logged_in", false, true);
        h.ChatGpt.Running = true;
        h.Core.On("set_relay_only", _ => Status("relay", "logged_in", true, true));

        await h.Model.SetRelayOnlyAsync(true);

        Assert.IsFalse(h.Model.Notices.Any(n => n.Title.StartsWith(NoticePrefix)));
    }

    [TestMethod]
    public async Task SetRelayOnly_TitleFollowsModeCoreReturned()
    {
        var h = LoggedInBoth(chatGptRunning: true);
        h.Core.On("set_relay_only", _ => Status("relay", "logged_in", true, true));

        await h.Model.SetRelayOnlyAsync(false);

        Assert.AreEqual(ToRelayTitle, h.Model.Notices.Single().Title);
    }

    [TestMethod]
    public async Task SetRelayOnly_OnThenOff_OnlyLatestSwitchedNoticeInPlace()
    {
        var h = LoggedInBoth(chatGptRunning: true);
        h.Core.On("set_relay_only", _ => Status("relay", "logged_in", true, true));
        await h.Model.SetRelayOnlyAsync(true);
        h.Core.On("detect_access", _ => Json.Status(s =>
        {
            Access("relay", "failed", true, true)(s);
        }));
        await h.Model.RecheckAccessAsync();
        Assert.AreEqual(2, h.Model.Notices.Count);

        h.Core.On("set_relay_only", _ => Status("both", "logged_in", false, true));
        await h.Model.SetRelayOnlyAsync(false);

        Assert.AreEqual(1, h.Model.Notices.Count(n => n.Title.StartsWith(NoticePrefix)));
        Assert.AreEqual(2, h.Model.Notices.Count);
        Assert.AreEqual(ToBothTitle, h.Model.Notices[0].Title);
    }

    [TestMethod]
    public async Task ManualSwitchedNotice_ThenAutoSwitchedByRecheck_OnlyLatestKeptInPlace()
    {
        var h = LoggedInBoth(chatGptRunning: true);
        h.Core.On("set_relay_only", _ => Status("relay", "logged_in", true, true));
        await h.Model.SetRelayOnlyAsync(true);
        Assert.AreEqual(ToRelayTitle, h.Model.Notices.Single().Title);
        h.Core.On("detect_access", _ => Json.Status(s =>
        {
            Access("relay", "logged_in", false, true)(s);
            s["access"]!["target"] = "both";
        }));
        h.Core.On("apply_access", _ => Status("both", "logged_in", false, true));

        await h.Model.RecheckAccessAsync();

        Assert.AreEqual(1, h.Core.Count("apply_access"));
        Assert.AreEqual(1, h.Model.Notices.Count(n => n.Title.StartsWith(NoticePrefix)));
        Assert.AreEqual(ToBothTitle, h.Model.Notices[0].Title);
    }

    #endregion

    #region 检测期间切换过开关

    [TestMethod]
    public async Task Recheck_SwitchToggledMeanwhile_DetectFailed_StillWarns()
    {
        var h = LoggedInBoth();
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("detect_access", _ => gate.Task);
        h.Core.On("set_relay_only", _ => Status("relay", "logged_in", true, true));

        var rechecking = h.Model.RecheckAccessAsync();
        await h.Model.SetRelayOnlyAsync(true);
        gate.SetResult(Status("both", "failed", false, true));
        await rechecking;

        Assert.AreEqual(1, h.Model.Notices.Count(n => n.Title == "没能检测 ChatGPT 登录状态"));
        Assert.IsTrue(h.Model.RelayOnlyEnabled);
        Assert.AreEqual("只用中转", h.Model.AccessModeName);
    }

    #endregion
}
