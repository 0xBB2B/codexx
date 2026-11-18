using System.Text.Json;
using System.Windows;
using System.Windows.Controls.Primitives;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.Services;
using ModelBridge.UI;

namespace ModelBridge.Tests;

[TestClass]
public class UninstallDialogTests
{
    const string PauseTitle = "卸载暂停：有设置被手动改过";

    static readonly ConfirmRequest Request = new("卸载模型接入助手？", "卸载会：…", "卸载");
    static readonly UninstallPause Pause = new(["model_provider"], "/home/example/.codex/config.toml");

    [TestMethod]
    public void ConfirmBusy_CancelDisabled_UninstallDisabledBusyAndRenamed()
    {
        var buttons = DialogModel.ForConfirm(Request, busy: true).Buttons;

        Assert.IsFalse(buttons[0].IsEnabled);
        Assert.AreEqual("正在卸载", buttons[1].Text);
        Assert.IsFalse(buttons[1].IsEnabled);
        Assert.IsTrue(buttons[1].IsBusy);
    }

    [TestMethod]
    public void ConfirmNotBusy_BothButtonsEnabledAndNotBusy()
    {
        var buttons = DialogModel.ForConfirm(Request, busy: false).Buttons;

        Assert.IsTrue(buttons[0].IsEnabled);
        Assert.AreEqual("卸载", buttons[1].Text);
        Assert.IsTrue(buttons[1].IsEnabled);
        Assert.IsFalse(buttons[1].IsBusy);
    }

    [TestMethod]
    public void PauseBusy_OnlyRecheckIsBusy_AllButtonsDisabled()
    {
        var buttons = DialogModel.ForUninstallPause(Pause, busy: true).Buttons;

        Assert.AreEqual("取消卸载", buttons[0].Text);
        Assert.IsFalse(buttons[0].IsEnabled);
        Assert.AreEqual("打开所在文件夹", buttons[1].Text);
        Assert.IsFalse(buttons[1].IsEnabled);
        Assert.AreEqual("正在卸载", buttons[2].Text);
        Assert.IsFalse(buttons[2].IsEnabled);
        Assert.IsTrue(buttons[2].IsBusy);
        Assert.IsFalse(buttons[0].IsBusy || buttons[1].IsBusy);
    }

    [TestMethod]
    public void PauseNotBusy_ButtonsEnabledAndRecheckLabelKept()
    {
        var buttons = DialogModel.ForUninstallPause(Pause, busy: false).Buttons;

        Assert.IsTrue(buttons.All(b => b.IsEnabled && !b.IsBusy));
        Assert.AreEqual("重新检查", buttons[2].Text);
    }

    static async Task<(MultiHarness H, TaskCompletionSource<JsonElement> Gate)> PrepareAsync(bool headless)
    {
        var h = new MultiHarness();
        if (headless) h.Seed("1", "中转", "https://relay.example.com", "sk-example-0001");
        else await h.StartWithTwoAsync();
        return (h, new TaskCompletionSource<JsonElement>());
    }

    static async Task<(DialogWindow Dialog, Task<int> Shown)> OpenAsync(AppModel model, bool headless)
    {
        var before = Application.Current.Windows.OfType<DialogWindow>().ToList();
        var shown = System.Windows.Threading.Dispatcher.CurrentDispatcher
            .InvokeAsync(() => Dialogs.ShowUninstall(model, headless)).Task.Unwrap();
        await UiHost.Settle();
        return (Application.Current.Windows.OfType<DialogWindow>().Except(before).Single(), shown);
    }

    static async Task ClickAsync(DialogWindow dialog, string label)
    {
        UiHost.FindButton(dialog, label)!.RaiseEvent(new RoutedEventArgs(ButtonBase.ClickEvent));
        await UiHost.Settle();
    }

    static async Task PressEscapeAsync(DialogWindow dialog)
    {
        var escape = new System.Windows.Input.KeyEventArgs(
            System.Windows.Input.Keyboard.PrimaryDevice,
            PresentationSource.FromVisual(dialog)!,
            0,
            System.Windows.Input.Key.Escape)
        { RoutedEvent = System.Windows.Input.Keyboard.PreviewKeyDownEvent };
        dialog.RaiseEvent(escape);
        await UiHost.Settle();
    }

    static void AssertBusyButton(DialogWindow dialog)
    {
        var button = UiHost.FindButton(dialog, "正在卸载");
        Assert.IsNotNull(button);
        Assert.IsFalse(button.IsEnabled);
        Assert.IsTrue(UiHost.All<Spinner>(button).Any());
    }

