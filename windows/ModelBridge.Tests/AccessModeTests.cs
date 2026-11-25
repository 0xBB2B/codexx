using System.Text.Json.Nodes;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.UI;

namespace ModelBridge.Tests;

[TestClass]
public class AccessModeTests
{
    const string DetectFailedTitle = "没能检测 ChatGPT 登录状态";
    const string DetectFailedDetail = "沿用上次的方式。可以稍后点「重新检测」再试。";
    const string NoChangeTitle = "接入方式没有变化";
    const string RecheckFailedTitle = "没能重新检测";
    const string ToBothTitle = "已切换到「官方 + 中转」";
    const string ToBothDetail = "已检测到 ChatGPT 登录。重启 ChatGPT 后生效。";
    const string ToRelayTitle = "已切换到「只用中转」";
    const string ToRelayDetail = "没有检测到 ChatGPT 登录。重启 ChatGPT 后生效。";
    const string SwitchFailedTitle = "没能切换接入方式";
    const string RestoreBlockedTitle = "没能自动还原 Codex 配置";
    const string SuccessTail = "\n请重启 ChatGPT 后使用。助手会在你登录电脑时自动启动，在任务栏通知区域里常驻。";

    static Action<JsonObject> Access(string? mode, string? target, string? detect, bool forwarding = true) => s =>
    {
        s["forwarding"] = forwarding;
        s["access"] = new JsonObject { ["mode"] = mode, ["target"] = target, ["detect"] = detect };
    };

    static System.Text.Json.JsonElement Status(string? mode, string? target, string? detect, bool forwarding = true) =>
        Json.Status(Access(mode, target, detect, forwarding));

    static void Answer(Harness h, string method, string? mode, string? target, string? detect) =>
        h.Core.On(method, _ => Status(mode, target, detect));

    static async Task<Harness> StartedAsync(bool chatGptRunning = false)
    {
        var h = new Harness();
        h.ChatGpt.Running = chatGptRunning;
        await h.Model.StartAsync();
        return h;
    }

    static Harness Pushed(string? mode, string? target, string? detect, bool forwarding = true)
    {
        var h = new Harness();
        h.Push(Access(mode, target, detect, forwarding));
        return h;
    }

    static async Task WaitUntilAsync(Func<bool> condition)
    {
        var deadline = DateTime.UtcNow.AddSeconds(5);
        while (!condition() && DateTime.UtcNow < deadline) await Task.Delay(20);
    }

    static string Msg(string code) => ModelBridge.Services.CoreErrorMessages.Messages[code];

    static string[] Titles(Harness h) => h.Model.Notices.Select(n => n.Title).ToArray();

    #region 状态里的接入方式

    [TestMethod]
    public void AccessModeName_Relay_IsRelayOnly()
    {
        Assert.AreEqual("只用中转", Pushed("relay", "relay", "not_logged_in").Model.AccessModeName);
    }

    [TestMethod]
    public void AccessModeName_Both_IsOfficialPlusRelay()
    {
        Assert.AreEqual("官方 + 中转", Pushed("both", "both", "logged_in").Model.AccessModeName);
    }

    [TestMethod]
    public void AccessModeName_ForwardingOff_IsNull()
    {
        Assert.IsNull(Pushed("both", "both", "logged_in", forwarding: false).Model.AccessModeName);
    }

    [TestMethod]
    public void AccessModeName_ModeNull_IsNull()
    {
        Assert.IsNull(Pushed(null, null, null).Model.AccessModeName);
    }

    [TestMethod]
    public void AccessModeName_StatusWithoutAccessKey_IsNull()
    {
        var h = new Harness();
        h.Push(s => s["forwarding"] = true);

        Assert.IsNull(h.Model.AccessModeName);
    }

    [TestMethod]
    public void AccessReasonText_LoggedIn()
    {
        Assert.AreEqual("· 已检测到 ChatGPT 登录", Pushed("both", "both", "logged_in").Model.AccessReasonText);
    }

    [TestMethod]
    public void AccessReasonText_NotLoggedIn()
    {
        Assert.AreEqual("· 没有检测到 ChatGPT 登录", Pushed("relay", "relay", "not_logged_in").Model.AccessReasonText);
    }

    [TestMethod]
    public void AccessReasonText_DetectFailed_KeepsLastWay()
    {
        Assert.AreEqual("· 沿用上次的方式", Pushed("relay", "relay", "failed").Model.AccessReasonText);
    }

