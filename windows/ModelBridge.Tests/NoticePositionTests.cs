using System.Text.Json;
using System.Text.Json.Nodes;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Controls.Primitives;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.UI;

namespace ModelBridge.Tests;

[TestClass]
public class NoticePositionTests
{
    const double MainTitleBarHeight = 32;
    const string SwitchedTitle = "已切换到「只用中转」";
    const string ProxyTitle = "本机转发服务没能启动";
    const string NoChatGptTitle = "没有找到 ChatGPT";
    const string NoChatGptDetail = "请先安装官方 ChatGPT 桌面版，再回来打开转发。如果已经安装，可能是当前 ChatGPT 版本暂不支持，请等待助手新版本。";
    const string ConnectFailedTitle = "没能连上中转服务";
    const string NoSaveTitle = "没有保存";

    static Rect RectIn(Window window, FrameworkElement e) =>
        new(UiHost.PositionIn(window, e), new System.Windows.Size(e.ActualWidth, e.ActualHeight));

    static FrameworkElement AlertOf(Window window, string title)
    {
        var text = UiHost.FindText(window, title);
        Assert.IsNotNull(text, title);
        return UiHost.Ancestors(text).OfType<Border>().First();
    }

    static bool Inside(FrameworkElement e, FrameworkElement alert) => e == alert || UiHost.Ancestors(e).Contains(alert);

    static int AssertNoOverlap(Window window, FrameworkElement alert, string context, IEnumerable<(string Name, FrameworkElement Element)> targets)
    {
        var a = RectIn(window, alert);
        var checkedCount = 0;
        foreach (var (name, element) in targets.Where(t => t.Element.IsVisible))
        {
            checkedCount++;
            var r = RectIn(window, element);
            var w = Math.Min(a.Right, r.Right) - Math.Max(a.Left, r.Left);
            var h = Math.Min(a.Bottom, r.Bottom) - Math.Max(a.Top, r.Top);
            Assert.IsFalse(w > 0.5 && h > 0.5, $"{context}: 提示条 {a} 压住 {name} {r}");
        }
        return checkedCount;
    }

    static void AssertStackedWithGap(Window window, IReadOnlyList<FrameworkElement> alerts, string context)
    {
        for (var i = 1; i < alerts.Count; i++)
        {
            var upper = RectIn(window, alerts[i - 1]);
            var lower = RectIn(window, alerts[i]);
            Assert.AreEqual(upper.Left, lower.Left, 0.5, $"{context}: 第 {i + 1} 条左边");
            Assert.AreEqual(upper.Bottom + 8, lower.Top, 0.5, $"{context}: 第 {i + 1} 条与上一条的间距");
        }
    }

    #region 主窗口

    static JsonElement MainStatus(MultiHarness h, string mode, bool relayOnly, bool chatGptFound = true, string? proxyError = null)
    {
        var status = JsonNode.Parse(RelayJson.Status(true, h.Core.Entries.ToArray(), "中转", mode).GetRawText())!;
        status["access"]!["logged_in"] = true;
        status["access"]!["relay_only"] = relayOnly;
        status["chatgpt"]!["found"] = chatGptFound;
        status["proxy"]!["error"] = proxyError;
        return RelayJson.Parse(status.ToJsonString());
    }

    static readonly string[] MainStates = ["刚切换成功", "一条错误加一条成功带重试", "没有找到ChatGPT"];

