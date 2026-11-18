using System.Text.Json;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.Services;
using ModelBridge.UI;

namespace ModelBridge.Tests;

[TestClass]
public class ConnectingGuardTests
{
    static async Task<(MultiHarness Harness, TaskCompletionSource<JsonElement> Gate, Task Submit)> ConnectingAsync()
    {
        var h = new MultiHarness();
        await h.Model.StartAsync();
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("save_relay", _ => gate.Task);
        h.Model.BaseUrlInput = "https://relay.example.com/v1";
        h.Model.ApiKeyInput = "sk-example-0001";
        var submit = h.Model.CompleteSetupAsync();
        Assert.IsTrue(h.Model.IsConnecting);
        return (h, gate, submit);
    }

    static async Task FinishAsync(TaskCompletionSource<JsonElement> gate, Task submit)
    {
        gate.TrySetException(new CoreCallException("relay_401", []));
        await submit;
    }

    [TestMethod]
    public async Task Quit_WhileConnecting_DoesNotExitAndAsksToShowWindow()
    {
        var (h, gate, submit) = await ConnectingAsync();
        var exits = 0;
        var shows = 0;
        h.Model.ExitRequested += () => exits++;
        h.Model.ShowMainWindowRequested += () => shows++;
        try
        {
            await h.Model.QuitAsync();

            Assert.AreEqual(0, exits);
            Assert.AreEqual(0, h.Core.Count("shutdown"));
            Assert.AreEqual(1, shows);
        }
        finally
        {
            await FinishAsync(gate, submit);
        }
    }

    [TestMethod]
    public async Task Quit_AfterConnectFailed_ExitsNormally()
    {
        var (h, gate, submit) = await ConnectingAsync();
        await FinishAsync(gate, submit);
        var exits = 0;
        h.Model.ExitRequested += () => exits++;

        await h.Model.QuitAsync();

        Assert.AreEqual(1, exits);
    }

    [TestMethod]
    public async Task TryBeginUninstall_WhileConnecting_RefusesAndAsksToShowWindow()
    {
        var (h, gate, submit) = await ConnectingAsync();
        var shows = 0;
        h.Model.ShowMainWindowRequested += () => shows++;
        try
        {
            var allowed = h.Model.TryBeginUninstall();

            Assert.IsFalse(allowed);
            Assert.AreEqual(0, h.Core.Count("prepare_uninstall"));
            Assert.AreEqual(0, h.Vault.DeleteAllCount);
            Assert.IsNull(h.Model.Uninstalled);
            Assert.AreEqual(1, shows);
        }
        finally
        {
            await FinishAsync(gate, submit);
        }
    }

    [TestMethod]
    public async Task RecheckTryBeginUninstall_WhileConnecting_RefusesAndAsksToShowWindow()
    {
        var (h, gate, submit) = await ConnectingAsync();
        var shows = 0;
        h.Model.ShowMainWindowRequested += () => shows++;
        try
        {
            var code = await h.Model.RunConfirmedUninstallAsync(false);

            Assert.AreNotEqual(0, code);
            Assert.AreEqual(0, h.Core.Count("prepare_uninstall"));
            Assert.AreEqual(1, shows);
        }
        finally
        {
            await FinishAsync(gate, submit);
        }
    }

    [TestMethod]
    public void ForUninstallLaunchFailed_MatchesConfirmedCopy()
    {
        var dialog = DialogModel.ForUninstallLaunchFailed();

        Assert.AreEqual(520, dialog.Width);
        Assert.AreEqual("没能自动删除程序文件", dialog.Title);
        Assert.AreEqual(
            "卸载程序没能启动，本助手的程序文件还留在电脑上。Codex 配置已经还原。\n\n"
            + "请打开「设置 > 应用 > 已安装的应用」（有的系统里叫「应用和功能」），找到「模型接入助手」，点「卸载」。\n\n"
            + "点「退出」后本助手会关闭。",
            dialog.Message);
        Assert.AreEqual(1, dialog.Buttons.Count);
        Assert.AreEqual("退出", dialog.Buttons[0].Text);
        Assert.AreEqual(DialogButtonRole.Primary, dialog.Buttons[0].Role);
        Assert.IsNull(dialog.Alert);
    }

    [TestMethod]
    public void ForUnexpectedError_MatchesConfirmedCopy()
    {
        var dialog = DialogModel.ForUnexpectedError();

        Assert.AreEqual(520, dialog.Width);
        Assert.AreEqual("操作没有完成", dialog.Title);
        Assert.AreEqual("出了点意外，这次操作没有完成。请再试一次；还是不行的话，退出本助手再重新打开。", dialog.Message);
        Assert.AreEqual(1, dialog.Buttons.Count);
        Assert.AreEqual("知道了", dialog.Buttons[0].Text);
        Assert.AreEqual(DialogButtonRole.Primary, dialog.Buttons[0].Role);
        Assert.IsNull(dialog.Alert);
    }
}
