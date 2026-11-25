using System;
using System.Collections.Generic;
using System.Linq;
using System.ComponentModel;
using System.Runtime.InteropServices;
using System.Windows;
using System.Windows.Interop;
using System.Windows.Automation;
using System.Windows.Controls;
using System.Windows.Controls.Primitives;

namespace ModelBridge.UI;

public partial class MainWindow : Window
{
    readonly AppModel model;
    object? shownBanners;
    object? shownNotices;
    bool shownRechecking;

    public MainWindow(AppModel appModel)
    {
        model = appModel;
        InitializeComponent();
        model.PropertyChanged += (_, _) => Refresh();
        Refresh();
    }

    [DllImport("dwmapi.dll")]
    static extern int DwmSetWindowAttribute(IntPtr hwnd, int attribute, ref int value, int size);

    protected override void OnSourceInitialized(EventArgs e)
    {
        base.OnSourceInitialized(e);
        var round = 2;
        try { DwmSetWindowAttribute(new WindowInteropHelper(this).Handle, 33, ref round, sizeof(int)); }
        catch (Exception ex) when (ex is DllNotFoundException or EntryPointNotFoundException) { }
    }

    public bool AllowClose { get; set; }

    void OverlaySizeChanged(object sender, SizeChangedEventArgs e) => AlertHost.Width = System.Math.Max(0, Overlay.ActualWidth - 48);

    void Refresh()
    {
        ForwardToggle.SetCurrentValue(ToggleButton.IsCheckedProperty, model.ForwardingEnabled);
        ForwardToggle.IsEnabled = model.ToggleEnabled;
        Hero.Background = AlertView.Brush(model.ForwardingEnabled ? "SelectedBgBrush" : "SurfaceBrush");
        Hero.BorderBrush = AlertView.Brush(model.ForwardingEnabled ? "PrimaryBrush" : "DividerBrush");
        HeroTitle.Text = model.OverviewTitle;
        HeroSub.Text = model.ChatGptFound && !model.ChatGptUnsupported ? model.OverviewText ?? model.ForwardingHelpText : model.ForwardingHelpText;
        HeroSub.ToolTip = HeroSub.Text;
        RelayOnlyToggle.SetCurrentValue(ToggleButton.IsCheckedProperty, model.RelayOnlyEnabled);
        RelayOnlyToggle.IsEnabled = model.RelayOnlySwitchUsable;
        AutomationProperties.SetHelpText(RelayOnlyToggle, HeroSub.Text);
        ModeRow.Visibility = model.AccessModeName != null ? Visibility.Visible : Visibility.Collapsed;
        AccessName.Text = model.AccessModeName ?? "";
        AccessReason.Text = model.AccessReasonText ?? "";
        RecheckAccessButton.IsEnabled = !model.IsDetectingAccess;
        AccessSpinner.Visibility = model.IsDetectingAccess ? Visibility.Visible : Visibility.Collapsed;
        RecheckAccessText.Text = model.IsDetectingAccess ? "正在检测…" : "重新检测";

        if (!ReferenceEquals(RelayList.ItemsSource, model.Relays)) RelayList.ItemsSource = model.Relays;
        AddRelayButton.IsEnabled = model.CanAddRelay;
        RelayFootText.Text = model.RelayMinimumText ?? model.RelayCountText;

        VersionText.Text = model.Version;
        StatusDot.Fill = AlertView.Brush(model.CoreRunning ? "SuccessBrush" : "DangerBrush");
        CoreText.Text = model.CoreStatusText;
        SetStatusGlyph(ChatGptGlyph, model.ChatGptFound && !model.ChatGptUnsupported);
        ChatGptValue.Text = model.ChatGptText;
        SetStatusGlyph(RelayGlyph, model.ForwardingEnabled);
        var summary = model.RelayModelSummary;
        var space = summary.IndexOf(' ');
        RelayLabel.Text = summary[..space];
        var rest = summary[(space + 1)..];
        var note = rest.IndexOf('（');
        RelayValue.Text = note < 0 ? rest : rest[..note];
        RelayNote.Text = note < 0 ? "" : rest[note..];
        RelayNote.Margin = new Thickness(note < 0 ? 0 : 4, 0, 0, 0);
        UpdateButton.Content = model.UpdateVersion == null ? "检查更新" : $"新版本 {model.UpdateVersion}";
        AutomationProperties.SetName(UpdateButton, model.UpdateVersion == null ? "检查更新" : $"发现新版本 {model.UpdateVersion}，{(model.UpdateIsInstaller ? "下载安装包" : "打开下载页")}");

        if (ReferenceEquals(shownBanners, model.Banners) && ReferenceEquals(shownNotices, model.Notices) && shownRechecking == model.IsRechecking)
            return;
        Rebuild();
    }