    static async Task RunMainAsync(string state, Func<MainWindow, double, string[], Task> check)
    {
        var h = new MultiHarness();
        h.Seed("1", "中转", "https://relay1.example.com/v1", "sk-example-0001");
        await h.Model.StartAsync();
        h.ChatGpt.Running = true;
        h.Model.NoticeAutoDismissDelay = TimeSpan.FromMinutes(5);
        h.Core.RaiseStatus(MainStatus(h, "both", false));
        h.Core.OnAsync("set_relay_only", _ => Task.FromResult(MainStatus(h, "relay", true)));

        await UiHost.RunAsync(async () =>
        {
            var window = new MainWindow(h.Model);
            try
            {
                await UiHost.ShowAsync(window);
                var before = window.ActualHeight;
                string[] titles;
                switch (state)
                {
                    case "刚切换成功":
                        await h.Model.SetRelayOnlyAsync(true);
                        titles = [SwitchedTitle];
                        break;
                    case "一条错误加一条成功带重试":
                        await h.Model.SetRelayOnlyAsync(true);
                        h.Core.RaiseStatus(MainStatus(h, "relay", true, proxyError: "internal_error"));
                        titles = [SwitchedTitle, ProxyTitle];
                        break;
                    default:
                        h.Core.RaiseStatus(MainStatus(h, "both", false, chatGptFound: false));
                        titles = [NoChatGptTitle];
                        break;
                }
                await UiHost.Settle();
                await check(window, before, titles);
            }
            finally
            {
                window.AllowClose = true;
                window.Close();
            }
        });
    }

    static async Task ForEachMainStateAsync(Func<string, MainWindow, double, string[], Task> check)
    {
        foreach (var state in MainStates)
            await RunMainAsync(state, (window, before, titles) => check(state, window, before, titles));
    }

    static double MainTitleBarBottom(MainWindow window)
    {
        var title = UiHost.FindText(window, "模型接入助手");
        Assert.IsNotNull(title);
        var bar = UiHost.Ancestors(title).First(a => Math.Abs(a.ActualHeight - MainTitleBarHeight) < 0.5);
        return UiHost.PositionIn(window, bar).Y + bar.ActualHeight;
    }

    [TestMethod]
    public async Task Main_Notices_FloatOverContent_WindowHeightUnchanged() =>
        await ForEachMainStateAsync(async (state, window, _, titles) =>
        {
            foreach (var title in titles) Assert.IsNotNull(UiHost.FindText(window, title), $"{state}: {title}");
            var withNotices = window.ActualHeight;

            foreach (var title in titles)
                UiHost.All<Button>(AlertOf(window, title))
                    .First(b => System.Windows.Automation.AutomationProperties.GetName(b) == "关闭提示")
                    .RaiseEvent(new RoutedEventArgs(ButtonBase.ClickEvent));
            await UiHost.Settle();

            foreach (var title in titles) Assert.IsNull(UiHost.FindText(window, title), $"{state}: {title} 应已关闭");
            Assert.AreEqual(withNotices, window.ActualHeight, 0.5, state);
        });

    [TestMethod]
    public async Task Main_FirstNotice_Is12BelowTitleBar_24FromLeftEdge() =>
        await ForEachMainStateAsync((state, window, _, titles) =>
        {
            var first = RectIn(window, AlertOf(window, titles[0]));
            Assert.AreEqual(MainTitleBarBottom(window) + 12, first.Top, 0.5, state + " 上边");
            Assert.AreEqual(24, first.Left, 0.5, state + " 左边");
            return Task.CompletedTask;
        });

    [TestMethod]
    public async Task Main_Notices_Are376Wide() =>
        await ForEachMainStateAsync((state, window, _, titles) =>
        {
            foreach (var title in titles)
                Assert.AreEqual(376, AlertOf(window, title).ActualWidth, 0.5, $"{state}: {title}");
            return Task.CompletedTask;
        });

    [TestMethod]
    public async Task Main_TwoNotices_StackTopDown_8Apart() =>
        await RunMainAsync("一条错误加一条成功带重试", (window, _, titles) =>
        {
            AssertStackedWithGap(window, titles.Select(t => AlertOf(window, t)).ToList(), "两条");
            return Task.CompletedTask;
        });

