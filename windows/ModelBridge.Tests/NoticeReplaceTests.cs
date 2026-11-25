using System.Diagnostics;
using System.Text.Json.Nodes;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.UI;

namespace ModelBridge.Tests;

[TestClass]
public class NoticeReplaceTests
{
    const string SwitchFailedTitle = "没能切换接入方式";
    const string DetectFailedTitle = "没能检测 ChatGPT 登录状态";
    const string NoChangeTitle = "接入方式没有变化";

    static Action<JsonObject> Access(string? mode, string? target, string? detect, bool forwarding = true) => s =>
    {
        s["forwarding"] = forwarding;
        s["access"] = new JsonObject { ["mode"] = mode, ["target"] = target, ["detect"] = detect };
    };

    static void Answer(Harness h, string method, string? mode, string? target, string? detect, bool forwarding = true) =>
        h.Core.On(method, _ => Json.Status(Access(mode, target, detect, forwarding)));

    static string Msg(string code) => ModelBridge.Services.CoreErrorMessages.Messages[code];

    static string[] Titles(Harness h) => h.Model.Notices.Select(n => n.Title).ToArray();

    static async Task WaitUntilAsync(Func<bool> condition)
    {
        var deadline = DateTime.UtcNow.AddSeconds(5);
        while (!condition() && DateTime.UtcNow < deadline) await Task.Delay(20);
    }

    static Harness FailingSwitch()
    {
        var h = new Harness();
        h.Push(Access("relay", "relay", "not_logged_in"));
        Answer(h, "detect_access", "relay", "both", "logged_in");
        h.Core.Fail("apply_access", "config_changed");
        return h;
    }

    [TestMethod]
    public async Task SwitchFailsTwice_StillOneErrorNoticeWithNewReason()
    {
        var h = FailingSwitch();
        await h.Model.RecheckAccessAsync();
        StringAssert.Contains(h.Model.Notices.Single().Detail, Msg("config_changed"));

        h.Core.Fail("apply_access", "internal_error");
        await h.Model.RecheckAccessAsync();

        var notice = h.Model.Notices.Single();
        Assert.AreEqual(SwitchFailedTitle, notice.Title);
        Assert.AreEqual(AppSeverity.Danger, notice.Severity);
        StringAssert.Contains(notice.Detail, Msg("internal_error"));
        Assert.IsFalse(notice.Detail.Contains(Msg("config_changed")));
    }

    [TestMethod]
    public async Task SwitchFailsThreeTimes_StillOneErrorNotice()
    {
        var h = FailingSwitch();
        await h.Model.RecheckAccessAsync();
        h.Core.Fail("apply_access", "internal_error");
        await h.Model.RecheckAccessAsync();
        h.Core.Fail("apply_access", "config_changed");
        await h.Model.RecheckAccessAsync();

        var notice = h.Model.Notices.Single();
        StringAssert.Contains(notice.Detail, Msg("config_changed"));
    }

    [TestMethod]
    public async Task SwitchFailsAgain_ErrorKeepsIndexAndWarningUntouched()
    {
        var h = new Harness();
        h.Push(Access("relay", "relay", "not_logged_in", forwarding: false));
        Answer(h, "detect_access", "relay", "both", "logged_in");
        h.Core.Fail("apply_access", "config_changed");
        await h.Model.RecheckAccessAsync();
        CollectionAssert.AreEqual(new[] { SwitchFailedTitle }, Titles(h));

        h.Core.On("detect_access", _ =>
        {
            h.Push(Access("relay", "both", "failed"));
            return Json.Status(Access("relay", "both", "logged_in"));
        });
        h.Core.Fail("apply_access", "internal_error");
        await h.Model.RecheckAccessAsync();

        CollectionAssert.AreEqual(new[] { SwitchFailedTitle, DetectFailedTitle }, Titles(h));
        StringAssert.Contains(h.Model.Notices[0].Detail, Msg("internal_error"));
        Assert.IsFalse(h.Model.Notices[0].Detail.Contains(Msg("config_changed")));
        Assert.AreEqual(AppSeverity.Warning, h.Model.Notices[1].Severity);
        Assert.AreEqual("沿用上次的方式。可以稍后点「重新检测」再试。", h.Model.Notices[1].Detail);
    }

    [TestMethod]
    public async Task ReplacedErrorNotice_StillDoesNotAutoDismiss()
    {
        var h = FailingSwitch();
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMilliseconds(50);
        await h.Model.RecheckAccessAsync();
        h.Core.Fail("apply_access", "internal_error");
        await h.Model.RecheckAccessAsync();

        await Task.Delay(300);

        Assert.AreEqual(1, h.Model.Notices.Count);
    }