    [TestMethod]
    public void OverviewText_Both()
    {
        Assert.AreEqual(
            "官方模型在上，中转模型在下；选官方模型时仍访问官方服务。",
            Pushed("both", "both", "logged_in").Model.OverviewText);
    }

    [TestMethod]
    public void OverviewText_Relay()
    {
        Assert.AreEqual(
            "没有登录 ChatGPT，只能用中转模型，默认选中「中转 · auto」。登录后才能改接入方式。",
            Pushed("relay", "relay", "not_logged_in").Model.OverviewText);
    }

    [TestMethod]
    public void OverviewText_ForwardingOff_IsClosedText()
    {
        Assert.AreEqual(
            "已关闭：Codex 配置已还原为开启前的样子。",
            Pushed("relay", "relay", "not_logged_in", forwarding: false).Model.OverviewText);
        Assert.IsNull(Pushed("relay", "relay", "not_logged_in", forwarding: false).Model.AccessModeName);
    }

    [TestMethod]
    public void AccessDetectFailed_NoSeparateBanner()
    {
        Assert.AreEqual(0, Pushed("relay", "relay", "failed").Model.Banners.Count);
    }

    [TestMethod]
    public void ForwardingHelpText_On()
    {
        Assert.AreEqual("", Pushed("both", "both", "logged_in").Model.ForwardingHelpText);
    }

    [TestMethod]
    public void ForwardingHelpText_OnWithoutAccessMode_IsEmpty()
    {
        Assert.AreEqual("", Pushed(null, null, null).Model.ForwardingHelpText);
    }

    [TestMethod]
    public void ForwardingHelpText_Off()
    {
        var h = new Harness();
        h.Push();

        Assert.AreEqual("已关闭：Codex 配置已还原为开启前的样子。", h.Model.ForwardingHelpText);
    }

    [TestMethod]
    public void ForwardingHelpText_ChatGptNotFound_KeepsExistingText()
    {
        var h = new Harness();
        h.Push(s => s["chatgpt"] = new JsonObject { ["found"] = false, ["error"] = null });

        Assert.AreEqual("找到 ChatGPT 后才能打开。", h.Model.ForwardingHelpText);
    }

    #endregion

    #region 重新检测

    [TestMethod]
    public async Task Recheck_CallsDetectAccessOnly_WhenNothingChanged()
    {
        var h = Pushed("relay", "relay", "not_logged_in");
        Answer(h, "detect_access", "relay", "relay", "not_logged_in");

        await h.Model.RecheckAccessAsync();

        Assert.AreEqual(1, h.Core.Count("detect_access"));
        Assert.AreEqual(0, h.Core.Count("apply_access"));
    }

    [TestMethod]
    public async Task Recheck_IsDetectingWhileInFlight()
    {
        var h = Pushed("both", "both", "logged_in");
        var gate = new TaskCompletionSource<System.Text.Json.JsonElement>();
        h.Core.OnAsync("detect_access", _ => gate.Task);

        Assert.IsFalse(h.Model.IsDetectingAccess);
        var task = h.Model.RecheckAccessAsync();
        Assert.IsTrue(h.Model.IsDetectingAccess);

        gate.SetResult(Status("both", "both", "logged_in"));
        await task;
        Assert.IsFalse(h.Model.IsDetectingAccess);
    }

    [TestMethod]
    public async Task Recheck_UnchangedRelay_InfoNoticeWithKeepRelayText()
    {
        var h = Pushed("relay", "relay", "not_logged_in");
        Answer(h, "detect_access", "relay", "relay", "not_logged_in");

        await h.Model.RecheckAccessAsync();

        var notice = h.Model.Notices.Single();
        Assert.AreEqual(NoChangeTitle, notice.Title);
        Assert.AreEqual("仍然没有检测到 ChatGPT 登录，继续只用中转模型。", notice.Detail);
        Assert.AreEqual(AppSeverity.Info, notice.Severity);
    }

    [TestMethod]
    public async Task Recheck_UnchangedBoth_InfoNoticeWithKeepBothText()
    {
        var h = Pushed("both", "both", "logged_in");
        Answer(h, "detect_access", "both", "both", "logged_in");

        await h.Model.RecheckAccessAsync();

        var notice = h.Model.Notices.Single();
        Assert.AreEqual(NoChangeTitle, notice.Title);
        Assert.AreEqual("仍然检测到 ChatGPT 登录，继续同时使用官方模型和中转模型。", notice.Detail);
        Assert.AreEqual(AppSeverity.Info, notice.Severity);
    }