    static async Task AssertEscapeAndCloseIgnoredAsync(DialogWindow dialog)
    {
        await PressEscapeAsync(dialog);
        Assert.IsTrue(dialog.IsVisible, "Esc while busy");
        dialog.Close();
        await UiHost.Settle();
        Assert.IsTrue(dialog.IsVisible, "Close while busy");
    }

    static async Task FinishAsync(DialogWindow dialog, Task<int> shown, TaskCompletionSource<JsonElement> gate)
    {
        gate.TrySetResult(Json.Parse("{}"));
        dialog.ForceClose();
        await shown;
    }

    [TestMethod]
    [DataRow(false)]
    [DataRow(true)]
    public async Task Confirmed_StaysOpenBusy_CancelDisabled_EscapeAndCloseIgnored_ClosesWithCode0(bool headless)
    {
        var (h, gate) = await PrepareAsync(headless);
        h.Core.OnAsync("prepare_uninstall", _ => gate.Task);

        await UiHost.RunAsync(async () =>
        {
            var (dialog, shown) = await OpenAsync(h.Model, headless);
            try
            {
                Assert.IsTrue(UiHost.FindButton(dialog, "取消")!.IsEnabled);
                await ClickAsync(dialog, "卸载");

                Assert.IsTrue(dialog.IsVisible);
                Assert.IsFalse(UiHost.FindButton(dialog, "取消")!.IsEnabled);
                AssertBusyButton(dialog);
                await AssertEscapeAndCloseIgnoredAsync(dialog);

                gate.SetResult(Json.Parse("{}"));
                var code = await shown;

                Assert.AreEqual(0, code);
                Assert.IsFalse(dialog.IsVisible);
            }
            finally
            {
                await FinishAsync(dialog, shown, gate);
            }
        });
    }

    [TestMethod]
    [DataRow(false)]
    [DataRow(true)]
    public async Task Cancel_ClosesWithCode1_PrepareNeverCalled(bool headless)
    {
        var (h, gate) = await PrepareAsync(headless);
        h.Core.RaiseFailed("核心进程反复崩溃");
        var noticesBefore = h.Model.Notices.Select(n => n.Id).ToList();
        var keysBefore = h.Vault.Keys.Count;
        var relaysBefore = h.Settings.Relays.Count;

        await UiHost.RunAsync(async () =>
        {
            var (dialog, shown) = await OpenAsync(h.Model, headless);
            try
            {
                await ClickAsync(dialog, "取消");
                var code = await shown;

                Assert.AreEqual(1, code);
                Assert.IsFalse(dialog.IsVisible);
                Assert.AreEqual(0, h.Core.Count("prepare_uninstall"));
                Assert.AreEqual(0, h.Core.Count("set_forwarding"));
                Assert.AreEqual(0, h.Vault.DeleteAllCount);
                Assert.AreEqual(0, h.Login.DisableCount);
                Assert.AreEqual(keysBefore, h.Vault.Keys.Count);
                Assert.AreEqual(relaysBefore, h.Settings.Relays.Count);
                CollectionAssert.AreEqual(noticesBefore, h.Model.Notices.Select(n => n.Id).ToList());
                if (headless)
                {
                    Assert.IsNull(h.Core.StartedWith);
                    Assert.AreEqual(0, h.Core.Calls.Count);
                }
            }
            finally
            {
                await FinishAsync(dialog, shown, gate);
            }
        });
    }

    [TestMethod]
    [DataRow(false)]
    [DataRow(true)]
    public async Task RestoreBlocked_SwapsToPauseInSameWindow_RecheckGoesBusy_ThenClosesWithCode0(bool headless)
    {
        var (h, gate) = await PrepareAsync(headless);
        var attempts = 0;
        h.Core.OnAsync("prepare_uninstall", _ => ++attempts == 1
            ? Task.FromException<JsonElement>(new CoreCallException("restore_blocked", ["model_provider"]))
            : gate.Task);

        await UiHost.RunAsync(async () =>
        {
            var (dialog, shown) = await OpenAsync(h.Model, headless);
            try
            {
                await ClickAsync(dialog, "卸载");

                Assert.IsTrue(dialog.IsVisible);
                Assert.IsNotNull(UiHost.FindText(dialog, PauseTitle));
                Assert.IsTrue(UiHost.FindButton(dialog, "取消卸载")!.IsEnabled);
                Assert.AreEqual(1, Application.Current.Windows.OfType<DialogWindow>().Count(w => w.IsVisible));

                await ClickAsync(dialog, "重新检查");

                Assert.IsTrue(dialog.IsVisible);
                Assert.IsNotNull(UiHost.FindText(dialog, PauseTitle));
                Assert.IsFalse(UiHost.FindButton(dialog, "取消卸载")!.IsEnabled);
                Assert.IsFalse(UiHost.FindButton(dialog, "打开所在文件夹")!.IsEnabled);
                AssertBusyButton(dialog);
                await AssertEscapeAndCloseIgnoredAsync(dialog);

                gate.SetResult(Json.Parse("{}"));
                var code = await shown;

                Assert.AreEqual(0, code);
                Assert.IsFalse(dialog.IsVisible);
                Assert.AreEqual(2, h.Core.Count("prepare_uninstall"));
            }
            finally
            {
                await FinishAsync(dialog, shown, gate);
            }
        });
    }

