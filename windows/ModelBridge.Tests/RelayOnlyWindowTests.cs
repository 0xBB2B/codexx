using System.Text.Json.Nodes;
using System.Windows;
using System.Windows.Automation;
using System.Windows.Controls;
using System.Windows.Controls.Primitives;
using System.Windows.Input;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.UI;

namespace ModelBridge.Tests;

[TestClass]
public class RelayOnlyWindowTests
{
    const string SixteenCharName = "一二三四五六七八九十甲乙丙丁戊己";

    static async Task<MultiHarness> StartAsync(string relayName = "中转")
    {
        var h = new MultiHarness();
        h.Seed("1", relayName, "https://relay1.example.com/v1", "sk-example-0001");
        await h.Model.StartAsync();
        return h;
    }

    static void PushAccess(MultiHarness h, string mode, bool loggedIn, bool relayOnly, bool forwarding = true, string? defaultRelay = "中转")
    {
        var status = JsonNode.Parse(RelayJson.Status(forwarding, h.Core.Entries.ToArray(), defaultRelay, mode).GetRawText())!;
        status["access"]!["logged_in"] = loggedIn;
        status["access"]!["relay_only"] = relayOnly;
        h.Core.RaiseStatus(RelayJson.Parse(status.ToJsonString()));
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

    static ToggleButton? ToggleNamed(MainWindow window, string name) =>
        UiHost.All<ToggleButton>(window).FirstOrDefault(t => AutomationProperties.GetName(t) == name);

    static string LabelText(ToggleButton toggle)
    {
        toggle.ApplyTemplate();
        var label = (TextBlock)toggle.Template.FindName("Label", toggle);
        return string.Concat(label.Inlines.OfType<System.Windows.Documents.Run>().Select(r => r.Text));
    }

    static double CenterY(MainWindow window, FrameworkElement e) =>
        UiHost.PositionIn(window, e).Y + e.ActualHeight / 2;

    static List<Control> TabStops(MainWindow window) =>
        UiHost.All<Control>(window)
            .Where(c => c.IsTabStop && c.Focusable && c.IsEnabled && c.IsVisible)
            .Select((c, i) => (c, i))
            .OrderBy(x => KeyboardNavigation.GetTabIndex(x.c))
            .ThenBy(x => x.i)
            .Select(x => x.c)
            .ToList();

    [TestMethod]
    public async Task AccessRow_ForwardingOn_SingleLine_TextThenSwitchThenRecheck()
    {
        var h = await StartAsync();
        PushAccess(h, "both", loggedIn: true, relayOnly: false);

        await WithWindowAsync(h, window =>
        {
            var label = UiHost.FindText(window, "接入方式");
            var name = UiHost.FindText(window, h.Model.AccessModeName!);
            var toggle = ToggleNamed(window, "只用中转");
            var recheck = UiHost.FindButton(window, "重新检测");
            Assert.IsNotNull(label);
            Assert.IsNotNull(name);
            Assert.IsNotNull(toggle);
            Assert.IsNotNull(recheck);
            Assert.IsTrue(toggle.IsVisible);

            var centers = new[] { CenterY(window, label), CenterY(window, name), CenterY(window, toggle), CenterY(window, recheck) };
            Assert.IsTrue(centers.Max() - centers.Min() < 8, string.Join(",", centers));
            var nameX = UiHost.PositionIn(window, name).X;
            var toggleX = UiHost.PositionIn(window, toggle).X;
            var recheckX = UiHost.PositionIn(window, recheck).X;
            Assert.IsTrue(UiHost.PositionIn(window, label).X < nameX && nameX < toggleX && toggleX < recheckX,
                $"x: {nameX} {toggleX} {recheckX}");
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task RelayOnlySwitch_ShowsStateTextBesideSwitch()
    {
        var h = await StartAsync();
        PushAccess(h, "both", loggedIn: true, relayOnly: false);

        await WithWindowAsync(h, async window =>
        {
            var toggle = ToggleNamed(window, "只用中转")!;
            Assert.AreEqual("只用中转 · 已关闭", LabelText(toggle));

            PushAccess(h, "relay", loggedIn: true, relayOnly: true);
            await UiHost.Settle();

            Assert.AreEqual("只用中转 · 已开启", LabelText(toggle));
        });
    }

    [TestMethod]
    public async Task AccessRow_SwitchDoesNotMakeRowTallerThanRecheckButton()
    {
        var h = await StartAsync();
        PushAccess(h, "both", loggedIn: true, relayOnly: false);

        await WithWindowAsync(h, window =>
        {
            var toggle = ToggleNamed(window, "只用中转")!;
            var recheck = UiHost.FindButton(window, "重新检测")!;
            var row = (FrameworkElement)window.FindName("ModeRow");
            Assert.IsTrue(toggle.ActualHeight <= recheck.ActualHeight + 0.5, $"toggle={toggle.ActualHeight} button={recheck.ActualHeight}");
            Assert.AreEqual(44, row.ActualHeight, 0.5);
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task AccessRow_ForwardingOff_RowAndRelayOnlySwitchNotVisible()
    {
        var h = await StartAsync();
        PushAccess(h, "both", loggedIn: true, relayOnly: false, forwarding: false);

        await WithWindowAsync(h, window =>
        {
            Assert.IsNull(UiHost.FindText(window, "接入方式"));
            Assert.IsNull(UiHost.FindButton(window, "重新检测"));
            Assert.IsFalse(ToggleNamed(window, "只用中转")?.IsVisible ?? false);
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task TabOrder_ForwardThenRelayOnlyThenRecheck()
    {
        var h = await StartAsync();
        PushAccess(h, "both", loggedIn: true, relayOnly: false);

        await WithWindowAsync(h, window =>
        {
            var stops = TabStops(window);
            var forward = stops.IndexOf(ToggleNamed(window, "使用中转")!);
            var relayOnly = stops.IndexOf(ToggleNamed(window, "只用中转")!);
            var recheck = stops.IndexOf(UiHost.FindButton(window, "重新检测")!);

            Assert.IsTrue(forward >= 0 && relayOnly == forward + 1 && recheck == relayOnly + 1, $"{forward} {relayOnly} {recheck}");
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task TabOrder_RelayOnlyDisabled_SkippedBetweenForwardAndRecheck()
    {
        var h = await StartAsync();
        PushAccess(h, "relay", loggedIn: false, relayOnly: false);

        await WithWindowAsync(h, window =>
        {
            var stops = TabStops(window);
            var relayOnly = ToggleNamed(window, "只用中转")!;
            var forward = stops.IndexOf(ToggleNamed(window, "使用中转")!);
            var recheck = stops.IndexOf(UiHost.FindButton(window, "重新检测")!);

            Assert.IsFalse(relayOnly.IsEnabled);
            Assert.IsFalse(stops.Contains(relayOnly));
            Assert.IsTrue(forward >= 0 && recheck == forward + 1, $"{forward} {recheck}");
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task RelayOnlySwitch_AccessibleNameAndHelpTextIsOverview()
    {
        var h = await StartAsync();
        PushAccess(h, "both", loggedIn: true, relayOnly: false);

        await WithWindowAsync(h, window =>
        {
            var toggle = ToggleNamed(window, "只用中转");
            Assert.IsNotNull(toggle);
            Assert.AreEqual(h.Model.OverviewText, AutomationProperties.GetHelpText(toggle));
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task RelayOnlySwitch_HelpTextFollowsOverviewWhenModeChanges()
    {
        var h = await StartAsync();
        PushAccess(h, "both", loggedIn: true, relayOnly: false);

        await WithWindowAsync(h, async window =>
        {
            PushAccess(h, "relay", loggedIn: true, relayOnly: true);
            await UiHost.Settle();

            var toggle = ToggleNamed(window, "只用中转")!;
            Assert.AreEqual(h.Model.OverviewText, AutomationProperties.GetHelpText(toggle));
        });
    }

    [TestMethod]
    public async Task Subtitle_OneLine_EllipsisAndTooltipHoldsFullText()
    {
        var h = await StartAsync();
        PushAccess(h, "both", loggedIn: true, relayOnly: false);

        await WithWindowAsync(h, window =>
        {
            var sub = UiHost.FindText(window, h.Model.OverviewText!);
            Assert.IsNotNull(sub);
            Assert.AreEqual(TextTrimming.CharacterEllipsis, sub.TextTrimming);
            Assert.AreEqual(h.Model.OverviewText, sub.ToolTip as string);
            Assert.IsTrue(sub.ActualHeight <= sub.FontSize * sub.FontFamily.LineSpacing * 1.5, "height=" + sub.ActualHeight);
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task Subtitle_SixteenCharRelayName_StillOneLine()
    {
        var h = await StartAsync(SixteenCharName);
        PushAccess(h, "relay", loggedIn: false, relayOnly: false, defaultRelay: SixteenCharName);

        await WithWindowAsync(h, window =>
        {
            StringAssert.Contains(h.Model.OverviewText, SixteenCharName);
            var sub = UiHost.FindText(window, h.Model.OverviewText!);
            Assert.IsNotNull(sub);
            Assert.AreEqual(TextWrapping.NoWrap, sub.TextWrapping);
            Assert.IsTrue(sub.ActualHeight <= sub.FontSize * sub.FontFamily.LineSpacing * 1.5, "height=" + sub.ActualHeight);
            Assert.AreEqual(h.Model.OverviewText, sub.ToolTip as string);
            return Task.CompletedTask;
        });
    }
}