    [TestMethod]
    public async Task Recheck_UnchangedInfoNotice_AutoDismissesAfterDelay()
    {
        var h = Pushed("both", "both", "logged_in");
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMilliseconds(100);
        Answer(h, "detect_access", "both", "both", "logged_in");

        await h.Model.RecheckAccessAsync();
        Assert.AreEqual(1, h.Model.Notices.Count);

        await WaitUntilAsync(() => h.Model.Notices.Count == 0);
        Assert.AreEqual(0, h.Model.Notices.Count);
    }

    [TestMethod]
    public async Task Recheck_UnchangedInfoNotice_StaysUntilDelayElapses()
    {
        var h = Pushed("both", "both", "logged_in");
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMilliseconds(800);
        Answer(h, "detect_access", "both", "both", "logged_in");

        await h.Model.RecheckAccessAsync();
        await Task.Delay(200);

        CollectionAssert.AreEqual(new[] { NoChangeTitle }, Titles(h));
    }

    [TestMethod]
    public async Task Recheck_DetectFailed_WarningAndNoApply_EvenWithDifferentTarget()
    {
        foreach (var running in new[] { false, true })
        {
            var h = Pushed("relay", "relay", "not_logged_in");
            h.ChatGpt.Running = running;
            Answer(h, "detect_access", "relay", "both", "failed");

            await h.Model.RecheckAccessAsync();

            var notice = h.Model.Notices.Single();
            Assert.AreEqual(DetectFailedTitle, notice.Title, $"running={running}");
            Assert.AreEqual(DetectFailedDetail, notice.Detail, $"running={running}");
            Assert.AreEqual(AppSeverity.Warning, notice.Severity, $"running={running}");
            Assert.AreEqual(0, h.Core.Count("apply_access"), $"running={running}");
            Assert.AreEqual(0, h.Confirmer.Requests.Count, $"running={running}");
        }
    }

    [TestMethod]
    public async Task Recheck_Changed_ChatGptNotRunning_AppliesDirectlyWithoutConfirmOrNotice()
    {
        var h = Pushed("relay", "relay", "not_logged_in");
        h.ChatGpt.Running = false;
        Answer(h, "detect_access", "relay", "both", "logged_in");
        Answer(h, "apply_access", "both", "both", "logged_in");

        await h.Model.RecheckAccessAsync();

        CollectionAssert.AreEqual(new[] { "call:detect_access", "call:apply_access" }, h.Log);
        Assert.AreEqual(0, h.Confirmer.Requests.Count);
        Assert.AreEqual(0, h.Model.Notices.Count);
        Assert.AreEqual("官方 + 中转", h.Model.AccessModeName);
    }

    [TestMethod]
    public async Task Recheck_ChangedToBoth_ChatGptRunning_SuccessNotice()
    {
        var h = Pushed("relay", "relay", "not_logged_in");
        h.ChatGpt.Running = true;
        Answer(h, "detect_access", "relay", "both", "logged_in");
        Answer(h, "apply_access", "both", "both", "logged_in");

        await h.Model.RecheckAccessAsync();

        CollectionAssert.AreEqual(new[] { "call:detect_access", "call:apply_access" }, h.Log);
        Assert.AreEqual(0, h.Confirmer.Requests.Count);
        var notice = h.Model.Notices.Single();
        Assert.AreEqual(ToBothTitle, notice.Title);
        Assert.AreEqual(ToBothDetail, notice.Detail);
        Assert.AreEqual(AppSeverity.Success, notice.Severity);
    }

    [TestMethod]
    public async Task Recheck_ChangedToRelay_ChatGptRunning_SuccessNotice()
    {
        var h = Pushed("both", "both", "logged_in");
        h.ChatGpt.Running = true;
        Answer(h, "detect_access", "both", "relay", "not_logged_in");
        Answer(h, "apply_access", "relay", "relay", "not_logged_in");

        await h.Model.RecheckAccessAsync();

        CollectionAssert.AreEqual(new[] { "call:detect_access", "call:apply_access" }, h.Log);
        Assert.AreEqual(0, h.Confirmer.Requests.Count);
        var notice = h.Model.Notices.Single();
        Assert.AreEqual(ToRelayTitle, notice.Title);
        Assert.AreEqual(ToRelayDetail, notice.Detail);
        Assert.AreEqual(AppSeverity.Success, notice.Severity);
    }