    [TestMethod]
    public async Task Main_Notices_DoNotCoverSwitchesButtonsOrUpdateLink() =>
        await ForEachMainStateAsync((state, window, _, titles) =>
        {
            var targets = new List<(string, FrameworkElement)>
            {
                ("转发开关", window.ForwardToggle),
                ("只用中转开关", window.RelayOnlyToggle),
                ("重新检测", window.RecheckAccessButton),
                ("添加配置", window.AddRelayButton),
                ("检查更新", window.UpdateButton),
            };
            targets.AddRange(UiHost.Buttons(window, "编辑").Select(b => ("编辑", (FrameworkElement)b)));
            targets.AddRange(UiHost.Buttons(window, "删除").Select(b => ("删除", (FrameworkElement)b)));
            foreach (var title in titles)
            {
                var alert = AlertOf(window, title);
                var count = AssertNoOverlap(window, alert, $"{state}: {title}", targets.Where(t => !Inside(t.Item2, alert)));
                Assert.IsTrue(count >= 1, $"{state}: {title} 没有可见目标被检查");
            }
            return Task.CompletedTask;
        });

    [TestMethod]
    public async Task Main_Notice_KeepsShadowCloseButtonAndText() =>
        await RunMainAsync("没有找到ChatGPT", (window, _, _) =>
        {
            var alert = AlertOf(window, NoChatGptTitle);
            Assert.IsNotNull(alert.Effect, "浮动提示条保留阴影");
            Assert.IsTrue(UiHost.All<Button>(alert).Any(b => System.Windows.Automation.AutomationProperties.GetName(b) == "关闭提示"));
            Assert.IsNotNull(UiHost.FindText(window, NoChatGptDetail));
            Assert.IsNotNull(UiHost.FindButton(alert, "重新检测"));
            return Task.CompletedTask;
        });

    #endregion

    #region 首次设置窗口

    static readonly string[] SetupScenarios = ["连接失败", "写不进设置文件"];

    static async Task RunSetupAsync(string scenario, bool withCoreNotice, Func<SetupWindow, double, string[], Task> check)
    {
        var h = new MultiHarness();
        await h.Model.StartAsync();
        Assert.AreEqual(AppPage.Setup, h.Model.Page);
        h.Model.BaseUrlInput = "https://relay.example.com/v1";
        h.Model.ApiKeyInput = "sk-example-0001";
        string title;
        if (scenario == "连接失败")
        {
            h.Core.Fail("save_relay", "relay_401");
            title = ConnectFailedTitle;
        }
        else
        {
            h.Settings.SaveException = new IOException("disk full");
            h.Core.Fail("init", "internal_error");
            title = NoSaveTitle;
        }

        await UiHost.RunAsync(async () =>
        {
            var window = new SetupWindow(h.Model);
            try
            {
                await UiHost.ShowAsync(window);
                var before = window.ActualHeight;
                await h.Model.CompleteSetupAsync();
                var titles = new List<string> { title };
                if (withCoreNotice)
                {
                    h.Core.RaiseFailed("example");
                    titles.Add(h.Model.Notices.Last().Title);
                }
                await UiHost.Settle();
                await check(window, before, titles.ToArray());
            }
            finally
            {
                h.ForwardingOff();
                window.Close();
            }
        });
    }

    static async Task ForEachSetupScenarioAsync(Func<string, SetupWindow, double, string[], Task> check)
    {
        foreach (var scenario in SetupScenarios)
            await RunSetupAsync(scenario, false, (window, before, titles) => check(scenario, window, before, titles));
    }

    [TestMethod]
    public async Task Setup_Notice_FloatsOverContent_WindowHeightUnchanged() =>
        await ForEachSetupScenarioAsync((scenario, window, before, titles) =>
        {
            Assert.IsNotNull(UiHost.FindText(window, titles[0]), scenario);
            Assert.AreEqual(before, window.ActualHeight, 0.5, scenario);
            return Task.CompletedTask;
        });

