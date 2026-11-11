using System;
using System.ComponentModel;
using System.Diagnostics;
using System.IO;
using System.Linq;
using System.Threading.Tasks;
using System.Windows;
using ModelBridge.Services;
using ModelBridge.UI;

namespace ModelBridge;

public partial class App : Application
{
    const string AppTitle = "模型接入助手";

    SingleInstance? singleInstance;
    AppModel? model;
    MainWindow? mainWindow;
    SetupWindow? setupWindow;
    TrayMenu? tray;
    bool exiting;

    protected override async void OnStartup(StartupEventArgs e)
    {
        base.OnStartup(e);
        System.Windows.Forms.Application.EnableVisualStyles();
        var uninstall = e.Args.Contains("--uninstall");

        singleInstance = new SingleInstance();
        var acquired = singleInstance.TryAcquire(() => Dispatcher.InvokeAsync(ShowMainWindow));
        if (acquired != SingleInstanceResult.Acquired)
        {
            if (acquired == SingleInstanceResult.ActivationFailed)
                MessageBox.Show(AppModel.ActivationFailedMessage, AppTitle, MessageBoxButton.OK, MessageBoxImage.Warning);
            else if (uninstall)
                MessageBox.Show("模型接入助手正在运行，请先从任务栏通知区域退出它，再卸载。", AppTitle, MessageBoxButton.OK, MessageBoxImage.Warning);
            Shutdown(uninstall ? 1 : 0);
            return;
        }

        var installDir = AppContext.BaseDirectory;
        var localAppData = Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData);
        var chatGptApp = new ChatGptApp();
        model = new AppModel(
            new CoreClient(CoreProcessOptions.ForInstall(installDir, localAppData), Dispatcher),
            new VaultAdapter(new CredentialStore()),
            new SettingsAdapter(new ShellSettings(Path.Combine(localAppData, "ModelBridge"))),
            new LoginAdapter(new LoginItem(installDir)),
            new ChatGptAdapter(chatGptApp, new ChatGptRestarter(chatGptApp)),
            new TaskDialogConfirmer());
        model.ExitRequested += ExitApp;

        if (uninstall)
        {
            Shutdown(model.IsAlreadyCleaned() ? 0 : await RunHeadlessUninstallAsync());
            return;
        }

        mainWindow = new MainWindow(model);
        tray = new TrayMenu(model, ShowMainWindow, UninstallFromMenuAsync);
        model.ShowMainWindowRequested += ShowMainWindow;

        await model.StartAsync();
        if (model.Page == AppPage.Setup) ShowMainWindow();
        else if (!LoginItem.WasLaunchedAtLogin(e.Args)) mainWindow.Show();
    }

    void ShowMainWindow()
    {
        if (exiting || model == null || mainWindow == null) return;
        if (model.Page != AppPage.Main)
        {
            ShowSetupWindow();
            return;
        }

        mainWindow.Show();
        if (mainWindow.WindowState == WindowState.Minimized) mainWindow.WindowState = WindowState.Normal;
        mainWindow.Activate();
    }

    void ShowSetupWindow()
    {
        if (setupWindow == null)
        {
            setupWindow = new SetupWindow(model!);
            setupWindow.Closed += (_, _) =>
            {
                setupWindow = null;
                if (!exiting && model!.Page == AppPage.Main) ShowMainWindow();
            };
        }
        setupWindow.Show();
        setupWindow.Activate();
    }

    async Task<int> RunHeadlessUninstallAsync()
    {
        await model!.StartAsync();
        var code = await RunUninstallAsync();
        if (code == 1 && model.Notice is { } notice)
            MessageBox.Show($"{notice.Title}\n{notice.Detail}", AppTitle, MessageBoxButton.OK, MessageBoxImage.Warning);
        await model.StopCoreAsync();
        return code;
    }

    async Task<int> RunUninstallAsync()
    {
        var code = await model!.UninstallAsync();
        while (code == 2)
        {
            var pause = model.UninstallPause!;
            var choice = Dialogs.Choose(
                "卸载暂停：有设置被手动改过",
                $"安装后，有人手动改过本助手写入的设置项：{string.Join("、", pause.Keys)}。为了不覆盖这些修改，助手没有改动它，也还没有删除任何东西。\n\n请检查 {pause.ConfigPath}，处理后重新检查。",
                ["取消卸载", "打开所在文件夹", "重新检查"]);
            if (choice == 1) ExternalOpen.FolderOf(pause.ConfigPath);
            else if (choice == 2) code = await model.RecheckUninstallAsync();
            else return 2;
        }

        if (code == 0 && model.Uninstalled is { Leftovers.Count: > 0 } outcome)
            MessageBox.Show(
                "以下内容没能自动清理，请手动处理：\n· " + string.Join("\n· ", outcome.Leftovers),
                AppTitle,
                MessageBoxButton.OK,
                MessageBoxImage.Warning);
        return code;
    }

    async Task UninstallFromMenuAsync()
    {
        var code = await RunUninstallAsync();
        if (code != 0)
        {
            if (model!.Notice != null) ShowMainWindow();
            return;
        }

        await model!.StopCoreAsync();
        var uninstaller = Path.Combine(AppContext.BaseDirectory, "unins000.exe");
        tray?.Dispose();
        singleInstance?.Dispose();
        try { Process.Start(new ProcessStartInfo(uninstaller, "/SILENT") { UseShellExecute = false })?.Dispose(); }
        catch (Win32Exception) { }
        ExitApp();
    }

    void ExitApp()
    {
        if (exiting) return;
        exiting = true;
        tray?.Dispose();
        if (mainWindow != null) mainWindow.AllowClose = true;
        singleInstance?.Dispose();
        Shutdown();
    }
}