    [TestMethod]
    [DataRow(false)]
    [DataRow(true)]
    public async Task RestoreBlocked_CancelUninstall_ClosesWithCode2_NothingCleaned(bool headless)
    {
        var (h, gate) = await PrepareAsync(headless);
        h.Core.Fail("prepare_uninstall", "restore_blocked", "model_provider");

        await UiHost.RunAsync(async () =>
        {
            var (dialog, shown) = await OpenAsync(h.Model, headless);
            try
            {
                await ClickAsync(dialog, "卸载");
                await ClickAsync(dialog, "取消卸载");
                var code = await shown;

                Assert.AreEqual(2, code);
                Assert.IsFalse(dialog.IsVisible);
                Assert.AreEqual(0, h.Vault.DeleteAllCount);
            }
            finally
            {
                await FinishAsync(dialog, shown, gate);
            }
        });
    }

    [TestMethod]
    [DataRow(false)]
    [DataRow(true)]
    public async Task PrepareFails_ClosesWithCode1(bool headless)
    {
        var (h, gate) = await PrepareAsync(headless);
        h.Core.Fail("prepare_uninstall", "internal_error");

        await UiHost.RunAsync(async () =>
        {
            var (dialog, shown) = await OpenAsync(h.Model, headless);
            try
            {
                await ClickAsync(dialog, "卸载");
                var code = await shown;

                Assert.AreEqual(1, code);
                Assert.IsFalse(dialog.IsVisible);
            }
            finally
            {
                await FinishAsync(dialog, shown, gate);
            }
        });
    }

    [TestMethod]
    [DataRow(false)]
    [DataRow(true)]
    public async Task Cancel_WithForwardingOn_CoreNeverStartedNoForwardingRewrite(bool headless)
    {
        var (h, gate) = await PrepareAsync(headless);
        h.Push(forwarding: true);

        await UiHost.RunAsync(async () =>
        {
            var (dialog, shown) = await OpenAsync(h.Model, headless);
            try
            {
                var callsBefore = h.Core.Calls.Count;
                await ClickAsync(dialog, "取消");
                var code = await shown;

                Assert.AreEqual(1, code);
                Assert.AreEqual(callsBefore, h.Core.Calls.Count);
                Assert.AreEqual(0, h.Core.Count("set_forwarding"));
                Assert.AreEqual(0, h.Core.Count("apply_access"));
                if (headless) Assert.IsNull(h.Core.StartedWith);
            }
            finally
            {
                await FinishAsync(dialog, shown, gate);
            }
        });
    }

    static System.Text.Json.Nodes.JsonObject MisalignedAccess(System.Text.Json.Nodes.JsonObject s)
    {
        s["forwarding"] = true;
        s["access"] = new System.Text.Json.Nodes.JsonObject { ["mode"] = "relay", ["target"] = "both", ["detect"] = "logged_in" };
        return s;
    }

    [TestMethod]
    [DataRow(false)]
    [DataRow(true)]
    public async Task ConfirmOpen_HoldsBusy_StatusPushedMeanwhileDoesNotAlign_ConfirmThenCompletesWithCode0(bool headless)
    {
        var h = new Harness();
        if (!headless) await h.Model.StartAsync();

        await UiHost.RunAsync(async () =>
        {
            var (dialog, shown) = await OpenAsync(h.Model, headless);
            try
            {
                h.Push(s => MisalignedAccess(s));
                await UiHost.Settle();

                Assert.AreEqual(0, h.Core.Count("apply_access"));

                await ClickAsync(dialog, "卸载");
                var code = await shown;

                Assert.AreEqual(0, code);
                Assert.IsFalse(dialog.IsVisible);
                Assert.AreEqual(0, h.Core.Count("apply_access"));
            }
            finally
            {
                dialog.ForceClose();
                await shown;
            }
        });
    }

