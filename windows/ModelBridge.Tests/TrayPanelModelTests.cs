using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.UI;

namespace ModelBridge.Tests;

[TestClass]
public class TrayPanelModelTests
{
    static TrayMenuItem Item(TrayPanelModel panel, TrayMenuCommand command) =>
        panel.Groups.SelectMany(g => g).Single(i => i.Command == command);

    static async Task<Harness> StartedAsync(Action<System.Text.Json.Nodes.JsonObject>? tweak = null)
    {
        var h = new Harness();
        await h.Model.StartAsync();
        if (tweak != null) h.Push(tweak);
        return h;
    }

    [TestMethod]
    public void Width_Is286()
    {
        var panel = TrayPanelModel.From(new Harness().Model);

        Assert.AreEqual(286, panel.Width);
    }

    [TestMethod]
    public async Task Title_IsProductName()
    {
        var h = await StartedAsync();

        Assert.AreEqual("模型接入助手", TrayPanelModel.From(h.Model).Title);
    }

    [TestMethod]
    public async Task Subtitle_ForwardingOn_ShowsOnAndRunning()
    {
        var h = await StartedAsync(s => s["forwarding"] = true);

        Assert.AreEqual("转发已开启 · 后台运行中", TrayPanelModel.From(h.Model).Subtitle);
    }

    [TestMethod]
    public async Task Subtitle_ForwardingOff_ShowsOffAndRunning()
    {
        var h = await StartedAsync();

        Assert.AreEqual("转发已关闭 · 后台运行中", TrayPanelModel.From(h.Model).Subtitle);
    }

    [TestMethod]
    public async Task Subtitle_CoreStopped_ShowsStoppedOnly()
    {
        var h = await StartedAsync(s => s["forwarding"] = true);
        h.Core.RaiseFailed("核心进程反复崩溃");

        Assert.AreEqual("后台服务已停止", TrayPanelModel.From(h.Model).Subtitle);
    }

    [TestMethod]
    public async Task Groups_AreThree_WithExpectedCommandsInOrder()
    {
        var h = await StartedAsync();

        var groups = TrayPanelModel.From(h.Model).Groups;

        Assert.AreEqual(3, groups.Count);
        CollectionAssert.AreEqual(
            new[] { TrayMenuCommand.ToggleForwarding, TrayMenuCommand.OpenConfig },
            groups[0].Select(i => i.Command).ToArray());
        CollectionAssert.AreEqual(
            new[] { TrayMenuCommand.ToggleLoginStart, TrayMenuCommand.CheckUpdate },
            groups[1].Select(i => i.Command).ToArray());
        CollectionAssert.AreEqual(
            new[] { TrayMenuCommand.Uninstall, TrayMenuCommand.Quit },
            groups[2].Select(i => i.Command).ToArray());
    }

    [TestMethod]
    public async Task Labels_MatchPrototype()
    {
        var h = await StartedAsync();
        var panel = TrayPanelModel.From(h.Model);

        Assert.AreEqual("转发到中台", Item(panel, TrayMenuCommand.ToggleForwarding).Label);
        Assert.AreEqual("打开配置…", Item(panel, TrayMenuCommand.OpenConfig).Label);
        Assert.AreEqual("登录时启动", Item(panel, TrayMenuCommand.ToggleLoginStart).Label);
        Assert.AreEqual("检查更新", Item(panel, TrayMenuCommand.CheckUpdate).Label);
        Assert.AreEqual("卸载模型接入助手…", Item(panel, TrayMenuCommand.Uninstall).Label);
        Assert.AreEqual("退出模型接入助手", Item(panel, TrayMenuCommand.Quit).Label);
    }

    [TestMethod]
    public async Task Forwarding_IsCheckable_CheckedFollowsForwardingEnabled()
    {
        var off = await StartedAsync();
        var on = await StartedAsync(s => s["forwarding"] = true);

        var offItem = Item(TrayPanelModel.From(off.Model), TrayMenuCommand.ToggleForwarding);
        var onItem = Item(TrayPanelModel.From(on.Model), TrayMenuCommand.ToggleForwarding);

        Assert.IsTrue(offItem.IsCheckable);
        Assert.IsFalse(offItem.IsChecked);
        Assert.IsTrue(onItem.IsChecked);
    }

    [TestMethod]
    public async Task Forwarding_EnabledFollowsToggleEnabled()
    {
        var running = await StartedAsync();
        var stopped = await StartedAsync();
        stopped.Core.RaiseFailed("核心进程反复崩溃");

        var runningItem = Item(TrayPanelModel.From(running.Model), TrayMenuCommand.ToggleForwarding);
        var stoppedItem = Item(TrayPanelModel.From(stopped.Model), TrayMenuCommand.ToggleForwarding);

        Assert.IsTrue(runningItem.IsEnabled);
        Assert.IsFalse(stoppedItem.IsEnabled);
    }

    [TestMethod]
    public async Task LoginStart_IsCheckable_CheckedFollowsLoginStartEnabled()
    {
        var off = await StartedAsync();
        var on = new Harness();
        on.Login.Enabled = true;
        await on.Model.StartAsync();

        var offItem = Item(TrayPanelModel.From(off.Model), TrayMenuCommand.ToggleLoginStart);
        var onItem = Item(TrayPanelModel.From(on.Model), TrayMenuCommand.ToggleLoginStart);

        Assert.IsTrue(offItem.IsCheckable);
        Assert.IsFalse(offItem.IsChecked);
        Assert.IsTrue(onItem.IsChecked);
    }