    static void SetStatusGlyph(TextBlock glyph, bool ok)
    {
        glyph.Text = ok ? "\uE73E" : "\uE738";
        glyph.Foreground = AlertView.Brush(ok ? "SuccessBrush" : "TextMutedBrush");
    }

    void Rebuild()
    {
        shownBanners = model.Banners;
        shownNotices = model.Notices;
        shownRechecking = model.IsRechecking;
        AlertView.Fill(AlertHost, BuildAlerts());
    }

    List<UIElement> BuildAlerts()
    {
        var alerts = new List<UIElement>();
        foreach (var notice in model.Notices)
            alerts.Add(AlertView.Create(notice.Severity, notice.Title, notice.Detail, () => model.DismissNotice(notice.Id)));
        foreach (var banner in model.Banners)
            alerts.Add(AlertView.Create(
                banner.Severity,
                banner.Title,
                banner.Detail,
                () => model.DismissBanner(banner),
                banner.Actions.Select(a => (Label(a, model.IsRechecking), (Action)(() => Run(a, banner)), a == BannerAction.Redetect && model.IsRechecking)).ToArray()));
        return alerts;
    }

    static string Label(BannerAction action, bool rechecking) => action switch
    {
        BannerAction.Retry => "重试",
        BannerAction.ShowDetail => "查看详情",
        _ => rechecking ? "正在检测…" : "重新检测",
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

    void MinimizeClick(object sender, RoutedEventArgs e) => WindowState = WindowState.Minimized;

    void CloseClick(object sender, RoutedEventArgs e) => Close();

    void AddRelayClick(object sender, RoutedEventArgs e)
    {
        if (model.BeginAddRelay() is { } editor) new RelayFormWindow(editor).ShowForm(this);
    }

    void EditClick(object sender, RoutedEventArgs e)
    {
        if (RowOf(sender) is { } row && model.BeginEditRelay(row.Id) is { } editor) new RelayFormWindow(editor).ShowForm(this);
    }

    void DeleteClick(object sender, RoutedEventArgs e)
    {
        if (RowOf(sender) is { } row) Dialogs.ShowDeleteRelay(model, row, this);
    }

    async void RetryClick(object sender, RoutedEventArgs e)
    {
        if (RowOf(sender) is { } row) await model.RetryRelayAsync(row.Id);
    }

    static RelayRow? RowOf(object sender) => (sender as FrameworkElement)?.DataContext as RelayRow;

    async void ForwardClick(object sender, RoutedEventArgs e)
    {
        ForwardToggle.SetCurrentValue(ToggleButton.IsCheckedProperty, model.ForwardingEnabled);
        await model.ToggleForwardingAsync();
        ForwardToggle.SetCurrentValue(ToggleButton.IsCheckedProperty, model.ForwardingEnabled);
    }

    async void RelayOnlyClick(object sender, RoutedEventArgs e)
    {
        await model.SetRelayOnlyAsync(!model.RelayOnlyEnabled);
        RelayOnlyToggle.SetCurrentValue(ToggleButton.IsCheckedProperty, model.RelayOnlyEnabled);
    }

    async void RecheckAccessClick(object sender, RoutedEventArgs e) => await model.RecheckAccessAsync();

    async void UpdateClick(object sender, RoutedEventArgs e)
    {
        if (model.UpdateVersion == null) await model.CheckUpdateAsync();
        else await model.OpenUpdateAsync();
    }

    protected override void OnClosing(CancelEventArgs e)
    {
        base.OnClosing(e);
        if (AllowClose) return;
        e.Cancel = true;
        Hide();
    }
}
