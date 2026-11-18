using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Controls.Primitives;
using System.Windows.Media;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.UI;

namespace ModelBridge.Tests;

[TestClass]
public class PrototypeAlignmentTests
{
    static async Task<MultiHarness> StartedAsync()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        return h;
    }

    [TestMethod]
    public async Task Setup_ConnectFails_DetailEndsWithSingleFullStop()
    {
        var h = new MultiHarness();
        await h.Model.StartAsync();
        h.Core.Fail("save_relay", "relay_401");
        h.Model.BaseUrlInput = "https://relay.example.com/v1";
        h.Model.ApiKeyInput = "sk-example-0001";

        await h.Model.CompleteSetupAsync();

        Assert.AreEqual("中转服务返回 401：API Key 无效或已过期。", h.Model.TaskError!.Detail);
    }

    [TestMethod]
    public async Task Form_SaveFails_AlertHasNoDoubleFullStop()
    {
        var h = await StartedAsync();
        h.Core.Fail("save_relay", "relay_401");
        var editor = Editors.Add(h.Model);
        editor.NameInput = "B家";
        editor.BaseUrlInput = "https://relay3.example.com/v1";
        editor.ApiKeyInput = "sk-example-0003";

        await editor.SaveAsync();

        Assert.IsFalse(editor.SaveAlert!.Message.Contains("。。"));
        Assert.IsTrue(editor.SaveAlert.Message.StartsWith("中转服务返回 401：API Key 无效或已过期。已填的内容都还在"));
    }

    [TestMethod]
    public async Task DetectingAccess_ForwardToggleStaysEnabled_OnlyRecheckButtonDisabled()
    {
        var h = await StartedAsync();
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("detect_access", _ => gate.Task);

        await UiHost.RunAsync(async () =>
        {
            var window = new MainWindow(h.Model);
            try
            {
                await UiHost.ShowAsync(window);
                var detecting = h.Model.RecheckAccessAsync();
                await UiHost.Settle();

                Assert.IsTrue(h.Model.ToggleEnabled);
                Assert.IsTrue(UiHost.All<ToggleButton>(window).Any(t => t.IsVisible && t.IsEnabled));
                Assert.IsFalse(UiHost.FindButton(window, "正在检测…")!.IsEnabled);

                gate.SetResult(RelayJson.Status(true, h.Core.Entries.ToArray()));
                await detecting;
            }
            finally
            {
                window.AllowClose = true;
                window.Close();
            }
        });
    }

    [TestMethod]
    public async Task DetectingAccess_ForwardingClosedMeanwhile_StaysClosedAfterDetection()
    {
        var h = await StartedAsync();
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("detect_access", _ => gate.Task);

        var detecting = h.Model.RecheckAccessAsync();
        await h.Model.ToggleForwardingAsync();
        Assert.IsFalse(h.Model.ForwardingEnabled);

        gate.SetResult(RelayJson.Status(true, h.Core.Entries.ToArray()));
        await detecting;

        Assert.IsFalse(h.Model.ForwardingEnabled);
        Assert.AreEqual(1, h.Core.Count("set_forwarding"));
    }

    static JsonElement RelayTargetStatus(MultiHarness h, bool forwarding = true)
    {
        var node = System.Text.Json.Nodes.JsonNode.Parse(RelayJson.Status(forwarding, h.Core.Entries.ToArray(), null, "both").GetRawText())!;
        node["access"]!["target"] = "relay";
        return RelayJson.Parse(node.ToJsonString());
    }

    [TestMethod]
    public async Task DetectingAccess_UserToggledMeanwhile_DoesNotApplyAccess()
    {
        var h = await StartedAsync();
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("detect_access", _ => gate.Task);

        var detecting = h.Model.RecheckAccessAsync();
        await h.Model.ToggleForwardingAsync();
        gate.SetResult(RelayTargetStatus(h));
        await detecting;

        Assert.AreEqual(0, h.Core.Count("apply_access"));
    }

    [TestMethod]
    public async Task DetectingAccess_ThenApplying_IsBusyAndToggleDisabledUntilDone()
    {
        var h = await StartedAsync();
        var apply = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("detect_access", _ => Task.FromResult(RelayTargetStatus(h)));
        h.Core.OnAsync("apply_access", _ => apply.Task);

        var rechecking = h.Model.RecheckAccessAsync();
        await Task.Yield();

        Assert.AreEqual(1, h.Core.Count("apply_access"));
        Assert.IsTrue(h.Model.IsBusy);
        Assert.IsFalse(h.Model.ToggleEnabled);

        apply.SetResult(RelayJson.Status(true, h.Core.Entries.ToArray(), null, "relay"));
        await rechecking;

        Assert.IsFalse(h.Model.IsBusy);
        Assert.IsTrue(h.Model.ToggleEnabled);
    }

    [TestMethod]
    public async Task StartupAlign_Applying_IsBusyToggleDisabledAndNoSetForwarding()
    {
        var h = new MultiHarness();
        h.Seed("1", "中转", "https://relay.example.com/v1", "sk-example-0001");
        await h.Model.StartAsync();
        var apply = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("apply_access", _ => apply.Task);

        h.Core.RaiseStatus(RelayTargetStatus(h));
        await Task.Yield();

        Assert.AreEqual(1, h.Core.Count("apply_access"));
        Assert.IsTrue(h.Model.IsBusy);
        Assert.IsFalse(h.Model.ToggleEnabled);

        await h.Model.ToggleForwardingAsync();
        Assert.AreEqual(0, h.Core.Count("set_forwarding"));

        apply.SetResult(RelayJson.Status(true, h.Core.Entries.ToArray(), null, "relay"));
        await Task.Delay(100);

        Assert.IsFalse(h.Model.IsBusy);
        Assert.IsTrue(h.Model.ToggleEnabled);
    }

    [TestMethod]
    public async Task Setup_FinishButton_ClosesSetupWindowAndMovesToMainPage()
    {
        var h = new MultiHarness();
        await h.Model.StartAsync();
        h.Model.BaseUrlInput = "https://relay.example.com/v1";
        h.Model.ApiKeyInput = "sk-example-0001";
        await h.Model.CompleteSetupAsync();
        Assert.AreEqual(AppPage.Success, h.Model.Page);

        await UiHost.RunAsync(async () =>
        {
            var window = new SetupWindow(h.Model);
            var closed = false;
            window.Closed += (_, _) => closed = true;
            try
            {
                await UiHost.ShowAsync(window);

                UiHost.FindButton(window, "完成")!.RaiseEvent(new RoutedEventArgs(ButtonBase.ClickEvent));
                await UiHost.Settle();

                Assert.AreEqual(AppPage.Main, h.Model.Page);
                Assert.IsTrue(closed);
            }
            finally
            {
                if (!closed) window.Close();
            }
        });
    }

    [TestMethod]
    public void DeleteDialog_BlankLineBeforeRestartHint()
    {
        var message = DialogModel.ForDeleteRelay("A家", 3, isDeleting: false).Message;

        Assert.IsTrue(message.EndsWith("会从系统凭据管理器里删除\n\n重启 ChatGPT 后生效。"), message);
    }

    [TestMethod]
    public async Task Form_SaveFailedAlert_HasCloseButton_ClosesKeepingContentAndHeight()
    {
        var h = await StartedAsync();
        h.Core.Fail("save_relay", "relay_401");
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

                await editor.SaveAsync();
                await UiHost.Settle();

                Assert.IsNotNull(UiHost.FindText(window, "没有保存"));
                var close = UiHost.All<Button>(window).FirstOrDefault(b => b.IsVisible && b.Content as string == "");
                Assert.IsNotNull(close, "alert close button");
                Assert.AreEqual(before, window.ActualHeight, 0.5);

                close.RaiseEvent(new RoutedEventArgs(ButtonBase.ClickEvent));
                await UiHost.Settle();

                Assert.IsNull(UiHost.FindText(window, "没有保存"));
                Assert.IsNull(editor.SaveAlert);
                Assert.AreEqual("B家", editor.NameInput);
                Assert.AreEqual("sk-example-0003", editor.ApiKeyInput);
                Assert.AreEqual(before, window.ActualHeight, 0.5);
            }
            finally
            {
                window.Close();
            }
        });
    }

    [TestMethod]
    public async Task Form_Saving_DisabledInputsKeepVisibleBorder()
    {
        var h = await StartedAsync();
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("save_relay", _ => gate.Task);
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
                var saving = editor.SaveAsync();
                await UiHost.Settle();

                var box = UiHost.All<TextBox>(window).First(t => t.IsVisible && t.Text == "B家");
                Assert.IsFalse(box.IsEnabled);
                var root = (Border)box.Template.FindName("Root", box);
                Assert.IsTrue(root.BorderBrush is SolidColorBrush border && border.Color.A > 0);
                Assert.IsTrue(root.Background is SolidColorBrush fill && fill.Color != ((SolidColorBrush)root.BorderBrush).Color);

                gate.SetResult(RelayJson.Parse("{\"id\":\"3\",\"name\":\"B家\",\"model_count\":3,\"models_changed\":true}"));
                await saving;
            }
            finally
            {
                window.Close();
            }
        });
    }

    [TestMethod]
    public async Task StartupAlign_SkippedWhileBusy_AppliesOnNextMismatchedStatus()
    {
        var h = new MultiHarness();
        h.Seed("1", "中转", "https://relay.example.com/v1", "sk-example-0001");
        await h.Model.StartAsync();
        var save = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("save_relay", _ => save.Task);
        var editor = Editors.Add(h.Model);
        editor.NameInput = "B家";
        editor.BaseUrlInput = "https://relay3.example.com/v1";
        editor.ApiKeyInput = "sk-example-0003";
        var saving = editor.SaveAsync();
        await Task.Yield();
        Assert.IsTrue(h.Model.IsBusy);

        h.Core.RaiseStatus(RelayTargetStatus(h));
        Assert.AreEqual(0, h.Core.Count("apply_access"));

        save.SetResult(RelayJson.Parse("{\"id\":\"3\",\"name\":\"B家\",\"model_count\":3,\"models_changed\":true}"));
        await saving;
        h.Core.RaiseStatus(RelayTargetStatus(h));
        await Task.Delay(100);

        Assert.AreEqual(1, h.Core.Count("apply_access"));
    }
}
