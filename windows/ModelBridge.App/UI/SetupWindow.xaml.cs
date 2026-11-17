using System.ComponentModel;
using System.Collections.Generic;
using System.Windows;
using System.Windows.Controls;

namespace ModelBridge.UI;

public partial class SetupWindow : Window
{
    readonly AppModel model;
    bool closing;
    object? shownNotices;
    object? shownTaskError;

    public SetupWindow(AppModel appModel)
    {
        model = appModel;
        InitializeComponent();
        Form.Bind(model);
        model.PropertyChanged += OnModelChanged;
        Closed += (_, _) => model.PropertyChanged -= OnModelChanged;
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
        SubmitButton.Content = model.IsConnecting ? AppModel.ConnectingText : "完成设置";

        DoneTitle.Text = model.SuccessTitle;
        DoneText.Text = model.SuccessText;

        if (ReferenceEquals(shownNotices, model.Notices) && ReferenceEquals(shownTaskError, model.TaskError)) return;
        shownNotices = model.Notices;
        shownTaskError = model.TaskError;
        var alerts = new List<UIElement>();
        if (model.TaskError is { } error)
            alerts.Add(AlertView.Create(AppSeverity.Danger, error.Title, error.Detail, model.DismissTaskError));
        foreach (var notice in model.Notices)
            alerts.Add(AlertView.Create(notice.Severity, notice.Title, notice.Detail, () => model.DismissNotice(notice.Id)));
        AlertView.Fill(AlertHost, alerts);
    }

    async void SubmitClick(object sender, RoutedEventArgs e) => await model.CompleteSetupAsync();

    void FinishClick(object sender, RoutedEventArgs e) => model.FinishSetup();

    protected override void OnClosing(CancelEventArgs e)
    {
        base.OnClosing(e);
        closing = true;
        if (model.Page == AppPage.Success) model.FinishSetup();
        else if (model.Page == AppPage.Setup) _ = model.QuitAsync();
    }
}
