using Button = System.Windows.Controls.Button;
using Point = System.Windows.Point;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using System.Windows.Threading;

namespace ModelBridge.Tests;

static class UiHost
{
    static readonly Lazy<Dispatcher> Ui = new(Start);

    static Dispatcher Start()
    {
        var ready = new TaskCompletionSource<Dispatcher>();
        var thread = new Thread(() =>
        {
            var app = new Application { ShutdownMode = ShutdownMode.OnExplicitShutdown };
            app.Resources.MergedDictionaries.Add(new ResourceDictionary
            {
                Source = new Uri("pack://application:,,,/ModelBridge;component/UI/Theme.xaml"),
            });
            ready.SetResult(Dispatcher.CurrentDispatcher);
            Dispatcher.Run();
        })
        { IsBackground = true, Name = "ui-sta" };
        thread.SetApartmentState(ApartmentState.STA);
        thread.Start();
        return ready.Task.GetAwaiter().GetResult();
    }

    public static Task RunAsync(Func<Task> body) => Ui.Value.InvokeAsync(body).Task.Unwrap();

    public static Task<T> RunAsync<T>(Func<Task<T>> body) => Ui.Value.InvokeAsync(body).Task.Unwrap();

    public static Task Settle() => Ui.Value.InvokeAsync(() => { }, DispatcherPriority.ApplicationIdle).Task;

    public static async Task ShowAsync(Window window)
    {
        window.WindowStartupLocation = WindowStartupLocation.Manual;
        window.Left = -20000;
        window.Top = -20000;
        window.ShowActivated = false;
        window.Show();
        await Settle();
    }

    public static IEnumerable<DependencyObject> Descendants(DependencyObject root)
    {
        var count = VisualTreeHelper.GetChildrenCount(root);
        for (var i = 0; i < count; i++)
        {
            var child = VisualTreeHelper.GetChild(root, i);
            yield return child;
            foreach (var inner in Descendants(child)) yield return inner;
        }
    }

    public static IEnumerable<T> All<T>(DependencyObject root) where T : DependencyObject =>
        Descendants(root).OfType<T>();

    public static List<string> Texts(DependencyObject root) =>
        All<TextBlock>(root).Where(t => t.IsVisible && t.Text != "").Select(t => t.Text).ToList();

    public static string Joined(DependencyObject root) => string.Join("\n", Texts(root));

    public static TextBlock? FindText(DependencyObject root, string text) =>
        All<TextBlock>(root).FirstOrDefault(t => t.IsVisible && t.Text == text);

    public static List<Button> Buttons(DependencyObject root, string label) =>
        All<Button>(root).Where(b => b.IsVisible && (b.Content as string == label || All<TextBlock>(b).Any(t => t.Text == label))).ToList();

    public static Button? FindButton(DependencyObject root, string label) => Buttons(root, label).FirstOrDefault();

    public static Point PositionIn(Visual root, FrameworkElement element) =>
        element.TransformToAncestor(root).Transform(new Point(0, 0));

    public static IEnumerable<FrameworkElement> Sized(DependencyObject root, double width, double height) =>
        All<FrameworkElement>(root).Where(e => e.IsVisible && Math.Abs(e.ActualWidth - width) < 0.5 && Math.Abs(e.ActualHeight - height) < 0.5);

    public static IEnumerable<FrameworkElement> Ancestors(FrameworkElement element)
    {
        DependencyObject? current = VisualTreeHelper.GetParent(element);
        while (current != null)
        {
            if (current is FrameworkElement fe) yield return fe;
            current = VisualTreeHelper.GetParent(current);
        }
    }

    public static SolidColorBrush ThemeBrush(string key) => (SolidColorBrush)Application.Current.FindResource(key);

    public static string AppSourceDir()
    {
        for (var dir = new DirectoryInfo(AppContext.BaseDirectory); dir != null; dir = dir.Parent)
        {
            var candidate = Path.Combine(dir.FullName, "ModelBridge.App");
            if (File.Exists(Path.Combine(candidate, "ModelBridge.App.csproj"))) return candidate;
        }
        throw new DirectoryNotFoundException("ModelBridge.App");
    }
}