    [TestMethod]
    public async Task Recheck_ApplyRestoreBlocked_RecordsErrorAndBannerWithoutSuccessNotice()
    {
        foreach (var running in new[] { false, true })
        {
            var h = Pushed("relay", "relay", "not_logged_in");
            h.ChatGpt.Running = running;
            Answer(h, "detect_access", "relay", "both", "logged_in");
            h.Core.Fail("apply_access", "restore_blocked", "openai_base_url");

            await h.Model.RecheckAccessAsync();

            Assert.AreEqual("restore_blocked", h.Model.LastError?.Code, $"running={running}");
            CollectionAssert.AreEqual(new[] { "openai_base_url" }, h.Model.LastError!.Keys.ToArray(), $"running={running}");
            Assert.IsTrue(h.Model.Banners.Any(b => b.Title == RestoreBlockedTitle), $"running={running}");
            Assert.AreEqual(0, h.Model.Notices.Count, $"running={running}");
            Assert.IsFalse(h.Model.IsDetectingAccess, $"running={running}");
        }
    }

    [TestMethod]
    public async Task Recheck_DetectCallFails_ErrorNoticeAndNoApply()
    {
        var h = Pushed("relay", "relay", "not_logged_in");
        h.Core.Fail("detect_access", "internal_error");

        await h.Model.RecheckAccessAsync();

        var notice = h.Model.Notices.Single();
        Assert.AreEqual(RecheckFailedTitle, notice.Title);
        Assert.AreEqual(AppSeverity.Danger, notice.Severity);
        Assert.IsFalse(h.Model.IsDetectingAccess);
        Assert.AreEqual(0, h.Core.Count("apply_access"));
    }

    [TestMethod]
    public async Task Recheck_NextDetectCallSucceeds_RemovesErrorNotice()
    {
        var h = Pushed("relay", "relay", "not_logged_in");
        h.Core.Fail("detect_access", "internal_error");
        await h.Model.RecheckAccessAsync();
        Assert.AreEqual(RecheckFailedTitle, h.Model.Notices.Single().Title);

        Answer(h, "detect_access", "relay", "relay", "not_logged_in");
        await h.Model.RecheckAccessAsync();

        CollectionAssert.AreEqual(new[] { NoChangeTitle }, Titles(h));
    }

    [TestMethod]
    public async Task Recheck_NextDetectReportsFailed_StillRemovesErrorNotice()
    {
        var h = Pushed("relay", "relay", "not_logged_in");
        h.Core.Fail("detect_access", "internal_error");
        await h.Model.RecheckAccessAsync();

        Answer(h, "detect_access", "relay", "relay", "failed");
        await h.Model.RecheckAccessAsync();

        CollectionAssert.AreEqual(new[] { DetectFailedTitle }, Titles(h));
    }

    [TestMethod]
    public async Task Recheck_RemovesOldDetectFailedWarning_WhenDetectSucceeds()
    {
        var h = await StartedAsync();
        h.Push(Access("relay", "relay", "failed"));
        CollectionAssert.AreEqual(new[] { DetectFailedTitle }, Titles(h));

        Answer(h, "detect_access", "relay", "both", "logged_in");
        Answer(h, "apply_access", "both", "both", "logged_in");
        await h.Model.RecheckAccessAsync();

        Assert.IsFalse(Titles(h).Contains(DetectFailedTitle));
        Assert.AreEqual(1, h.Core.Count("apply_access"));
    }

    [TestMethod]
    public async Task Recheck_RemovesOldDetectFailedWarning_EvenWhenDetectCallFails()
    {
        var h = await StartedAsync();
        h.Push(Access("relay", "relay", "failed"));
        h.Core.Fail("detect_access", "internal_error");

        await h.Model.RecheckAccessAsync();

        CollectionAssert.AreEqual(new[] { RecheckFailedTitle }, Titles(h));
    }