    [TestMethod]
    public async Task Setup_Notice_Is12BelowTitleBar_24FromLeftEdge() =>
        await ForEachSetupScenarioAsync((scenario, window, _, titles) =>
        {
            var rect = RectIn(window, AlertOf(window, titles[0]));
            Assert.AreEqual(12, rect.Top, 0.5, scenario + " 上边");
            Assert.AreEqual(24, rect.Left, 0.5, scenario + " 左边");
            return Task.CompletedTask;
        });

    [TestMethod]
    public async Task Setup_Notice_Is608Wide() =>
        await ForEachSetupScenarioAsync((scenario, window, _, titles) =>
        {
            Assert.AreEqual(608, AlertOf(window, titles[0]).ActualWidth, 0.5, scenario);
            return Task.CompletedTask;
        });

    [TestMethod]
    public async Task Setup_TwoNotices_StackTopDown_8Apart() =>
        await RunSetupAsync("连接失败", true, (window, _, titles) =>
        {
            Assert.AreEqual(2, titles.Length);
            AssertStackedWithGap(window, titles.Select(t => AlertOf(window, t)).ToList(), "两条");
            return Task.CompletedTask;
        });

    [TestMethod]
    public async Task Setup_Notice_DoesNotCoverInputsKeyToggleOrFinishButton() =>
        await ForEachSetupScenarioAsync((scenario, window, _, titles) =>
        {
            var alert = AlertOf(window, titles[0]);
            var targets = UiHost.All<FrameworkElement>(window)
                .Where(e => e is TextBox or PasswordBox or ToggleButton && e.IsVisible && !Inside(e, alert))
                .Select(e => (e.GetType().Name, e))
                .Append(("完成设置", (FrameworkElement)window.SubmitButton));
            AssertNoOverlap(window, alert, scenario, targets);
            return Task.CompletedTask;
        });

    [TestMethod]
    public async Task Setup_Notice_KeepsShadowAndCloseButton() =>
        await ForEachSetupScenarioAsync((scenario, window, _, titles) =>
        {
            var alert = AlertOf(window, titles[0]);
            Assert.IsNotNull(alert.Effect, scenario);
            Assert.IsTrue(UiHost.All<Button>(alert).Any(b => System.Windows.Automation.AutomationProperties.GetName(b) == "关闭提示"), scenario);
            return Task.CompletedTask;
        });

    [TestMethod]
    public async Task MainAndSetup_FirstNotice_SameOffsetBelowTitleBar()
    {
        double mainOffset = 0, setupOffset = 0;
        await RunMainAsync("没有找到ChatGPT", (window, _, titles) =>
        {
            mainOffset = RectIn(window, AlertOf(window, titles[0])).Top - MainTitleBarBottom(window);
            return Task.CompletedTask;
        });
        await RunSetupAsync("连接失败", false, (window, _, titles) =>
        {
            setupOffset = RectIn(window, AlertOf(window, titles[0])).Top;
            return Task.CompletedTask;
        });

        Assert.AreEqual(12, mainOffset, 0.5);
        Assert.AreEqual(mainOffset, setupOffset, 0.5);
    }

    #endregion

    #region 添加/编辑配置窗口

    static readonly string[] FormScenarios = ["保存失败", "写不进设置文件"];

    static async Task RunFormAsync(string scenario, Func<RelayFormWindow, RelayEditorModel, double, FrameworkElement, Task> check)
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        if (scenario == "保存失败") h.Core.Fail("save_relay", "relay_401");
        else h.Settings.SaveException = new IOException("disk full");
        var editor = Editors.Edit(h.Model, "1");
        editor.NameInput = "中转";
        editor.BaseUrlInput = "https://relay.example.com/v1";
        editor.ApiKeyInput = "sk-example-0009";

