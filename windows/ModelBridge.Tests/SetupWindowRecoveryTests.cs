using System.Text.Json;
using System.Windows;
using System.Windows.Controls.Primitives;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.Services;
using ModelBridge.UI;

namespace ModelBridge.Tests;

[TestClass]
public class SetupWindowRecoveryTests
{
    const string BackedUpTitle = "原来的设置读不出来，已备份原文件";
    const string UnbackedTitle = "原来的设置读不出来";
    const string Corrupt = "{ 这不是 json";
    const string Reason = "保存中转名字和地址的设置文件读不出来，可能是文件坏了、正被别的程序占用，或者格式认不出来。";
    const string NoKeyHint = "这个文件里只有中转的名字和地址，没有 API Key。";

    string dir = "";

    string SettingsPath => Path.Combine(dir, "shell-settings.json");
    string BackupPath => SettingsPath + ".bak";

    [TestInitialize]
    public void Setup()
    {
        dir = Path.Combine(Path.GetTempPath(), "mb-settings-" + Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(dir);
    }

    [TestCleanup]
    public void Cleanup()
    {
        if (Directory.Exists(dir)) Directory.Delete(dir, true);
    }

    (MultiHarness Harness, AppModel Model) ModelOnRealSettings()
    {
        var h = new MultiHarness();
        var model = new AppModel(h.Core, h.Vault, new SettingsAdapter(new ShellSettings(dir)), h.Login, h.ChatGpt, h.Confirmer);
        return (h, model);
    }

    static AppNotice? Warning(AppModel model) =>
        model.Notices.FirstOrDefault(n => n.Title.StartsWith("原来的设置读不出来"));

    [TestMethod]
    public async Task Start_SettingsFileCorrupt_RenamedToBakAndWarnsWithBackupPath()
    {
        File.WriteAllText(SettingsPath, Corrupt);
        var (_, model) = ModelOnRealSettings();

        await model.StartAsync();

        Assert.AreEqual(AppPage.Setup, model.Page);
        Assert.IsFalse(File.Exists(SettingsPath));
        Assert.AreEqual(Corrupt, File.ReadAllText(BackupPath));
        var notice = Warning(model)!;
        Assert.IsNotNull(notice);
        Assert.AreEqual(BackedUpTitle, notice.Title);
        Assert.AreEqual(AppSeverity.Warning, notice.Severity);
        StringAssert.Contains(notice.Detail, Reason);
        StringAssert.Contains(notice.Detail, "原文件已经改名备份到：");
        StringAssert.Contains(notice.Detail, BackupPath);
        StringAssert.Contains(notice.Detail, NoKeyHint + "重新填写并完成设置后就能照常使用。");
    }

    [TestMethod]
    public async Task Start_SettingsFileCorrupt_ShowsOnlyOneWarning()
    {
        File.WriteAllText(SettingsPath, Corrupt);
        var (_, model) = ModelOnRealSettings();

        await model.StartAsync();

        Assert.AreEqual(1, model.Notices.Count(n => n.Title.StartsWith("原来的设置读不出来")));
    }

    [TestMethod]
    public async Task Start_BackupRenameFails_KeepsOriginalAndWarnsWithOriginalPath()
    {
        File.WriteAllText(SettingsPath, Corrupt);
        Directory.CreateDirectory(BackupPath);
        var (_, model) = ModelOnRealSettings();

        await model.StartAsync();

        Assert.AreEqual(AppPage.Setup, model.Page);
        Assert.AreEqual(Corrupt, File.ReadAllText(SettingsPath));
        var notice = Warning(model)!;
        Assert.IsNotNull(notice);
        Assert.AreEqual(UnbackedTitle, notice.Title);
        Assert.AreEqual(AppSeverity.Warning, notice.Severity);
        StringAssert.Contains(notice.Detail, Reason);
        StringAssert.Contains(notice.Detail, "没能备份这个文件，它还在原处：");
        StringAssert.Contains(notice.Detail, SettingsPath);
        StringAssert.Contains(notice.Detail, "完成设置后，它会被新的设置覆盖。想留着原文件的话，请先自己复制一份。");
        StringAssert.Contains(notice.Detail, NoKeyHint);
        Assert.IsFalse(notice.Detail.Contains("已经改名备份"));
    }

    [TestMethod]
    public async Task Start_SettingsFileLockedByAnotherProgram_WarnsAndLeavesFileInPlace()
    {
        File.WriteAllText(SettingsPath, Corrupt);
        var (_, model) = ModelOnRealSettings();

        using (new FileStream(SettingsPath, FileMode.Open, FileAccess.ReadWrite, FileShare.None))
            await model.StartAsync();

        Assert.AreEqual(AppPage.Setup, model.Page);
        Assert.AreEqual(Corrupt, File.ReadAllText(SettingsPath));
        Assert.AreEqual(UnbackedTitle, Warning(model)?.Title);
    }

    [TestMethod]
    public async Task Start_SettingsFileMissing_NoWarning()
    {
        var (_, model) = ModelOnRealSettings();

        await model.StartAsync();

        Assert.AreEqual(AppPage.Setup, model.Page);
        Assert.IsNull(Warning(model));
        Assert.IsFalse(File.Exists(BackupPath));
    }

    [TestMethod]
    public async Task Start_SettingsFileValid_NoWarningAndNoBackup()
    {
        new ShellSettings(dir).SaveRelays([new RelayConfig("1", "中转", "https://relay.example.com/v1")]);
        var (_, model) = ModelOnRealSettings();

        await model.StartAsync();

        Assert.AreEqual(AppPage.Main, model.Page);
        Assert.IsNull(Warning(model));
        Assert.IsFalse(File.Exists(BackupPath));
    }

    [TestMethod]
    public async Task CompleteSetup_AfterBackup_WarningRemoved()
    {
        File.WriteAllText(SettingsPath, Corrupt);
        var (_, model) = ModelOnRealSettings();
        await model.StartAsync();
        model.BaseUrlInput = "https://relay.example.com/v1";
        model.ApiKeyInput = "sk-example-0001";

        await model.CompleteSetupAsync();

        Assert.AreEqual(AppPage.Success, model.Page);
        Assert.IsNull(Warning(model));
        Assert.AreEqual(Corrupt, File.ReadAllText(BackupPath));
    }

    [TestMethod]
    public async Task CompleteSetup_ConnectFails_WarningStays()
    {
        File.WriteAllText(SettingsPath, Corrupt);
        var (h, model) = ModelOnRealSettings();
        await model.StartAsync();
        h.Core.Fail("save_relay", "relay_401");
        model.BaseUrlInput = "https://relay.example.com/v1";
        model.ApiKeyInput = "sk-example-0001";

        await model.CompleteSetupAsync();

        Assert.AreEqual(AppPage.Setup, model.Page);
        Assert.IsNotNull(Warning(model));
    }

    [TestMethod]
    [DataRow("{\"relays\":\"oops\"}")]
    [DataRow("[]")]
    [DataRow("{}")]
    [DataRow("{\"relays\":[{\"id\":\"1\",\"name\":\"中转\"}]}")]
    public async Task Start_ValidJsonWithWrongShape_BackedUpAndWarns(string content)
    {
        File.WriteAllText(SettingsPath, content);
        var (_, model) = ModelOnRealSettings();

        await model.StartAsync();

        Assert.AreEqual(AppPage.Setup, model.Page);
        Assert.IsFalse(File.Exists(SettingsPath));
        Assert.AreEqual(content, File.ReadAllText(BackupPath));
        Assert.AreEqual(BackedUpTitle, Warning(model)?.Title);
    }

    [TestMethod]
    public void DeleteAll_RemovesBackupToo()
    {
        var settings = new ShellSettings(dir);
        File.WriteAllText(SettingsPath, "{\"relays\":[]}");
        File.WriteAllText(BackupPath, Corrupt);

        settings.DeleteAll();

        Assert.IsFalse(File.Exists(SettingsPath));
        Assert.IsFalse(File.Exists(BackupPath));
    }

    [TestMethod]
    public void DeleteAll_OnlyBackupLeft_RemovesIt()
    {
        File.WriteAllText(BackupPath, Corrupt);

        new ShellSettings(dir).DeleteAll();

        Assert.IsFalse(File.Exists(BackupPath));
    }

    [TestMethod]
    public void DeleteAll_MainFileLocked_StillRemovesBackupAndThrows()
    {
        File.WriteAllText(SettingsPath, "{\"relays\":[]}");
        File.WriteAllText(BackupPath, Corrupt);

        using (new FileStream(SettingsPath, FileMode.Open, FileAccess.ReadWrite, FileShare.None))
            Assert.ThrowsExactly<IOException>(() => new ShellSettings(dir).DeleteAll());

        Assert.IsFalse(File.Exists(BackupPath));
    }

    [TestMethod]
    public void IsAlreadyCleaned_OnlyBackupLeft_False()
    {
        File.WriteAllText(BackupPath, Corrupt);
        var (_, model) = ModelOnRealSettings();

        Assert.IsFalse(model.IsAlreadyCleaned());
    }

    [TestMethod]
    public async Task Uninstall_AfterBackup_BackupFileRemoved()
    {
        File.WriteAllText(SettingsPath, Corrupt);
        var (_, model) = ModelOnRealSettings();
        await model.StartAsync();
        Assert.IsTrue(File.Exists(BackupPath));

        Assert.AreEqual(0, await model.RunConfirmedUninstallAsync(false));

        Assert.IsFalse(File.Exists(BackupPath));
        Assert.IsFalse(File.Exists(SettingsPath));
    }

    [TestMethod]
    public async Task CompleteSetup_InputsEditedWhileConnecting_SavesValuesFromClickTime()
    {
        var (h, model) = ModelOnRealSettings();
        await model.StartAsync();
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("save_relay", _ => gate.Task);
        model.NameInput = "原名";
        model.BaseUrlInput = "https://original.example.com/v1";
        model.ApiKeyInput = "sk-original-0000";
        var submit = model.CompleteSetupAsync();

        model.NameInput = "改后名字";
        model.BaseUrlInput = "https://changed.example.com/v1";
        model.ApiKeyInput = "sk-changed-9999";
        gate.SetResult(RelayJson.Parse("{\"id\":\"1\",\"name\":\"原名\",\"model_count\":3}"));
        await submit;

        Assert.AreEqual(
            new RelayConfig("1", "原名", "https://original.example.com/v1"),
            new ShellSettings(dir).ReadRelays().Single());
        Assert.AreEqual("sk-original-0000", h.Vault.Keys["1"]);
        Assert.AreEqual("原名", model.Relays.Single().Name);
        Assert.AreEqual("https://original.example.com/v1", model.Relays.Single().BaseUrl);
    }

    static async Task<DialogWindow> ClickDetailAsync(SetupWindow window)
    {
        var before = Application.Current.Windows.OfType<DialogWindow>().ToList();
        var button = UiHost.FindButton(window, "查看详情");
        Assert.IsNotNull(button);
        _ = System.Windows.Threading.Dispatcher.CurrentDispatcher.InvokeAsync(
            () => button.RaiseEvent(new RoutedEventArgs(ButtonBase.ClickEvent)));
        await UiHost.Settle();
        return Application.Current.Windows.OfType<DialogWindow>().Except(before).Single();
    }

    [TestMethod]
    public async Task Window_BackedUp_ShowsWarningWithDetailButtonAndEmptyForm()
    {
        File.WriteAllText(SettingsPath, Corrupt);
        var (_, model) = ModelOnRealSettings();
        await model.StartAsync();

        await UiHost.RunAsync(async () =>
        {
            var window = new SetupWindow(model);
            try
            {
                await UiHost.ShowAsync(window);

                Assert.IsNotNull(UiHost.FindText(window, BackedUpTitle));
                Assert.IsNotNull(UiHost.FindButton(window, "查看详情"));
                Assert.IsNotNull(UiHost.FindText(window, "连接中转服务"));
                Assert.AreEqual("", model.BaseUrlInput);
                Assert.AreEqual("", model.ApiKeyInput);
            }
            finally
            {
                window.Close();
            }
        });
    }

    [TestMethod]
    public async Task Window_ClickDetail_ShowsBackupPathAndGotItClosesOnlyTheDialog()
    {
        File.WriteAllText(SettingsPath, Corrupt);
        var (_, model) = ModelOnRealSettings();
        await model.StartAsync();

        await UiHost.RunAsync(async () =>
        {
            var window = new SetupWindow(model);
            DialogWindow? dialog = null;
            try
            {
                await UiHost.ShowAsync(window);

                dialog = await ClickDetailAsync(window);

                Assert.IsNotNull(UiHost.FindText(dialog, UnbackedTitle));
                var text = UiHost.Joined(dialog);
                StringAssert.Contains(text, Reason);
                StringAssert.Contains(text, "原文件已经改名备份到：");
                StringAssert.Contains(text, BackupPath);
                StringAssert.Contains(text, NoKeyHint + "重新填写并完成设置后就能照常使用。");

                UiHost.FindButton(dialog, "知道了")!.RaiseEvent(new RoutedEventArgs(ButtonBase.ClickEvent));
                await UiHost.Settle();

                Assert.IsFalse(dialog.IsVisible);
                Assert.IsNotNull(UiHost.FindText(window, BackedUpTitle));
            }
            finally
            {
                dialog?.ForceClose();
                window.Close();
            }
        });
    }

    [TestMethod]
    public async Task Window_BackupFailed_ClickDetail_ShowsOriginalPath()
    {
        File.WriteAllText(SettingsPath, Corrupt);
        Directory.CreateDirectory(BackupPath);
        var (_, model) = ModelOnRealSettings();
        await model.StartAsync();

        await UiHost.RunAsync(async () =>
        {
            var window = new SetupWindow(model);
            DialogWindow? dialog = null;
            try
            {
                await UiHost.ShowAsync(window);
                Assert.IsNotNull(UiHost.FindText(window, UnbackedTitle));

                dialog = await ClickDetailAsync(window);

                var text = UiHost.Joined(dialog);
                StringAssert.Contains(text, "没能备份这个文件，它还在原处：");
                StringAssert.Contains(text, SettingsPath);
                StringAssert.Contains(text, "完成设置后，它会被新的设置覆盖。想留着原文件的话，请先自己复制一份。");
            }
            finally
            {
                dialog?.ForceClose();
                window.Close();
            }
        });
    }

    [TestMethod]
    public async Task Window_DismissWarning_RemovesItAndKeepsFilledInput()
    {
        File.WriteAllText(SettingsPath, Corrupt);
        var (_, model) = ModelOnRealSettings();
        await model.StartAsync();
        model.BaseUrlInput = "https://relay.example.com/v1";

        await UiHost.RunAsync(async () =>
        {
            var window = new SetupWindow(model);
            try
            {
                await UiHost.ShowAsync(window);

                model.DismissNotice(Warning(model)!.Id);
                await UiHost.Settle();

                Assert.IsNull(UiHost.FindText(window, BackedUpTitle));
                Assert.AreEqual("https://relay.example.com/v1", model.BaseUrlInput);
            }
            finally
            {
                window.Close();
            }
        });
    }

    [TestMethod]
    public async Task Window_CloseWhileConnecting_IsCancelledAndCoreNotAskedToShutDown()
    {
        var h = new MultiHarness();
        await h.Model.StartAsync();
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("save_relay", _ => gate.Task);
        h.Model.BaseUrlInput = "https://relay.example.com/v1";
        h.Model.ApiKeyInput = "sk-example-0001";

        await UiHost.RunAsync(async () =>
        {
            var window = new SetupWindow(h.Model);
            var closed = false;
            window.Closed += (_, _) => closed = true;
            Task? submit = null;
            try
            {
                await UiHost.ShowAsync(window);
                submit = h.Model.CompleteSetupAsync();
                Assert.IsTrue(h.Model.IsConnecting);

                window.Close();
                await UiHost.Settle();

                Assert.IsFalse(closed);
                Assert.IsTrue(window.IsVisible);
                Assert.AreEqual(0, h.Core.Count("shutdown"));
            }
            finally
            {
                gate.TrySetException(new CoreCallException("relay_401", []));
                if (submit != null) await submit;
                if (window.IsVisible) window.Close();
            }
        });
    }

    [TestMethod]
    public async Task Window_ConnectFailed_CloseWorksAgain()
    {
        var h = new MultiHarness();
        await h.Model.StartAsync();
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("save_relay", _ => gate.Task);
        h.Model.BaseUrlInput = "https://relay.example.com/v1";
        h.Model.ApiKeyInput = "sk-example-0001";

        await UiHost.RunAsync(async () =>
        {
            var window = new SetupWindow(h.Model);
            var closed = false;
            window.Closed += (_, _) => closed = true;
            Task? submit = null;
            try
            {
                await UiHost.ShowAsync(window);
                submit = h.Model.CompleteSetupAsync();
                window.Close();
                gate.SetException(new CoreCallException("relay_401", []));
                await submit;
                Assert.IsFalse(h.Model.IsConnecting);

                window.Close();

                Assert.IsTrue(closed);
            }
            finally
            {
                gate.TrySetException(new CoreCallException("relay_401", []));
                if (submit != null) await submit;
                if (window.IsVisible) window.Close();
            }
        });
    }

    [TestMethod]
    public async Task Window_Connecting_SubmitButtonShowsSpinnerAndTextThenSpinnerGoes()
    {
        var h = new MultiHarness();
        await h.Model.StartAsync();
        var gate = new TaskCompletionSource<JsonElement>();
        h.Core.OnAsync("save_relay", _ => gate.Task);
        h.Model.BaseUrlInput = "https://relay.example.com/v1";
        h.Model.ApiKeyInput = "sk-example-0001";

        await UiHost.RunAsync(async () =>
        {
            var window = new SetupWindow(h.Model);
            Task? submit = null;
            try
            {
                await UiHost.ShowAsync(window);
                Assert.IsFalse(UiHost.All<Spinner>(UiHost.FindButton(window, "完成设置")!).Any());

                submit = h.Model.CompleteSetupAsync();
                await UiHost.Settle();

                var connecting = UiHost.FindButton(window, AppModel.ConnectingText);
                Assert.IsNotNull(connecting);
                Assert.AreEqual("正在连接中转…", AppModel.ConnectingText);
                Assert.IsFalse(connecting.IsEnabled);
                Assert.IsTrue(UiHost.All<Spinner>(connecting).Any(s => s.IsVisible));
                Assert.IsTrue(UiHost.Descendants(window).OfType<Spinner>().Count(s => s.IsVisible) == 1);

                gate.SetException(new CoreCallException("relay_401", []));
                await submit;
                await UiHost.Settle();

                var idle = UiHost.FindButton(window, "完成设置");
                Assert.IsNotNull(idle);
                Assert.IsFalse(UiHost.All<Spinner>(idle).Any());
            }
            finally
            {
                gate.TrySetException(new CoreCallException("relay_401", []));
                if (submit != null) await submit;
                if (window.IsVisible) window.Close();
            }
        });
    }

    [TestMethod]
    public async Task Window_MinimizedThenBringToFront_RestoresToNormalAndVisible()
    {
        var h = new MultiHarness();
        await h.Model.StartAsync();

        await UiHost.RunAsync(async () =>
        {
            var window = new SetupWindow(h.Model);
            try
            {
                await UiHost.ShowAsync(window);
                window.WindowState = WindowState.Minimized;
                await UiHost.Settle();
                Assert.AreEqual(WindowState.Minimized, window.WindowState);

                window.BringToFront();
                await UiHost.Settle();

                Assert.AreEqual(WindowState.Normal, window.WindowState);
                Assert.IsTrue(window.IsVisible);
            }
            finally
            {
                if (window.IsVisible) window.Close();
            }
        });
    }
}
