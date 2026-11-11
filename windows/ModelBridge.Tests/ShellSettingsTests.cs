using System.Text.Json;
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
    public void ReadBaseUrl_NoFile_ReturnsNull()
    {
        Assert.IsNull(new ShellSettings(dir).ReadBaseUrl());
    }

    [TestMethod]
    public void SaveBaseUrl_DirectoryMissing_CreatesItAndRoundTrips()
    {
        var settings = new ShellSettings(dir);

        settings.SaveBaseUrl("https://relay.example.com");

        Assert.AreEqual("https://relay.example.com", settings.ReadBaseUrl());
    }

    [TestMethod]
    public void SaveBaseUrl_FileContainsOnlyBaseUrlProperty()
    {
        var settings = new ShellSettings(dir);

        settings.SaveBaseUrl("https://relay.example.com");

        using var doc = JsonDocument.Parse(File.ReadAllText(settings.FilePath));
        var names = doc.RootElement.EnumerateObject().Select(p => p.Name).ToArray();
        CollectionAssert.AreEqual(new[] { "base_url" }, names);
    }

    [TestMethod]
    public void SaveBaseUrl_Twice_KeepsLatestOnly()
    {
        var settings = new ShellSettings(dir);

        settings.SaveBaseUrl("https://relay.example.com");
        settings.SaveBaseUrl("https://relay2.example.com");

        Assert.AreEqual("https://relay2.example.com", settings.ReadBaseUrl());
    }

    [TestMethod]
    public void DeleteAll_RemovesFile_ReadReturnsNull()
    {
        var settings = new ShellSettings(dir);
        settings.SaveBaseUrl("https://relay.example.com");

        settings.DeleteAll();

        Assert.IsFalse(File.Exists(settings.FilePath));
        Assert.IsNull(settings.ReadBaseUrl());
    }

    [TestMethod]
    public void DeleteAll_NoFile_DoesNotThrow()
    {
        new ShellSettings(dir).DeleteAll();
    }
}
