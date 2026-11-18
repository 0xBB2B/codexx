using System.Text.Json;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.Services;

namespace ModelBridge.Tests;

[TestClass]
public class ShellSettingsRelaysTests
{
    string root = "";
    string dir = "";

    [TestInitialize]
    public void Setup()
    {
        root = Path.Combine(Path.GetTempPath(), "mb-relays-" + Guid.NewGuid().ToString("N"));
        dir = Path.Combine(root, "ModelBridge");
    }

    [TestCleanup]
    public void Cleanup()
    {
        if (Directory.Exists(root)) Directory.Delete(root, true);
    }

    [TestMethod]
    public void ReadRelays_NoFile_ReturnsEmpty()
    {
        Assert.AreEqual(0, new ShellSettings(dir).ReadRelays().Count);
    }

    [TestMethod]
    public void SaveRelays_ThenRead_KeepsOrderAndFields()
    {
        var settings = new ShellSettings(dir);
        RelayConfig[] saved = [new("1", "中转", "https://relay.example.com/v1"), new("3", "A家", "https://relay2.example.com/v1")];

        settings.SaveRelays(saved);

        CollectionAssert.AreEqual(saved, settings.ReadRelays().ToArray());
    }

    [TestMethod]
    public void SaveRelays_FileHasRelaysArrayWithIdNameBaseUrlOnly()
    {
        var settings = new ShellSettings(dir);

        settings.SaveRelays([new("1", "中转", "https://relay.example.com/v1")]);

        using var doc = JsonDocument.Parse(File.ReadAllText(settings.FilePath));
        var item = doc.RootElement.GetProperty("relays")[0];
        CollectionAssert.AreEqual(new[] { "id", "name", "base_url" }, item.EnumerateObject().Select(p => p.Name).ToArray());
        Assert.IsFalse(doc.RootElement.TryGetProperty("base_url", out _));
    }

    [TestMethod]
    public void LegacyBaseUrl_BecomesSingleRelayNamedZhongzhuanWithId1()
    {
        var settings = new ShellSettings(dir);
        Directory.CreateDirectory(dir);
        File.WriteAllText(settings.FilePath, """{"base_url":"https://old.example.com/v1"}""");

        var relays = settings.ReadRelays();

        CollectionAssert.AreEqual(new[] { new RelayConfig("1", "中转", "https://old.example.com/v1") }, relays.ToArray());
    }

    [TestMethod]
    public void LegacyBaseUrl_IsRewrittenInNewFormat()
    {
        var settings = new ShellSettings(dir);
        Directory.CreateDirectory(dir);
        File.WriteAllText(settings.FilePath, """{"base_url":"https://old.example.com/v1"}""");

        settings.ReadRelays();

        using var doc = JsonDocument.Parse(File.ReadAllText(settings.FilePath));
        Assert.IsFalse(doc.RootElement.TryGetProperty("base_url", out _));
        Assert.AreEqual(1, doc.RootElement.GetProperty("relays").GetArrayLength());
        Assert.AreEqual("https://old.example.com/v1", doc.RootElement.GetProperty("relays")[0].GetProperty("base_url").GetString());
    }

    [TestMethod]
    public void LegacyBaseUrl_ReadTwice_StaysSingleRelay()
    {
        var settings = new ShellSettings(dir);
        Directory.CreateDirectory(dir);
        File.WriteAllText(settings.FilePath, """{"base_url":"https://old.example.com/v1"}""");

        settings.ReadRelays();
        var second = settings.ReadRelays();

        Assert.AreEqual(1, second.Count);
        Assert.AreEqual("1", second[0].Id);
    }

    [TestMethod]
    public void DeleteAll_RemovesFile()
    {
        var settings = new ShellSettings(dir);
        settings.SaveRelays([new("1", "中转", "https://relay.example.com/v1")]);

        settings.DeleteAll();

        Assert.IsFalse(File.Exists(settings.FilePath));
        Assert.AreEqual(0, settings.ReadRelays().Count);
    }

    [TestMethod]
    public void LegacyBaseUrl_WriteBackFails_StillReturnsMigratedList()
    {
        var settings = new ShellSettings(dir);
        Directory.CreateDirectory(dir);
        File.WriteAllText(settings.FilePath, """{"base_url":"https://old.example.com/v1"}""");
        File.SetAttributes(settings.FilePath, FileAttributes.ReadOnly);

        try
        {
            var relays = settings.ReadRelays();

            CollectionAssert.AreEqual(new[] { new RelayConfig("1", "中转", "https://old.example.com/v1") }, relays.ToArray());
        }
        finally
        {
            File.SetAttributes(settings.FilePath, FileAttributes.Normal);
        }
    }

    [TestMethod]
    public void SaveRelays_WriteFails_LeavesOriginalFileIntactAndNoTempFiles()
    {
        var settings = new ShellSettings(dir);
        settings.SaveRelays([new("1", "中转", "https://relay.example.com/v1")]);
        var before = File.ReadAllText(settings.FilePath);
        File.SetAttributes(settings.FilePath, FileAttributes.ReadOnly);

        try
        {
            try
            {
                settings.SaveRelays([new("1", "中转", "https://other.example.com/v1"), new("2", "A家", "https://relay2.example.com/v1")]);
                Assert.Fail("目标文件只读时应该写入失败");
            }
            catch (Exception ex) when (ex is IOException or UnauthorizedAccessException)
            {
            }
        }
        finally
        {
            File.SetAttributes(settings.FilePath, FileAttributes.Normal);
        }

        Assert.AreEqual(before, File.ReadAllText(settings.FilePath));
        CollectionAssert.AreEqual(new[] { settings.FilePath }, Directory.GetFileSystemEntries(dir));
    }

    [TestMethod]
    public void SaveRelays_Success_LeavesNoTempFiles()
    {
        var settings = new ShellSettings(dir);

        settings.SaveRelays([new("1", "中转", "https://relay.example.com/v1")]);
        settings.SaveRelays([new("1", "中转", "https://relay2.example.com/v1")]);

        CollectionAssert.AreEqual(new[] { settings.FilePath }, Directory.GetFileSystemEntries(dir));
    }
}
