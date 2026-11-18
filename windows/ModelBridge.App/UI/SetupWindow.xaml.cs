using System.ComponentModel;
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Interop;

namespace ModelBridge.UI;

public partial class SetupWindow : Window
{
    const uint ScClose = 0xF060;
    const uint MfGrayed = 1;

    [DllImport("user32.dll")]
    static extern IntPtr GetSystemMenu(IntPtr hWnd, bool bRevert);

    [DllImport("user32.dll")]
    static extern bool EnableMenuItem(IntPtr hMenu, uint uIDEnableItem, uint uEnable);

    readonly AppModel model;
    bool closing;
    bool submitShowsConnecting;
    object? shownNotices;
    object? shownTaskError;

    public SetupWindow(AppModel appModel)
    {
        model = appModel;
        InitializeComponent();
        Form.Bind(model);
        model.PropertyChanged += OnModelChanged;
        Closed += (_, _) => model.PropertyChanged -= OnModelChanged;
        SourceInitialized += (_, _) => SetCloseEnabled(!model.IsConnecting);
        Refresh();
    }

    void OnModelChanged(object? sender, PropertyChangedEventArgs e) => Refresh();

    void OverlaySizeChanged(object sender, SizeChangedEventArgs e) => AlertHost.Width = System.Math.Max(0, Overlay.ActualWidth - 48);

    void Refresh()
    {
        if (model.Page == AppPage.Main)
        {
            if (!closing) Close();
            return;
        }

        var done = model.Page == AppPage.Success;
        FormPanel.Visibility = done ? Visibility.Collapsed : Visibility.Visible;
        DonePanel.Visibility = done ? Visibility.Visible : Visibility.Collapsed;
        SubmitButton.IsEnabled = !model.IsConnecting;
        SetCloseEnabled(!model.IsConnecting);
        if (submitShowsConnecting != model.IsConnecting)
        {
            submitShowsConnecting = model.IsConnecting;
            SubmitButton.Content = model.IsConnecting ? DialogWindow.BusyContent(AppModel.ConnectingText) : "完成设置";
        }

        DoneTitle.Text = model.SuccessTitle;
        DoneText.Text = model.SuccessText;

        if (ReferenceEquals(shownNotices, model.Notices) && ReferenceEquals(shownTaskError, model.TaskError)) return;
        shownNotices = model.Notices;
        shownTaskError = model.TaskError;
        var alerts = new List<UIElement>();
        if (model.TaskError is { } error)
            alerts.Add(AlertView.Create(AppSeverity.Danger, error.Title, error.Detail, model.DismissTaskError));
        foreach (var notice in model.Notices)
            alerts.Add(notice.DetailInDialog
                ? AlertView.Create(notice.Severity, notice.Title, "", () => model.DismissNotice(notice.Id),
                    ("查看详情", () => Dialogs.Show(DialogModel.ForInfo(AppModel.SettingsUnreadable, notice.Detail)), false))
                : AlertView.Create(notice.Severity, notice.Title, notice.Detail, () => model.DismissNotice(notice.Id)));
        AlertView.Fill(AlertHost, alerts);
    }

    async void SubmitClick(object sender, RoutedEventArgs e) => await model.CompleteSetupAsync();

    void FinishClick(object sender, RoutedEventArgs e) => model.FinishSetup();

    internal void BringToFront()
    {
        if (WindowState == WindowState.Minimized) WindowState = WindowState.Normal;
        Show();
        Activate();
    }

    void SetCloseEnabled(bool enabled)
    {
        var handle = new WindowInteropHelper(this).Handle;
        if (handle == IntPtr.Zero) return;
        EnableMenuItem(GetSystemMenu(handle, false), ScClose, enabled ? 0u : MfGrayed);
    }

    protected override void OnClosing(CancelEventArgs e)
    {
        base.OnClosing(e);
        if (closing) return;
        if (model.IsConnecting)
        {
            e.Cancel = true;
            return;
        }
        if (model.Page == AppPage.Setup && model.QuitConfirmRequest() != null)
        {
            e.Cancel = true;
            closing = Dialogs.ShowQuit(model).Result;
            return;
        }

        closing = true;
        if (model.Page == AppPage.Success) model.FinishSetup();
        else if (model.Page == AppPage.Setup) _ = model.QuitAsync();
    }
}