    [TestMethod]
    public async Task Recheck_ApplyFailsWithPlainCode_ErrorNoticeWithReason()
    {
        foreach (var running in new[] { false, true })
        {
            var h = Pushed("relay", "relay", "not_logged_in");
            h.ChatGpt.Running = running;
            Answer(h, "detect_access", "relay", "both", "logged_in");
            h.Core.Fail("apply_access", "config_changed");

            await h.Model.RecheckAccessAsync();

            var notice = h.Model.Notices.Single();
            Assert.AreEqual(SwitchFailedTitle, notice.Title, $"running={running}");
            StringAssert.Contains(notice.Detail, Msg("config_changed"), $"running={running}");
            Assert.AreEqual(AppSeverity.Danger, notice.Severity, $"running={running}");
            Assert.IsFalse(h.Model.IsDetectingAccess, $"running={running}");
        }
    }

    [TestMethod]
    public async Task Recheck_ApplyFailsWithConfigInvalidKeys_RecordsErrorAndBannerWithoutNotice()
    {
        var h = Pushed("relay", "relay", "not_logged_in");
        Answer(h, "detect_access", "relay", "both", "logged_in");
        h.Core.Fail("apply_access", "config_invalid", "model_provider");

        await h.Model.RecheckAccessAsync();

        Assert.AreEqual("config_invalid", h.Model.LastError?.Code);
        CollectionAssert.AreEqual(new[] { "model_provider" }, h.Model.LastError!.Keys.ToArray());
        Assert.IsTrue(h.Model.Banners.Any(b => b.Title == Msg("config_invalid")));
        Assert.AreEqual(0, h.Model.Notices.Count);
    }

    [TestMethod]
    public async Task Recheck_SwitchSucceedsAfterPlainFailure_RemovesErrorNotice()
    {
        var h = Pushed("relay", "relay", "not_logged_in");
        Answer(h, "detect_access", "relay", "both", "logged_in");
        h.Core.Fail("apply_access", "config_changed");
        await h.Model.RecheckAccessAsync();
        Assert.AreEqual(SwitchFailedTitle, h.Model.Notices.Single().Title);

        Answer(h, "apply_access", "both", "both", "logged_in");
        await h.Model.RecheckAccessAsync();

        Assert.IsFalse(Titles(h).Contains(SwitchFailedTitle));
        Assert.AreEqual("官方 + 中转", h.Model.AccessModeName);
    }

    [TestMethod]
    public async Task Recheck_SwitchSucceedsAfterRestoreBlocked_ClearsLastErrorAndBanner()
    {
        var h = Pushed("relay", "relay", "not_logged_in");
        Answer(h, "detect_access", "relay", "both", "logged_in");
        h.Core.Fail("apply_access", "restore_blocked", "openai_base_url");
        await h.Model.RecheckAccessAsync();
        Assert.IsTrue(h.Model.Banners.Any(b => b.Title == RestoreBlockedTitle));

        Answer(h, "apply_access", "both", "both", "logged_in");
        await h.Model.RecheckAccessAsync();

        Assert.IsNull(h.Model.LastError);
        Assert.IsFalse(h.Model.Banners.Any(b => b.Title == RestoreBlockedTitle));
    }

    [TestMethod]
    public async Task Recheck_WhileDetecting_FlagIsSetThenCleared()
    {
        var h = Pushed("both", "both", "logged_in");
        await h.Model.StartAsync();
        h.Push(Access("both", "both", "logged_in"));
        var gate = new TaskCompletionSource<System.Text.Json.JsonElement>();
        h.Core.OnAsync("detect_access", _ => gate.Task);
        Assert.IsFalse(h.Model.IsDetectingAccess);

        var task = h.Model.RecheckAccessAsync();
        Assert.IsTrue(h.Model.IsDetectingAccess);

        gate.SetResult(Status("both", "both", "logged_in"));
        await task;
        Assert.IsFalse(h.Model.IsDetectingAccess);
    }

    #endregion

    #region 启动对齐

    [TestMethod]
    public async Task StartupAlign_FirstForwardingStatusMismatch_AppliesOnceAndNotRepeated()
    {
        var h = await StartedAsync();
        Answer(h, "apply_access", "relay", "relay", "not_logged_in");
        var mismatch = Access("both", "relay", "not_logged_in");

        h.Push(mismatch);
        await WaitUntilAsync(() => h.Core.Count("apply_access") == 1);
        h.Push(mismatch);
        h.Push(mismatch);
        h.Push(Access("both", "both", "logged_in"));
        await Task.Delay(100);

        Assert.AreEqual(1, h.Core.Count("apply_access"));
        Assert.AreEqual(0, h.Core.Count("detect_access"));
        Assert.AreEqual(0, h.Confirmer.Requests.Count);
    }

