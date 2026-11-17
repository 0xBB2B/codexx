using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.Services;

namespace ModelBridge.Tests;

[TestClass]
public class ShellSettingsTests
{
    string root = "";
    string dir = "";

    [TestInitialize]
    public void Setup()
    {
        root = Path.Combine(Path.GetTempPath(), "mb-settings-" + Guid.NewGuid().ToString("N"));
        dir = Path.Combine(root, "nested", "ModelBridge");
    }

    [TestCleanup]
    public void Cleanup()
    {
        if (Directory.Exists(root)) Directory.Delete(root, true);
    }

    [TestMethod]
    public void FilePath_IsShellSettingsJsonInDirectory()
    {
        Assert.AreEqual(Path.Combine(dir, "shell-settings.json"), new ShellSettings(dir).FilePath);
    }

    [TestMethod]
    public void SaveRelays_DirectoryMissing_CreatesItAndRoundTrips()
    {
        var settings = new ShellSettings(dir);

        settings.SaveRelays([new("1", "中转", "https://relay.example.com")]);

        Assert.AreEqual("https://relay.example.com", settings.ReadRelays().Single().BaseUrl);
    }

    [TestMethod]
    public void SaveRelays_Twice_KeepsLatestOnly()
    {
        var settings = new ShellSettings(dir);

        settings.SaveRelays([new("1", "中转", "https://relay.example.com")]);
        settings.SaveRelays([new("1", "中转", "https://relay2.example.com")]);

        Assert.AreEqual("https://relay2.example.com", settings.ReadRelays().Single().BaseUrl);
    }

    [TestMethod]
    public void DeleteAll_NoFile_DoesNotThrow()
    {
        new ShellSettings(dir).DeleteAll();
    }

    [TestMethod]
    public void DeleteAll_DirectoryBecomesEmpty_RemovesDirectory()
    {
        var settings = new ShellSettings(dir);
        settings.SaveRelays([new("1", "中转", "https://relay.example.com")]);
        File.WriteAllText(settings.FilePath + ".bak", "{}");

        settings.DeleteAll();

        Assert.IsFalse(Directory.Exists(dir));
    }

    [TestMethod]
    public void DeleteAll_DirectoryHasOtherFiles_KeepsDirectoryAndFilesWithoutError()
    {
        var settings = new ShellSettings(dir);
        settings.SaveRelays([new("1", "中转", "https://relay.example.com")]);
        var other = Path.Combine(dir, "other.log");
        File.WriteAllText(other, "keep");

        settings.DeleteAll();

        Assert.IsFalse(File.Exists(settings.FilePath));
        Assert.AreEqual("keep", File.ReadAllText(other));
    }
}
