using System.Text.Json;
using System.Text.Json.Nodes;
using System.Windows;
using System.Windows.Controls;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.UI;

namespace ModelBridge.Tests;

[TestClass]
public class AccountModelNoticeTests
{
    const string UpdatedTitle = "官方模型已按当前账号更新";
    const string RestartDetail = "重启 ChatGPT 后生效。";
    const string SavedTitle = "已保存「中转」";

    static async Task<MultiHarness> StartedAsync(int? startRevision = null)
    {
        var h = new MultiHarness();
        h.Seed("1", "中转", "https://relay1.example.com/v1", "sk-example-0001");
        if (startRevision is { } r) h.Core.OnAsync("status", _ => Task.FromResult(Status(h, r)));
        await h.Model.StartAsync();
        return h;
    }

    static JsonElement Status(MultiHarness h, int? revision, bool forwarding = true, string mode = "both")
    {
        var status = JsonNode.Parse(RelayJson.Status(forwarding, h.Core.Entries.ToArray(), "中转", mode).GetRawText())!;
        if (revision is { } r) status["official_models_revision"] = r;
        return RelayJson.Parse(status.ToJsonString());
    }

    static void Push(MultiHarness h, int? revision, bool forwarding = true, string mode = "both") =>
        h.Core.RaiseStatus(Status(h, revision, forwarding, mode));

    static string[] Titles(MultiHarness h) => h.Model.Notices.Select(n => n.Title).ToArray();

    static async Task SaveRelayAsync(MultiHarness h)
    {
        var editor = Editors.Edit(h.Model, "1");
        editor.ApiKeyInput = "sk-example-0009";
        Assert.IsTrue(await editor.SaveAsync());
    }

    static async Task WaitUntilAsync(Func<bool> condition)
    {
        var deadline = DateTime.UtcNow.AddSeconds(5);
        while (!condition() && DateTime.UtcNow < deadline) await Task.Delay(20);
    }

    static Rect RectIn(Window window, FrameworkElement e) =>
        new(UiHost.PositionIn(window, e), new System.Windows.Size(e.ActualWidth, e.ActualHeight));

    static FrameworkElement AlertOf(Window window, string title)
    {
        var text = UiHost.FindText(window, title);
        Assert.IsNotNull(text, title);
        return UiHost.Ancestors(text).OfType<Border>().First();
    }

    static double TitleBarBottom(Window window)
    {
        var title = UiHost.FindText(window, "模型接入助手");
        Assert.IsNotNull(title);
        var bar = UiHost.Ancestors(title).First(a => Math.Abs(a.ActualHeight - 32) < 0.5);
        return UiHost.PositionIn(window, bar).Y + bar.ActualHeight;
    }

    static bool IsClose(Button b) => System.Windows.Automation.AutomationProperties.GetName(b) == "关闭提示";

    [TestMethod]
    public async Task FirstStatus_OnlyRecordsRevision_NoNotice()
    {
        var h = await StartedAsync(5);
        Assert.AreEqual(0, h.Model.Notices.Count);

        Push(h, 6);

        Assert.AreEqual(UpdatedTitle, h.Model.Notices.Single().Title);
    }

    [TestMethod]
    public async Task FirstStatusWithoutField_ThenRevision1_ShowsNotice()
    {
        var h = await StartedAsync();
        Push(h, null);
        Assert.AreEqual(0, h.Model.Notices.Count);

        Push(h, 1);

        Assert.AreEqual(UpdatedTitle, h.Model.Notices.Single().Title);
    }

    [TestMethod]
    public async Task RevisionRises_ShowsSuccessNoticeWithTitleAndDetail()
    {
        var h = await StartedAsync();
        Push(h, 0);

        Push(h, 1);

        var notice = h.Model.Notices.Single();
        Assert.AreEqual(UpdatedTitle, notice.Title);
        Assert.AreEqual(RestartDetail, notice.Detail);
        Assert.AreEqual(AppSeverity.Success, notice.Severity);
    }

    [TestMethod]
    public async Task RevisionUnchanged_NoNewNotice()
    {
        var h = await StartedAsync();
        Push(h, 0);
        Push(h, 1);
        h.Model.DismissNotice(h.Model.Notices.Single().Id);

        Push(h, 1);

        Assert.AreEqual(0, h.Model.Notices.Count);
    }

    [TestMethod]
    public async Task RevisionDrops_KeepsRecord_OnlyRiseAboveHistoricMaxNotifies()
    {
        var h = await StartedAsync(5);

        Push(h, 2);
        Push(h, 3);
        Push(h, 5);
        Assert.AreEqual(0, h.Model.Notices.Count);

        Push(h, 6);
        Assert.AreEqual(UpdatedTitle, h.Model.Notices.Single().Title);
    }

