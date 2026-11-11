using System;
using System.Drawing;
using System.Runtime.InteropServices;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Interop;
using Forms = System.Windows.Forms;

namespace ModelBridge.UI;

public sealed class TrayMenu : IDisposable
{
    readonly AppModel model;
    readonly Action openMain;
    readonly Func<Task> uninstall;
    readonly Forms.NotifyIcon icon;
    readonly Icon iconImage;
    TrayPanelWindow? panel;
    DateTime lastClosed = DateTime.MinValue;
    bool disposed;

    public TrayMenu(AppModel appModel, Action openMainWindow, Func<Task> uninstallApp)
    {
        model = appModel;
        openMain = openMainWindow;
        uninstall = uninstallApp;

        iconImage = CreateIcon();
        icon = new Forms.NotifyIcon { Icon = iconImage, Text = "模型接入助手", Visible = true };
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
        panel?.Close();
        icon.Visible = false;
        icon.Dispose();
        iconImage.Dispose();
    }

    async void Run(TrayMenuCommand command)
    {
        switch (command)
        {
            case TrayMenuCommand.ToggleForwarding:
                await model.ToggleForwardingAsync();
                ShowNotice();
                break;
            case TrayMenuCommand.OpenConfig:
                openMain();
                break;
            case TrayMenuCommand.ToggleLoginStart:
                await model.SetLoginStartAsync(!model.LoginStartEnabled);
                ShowNotice();
                break;
            case TrayMenuCommand.CheckUpdate:
                if (model.UpdateVersion == null)
                {
                    await model.CheckUpdateAsync();
                    ShowNotice();
                }
                else ExternalOpen.Url(model.UpdateUrl);
                break;
            case TrayMenuCommand.Uninstall:
                await uninstall();
                break;
            case TrayMenuCommand.Quit:
                await model.QuitAsync();
                ShowNotice();
                break;
        }
    }

    void ShowNotice()
    {
        if (model.Notice != null) openMain();
    }

    static Icon CreateIcon()
    {
        using var bitmap = new Bitmap(32, 32);
        using (var g = Graphics.FromImage(bitmap))
        {
            g.SmoothingMode = System.Drawing.Drawing2D.SmoothingMode.AntiAlias;
            using var fill = new SolidBrush(ColorTranslator.FromHtml("#0F766E"));
            g.FillEllipse(fill, 2, 2, 28, 28);
            using var ring = new Pen(Color.White, 3);
            g.DrawEllipse(ring, 9, 9, 14, 14);
        }
        var handle = bitmap.GetHicon();
        using var temp = Icon.FromHandle(handle);
        var copy = (Icon)temp.Clone();
        DestroyIcon(handle);
        return copy;
    }

    [DllImport("user32.dll")]
    static extern bool SetForegroundWindow(IntPtr hwnd);

    [DllImport("user32.dll")]
    static extern bool DestroyIcon(IntPtr handle);
}