    static Action<JsonObject> Unavailable(bool unsupported) => s =>
    {
        Access("relay", "both", "logged_in")(s);
        s["chatgpt"] = unsupported
            ? new JsonObject { ["found"] = true, ["error"] = "chatgpt_unsupported" }
            : new JsonObject { ["found"] = false, ["error"] = "chatgpt_not_found" };
    };

    [TestMethod]
    public async Task StartupAlign_ChatGptUnavailable_DoesNotApplyUntilRecheckSucceeds()
    {
        foreach (var unsupported in new[] { true, false })
        {
            var h = await StartedAsync();
            Answer(h, "apply_access", "both", "both", "logged_in");
            h.Core.On("status", _ => Status("relay", "both", "logged_in"));

            h.Push(Unavailable(unsupported));
            await Task.Delay(100);
            Assert.AreEqual(0, h.Core.Count("apply_access"), $"unsupported={unsupported}");

            await h.Model.RecheckAsync();
            await WaitUntilAsync(() => h.Core.Count("apply_access") == 1);
            h.Push(Access("relay", "both", "logged_in"));
            await Task.Delay(100);
            Assert.AreEqual(1, h.Core.Count("apply_access"), $"unsupported={unsupported}");
        }
    }

    [TestMethod]
    public async Task StartupAlign_ChatGptRunning_AppliesWithSuccessNotice()
    {
        var h = await StartedAsync(chatGptRunning: true);
        Answer(h, "apply_access", "relay", "relay", "not_logged_in");

        h.Push(Access("both", "relay", "not_logged_in"));
        await WaitUntilAsync(() => h.Model.Notices.Count > 0);

        var notice = h.Model.Notices.Single();
        Assert.AreEqual(ToRelayTitle, notice.Title);
        Assert.AreEqual(ToRelayDetail, notice.Detail);
        Assert.AreEqual(AppSeverity.Success, notice.Severity);
        Assert.AreEqual(1, h.Core.Count("apply_access"));
        Assert.AreEqual(0, h.Confirmer.Requests.Count);
    }

    [TestMethod]
    public async Task StartupAlign_ChatGptNotRunning_AppliesWithoutNotice()
    {
        var h = await StartedAsync(chatGptRunning: false);
        Answer(h, "apply_access", "both", "both", "logged_in");

        h.Push(Access("relay", "both", "logged_in"));
        await WaitUntilAsync(() => h.Core.Count("apply_access") == 1);
        await Task.Delay(100);

        Assert.AreEqual(0, h.Model.Notices.Count);
    }

    [TestMethod]
    public async Task StartupAlign_ApplyRestoreBlocked_RecordsError()
    {
        var h = await StartedAsync();
        h.Core.Fail("apply_access", "restore_blocked", "openai_base_url");

        h.Push(Access("relay", "both", "logged_in"));
        await WaitUntilAsync(() => h.Model.LastError != null);

        Assert.AreEqual("restore_blocked", h.Model.LastError?.Code);
        Assert.AreEqual(0, h.Model.Notices.Count);
    }

    [TestMethod]
    public async Task StartupAlign_DetectFailed_WarnsWithoutApply_EvenWithDifferentTarget()
    {
        foreach (var running in new[] { false, true })
        {
            var h = await StartedAsync(running);

            h.Push(Access("relay", "both", "failed"));
            await WaitUntilAsync(() => h.Model.Notices.Count > 0);
            await Task.Delay(100);

            var notice = h.Model.Notices.Single();
            Assert.AreEqual(DetectFailedTitle, notice.Title, $"running={running}");
            Assert.AreEqual(DetectFailedDetail, notice.Detail, $"running={running}");
            Assert.AreEqual(AppSeverity.Warning, notice.Severity, $"running={running}");
            Assert.AreEqual(0, h.Core.Count("apply_access"), $"running={running}");
        }
    }

    [TestMethod]
    public async Task StartupAlign_DetectFailedSameTarget_AlsoWarns()
    {
        var h = await StartedAsync();

        h.Push(Access("relay", "relay", "failed"));
        await WaitUntilAsync(() => h.Model.Notices.Count > 0);

        CollectionAssert.AreEqual(new[] { DetectFailedTitle }, Titles(h));
        Assert.AreEqual(0, h.Core.Count("apply_access"));
    }

