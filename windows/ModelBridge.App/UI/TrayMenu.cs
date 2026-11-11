using System;
using System.ComponentModel;
using System.Drawing;
using System.Runtime.InteropServices;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Controls.Primitives;
using System.Windows.Interop;
using Forms = System.Windows.Forms;

namespace ModelBridge.UI;

public sealed class TrayMenu : IDisposable
{
    readonly AppModel model;
    bool disposed;
    readonly Forms.NotifyIcon icon;
    readonly Icon iconImage;
    readonly ContextMenu menu = new();
    readonly MenuItem status = new() { IsEnabled = false };
    readonly MenuItem forwarding = new() { Header = "转发到中台" };
    readonly MenuItem loginStart = new() { Header = "登录时启动" };
    readonly MenuItem update = new();
    readonly MenuItem remove = new() { Header = "卸载模型接入助手…" };
    readonly MenuItem quit = new() { Header = "退出模型接入助手" };

    public TrayMenu(AppModel appModel, Action openMain, Func<Task> uninstall)
    {
        model = appModel;

        forwarding.Click += async (_, _) =>
        {
            await model.ToggleForwardingAsync();
            ShowNotice(openMain);
        };
        var openConfig = new MenuItem { Header = "打开配置…" };
        openConfig.Click += (_, _) => openMain();
        loginStart.Click += async (_, _) =>
        {
            await model.SetLoginStartAsync(!model.LoginStartEnabled);
            ShowNotice(openMain);
        };
        update.Click += async (_, _) =>
        {
            if (model.UpdateVersion == null)
            {
                await model.CheckUpdateAsync();
                ShowNotice(openMain);
            }
            else ExternalOpen.Url(model.UpdateUrl);
        };
        remove.Click += async (_, _) => await uninstall();
        quit.Click += async (_, _) =>
        {
            await model.QuitAsync();
            ShowNotice(openMain);
        };

        menu.Items.Add(status);
        menu.Items.Add(new Separator());
        menu.Items.Add(forwarding);
        menu.Items.Add(openConfig);
        menu.Items.Add(new Separator());
        menu.Items.Add(loginStart);
        menu.Items.Add(update);
        menu.Items.Add(new Separator());
        menu.Items.Add(remove);
        menu.Items.Add(quit);
        menu.Placement = PlacementMode.AbsolutePoint;
        menu.Opened += (_, _) =>
        {
            if (PresentationSource.FromVisual(menu) is HwndSource source) SetForegroundWindow(source.Handle);
        };

        iconImage = CreateIcon();
        icon = new Forms.NotifyIcon { Icon = iconImage, Text = "模型接入助手", Visible = true };
        icon.MouseUp += (_, e) =>
        {
            if (e.Button is Forms.MouseButtons.Left or Forms.MouseButtons.Right) Show();
        };
        model.PropertyChanged += OnModelChanged;
        Refresh();
    }

    public void Show()
    {
        Refresh();
        using var graphics = Graphics.FromHwnd(IntPtr.Zero);
        var scale = graphics.DpiX / 96.0;
        var cursor = Forms.Cursor.Position;
        menu.HorizontalOffset = cursor.X / scale;
        menu.VerticalOffset = cursor.Y / scale;
        menu.IsOpen = true;
    }

    public void Dispose()
    {
        if (disposed) return;
        disposed = true;
        model.PropertyChanged -= OnModelChanged;
        icon.Visible = false;
        icon.Dispose();
        iconImage.Dispose();
    }

    void OnModelChanged(object? sender, PropertyChangedEventArgs e) => Refresh();

    void ShowNotice(Action openMain)
    {
        if (model.Notice != null) openMain();
    }

    void Refresh()
    {
        status.Header = model.TrayStatusText;
        forwarding.IsChecked = model.ForwardingEnabled;
        forwarding.IsEnabled = model.ToggleEnabled;
        loginStart.IsChecked = model.LoginStartEnabled;
        loginStart.IsEnabled = !model.IsBusy;
        update.IsEnabled = !model.IsBusy;
        remove.IsEnabled = !model.IsBusy;
        quit.IsEnabled = !model.IsBusy;
        update.Header = model.UpdateVersion == null ? "检查更新" : $"发现新版本 {model.UpdateVersion} · 打开下载页";
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
