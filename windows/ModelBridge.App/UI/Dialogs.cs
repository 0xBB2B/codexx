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
using System.Windows.Media;

namespace ModelBridge.UI;

public sealed class WindowConfirmer : IConfirmer
{
    public Task<bool> ConfirmAsync(ConfirmRequest request) =>
        Task.FromResult(Dialogs.Show(DialogModel.ForConfirm(request)) == 1);
}

static class Dialogs
{
    public static DialogWindow? Current { get; private set; }

    public static int Show(DialogModel model)
    {
        var dialog = new DialogWindow(model);
        Current = dialog;
        var owner = Application.Current.Windows.OfType<Window>().FirstOrDefault(w => w.IsVisible && w.IsActive && w is not TrayPanelWindow);
        if (owner != null)
        {
            dialog.Owner = owner;
            dialog.WindowStartupLocation = WindowStartupLocation.CenterOwner;
        }
        else dialog.ShowInTaskbar = true;
        try { dialog.ShowDialog(); }
        finally { Current = null; }
        return dialog.Result;
    }
}

static class Motion
{
    public static Duration SwitchDuration =>
        new(SystemParameters.ClientAreaAnimation ? TimeSpan.FromMilliseconds(200) : TimeSpan.Zero);
}

public sealed class Spinner : Control
{
    public static readonly DependencyProperty IsAnimatedProperty =
        DependencyProperty.Register(nameof(IsAnimated), typeof(bool), typeof(Spinner), new PropertyMetadata(SystemParameters.ClientAreaAnimation));

    public bool IsAnimated
    {
        get => (bool)GetValue(IsAnimatedProperty);
        set => SetValue(IsAnimatedProperty, value);
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
            text.Children.Add(new Spinner { Margin = new Thickness(0, 12, 0, 0), HorizontalAlignment = HorizontalAlignment.Left });
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
