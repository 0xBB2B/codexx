using System.Windows;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.UI;

namespace ModelBridge.Tests;

[TestClass]
public class DeleteRelayDialogTests
{
    static async Task WithDialogAsync(DialogModel model, Func<DialogWindow, Task> check)
    {
        await UiHost.RunAsync(async () =>
        {
            var window = new DialogWindow(model);
            try
            {
                await UiHost.ShowAsync(window);
                await check(window);
            }
            finally
            {
                window.ForceClose();
            }
        });
    }

    static async Task<(DialogWindow Dialog, Task Shown)> OpenDeleteAsync(MultiHarness h, string id)
    {
        var before = Application.Current.Windows.OfType<DialogWindow>().ToList();
        var row = h.Model.Relays.Single(r => r.Id == id);
        var shown = System.Windows.Threading.Dispatcher.CurrentDispatcher.InvokeAsync(() => Dialogs.ShowDeleteRelay(h.Model, row)).Task;
        await UiHost.Settle();
        return (Application.Current.Windows.OfType<DialogWindow>().Except(before).Single(), shown);
    }

    [TestMethod]
    public async Task Idle_Is520Wide_ShowsTitleMessageAndEnabledButtons()
    {
        await WithDialogAsync(DialogModel.ForDeleteRelay("A家", 3, isDeleting: false), window =>
        {
            Assert.IsTrue(UiHost.All<FrameworkElement>(window).Any(e => e.IsVisible && Math.Abs(e.ActualWidth - 520) < 0.5), "width 520");
            Assert.IsNotNull(UiHost.FindText(window, "删除配置「A家」？"));
            StringAssert.Contains(UiHost.Joined(window), "「A家」的 3 个模型");
            Assert.IsTrue(UiHost.FindButton(window, "取消")!.IsEnabled);
            Assert.IsTrue(UiHost.FindButton(window, "删除")!.IsEnabled);
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task Deleting_ButtonShowsProgress_CancelDisabled()
    {
        await WithDialogAsync(DialogModel.ForDeleteRelay("A家", 3, isDeleting: true), window =>
        {
            Assert.IsNotNull(UiHost.FindButton(window, "正在删除…"));
            Assert.IsFalse(UiHost.FindButton(window, "取消")!.IsEnabled);
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public void Deleting_ModelMarksDeleteButtonBusyAndDisabled()
    {
        var buttons = DialogModel.ForDeleteRelay("A家", 3, isDeleting: true).Buttons;

        Assert.IsTrue(buttons[1].IsBusy);
        Assert.IsFalse(buttons[1].IsEnabled);
        Assert.IsFalse(DialogModel.ForDeleteRelay("A家", 3, isDeleting: false).Buttons[1].IsBusy);
    }

    [TestMethod]
    public async Task Deleting_DeleteButtonDisabledWithSpinner()
    {
        await WithDialogAsync(DialogModel.ForDeleteRelay("A家", 3, isDeleting: true), window =>
        {
            var button = UiHost.FindButton(window, "正在删除…");
            Assert.IsNotNull(button);
            Assert.IsFalse(button.IsEnabled);
            Assert.IsTrue(UiHost.All<ModelBridge.UI.Spinner>(button).Any());
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task Idle_FocusStartsOnCancel()
    {
        await WithDialogAsync(DialogModel.ForDeleteRelay("A家", 3, isDeleting: false), window =>
        {
            Assert.AreSame(UiHost.FindButton(window, "取消"), System.Windows.Input.FocusManager.GetFocusedElement(window));
            return Task.CompletedTask;
        });
    }

    [TestMethod]
    public async Task ShowDeleteRelay_ClickDelete_ShowsProgressThenClosesAfterDeletion()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var gate = new TaskCompletionSource<System.Text.Json.JsonElement>();
        h.Core.OnAsync("delete_relay", _ => gate.Task);

        await UiHost.RunAsync(async () =>
        {
            var (dialog, shown) = await OpenDeleteAsync(h, "2");
            try
            {
                UiHost.FindButton(dialog, "删除")!.RaiseEvent(new RoutedEventArgs(System.Windows.Controls.Primitives.ButtonBase.ClickEvent));
                await UiHost.Settle();

                Assert.IsTrue(dialog.IsVisible);
                Assert.IsFalse(UiHost.FindButton(dialog, "取消")!.IsEnabled);
                Assert.IsFalse(UiHost.FindButton(dialog, "正在删除…")!.IsEnabled);

                gate.SetResult(RelayJson.Status(true, [new("1", "中转")]));
                await shown;

                Assert.IsFalse(dialog.IsVisible);
                Assert.AreEqual(1, h.Core.Count("delete_relay"));
            }
            finally
            {
                gate.TrySetResult(RelayJson.Status(true, [new("1", "中转")]));
                dialog.ForceClose();
                await shown;
            }
        });
    }

    [TestMethod]
    public async Task ShowDeleteRelay_OwnerIsMainWindow()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        await UiHost.RunAsync(async () =>
        {
            var main = new MainWindow(h.Model);
            try
            {
                await UiHost.ShowAsync(main);
                var (dialog, shown) = await OpenDeleteAsync(h, "2");
                try
                {
                    Assert.AreSame(main, dialog.Owner);
                }
                finally
                {
                    dialog.ForceClose();
                    await shown;
                }
            }
            finally
            {
                main.AllowClose = true;
                main.Close();
            }
        });
    }

    [TestMethod]
    public async Task Deleting_EscapeAndCloseDoNotDismissDialog()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();
        var gate = new TaskCompletionSource<System.Text.Json.JsonElement>();
        h.Core.OnAsync("delete_relay", _ => gate.Task);

        await UiHost.RunAsync(async () =>
        {
            var (dialog, shown) = await OpenDeleteAsync(h, "2");
            try
            {
                UiHost.FindButton(dialog, "删除")!.RaiseEvent(new RoutedEventArgs(System.Windows.Controls.Primitives.ButtonBase.ClickEvent));
                await UiHost.Settle();

                var escape = new System.Windows.Input.KeyEventArgs(
                    System.Windows.Input.Keyboard.PrimaryDevice,
                    PresentationSource.FromVisual(dialog)!,
                    0,
                    System.Windows.Input.Key.Escape)
                { RoutedEvent = System.Windows.Input.Keyboard.PreviewKeyDownEvent };
                dialog.RaiseEvent(escape);
                await UiHost.Settle();
                Assert.IsTrue(dialog.IsVisible, "Esc while deleting");

                dialog.Close();
                await UiHost.Settle();
                Assert.IsTrue(dialog.IsVisible, "Close while deleting");
            }
            finally
            {
                gate.TrySetResult(RelayJson.Status(true, [new("1", "中转")]));
                await shown;
                dialog.ForceClose();
            }
        });
    }

    [TestMethod]
    public async Task Idle_CancelClosesDialogWithoutDeleting()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        await WithDialogAsync(DialogModel.ForDeleteRelay("A家", 3, isDeleting: false), window =>
        {
            UiHost.FindButton(window, "取消")!.RaiseEvent(new RoutedEventArgs(System.Windows.Controls.Primitives.ButtonBase.ClickEvent));
            Assert.AreEqual(0, window.Result);
            return Task.CompletedTask;
        });

        Assert.AreEqual(0, h.Core.Count("delete_relay"));
    }
}
