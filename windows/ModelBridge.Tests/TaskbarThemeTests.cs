using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.Services;

namespace ModelBridge.Tests;

[TestClass]
public class TaskbarThemeTests
{
    [TestMethod]
    public void RegistryValueOne_IsLight()
    {
        Assert.IsTrue(TaskbarTheme.IsLight(() => 1));
    }

    [TestMethod]
    public void RegistryValueZero_IsDark()
    {
        Assert.IsFalse(TaskbarTheme.IsLight(() => 0));
    }

    [TestMethod]
    public void RegistryValueMissing_IsDark()
    {
        Assert.IsFalse(TaskbarTheme.IsLight(() => null));
    }

    [TestMethod]
    public void LightTaskbar_UsesTrayIconLight()
    {
        Assert.AreEqual("TrayIconLight.ico", TaskbarTheme.IconFor(true));
    }

    [TestMethod]
    public void DarkTaskbar_UsesTrayIconDark()
    {
        Assert.AreEqual("TrayIconDark.ico", TaskbarTheme.IconFor(false));
    }

    [TestMethod]
    public void Dpi96_Uses16Pixels()
    {
        Assert.AreEqual(16, TaskbarTheme.IconSizeFor(96));
    }

    [TestMethod]
    public void DpiAbove96_Uses32Pixels()
    {
        Assert.AreEqual(32, TaskbarTheme.IconSizeFor(97));
        Assert.AreEqual(32, TaskbarTheme.IconSizeFor(144));
    }

    [TestMethod]
    public void DpiBelow96_Uses16Pixels()
    {
        Assert.AreEqual(16, TaskbarTheme.IconSizeFor(72));
    }

    [TestMethod]
    public void AppSources_HaveNoDrawnCircleTrayIcon()
    {
        var dir = AppContext.BaseDirectory;
        while (dir != null && !Directory.Exists(Path.Combine(dir, "ModelBridge.App", "UI")))
            dir = Path.GetDirectoryName(dir);
        Assert.IsNotNull(dir);
        var offenders = Directory.EnumerateFiles(Path.Combine(dir, "ModelBridge.App"), "*.cs", SearchOption.AllDirectories)
            .Where(f => !f.Contains($"{Path.DirectorySeparatorChar}obj{Path.DirectorySeparatorChar}")
                     && !f.Contains($"{Path.DirectorySeparatorChar}bin{Path.DirectorySeparatorChar}"))
            .Where(f => File.ReadAllText(f).Contains("FillEllipse"))
            .ToList();
        CollectionAssert.AreEqual(Array.Empty<string>(), offenders);
    }
}