        await UiHost.RunAsync(async () =>
        {
            var window = new RelayFormWindow(editor);
            try
            {
                await UiHost.ShowAsync(window);
                var before = window.ActualHeight;
                Assert.IsFalse(await editor.SaveAsync());
                await UiHost.Settle();
                await check(window, editor, before, AlertOf(window, NoSaveTitle));
            }
            finally
            {
                window.Close();
            }
        });
    }

    static async Task ForEachFormScenarioAsync(Func<string, RelayFormWindow, RelayEditorModel, double, FrameworkElement, Task> check)
    {
        foreach (var scenario in FormScenarios)
            await RunFormAsync(scenario, (window, editor, before, alert) => check(scenario, window, editor, before, alert));
    }

    static FrameworkElement HeaderRow(RelayFormWindow window) =>
        UiHost.Ancestors(UiHost.FindText(window, "编辑配置")!).OfType<StackPanel>().First();

    static FrameworkElement Card(RelayFormWindow window) =>
        UiHost.Ancestors(UiHost.FindText(window, "编辑配置")!).OfType<Border>().First(b => Math.Abs(b.ActualWidth - 520) < 0.5);

    [TestMethod]
    public async Task Form_Alert_SitsBetweenTitleAndFirstField_12Above_20Below() =>
        await ForEachFormScenarioAsync((scenario, window, _, _, alert) =>
        {
            var header = RectIn(window, HeaderRow(window));
            var rect = RectIn(window, alert);
            var form = RectIn(window, window.Form);
            Assert.AreEqual(header.Bottom + 12, rect.Top, 0.5, scenario + " 标题下面");
            Assert.AreEqual(rect.Bottom + 20, form.Top, 0.5, scenario + " 第一个字段上面");
            return Task.CompletedTask;
        });

    [TestMethod]
    public async Task Form_Alert_SameWidthAsFields_24FromWindowEdges() =>
        await ForEachFormScenarioAsync((scenario, window, _, _, alert) =>
        {
            var card = RectIn(window, Card(window));
            var rect = RectIn(window, alert);
            Assert.AreEqual(card.Left + 24, rect.Left, 0.5, scenario + " 左边");
            Assert.AreEqual(card.Right - 24, rect.Right, 0.5, scenario + " 右边");
            Assert.AreEqual(window.Form.ActualWidth, alert.ActualWidth, 0.5, scenario + " 与字段同宽");
            return Task.CompletedTask;
        });

    [TestMethod]
    public async Task Form_Alert_HasNoShadow() =>
        await ForEachFormScenarioAsync((scenario, _, _, _, alert) =>
        {
            Assert.IsNull(alert.Effect, scenario);
            return Task.CompletedTask;
        });

    [TestMethod]
    public async Task Form_Alert_TakesLayoutSpace_ClosingRestoresHeightAndKeepsInputs() =>
        await ForEachFormScenarioAsync(async (scenario, window, editor, before, alert) =>
        {
            Assert.AreEqual(before + alert.ActualHeight + 20, window.ActualHeight, 1, scenario + " 出现后");

            var close = UiHost.All<Button>(alert).First(b => System.Windows.Automation.AutomationProperties.GetName(b) == "关闭提示");
            close.RaiseEvent(new RoutedEventArgs(ButtonBase.ClickEvent));
            await UiHost.Settle();

            Assert.IsNull(UiHost.FindText(window, NoSaveTitle), scenario);
            Assert.AreEqual(before, window.ActualHeight, 0.5, scenario + " 关闭后");
            Assert.AreEqual("sk-example-0009", editor.ApiKeyInput, scenario);
        });

    [TestMethod]
    public async Task Form_Alert_DoesNotCoverAnyControl() =>
        await ForEachFormScenarioAsync((scenario, window, _, _, alert) =>
        {
            var targets = UiHost.All<FrameworkElement>(window)
                .Where(e => e is TextBox or PasswordBox or ButtonBase or TextBlock && e.IsVisible && e.ActualWidth > 0 && !Inside(e, alert))
                .Select(e => (e.GetType().Name, e));
            AssertNoOverlap(window, alert, scenario, targets);
            return Task.CompletedTask;
        });

    #endregion
}
