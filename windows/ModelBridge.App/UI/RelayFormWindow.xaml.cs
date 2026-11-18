using System.ComponentModel;
using System.Collections.Generic;
using System.Windows;
using System.Windows.Input;

namespace ModelBridge.UI;

public partial class RelayFormWindow : Window
{
    readonly RelayEditorModel editor;
    object? shownAlert;

    public RelayFormWindow(RelayEditorModel relayEditor)
    {
        editor = relayEditor;
        InitializeComponent();
        TitleText.Text = editor.Title;
        Title = editor.Title;
        Form.Bind(editor);
        editor.PropertyChanged += OnEditorChanged;
        Closed += (_, _) => editor.PropertyChanged -= OnEditorChanged;
        Loaded += (_, _) =>
        {
            Activate();
            Form.FocusFirst();
        };
        Refresh();
    }

    public void ShowForm(Window owner)
    {
        Owner = owner;
        WindowStartupLocation = WindowStartupLocation.CenterOwner;
        Dialogs.Current = this;
        try { ShowDialog(); }
        finally { Dialogs.Current = null; }
    }

    void OnEditorChanged(object? sender, PropertyChangedEventArgs e) => Refresh();

    void OverlaySizeChanged(object sender, SizeChangedEventArgs e) => AlertHost.Width = System.Math.Max(0, Overlay.ActualWidth - 48);

    void Refresh()
    {
        SaveButton.Content = editor.SaveButtonText;
        SaveButton.IsEnabled = editor.FieldsEnabled;
        CancelButton.IsEnabled = editor.CancelEnabled;

        if (ReferenceEquals(shownAlert, editor.SaveAlert)) return;
        shownAlert = editor.SaveAlert;
        var alerts = new List<UIElement>();
        if (editor.SaveAlert is { } alert)
            alerts.Add(AlertView.Create(AppSeverity.Danger, alert.Title, alert.Message, () => editor.SaveAlert = null));
        AlertView.Fill(AlertHost, alerts);
    }

    async void SaveClick(object sender, RoutedEventArgs e)
    {
        if (await editor.SaveAsync()) Close();
    }

    void CancelClick(object sender, RoutedEventArgs e) => Close();

    protected override void OnClosing(CancelEventArgs e)
    {
        base.OnClosing(e);
        if (editor.IsSaving) e.Cancel = true;
    }

    protected override void OnPreviewKeyDown(KeyEventArgs e)
    {
        base.OnPreviewKeyDown(e);
        if (e.Key != Key.Escape || !editor.CancelEnabled) return;
        e.Handled = true;
        Close();
    }
}