    [TestMethod]
    [DataRow(false)]
    [DataRow(true)]
    public async Task Cancel_ReleasesBusy_LaterForwardingToggleRuns(bool headless)
    {
        var h = new Harness();
        if (!headless) await h.Model.StartAsync();
        h.Push();

        await UiHost.RunAsync(async () =>
        {
            var (dialog, shown) = await OpenAsync(h.Model, headless);
            try
            {
                Assert.IsTrue(h.Model.IsBusy);
                await ClickAsync(dialog, "取消");
                await shown;

                Assert.IsFalse(h.Model.IsBusy);
                await h.Model.ToggleForwardingAsync();
                Assert.AreEqual(1, h.Core.Count("set_forwarding"));
            }
            finally
            {
                dialog.ForceClose();
                await shown;
            }
        });
    }

    [TestMethod]
    [DataRow(false)]
    [DataRow(true)]
    public async Task RestoreBlocked_PauseDialogFocusesCancelUninstall(bool headless)
    {
        var (h, gate) = await PrepareAsync(headless);
        h.Core.Fail("prepare_uninstall", "restore_blocked", "model_provider");

        await UiHost.RunAsync(async () =>
        {
            var (dialog, shown) = await OpenAsync(h.Model, headless);
            try
            {
                await ClickAsync(dialog, "卸载");

                Assert.IsNotNull(UiHost.FindText(dialog, PauseTitle));
                Assert.AreSame(UiHost.FindButton(dialog, "取消卸载"), System.Windows.Input.FocusManager.GetFocusedElement(dialog));
            }
            finally
            {
                await FinishAsync(dialog, shown, gate);
            }
        });
    }

    [TestMethod]
    public async Task ShowUninstall_WhileModelBusy_OpensNoWindowAndReturns1()
    {
        var (h, gate) = await PrepareAsync(false);
        h.Core.OnAsync("prepare_uninstall", _ => gate.Task);
        var running = h.Model.RunConfirmedUninstallAsync(false);

        await UiHost.RunAsync(async () =>
        {
            var before = Application.Current.Windows.OfType<DialogWindow>().Count();

            var code = await Dialogs.ShowUninstall(h.Model, false);

            Assert.AreEqual(1, code);
            Assert.AreEqual(before, Application.Current.Windows.OfType<DialogWindow>().Count());
        });

        gate.SetResult(Json.Parse("{}"));
        await running;
    }

    [TestMethod]
    public async Task ShowUninstall_WhileConnecting_OpensNoWindow_Returns1_AsksToShowMainWindow()
    {
        var h = new MultiHarness();
        await h.Model.StartAsync();
        var saveGate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("save_relay", _ => saveGate.Task);
        h.Model.BaseUrlInput = "https://relay.example.com/v1";
        h.Model.ApiKeyInput = "sk-example-0001";
        var submit = h.Model.CompleteSetupAsync();
        Assert.IsTrue(h.Model.IsConnecting);
        var shows = 0;
        h.Model.ShowMainWindowRequested += () => shows++;

        try
        {
            await UiHost.RunAsync(async () =>
            {
                var before = Application.Current.Windows.OfType<DialogWindow>().Count();

                var code = await Dialogs.ShowUninstall(h.Model, false);

                Assert.AreEqual(1, code);
                Assert.AreEqual(before, Application.Current.Windows.OfType<DialogWindow>().Count());
            });

            Assert.AreEqual(1, shows);
        }
        finally
        {
            saveGate.TrySetException(new CoreCallException("relay_401", []));
            await submit;
        }
    }

    [TestMethod]
    [DataRow(false)]
    [DataRow(true)]
    public async Task ShowUninstall_WhileConfirmOpen_OpensNoSecondWindow_Returns1(bool headless)
    {
        var (h, gate) = await PrepareAsync(headless);

        await UiHost.RunAsync(async () =>
        {
            var (dialog, shown) = await OpenAsync(h.Model, headless);
            try
            {
                var open = Application.Current.Windows.OfType<DialogWindow>().Count(w => w.IsVisible);

                var code = await Dialogs.ShowUninstall(h.Model, headless);

                Assert.AreEqual(1, code);
                Assert.AreEqual(open, Application.Current.Windows.OfType<DialogWindow>().Count(w => w.IsVisible));
                Assert.IsTrue(dialog.IsVisible);
            }
            finally
            {
                await FinishAsync(dialog, shown, gate);
            }
        });
    }
}
