using System;
using System.Drawing;
using System.Linq;
using System.Runtime.InteropServices;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Interop;
using Microsoft.Win32;
using ModelBridge.Services;
using Forms = System.Windows.Forms;

namespace ModelBridge.UI;

public sealed class TrayMenu : IDisposable
{
    readonly AppModel model;
    readonly Action openMain;
    readonly Func<Task> uninstall;
    readonly Forms.NotifyIcon icon;
    Icon? iconImage;
    TrayPanelWindow? panel;
    DateTime lastClosed = DateTime.MinValue;
    bool disposed;

    public TrayMenu(AppModel appModel, Action openMainWindow, Func<Task> uninstallApp)
    {
        model = appModel;
        openMain = openMainWindow;
        uninstall = uninstallApp;

        icon = new Forms.NotifyIcon { Text = "模型接入助手", Visible = true };
        UpdateTrayIcon();
        SystemEvents.UserPreferenceChanged += OnUserPreferenceChanged;
        icon.MouseUp += (_, e) =>
        {
            if (e.Button is Forms.MouseButtons.Left or Forms.MouseButtons.Right) Show();
        };
    }

    public void Show()
    {
        if (Dialogs.Current is { } dialog)
        {
            dialog.Activate();
            return;
        }

        // 点托盘图标会先让打开着的面板失去焦点而关闭，随后的鼠标抬起不应又把它打开
        if (disposed || panel != null || DateTime.UtcNow - lastClosed < TimeSpan.FromMilliseconds(300)) return;

        var cursor = Forms.Cursor.Position;
        panel = new TrayPanelWindow(model, cursor, Run);
        panel.Closed += (_, _) =>
        {
            panel = null;
            lastClosed = DateTime.UtcNow;
        };
        panel.Show();
        if (PresentationSource.FromVisual(panel) is HwndSource source) SetForegroundWindow(source.Handle);
    }

    public void Dispose()
    {
        if (disposed) return;
        disposed = true;
        SystemEvents.UserPreferenceChanged -= OnUserPreferenceChanged;
        panel?.Close();
        icon.Visible = false;
        icon.Dispose();
        iconImage?.Dispose();
    }

    async void Run(TrayMenuCommand command)
    {
        try
        {
            await RunCommandAsync(command);
        }
        catch (Exception)
        {
            Dialogs.Show(DialogModel.ForUnexpectedError());
        }
    }

    async Task RunCommandAsync(TrayMenuCommand command)
    {
        switch (command)
        {
            case TrayMenuCommand.ToggleForwarding:
                await ShowIfNewNoticeAsync(model.ToggleForwardingAsync);
                break;
            case TrayMenuCommand.OpenConfig:
                openMain();
                break;
            case TrayMenuCommand.ToggleLoginStart:
                await ShowIfNewNoticeAsync(() => model.SetLoginStartAsync(!model.LoginStartEnabled));
                break;
            case TrayMenuCommand.CheckUpdate:
                if (model.UpdateVersion == null)
                {
                    await ShowIfNewNoticeAsync(model.CheckUpdateAsync);
                }
                else ExternalOpen.Url(model.UpdateUrl);
                break;
            case TrayMenuCommand.Uninstall:
                await uninstall();
                break;
            case TrayMenuCommand.Quit:
                if (model.QuitConfirmRequest() == null) await model.QuitAsync();
                else await Dialogs.ShowQuit(model);
                break;
        }
    }

    async Task ShowIfNewNoticeAsync(Func<Task> action)
    {
        var before = model.Notices.Select(n => n.Id).ToHashSet();
        await action();
        if (model.Notices.Any(n => !before.Contains(n.Id))) openMain();
    }

    void UpdateTrayIcon()
    {
        var size = TaskbarTheme.IconSizeFor(GetDpiForSystem());
        var uri = new Uri($"pack://application:,,,/Assets/{TaskbarTheme.IconFor(TaskbarTheme.IsLight())}");
        using var stream = Application.GetResourceStream(uri).Stream;
        var next = new Icon(stream, new System.Drawing.Size(size, size));
        icon.Icon = next;
        iconImage?.Dispose();
        iconImage = next;
    }

    void OnUserPreferenceChanged(object sender, UserPreferenceChangedEventArgs e)
    {
        if (e.Category is UserPreferenceCategory.General or UserPreferenceCategory.VisualStyle)
            Application.Current?.Dispatcher.BeginInvoke(UpdateTrayIcon);
    }

    [DllImport("user32.dll")]
    static extern uint GetDpiForSystem();

    [DllImport("user32.dll")]
    static extern bool SetForegroundWindow(IntPtr hwnd);
}