    [TestMethod]
    public async Task LateLowerRevision_AfterDismiss_RiseBackToSeenValue_NoNewNotice()
    {
        var h = await StartedAsync();
        Push(h, 0);
        Push(h, 1);
        h.Model.DismissNotice(h.Model.Notices.Single().Id);

        Push(h, 0);
        Push(h, 1);

        Assert.AreEqual(0, h.Model.Notices.Count);
    }

    [TestMethod]
    public async Task CoreRestarted_FirstStatusOnlyRecords_ThenRiseFromZeroNotifies()
    {
        var h = await StartedAsync(5);
        Push(h, 7);
        h.Model.DismissNotice(h.Model.Notices.Single().Id);

        h.Core.OnAsync("status", _ => Task.FromResult(Status(h, 0)));
        await h.Model.StartAsync();
        Assert.AreEqual(0, h.Model.Notices.Count);

        Push(h, 1);
        Assert.AreEqual(UpdatedTitle, h.Model.Notices.Single().Title);
    }

    [TestMethod]
    public async Task StatusWithoutField_CountsAsZero()
    {
        var h = await StartedAsync(3);

        Push(h, null);
        Push(h, 1);
        Assert.AreEqual(0, h.Model.Notices.Count);

        Push(h, 4);
        Assert.AreEqual(UpdatedTitle, h.Model.Notices.Single().Title);
    }

    [TestMethod]
    public async Task Notice_DoesNotAutoDismiss_OtherSuccessNoticeDoes()
    {
        var h = await StartedAsync();
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMilliseconds(50);
        Push(h, 0);
        Push(h, 1);
        await SaveRelayAsync(h);
        CollectionAssert.AreEqual(new[] { UpdatedTitle, SavedTitle }, Titles(h));

        await WaitUntilAsync(() => h.Model.Notices.Count == 1);
        await Task.Delay(300);

        CollectionAssert.AreEqual(new[] { UpdatedTitle }, Titles(h));
    }

    [TestMethod]
    public async Task Notice_RemovedWhenDismissed()
    {
        var h = await StartedAsync();
        Push(h, 0);
        Push(h, 1);

        h.Model.DismissNotice(h.Model.Notices.Single().Id);

        Assert.AreEqual(0, h.Model.Notices.Count);
    }

    [TestMethod]
    public async Task RevisionRisesAgain_ReplacesInPlace_StillOneNotice()
    {
        var h = await StartedAsync();
        Push(h, 0);
        Push(h, 1);
        var first = h.Model.Notices.Single();

        Push(h, 2);

        var second = h.Model.Notices.Single();
        Assert.AreEqual(UpdatedTitle, second.Title);
        Assert.AreNotEqual(first.Id, second.Id);
    }

    [TestMethod]
    public async Task RevisionRisesAgain_AmongOthers_KeepsIndex()
    {
        var h = await StartedAsync();
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMinutes(5);
        Push(h, 0);
        Push(h, 1);
        await SaveRelayAsync(h);
        CollectionAssert.AreEqual(new[] { UpdatedTitle, SavedTitle }, Titles(h));

        Push(h, 2);

        CollectionAssert.AreEqual(new[] { UpdatedTitle, SavedTitle }, Titles(h));
    }

    [TestMethod]
    public async Task ForwardingTurnsOff_RemovesNotice_KeepsOthers()
    {
        var h = await StartedAsync();
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMinutes(5);
        Push(h, 0);
        Push(h, 1);
        await SaveRelayAsync(h);

        Push(h, 1, forwarding: false);

        CollectionAssert.AreEqual(new[] { SavedTitle }, Titles(h));
    }

    [TestMethod]
    public async Task CoreAutoRestarted_FirstStatusOnlyRecords_ThenRiseFromZeroNotifies()
    {
        var h = await StartedAsync(5);
        Push(h, 7);
        h.Model.DismissNotice(h.Model.Notices.Single().Id);

        h.Core.RaiseRestarted();
        Push(h, 0);
        Assert.AreEqual(0, h.Model.Notices.Count);

        Push(h, 1);
        Assert.AreEqual(UpdatedTitle, h.Model.Notices.Single().Title);
    }

    [TestMethod]
    public async Task AccessModeBecomesRelay_WhileForwardingOn_RemovesNotice_KeepsOthers()
    {
        var h = await StartedAsync();
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMinutes(5);
        Push(h, 0);
        Push(h, 1);
        await SaveRelayAsync(h);

        Push(h, 1, mode: "relay");

        CollectionAssert.AreEqual(new[] { SavedTitle }, Titles(h));
    }

    [TestMethod]
    public async Task SaveRelayThenNotice_OrderFollowsAppearance_NoticeAfterSaved()
    {
        var h = await StartedAsync();
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMinutes(5);
        Push(h, 0);
        await SaveRelayAsync(h);

        Push(h, 1);

        CollectionAssert.AreEqual(new[] { SavedTitle, UpdatedTitle }, Titles(h));
    }