    [TestMethod]
    public async Task InfoNotice_ReplacedInPlaceWithNewDetail()
    {
        var h = new Harness();
        h.Push(Access("both", "both", "logged_in"));
        Answer(h, "detect_access", "both", "both", "logged_in");
        await h.Model.RecheckAccessAsync();
        var first = h.Model.Notices.Single().Detail;

        Answer(h, "detect_access", "relay", "relay", "not_logged_in");
        await h.Model.RecheckAccessAsync();

        var notice = h.Model.Notices.Single();
        Assert.AreEqual(NoChangeTitle, notice.Title);
        Assert.AreEqual(AppSeverity.Info, notice.Severity);
        Assert.AreNotEqual(first, notice.Detail);
        Assert.AreEqual("仍然没有检测到 ChatGPT 登录，继续只用中转模型。", notice.Detail);
    }

    [TestMethod]
    public async Task InfoNotice_ReplacedRestartsAutoDismissClock()
    {
        var h = new Harness();
        h.Push(Access("both", "both", "logged_in"));
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMilliseconds(600);
        Answer(h, "detect_access", "both", "both", "logged_in");
        await h.Model.RecheckAccessAsync();
        await Task.Delay(400);

        Answer(h, "detect_access", "relay", "relay", "not_logged_in");
        var replacedAt = Stopwatch.StartNew();
        await h.Model.RecheckAccessAsync();
        await Task.Delay(300);

        Assert.AreEqual(1, h.Model.Notices.Count, "旧计时到点不能删掉新提示");
        Assert.AreEqual("仍然没有检测到 ChatGPT 登录，继续只用中转模型。", h.Model.Notices[0].Detail);

        await WaitUntilAsync(() => h.Model.Notices.Count == 0);
        Assert.AreEqual(0, h.Model.Notices.Count);
        Assert.IsTrue(replacedAt.ElapsedMilliseconds >= 550, $"新计时应从替换时算起，实际 {replacedAt.ElapsedMilliseconds}ms");
    }

    [TestMethod]
    public async Task InfoNotice_ReplacedAmongOthers_KeepsIndex()
    {
        var h = new Harness();
        h.Push(Access("both", "both", "logged_in"));
        Answer(h, "detect_access", "both", "both", "logged_in");
        await h.Model.RecheckAccessAsync();
        h.Core.Fail("apply_access", "config_changed");
        h.Push(Access("relay", "both", "logged_in"));
        Answer(h, "detect_access", "relay", "both", "logged_in");
        await h.Model.RecheckAccessAsync();
        CollectionAssert.AreEqual(new[] { NoChangeTitle, SwitchFailedTitle }, Titles(h));

        Answer(h, "detect_access", "relay", "relay", "not_logged_in");
        await h.Model.RecheckAccessAsync();

        CollectionAssert.AreEqual(new[] { NoChangeTitle, SwitchFailedTitle }, Titles(h));
        Assert.AreEqual("仍然没有检测到 ChatGPT 登录，继续只用中转模型。", h.Model.Notices[0].Detail);
    }

    [TestMethod]
    public async Task SettingsUnreadableTwice_ReplacedNoticeHasNewPathAndStillOpensInDialog()
    {
        var h = new Harness();
        h.Settings.ReadException = new ModelBridge.Services.SettingsUnreadableException(@"C:\old\settings.json", new IOException());
        await h.Model.StartAsync();
        Assert.IsTrue(h.Model.Notices.Single().DetailInDialog);

        h.Settings.ReadException = new ModelBridge.Services.SettingsUnreadableException(@"C:\new\settings.json", new IOException());
        await h.Model.StartAsync();

        var notice = h.Model.Notices.Single();
        StringAssert.Contains(notice.Detail, @"C:\new\settings.json");
        Assert.IsFalse(notice.Detail.Contains(@"C:\old\settings.json"));
        Assert.IsTrue(notice.DetailInDialog);
    }

    [TestMethod]
    public async Task ErrorNoticeWithoutDialogDetail_ReplacedKeepsNoDialogFlag()
    {
        var h = FailingSwitch();
        await h.Model.RecheckAccessAsync();
        h.Core.Fail("apply_access", "internal_error");
        await h.Model.RecheckAccessAsync();

        Assert.IsFalse(h.Model.Notices.Single().DetailInDialog);
    }
}
