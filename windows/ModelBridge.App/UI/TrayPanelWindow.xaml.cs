using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.Linq;
using System.Windows;
using System.Windows.Automation;
using System.Windows.Controls;
using System.Windows.Controls.Primitives;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Shapes;

namespace ModelBridge.UI;

public partial class TrayPanelWindow : Window
{
    const double ShadowLeft = 48;
    const double ShadowTop = 32;
    const double ShadowBottom = 64;
    const double ScreenGap = 8;

    readonly List<ButtonBase> items = [];
    readonly System.Drawing.Point anchor;
    readonly AppModel appModel;
    readonly Action<TrayMenuCommand> onCommand;
    bool closed;

    public TrayPanelWindow(AppModel appModel, System.Drawing.Point anchorPixels, Action<TrayMenuCommand> onCommand)
    {
        this.appModel = appModel;
        this.onCommand = onCommand;
        anchor = anchorPixels;
        InitializeComponent();
        Build();

        appModel.PropertyChanged += OnModelChanged;
        Deactivated += (_, _) => CloseOnce();
        Loaded += OnLoaded;
        PanelBorder.SizeChanged += (_, _) =>
        {
            if (IsLoaded) Reposition();
        };
    }

    void Build()
    {
        var model = TrayPanelModel.From(appModel);
        var focusedIndex = items.FindIndex(i => i.IsKeyboardFocused);
        items.Clear();
        ItemHost.Children.Clear();
        PanelBorder.Width = model.Width;
        Title = model.Title;
        ItemHost.Children.Add(BuildHeader(model));
        foreach (var group in model.Groups)
        {
            ItemHost.Children.Add(new Border
            {
                Height = 1,
                Margin = new Thickness(8, 4, 8, 4),
                Background = AlertView.Brush("DividerBrush"),
            });
            foreach (var item in group) ItemHost.Children.Add(BuildItem(item, onCommand));
        }

        if (focusedIndex >= 0) items[focusedIndex].Focus();
    }

    void OnModelChanged(object? sender, PropertyChangedEventArgs e)
    {
        if (!closed) Build();
    }

    protected override void OnClosing(CancelEventArgs e)
    {
        base.OnClosing(e);
        closed = true;
        appModel.PropertyChanged -= OnModelChanged;
    }

    void CloseOnce()
    {
        if (closed) return;
        closed = true;
        Close();
    }

    void OnLoaded(object sender, RoutedEventArgs e)
    {
        Reposition();
        Opacity = 1;
        Activate();
        Focus();
    }

    void Reposition()
    {
        var dpi = VisualTreeHelper.GetDpi(this);
        var area = System.Windows.Forms.Screen.FromPoint(anchor).WorkingArea;
        var width = PanelBorder.ActualWidth * dpi.DpiScaleX;
        var height = PanelBorder.ActualHeight * dpi.DpiScaleY;
        var gap = ScreenGap * dpi.DpiScaleY;

        var left = Math.Clamp(anchor.X - width / 2, area.Left + gap, Math.Max(area.Left + gap, area.Right - width - gap));
        var top = anchor.Y <= area.Top
            ? area.Top + gap
            : Math.Max(area.Top + gap, Math.Min(anchor.Y, area.Bottom) - gap - height);

        Left = left / dpi.DpiScaleX - ShadowLeft;
        Top = top / dpi.DpiScaleY - ShadowTop;
    }

    protected override void OnPreviewKeyDown(KeyEventArgs e)
    {
        base.OnPreviewKeyDown(e);
        switch (e.Key)
        {
            case Key.Escape:
                CloseOnce();
                e.Handled = true;
                break;
            case Key.Down or Key.Up:
                MoveFocus(e.Key == Key.Down ? 1 : -1);
                e.Handled = true;
                break;
        }
    }

    void MoveFocus(int step)
    {
        var enabled = items.Where(i => i.IsEnabled).ToList();
        if (enabled.Count == 0) return;
        var current = enabled.FindIndex(i => i.IsKeyboardFocused);
        var next = current < 0 ? (step > 0 ? 0 : enabled.Count - 1) : (current + step + enabled.Count) % enabled.Count;
        enabled[next].Focus();
    }

    static UIElement BuildHeader(TrayPanelModel model)
    {
        var grid = new Grid { Margin = new Thickness(12) };
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        grid.ColumnDefinitions.Add(new ColumnDefinition());
        grid.Children.Add(new Path
        {
            Data = Geometry.Parse("M3,13 C5,8 15,8 17,13 M6,13 L6,16 M14,13 L14,16 M3,16 L17,16"),
            Width = 20,
            Height = 20,
            Stroke = AlertView.Brush("PrimaryBrush"),
            StrokeThickness = 1.6,
            StrokeStartLineCap = PenLineCap.Round,
            StrokeEndLineCap = PenLineCap.Round,
            Margin = new Thickness(0, 0, 8, 0),
            VerticalAlignment = VerticalAlignment.Center,
        });
        var text = new StackPanel();
        text.Children.Add(new TextBlock { Text = model.Title, FontWeight = FontWeights.SemiBold });
        text.Children.Add(new TextBlock { Text = model.Subtitle, FontSize = 12, Foreground = AlertView.Brush("TextMutedBrush") });
        Grid.SetColumn(text, 1);
        grid.Children.Add(text);
        return grid;
    }

    UIElement BuildItem(TrayMenuItem item, Action<TrayMenuCommand> onCommand)
    {
        var row = new Grid();
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(16) });
        row.ColumnDefinitions.Add(new ColumnDefinition());
        if (item.IsChecked)
            row.Children.Add(new Path
            {
                Data = Geometry.Parse("M3.5,8.5 L6.5,11.5 L12.5,4.5"),
                Width = 16,
                Height = 16,
                Stroke = AlertView.Brush("PrimaryBrush"),
                StrokeThickness = 1.6,
                StrokeStartLineCap = PenLineCap.Round,
                StrokeEndLineCap = PenLineCap.Round,
                VerticalAlignment = VerticalAlignment.Center,
            });

        var label = new StackPanel { Margin = new Thickness(8, 0, 0, 0), VerticalAlignment = VerticalAlignment.Center };
        label.Children.Add(new TextBlock
        {
            Text = item.Label,
            FontWeight = item.Detail != null ? FontWeights.SemiBold : FontWeights.Normal,
        });
        if (!string.IsNullOrEmpty(item.Detail))
            label.Children.Add(new TextBlock { Text = item.Detail, FontSize = 12, Foreground = AlertView.Brush("TextMutedBrush") });
        Grid.SetColumn(label, 1);
        row.Children.Add(label);

        ButtonBase button = item.IsCheckable
            ? new ToggleButton { IsChecked = item.IsChecked }
            : new Button();
        button.Style = (Style)FindResource("MenuItemButton");
        button.Content = row;
        button.IsEnabled = item.IsEnabled;
        AutomationProperties.SetName(button, item.Label);
        button.Click += (_, _) =>
        {
            CloseOnce();
            onCommand(item.Command);
        };
        items.Add(button);
        return button;
    }
}
