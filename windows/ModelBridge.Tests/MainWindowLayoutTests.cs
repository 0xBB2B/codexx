using Button = System.Windows.Controls.Button;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.UI;

namespace ModelBridge.Tests;

[TestClass]
public class MainWindowLayoutTests
{
    static async Task<MultiHarness> StartAsync(int relays, bool forwarding = true, bool firstStale = false)
    {
        var h = new MultiHarness();
        for (var i = 1; i <= relays; i++)
            h.Seed(i.ToString(), i == 1 ? "中转" : $"配置{i}", $"https://relay{i}.example.com/v1", $"sk-example-{i:0000}");
        if (firstStale) h.Core.Entries[0] = h.Core.Entries[0] with { Stale = true, Error = "timeout" };
        await h.Model.StartAsync();
        h.Push(forwarding);
        return h;
    }

    static async Task WithWindowAsync(MultiHarness h, Func<MainWindow, Task> check)
    {
        await UiHost.RunAsync(async () =>
        {
            var window = new MainWindow(h.Model);
            try
            {
                await UiHost.ShowAsync(window);
                await check(window);
            }
            finally
            {
                window.AllowClose = true;
                window.Close();
            }
        });
    }

    [TestMethod]
    public async Task Window_WidthIs620_AndThreeRelaysHeightAbout633()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, window =>
        {
            Assert.AreEqual(620, window.ActualWidth, 0.5);
            Assert.IsTrue(window.ActualHeight is >= 621 and <= 645, "height=" + window.ActualHeight);
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task Window_NoRunStatusOrRelayConnectionCards()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, window =>
        {
            var text = UiHost.Joined(window);
            Assert.IsFalse(text.Contains("运行状态"), text);
            Assert.IsFalse(text.Contains("中转连接"), text);
            Assert.IsNotNull(UiHost.FindText(window, "中转配置"));
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task Window_MoreThanThreeRelays_HeightUnchanged_ListScrolls()
    {
        var three = await StartAsync(3);
        double heightOfThree = 0;
        await WithWindowAsync(three, w => { heightOfThree = w.ActualHeight; return Task.CompletedTask; });

        var five = await StartAsync(5);
        await WithWindowAsync(five, window =>
        {
            Assert.AreEqual(heightOfThree, window.ActualHeight, 1);
            Assert.IsTrue(UiHost.All<ScrollViewer>(window).Any(s => s.ScrollableHeight > 0));
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task TitleBar_Height32_Icon16_TitleLeftAligned()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, window =>
        {
            var title = UiHost.FindText(window, "模型接入助手");
            Assert.IsNotNull(title);
            var titlePos = UiHost.PositionIn(window, title);
            Assert.IsTrue(titlePos.Y >= 0 && titlePos.Y + title.ActualHeight <= 32.5, "title y=" + titlePos.Y);
            Assert.IsTrue(titlePos.X < window.ActualWidth / 2);

            var icon = UiHost.Sized(window, 16, 16).FirstOrDefault(e => UiHost.PositionIn(window, e).X < titlePos.X && UiHost.PositionIn(window, e).Y < 32);
            Assert.IsNotNull(icon, "16px app icon before title");
            Assert.IsTrue(UiHost.All<FrameworkElement>(window).Any(e => e.IsVisible && Math.Abs(e.ActualHeight - 32) < 0.5 && UiHost.PositionIn(window, e).Y < 0.5), "32px title bar");
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task Overview_On_ShowsTitleIconPrimaryBorderAndAccessRow()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, window =>
        {
            Assert.IsNotNull(UiHost.FindText(window, "使用中转 · 已开启"));
            Assert.IsTrue(UiHost.Sized(window, 48, 48).Any(), "48px app icon");
            var primary = UiHost.ThemeBrush("PrimaryBrush").Color;
            var tint = UiHost.ThemeBrush("SelectedBgBrush").Color;
            Assert.IsTrue(UiHost.All<Border>(window).Any(b =>
                b.IsVisible && b.BorderBrush is SolidColorBrush bb && bb.Color == primary
                && b.Background is SolidColorBrush bg && bg.Color == tint), "light green fill with primary border");
            Assert.IsNotNull(UiHost.FindText(window, "接入方式"));
            Assert.IsNotNull(UiHost.FindText(window, h.Model.AccessModeName!));
            StringAssert.Contains(UiHost.Joined(window), "已检测到 ChatGPT 登录");
            Assert.IsTrue(UiHost.FindButton(window, "重新检测")!.IsEnabled);
            Assert.IsTrue(UiHost.All<System.Windows.Controls.Primitives.ToggleButton>(window).Any(t => t.IsVisible));
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task Overview_Off_ShowsClosedTitle_NoAccessRow()
    {
        var h = await StartAsync(3, forwarding: false);

        await WithWindowAsync(h, window =>
        {
            Assert.IsNotNull(UiHost.FindText(window, "使用中转 · 已关闭"));
            Assert.IsNull(UiHost.FindText(window, "接入方式"));
            Assert.IsNull(UiHost.FindButton(window, "重新检测"));
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task Overview_Detecting_ShowsDetectingText_ButtonDisabled()
    {
        var h = await StartAsync(3);
        var gate = new TaskCompletionSource<System.Text.Json.JsonElement>();
        h.Core.OnAsync("detect_access", _ => gate.Task);

        await WithWindowAsync(h, async window =>
        {
            var detecting = h.Model.RecheckAccessAsync();
            await UiHost.Settle();

            var button = UiHost.FindButton(window, "正在检测…");
            Assert.IsNotNull(button);
            Assert.IsFalse(button.IsEnabled);

            gate.SetResult(RelayJson.Status(true, h.Core.Entries.ToArray()));
            await detecting;
        });
    }

    [TestMethod]
    public async Task RelayCard_Rows64High_NameBold_StatusAndBaseUrl_Buttons()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, window =>
        {
            var edits = UiHost.Buttons(window, "编辑");
            Assert.AreEqual(3, edits.Count);
            Assert.AreEqual(3, UiHost.Buttons(window, "删除").Count);
            Assert.IsTrue(UiHost.Ancestors(edits[0]).Any(a => Math.Abs(a.ActualHeight - 64) < 0.5), "row height 64");

            var name = UiHost.FindText(window, "中转");
            Assert.IsNotNull(name);
            Assert.IsTrue(name.FontWeight.ToOpenTypeWeight() >= 600);
            var joined = UiHost.Joined(window);
            StringAssert.Contains(joined, "https://relay1.example.com/v1");
            StringAssert.Contains(joined, h.Model.Relays[0].StatusText);
            Assert.IsNull(UiHost.FindButton(window, "重试"));
            Assert.IsNotNull(UiHost.FindButton(window, "添加配置"));
            StringAssert.Contains(joined, h.Model.RelayCountText);
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task RelayCard_FailedRow_HasRetryButton()
    {
        var h = await StartAsync(3, firstStale: true);

        await WithWindowAsync(h, window =>
        {
            Assert.IsTrue(h.Model.Relays[0].CanRetry);
            Assert.IsNotNull(UiHost.FindButton(window, "重试"));
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task StatusRow_ShowsVersionCoreChatGptRelayModelsAndUpdate()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, window =>
        {
            var joined = UiHost.Joined(window);
            StringAssert.Contains(joined, h.Model.Version);
            StringAssert.Contains(joined, "后台运行中");
            StringAssert.Contains(joined, "ChatGPT");
            StringAssert.Contains(joined, "已检测到");
            StringAssert.Contains(joined, "中转模型");
            StringAssert.Contains(joined, "9 个");
            StringAssert.Contains(joined, "（3 个配置）");
            Assert.IsNotNull(UiHost.FindButton(window, "检查更新"));
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task Notice_FloatsOverWindow_HeightUnchanged()
    {
        var h = await StartAsync(3);
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMinutes(5);

        await WithWindowAsync(h, async window =>
        {
            var before = window.ActualHeight;

            await h.Model.CheckUpdateAsync();
            await UiHost.Settle();

            Assert.IsNotNull(UiHost.FindText(window, "已是最新版本"));
            Assert.AreEqual(before, window.ActualHeight, 0.5);
        });
    }

    [TestMethod]
    public async Task StatusRow_ConfigCountIsMutedRegularSupplement()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, window =>
        {
            var muted = UiHost.ThemeBrush("TextMutedBrush").Color;
            var pieces = UiHost.All<TextBlock>(window)
                .SelectMany(t => t.Inlines.OfType<System.Windows.Documents.Run>().Select(r => (r.Text, r.Foreground, r.FontWeight))
                    .Append((t.Text, t.Foreground, t.FontWeight)))
                .Where(p => p.Text.Trim() == "（3 个配置）")
                .ToList();
            Assert.IsTrue(pieces.Count > 0, "separate supplement text");
            Assert.IsTrue(pieces.All(p => p.Foreground is SolidColorBrush b && b.Color == muted && p.FontWeight.ToOpenTypeWeight() <= 400));
            return Task.CompletedTask;
        });
    }

    [System.Runtime.InteropServices.DllImport("user32.dll")]
    static extern bool IsWindowEnabled(IntPtr hWnd);

    static async Task<RelayFormWindow> OpenFormAsync(MainWindow window, string label)
    {
        var button = UiHost.Buttons(window, label)[0];
        _ = System.Windows.Threading.Dispatcher.CurrentDispatcher.InvokeAsync(() => button.RaiseEvent(new RoutedEventArgs(Button.ClickEvent)));
        await UiHost.Settle();
        var forms = Application.Current.Windows.OfType<RelayFormWindow>().ToList();
        Assert.AreEqual(1, forms.Count, "forms open after one click");
        return forms[0];
    }

    static async Task AssertModalOverMainAsync(MainWindow window, RelayFormWindow form)
    {
        Assert.AreSame(window, form.Owner);
        Assert.IsTrue(System.Windows.Interop.ComponentDispatcher.IsThreadModal);
        Assert.IsFalse(IsWindowEnabled(new System.Windows.Interop.WindowInteropHelper(window).Handle), "main window blocked");
        form.Close();
        await UiHost.Settle();
    }

    [TestMethod]
    public async Task AddRelay_OpensOneModalOwnedByMainWindow_BlockingIt()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, async window => await AssertModalOverMainAsync(window, await OpenFormAsync(window, "添加配置")));
    }

    [TestMethod]
    public async Task EditRelay_OpensOneModalOwnedByMainWindow_BlockingIt()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, async window => await AssertModalOverMainAsync(window, await OpenFormAsync(window, "编辑")));
    }

    [TestMethod]
    public async Task BeginEditRelay_WhileOtherEditorSaving_KeepsSavingEditorContent()
    {
        var h = await StartAsync(3);
        var gate = new TaskCompletionSource<System.Text.Json.JsonElement>();
        h.Core.OnAsync("save_relay", _ => gate.Task);
        var editor = Editors.Edit(h.Model, "1");
        editor.NameInput = "改名";

        var saving = editor.SaveAsync();
        await Task.Yield();
        Assert.IsTrue(editor.IsSaving);

        h.Model.BeginEditRelay("2");

        Assert.IsTrue(editor.IsSaving);
        Assert.AreEqual("改名", editor.NameInput);
        Assert.AreEqual("https://relay1.example.com/v1", editor.BaseUrlInput);
        Assert.AreEqual("sk-example-0001", editor.ApiKeyInput);
        Assert.AreEqual("编辑配置", editor.Title);

        gate.SetResult(RelayJson.Parse("{\"id\":\"1\",\"name\":\"改名\",\"model_count\":3,\"models_changed\":false}"));
        await saving;
    }

    [TestMethod]
    public async Task Overview_Off_WhiteWithGrayBorder()
    {
        var h = await StartAsync(3, forwarding: false);

        await WithWindowAsync(h, window =>
        {
            var surface = UiHost.ThemeBrush("SurfaceBrush").Color;
            var divider = UiHost.ThemeBrush("DividerBrush").Color;
            Assert.IsTrue(UiHost.All<Border>(window).Any(b =>
                b.IsVisible && b.Background is SolidColorBrush bg && bg.Color == surface
                && b.BorderBrush is SolidColorBrush bb && bb.Color == divider
                && UiHost.Descendants(b).OfType<TextBlock>().Any(t => t.Text == "使用中转 · 已关闭")), "white hero with gray border");
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task RelayRow_SecondLineIs12px()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, window =>
        {
            var status = UiHost.FindText(window, h.Model.Relays[0].StatusText);
            Assert.IsNotNull(status);
            Assert.AreEqual(12, status.FontSize);
            var url = UiHost.All<TextBlock>(window).First(t => t.IsVisible && t.Text.Contains("https://relay1.example.com/v1"));
            Assert.AreEqual(12, url.FontSize);
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task StatusArea_IconsAre16px()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, window =>
        {
            var version = UiHost.FindText(window, h.Model.Version);
            Assert.IsNotNull(version);
            var statusTop = UiHost.PositionIn(window, version).Y - 8;
            var icons = UiHost.Sized(window, 16, 16).Where(e => UiHost.PositionIn(window, e).Y >= statusTop).ToList();
            Assert.IsTrue(icons.Count > 0, "16px icon in status area");
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task RelayCard_TitleToListGapIs20px()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, window =>
        {
            var title = UiHost.FindText(window, "中转配置")!;
            var list = UiHost.All<ItemsControl>(window).First(i => i.IsVisible && UiHost.Buttons(i, "编辑").Count > 0);
            var gap = UiHost.PositionIn(window, list).Y - (UiHost.PositionIn(window, title).Y + title.ActualHeight);
            Assert.AreEqual(20, gap, 0.5);
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task StatusRow_VersionToDotGapIs16px()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, window =>
        {
            var version = UiHost.FindText(window, h.Model.Version)!;
            var dot = UiHost.Sized(window, 8, 8).OfType<System.Windows.Shapes.Ellipse>().First();
            var gap = UiHost.PositionIn(window, dot).X - (UiHost.PositionIn(window, version).X + version.ActualWidth);
            Assert.AreEqual(16, gap, 0.5);
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task FormOpen_DialogsCurrentIsSet()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, async window =>
        {
            var form = await OpenFormAsync(window, "添加配置");

            Assert.IsNotNull(Dialogs.Current);
            form.Close();
            await UiHost.Settle();
        });
    }

    static void AssertBusyLocksList(MultiHarness h)
    {
        Assert.IsTrue(h.Model.IsBusy);
        Assert.IsFalse(h.Model.CanAddRelay);
        Assert.IsTrue(h.Model.Relays.All(r => !r.CanEdit && !r.CanDelete));
    }

    [TestMethod]
    public async Task Saving_LocksAddEditDelete()
    {
        var h = await StartAsync(3);
        var gate = new TaskCompletionSource<System.Text.Json.JsonElement>();
        h.Core.OnAsync("save_relay", _ => gate.Task);
        var editor = Editors.Edit(h.Model, "1");
        editor.NameInput = "改名";

        var saving = editor.SaveAsync();
        await Task.Yield();
        AssertBusyLocksList(h);

        gate.SetResult(RelayJson.Parse("{\"id\":\"1\",\"name\":\"改名\",\"model_count\":3,\"models_changed\":false}"));
        await saving;
    }

    [TestMethod]
    public async Task TogglingForwarding_LocksAddEditDelete()
    {
        var h = await StartAsync(3);
        var gate = new TaskCompletionSource<System.Text.Json.JsonElement>();
        h.Core.OnAsync("set_forwarding", _ => gate.Task);

        var toggling = h.Model.ToggleForwardingAsync();

        await Task.Yield();
        AssertBusyLocksList(h);
        gate.SetResult(RelayJson.Status(false, h.Core.Entries.ToArray()));
        await toggling;
    }

    [TestMethod]
    public void Sources_DoNotContainLegacyTerm()
    {
        var legacy = "中" + "台";
        var hits = Directory.EnumerateFiles(UiHost.AppSourceDir(), "*.*", SearchOption.AllDirectories)
            .Where(f => (f.EndsWith(".xaml") || f.EndsWith(".cs")) && !f.Contains($"{Path.DirectorySeparatorChar}obj{Path.DirectorySeparatorChar}") && !f.Contains($"{Path.DirectorySeparatorChar}bin{Path.DirectorySeparatorChar}"))
            .Where(f => File.ReadAllText(f).Contains(legacy))
            .ToList();

        Assert.AreEqual(0, hits.Count, string.Join("\n", hits));
    }
}
