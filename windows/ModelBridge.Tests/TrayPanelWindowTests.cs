using System.Windows;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.UI;

namespace ModelBridge.Tests;

[TestClass]
public class TrayPanelWindowTests
{
    [TestMethod]
    public async Task Header_AppIcon16_8pxBeforeTitle_Padding12()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        await UiHost.RunAsync(async () =>
        {
            var window = new TrayPanelWindow(h.Model, new System.Drawing.Point(100, 100), _ => { });
            try
            {
                await UiHost.ShowAsync(window);

                var icon = UiHost.All<AppIcon>(window).FirstOrDefault(i => i.IsVisible);
                Assert.IsNotNull(icon);
                Assert.AreEqual(16, icon.ActualWidth, 0.5);
                Assert.AreEqual(16, icon.ActualHeight, 0.5);
                Assert.IsFalse(UiHost.All<System.Windows.Shapes.Path>(window).Any(p => p.IsVisible && p.ActualWidth == 20 && p.ActualHeight == 20), "old 20px line icon");

                var title = UiHost.FindText(window, "模型接入助手");
                Assert.IsNotNull(title);
                var iconX = UiHost.PositionIn(window, icon).X;
                Assert.AreEqual(8, UiHost.PositionIn(window, title).X - (iconX + 16), 0.5);

                var host = (FrameworkElement)window.FindName("ItemHost");
                Assert.AreEqual(12, UiHost.PositionIn(window, icon).X - UiHost.PositionIn(window, host).X, 0.5);
                Assert.AreEqual(12, UiHost.PositionIn(window, title).Y - UiHost.PositionIn(window, host).Y, 0.5);
            }
            finally
            {
                if (window.IsVisible) window.Close();
            }
        });
    }

    [TestMethod]
    public async Task Header_TitleIs13SemiBold_StatusLineIs12Muted()
    {
        var h = new MultiHarness();
        await h.StartWithTwoAsync();

        await UiHost.RunAsync(async () =>
        {
            var window = new TrayPanelWindow(h.Model, new System.Drawing.Point(100, 100), _ => { });
            try
            {
                await UiHost.ShowAsync(window);

                var title = UiHost.FindText(window, "模型接入助手");
                Assert.IsNotNull(title);
                Assert.AreEqual(13, title.FontSize);
                Assert.AreEqual(600, title.FontWeight.ToOpenTypeWeight());

                var status = UiHost.FindText(window, h.Model.TrayStatusText);
                Assert.IsNotNull(status);
                Assert.AreEqual(12, status.FontSize);
                var muted = UiHost.ThemeBrush("TextMutedBrush").Color;
                Assert.IsTrue(status.Foreground is System.Windows.Media.SolidColorBrush b && b.Color == muted);
            }
            finally
            {
                if (window.IsVisible) window.Close();
            }
        });
    }
}