    [TestMethod]
    public async Task Window_Notice_At24Left_376Wide_12BelowTitleBar_OnlyCloseButton()
    {
        var h = await StartedAsync();
        h.ChatGpt.Running = true;
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMinutes(5);
        Push(h, 0);

        await UiHost.RunAsync(async () =>
        {
            var window = new MainWindow(h.Model);
            try
            {
                await UiHost.ShowAsync(window);
                Push(h, 1);
                await UiHost.Settle();

                var alert = AlertOf(window, UpdatedTitle);
                Assert.IsNotNull(UiHost.FindText(window, RestartDetail));
                Assert.AreEqual(376, alert.ActualWidth, 0.5);
                Assert.AreEqual(24, RectIn(window, alert).Left, 0.5);
                Assert.AreEqual(TitleBarBottom(window) + 12, RectIn(window, alert).Top, 0.5);
                var buttons = UiHost.All<Button>(alert).ToList();
                Assert.AreEqual(1, buttons.Count);
                Assert.IsTrue(IsClose(buttons[0]));
            }
            finally
            {
                window.AllowClose = true;
                window.Close();
            }
        });
    }

    [TestMethod]
    public async Task Window_Notice_DoesNotCoverSwitchesAndButtons()
    {
        var h = await StartedAsync();
        h.ChatGpt.Running = true;
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMinutes(5);
        Push(h, 0);

        await UiHost.RunAsync(async () =>
        {
            var window = new MainWindow(h.Model);
            try
            {
                await UiHost.ShowAsync(window);
                Push(h, 1);
                await UiHost.Settle();

                var alert = AlertOf(window, UpdatedTitle);
                var a = RectIn(window, alert);
                var targets = new (string, FrameworkElement)[]
                {
                    ("转发开关", window.ForwardToggle),
                    ("只用中转开关", window.RelayOnlyToggle),
                    ("重新检测", window.RecheckAccessButton),
                    ("添加配置", window.AddRelayButton),
                    ("检查更新", window.UpdateButton),
                };
                var checkedCount = 0;
                foreach (var (name, element) in targets.Where(t => t.Item2.IsVisible))
                {
                    checkedCount++;
                    var r = RectIn(window, element);
                    var w = Math.Min(a.Right, r.Right) - Math.Max(a.Left, r.Left);
                    var hgt = Math.Min(a.Bottom, r.Bottom) - Math.Max(a.Top, r.Top);
                    Assert.IsFalse(w > 0.5 && hgt > 0.5, $"提示条 {a} 压住 {name} {r}");
                }
                Assert.IsTrue(checkedCount >= 1);
            }
            finally
            {
                window.AllowClose = true;
                window.Close();
            }
        });
    }

    [TestMethod]
    public async Task Window_StackedWithSaved_NoticeOnTop_8Apart_PositionKeptAfterSavedDismissed()
    {
        var h = await StartedAsync();
        h.ChatGpt.Running = true;
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromSeconds(3);
        Push(h, 0);

        await UiHost.RunAsync(async () =>
        {
            var window = new MainWindow(h.Model);
            try
            {
                await UiHost.ShowAsync(window);
                Push(h, 1);
                await SaveRelayAsync(h);
                await UiHost.Settle();

                var upper = RectIn(window, AlertOf(window, UpdatedTitle));
                var lower = RectIn(window, AlertOf(window, SavedTitle));
                Assert.AreEqual(upper.Left, lower.Left, 0.5);
                Assert.AreEqual(upper.Bottom + 8, lower.Top, 0.5);

                await WaitUntilAsync(() => h.Model.Notices.Count == 1);
                await UiHost.Settle();

                Assert.IsNull(UiHost.FindText(window, SavedTitle));
                var after = RectIn(window, AlertOf(window, UpdatedTitle));
                Assert.AreEqual(upper.Top, after.Top, 0.5);
                Assert.AreEqual(upper.Left, after.Left, 0.5);
            }
            finally
            {
                window.AllowClose = true;
                window.Close();
            }
        });
    }

    [TestMethod]
    public async Task Window_CloseButton_RemovesNotice()
    {
        var h = await StartedAsync();
        h.ChatGpt.Running = true;
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMinutes(5);
        Push(h, 0);

        await UiHost.RunAsync(async () =>
        {
            var window = new MainWindow(h.Model);
            try
            {
                await UiHost.ShowAsync(window);
                Push(h, 1);
                await UiHost.Settle();

                UiHost.All<Button>(AlertOf(window, UpdatedTitle)).First(IsClose)
                    .RaiseEvent(new RoutedEventArgs(System.Windows.Controls.Primitives.ButtonBase.ClickEvent));
                await UiHost.Settle();

                Assert.IsNull(UiHost.FindText(window, UpdatedTitle));
                Assert.AreEqual(0, h.Model.Notices.Count);
            }
            finally
            {
                window.AllowClose = true;
                window.Close();
            }
        });
    }
}