    [TestMethod]
    public async Task NonToggleItems_AreNotCheckable()
    {
        var h = await StartedAsync();
        var panel = TrayPanelModel.From(h.Model);

        Assert.IsFalse(Item(panel, TrayMenuCommand.OpenConfig).IsCheckable);
        Assert.IsFalse(Item(panel, TrayMenuCommand.CheckUpdate).IsCheckable);
        Assert.IsFalse(Item(panel, TrayMenuCommand.Uninstall).IsCheckable);
        Assert.IsFalse(Item(panel, TrayMenuCommand.Quit).IsCheckable);
    }

    [TestMethod]
    public async Task CheckUpdate_NoNewVersion_HasNoDetail()
    {
        var h = await StartedAsync();

        var item = Item(TrayPanelModel.From(h.Model), TrayMenuCommand.CheckUpdate);

        Assert.AreEqual("检查更新", item.Label);
        Assert.IsTrue(string.IsNullOrEmpty(item.Detail));
    }

    [TestMethod]
    public async Task CheckUpdate_NewVersion_ShowsVersionAndDownloadHint()
    {
        var h = await StartedAsync(s => s["update"] = new System.Text.Json.Nodes.JsonObject
        {
            ["latest_version"] = "1.3.0",
            ["url"] = "https://example.com/download",
        });

        var item = Item(TrayPanelModel.From(h.Model), TrayMenuCommand.CheckUpdate);

        Assert.AreEqual("发现新版本 1.3.0", item.Label);
        Assert.AreEqual("打开下载页", item.Detail);
    }

    [TestMethod]
    public async Task Busy_DisablesLoginStartUpdateUninstallAndQuit()
    {
        var h = await StartedAsync();
        h.Confirmer.Gate = new TaskCompletionSource<bool>();
        var pending = h.Model.UninstallAsync();
        Assert.IsTrue(h.Model.IsBusy);

        var panel = TrayPanelModel.From(h.Model);

        Assert.IsFalse(Item(panel, TrayMenuCommand.ToggleForwarding).IsEnabled);
        Assert.IsFalse(Item(panel, TrayMenuCommand.ToggleLoginStart).IsEnabled);
        Assert.IsFalse(Item(panel, TrayMenuCommand.CheckUpdate).IsEnabled);
        Assert.IsFalse(Item(panel, TrayMenuCommand.Uninstall).IsEnabled);
        Assert.IsFalse(Item(panel, TrayMenuCommand.Quit).IsEnabled);
        Assert.IsTrue(Item(panel, TrayMenuCommand.OpenConfig).IsEnabled);

        h.Confirmer.Gate.SetResult(false);
        await pending;
    }

    [TestMethod]
    public async Task NotBusy_EnablesLoginStartUpdateUninstallAndQuit()
    {
        var h = await StartedAsync();

        var panel = TrayPanelModel.From(h.Model);

        Assert.IsTrue(Item(panel, TrayMenuCommand.ToggleLoginStart).IsEnabled);
        Assert.IsTrue(Item(panel, TrayMenuCommand.CheckUpdate).IsEnabled);
        Assert.IsTrue(Item(panel, TrayMenuCommand.Uninstall).IsEnabled);
        Assert.IsTrue(Item(panel, TrayMenuCommand.Quit).IsEnabled);
    }

    [TestMethod]
    public async Task Busy_OpenConfigStaysEnabled_OthersDisabled()
    {
        var h = await StartedAsync();
        h.Confirmer.Gate = new TaskCompletionSource<bool>();
        var pending = h.Model.UninstallAsync();

        var items = TrayPanelModel.From(h.Model).Groups.SelectMany(g => g).ToList();

        Assert.IsTrue(items.Single(i => i.Command == TrayMenuCommand.OpenConfig).IsEnabled);
        Assert.IsTrue(items.Where(i => i.Command != TrayMenuCommand.OpenConfig).All(i => !i.IsEnabled));

        h.Confirmer.Gate.SetResult(false);
        await pending;
    }

    [TestMethod]
    public async Task Subtitle_EqualsTrayStatusText_WhenRunning()
    {
        var h = await StartedAsync(s => s["forwarding"] = true);

        Assert.AreEqual(h.Model.TrayStatusText, TrayPanelModel.From(h.Model).Subtitle);
        Assert.AreEqual("转发已开启 · 后台运行中", h.Model.TrayStatusText);
    }

    [TestMethod]
    public async Task Subtitle_EqualsTrayStatusText_WhenCoreStopped()
    {
        var h = await StartedAsync(s => s["forwarding"] = true);
        h.Core.RaiseFailed("核心进程反复崩溃");

        Assert.AreEqual("后台服务已停止", h.Model.TrayStatusText);
        Assert.AreEqual(h.Model.TrayStatusText, TrayPanelModel.From(h.Model).Subtitle);
    }

    [TestMethod]
    public async Task OpenConfig_IsEnabled()
    {
        var h = await StartedAsync();

        Assert.IsTrue(Item(TrayPanelModel.From(h.Model), TrayMenuCommand.OpenConfig).IsEnabled);
    }
}
