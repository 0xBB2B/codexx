using System;
using System.Linq;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;

namespace ModelBridge.UI;

public partial class DialogWindow : Window
{
    DialogModel model;
    Button? defaultButton;

    public DialogWindow(DialogModel dialogModel)
    {
        model = dialogModel;
        InitializeComponent();
        Width = model.Width + 96;
        Apply(model);

        Loaded += (_, _) =>
        {
            Activate();
            defaultButton?.Focus();
        };
    }

    bool forceClose;

    public void ForceClose()
    {
        forceClose = true;
        Close();
    }

    protected override void OnClosing(System.ComponentModel.CancelEventArgs e)
    {
        base.OnClosing(e);
        if (!forceClose && model.Buttons.Any(b => b.IsBusy)) e.Cancel = true;
    }

    public Func<int, Task>? ButtonHandler { get; set; }

    public void Apply(DialogModel next)
    {
        model = next;
        Title = model.Title;
        TitleText.Text = model.Title;
        MessageText.Text = model.Message;
        MessageText.Visibility = model.Message == "" ? Visibility.Collapsed : Visibility.Visible;
        if (model.Alert is { } alert)
        {
            AlertHost.Content = AlertView.Create(alert.Severity switch { DialogAlertSeverity.Danger => AppSeverity.Danger, _ => throw new ArgumentOutOfRangeException() }, alert.Title, alert.Message);
            AlertHost.Visibility = Visibility.Visible;
        }
        Result = model.EscapeButtonIndex;

        ButtonRow.Children.Clear();
        defaultButton = null;
        for (var i = 0; i < model.Buttons.Count; i++)
        {
            var spec = model.Buttons[i];
            var index = i;
            var button = new Button
            {
                Content = spec.IsBusy ? BusyContent(spec.Text) : spec.Text,
                Style = (Style)FindResource(spec.Role == DialogButtonRole.Primary ? "PrimaryButton" : "SecondaryButton"),
                IsDefault = spec.IsDefault,
                IsEnabled = spec.IsEnabled,
                Margin = new Thickness(i == 0 ? 0 : 12, 0, 0, 0),
            };
            button.Click += async (_, _) =>
            {
                if (ButtonHandler != null)
                {
                    await ButtonHandler(index);
                    return;
                }
                Result = index;
                Close();
            };
            if (spec.IsDefault) defaultButton = button;
            ButtonRow.Children.Add(button);
        }
    }

    internal static StackPanel BusyContent(string text)
    {
        var panel = new StackPanel { Orientation = Orientation.Horizontal };
        panel.Children.Add(new Spinner { Margin = new Thickness(0, 0, 8, 0), VerticalAlignment = VerticalAlignment.Center });
        panel.Children.Add(new TextBlock { Text = text, VerticalAlignment = VerticalAlignment.Center });
        return panel;
    }

    public int Result { get; private set; }

    protected override void OnPreviewKeyDown(KeyEventArgs e)
    {
        base.OnPreviewKeyDown(e);
        if (e.Key != Key.Escape) return;
        e.Handled = true;
        if (!model.Buttons[model.EscapeButtonIndex].IsEnabled || model.Buttons.Any(b => b.IsBusy)) return;
        if (ButtonHandler != null) _ = ButtonHandler(model.EscapeButtonIndex);
        else Close();
    }
}
