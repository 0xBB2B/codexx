using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.Diagnostics;
using System.IO;
using System.Linq;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Automation.Peers;
using System.Windows.Controls;
using System.Windows.Interop;
using System.Windows.Media;
using Forms = System.Windows.Forms;

namespace ModelBridge.UI;

public sealed class TaskDialogConfirmer : IConfirmer
{
    public Task<bool> ConfirmAsync(ConfirmRequest request) =>
        Task.FromResult(Dialogs.Choose(request.Title, request.Message, [request.CancelText, request.ConfirmText]) == 1);
}

static class Dialogs
{
    const string Caption = "模型接入助手";

    public static int Choose(string heading, string text, string[] buttons)
    {
        var page = new Forms.TaskDialogPage
        {
            Caption = Caption,
            Heading = heading,
            Text = text,
            AllowCancel = true,
            Icon = Forms.TaskDialogIcon.None,
        };
        foreach (var label in buttons) page.Buttons.Add(new Forms.TaskDialogButton(label));
        page.DefaultButton = page.Buttons[0];

        var owner = Application.Current.Windows.OfType<Window>().FirstOrDefault(w => w.IsVisible && w.IsActive);
        var clicked = owner == null
            ? Forms.TaskDialog.ShowDialog(page)
            : Forms.TaskDialog.ShowDialog(new WindowInteropHelper(owner).Handle, page);
        var index = page.Buttons.IndexOf(clicked);
        return index < 0 ? 0 : index;
    }
}

static class ExternalOpen
{
    public static void Url(string? url)
    {
        if (!Uri.TryCreate(url, UriKind.Absolute, out var uri) || uri.Scheme != Uri.UriSchemeHttps) return;
        Start(new ProcessStartInfo(uri.AbsoluteUri) { UseShellExecute = true });
    }

    public static void FolderOf(string path)
    {
        if (path == "") return;
        var info = File.Exists(path)
            ? new ProcessStartInfo("explorer.exe", $"/select,\"{path}\"")
            : new ProcessStartInfo("explorer.exe", $"\"{Path.GetDirectoryName(path)}\"");
        Start(info);
    }

    static void Start(ProcessStartInfo info)
    {
        try { Process.Start(info)?.Dispose(); }
        catch (Exception ex) when (ex is Win32Exception or InvalidOperationException) { }
    }
}

static class AlertView
{
    public static Border Create(
        AppSeverity severity,
        string title,
        string detail,
        bool showProgress = false,
        params (string Label, Action Click)[] actions)
    {
        var (fg, bg, glyph) = severity switch
        {
            AppSeverity.Success => ("SuccessBrush", "SuccessBgBrush", ""),
            AppSeverity.Warning => ("WarningBrush", "WarningBgBrush", ""),
            AppSeverity.Danger => ("DangerBrush", "DangerBgBrush", ""),
            _ => ("InfoBrush", "InfoBgBrush", ""),
        };
        var foreground = Brush(fg);

        var text = new StackPanel();
        text.Children.Add(new TextBlock
        {
            Text = title,
            FontSize = 13,
            FontWeight = FontWeights.SemiBold,
            Foreground = foreground,
            TextWrapping = TextWrapping.Wrap,
        });
        if (detail != "")
            text.Children.Add(new TextBlock
            {
                Text = detail,
                FontSize = 13,
                Foreground = Brush("TextBrush"),
                TextWrapping = TextWrapping.Wrap,
                Margin = new Thickness(0, 4, 0, 0),
            });
        if (showProgress)
            text.Children.Add(new ProgressBar { IsIndeterminate = true, Height = 4, Margin = new Thickness(0, 12, 0, 0) });
        if (actions.Length > 0)
        {
            var row = new StackPanel { Orientation = Orientation.Horizontal, Margin = new Thickness(0, 12, 0, 0) };
            foreach (var (label, click) in actions)
            {
                var button = new Button { Content = label, MinHeight = 44, Padding = new Thickness(16, 0, 16, 0), Margin = new Thickness(0, 0, 8, 0) };
                button.Click += (_, _) => click();
                row.Children.Add(button);
            }
            text.Children.Add(row);
        }

        var grid = new Grid();
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        grid.ColumnDefinitions.Add(new ColumnDefinition());
        grid.Children.Add(new TextBlock
        {
            Text = glyph,
            FontFamily = new FontFamily("Segoe MDL2 Assets"),
            FontSize = 16,
            Foreground = foreground,
            Margin = new Thickness(0, 0, 12, 0),
            VerticalAlignment = VerticalAlignment.Top,
        });
        Grid.SetColumn(text, 1);
        grid.Children.Add(text);

        var border = new Border
        {
            Style = (Style)Application.Current.FindResource("AlertBorder"),
            BorderBrush = foreground,
            Background = Brush(bg),
            Child = grid,
        };
        System.Windows.Automation.AutomationProperties.SetLiveSetting(
            border,
            severity == AppSeverity.Danger
                ? System.Windows.Automation.AutomationLiveSetting.Assertive
                : System.Windows.Automation.AutomationLiveSetting.Polite);
        return border;
    }

    public static Brush Brush(string key) => (Brush)Application.Current.FindResource(key);

    public static void Fill(Panel host, IEnumerable<UIElement> alerts)
    {
        host.Children.Clear();
        foreach (var alert in alerts)
        {
            ((FrameworkElement)alert).Margin = new Thickness(0, 0, 0, 20);
            host.Children.Add(alert);
            UIElementAutomationPeer.CreatePeerForElement(alert)?.RaiseAutomationEvent(AutomationEvents.LiveRegionChanged);
        }
    }
}
