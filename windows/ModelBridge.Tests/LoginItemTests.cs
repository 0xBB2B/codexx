using Microsoft.VisualStudio.TestTools.UnitTesting;
using Microsoft.Win32;
using ModelBridge.Services;

namespace ModelBridge.Tests;

[TestClass]
public class LoginItemTests
{
    const string InstallDir = @"C:\Apps\ModelBridge";

    string subKey = "";
    LoginItem item = null!;

    [TestInitialize]
    public void Setup()
    {
        subKey = @"Software\ModelBridge.Tests\" + Guid.NewGuid().ToString("N");
        item = new LoginItem(InstallDir, subKey, "ModelBridgeTest");
    }

    [TestCleanup]
    public void Cleanup()
    {
        Registry.CurrentUser.DeleteSubKeyTree(subKey, false);
    }

    object? RawValue()
    {
        using var key = Registry.CurrentUser.OpenSubKey(subKey);
        return key?.GetValue("ModelBridgeTest");
    }

    [TestMethod]
    public void Defaults_RunKeyAndValueName()
    {
        Assert.AreEqual(@"Software\Microsoft\Windows\CurrentVersion\Run", LoginItem.DefaultRunKeyPath);
        Assert.AreEqual("ModelBridge", LoginItem.DefaultValueName);
    }

    [TestMethod]
    public void IsEnabled_NothingWritten_ReturnsFalse()
    {
        Assert.IsFalse(item.IsEnabled());
    }

    [TestMethod]
    public void Enable_WritesQuotedExePathWithLoginArg()
    {
        item.Enable();

        Assert.AreEqual("\"C:\\Apps\\ModelBridge\\ModelBridge.exe\" --login", RawValue());
        Assert.IsTrue(item.IsEnabled());
    }

    [TestMethod]
    public void Enable_InstallPathWithSpaces_WritesQuotedPath()
    {
        new LoginItem(@"C:\Program Files\ModelBridge", subKey, "ModelBridgeTest").Enable();

        Assert.AreEqual("\"C:\\Program Files\\ModelBridge\\ModelBridge.exe\" --login", RawValue());
    }

    [TestMethod]
    public void Enable_Twice_KeepsSingleValue()
    {
        item.Enable();
        item.Enable();

        Assert.AreEqual("\"C:\\Apps\\ModelBridge\\ModelBridge.exe\" --login", RawValue());
    }

    [TestMethod]
    public void Disable_RemovesValue()
    {
        item.Enable();

        item.Disable();

        Assert.IsNull(RawValue());
        Assert.IsFalse(item.IsEnabled());
    }

    [TestMethod]
    public void Disable_NothingWritten_DoesNotThrow()
    {
        item.Disable();
    }

    [TestMethod]
    public void IsEnabled_ValueNameInjected_OtherValuesIgnored()
    {
        using (var key = Registry.CurrentUser.CreateSubKey(subKey))
        {
            key.SetValue("SomethingElse", "x");
        }

        Assert.IsFalse(item.IsEnabled());
    }

    [TestMethod]
    public void WasLaunchedAtLogin_WithLoginArg_ReturnsTrue()
    {
        Assert.IsTrue(LoginItem.WasLaunchedAtLogin(["--login"]));
    }

    [TestMethod]
    public void WasLaunchedAtLogin_WithoutLoginArg_ReturnsFalse()
    {
        Assert.IsFalse(LoginItem.WasLaunchedAtLogin([]));
        Assert.IsFalse(LoginItem.WasLaunchedAtLogin(["--other"]));
    }
}