    [TestMethod]
    public async Task StartupAlign_DetectFailed_SameFailedStatusLater_NotWarnedAgain()
    {
        var h = await StartedAsync();
        h.Push(Access("relay", "relay", "failed"));
        await WaitUntilAsync(() => h.Model.Notices.Count > 0);
        h.Model.DismissNotice(h.Model.Notices.Single().Id);

        h.Push(Access("relay", "relay", "failed"));
        h.Push(Access("relay", "both", "failed"));
        await Task.Delay(100);

        Assert.AreEqual(0, h.Model.Notices.Count);
        Assert.AreEqual(0, h.Core.Count("apply_access"));
    }

    [TestMethod]
    public async Task StartupAlign_DetectFailed_LaterMismatchStatusDoesNotApply()
    {
        var h = await StartedAsync();
        h.Push(Access("relay", "relay", "failed"));

        h.Push(Access("relay", "both", "logged_in"));
        await Task.Delay(100);

        Assert.AreEqual(0, h.Core.Count("apply_access"));
    }

    [TestMethod]
    public async Task StartupAlign_ForwardingOffOrTargetNull_DoesNothing()
    {
        var h = await StartedAsync();

        h.Push(Access("both", "relay", "not_logged_in", forwarding: false));
        h.Push(Access("both", null, null));
        await Task.Delay(100);

        Assert.AreEqual(0, h.Core.Count("apply_access"));
        Assert.AreEqual(0, h.Model.Notices.Count);
    }

    [TestMethod]
    public async Task StartupAlign_ForwardingOffStatusDoesNotUseUpTheOneShot()
    {
        var h = await StartedAsync();
        Answer(h, "apply_access", "relay", "relay", "not_logged_in");
        h.Push(Access("both", "relay", "not_logged_in", forwarding: false));

        h.Push(Access("both", "relay", "not_logged_in"));
        await WaitUntilAsync(() => h.Core.Count("apply_access") == 1);

        Assert.AreEqual(1, h.Core.Count("apply_access"));
    }

    [TestMethod]
    public async Task StartupAlign_FirstForwardingStatusAligned_LaterMismatchDoesNotApply()
    {
        var h = await StartedAsync();
        h.Push(Access("both", "both", "logged_in"));

        h.Push(Access("both", "relay", "not_logged_in"));
        await Task.Delay(100);

        Assert.AreEqual(0, h.Core.Count("apply_access"));
    }

    #endregion

    #region 首次设置完成页

    static async Task<string> SetupSuccessTextAsync(Action<JsonObject> tweak)
    {
        var h = new Harness();
        h.FillInputs();
        h.Core.On("set_forwarding", _ => Json.Status(tweak));
        await h.Model.CompleteSetupAsync();
        return h.Model.SuccessText;
    }

    [TestMethod]
    public async Task SetupSuccess_Relay_TextNamesRelayOnlyAccess()
    {
        var text = await SetupSuccessTextAsync(Access("relay", "relay", "not_logged_in"));

        Assert.AreEqual(
            "已获取 6 个中转模型。没有检测到 ChatGPT 登录，接入方式为「只用中转」：Codex 不需要登录，模型列表里只有中转模型。" + SuccessTail,
            text);
    }

    [TestMethod]
    public async Task SetupSuccess_Both_TextNamesOfficialPlusRelayAccess()
    {
        var text = await SetupSuccessTextAsync(Access("both", "both", "logged_in"));

        Assert.AreEqual("已获取 6 个中转模型。已检测到 ChatGPT 登录，接入方式为「官方 + 中转」。" + SuccessTail, text);
    }

    [TestMethod]
    public async Task SetupSuccess_ModeNull_KeepsTextWithoutAccess()
    {
        var text = await SetupSuccessTextAsync(Access(null, null, null));

        Assert.AreEqual("已获取 6 个中转模型。" + SuccessTail, text);
    }

    [TestMethod]
    public async Task SetupSuccess_StatusWithoutAccessKey_KeepsTextWithoutAccess()
    {
        var text = await SetupSuccessTextAsync(s => s["forwarding"] = true);

        Assert.AreEqual("已获取 6 个中转模型。" + SuccessTail, text);
    }

    #endregion
}
