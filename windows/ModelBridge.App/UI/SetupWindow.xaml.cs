using System.ComponentModel;
using System.Collections.Generic;
using System.Windows;
using System.Windows.Controls;

namespace ModelBridge.UI;

public partial class SetupWindow : Window
{
    readonly AppModel model;
    bool closing;

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
        SubmitButton.Content = model.IsConnecting ? "正在连接中台…" : "完成设置";

        var errors = new List<UIElement>();
        if (model.TaskError is { } error)
            errors.Add(AlertView.Create(AppSeverity.Danger, error.Title, error.Detail));
        if (model.Notice is { } notice && !done)
            errors.Add(AlertView.Create(notice.Severity, notice.Title, notice.Detail));
        AlertView.Fill(ErrorHost, errors);

        var summary = new List<UIElement> { AlertView.Create(AppSeverity.Success, model.SuccessTitle, model.SuccessText) };
        if (model.Notice is { } warning && done)
            summary.Add(AlertView.Create(warning.Severity, warning.Title, warning.Detail));
        AlertView.Fill(DoneHost, summary);
    }

    async void SubmitClick(object sender, RoutedEventArgs e) => await model.CompleteSetupAsync();

    async void LaterClick(object sender, RoutedEventArgs e) => await model.FinishSetupAsync(false);

    async void RestartClick(object sender, RoutedEventArgs e) => await model.FinishSetupAsync(true);

    protected override void OnClosing(CancelEventArgs e)
    {
        base.OnClosing(e);
        closing = true;
        if (model.Page == AppPage.Success) _ = model.FinishSetupAsync(false);
        else if (model.Page == AppPage.Setup) _ = model.QuitAsync();
    }
}
