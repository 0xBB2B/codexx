using System.ComponentModel;
using System.Windows;
using System.Windows.Controls;

namespace ModelBridge.UI;

public interface IRelayFields : INotifyPropertyChanged
{
    string NameInput { get; set; }
    string BaseUrlInput { get; set; }
    string ApiKeyInput { get; set; }
    string? NameError { get; }
    string? BaseUrlError { get; }
    string? ApiKeyError { get; }
    string ApiKeyHelp { get; }
    bool FieldsEnabled { get; }
}

public partial class ConnectionForm : UserControl
{
    IRelayFields? model;
    PropertyChangedEventHandler? handler;

    public ConnectionForm()
    {
        InitializeComponent();
        Loaded += (_, _) => Subscribe();
        Unloaded += (_, _) => Unsubscribe();
    }

    public void Bind(IRelayFields fields)
    {
        Unsubscribe();
        model = fields;
        if (IsLoaded) Subscribe();
        Refresh();
    }

    void Subscribe()
    {
        if (model == null || handler != null) return;
        handler = (_, _) => Refresh();
        model.PropertyChanged += handler;
        Refresh();
    }

    void Unsubscribe()
    {
        if (model != null && handler != null) model.PropertyChanged -= handler;
        handler = null;
    }

    void Refresh()
    {
        if (model == null) return;

        if (NameBox.Text != model.NameInput) NameBox.Text = model.NameInput;
        KeyHelp.Text = model.ApiKeyHelp;
        if (UrlBox.Text != model.BaseUrlInput) UrlBox.Text = model.BaseUrlInput;
        if (KeyBox.Password != model.ApiKeyInput) KeyBox.Password = model.ApiKeyInput;
        if (KeyText.Text != model.ApiKeyInput) KeyText.Text = model.ApiKeyInput;
        UrlHint.Visibility = UrlBox.Text == "" ? Visibility.Visible : Visibility.Collapsed;

        ShowError(NameBox, NameHelp, NameError, NameErrorText, model.NameError);
        ShowError(UrlBox, UrlHelp, UrlError, UrlErrorText, model.BaseUrlError);
        ShowError(KeyBox, KeyHelp, KeyError, KeyErrorText, model.ApiKeyError);
        ShowError(KeyText, KeyHelp, KeyError, KeyErrorText, model.ApiKeyError);
        IsEnabled = model.FieldsEnabled;
    }

    static void ShowError(Control box, UIElement help, UIElement error, TextBlock errorText, string? message)
    {
        errorText.Text = message ?? "";
        errorText.ToolTip = message;
        System.Windows.Automation.AutomationProperties.SetHelpText(box, message ?? "");
        error.Visibility = message == null ? Visibility.Collapsed : Visibility.Visible;
        help.Visibility = message == null ? Visibility.Visible : Visibility.Collapsed;
        FieldState.SetHasError(box, message != null);
    }

    void NameChanged(object sender, TextChangedEventArgs e)
    {
        if (model != null) model.NameInput = NameBox.Text;
    }

    public void FocusFirst() => NameBox.Focus();

    void UrlChanged(object sender, TextChangedEventArgs e)
    {
        UrlHint.Visibility = UrlBox.Text == "" ? Visibility.Visible : Visibility.Collapsed;
        if (model != null) model.BaseUrlInput = UrlBox.Text;
    }

    void KeyPasswordChanged(object sender, RoutedEventArgs e)
    {
        if (model != null) model.ApiKeyInput = KeyBox.Password;
    }

    void KeyTextChanged(object sender, TextChangedEventArgs e)
    {
        if (model != null) model.ApiKeyInput = KeyText.Text;
    }

    void KeyToggleClick(object sender, RoutedEventArgs e)
    {
        var show = KeyToggle.IsChecked == true;
        KeyText.Visibility = show ? Visibility.Visible : Visibility.Collapsed;
        KeyBox.Visibility = show ? Visibility.Collapsed : Visibility.Visible;
        System.Windows.Automation.AutomationProperties.SetName(KeyToggle, show ? "隐藏 API Key" : "显示 API Key");
    }
}
