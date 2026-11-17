using System.Windows.Controls;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.UI;

namespace ModelBridge.Tests;

[TestClass]
public class SetupWindowNameTests
{
    [TestMethod]
    public async Task Setup_NameFieldFirst_DefaultsToRelayName()
    {
        var h = new MultiHarness();
        await h.Model.StartAsync();
        Assert.AreEqual(AppPage.Setup, h.Model.Page);

        await UiHost.RunAsync(async () =>
        {
            var window = new SetupWindow(h.Model);
            try
            {
                await UiHost.ShowAsync(window);

                var nameLabel = UiHost.FindText(window, "名字");
                var urlLabel = UiHost.FindText(window, "Base URL");
                Assert.IsNotNull(nameLabel);
                Assert.IsNotNull(urlLabel);
                Assert.IsTrue(UiHost.PositionIn(window, nameLabel).Y < UiHost.PositionIn(window, urlLabel).Y);

                var firstBox = UiHost.All<TextBox>(window).First(t => t.IsVisible);
                Assert.AreEqual("中转", firstBox.Text);
                Assert.AreEqual("中转", h.Model.NameInput);
            }
            finally
            {
                window.Close();
            }
        });
    }
}
