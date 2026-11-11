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
    object? shownNotice;
    string? shownProgress;

    public MainWindow(AppModel appModel)
    {
        model = appModel;
        InitializeComponent();
        Form.Bind(model);
        model.PropertyChanged += (_, _) => Refresh();
        Refresh();
    }

    public bool AllowClose { get; set; }

    void Refresh()
    {
        ForwardToggle.SetCurrentValue(ToggleButton.IsCheckedProperty, model.ForwardingEnabled);
        ForwardToggle.IsEnabled = model.ToggleEnabled;
        ForwardHelp.Text = !model.ChatGptFound
            ? "找到 ChatGPT 后才能打开。"
            : model.ForwardingEnabled
                ? "开启时，Codex 的模型列表里会多出中台模型；选官方模型时仍然访问官方服务。"
                : "已关闭：Codex 配置已还原为开启前的样子，只显示官方模型。";
        SaveButton.IsEnabled = !model.IsBusy;
        ChatGptValue.Text = model.ChatGptText;
        RelayValue.Text = model.RelayStatusText;

        VersionText.Text = model.Version;
        StatusDot.Fill = AlertView.Brush(model.CoreRunning ? "SuccessBrush" : "DangerBrush");
        CoreText.Text = model.CoreStatusText + (model.ForwardingEnabled || !model.CoreRunning ? "" : " · 转发已关闭");
        UpdateText.Text = model.UpdateVersion == null ? "" : $"发现新版本 {model.UpdateVersion}";
        UpdateButton.Content = model.UpdateVersion == null ? "检查更新" : "打开下载页";

        if (ReferenceEquals(shownBanners, model.Banners) && ReferenceEquals(shownNotice, model.Notice) && shownProgress == model.ProgressText)
            return;
        shownBanners = model.Banners;
        shownNotice = model.Notice;
        shownProgress = model.ProgressText;
        AlertView.Fill(AlertHost, BuildAlerts());
    }

    List<UIElement> BuildAlerts()
    {
        var alerts = new List<UIElement>();
        if (model.ProgressText is { } progress)
            alerts.Add(AlertView.Create(AppSeverity.Info, progress, "完成前请不要关闭本窗口。", true));
        if (model.Notice is { } notice)
            alerts.Add(AlertView.Create(notice.Severity, notice.Title, notice.Detail, false, ("知道了", model.ClearNotice)));
        foreach (var banner in model.Banners)
            alerts.Add(AlertView.Create(
                banner.Severity,
                banner.Title,
                banner.Detail,
                false,
                banner.Actions.Select(a => (Label(a), (Action)(() => Run(a)))).ToArray()));
        return alerts;
    }

    static string Label(BannerAction action) => action switch
    {
        BannerAction.Retry => "重试",
        BannerAction.OpenConfigFolder => "打开所在文件夹",
        BannerAction.Recheck => "重新检查",
        _ => "重新检测",
    };

    void Run(BannerAction action)
    {
        switch (action)
        {
            case BannerAction.Retry: _ = model.RetryAsync(); break;
            case BannerAction.OpenConfigFolder: ExternalOpen.FolderOf(model.ConfigPath); break;
            default: _ = model.RecheckAsync(); break;
        }
    }

    async void ForwardClick(object sender, RoutedEventArgs e)
    {
        ForwardToggle.SetCurrentValue(ToggleButton.IsCheckedProperty, model.ForwardingEnabled);
        await model.ToggleForwardingAsync();
        ForwardToggle.SetCurrentValue(ToggleButton.IsCheckedProperty, model.ForwardingEnabled);
    }

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
