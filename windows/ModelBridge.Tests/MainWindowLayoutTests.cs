using Button = System.Windows.Controls.Button;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using System.Text.Json.Nodes;
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
    public async Task Window_WidthIs760_AndThreeRelaysHeightAbout633()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, window =>
        {
            Assert.AreEqual(760, window.ActualWidth, 0.5);
            Assert.IsTrue(window.ActualHeight is >= 621 and <= 645, "height=" + window.ActualHeight);
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task Window_WidthCannotBeChangedByUser()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, window =>
        {
            var fixedByMode = window.ResizeMode is not (ResizeMode.CanResize or ResizeMode.CanResizeWithGrip);
            var fixedByLimits = window.MinWidth == 760 && window.MaxWidth == 760;
            Assert.IsTrue(fixedByMode || fixedByLimits, $"ResizeMode={window.ResizeMode} MinWidth={window.MinWidth} MaxWidth={window.MaxWidth}");
            Assert.AreEqual(new Thickness(0), System.Windows.Shell.WindowChrome.GetWindowChrome(window).ResizeBorderThickness);
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task TitleBar_MaximizeButtonDisabled()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, window =>
        {
            var maximize = (Button)window.FindName("MaximizeButton");
            Assert.IsNotNull(maximize);
            Assert.IsFalse(maximize.IsEnabled);
            return Task.CompletedTask;
        });
    }

    static void SetNewVersion(MultiHarness h) =>
        h.Core.OnAsync("check_update", _ => Task.FromResult(RelayJson.Parse(
            """{"latest_version":"0.2.0","url":"https://relay.example.com/download","is_newer":true}""")));

    [TestMethod]
    public async Task UpdateButton_NoNewVersion_ShowsCheckUpdate()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, window =>
        {
            Assert.AreEqual("检查更新", window.UpdateButton.Content);
            Assert.AreEqual("检查更新", System.Windows.Automation.AutomationProperties.GetName(window.UpdateButton));
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task UpdateButton_NewVersion_ShowsVersionOnlyAndNoSeparateNoticeText()
    {
        var h = await StartAsync(3);
        SetNewVersion(h);

        await WithWindowAsync(h, async window =>
        {
            await h.Model.CheckUpdateAsync();
            await UiHost.Settle();

            Assert.AreEqual("新版本 0.2.0", window.UpdateButton.Content);
            Assert.AreEqual("发现新版本 0.2.0，打开下载页", System.Windows.Automation.AutomationProperties.GetName(window.UpdateButton));
            Assert.IsNull(UiHost.FindText(window, "发现新版本 0.2.0"));
            Assert.IsNull(UiHost.FindText(window, "打开下载页"));
            Assert.IsFalse(UiHost.Joined(window).Contains("打开下载页"), UiHost.Joined(window));
        });
    }

    [TestMethod]
    public async Task UpdateButton_InstallerTrue_AccessibleNameSaysDownloadInstaller()
    {
        var h = await StartAsync(3);
        h.Core.OnAsync("check_update", _ => Task.FromResult(RelayJson.Parse(
            """{"latest_version":"0.1.2","url":"https://github.com/o/r/releases/download/v0.1.2/model-bridge-0.1.2-windows-setup.exe","installer":true,"is_newer":true}""")));

        await WithWindowAsync(h, async window =>
        {
            await h.Model.CheckUpdateAsync();
            await UiHost.Settle();

            Assert.AreEqual("新版本 0.1.2", window.UpdateButton.Content);
            Assert.AreEqual("发现新版本 0.1.2，下载安装包", System.Windows.Automation.AutomationProperties.GetName(window.UpdateButton));
            Assert.IsFalse(UiHost.Joined(window).Contains("下载安装包"), UiHost.Joined(window));
        });
    }

    [TestMethod]
    public async Task UpdateButton_InstallerFalse_AccessibleNameSaysOpenDownloadPage()
    {
        var h = await StartAsync(3);
        h.Core.OnAsync("check_update", _ => Task.FromResult(RelayJson.Parse(
            """{"latest_version":"0.1.2","url":"https://github.com/o/r/releases/tag/v0.1.2","installer":false,"is_newer":true}""")));

        await WithWindowAsync(h, async window =>
        {
            await h.Model.CheckUpdateAsync();
            await UiHost.Settle();

            Assert.AreEqual("新版本 0.1.2", window.UpdateButton.Content);
            Assert.AreEqual("发现新版本 0.1.2，打开下载页", System.Windows.Automation.AutomationProperties.GetName(window.UpdateButton));
        });
    }

    [TestMethod]
    public async Task StatusRow_HeightSameWithAndWithoutNewVersion()
    {
        var h = await StartAsync(3);
        var before = 0.0;
        double HeightOf(MainWindow w) => UiHost.Ancestors(w.VersionText).OfType<Border>().First().ActualHeight;

        await WithWindowAsync(h, async window =>
        {
            before = HeightOf(window);
            SetNewVersion(h);
            await h.Model.CheckUpdateAsync();
            await UiHost.Settle();

            Assert.AreEqual(before, HeightOf(window), 0.5);
            var left = (FrameworkElement)VisualTreeHelper.GetParent(window.VersionText);
            left.Measure(new System.Windows.Size(double.PositiveInfinity, double.PositiveInfinity));
            Assert.IsTrue(left.DesiredSize.Width <= left.ActualWidth + 0.5, $"左侧需要 {left.DesiredSize.Width}，实际 {left.ActualWidth}");
            Assert.IsTrue(UiHost.PositionIn(window, left).X + left.DesiredSize.Width <= UiHost.PositionIn(window, window.UpdateButton).X, "左侧与新版本按钮重叠");
        });
    }

    [TestMethod]
    public async Task Uninstalling_MainWindowShowsNoProgressBanner()
    {
        var h = await StartAsync(1);
        var gate = new TaskCompletionSource<System.Text.Json.JsonElement>();
        h.Core.OnAsync("prepare_uninstall", _ => gate.Task);

        await WithWindowAsync(h, async window =>
        {
            var pending = h.Model.RunConfirmedUninstallAsync(false);
            await UiHost.Settle();

            var text = UiHost.Joined(window);
            Assert.IsFalse(text.Contains("正在卸载"), text);
            Assert.IsFalse(text.Contains("完成前请不要关闭本窗口"), text);

            gate.SetResult(Json.Parse("{}"));
            await pending;
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
    public async Task StatusRow_ForwardingOff_CoreTextHasNoForwardingClosedSuffix()
    {
        var h = await StartAsync(3, forwarding: false);

        await WithWindowAsync(h, window =>
        {
            Assert.IsNotNull(UiHost.FindText(window, "后台运行中"));
            Assert.IsFalse(UiHost.Joined(window).Contains("转发已关闭"));
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task RelayRow_StaleAndNeverFetched_TextUsesWarningBrush()
    {
        var h = await StartAsync(3, firstStale: true);

        await WithWindowAsync(h, async window =>
        {
            var warning = UiHost.ThemeBrush("WarningBrush").Color;
            AssertRowTextColor(window, h.Model.Relays[0].StatusText, warning);

            h.Core.Entries[0] = h.Core.Entries[0] with { Stale = false, ModelCount = 0, FetchedAt = null, Error = "relay_401" };
            h.Push();
            await UiHost.Settle();

            Assert.AreEqual("还没获取到模型", h.Model.Relays[0].StatusText);
            AssertRowTextColor(window, "还没获取到模型", warning);
        });
    }

    [TestMethod]
    public async Task RelayRow_Normal_TextUsesSuccessBrush()
    {
        var h = await StartAsync(3);

        await WithWindowAsync(h, window =>
        {
            Assert.AreEqual(RelayRowState.Normal, h.Model.Relays[0].State);
            AssertRowTextColor(window, h.Model.Relays[0].StatusText, UiHost.ThemeBrush("SuccessBrush").Color);
            return Task.CompletedTask;
        });
    }

    static void AssertRowTextColor(MainWindow window, string text, System.Windows.Media.Color expected)
    {
        var block = UiHost.FindText(window, text);
        Assert.IsNotNull(block, text);
        Assert.IsTrue(block.Foreground is SolidColorBrush b && b.Color == expected, text);
    }

    static async Task PushChatGptUnsupported(MultiHarness h)
    {
        var status = JsonNode.Parse(RelayJson.Status(false, h.Core.Entries.ToArray()).GetRawText())!;
        status["chatgpt"] = new JsonObject { ["found"] = true, ["error"] = "chatgpt_unsupported" };
        h.Core.RaiseStatus(RelayJson.Parse(status.ToJsonString()));
        await UiHost.Settle();
    }

    [TestMethod]
    public async Task ChatGptUnsupported_ToggleDisabled_OverviewBannerAndStatusRow()
    {
        var h = await StartAsync(3, forwarding: false);

        await WithWindowAsync(h, async window =>
        {
            await PushChatGptUnsupported(h);

            Assert.IsFalse(UiHost.All<System.Windows.Controls.Primitives.ToggleButton>(window).Single(t => t.Name == "ForwardToggle").IsEnabled);
            Assert.IsNotNull(UiHost.FindText(window, "当前 ChatGPT 版本暂不支持，暂时不能打开。"));
            Assert.IsNotNull(UiHost.FindText(window, "当前 ChatGPT 版本暂不支持"));
            Assert.IsNotNull(UiHost.FindText(window,
                "读不到 ChatGPT 自带的官方模型清单，转发保持关闭。请把 ChatGPT 更新到最新版后点「重新检测」；还是不行的话，请等待助手新版本。"));
            Assert.IsNotNull(UiHost.FindButton(window, "重新检测"));
            var statusRow = string.Concat(UiHost.Joined(window).Where(c => !char.IsWhiteSpace(c)));
            StringAssert.Contains(statusRow, "ChatGPT版本暂不支持");
            Assert.IsFalse(statusRow.Contains("ChatGPT已检测到"));
        });
    }

    [TestMethod]
    public async Task ChatGptUnsupported_StatusRow_TextsFullyShownAndDoNotOverlapUpdateButton()
    {
        var h = await StartAsync(3, forwarding: false);

        await WithWindowAsync(h, async window =>
        {
            await PushChatGptUnsupported(h);
            var items = new (string Name, TextBlock Block)[]
            {
                ("版本号", window.VersionText),
                ("后台运行中", window.CoreText),
                ("ChatGPT 取值", window.ChatGptValue),
                ("中转模型 标签", window.RelayLabel),
                ("中转模型 取值", window.RelayValue),
            };
            var statusBarWidth = UiHost.Ancestors(window.VersionText).OfType<Border>().First().ActualWidth;
            foreach (var (name, block) in items)
                block.Measure(new System.Windows.Size(double.PositiveInfinity, double.PositiveInfinity));
            foreach (var (name, block) in items)
                Console.WriteLine($"[状态行] {name} Text=\"{block.Text}\" X={UiHost.PositionIn(window, block).X} ActualWidth={block.ActualWidth} DesiredWidth={block.DesiredSize.Width}");
            Console.WriteLine($"[状态行] 检查更新按钮 X={UiHost.PositionIn(window, window.UpdateButton).X} ActualWidth={window.UpdateButton.ActualWidth}; 状态栏 ActualWidth={statusBarWidth}; 窗口 ActualWidth={window.ActualWidth}");

            Assert.AreEqual("后台运行中", window.CoreText.Text, $"状态栏 ActualWidth={statusBarWidth}");
            Assert.AreEqual("转发关闭时不获取", window.RelayValue.Text, $"状态栏 ActualWidth={statusBarWidth}");
            foreach (var (name, block) in items)
            {
                Assert.IsTrue(block.ActualWidth >= block.DesiredSize.Width - block.Margin.Left - block.Margin.Right - 0.5,
                    $"{name} 被裁掉: Text=\"{block.Text}\" ActualWidth={block.ActualWidth} DesiredWidth={block.DesiredSize.Width} 状态栏 ActualWidth={statusBarWidth}");
            }

            var right = UiHost.PositionIn(window, window.RelayValue).X + window.RelayValue.ActualWidth;
            var buttonLeft = UiHost.PositionIn(window, window.UpdateButton).X;
            Assert.IsTrue(right <= buttonLeft,
                $"状态行文字右边界 {right} 压到了「检查更新」左边界 {buttonLeft}; RelayValue Text=\"{window.RelayValue.Text}\" ActualWidth={window.RelayValue.ActualWidth} DesiredWidth={window.RelayValue.DesiredSize.Width} 状态栏 ActualWidth={statusBarWidth}");
        });
    }

    [TestMethod]
    public async Task ChatGptUnsupported_Redetect_ShowsBusyAndCallsStatus()
    {
        var h = await StartAsync(3, forwarding: false);

        await WithWindowAsync(h, async window =>
        {
            await PushChatGptUnsupported(h);
            var gate = new TaskCompletionSource<System.Text.Json.JsonElement>();
            h.Core.OnAsync("status", _ => gate.Task);

            var before = h.Core.Count("status");
            var rechecking = h.Model.RecheckAsync();
            await UiHost.Settle();

            Assert.AreEqual(before + 1, h.Core.Count("status"));
            Assert.IsNotNull(UiHost.FindText(window, "正在检测…"));
            Assert.IsNull(UiHost.FindButton(window, "重新检测"));
            gate.SetResult(RelayJson.Status(false, h.Core.Entries.ToArray()));
            await rechecking;
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
