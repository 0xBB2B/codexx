using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.UI;

namespace ModelBridge.Tests;

[TestClass]
public class RelayFormWindowTests
{
    const string NameHelp = "1~16 个字，不能重名，不能含「·」。";
    const string UrlHelp = "以 https:// 开头的完整地址。";
    const string KeyHelp = "保存在系统凭据管理器中。";

    static async Task WithFormAsync(Func<MultiHarness, RelayEditorModel> begin, Func<MultiHarness, RelayEditorModel, RelayFormWindow, Task> check)
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var editor = begin(h);
        await UiHost.RunAsync(async () =>
        {
            var window = new RelayFormWindow(editor);
            try
            {
                await UiHost.ShowAsync(window);
                await check(h, editor, window);
            }
            finally
            {
                window.Close();
            }
        });
    }

    [TestMethod]
    public async Task Add_Is520Wide_HeaderIcon24AndTitle()
    {
        await WithFormAsync(h => Editors.Add(h.Model), (_, _, window) =>
        {
            Assert.IsTrue(window.ActualWidth == 520 || UiHost.All<FrameworkElement>(window).Any(e => Math.Abs(e.ActualWidth - 520) < 0.5), "width 520");
            var title = UiHost.FindText(window, "添加配置");
            Assert.IsNotNull(title);
            var icon = UiHost.Sized(window, 24, 24).FirstOrDefault(e => UiHost.PositionIn(window, e).X < UiHost.PositionIn(window, title).X);
            Assert.IsNotNull(icon, "24px icon before title");
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task Edit_ShowsEditTitleAndExistingName()
    {
        await WithFormAsync(h => Editors.Edit(h.Model, "1"), (_, _, window) =>
        {
            Assert.IsNotNull(UiHost.FindText(window, "编辑配置"));
            Assert.IsTrue(UiHost.All<TextBox>(window).Any(t => t.Text == "中转"));
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task Form_ThreeRequiredFields_WithHelpTexts_AndButtons()
    {
        await WithFormAsync(h => Editors.Add(h.Model), (_, _, window) =>
        {
            Assert.AreEqual(3, UiHost.Texts(window).Count(t => t == "必填"));
            foreach (var label in new[] { "名字", "Base URL", "API Key" })
                Assert.IsNotNull(UiHost.FindText(window, label), label);
            foreach (var help in new[] { NameHelp, UrlHelp, KeyHelp })
                Assert.IsNotNull(UiHost.FindText(window, help), help);
            Assert.IsNotNull(UiHost.FindButton(window, "取消"));
            Assert.IsNotNull(UiHost.FindButton(window, "保存"));
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task Error_ReplacesHelpInPlace_HeightUnchanged()
    {
        await WithFormAsync(h => Editors.Add(h.Model), async (_, editor, window) =>
        {
            var help = UiHost.FindText(window, NameHelp)!;
            var helpY = UiHost.PositionIn(window, help).Y;
            var before = window.ActualHeight;
            editor.NameInput = "";

            await editor.SaveAsync();
            await UiHost.Settle();

            Assert.IsNotNull(editor.NameError);
            var error = UiHost.FindText(window, editor.NameError);
            Assert.IsNotNull(error);
            Assert.AreEqual(helpY, UiHost.PositionIn(window, error).Y, 1);
            Assert.AreEqual(before, window.ActualHeight, 0.5);
        });
    }

    [TestMethod]
    public async Task Saving_ShowsProgressText_DisablesFieldsAndCancel()
    {
        await WithFormAsync(h => Editors.Add(h.Model), async (h, editor, window) =>
        {
            var gate = new TaskCompletionSource<JsonElement>();
            h.Core.OnAsync("save_relay", _ => gate.Task);
            editor.NameInput = "B家";
            editor.BaseUrlInput = "https://relay3.example.com/v1";
            editor.ApiKeyInput = "sk-example-0003";

            var saving = editor.SaveAsync();
            await UiHost.Settle();

            Assert.IsNotNull(UiHost.FindButton(window, "正在保存并获取模型…"));
            Assert.IsFalse(UiHost.FindButton(window, "取消")!.IsEnabled);
            var inputs = UiHost.All<Control>(window).Where(c => c is TextBox or PasswordBox).ToList();
            Assert.IsTrue(inputs.Count >= 3);
            Assert.IsTrue(inputs.All(c => !c.IsEnabled));

            gate.SetResult(RelayJson.Parse("{\"id\":\"3\",\"name\":\"B家\",\"model_count\":3,\"models_changed\":true}"));
            await saving;
        });
    }

    static string? TooltipText(FrameworkElement element) => element.ToolTip switch
    {
        string text => text,
        ToolTip { Content: string text } => text,
        _ => null,
    };

    static async Task AssertErrorKeepsLayoutAsync(
        Action<RelayEditorModel> arrange, Func<RelayEditorModel, string?> errorOf, string helpText, string? seedLongName = null)
    {
        var h = new MultiHarness();
        h.Seed("1", "中转", "https://relay.example.com/v1", "sk-example-0001");
        if (seedLongName != null) h.Seed("2", seedLongName, "https://relay2.example.com/v1", "sk-example-0002");
        await h.Model.StartAsync();
        h.Push();
        var editor = Editors.Add(h.Model);
        editor.NameInput = "B家";
        editor.BaseUrlInput = "https://relay3.example.com/v1";
        editor.ApiKeyInput = "sk-example-0003";

        await UiHost.RunAsync(async () =>
        {
            var window = new RelayFormWindow(editor);
            try
            {
                await UiHost.ShowAsync(window);
                var before = window.ActualHeight;
                arrange(editor);

                await editor.SaveAsync();
                await UiHost.Settle();

                var message = errorOf(editor);
                Assert.IsNotNull(message);
                var error = UiHost.FindText(window, message);
                Assert.IsNotNull(error);
                Assert.AreNotEqual(TextTrimming.None, error.TextTrimming);
                Assert.AreEqual(TextWrapping.NoWrap, error.TextWrapping);
                Assert.AreEqual(message, TooltipText(error));
                Assert.AreEqual(before, window.ActualHeight, 0.5);
                Assert.IsNull(UiHost.FindText(window, helpText));
            }
            finally
            {
                window.Close();
            }
        });
    }

    [TestMethod]
    public async Task LongNameError_OneLineEllipsisTooltip_HeightUnchanged()
    {
        const string longName = "一二三四五六七八九十一二三四五六";
        await AssertErrorKeepsLayoutAsync(e => e.NameInput = longName, e => e.NameError, NameHelp, longName);
    }

    [TestMethod]
    public async Task BaseUrlError_OneLineEllipsisTooltip_HeightUnchanged() =>
        await AssertErrorKeepsLayoutAsync(e => e.BaseUrlInput = "http://relay.example.com", e => e.BaseUrlError, UrlHelp);

    [TestMethod]
    public async Task ApiKeyError_OneLineEllipsisTooltip_HeightUnchanged() =>
        await AssertErrorKeepsLayoutAsync(e => e.ApiKeyInput = "", e => e.ApiKeyError, KeyHelp);

    [TestMethod]
    public async Task ApiKey_MaskedByDefault_VisibleAfterShow_BaseUrlMonospace()
    {
        await WithFormAsync(h => Editors.Edit(h.Model, "1"), async (_, _, window) =>
        {
            const string key = "sk-example-0001";
            var masked = UiHost.All<PasswordBox>(window).FirstOrDefault(p => p.IsVisible);
            Assert.IsNotNull(masked);
            Assert.AreEqual(key, masked.Password);
            Assert.IsFalse(UiHost.All<TextBox>(window).Any(t => t.IsVisible && t.Text == key));

            var url = UiHost.All<TextBox>(window).First(t => t.IsVisible && t.Text == "https://relay.example.com/v1");
            StringAssert.Contains(url.FontFamily.Source, "Consolas");

            var toggle = UiHost.All<System.Windows.Controls.Primitives.ButtonBase>(window).First(b =>
                b.IsVisible && (System.Windows.Automation.AutomationProperties.GetName(b).Contains("显示") || UiHost.All<TextBlock>(b).Any(t => t.Text == "显示")));
            ((System.Windows.Controls.Primitives.ToggleButton)toggle).IsChecked = true;
            toggle.RaiseEvent(new RoutedEventArgs(System.Windows.Controls.Primitives.ButtonBase.ClickEvent));
            await UiHost.Settle();

            Assert.IsTrue(UiHost.All<TextBox>(window).Any(t => t.IsVisible && t.Text == key));
        });
    }

    [TestMethod]
    public async Task Saving_CloseIsCancelled_WindowStaysVisible()
    {
        await WithFormAsync(h => Editors.Add(h.Model), async (h, editor, window) =>
        {
            var gate = new TaskCompletionSource<JsonElement>();
            h.Core.OnAsync("save_relay", _ => gate.Task);
            editor.NameInput = "B家";
            editor.BaseUrlInput = "https://relay3.example.com/v1";
            editor.ApiKeyInput = "sk-example-0003";
            var saving = editor.SaveAsync();
            await UiHost.Settle();

            window.Close();
            await UiHost.Settle();

            Assert.IsTrue(window.IsVisible);

            gate.SetResult(RelayJson.Parse("{\"id\":\"3\",\"name\":\"B家\",\"model_count\":3,\"models_changed\":true}"));
            await saving;
        });
    }
}
