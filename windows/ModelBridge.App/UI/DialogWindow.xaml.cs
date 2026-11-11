using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;

namespace ModelBridge.UI;

public partial class DialogWindow : Window
{
    readonly DialogModel model;
    Button? defaultButton;

    public DialogWindow(DialogModel dialogModel)
    {
        model = dialogModel;
        InitializeComponent();
        Width = model.Width + 96;
        Title = model.Title;
        TitleText.Text = model.Title;
        MessageText.Text = model.Message;
        if (model.Message == "") MessageText.Visibility = Visibility.Collapsed;
        if (model.Alert is { } alert)
        {
            AlertHost.Content = AlertView.Create(alert.Severity switch { DialogAlertSeverity.Danger => AppSeverity.Danger, _ => throw new System.ArgumentOutOfRangeException() }, alert.Title, alert.Message);
            AlertHost.Visibility = Visibility.Visible;
        }
        Result = model.EscapeButtonIndex;

        for (var i = 0; i < model.Buttons.Count; i++)
        {
            var spec = model.Buttons[i];
            var index = i;
            var button = new Button
            {
                Content = spec.Text,
                Style = (Style)FindResource(spec.Role == DialogButtonRole.Primary ? "PrimaryButton" : "SecondaryButton"),
                IsDefault = spec.IsDefault,
                Margin = new Thickness(i == 0 ? 0 : 12, 0, 0, 0),
            };
            button.Click += (_, _) =>
            {
                Result = index;
                Close();
            };
            if (spec.IsDefault) defaultButton = button;
            ButtonRow.Children.Add(button);
        }

        Loaded += (_, _) =>
        {
            Activate();
            defaultButton?.Focus();
        };
    }

    public int Result { get; private set; }

    protected override void OnPreviewKeyDown(KeyEventArgs e)
    {
        base.OnPreviewKeyDown(e);
        if (e.Key != Key.Escape) return;
        Result = model.EscapeButtonIndex;
        e.Handled = true;
        Close();
    }
}
