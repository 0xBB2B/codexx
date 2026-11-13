using System;
using System.Collections.Generic;
using System.Linq;
using System.ComponentModel;
using System.Windows;
using System.Windows.Controls.Primitives;

namespace ModelBridge.UI;

public partial class MainWindow : Window
{
    readonly AppModel model;
    object? shownBanners;
    object? shownNotices;
    string? shownProgress;
    string? dismissedProgress;

    public MainWindow(AppModel appModel)
    {
        model = appModel;
        InitializeComponent();
        Form.Bind(model);
        model.PropertyChanged += (_, _) => Refresh();
        Refresh();
    }

    public bool AllowClose { get; set; }

    void OverlaySizeChanged(object sender, SizeChangedEventArgs e) => AlertHost.Width = System.Math.Max(0, Overlay.ActualWidth - 48);

    void Refresh()
    {
        ForwardToggle.SetCurrentValue(ToggleButton.IsCheckedProperty, model.ForwardingEnabled);
        ForwardToggle.IsEnabled = model.ToggleEnabled;
        ForwardHelp.Text = model.ForwardingHelpText;
        var accessVisible = model.AccessModeName != null ? Visibility.Visible : Visibility.Collapsed;
        AccessLabel.Visibility = accessVisible;
        AccessPanel.Visibility = accessVisible;
        AccessName.Text = model.AccessModeName ?? "";
        AccessReason.Text = model.AccessReasonText ?? "";
        AccessHelp.Text = model.AccessHelpText ?? "";
        RecheckAccessButton.IsEnabled = !model.IsDetectingAccess;
        AccessSpinner.Visibility = model.IsDetectingAccess ? Visibility.Visible : Visibility.Collapsed;
        RecheckAccessText.Text = model.IsDetectingAccess ? "正在检测…" : "重新检测";
        SaveButton.IsEnabled = !model.IsBusy;
        ChatGptValue.Text = model.ChatGptText;
        RelayValue.Text = model.RelayStatusText;

        VersionText.Text = model.Version;
        StatusDot.Fill = AlertView.Brush(model.CoreRunning ? "SuccessBrush" : "DangerBrush");
        CoreText.Text = model.CoreStatusText + (model.ForwardingEnabled || !model.CoreRunning ? "" : " · 转发已关闭");
        UpdateText.Text = model.UpdateVersion == null ? "" : $"发现新版本 {model.UpdateVersion}";
        UpdateButton.Content = model.UpdateVersion == null ? "检查更新" : "打开下载页";

        if (model.ProgressText == null) dismissedProgress = null;
        if (ReferenceEquals(shownBanners, model.Banners) && ReferenceEquals(shownNotices, model.Notices) && shownProgress == model.ProgressText)
            return;
        Rebuild();
    }

    void Rebuild()
    {
        shownBanners = model.Banners;
        shownNotices = model.Notices;
        shownProgress = model.ProgressText;
        AlertView.Fill(AlertHost, BuildAlerts());
    }

    List<UIElement> BuildAlerts()
    {
        var alerts = new List<UIElement>();
        if (model.ProgressText is { } progress && progress != dismissedProgress)
            alerts.Add(AlertView.Create(AppSeverity.Info, progress, "完成前请不要关闭本窗口。", () =>
            {
                dismissedProgress = progress;
                Rebuild();
            }, true));
        foreach (var notice in model.Notices)
            alerts.Add(AlertView.Create(notice.Severity, notice.Title, notice.Detail, () => model.DismissNotice(notice.Id)));
        foreach (var banner in model.Banners)
            alerts.Add(AlertView.Create(
                banner.Severity,
                banner.Title,
                banner.Detail,
                () => model.DismissBanner(banner),
                false,
                banner.Actions.Select(a => (Label(a), (Action)(() => Run(a, banner)))).ToArray()));
        return alerts;
    }

    static string Label(BannerAction action) => action switch
    {
        BannerAction.Retry => "重试",
        BannerAction.ShowDetail => "查看详情",
        _ => "重新检测",
    };

    void Run(BannerAction action, AppBanner banner)
    {
        switch (action)
        {
            case BannerAction.Retry: _ = model.RetryAsync(); break;
            case BannerAction.ShowDetail: RestoreHelp.Show(model, banner); break;
            default: _ = model.RecheckAsync(); break;
        }
    }

    async void ForwardClick(object sender, RoutedEventArgs e)
    {
        ForwardToggle.SetCurrentValue(ToggleButton.IsCheckedProperty, model.ForwardingEnabled);
        await model.ToggleForwardingAsync();
        ForwardToggle.SetCurrentValue(ToggleButton.IsCheckedProperty, model.ForwardingEnabled);
    }

    async void RecheckAccessClick(object sender, RoutedEventArgs e) => await model.RecheckAccessAsync();

    async void SaveClick(object sender, RoutedEventArgs e) => await model.SaveConnectionAsync();

    async void UpdateClick(object sender, RoutedEventArgs e)
    {
        if (model.UpdateVersion == null) await model.CheckUpdateAsync();
        else ExternalOpen.Url(model.UpdateUrl);
    }

    protected override void OnClosing(CancelEventArgs e)
    {
        base.OnClosing(e);
        if (AllowClose) return;
        e.Cancel = true;
        Hide();
    }
}
